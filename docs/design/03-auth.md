# 鉴权与安全

本文档定义 DormWatt 的鉴权模型, 凭据管理, 传输安全, 以及本地客户端的防护. 核心变化是移除 mTLS, 改用可撤销的 Bearer Token, 并把 TLS 终止交给反向代理或服务端可选的证书配置.

## 1. 设计目标

- 服务端可以挂在任意子路径下, 不依赖客户端证书.
- Token 可以分级, 可以单独撤销, 不需要重新签发证书.
- 服务端与客户端实现都简单, 不引入 JWT 签名密钥管理.
- 传输仍然加密, 由反向代理或服务端 TLS 提供.
- 本地 Web GUI 不能被任意网页或本机进程随意调用.

## 2. 服务端 Token

### 2.1 Token 形态

- 随机 32 字节, 经 base64url 无填充编码, 前缀为 `dw_`.
- 示例: `dw_7Q3k...` (完整长度 46 字符).
- 前缀仅用于识别与日志脱敏, 不参与鉴权.
- Token 只在创建时显示一次, 之后服务端只保存 SHA-256 摘要.

### 2.2 存储位置

主存储为 SQLite `tokens` 表, 由服务端 CLI 管理:

```sql
CREATE TABLE tokens (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL UNIQUE,
  token_prefix  TEXT NOT NULL,
  token_sha256  TEXT NOT NULL UNIQUE,
  scopes        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER,
  revoked_at    INTEGER
);
```

- `token_sha256` 为小写十六进制.
- `scopes` 为逗号分隔字符串, 例如 `read,write`.
- `last_used_at` 最多每分钟更新一次, 避免每个请求都写库.
- 撤销为设置 `revoked_at`, 不物理删除, 便于审计.
- 已撤销 token 立即失效, 无需重启服务端.

可选静态 token 用于声明式部署:

```toml
[[auth.static_tokens]]
name = "monitoring"
scopes = ["read"]
sha256 = "0123abcd..."
```

- 静态 token 与数据库 token 同时生效.
- 静态 token 只读建议, 修改后发送 `SIGHUP` 或重启生效.
- 如果同时存在同名 token, 数据库 token 优先, 启动时记录警告.

### 2.3 权限范围

| scope | 能力 |
|-------|------|
| `read` | GET `/v1/status`, `/v1/degree`, `/v1/records`, `/v1/rooms*`, `/v1/ecnu/*`, `/v1/session`, `/v1/archives*` 下载 |
| `write` | 包含 `read`, 并允许 PUT/DELETE `/v1/session`, 修改房间, 创建与删除归档 |

规则:

- `write` 隐含 `read`.
- 没有 `admin` scope. 服务端管理操作只能通过本机 CLI 执行, 不通过 HTTP 暴露.
- `GET /v1/health` 不需要 token.
- `GET /v1/whoami` 需要任意有效 token, 不要求具体 scope.

### 2.4 Token 管理命令

```shell
# 创建, 输出一次完整 token
dormwatt server token create --name alice-laptop --scope read,write

# 列表, 只显示前缀
dormwatt server token list

# 撤销
dormwatt server token revoke --name alice-laptop

# 轮换, 生成新 token 并撤销旧 token
dormwatt server token rotate --name alice-laptop
```

行为约定:

- `create` 发现同名未撤销 token 时拒绝, 提示先撤销或轮换.
- `create` 输出完整 token 后不再提供找回方式.
- `rotate` 在一个事务内创建新 token 并撤销旧 token, 只输出新 token.
- `list` 显示 name, prefix, scopes, created_at, last_used_at, 是否撤销.
- 命令操作数据库文件, 需要与运行中的服务端同一用户或至少有写权限.
- 服务端每个请求实时查询 token 状态, 撤销立即生效.

### 2.5 请求头

```http
Authorization: Bearer dw_7Q3k...
```

- scheme 大小写不敏感.
- 只接受一个 `Authorization` 头, 多个或空值一律 401.
- 不接受 URL query, Cookie 或表单中的 token.
- 代理层不得把 token 写入访问日志.

### 2.6 校验流程

1. 提取 Bearer token, 格式不合法直接 401.
2. 计算 SHA-256.
3. 在数据库中按摘要查找未撤销记录.
4. 未找到则查静态 token 列表.
5. 校验所需 scope.
6. 把 token name 与 scopes 放入请求上下文, 供审计日志使用.

失败响应不透露 token 是否存在, 是否过期或是否撤销, 统一为:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="dormwatt"
Content-Type: application/problem+json
```

```json
{
  "type": "about:blank",
  "title": "authentication required",
  "status": 401,
  "code": "unauthorized",
  "detail": "missing or invalid bearer token",
  "request_id": "..."
}
```

## 3. 传输安全

### 3.1 三种部署形态

| 形态 | 说明 | 适用 |
|------|------|------|
| 反向代理终止 TLS | Caddy, Nginx 等对外提供 HTTPS, 服务端只监听回环 | 推荐 |
| 服务端直接 TLS | `server.tls.cert` 与 `server.tls.key` | 无代理的小规模部署 |
| 明文 HTTP | 只允许回环地址或显式开启 | 本地开发 |

### 3.2 配置

```toml
[server]
# bind 使用默认值, 具体端口见 04-data-model.md.

# 允许在非回环地址上使用明文 HTTP. 默认 false.
allow_insecure_http = false

# 可选. 配置后服务端直接提供 HTTPS.
[server.tls]
cert = "/path/to/fullchain.pem"
key = "/path/to/privkey.pem"

[auth]
# 可信反向代理 CIDR. 只有来自这些地址的转发头才被采信.
trusted_proxies = ["127.0.0.1/32", "::1/128"]
```

启动规则:

- 未配置 TLS, 且 `bind` 不是回环地址, 且 `allow_insecure_http=false` 时, 拒绝启动并给出明确错误.
- `bind` 是回环地址时允许明文, 用于配合本机反向代理.
- 开启 `allow_insecure_http` 时启动日志输出醒目警告.
- 旧 mTLS 配置项 `server_cert`, `server_key`, `root_ca` 不再识别, 启动时提示迁移.

### 3.3 转发头

- 仅当对端地址命中 `trusted_proxies` 时, 采信 `X-Forwarded-For` 与 `X-Forwarded-Proto`.
- `X-Forwarded-Prefix` 不参与路由, 也不参与生成响应链接. 外部挂载前缀由反向代理决定, 服务端只识别 `/v1/...`.
- 转发头只用于日志记录与速率限制的客户端识别, 不用于鉴权判断.
- 多级代理时从右向左取第一个不可信地址.

### 3.4 速率限制

默认值, 均可配置:

| 对象 | 限制 |
|------|------|
| 认证失败 | 每 IP 每分钟 20 次, 超过后 429, `Retry-After: 60` |
| 普通请求 | 每 token 每分钟 300 次, 突发 60 |
| 上游触发型请求 (`fresh=true`, 目录刷新) | 每 token 每分钟 30 次 |

实现使用令牌桶, 状态保存在内存, 重启清零. 多实例部署不在 V1 范围, 因此不需要共享计数器.

## 4. 凭据保护

### 4.1 ECNU 会话

- 会话三元组 `JSESSIONID`, `cookie`, `X-CSRF-TOKEN` 属于敏感凭据.
- 默认持久化到 SQLite `session` 表, 数据库文件权限 0600, 目录 0700.
- 可通过 `[ecnu] persist_session = false` 改为仅内存保存, 重启后需要重新上传.
- 不提供任何返回会话原值的接口.
- 日志中禁止出现字段名对应的值, 只允许输出 `session updated` 一类事件.
- 上传接口的请求体不记录, 错误信息也不回显内容.

### 4.2 客户端 Token

- 客户端配置 `client.toml` 含服务端 token 时, 文件权限必须为 0600.
- 写入配置时如果权限过宽, 自动收紧到 0600 并记录警告.
- `dormwatt client config show` 只显示 token 前缀与是否存在, 不显示完整值.
- 未来可接入系统钥匙串, V1 不做.

### 4.3 日志脱敏

必须脱敏的键与头:

- `Authorization`
- `Cookie`, `Set-Cookie`
- `X-CSRF-TOKEN`
- `jsession_id`, `x_csrf_token`, `token`, `password`, `secret`
- 请求体中出现上述字段的值

日志结构建议:

```json
{
  "level": "INFO",
  "time": "2026-01-24T14:35:32+08:00",
  "component": "http",
  "request_id": "2f9c1a7b8e4d4c3a",
  "method": "PUT",
  "path": "/dormwatt/api/v1/session",
  "status": 204,
  "duration_ms": 12,
  "token_name": "alice-laptop",
  "remote_ip": "203.0.113.7"
}
```

## 5. 本地客户端防护

客户端 `serve` 会在回环地址监听, 托管 Web GUI 并代理服务端 API. 本机任何进程都可能访问它, 浏览器页面也可能通过 DNS rebinding 或 CSRF 尝试访问, 因此需要单独防护.

### 5.1 监听地址

- 默认监听回环地址, 端口由 `client.toml` 的 `local.bind` 决定.
- 非回环地址默认拒绝, 除非设置 `client.allow_remote = true`.
- 开启 `allow_remote` 时启动日志警告, 并要求所有本地 API 请求携带本地 token.
- 系统托盘与守护进程同进程, 不新增监听端口, 也不新增凭据.
- 托盘退出直接调用进程内关闭函数, 不经过 HTTP; `POST /api/shutdown` 需要本地会话.

### 5.2 本地 token

- 守护进程启动时生成 32 字节随机 token, 前缀 `lw_`.
- 写入运行状态文件:

```json
{
  "pid": 12345,
  "port": <local_port>,
  "token": "lw_...",
  "started_at": "2026-01-24T14:30:00+08:00"
}
```

- 示例中的 `<local_port>` 为占位符, 实际值取自 `local.bind`.
- 文件位于数据目录, 权限 0600.
- 正常退出时删除, 异常退出时由下次启动覆盖.
- CLI 需要访问本地 API 时读取该文件.
- token 不写入配置文件, 不写入日志, 不出现在 URL.

### 5.3 浏览器会话

- 访问 `GET /` 时, 守护进程设置 Cookie:
  - 名称 `dormwatt_local`
  - 值: 当前本地 token
  - `HttpOnly`, `SameSite=Strict`, `Path=/`, 无 `Secure` (回环 HTTP)
- `/api/*` 同时接受:
  - `Cookie: dormwatt_local=...`
  - `Authorization: Bearer lw_...` (供 CLI 使用)
- Cookie 值使用常量时间比较.

### 5.4 防 CSRF 与 DNS rebinding

每个 `/api/*` 请求都必须满足:

1. `Host` 必须是 `127.0.0.1:<port>`, `localhost:<port>` 或 `[::1]:<port>` 之一.
2. 非 GET/HEAD 请求的 `Origin`, 如果存在, 必须与 `http://<host>` 完全一致.
3. 非 GET/HEAD 请求必须使用 `Content-Type: application/json`.
4. 非 GET/HEAD 请求必须带 `X-Dormwatt-Client: 1`.
5. 不返回任何 CORS 头. `OPTIONS` 请求返回 405.
6. 响应加 `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`.
7. Content-Security-Policy 只允许同源脚本与样式, 不使用内联脚本.

说明:

- 浏览器跨源请求无法自行添加 `X-Dormwatt-Client`, 预检也会失败.
- 表单提交无法设置 `Content-Type: application/json`, 因此被拒绝.
- DNS rebinding 攻击中 `Host` 会变成攻击者域名, 被第 1 条拒绝.
- `SameSite=Strict` 进一步降低跨站携带 Cookie 的可能.

### 5.5 本地 API 权限

- 本地 API 不提供管理服务端 token 的能力.
- 修改本地配置时, 服务端 token 字段只可写不可读.
- 本地 API 代理上游时使用客户端配置中的服务端 token, 不把 token 返回给浏览器.
- Web GUI 不能直接访问上游服务端, 避免服务端 token 落入浏览器存储.

## 6. 威胁模型

| 威胁 | 缓解 |
|------|------|
| 公网窃听 | HTTPS 由代理或服务端提供 |
| Token 泄露 | 可撤销, 最小 scope, 日志脱敏, 文件 0600 |
| 暴力猜 token | 32 字节随机, 认证失败限速 |
| 撤销不及时 | 每请求查库, 撤销立即生效 |
| 恶意网页调用本地 API | Host/Origin 检查, 自定义头, SameSite Cookie, 无 CORS |
| DNS rebinding | Host 白名单 |
| 同机恶意进程 | 无法防御, 按同一信任域处理 |
| 磁盘被读取 | 数据库与配置文件 0600, 建议全盘加密 |
| 反向代理配置错误 | 启动时拒绝非回环明文, 文档给出正确代理示例 |
| 旧证书遗留 | 启动时检测旧配置字段并提示移除 |

## 7. 不考虑的方案

| 方案 | 不采用原因 |
|------|------------|
| mTLS | 证书分发与续期成本高, 代理终止 TLS 后客户端证书验证复杂, 与用户需求冲突 |
| Basic Auth | 无法分级, 撤销粒度差, 浏览器与 CLI 都容易缓存凭据 |
| JWT | 需要签名密钥与撤销列表, 对单用户场景过重 |
| 反向代理认证头 | 依赖代理严格配置, 容易伪造, 留作未来可选模式 |
| OAuth2 设备码 | 引入授权服务器, 与自托管单实例目标不符 |

## 8. 迁移步骤

旧部署升级时:

1. 移除 `[tls]` 配置或只保留服务端证书, 删除客户端证书与根证书.
2. 在服务端执行 `dormwatt server token create --name <客户端名> --scope read,write`.
3. 把 token 写入客户端配置 `[server] token = "..."`.
4. 删除客户端证书路径配置.
5. 启动服务端, 用 `dormwatt client doctor` 验证 401 不再出现.
6. 确认反向代理保留 `/dormwatt/api` 前缀并传递 `Authorization` 头.

`epu-cert` 工具不再需要, 可以删除.
