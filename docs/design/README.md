# DormWatt 设计文档

DormWatt 是 ECNU 宿舍电量查询与监控工具. 本项目把原有 Rust + Tauri 实现迁移为 Go 服务端 + Go 客户端, 客户端提供 CLI 与本地 Web GUI. 本文档集只描述设计, 暂不包含实现.

## 范围

- 服务端: 单进程, 定时查询 ECNU 电费接口, 记录电量样本, 提供 HTTP API, 支持挂载到任意 URL 子路径.
- 客户端: 单个 `dormwatt` 二进制, 提供直接访问服务端的 CLI, 以及监听本地端口的守护进程, 由它托管 Web GUI 与系统托盘.
- 迁移旧服务端的接口语义, 数据格式与用户可见功能.
- 不迁移 Tauri 桌面壳, 但保留系统托盘, 用 Go 跨平台托盘库重做, 尽量覆盖 Windows, macOS, Linux.
- 不再使用 mTLS, 改用可撤销的 Bearer Token.
- 认证终止在反向代理或服务端自身的 TLS, 客户端不持有客户端证书.

## 非目标

- V1 不支持多用户共享一个服务端实例. 一个实例对应一个宿舍房间的监控目标, 但保留多个房间的历史数据.
- V1 不提供账号注册, 找回密码等用户体系. Token 由管理员通过 CLI 生成.
- V1 不提供移动端原生应用. Web GUI 在移动浏览器上可用即可.
- V1 不实现服务端主动登录 ECNU. 登录态由客户端获取后上传.

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-architecture.md](01-architecture.md) | 总体架构, 进程模型, 模块划分, 技术选型 |
| [02-server-api.md](02-server-api.md) | 服务端 HTTP API 契约与错误模型 |
| [03-auth.md](03-auth.md) | Token 鉴权, 权限范围, 本地 API 防护 |
| [04-data-model.md](04-data-model.md) | 配置, SQLite 表, CSV 格式, 版本迁移 |
| [05-server-runtime.md](05-server-runtime.md) | ECNU 查询, 采集循环, 归档事务, 日志 |
| [06-client-cli.md](06-client-cli.md) | CLI 命令面, 登录流程, 配置文件 |
| [07-local-api-web.md](07-local-api-web.md) | 本地 HTTP API, Web GUI, 浏览器通知 |
| [08-deployment.md](08-deployment.md) | 子路径挂载, 反向代理, 守护进程 |
| [09-migration-roadmap.md](09-migration-roadmap.md) | 旧接口映射, 数据导入, 里程碑, 开放问题 |
| [10-web-ui-spec.md](10-web-ui-spec.md) | Web UI 布局, 组件, 状态, 交互与验收 |
| [11-test-plan.md](11-test-plan.md) | 测试分层, 关键用例, CI 与人工冒烟 |

## 关键决策

| 主题 | 决策 | 理由 |
|------|------|------|
| 语言 | 全部使用 Go 1.27 | 单一工具链, 交叉编译简单, 便于部署 |
| 交付形态 | 单二进制 `dormwatt` + 子命令 `server` / `client` | 减少安装步骤, 共享配置与日志代码 |
| 服务端鉴权 | 不透明 Bearer Token, 配置只存 SHA-256 | 可撤销, 可分级, 不依赖证书分发 |
| 传输安全 | 反向代理终止 TLS, 或服务端配置证书 | 兼容子路径部署, 不引入 mTLS |
| 子路径 | `server.base_path` 可配置, 路由全部相对挂载 | 适配 `https://example.com/dormwatt/api` |
| 存储 | SQLite, WAL 模式, 纯 Go 驱动 | 归档操作可事务化, 查询与统计方便 |
| 时间 | 存储 UTC 毫秒, 展示时按配置时区 | 避免夏令时与时区歧义 |
| 房间 | 多房间表 + 一个 active room | 切房间不丢历史, 与旧目录结构对齐 |
| 登录 | 服务端只消费 cookie, 客户端负责获取 | 服务端常无图形环境, 登录需要交互 |
| 房间选择 | 服务端代理 ECNU 目录接口, 前端级联选择 | 去掉浏览器注入式选房, 更适合 Web |
| 客户端形态 | CLI 直连 + 本地 `serve` 托管 Web + 系统托盘 | CLI 不依赖守护进程, Web 不暴露服务端 token |
| 托盘 | `fyne.io/systray`, 三平台尽力适配 | 保留旧版常驻入口, 打开 Web 与退出 |
| 旧 API | 不保留旧路由, 提供映射表与导入工具 | 旧接口错误语义与 HTTP 状态混乱 |

## 术语

- 样本 sample: 一次电量读数, 由时间戳与剩余电量组成. 只在读数变化超过阈值时记录.
- 活动房间 active room: 采集循环当前查询的房间. 同一时刻最多一个.
- 会话 session: 从 ECNU 获取的 `JSESSIONID`, `cookie`, `X-CSRF-TOKEN` 三元组. 只在服务端内存或受限文件中保存.
- 归档 archive: 从活动样本流中切出的一段时间, 移入归档表, 可按名称下载为 CSV.
- 子路径 base path: 服务端在公网 URL 上的挂载前缀, 例如 `/dormwatt/api`.
- 本地守护进程 local daemon: `dormwatt client serve` 启动的进程, 监听回环地址, 托管 Web GUI, 并在桌面环境显示托盘图标.

## 文档状态

- 版本: 设计草案 v0.2
- 目标 Go 版本: 1.27
- 模块名: `github.com/azazo1/ecnu-dorm-watt`
- 本阶段只产出文档, 不写实现代码.
- 文档共 12 篇, 界面细节见 10, 测试与验收见 11.
