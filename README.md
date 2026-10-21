# DormWatt

ECNU 宿舍电量监控与查询工具. 项目从原 Rust + Tauri 实现迁移为 Go 服务端 + Go 客户端, 客户端提供 CLI, 本地 Web GUI 与系统托盘.

## 状态

当前处于设计阶段. 设计文档位于 [docs/design](docs/design), 实现尚未开始.

## 组成

- 服务端: 定时查询 ECNU 电费接口, SQLite 存储, HTTP API, Bearer Token, 支持子路径挂载.
- 客户端: 直连服务端的 CLI, 本地守护进程, 同源 Web GUI, 三平台系统托盘.
- 鉴权: 可撤销的 Bearer Token, 不使用 mTLS.
- 部署: 反向代理终止 TLS, 服务端可挂载到 `/dormwatt/api` 一类前缀.

## 文档

- [设计文档索引](docs/design/README.md)
- [总体架构](docs/design/01-architecture.md)
- [服务端 API](docs/design/02-server-api.md)
- [鉴权与安全](docs/design/03-auth.md)
- [数据模型](docs/design/04-data-model.md)
- [迁移与路线图](docs/design/09-migration-roadmap.md)

## 构建

实现阶段将提供 `justfile` 与三平台构建说明. 服务端使用纯 Go 静态构建, 桌面托盘版需要 cgo.

## 许可

待定.
