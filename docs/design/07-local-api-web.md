# 本地 API 与 Web GUI

客户端 `dormwatt client serve` 在回环地址监听, 同时承担三个角色:

1. 托管 Web GUI 静态资源.
2. 作为同源代理, 把浏览器请求转发到远端服务端.
3. 在桌面会话中显示系统托盘, 提供打开页面, 查看状态与退出入口.

这样浏览器不需要跨域访问服务端, 服务端 token 也不会进入浏览器存储.

## 1. 端口与路径

默认:

```
http://127.0.0.1:20532/
  /                  Web GUI
  /assets/*          构建产物
  /api/*             本地 API
```

- 监听地址来自 `client.toml` 的 `local.bind`.
- 端口被占用时启动失败, 输出占用进程提示.
- 非回环地址需要 `local.allow_remote = true`, 且所有请求必须携带本地 token.
- 不支持 HTTPS 本地监听. 回环 HTTP 在浏览器中属于安全上下文, Notification API 可用.

## 2. 本地鉴权

详细威胁模型见 [03-auth.md](03-auth.md), 实现要点如下.

### 2.1 本地 token

- 启动时生成 32 字节随机值, 前缀 `lw_`.
- 写入 `<data>/client/runtime.json`, 权限 0600.
- 正常退出删除.
- CLI 通过该文件访问本地 API.

### 2.2 浏览器会话

- `GET /` 响应设置 Cookie, SPA fallback 返回 `index.html` 时同样设置 Cookie, 保证直接打开 `/records` 或 `/settings` 也能获得会话:

```
Set-Cookie: dormwatt_local=<token>; HttpOnly; SameSite=Strict; Path=/
```

- `/api/*` 接受 Cookie 或 `Authorization: Bearer lw_...`.
- 常量时间比较.

### 2.3 请求校验

对每个 `/api/*` 请求:

1. `Host` 必须是 `127.0.0.1:<port>`, `localhost:<port>` 或 `[::1]:<port>`.
2. 非 GET/HEAD 的 `Origin` 如果存在, 必须与 `http://<host>` 一致.
3. 非 GET/HEAD 必须是 `Content-Type: application/json`.
4. 非 GET/HEAD 必须带 `X-Dormwatt-Client: 1`.
5. 无 CORS 响应头.
6. `OPTIONS` 返回 405.

失败返回:

```json
{
  "type": "about:blank",
  "title": "local request rejected",
  "status": 403,
  "code": "local_forbidden",
  "detail": "host, origin or client header check failed",
  "request_id": "..."
}
```

### 2.4 安全响应头

所有页面与 API 响应:

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'
```

说明:

- 不使用内联脚本.
- ECharts 会写内联 style, 因此 `style-src` 允许 `unsafe-inline`.
- 不引用任何外部 CDN.

## 3. 本地 API 契约

本地 API 与远端 API 形状保持一致, 便于代理层透传. 额外增加本地状态字段.

### 3.1 GET /api/health

需要本地会话, 与其它 `/api/*` 一致. 用于确认守护进程存活. 浏览器在加载首页时已获得 Cookie, CLI 使用 runtime token.

```json
{
  "status": "ok",
  "version": "0.1.0",
  "pid": 12345
}
```

### 3.2 GET /api/status

聚合本地状态与远端 `/v1/status`:

```json
{
  "local": {
    "version": "0.1.0",
    "uptime_seconds": 120,
    "health": "ok",
    "health_detail": null,
    "last_check_at": "2026-01-24T14:35:32+08:00",
    "consecutive_failures": 0,
    "upstream_base": "https://example.com/dormwatt/api"
  },
  "remote": {
    "version": "0.1.0",
    "server_time": "2026-01-24T14:35:32+08:00",
    "uptime_seconds": 3600,
    "active_room": {
      "id": 1,
      "room_no": "4408_MH_83_257",
      "elcarea": 102,
      "elcbuis": "new-83_MH"
    },
    "session": {
      "state": "ok",
      "updated_at": "2026-01-24T14:30:00+08:00"
    },
    "monitor": {
      "poll_interval_seconds": 300,
      "last_attempt_at": "2026-01-24T14:35:30+08:00",
      "last_success_at": "2026-01-24T14:35:30+08:00",
      "last_degree": 33.43,
      "last_error": null,
      "next_poll_at": "2026-01-24T14:40:30+08:00"
    },
    "storage": {
      "samples": 1234,
      "archives": 2
    }
  }
}
```

- 远端不可达时 `remote` 为 `null`, `local.health` 给出分类, `local.health_detail` 给出短句.
- 该接口不触发上游 ECNU 查询.
- 页面轮询该接口, 默认 5 秒.

### 3.3 透传接口

以下接口直接转发到远端对应 `/v1/...`, 保留状态码与 problem+json 响应体, 并补充本地 `request_id`.

| 本地路径 | 远端路径 | 方法 |
|----------|----------|------|
| `/api/degree` | `/v1/degree` | GET |
| `/api/records` | `/v1/records` | GET |
| `/api/rooms` | `/v1/rooms` | GET, POST |
| `/api/rooms/active` | `/v1/rooms/active` | GET, DELETE |
| `/api/rooms/{id}/active` | `/v1/rooms/{id}/active` | PUT |
| `/api/rooms/{id}/info` | `/v1/rooms/{id}/info` | GET |
| `/api/ecnu/districts` | `/v1/ecnu/districts` | GET |
| `/api/ecnu/buildings` | `/v1/ecnu/buildings` | GET |
| `/api/ecnu/floors` | `/v1/ecnu/floors` | GET |
| `/api/ecnu/rooms` | `/v1/ecnu/rooms` | GET |
| `/api/session` | `/v1/session` | GET, PUT, DELETE |
| `/api/archives` | `/v1/archives` | GET, POST |
| `/api/archives/{name}` | `/v1/archives/{name}` | GET, DELETE |

透传规则:

- 查询参数原样保留.
- 请求体限定为 JSON 且不超过 64 KiB.
- 响应体 JSON 直接透传, 不重写字段.
- CSV 下载流式转发, 不缓冲.
- 远端 4xx/5xx 原样返回, 前端按 `code` 处理.
- 远端不可达时返回 502, code 为 `upstream_unreachable`.
- 远端 token 无效时返回远端原始 401, code 为 `unauthorized`, 前端提示重新配置 token.
- 本地不缓存记录与归档内容.

### 3.4 GET /api/settings

```json
{
  "server": {
    "base_url": "https://example.com/dormwatt/api",
    "token_set": true,
    "token_prefix": "dw_7Q3k",
    "request_timeout": "20s"
  },
  "local": {
    "bind": "127.0.0.1:20532",
    "allow_remote": false,
    "tray": "auto"
  },
  "monitor": {
    "poll_interval": "10s",
    "degree_threshold": 10.0
  },
  "ui": {
    "timezone": "Asia/Shanghai",
    "refresh_interval": "5s"
  }
}
```

- 永不返回 token 原值.

### 3.5 PUT /api/settings

请求为部分更新:

```json
{
  "server": {
    "base_url": "https://example.com/dormwatt/api",
    "token": "dw_..."
  },
  "monitor": {
    "degree_threshold": 8.0
  },
  "ui": {
    "timezone": "Asia/Shanghai"
  }
}
```

规则:

- `token` 只写不读. 空字符串表示不修改, 显式 `null` 表示清除.
- 修改 `base_url` 或 `token` 后, 立即重建上游客户端并做一次健康检查.
- 修改 `local.bind` 需要重启守护进程, 接口返回 `restart_required: true`.
- 修改阈值与时区立即生效.
- 写入配置文件使用临时文件与 rename, 权限 0600.
- 保存失败返回 500 `config_write_failed`.
- 响应返回更新后的 `/api/settings` 结构.

### 3.6 POST /api/shutdown

请求:

```http
POST /api/shutdown
Content-Type: application/json
X-Dormwatt-Client: 1

{}
```

响应:

```json
{
  "status": "shutting_down"
}
```

- 202 Accepted, 随后异步关闭.
- 需要本地会话.
- 只允许来自回环 Host 且通过 Origin / 自定义头检查的请求.
- 托盘 Quit 与 `dormwatt client quit` 使用同一关闭路径.
- Web GUI 默认不提供退出按钮, 避免误触. 可在设置页提供二次确认入口.
- 关闭流程: 停止接受新请求, 取消轮询, 删除 `runtime.json`, 调用 `systray.Quit`, 退出进程.

### 3.7 可选接口

以下接口列为后续阶段, V1 不实现:

- `POST /api/session/browser-login`: 由守护进程拉起浏览器完成登录, 前端轮询状态.
- `GET /api/events`: SSE 推送状态与样本变化.
- `GET /api/stats`: 服务端聚合统计, 减少前端计算.
- `POST /api/notifications/test`: 测试浏览器通知.

## 4. 状态与轮询

### 4.1 守护进程状态机

```
starting
  -> ok
  -> no_room
  -> not_logged_in
  -> server_down
  -> no_net
  -> tls_error
  -> upstream_error
  -> unknown
```

判定顺序:

1. 读取本地配置, 检查 base_url 与 token 是否存在. 缺失为 `unknown`, detail 提示配置.
2. 请求远端 `/v1/health`.
3. 传输错误:
   - TLS 错误 -> `tls_error`.
   - 本机联网探测失败 -> `no_net`.
   - 其他 -> `server_down`.
4. 请求远端 `/v1/status`.
5. `active_room == null` -> `no_room`.
6. `session.state` 为 `missing` 或 `invalid` -> `not_logged_in`.
7. `monitor.last_error` 存在 -> `upstream_error`.
8. 其他 -> `ok`.

去抖:

- `server_down` 与 `no_net` 连续 3 次才切换.
- 其他状态立即切换.
- 状态变化记录 info 日志.

### 4.2 轮询周期

| 对象 | 周期 | 说明 |
|------|------|------|
| 守护进程 -> 远端 status | `monitor.poll_interval`, 默认 10s | 不触发上游 |
| 页面 -> 本地 status | `ui.refresh_interval`, 默认 5s | 读取缓存 |
| 页面 -> 记录 | 进入页面或手动刷新 | 不自动高频 |
| 页面 -> 归档列表 | 10s | 仅在归档页 |

- 守护进程对远端 status 做 2 秒缓存, 多个标签页同时请求也只打一次.
- 页面隐藏时暂停轮询, 重新可见时立即刷新一次.

### 4.3 低电量判断

- 页面读取 `remote.monitor.last_degree` 与 `monitor.degree_threshold` 比较.
- 低于阈值时显示横幅.
- 浏览器通知只在状态从未低于阈值变为低于阈值时发送, 并限制 30 分钟一次.
- 电量回升后重置通知节流.
- 若用户希望电量上升也通知, 在设置中提供开关, 默认开启.
- 判断使用服务端最后样本, 不额外触发上游查询.

### 4.4 托盘状态

- 守护进程每次完成状态轮询后, 把状态推给托盘模块.
- 托盘模块只读取内存状态, 不额外发 HTTP 请求.
- 状态映射:

| 健康状态 | 图标 | 菜单首项 |
|----------|------|----------|
| `ok` | 常规 | `当前电量: 33.43 度` |
| `no_room` | 灰色 | `未选择房间` |
| `not_logged_in` | 灰色 | `ECNU 登录已失效` |
| `server_down` | 红色 | `服务端不可达` |
| `no_net` | 红色 | `网络已断开` |
| `tls_error` | 红色 | `TLS 校验失败` |
| `upstream_error` | 黄色 | `上游查询失败` |
| `unknown` | 灰色 | `状态未知` |

- 低电量与错误状态可叠加, 低电量优先使用黄色图标.
- 菜单操作:
  - 打开 Web 界面: 用默认浏览器打开本地地址.
  - 立即刷新: 触发一次状态轮询, 不直接查上游.
  - 登录帮助: 打开 `http://127.0.0.1:<port>/settings#login`.
  - 退出: 调用统一关闭流程.
- 托盘不读取 token, 不显示 cookie.
- 托盘与 Web 页面可同时存在, 状态以守护进程缓存为准, 不互相覆盖.

## 5. Web GUI

页面布局, 组件, 状态与交互细节见 [10-web-ui-spec.md](10-web-ui-spec.md). 本章定义范围与结构.

### 5.1 页面结构

```
/              仪表盘
/records       记录与图表
/archives      归档
/settings      设置
```

侧边栏:

- 当前电量与状态徽标.
- 导航.
- 版本号与守护进程状态.

### 5.2 仪表盘

- 当前剩余电量, 大字显示.
- 健康状态横幅: ok, 未选房, 未登录, 服务端不可达, 断网, TLS 错误.
- 最近 24 小时或 7 天曲线.
- 最近一次采样时间与下一次采集时间.
- 快捷按钮: 刷新, 登录帮助, 选择房间.

### 5.3 记录页

- 时间范围选择: 24 小时, 7 天, 30 天, 全部, 自定义.
- 图例: 剩余电量曲线, 用量曲线.
- 差值计算在前端完成:

```
delta[i] = degree[i-1] - degree[i]
每日用量 = 当日样本差值求和
```

- 支持缩放与拖动.
- 框选时间范围后显示统计卡:
  - 起止时间.
  - 样本数.
  - 用量总和.
  - 日均用量.
  - 预计可用天数.
- 统计卡提供 "归档此范围" 按钮, 打开创建归档对话框, 预填时间范围.
- 表格视图与图表视图切换.
- CSV 导出按钮调用 `/api/records?format=csv`.

### 5.4 归档页

- 卡片或表格列出归档: 名称, 起止时间, 样本数, 创建时间.
- 新建归档: 时间范围与可选名称.
- 下载: 直接链接 `/api/archives/{name}`.
- 删除: 二次确认, 说明是软删除.
- 空状态提示先创建归档.

### 5.5 设置页

分组:

1. 服务端连接
   - base URL.
   - token, 密码输入框, 留空表示不修改.
   - 测试连接按钮, 调用 `/api/settings` 保存后触发检查, 或单独 `GET /api/status`.
2. 登录
   - 当前会话状态.
   - 手动上传 JSESSIONID / cookie / X-CSRF-TOKEN 的表单.
   - 说明: 推荐在终端执行 `dormwatt client login`.
   - 表单提交后清空字段.
3. 房间
   - 当前房间信息.
   - 级联选择器: 地区, 建筑, 楼层, 房间.
   - 切换房间按钮.
   - 清除房间按钮.
4. 通知与显示
   - 低电量阈值.
   - 时区.
   - 浏览器通知授权按钮.
   - 电量回升通知开关.
5. 关于
   - 客户端与服务端版本.
   - 配置路径.
   - 日志路径.
   - 托盘状态: 已启用 / 已关闭 / 当前平台不支持.
   - 退出守护进程按钮, 二次确认后调用 `POST /api/shutdown`.

### 5.6 登录帮助

- `not_logged_in` 时显示横幅:

```
ECNU 登录已失效. 在终端运行 dormwatt client login 重新登录,
或在设置页手动填入浏览器中的会话信息.
```

- 不提供网页内嵌 ECNU 登录, 因为跨域与安全边界复杂, 且学校 SSO 可能设置 `X-Frame-Options`.
- 手动表单只发送到本地守护进程, 再由守护进程用 token 转发到服务端.

## 6. 前端技术方案

| 项 | 选择 |
|----|------|
| 框架 | Vue 3 + TypeScript |
| 构建 | Vite |
| 路由 | Vue Router, history 模式, 本地服务提供 SPA fallback |
| 图表 | ECharts |
| 样式 | 原生 CSS 变量 + 少量组件样式, 不引入大型 UI 框架 |
| HTTP | 原生 `fetch` 封装, 统一处理 problem+json |
| 时间 | `Intl.DateTimeFormat` 与时区配置 |
| 状态 | 组合式函数, 不引入 Pinia, 页面规模小 |

可复用旧前端:

- 曲线图配置, 缩放, 双 Y 轴思路.
- 差值, 速度, 统计窗口切分算法.
- 归档对话框交互.
- 状态弹窗文案.

必须重写:

- 所有 `invoke` 调用改为 `fetch('/api/...')`.
- 删除证书选择与 TLS 配置.
- 删除 Tauri 窗口隐藏, 单实例插件与托盘实现代码, 托盘改用 Go 重写.
- 归档保存路径改为浏览器下载.
- 系统通知改为 Web Notification.
- 房间选择改为级联表单, 不再注入浏览器页面.

## 7. 本地服务实现要点

- 使用 `embed.FS` 打包 `web/dist`.
- 静态资源:
  - 文件名带 hash 的资源 `Cache-Control: public, max-age=31536000, immutable`.
  - `index.html` 使用 `Cache-Control: no-store`.
- SPA fallback: 非 `/api/*` 且非静态文件的 GET 请求返回 `index.html`.
- 不启用 service worker, 避免旧版本缓存.
- 只监听回环, 不使用 `0.0.0.0`.
- 请求日志记录 method, path, status, 耗时, request_id, 不记录 body.
- 代理错误统一转换为 problem+json.
- 关闭时等待进行中的代理请求, 最多 5 秒.

## 8. 浏览器通知

- 页面加载后检查 `Notification.permission`.
- 未授权时在设置页提供授权按钮, 不在页面加载时自动弹窗.
- 低电量通知内容:

```
标题: 宿舍电量不足
正文: 当前剩余 8.42 度, 低于阈值 10.00 度.
```

- 电量回升通知:

```
标题: 宿舍电量增加
正文: 电量从 5.20 度增加到 30.00 度.
```

- 多标签页去重:
  - 使用 `localStorage` 记录 `last_notify_at` 与 `last_notify_kind`.
  - 发送前检查 30 分钟窗口.
  - 多标签页并发写入存在竞态, 视为可接受, 最坏重复一次.
- 浏览器通知只在页面打开时生效, 页面隐藏时暂停轮询.
- 页面关闭后的提醒由守护进程的系统通知负责, 见 8.1.

### 8.1 系统通知

守护进程通过平台原生通知发送提醒, 点击通知打开本地 Web.

- 图标由程序生成 64x64 PNG, 缓存在客户端运行目录.
- Linux 使用 `notify-send` 的 action 与 wait, 点击后 `xdg-open`.
- Windows 使用 PowerShell Toast 的 protocol launch.
- macOS 优先 `terminal-notifier`; 未安装时使用 `osascript` 的 JXA ObjC 桥, 点击后打开本地 Web, 无需额外安装.
- 健康状态变化: 服务端不可达, 断网, TLS 错误, 未登录, 未选房, 上游异常.
- 状态恢复: 从异常回到 `ok`.
- 低电量: 首次立即通知, 之后 30 分钟最多一次.
- 电量回升: 电量增加时通知, 10 分钟最多一次.
- 通知内容不包含 token, cookie 或 CSRF.
- 无桌面会话时不启用.
- 开关来自 `client.toml` 的 `local.notify`, 默认 true.
- 托盘模式与无托盘模式都生效.
- 发送失败只记录 warn 日志, 不影响轮询.

## 9. 与旧 Tauri 客户端的能力对照

| 能力 | 旧实现 | 新实现 |
|------|--------|--------|
| 电量曲线 | 有 | 有 |
| 记录表格 | 有 | 有 |
| 框选统计 | 有 | 有 |
| 归档创建/下载/删除 | 有 | 有 |
| 归档下载本地缓存 | 有 | 浏览器下载, 不缓存 |
| 低电量通知 | 系统通知 | 系统通知 + Web Notification; 页面关闭后仍提醒 |
| 电量增加通知 | 系统通知 | 系统通知 + Web Notification |
| 状态异常通知 | 系统通知 | 系统通知, 含服务端不可达, 断网, TLS 错误, 未登录, 未选房 |
| 系统托盘 | 有 | 有, `fyne.io/systray`, 三平台尽力适配 |
| 托盘显示状态 | 有 | 有, 图标与菜单首项 |
| 托盘打开页面 | 有 (窗口) | 有 (默认浏览器) |
| 托盘退出 | 有 | 有, 统一关闭流程 |
| 关窗隐藏 | 有 | 不适用, 浏览器标签页 |
| 单实例 | 有 | 端口占用与 runtime 文件检测 |
| 浏览器自动登录 | 有 | CLI `login` 有, Web 暂不提供 |
| 浏览器选房 | 有 | 目录接口 + 级联选择 |
| 证书配置 | 有 | 无, 改为 token |
| 打开归档目录 | 有 | 浏览器下载目录 |
| 版本显示 | 有 | 有 |
