# 测试计划

本文档定义实现阶段的测试范围, 测试环境, 关键用例与验收方式. 测试目标是防住迁移中最容易回归的语义, 不追求表面覆盖率.

## 1. 原则

- 优先测试边界与状态转换, 不写文案测试.
- 涉及时间, 时区, 文件路径, URL 拼接的用例必须显式覆盖.
- 不访问真实 ECNU 接口, 全部使用假上游.
- 不读取真实 cookie, token 或用户数据库.
- 集成测试使用临时目录与临时 SQLite, 测试结束自动清理.
- 托盘与浏览器通知无法稳定自动化, 使用人工冒烟清单.

## 2. 测试分层

| 层 | 范围 | 工具 |
|----|------|------|
| 单元 | 纯函数, 解析, 校验, 状态映射 | `testing` |
| 存储 | SQLite 迁移, 事务, 约束 | `testing` + 临时 DB |
| 服务端集成 | HTTP 路由, 鉴权, API, 子路径 | `httptest` + 假 ECNU |
| 客户端集成 | CLI, 本地 API, 上游客户端 | `httptest` + 临时配置 |
| 前端 | 纯函数, 组件状态 | Vitest |
| 端到端 | 关键流程 | Playwright, 可选 |
| 人工 | 托盘, 通知, 代理部署 | 清单 |

## 3. 单元测试

### 3.1 配置

- `server.toml` 默认值正确.
- 配置版本:
  - 缺少 `version` 按 0 处理.
  - 旧版本迁移后字段正确.
  - 新版本拒绝.
- 环境变量覆盖优先级.
- 客户端 `base_url` 带路径与不带路径均通过校验.

### 3.2 URL 拼接

- `joinBaseURL("https://example.com/dormwatt/api", "/v1/records")` 保留 `/dormwatt/api`.
- base 带尾部斜杠时不产生双斜杠.
- base 为根路径时结果正确.
- 回归用例: 禁止 `ResolveReference` 绝对路径行为.

### 3.3 Token

- 生成格式 `dw_` + base64url, 长度固定.
- 摘要为小写十六进制.
- scope 解析与包含关系.
- `write` 隐含 `read`.
- 无效 scope 拒绝.
- 撤销后校验失败.
- 静态 token 与数据库 token 同时存在.

### 3.4 存储

- 迁移从 0 到 1 建表成功, `user_version=1`.
- 重复迁移幂等.
- 房间三元组唯一.
- 样本唯一索引 `(room_id, ts, degree)`.
- 活动房间设置与清除.
- 会话覆盖写与清除.
- token 最后使用时间更新节流.

### 3.5 归档

- 闭区间边界: 起止样本都包含.
- 空范围返回 `archive_empty`.
- 重名返回 `archive_exists`.
- 事务失败回滚, 样本数量不变.
- 软删除后列表默认不返回, `include_deleted` 返回.
- 清理只删除过期软删除.
- 默认名称格式 `YYYYMMDD-YYYYMMDD`, 冲突追加 `-2`.

### 3.6 ECNU 解析

- 正常响应解析电量.
- `retcode != 0` 映射上游错误.
- `retmsg` 非 `成功` 映射上游错误.
- 缺少 `restElecDegree` 映射 `ecnu_no_degree`.
- HTML 响应映射 `ecnu_not_logged_in`.
- 非 JSON Content-Type 映射 `ecnu_not_logged_in`.
- 目录响应 `buils` 映射 `buildings`, `buiId` 映射 `building_id`.
- 超大响应被限制.

### 3.7 房间信息

- `4408_MH_83_257` 拆分为房名, 地区, 楼层.
- 少于或多于 4 段返回 422.
- 地区按 `area_id` 匹配, 不取最后一个.
- 缓存键随 `session_version` 变化而失效.

### 3.8 客户端

- 配置文件权限过宽时收紧.
- token 写入后 `config show` 不显示原值.
- 退出码映射.
- `--json` 错误输出结构.
- `client login --from-file` 读取三行.
- `client quit` 在无守护进程时返回 7.

### 3.9 本地 API 鉴权

- 无 Cookie 与无 Bearer 返回 401.
- 错误 Host 返回 403.
- 跨源 Origin 返回 403.
- 缺少 `X-Dormwatt-Client` 的写请求返回 403.
- 非 JSON Content-Type 的写请求返回 415.
- `OPTIONS` 返回 405.
- 响应无 CORS 头.
- 正确 Cookie 与 Bearer 均通过.

### 3.10 托盘状态映射

- 健康状态到图标状态, 菜单首项的映射.
- 低电量优先于普通状态.
- 未知状态回退到灰色.
- 该测试只覆盖纯函数, 不启动托盘.

## 4. 服务端集成测试

### 4.1 测试夹具

- 假 ECNU:
  - `httptest.Server` 提供电费与目录接口.
  - 可切换响应: 正常, 业务错误, HTML, 超时, 缺少字段.
  - 记录收到的表单与头, 断言 `sysid`, `roomNo`, `elcarea`, `elcbuis`, `Cookie`, `X-CSRF-TOKEN`.
- 临时 SQLite:
  - 每个测试独立文件, 结束后删除.
- 服务端:
  - 使用真实 router 与中间件, 只替换上游地址.
  - 通过 `httptest.NewServer` 或直接调用 handler.

### 4.2 鉴权矩阵

| 场景 | 期望 |
|------|------|
| 无 Authorization | 401 `unauthorized` |
| 错误 token | 401 |
| 已撤销 token | 401 |
| read token 调 GET | 200 |
| read token 调 PUT session | 403 `forbidden` |
| write token 调 PUT session | 204 |
| 健康检查无 token | 200 |

### 4.3 路径严格性

服务端只接受 `/v1/...`, 断言:

- `/v1/health` 200.
- `/`, `/health`, `/dormwatt/api/v1/health` 404.
- `/v1/health/`, `/v1//health`, `/v1/../v1/health` 404.
- 404 使用 problem+json, 且不返回重定向.
- 响应中不出现绝对 URL.
- 静态路径与 API 路径不冲突.

### 4.4 API 用例

- 状态:
  - 无房间, 无会话, 正常, 上游错误.
- 电量:
  - 缓存命中, `fresh=true`, 无房间, 会话失效, 上游超时.
- 记录:
  - 全部, 时间范围, 房间过滤, limit, CSV, 超过上限.
- 房间:
  - 创建, 重复创建返回 200, 激活, 清除, 不存在 id, 非法字段.
- 会话:
  - 上传, 覆盖, 清除, 状态变化, 不返回凭据.
- 归档:
  - 创建, 列表, 下载, 软删除, 重名, 空范围, 事务回滚, 非活动房间.
- 目录:
  - 地区, 建筑, 楼层, 房间, 缓存命中, 会话失效.

### 4.5 采集循环

- 首次查询延迟在 0 到 2 秒.
- 度数变化小于阈值不写样本.
- 变化超过阈值写一条样本.
- 连续失败进入退避, 成功后退避重置.
- 会话失效进入 `not_logged_in`, 不持续打上游.
- 房间切换触发立即查询.
- 时间戳截断到秒.

## 5. 客户端集成测试

- CLI 对假服务端的全流程:
  - `config set-server`, `config set-token`.
  - `status`, `degree`, `records`, `room`, `archive`, `doctor`.
- `login --manual` 与 `--from-file`:
  - 断言 `PUT /v1/session` 字段.
  - 断言随后调用 `/v1/degree?fresh=true`.
- 归档命令:
  - 创建确认, `--yes`, `--dry-run`.
  - 下载文件名与内容.
  - 删除确认.
- 错误退出码:
  - 401 -> 3.
  - 502 -> 4.
  - 无房间 -> 5.
  - 无守护进程 -> 7.

## 6. 本地 API 集成测试

- 启动本地守护进程, 使用 runtime token 调用.
- Cookie 路径:
  - `GET /` 获得 Cookie.
  - 用 Cookie 调 `/api/status`.
  - SPA fallback 也设置 Cookie.
- Bearer 路径.
- `POST /api/shutdown`:
  - 未带自定义头 403.
  - 正确请求 202, 进程在超时内退出, runtime 文件删除.
- 设置:
  - 保存 base_url 与 token.
  - 响应不含 token 原值.
  - 文件权限 0600.
  - 非法时区拒绝.
- 代理:
  - 上游 401 原样透传.
  - 上游不可达返回 502 `upstream_unreachable`.
  - CSV 下载流式转发.

## 7. 数据导入测试

夹具:

- 一个旧 `room.toml`.
- 两个房间目录, 每个一份 `records.csv`.
- 两个归档, 每个 `.csv` + `.toml`.
- 一个 `deleted/` 文件.
- 一份末行无换行的 CSV.
- 一份含非法行的 CSV.

断言:

- 房间数, 样本数, 归档数一致.
- 时间转为 UTC 毫秒.
- 电量整数与小数的解析.
- 末行无换行不丢数据.
- 非法行报告行号并中止.
- 重复导入拒绝.
- 导入失败整体回滚.

## 8. 前端测试

- Vitest:
  - 差值, 用量, 日均, 预计天数计算.
  - 电量回升切窗.
  - 时间范围预设.
  - 时区格式化.
  - 错误码到文案映射.
  - 通知节流逻辑.
- 组件:
  - 四态渲染.
  - 表单校验.
  - 危险操作确认.
- Playwright, 可选:
  - 加载仪表盘.
  - 登录帮助跳转.
  - 切换房间.
  - 记录框选与归档.
  - 设置保存.
- 不测试具体文案.

## 9. 人工冒烟

### 9.1 服务端

- Caddy 子路径部署, 完整流程.
- Nginx 子路径部署, 完整流程.
- 服务端直接 TLS.
- token 创建, 撤销, 轮换.
- 备份与恢复.
- 旧数据导入.
- 升级迁移.

### 9.2 客户端

- macOS:
  - `DormWatt.app` 菜单栏图标.
  - 打开 Web, 退出.
  - 深色菜单栏图标.
- Windows:
  - `dormwatt-tray.exe` 无控制台窗口.
  - 托盘菜单与退出.
  - 开机自启动.
- Linux:
  - KDE 或 XFCE 托盘.
  - GNOME 安装扩展后托盘.
  - 无托盘宿主时 `--no-tray`.

### 9.3 浏览器

- Chrome, Safari, Firefox 最新版.
- 浅色与深色.
- 手机宽度.
- 通知授权拒绝, 允许, 已阻止.
- 页面隐藏后恢复.
- 多标签页通知去重.

## 10. 安全测试

- 日志断言:
  - 请求 `PUT /v1/session` 后, 日志不包含三个值.
  - 认证失败日志不包含 token.
- 响应断言:
  - `/v1/session` 不返回凭据.
  - `/api/settings` 不返回 token.
- 本地 API:
  - DNS rebinding 模拟: Host 为 `evil.example` 时 403.
  - 跨源表单模拟: 无自定义头时 403.
- 反向代理:
  - 非可信来源的 `X-Forwarded-For` 不影响限速与日志.
- 文件权限:
  - Unix 下配置, 数据库, runtime 文件为 0600.

## 11. 性能测试

- 10 万样本:
  - `/v1/records` 带时间范围响应时间.
  - CSV 导出内存占用稳定.
  - 归档创建事务耗时.
- 并发:
  - 10 个并发 `/v1/degree` 只触发一次上游.
  - 并发归档请求不产生重复样本.
- 前端:
  - 5000 条表格分页.
  - 10 万点图表采样后流畅.

## 12. CI 任务

| 任务 | 平台 | 命令 |
|------|------|------|
| 单元与集成 | Linux | `go test ./...` |
| 无托盘构建 | Linux | `CGO_ENABLED=0 go build -tags notray ./...` |
| 托盘编译 | macOS | `CGO_ENABLED=1 go build ./...` |
| 托盘编译 | Windows | `CGO_ENABLED=1 go build ./...` |
| 竞态 | Linux | `go test -race ./...` |
| 前端 | Linux | `bun run typecheck && bun run test && bun run build` |
| 端到端 | Linux | `bun run test:e2e` 可选 |
| 子路径 | Linux | `go test ./internal/server/... -run TestBasePath` |
| 数据导入 | Linux | `go test ./internal/store/... -run TestImportLegacy` |

说明:

- cgo 构建在各平台原生 runner 上执行, 不做交叉编译.
- 托盘只做编译检查, 交互走人工冒烟.
- 前端测试不阻塞后端单元测试, 但发布前必须通过.

## 13. 缺陷回归清单

以下旧缺陷必须有对应回归用例:

1. 子路径被 `join("/get-degree")` 丢弃.
2. 归档重名静默覆盖.
3. 默认归档名年月日错位.
4. 归档非原子导致样本丢失.
5. 上游请求无超时.
6. 地区取 `areas.pop()`.
7. 房间信息缓存不随会话更新失效.
8. 业务错误返回 200.
9. 配置无版本号升级失败.
10. 托盘退出后端口未释放.

## 14. 完成标准

- 关键包的单元测试通过, 竞态检测通过.
- 子路径, 鉴权, 归档事务, 数据导入四组集成测试通过.
- 三平台构建成功, 托盘人工冒烟通过.
- 本地 API 的 Host / Origin / 自定义头校验测试通过.
- 旧数据导入在真实样本副本上验证一次.
- 验收清单逐项勾选.
