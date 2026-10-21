# 服务端运行时设计

本文档描述服务端进程的内部行为: 启动顺序, ECNU 上游协议, 采集状态机, 归档事务, 锁与并发, 日志与关闭. API 契约见 [02-server-api.md](02-server-api.md).

## 1. 启动顺序

```
解析 CLI 参数与配置
  -> 初始化日志
  -> 获取数据目录单实例锁
  -> 打开 SQLite, 执行迁移与 quick_check
  -> 加载活动房间与会话
  -> 构造 ECNU 客户端与缓存
  -> 启动采集 goroutine
  -> 启动 HTTP 服务
  -> 等待 SIGINT / SIGTERM
  -> 优雅关闭
```

失败处理:

- 配置校验失败: 打印字段路径与原因, 退出码 2.
- 数据目录不可写: 退出码 1, 提示检查权限.
- 单实例锁被占用: 退出码 1, 输出持锁进程 PID.
- 数据库版本高于程序: 退出码 1, 提示升级.
- 数据库损坏: 退出码 1, 提示 `dormwatt server check` 与备份路径.

## 2. ECNU 上游协议

### 2.1 电费查询

```
POST https://epay.ecnu.edu.cn/epaycas/electric/queryelectricbill
Content-Type: application/x-www-form-urlencoded
Cookie: JSESSIONID=<jsession_id>; cookie=<cookie>
X-CSRF-TOKEN: <x_csrf_token>

sysid=1
roomNo=<room_no>
elcarea=<elcarea>
elcbuis=<elcbuis>
```

响应:

```json
{
  "retcode": 0,
  "retmsg": "成功",
  "restElecDegree": 33.43
}
```

成功判定:

- HTTP 2xx.
- `Content-Type` 包含 `application/json`.
- `retcode == 0`.
- `retmsg == "成功"`.
- `restElecDegree` 存在且为有限非负数.

失败分类:

| 现象 | 分类 | 对外 code |
|------|------|-----------|
| 302 到 `sso.ecnu.edu.cn` | 会话失效 | `ecnu_not_logged_in` |
| 返回 HTML 且 Content-Type 非 JSON | 会话失效或权限拒绝 | `ecnu_not_logged_in` |
| 连接超时, DNS 失败, TLS 失败 | 网络错误 | `ecnu_upstream_error` |
| 非 2xx | 上游错误 | `ecnu_upstream_error` |
| JSON 解析失败 | 协议变化 | `ecnu_upstream_error` |
| `retcode != 0` | 业务错误 | `ecnu_upstream_error`, detail 保留 retmsg |
| 缺少 `restElecDegree` | 协议变化 | `ecnu_no_degree` |

### 2.2 目录查询

| 用途 | URL | 表单字段 |
|------|-----|----------|
| 地区 | `/epaycas/electric/queryelectricarea` | `sysid=1` |
| 建筑 | `/epaycas/electric/queryelectricbuis` | `sysid=1&area=<id>&district=<id>` |
| 楼层 | `/epaycas/electric/queryelectricfloors` | `sysid=1&area=<id>&district=<id>&build=<id>` |
| 房间 | `/epaycas/electric/queryelectricrooms` | `sysid=1&area=<id>&district=<id>&build=<id>&floor=<id>` |

响应字段:

- 地区: `areas`, `districts`, `buils`, `floors`, `rooms`.
- 建筑: `buils`.
- 楼层: `floors`.
- 房间: `rooms`.

内部结构使用 `building_id` / `building_name`, 只在解析上游 JSON 时映射 `buiId` / `buiName`.

### 2.3 HTTP 客户端约束

- 总超时 `ecnu.request_timeout`, 默认 15 秒.
- 连接超时 5 秒, TLS 握手超时 5 秒.
- 响应体上限 1 MiB, 防止异常页面撑爆内存.
- 不自动重试. 由采集循环的退避负责.
- 重定向策略: 不自动跟随到非 `epay.ecnu.edu.cn` 主机. 遇到 SSO 跳转直接归类为会话失效.
- 固定 `User-Agent`, 默认 `dormwatt/0.1`.
- 不复用浏览器 cookie jar, 每次请求显式附带三个凭据.
- 所有请求带 `context.Context`, 可被关闭信号取消.

## 3. 采集状态机

状态:

| 状态 | 含义 |
|------|------|
| `idle` | 没有活动房间 |
| `no_session` | 有房间但没有会话 |
| `active` | 正常采集 |
| `degraded` | 连续失败, 等待退避 |

转移:

```
idle --设置房间--> no_session / active
no_session --上传会话--> active
active --上游要求登录--> no_session
active --网络错误--> degraded
degraded --成功--> active
degraded --会话失效--> no_session
任意 --清除房间--> idle
```

循环:

```
delay = initial jitter (0 到 2 秒)
for {
    wait(delay)
    snapshot = 读取活动房间与会话 (持锁时间只覆盖复制)
    if 无房间: 状态 idle, delay = poll_interval, continue
    if 无会话: 状态 no_session, delay = poll_interval, continue
    degree, err = query(snapshot)
    if err:
       状态 = degraded 或 no_session
       记录 last_error, last_attempt
       delay = backoff(consecutive_failures)
       continue
    成功:
       状态 = active
       重置失败计数
       if 与上一条样本度数差 >= threshold:
           INSERT sample
       delay = poll_interval * jitter(0.9 到 1.1)
}
```

细节:

- 默认 `poll_interval = 300s`. 旧实现为 10 秒, 对学校接口压力过大, 新默认放慢, 用户可配置到 10 秒.
- 首次查询延迟 0 到 2 秒随机, 避免多实例同时启动打上游.
- 不使用会累积的 ticker. 上一轮耗时不会导致下一轮立刻触发.
- 失败退避: 从 `poll_interval` 开始翻倍, 上限 30 分钟. 成功或会话更新后重置.
- 会话失效时不做无意义重试, 每 15 分钟最多探测一次, 直到会话更新.
- 房间切换或会话上传通过 channel 唤醒循环, 立即执行一次.
- 退避期间 `next_poll_at` 反映真实计划时间.

### 3.1 记录判定

- 读取该房间最后一条样本的度数.
- 如果 `abs(last - new) < degree_change_threshold`, 不记录.
- 否则插入新样本, 时间取当前 UTC 时间并截断到秒.
- 启动时从数据库加载最后一条样本, 不依赖文件末行.
- 并发插入由唯一索引 `(room_id, ts, degree)` 兜底, 重复冲突视为已记录.
- 服务端不记录"电量增加"事件, 这是客户端或 Web 端的展示逻辑.

### 3.2 状态字段

`/v1/status` 的 `monitor` 字段:

- `last_attempt_at`: 最近一次上游请求开始时间.
- `last_success_at`: 最近一次成功时间.
- `last_degree`: 最近一次成功度数.
- `last_error`: 最近一次错误 code 与安全文案.
- `consecutive_failures`: 连续失败次数.
- `next_poll_at`: 下一次计划时间.
- `state`: 状态机当前状态.

状态只保存在内存, 重启后从数据库可恢复部分字段 (最后样本, 会话).

## 4. 锁与并发

旧实现的问题:

- `get-degree` 在发起上游 HTTP 请求期间持有 `querier` 读锁, 阻塞 `post-room` 与 `post-cookies`.
- 客户端没有超时, 上游卡住时整个服务端状态被拖住.

新实现:

- 读取活动房间与会话时只复制值, 立即释放锁.
- 上游请求不持任何存储锁.
- 实时查询使用 singleflight 合并同房间并发请求.
- 查询结果缓存 `live_query_cache`, 默认 15 秒.
- 采集与实时查询共享同一个 singleflight 组, 避免同时打上游.
- 数据库写入只发生在采集 goroutine 与 API handler 的短事务中.
- 归档事务不包含网络 IO.
- `/records` 使用只读查询, 不持有写锁.

## 5. 归档事务

创建流程:

```
BEGIN IMMEDIATE
  校验房间存在
  校验名称唯一 (deleted_at IS NULL 或全部?)
  选出 samples 范围内记录 (FROM room + 时间范围, ORDER BY ts)
  if 空: ROLLBACK, archive_empty
  计算 start_ts, end_ts, records_num
  INSERT archives
  INSERT archive_samples (逐条或批量)
  DELETE samples WHERE id IN (...)
COMMIT
```

- 名称唯一约束为 `(room_id, name)`, 包含已删除归档. 如果允许复用已删除名称, 需要额外判断; V1 不允许复用, 避免歧义.
- 批量插入使用单条 `INSERT ... SELECT`, 避免逐条往返:

```sql
INSERT INTO archive_samples (archive_id, ts, degree)
SELECT ?, ts, degree FROM samples
WHERE room_id = ? AND ts BETWEEN ? AND ?;
```

- 删除同样使用范围条件, 与插入在同一事务.
- `records_num` 来自插入影响行数, 不做事后重算.
- 事务超时 30 秒, 超时回滚.
- 失败时返回 `storage_error`, 不返回半成品.

软删除:

```
BEGIN IMMEDIATE
  UPDATE archives SET deleted_at = ? WHERE room_id = ? AND name = ? AND deleted_at IS NULL
  if 影响行数 == 0: 404
COMMIT
```

物理清理:

```
dormwatt server purge --deleted-before <RFC3339>
  DELETE FROM archives WHERE deleted_at IS NOT NULL AND deleted_at < ?
  archive_samples 由外键级联删除
  VACUUM (可选 --vacuum)
```

默认不自动清理. 清理前输出将删除的归档数量, 需要 `--yes` 确认.

## 6. 房间信息解析

`room_no` 形如 `4408_MH_83_257`, 按 `_` 拆分:

| 段 | 含义 | 用途 |
|----|------|------|
| 0 | 房间名 | 匹配 `rooms` 列表的 `room_name` |
| 1 | 地区 id | 匹配 `districts` |
| 2 | 旧建筑段 | 忽略 |
| 3 | 楼层 id | 匹配 `floors` |

- 必须恰好 4 段, 否则 422 `validation_failed`.
- `area_id` 取 `elcarea`, 并与 `areas` 列表中 `area_id` 相同的项匹配. 旧实现取 `areas.pop()`, 有多项时会错.
- `building_id` 取 `elcbuis`, 匹配 `buils`.
- 四步查询按服务端 API 定义的顺序执行, 任一步缺少对应项返回 `catalog_incomplete`.
- 缓存键: `(room_no, elcarea, elcbuis, session_version)`.
- 会话更新后递增 `session_version`, 使旧缓存失效. 旧实现不失效, 会返回过期名称.

## 7. 日志

组件划分:

- `startup`: 配置, 目录, 数据库, 监听地址.
- `monitor`: 状态转移, 采样, 退避.
- `ecnu`: 上游请求耗时与错误分类, 不含凭据.
- `http`: 请求日志.
- `store`: 迁移, 备份, 清理.
- `auth`: 认证失败与 token 操作, 不含 token 值.

级别:

- info: 启动, 关闭, 状态转移, 记录样本, 归档创建/删除, token 创建/撤销.
- debug: 每次采集 tick, 缓存命中, SQL 耗时.
- warn: 认证失败, 上游异常, 配置过宽权限.
- error: 数据库错误, 无法恢复的启动错误.

采集日志示例:

```json
{
  "level": "INFO",
  "component": "monitor",
  "msg": "sample recorded",
  "room_id": 1,
  "degree": 33.43,
  "delta": -0.12
}
```

## 8. HTTP 服务

- 使用 `net/http` 与 `http.Server`.
- `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` 按配置设置.
- `MaxHeaderBytes` 默认 64 KiB.
- 路由挂载:

```go
api := http.NewServeMux()
api.HandleFunc("GET /v1/health", ...)
// ...
```

- 服务端只注册 `/v1/...`; 外部前缀由反向代理剥离.
- 静态文件不提供服务端页面, 服务端只有 API.
- 不做 CORS.
- panic 由中间件恢复并返回 500 `internal`, 同时记录堆栈.
- 请求体使用 `http.MaxBytesReader`.
- 所有 handler 接收 `context.Context`, 关闭时取消.

## 9. 优雅关闭

收到 SIGINT / SIGTERM 后:

1. 标记状态 `shutting_down`, `/v1/health` 返回 503.
2. 停止接受新连接, 等待进行中的请求, 最多 10 秒.
3. cancel 采集 goroutine, 等待其退出.
4. 关闭数据库连接, WAL checkpoint.
5. 释放单实例锁.
6. 退出码 0.

超时未完成则强制退出, 记录未完成的请求数与 goroutine 堆栈.

## 10. 单实例

- 启动时对 `<data>/server.lock` 加排他锁, 内容写入 PID 与启动时间.
- 锁文件不删除, 仅靠操作系统锁判断.
- 第二个实例启动失败时读取锁文件输出 PID, 提示已有实例.
- `dormwatt server token` 等管理命令不加锁, 通过 SQLite 并发访问工作.
- `dormwatt server purge` 需要加锁, 避免与服务端同时运行.

## 11. 旧实现缺陷与修正对照

| 旧问题 | 位置 | 新设计 |
|--------|------|--------|
| 归档重名检测比较文件名与全路径, 永不命中, 同名覆盖 | route.rs:206 | `(room_id, name)` UNIQUE + 409 |
| 默认归档名 `%Y%d%m` 年月日错位 | route.rs:179-185 | 固定 `YYYYMMDD-YYYYMMDD` |
| 归档先清空 records.csv 再重写, 崩溃丢数据 | mod.rs:423-431 | SQLite 单事务 |
| 上游查询持有读锁, 阻塞房间与会话更新 | mod.rs:535, route.rs:108 | 快照后释放锁 |
| HTTP 客户端无超时 | mod.rs:93-110 | 全链路超时与响应上限 |
| CSV 末行损坏导致启动失败 | mod.rs:342-350 | 数据库约束与 last sample 查询 |
| 地区取 `areas.pop()`, 不按 id 匹配 | mod.rs:264 | 按 id 匹配 |
| 房间信息缓存不随会话更新失效 | mod.rs:77-79 | 缓存键含 session_version |
| 业务错误用 200 返回 | route.rs:104-125 | 统一 problem+json 状态码 |
| 采集固定 10 秒且无退避 | mod.rs:531 | 默认 300 秒, 指数退避, 抖动 |
| 无日志脱敏 | 多处 | 统一中间件与字段过滤 |
| 明文模式无鉴权 | mod.rs:659-663 | 默认拒绝非回环明文 |
