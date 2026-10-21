# 数据模型与配置

本文档定义 DormWatt 的配置文件, 目录结构, SQLite 模式, 数据格式与版本迁移. 设计目标是让归档操作可事务化, 让多房间历史不丢失, 让升级路径明确.

## 1. 配置版本

- 所有配置文件首行必须包含 `version = 1`.
- 读取时:
  - 缺少 `version` 视为 0, 走迁移.
  - `version` 小于当前值, 走 `internal/config/migrate.go` 的逐版本迁移.
  - `version` 大于当前值, 拒绝启动并提示升级二进制.
- 迁移前把原文件备份为 `<name>.bak-v<old>`, 权限 0600.
- 数据库使用 `PRAGMA user_version`, 规则相同.
- 配置迁移与数据库迁移必须是独立模块, 不与业务逻辑混写.

## 2. 目录结构

| 内容 | 路径 |
|------|------|
| 服务端配置 | `<config>/server.toml` |
| 客户端配置 | `<config>/client.toml` |
| 数据库 | `<data>/dormwatt.db` |
| 数据库 WAL | `<data>/dormwatt.db-wal` |
| 单实例锁 | `<data>/server.lock` |
| 客户端运行状态 | `<data>/client/runtime.json` |
| 服务端日志 | `<data>/logs/server-YYYYMMDD.log` |
| 客户端日志 | `<data>/logs/client-YYYYMMDD.log` |
| 备份 | `<data>/backups/` |

权限:

- 配置目录与数据目录 0700.
- 配置文件, 数据库, WAL, 运行状态文件 0600.
- 日志 0600.
- Windows 上尽力设置, 并依赖用户目录 ACL.

### 2.1 存储映射

旧服务端的持久化实现使用 CSV 与 TOML, 没有 JSON 数据文件. 样本与归档内容写入 CSV; 归档元数据, 房间配置与服务端配置写入 TOML. 新实现将样本, 归档, 归档元数据与房间配置迁入 SQLite; 服务端与客户端配置继续使用 TOML.

| 旧文件或数据 | 内容 | 新位置 |
|--------------|------|--------|
| `records.csv` | 当前房间样本 | SQLite `samples` |
| `rooms/<room_no>/records.csv` | 按房间保存的样本 | SQLite `samples` + `room_id` |
| `archives/<name>.csv` | 归档样本 | SQLite `archive_samples` |
| `archives/<name>.toml` | 归档元数据 | SQLite `archives` |
| `deleted/*` | 软删除归档 | SQLite `archives.deleted_at` |
| `room.toml` | 当前房间配置 | SQLite `rooms` + `settings.active_room_id` |
| 进程内 cookies | ECNU 会话 | SQLite `session`, 可配置为仅内存 |
| `server.toml` | 服务端配置 | 保留 TOML, 增加 `version` |
| `gui-config.toml` | 客户端配置 | 保留 TOML, 改名为 `client.toml` |
| `archive-cache/*.csv` | 客户端下载缓存 | 不再缓存, 由浏览器下载 |
| `logs/` | 日志 | 保留文件, 按天轮转 |
| 无, 新增 | 服务端 token | SQLite `tokens` |
| 无, 新增 | 本地守护进程 IPC 状态 | `runtime.json`, 0600, 退出即删 |

文件形式保留范围:

- 配置文件要求人可读, 可版本控制, 可手工编辑, 不进入数据库.
- 日志为追加写, 不参与事务.
- `runtime.json` 为短生命周期进程间状态, 仅包含端口与本地 token, 不属于业务数据.

### 2.2 SQLite 承接范围

- 样本与归档: 范围查询, 事务切分, 软删除与统计.
- 房间: 唯一约束与活动房间切换.
- token: 即时撤销与最后使用时间.
- 会话: 跨重启保留, 与样本数据同权限保护.

收益:

- 归档操作原子化, 失败不丢样本.
- 归档重名由唯一约束兜底.
- 多房间历史与目录结构解耦.
- 时间范围查询与统计由索引支撑.

## 3. 服务端配置

```toml
version = 1

[server]
bind = "127.0.0.1:20531"
base_path = "/dormwatt/api"
strict_path = true
allow_insecure_http = false
max_body_bytes = 65536
read_header_timeout = "10s"
read_timeout = "30s"
write_timeout = "60s"
idle_timeout = "120s"

[server.tls]
# 留空表示由反向代理终止 TLS.
cert = ""
key = ""

[auth]
trusted_proxies = ["127.0.0.1/32", "::1/128"]
failed_auth_per_minute = 20
requests_per_minute = 300
upstream_requests_per_minute = 30

# 可选. 数据库 token 之外额外接受的静态 token.
# [[auth.static_tokens]]
# name = "monitoring"
# scopes = ["read"]
# sha256 = "0123abcd..."

[ecnu]
poll_interval = "300s"
request_timeout = "15s"
degree_change_threshold = 0.01
live_query_cache = "15s"
catalog_cache = "10m"
persist_session = true
user_agent = "dormwatt/0.1 (+https://github.com/azazo1/ecnu-dorm-watt)"

[storage]
# 留空使用数据目录下的 dormwatt.db.
path = ""
backup_dir = ""

[ui]
timezone = "Asia/Shanghai"

[log]
level = "info"
file = true
max_size_mb = 50
max_backups = 14
```

字段说明:

- `bind`: 默认只监听回环, 由反向代理对外.
- `base_path`: 公网挂载前缀, 规范化后不含尾部斜杠.
- `strict_path`: true 时, `base_path` 之外的请求统一 404.
- `allow_insecure_http`: 非回环明文监听开关, 默认 false.
- `poll_interval`: 采集周期, 默认 300 秒, 允许范围 10 秒到 1 小时.
- `degree_change_threshold`: 变化超过该值才记录新样本, 默认 0.01 度.
- `live_query_cache`: `/degree` 与采集共用的上游结果缓存.
- `persist_session`: false 时会话只保存在内存.
- `trusted_proxies`: 只有这些来源的转发头被采信.
- `max_body_bytes`: 默认 64 KiB.

旧配置迁移:

```toml
# 旧 server.toml
bind = "0.0.0.0:20531"
[tls]
server_cert = "..."
server_key = "..."
root_ca = "..."
```

迁移为:

- `bind` 移入 `[server]`.
- `[tls]` 丢弃并记录警告, 因为 mTLS 不再支持.
- 如果希望继续直接 HTTPS, 需要手工填写 `[server.tls] cert/key`.
- 生成 `auth` 默认值, 并要求执行 `dormwatt server token create` 创建首个 token.

## 4. 客户端配置

```toml
version = 1

[server]
base_url = "https://example.com/dormwatt/api"
token = "dw_..."
request_timeout = "20s"

[local]
bind = "127.0.0.1:20532"
allow_remote = false
open_browser = true
# auto: 有桌面会话时启用托盘; on: 强制启用; off: 关闭.
tray = "auto"
# 是否发送系统通知.
notify = true

[monitor]
poll_interval = "10s"
degree_threshold = 10.0

[ui]
timezone = "Asia/Shanghai"
refresh_interval = "5s"

[log]
level = "info"
file = true
```

说明:

- `base_url` 可以包含路径, 例如 `/dormwatt/api`, 但不要带结尾斜杠.
- `token` 为服务端 Bearer token, 文件权限 0600.
- `local.bind` 默认回环, 非回环需要 `allow_remote=true` 且所有本地 API 请求带 token.
- `local.tray` 控制托盘: `auto` 在检测到桌面会话时启用, `on` 强制启用, `off` 关闭. 命令行 `--tray` / `--no-tray` 优先于配置.
- `local.notify` 控制系统通知: 状态异常, 低电量与电量回升时由守护进程调用系统通知中心. 无桌面会话时自动跳过.
- `monitor.poll_interval` 是本地守护进程刷新上游状态与低电量判断的周期.
- `ui.refresh_interval` 是 Web 页面轮询本地 API 的周期.
- `ui.timezone` 只影响展示与 CSV 导出.

旧客户端配置迁移:

```toml
# 旧 gui-config.toml
serverBase = "..."
clientCert = "..."
clientKey = "..."
rootCA = "..."
useSelfSignedTls = true
degreeThreshold = 10.0
```

迁移为:

- `serverBase` -> `server.base_url`.
- 删除三个证书字段与 `useSelfSignedTls`.
- `degreeThreshold` -> `monitor.degree_threshold`.
- `server.token` 留空, 由用户执行 `dormwatt client config set-token` 填写.
- `local.tray` 默认 `auto`, 桌面环境保留托盘, 无桌面环境自动关闭.
- 迁移时输出提示: 旧证书不再使用.

## 5. SQLite 模式

### 5.1 连接参数

```
file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)
```

补充:

- 连接池最大 1 个写连接, 允许多个读连接.
- 写事务使用 `BEGIN IMMEDIATE`.
- 启动时执行 `PRAGMA quick_check`, 失败则拒绝启动并提示从备份恢复.
- 关闭前执行 `PRAGMA wal_checkpoint(TRUNCATE)`.

### 5.2 表结构 v1

```sql
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE rooms (
  id         INTEGER PRIMARY KEY,
  room_no    TEXT NOT NULL,
  elcarea    INTEGER NOT NULL CHECK (elcarea >= 0),
  elcbuis    TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (room_no, elcarea, elcbuis)
);

CREATE TABLE samples (
  id      INTEGER PRIMARY KEY,
  room_id INTEGER NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  ts      INTEGER NOT NULL,
  degree  REAL NOT NULL CHECK (degree >= 0)
);

CREATE INDEX idx_samples_room_ts ON samples(room_id, ts);
CREATE UNIQUE INDEX idx_samples_room_ts_degree ON samples(room_id, ts, degree);

CREATE TABLE archives (
  id          INTEGER PRIMARY KEY,
  room_id     INTEGER NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  start_ts    INTEGER NOT NULL,
  end_ts      INTEGER NOT NULL,
  records_num INTEGER NOT NULL CHECK (records_num > 0),
  created_at  INTEGER NOT NULL,
  deleted_at  INTEGER,
  UNIQUE (room_id, name)
);

CREATE INDEX idx_archives_room_created ON archives(room_id, created_at);
CREATE INDEX idx_archives_room_start ON archives(room_id, start_ts);

CREATE TABLE archive_samples (
  id         INTEGER PRIMARY KEY,
  archive_id INTEGER NOT NULL REFERENCES archives(id) ON DELETE CASCADE,
  ts         INTEGER NOT NULL,
  degree     REAL NOT NULL
);

CREATE INDEX idx_archive_samples_archive_ts ON archive_samples(archive_id, ts);

CREATE TABLE tokens (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  token_prefix TEXT NOT NULL,
  token_sha256 TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at   INTEGER
);

CREATE TABLE session (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  jsession_id     TEXT NOT NULL,
  cookie          TEXT NOT NULL,
  csrf_token      TEXT NOT NULL,
  updated_at      INTEGER NOT NULL,
  last_checked_at INTEGER,
  state           TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

### 5.3 时间与数值

- 所有 `ts`, `created_at`, `updated_at`, `deleted_at` 为 UTC Unix 毫秒.
- `degree` 为 REAL, 入库前四舍五入到两位小数.
- 时区只在输出时应用, 不写入数据库.
- 活动房间存于 `settings`:

```sql
INSERT INTO settings(key, value) VALUES ('active_room_id', '1');
```

- 没有活动房间时删除该 key, 而不是写空值.
- 会话没有记录时表示 `missing`.

### 5.4 索引与查询

- 记录范围查询: `WHERE room_id = ? AND ts BETWEEN ? AND ? ORDER BY ts`.
- 最近一条样本: `ORDER BY ts DESC LIMIT 1`.
- 房间统计: `COUNT(*)`, `MIN(ts)`, `MAX(ts)` 按 `room_id` 分组.
- 归档列表: `WHERE room_id = ? AND deleted_at IS NULL ORDER BY start_ts, name`.
- 归档下载: `WHERE archive_id = ? ORDER BY ts`, 流式扫描.
- 全部都在索引覆盖范围内, 十万级数据不需要额外优化.

## 6. 数据库迁移

`internal/store/migrate.go` 维护有序迁移:

```go
type migration struct {
    version int
    name    string
    apply   func(ctx context.Context, tx *sql.Tx) error
}
```

规则:

1. 读取 `PRAGMA user_version`.
2. 如果大于当前程序支持的版本, 拒绝启动.
3. 逐个执行未应用的迁移, 每个迁移在独立事务内.
4. 每个迁移成功后更新 `user_version`.
5. 迁移前用 `VACUUM INTO` 备份到 `backups/dormwatt-pre-v<from>-<timestamp>.db`.
6. 任一迁移失败则回滚并停止启动.

v1 初始化直接创建上述表, 并设置 `user_version = 1`.

## 7. 数据格式

### 7.1 JSON

- 时间: RFC3339, 带偏移, 有毫秒时保留最多三位, 例如 `2026-01-24T14:35:32.123+08:00`, 否则 `2026-01-24T14:35:32+08:00`.
- 电量: JSON 数字, 两位小数, 例如 `33.43`.
- 空值: 明确使用 `null`, 不使用空字符串代替.
- 字段名 snake_case.

### 7.2 CSV

导出与归档下载统一格式:

```csv
2026-01-24T14:35:32+08:00,33.43
2026-01-24T17:06:32+08:00,33.96
```

- 默认无表头.
- `header=true` 时首行为 `time,degree`.
- 编码 UTF-8, 行尾 `\n`.
- 电量固定两位小数.
- 时间按 `ui.timezone` 输出, 带偏移.
- 字段中不会出现逗号, 因此不需要引号转义. 导入时仍使用标准 CSV 解析器.

### 7.3 旧 CSV 兼容

旧格式为 `RFC3339,f32`, 无表头, 电量可能写成 `10` 而不是 `10.00`. 新导入器:

- 接受整数, 小数, 科学计数法.
- 接受带毫秒与不带毫秒的时间.
- 跳过空行, 但遇到非法行时报告行号并中止导入, 不静默跳过.
- 导入时按房间目录归入对应房间.

## 8. 备份与恢复

- `dormwatt server backup --out <path>` 使用 `VACUUM INTO` 生成一致性快照.
- 默认备份目录 `<data>/backups/`, 文件名为 `dormwatt-YYYYMMDD-HHMMSS.db`.
- 恢复流程:
  1. 停止服务端.
  2. 备份当前数据库.
  3. 用备份文件替换 `dormwatt.db`, 删除 `-wal` 与 `-shm`.
  4. 启动, 执行 `quick_check`.
- 提供 `dormwatt server check` 执行 `PRAGMA integrity_check` 并输出统计.

## 9. 旧数据导入

旧数据目录结构:

```
<data>/ecnu-power-usage/
  records.csv
  room.toml
  rooms/
    <room_no>/
      records.csv
      archives/
        <name>.csv
        <name>.toml
      deleted/
```

导入命令:

```shell
dormwatt server import-legacy \
  --data-dir ~/.local/share/ecnu-power-usage \
  --config-dir ~/.config/ecnu-power-usage
```

规则:

- 读取 `room.toml` 得到当前活动房间, 写入 `rooms` 与 `settings`.
- 遍历 `rooms/*/records.csv`, 按目录名还原房间. 缺少 `room.toml` 时无法还原 `elcarea` 与 `elcbuis`, 跳过并警告.
- 读取 `archives/*.toml` 与对应 `.csv`, 插入 `archives` 与 `archive_samples`.
- 读取 `deleted/` 下的文件, 以软删除状态插入, 保留原名与删除时间.
- 导入在单个事务中完成, 失败整体回滚.
- 导入前要求目标数据库为空, 避免重复导入.
- 导入完成后输出统计: 房间数, 样本数, 归档数, 跳过数.

已知旧数据问题:

- 默认归档名使用 `%Y%d%m`, 年月日顺序错误. 导入时按原名称保留, 不自动改名.
- 旧归档重名检测有缺陷, 可能已有同名覆盖丢失, 导入器无法恢复.
- 旧 `records.csv` 末行可能无换行, 新导入器按行读取, 不依赖末尾换行.
- 旧记录的时区偏移保留为原始值, 导入后统一转为 UTC 毫秒.

## 10. 完整性约束与校验

写入前校验:

- `room_no`: 非空, 长度 <= 128, 不含 `/`, `\`, 控制字符, 不是 `.` 或 `..`.
- `elcarea`: >= 0.
- `elcbuis`: 非空, 长度 <= 128, 不含控制字符.
- `degree`: 有限数, >= 0, <= 1000000.
- 时间: 在 2000-01-01 到当前时间加一天之间.
- 归档名称: 见 API 文档.

数据库层再用 CHECK 与 UNIQUE 兜底.

## 11. 保留策略

- 样本默认永久保留, 由用户归档控制.
- 软删除归档默认永久保留.
- `dormwatt server purge --deleted-before <RFC3339>` 物理删除过期软删除归档.
- `dormwatt server purge --orphan-samples` 可选, 默认不提供, 避免误删.
- 日志按 `max_backups` 轮转删除.
- 备份不自动清理, 由用户或外部脚本管理.

## 12. 数据量与性能

- 宿舍电量变化频率低, 一年样本量通常在数千级.
- 单次 `/records` 返回 20 万条上限, 足够覆盖多年数据.
- SQLite 单文件便于备份与迁移.
- 归档下载使用流式读取, 内存占用与归档大小无关.
- 如果未来样本量级达到千万, 再考虑按月分表或外部时序库, 不在 V1 范围.
