# DormWatt

ECNU 宿舍电量监控与查询工具. 项目从原 Rust + Tauri 实现迁移为 Go 服务端 + Go 客户端, 客户端提供 CLI, 本地 Web GUI 与系统托盘.

## 状态

核心功能已实现, 设计文档位于 [docs/design](docs/design).

已实现:

- 服务端: ECNU 电量与目录查询, SQLite 存储, 归档事务, Bearer Token, 子路径挂载, 结构化日志.
- 客户端: 直连 CLI, 本地守护进程, 同源 Web GUI, 三平台托盘, 浏览器登录与手动登录.
- 通知: 健康状态异常, 低电量与电量回升支持系统通知与浏览器通知.
- Web: 仪表盘, 记录图表与框选统计, 归档管理, 设置, 登录帮助.
- 数据: 样本, 归档, 房间, 会话, token.

## 快速开始

服务端:

```shell
just build-server

# 第一次运行会生成默认配置与数据库, 然后创建 token
./dist/dormwatt-server server token create --name laptop --scope read,write

# 按需编辑配置中的 base_path 与 bind, 然后启动
./dist/dormwatt-server server run
```

客户端:

```shell
just build

./dist/dormwatt client config set-server https://example.com/dormwatt/api
printf '%s\n' '<token>' | ./dist/dormwatt client config set-token
./dist/dormwatt client login
./dist/dormwatt client doctor
./dist/dormwatt client serve --open
```

浏览器会自动打开 `http://127.0.0.1:20532/`.

## 构建

依赖:

- Go 1.27
- bun, 仅构建 Web 时需要
- cgo 与 C 工具链, 仅托盘版本需要
- Chrome 或 Chromium, 仅浏览器登录需要

```shell
# 构建 Web 并复制到 embed 目录
just web
cp -R web/dist/. internal/client/webui/dist/

# 桌面版, 含托盘
just build

# 服务端静态版, 不含托盘
just build-server

# 测试
just test
```

## 配置

默认路径:

- 服务端配置: `<config>/dormwatt/server.toml`
- 客户端配置: `<config>/dormwatt/client.toml`
- 数据库: `<data>/dormwatt/dormwatt.db`
- 客户端运行状态: `<data>/dormwatt/client/runtime.json`

具体字段与迁移规则见 [04-data-model.md](docs/design/04-data-model.md).

## 文档

- [设计文档索引](docs/design/README.md)
- [总体架构](docs/design/01-architecture.md)
- [服务端 API](docs/design/02-server-api.md)
- [鉴权与安全](docs/design/03-auth.md)
- [数据模型](docs/design/04-data-model.md)
- [客户端 CLI](docs/design/06-client-cli.md)
- [本地 API 与 Web](docs/design/07-local-api-web.md)
- [部署与子路径](docs/design/08-deployment.md)
- [迁移与路线图](docs/design/09-migration-roadmap.md)
- [Web UI 规格](docs/design/10-web-ui-spec.md)
- [测试计划](docs/design/11-test-plan.md)

## 许可

待定.
