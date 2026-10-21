# 迁移, 里程碑与开放问题

本文档记录旧实现到新实现的接口映射, 数据迁移, 行为变化, 修复的问题, 以及后续实施阶段.

## 1. 迁移范围

保留:

- ECNU 电量查询与目录查询的上游协议.
- 宿舍房间配置的三个字段: `room_no`, `elcarea`, `elcbuis`.
- 电量样本, 归档, 会话, 房间名称解析等核心概念.
- 历史数据文件的可导入性.
- CLI 与 Web 需要的查询, 归档, 状态能力.
- 系统托盘入口, 用 Go 跨平台托盘库重做.

变化:

- 服务端从 Rust/axum 改为 Go/net/http.
- 客户端从 Tauri 桌面应用改为 Go CLI + 本地 Web 宿主 + 系统托盘.
- 鉴权从 mTLS 改为 Bearer Token.
- 存储从 CSV 文件改为 SQLite.
- API 从根路径改为可配置子路径, 并统一错误模型.
- 房间选择从浏览器注入改为上游目录接口.

不保留:

- Tauri 桌面壳, 窗口隐藏.
- 单实例插件与窗口聚焦.
- 客户端证书导入与 `epu-cert` 工具.
- 系统原生通知.
- 归档下载到本地缓存目录并打开文件管理器.
- `GuardClient` 自动重新登录循环.

## 2. 旧接口到新接口

### 2.1 路由映射

| 旧接口 | 新接口 | 说明 |
|--------|--------|------|
| `GET /get-degree` | `GET /v1/degree` | 返回对象, 支持缓存与 fresh |
| `GET /get-records` | `GET /v1/records` | 支持时间范围, 房间, CSV |
| `POST /post-room` | `POST /v1/rooms` | JSON 字段改 snake_case, 默认激活 |
| `GET /get-room` | `GET /v1/rooms/active` | 无房间返回 409 而非空对象 |
| `POST /clear-room` | `DELETE /v1/rooms/active` | 幂等 |
| `GET /get-room-info` | `GET /v1/rooms/{id}/info` | 错误分类更明确 |
| `POST /post-cookies` | `PUT /v1/session` | 204 无响应体 |
| `POST /clear-cookies` | `DELETE /v1/session` | 204 |
| `POST /create-archive` | `POST /v1/archives` | 请求字段改为 from/to/name |
| `GET /list-archives` | `GET /v1/archives` | 支持房间与软删除过滤 |
| `GET /download-archive?name=` | `GET /v1/archives/{name}` | 名称进入路径 |
| `POST /delete-archive` (form) | `DELETE /v1/archives/{name}` | 名称进入路径 |
| 无 | `GET /v1/health` | 新增存活检查 |
| 无 | `GET /v1/status` | 新增状态汇总 |
| 无 | `GET /v1/whoami` | 新增 token 信息 |
| 无 | `GET /v1/rooms` | 新增多房间列表 |
| 无 | `PUT /v1/rooms/{id}/active` | 新增切换房间 |
| 无 | `GET /v1/ecnu/districts` | 新增目录接口 |
| 无 | `GET /v1/ecnu/buildings` | 新增目录接口 |
| 无 | `GET /v1/ecnu/floors` | 新增目录接口 |
| 无 | `GET /v1/ecnu/rooms` | 新增目录接口 |

### 2.2 路径前缀

旧客户端使用 `server_base.join("/get-degree")`, 一旦 `server_base` 带路径, 前缀会被丢弃. 新约定:

```
base_url = https://example.com/dormwatt/api
GET      https://example.com/dormwatt/api/v1/degree
```

- 所有路由都在 `{base_path}/v1` 下.
- 客户端必须使用保留路径的拼接函数.
- 服务端必须用子路由挂载, 不能假设根路径.

### 2.3 响应包裹

旧实现:

```json
{"Ok": 33.43}
```

```json
{"Err": {"tag": "EcnuNotLogin"}}
```

新实现:

- 成功直接返回资源对象.
- 失败使用 HTTP 状态码 + problem+json.
- 不再有 `Ok` / `Err` 包裹.

### 2.4 错误映射

旧 `CSError` 到新 `code`:

| 旧 tag | 新 code | HTTP |
|--------|---------|------|
| `General` | `internal` | 500 |
| `EcnuNotLogin` | `ecnu_not_logged_in` | 502 |
| `RoomConfigMissing` | `room_not_configured` | 409 |
| `EmptyArchive` | `archive_empty` | 422 |
| `ArchiveDir` | `storage_error` | 500 |
| `RoomDir` | `storage_error` | 500 |
| `ListArchive` | `storage_error` | 500 |
| `SaveArchiveMeta` | `storage_error` | 500 |
| `SerializeMeta` | `internal` | 500 |
| `WriteArchive` | `storage_error` | 500 |
| `SerializeRecords` | `internal` | 500 |
| `DuplicatedArchive` | `archive_exists` | 409 |
| `ReadRecords` | `storage_error` | 500 |
| `QueryDegree` | `ecnu_upstream_error` | 502 |
| `SaveRoomConfig` | `storage_error` | 500 |
| `InvalidArchiveName` | `validation_failed` | 422 |
| `ArchiveNotFound` | `not_found` | 404 |
| `DeletedArchiveFailed` | `storage_error` | 500 |
| `InvalidRoomConfig` | `validation_failed` | 422 |
| `RoomInfoNotFound` | `catalog_incomplete` | 502 |
| `InvalidCookies` | `validation_failed` | 422 |
| `ServerRequestError` | `ecnu_upstream_error` | 502 |

旧 `Error::NoDegree` 映射为 `ecnu_no_degree`, HTTP 502.

### 2.5 字段命名

| 旧类型 | 旧字段 | 新字段 |
|--------|--------|--------|
| Cookies | `j_session_id` | `jsession_id` |
| Cookies | `cookie` | `cookie` |
| Cookies | `x_csrf_token` | `x_csrf_token` |
| RoomConfig | `roomNo` | `room_no` |
| RoomConfig | `elcarea` | `elcarea` |
| RoomConfig | `elcbuis` | `elcbuis` |
| TimeSpan | `start_time` / `end_time` | `from` / `to` |
| ArchiveMeta | `archive_name` | `name` |
| ArchiveMeta | `start_time` / `end_time` | 保留 |
| ArchiveMeta | `records_num` | 保留 |
| ArchiveMeta | 无 | 新增 `room_id`, `created_at` |
| Area | `areaId` / `areaName` | `area_id` / `area_name` |
| District | `districtId` / `districtName` | `district_id` / `district_name` |
| Building | `buiId` / `buiName` | `building_id` / `building_name` |
| Floor | `floorId` / `floorName` | `floor_id` / `floor_name` |
| Room | `roomId` / `roomName` | `room_id` / `room_name` |
| Districts 集合 | `buils` | `buildings` |

### 2.6 记录与时间格式

旧:

```json
{
  "Ok": [
    ["2026-01-24T14:35:32+08:00", 33.43],
    ["2026-01-24T17:06:32+08:00", 33.96]
  ]
}
```

新:

```json
{
  "room_id": 1,
  "room_no": "4408_MH_83_257",
  "from": null,
  "to": null,
  "records": [
    {
      "time": "2026-01-24T14:35:32+08:00",
      "degree": 33.43
    }
  ]
}
```

CSV 列保持不变: `time,degree`, 无表头, RFC3339.

## 3. 旧数据导入

见 [04-data-model.md](04-data-model.md) 第 9 节. 命令:

```shell
dormwatt server import-legacy \
  --data-dir ~/.local/share/ecnu-power-usage \
  --config-dir ~/.config/ecnu-power-usage
```

导入内容:

- `room.toml` -> `rooms` 与活动房间.
- `rooms/*/records.csv` -> `samples`.
- `rooms/*/archives/*.csv` + `*.toml` -> `archives` 与 `archive_samples`.
- `rooms/*/deleted/*` -> 软删除归档.

不导入:

- `logs/`.
- `server.toml` 的 TLS 配置.
- 旧客户端 `gui-config.toml` 的证书字段.
- 旧 `archive-cache/`.

## 4. 行为变化

| 主题 | 旧行为 | 新行为 | 原因 |
|------|--------|--------|------|
| 鉴权 | mTLS 双向证书 | Bearer token + scope | 部署简单, 可撤销 |
| 默认监听 | `0.0.0.0:20531` | `127.0.0.1:20531` | 默认安全, 由代理对外 |
| API 前缀 | 根路径 | 可配置 `{base_path}/v1` | 支持子路径部署 |
| 错误表达 | 大量 200 + Err | 正确 HTTP 状态 + problem+json | 可观测与可编程 |
| 采集周期 | 固定 10 秒 | 默认 300 秒, 可配置 | 降低学校接口压力 |
| 失败处理 | 无退避, 10 秒重试 | 指数退避 + 抖动 | 避免雪崩 |
| 会话保存 | 仅内存 | 默认 SQLite, 可关闭 | 重启后少一次登录 |
| 自动续期 | GuardClient 自动开浏览器 | 不自动, 用户手动登录 | SSO 必须人工交互 |
| 选房 | 浏览器注入点击 | 目录接口 + 级联选择 | 适配 Web, 更稳定 |
| 存储 | 样本 CSV, 归档元数据/房间/服务端配置 TOML | 样本/归档/元数据/房间 SQLite, 配置 TOML | 事务, 查询, 多房间 |
| 房间模型 | 单房间目录 | 多房间表 + 活动房间 | 切房间不丢历史 |
| 归档命名 | `%Y%d%m` 顺序错误 | `YYYYMMDD-YYYYMMDD` | 修复缺陷 |
| 归档重名 | 检测失效, 静默覆盖 | UNIQUE + 409 | 修复缺陷 |
| 归档原子性 | 先清空再重写 | 单事务 | 修复丢数据风险 |
| 通知 | 系统通知 | Web Notification + 托盘状态 | 去掉桌面壳, 保留常驻提示 |
| 托盘 | Tauri 托盘 | Go 跨平台托盘, 三平台适配 | 保留常驻入口 |
| 本地缓存归档 | 有 | 无, 浏览器下载 | 去掉桌面壳 |
| 单实例 | Tauri 插件 | 端口与 runtime 文件 | 无桌面框架 |
| 配置迁移 | 无版本号 | `version` + 迁移模块 | 升级可控 |
| 请求体上限 | 10 MiB | 64 KiB | 接口不需要大 body |

## 5. 修复的旧缺陷

1. 归档重名检测失效: 旧代码把文件名与完整路径比较, 永远不相等, 同名归档会被静默覆盖. 新实现使用数据库唯一约束并返回 409.
2. 默认归档名 `%Y%d%m`: 年月日顺序错误. 新实现固定 `YYYYMMDD`.
3. 归档非原子: 旧实现先 `set_len(0)` 再重写, 崩溃会丢样本. 新实现使用 SQLite 事务.
4. 采集持有读锁调用上游: 旧实现会阻塞房间切换与会话更新. 新实现先复制状态再释放锁.
5. 上游请求无超时: 新实现设置连接, 握手, 总超时与响应体上限.
6. CSV 末行损坏导致启动失败: 新实现从数据库读取最后样本.
7. 地区取 `areas.pop()`: 新实现按 `area_id` 匹配.
8. 房间信息缓存不随会话失效: 新实现缓存键包含 `session_version`.
9. 业务错误返回 200: 新实现使用规范状态码.
10. 采集固定周期无退避: 新实现指数退避与随机抖动.
11. 明文模式无认证: 新实现默认拒绝非回环明文.
12. 日志可能泄露凭据: 新实现统一脱敏与请求日志策略.

## 6. 实施阶段

### P0 文档与骨架

- 设计文档.
- `go.mod`, 目录骨架, `justfile`, `.gitignore`, README 草稿.
- CI 草案.

验收:

- 文档评审通过.
- `go build ./...` 可运行空骨架.

### P1 服务端核心

- 配置加载, 目录解析, 日志.
- SQLite 打开, 迁移, token 表, 房间表, 样本表, 会话表, 设置表.
- Bearer 鉴权与 scope 中间件.
- ECNU 客户端, 采集循环, 状态机.
- API: health, whoami, status, degree, records, rooms, session.
- 子路径路由与规范化.
- 单元与集成测试.

验收:

- `base_path=/dormwatt/api` 下全部接口通过.
- 401 / 403 / 409 / 422 / 502 行为符合文档.
- 采集样本只在变化时写入.
- token 撤销立即生效.

### P2 服务端归档与目录

- 归档表与事务切分.
- 归档列表, 创建, 下载, 软删除.
- 目录接口与缓存.
- 房间名称解析.
- 备份, 检查, 清理命令.
- 旧数据导入.

验收:

- 归档创建失败不会丢样本.
- 重名归档返回 409.
- 归档 CSV 可被旧脚本解析.
- 旧数据导入后样本数与归档数一致.

### P3 客户端 CLI

- 配置命令.
- 查询命令与输出格式.
- 房间选择与房间信息.
- 手动登录与浏览器辅助登录.
- 归档命令.
- doctor 与退出码.
- 集成测试: 假服务端 + CLI.

验收:

- 无守护进程时全部 CLI 命令可用.
- `login --manual` 与 `login` 都能更新服务端会话.
- `doctor` 能区分认证, 网络, TLS, 房间, 会话问题.

### P4 本地宿主与 Web 骨架

- `client serve`.
- 本地 token 与 Cookie.
- Host / Origin / 自定义头校验.
- 本地 API 透传.
- 设置读写.
- 静态资源 embed 与 SPA fallback.
- 基础页面: 状态, 当前电量, 记录列表.
- 托盘骨架: 菜单, 打开页面, 退出, 状态映射.
- 三平台构建脚本与 cgo / notray 变体.

验收:

- 浏览器只能通过回环访问.
- 跨源页面无法调用本地 API.
- 设置页可保存服务端地址与 token, 且不回显 token.
- 子路径服务端在 Web 中可正常查询.
- Windows, macOS, Linux 各出现托盘图标, 菜单可打开页面与退出.
- 无桌面环境使用 `--no-tray` 正常运行.

### P5 Web 完整界面

- 图表与统计.
- 框选统计与归档.
- 归档管理.
- 房间级联选择.
- 登录帮助与手动会话表单.
- 低电量与回升通知.
- 响应式布局.

验收:

- 主要流程与旧 Tauri 客户端等价, 除原生通知.
- 归档创建, 下载, 删除可用.
- 通知去重与阈值行为符合文档.
- 托盘图标与菜单状态随健康状态变化.

### P6 发布

- 各平台原生构建与版本注入.
- GitHub Actions: 测试, 前端构建, 三平台托盘冒烟, 二进制发布.
- 构建变体: 默认, `notray`, Windows GUI 托盘版.
- macOS `.app` 包装与 `LSUIElement`.
- Docker 镜像使用 `notray`.
- systemd 与 launchd 示例.
- README 与升级说明.
- 迁移工具实测.

验收:

- 干净机器按 README 可完成部署.
- 升级与回滚步骤验证.
- 反向代理子路径实测.
- 三平台安装包可启动, 托盘可打开 Web 并退出.

### P7 可选

- SSE 推送.
- 原生常驻通知.
- Prometheus 指标.
- 多用户与共享归档.
- 移动端适配增强.

## 7. 开放问题

以下问题需要确认后再进入实现:

1. 模块路径与仓库名.
   - 当前假设: `github.com/azazo1/ecnu-dorm-watt`, 二进制名 `dormwatt`.
   - 是否需要改为 `dormwatt` 或其他名字.
2. Web 技术栈.
   - 当前假设: Vue 3 + TypeScript + Vite + ECharts.
   - 是否沿用旧前端组件, 还是重写为更轻的页面.
3. 通知能力.
   - 当前设计: 页面打开时使用 Web Notification, 页面关闭后不通知; 托盘图标仍能反映低电量与错误.
   - 是否必须保留关闭页面后的系统通知. 如果需要, 要增加原生通知子命令或常驻服务.
4. 多房间.
   - 当前设计保留多房间历史, 同时只有一个活动房间.
   - 如果只需要单房间, 可以简化表与接口, 但迁移旧数据时仍建议保留多房间结构.
5. 旧数据导入.
   - 是否在首个版本就必须支持, 还是后续补.
6. 采集周期.
   - 当前默认 300 秒, 旧版 10 秒.
   - 是否需要保持 10 秒以获得更细曲线, 代价是学校接口压力更高.
7. Token 存储.
   - 当前设计: 数据库 token 为主, 配置文件静态 token 为辅.
   - 是否只需要配置文件 token, 以简化实现.
8. 服务端是否直接托管 Web.
   - 当前设计: Web 只由本地客户端托管, 浏览器不直连服务端.
   - 如果希望 `https://example.com/dormwatt/` 直接打开 Web, 需要额外的浏览器鉴权方案, 与当前 token 模型冲突.
9. `/v1` 版本段.
   - 当前使用 `{base_path}/v1/...`.
   - 是否希望直接 `{base_path}/...` 以缩短路径.
10. CLI `open` 是否自动拉起 `serve`.
    - 当前设计: 不自动拉起, 明确提示.
11. 构建与产物形态.
    - 当前设计: 默认桌面版含托盘, 服务端使用 `notray` 静态变体, Windows 额外提供 GUI 子系统托盘版.
    - 是否接受为此引入 cgo 构建与三平台原生 CI.
12. 是否需要兼容旧客户端继续访问新服务端.
    - 当前设计: 不兼容, 只提供数据导入与映射文档.

## 8. 验收总则

- 所有 API 在 `base_path=/dormwatt/api` 下通过集成测试.
- 所有写操作有权限校验, 审计日志与明确错误码.
- 凭据不进入日志, 不进入浏览器存储, 不出现在 URL.
- 归档操作具备事务性, 失败不丢数据.
- 子路径拼接有专门测试, 防止回归.
- 旧数据导入可重复验证, 统计一致.
- 三平台托盘可显示, 可打开 Web, 可退出, 无残留进程.
- 部署示例中的命令与实际 CLI 行为一致.
