# 总体架构

## 1. 目标与约束

- 用 Go 重写原 Rust 服务端与 Tauri 客户端.
- 服务端只做一件事: 周期性从 ECNU 电费系统取数, 保存样本, 提供查询与管理 API.
- 客户端不内置桌面 GUI, 改为浏览器访问本地守护进程托管的 Web 页面.
- 保留系统托盘: 打开 Web GUI, 展示状态, 退出守护进程, 尽量覆盖三平台.
- 服务端必须能部署在任意 URL 子路径下, 并且不依赖 mTLS.
- 单一代码库, 默认一个二进制同时包含 `server` 与 `client` 子命令; 另提供 `notray` 构建用于无托盘与静态部署.
- V1 面向单用户或单宿舍, 数据量在十万级样本以内, 不引入分布式组件.

## 2. 进程与部署拓扑

```
                    ECNU epay (HTTPS)
                           ^
                           | cookie + form
                           |
+--------------------------+--------------------------+
|                  dormwatt server                    |
|  monitor loop -> ECNU client -> store (SQLite)       |
|  HTTP API (/dormwatt/api/v1/...) + token auth        |
+--------------------------+--------------------------+
                           ^
                           | HTTPS + Bearer token
             +-------------+-------------+
             |                           |
   dormwatt client CLI          dormwatt client serve
   (短生命周期, 直连)            (本地守护进程 + 托盘)
             |                           |
             |                 127.0.0.1:20532
             |                           |
             |                    浏览器 Web GUI
             |                  (同源, 本地 Cookie)
             |
             |                    系统托盘 (三平台)
             |                  (打开 Web / 状态 / 退出)
```

三种运行形态:

1. 服务端常驻: 部署在宿舍内网小主机, NAS, 云服务器或容器中.
2. CLI 直连: `dormwatt client status` 等命令直接访问服务端, 不依赖本地守护进程.
3. 本地守护进程: `dormwatt client serve` 监听回环地址, 托管静态页面并代理服务端 API, 同时在桌面会话中显示托盘图标. Web GUI 全程只访问本地端口, 因此不涉及 CORS, 也不需要把服务端 token 放进浏览器存储.

### 2.1 客户端进程模型

- `client serve` 是单进程, 内部包含:
  - HTTP 服务 goroutine (本地 API + 静态资源).
  - 状态轮询 goroutine.
  - 托盘事件循环.
- 托盘库要求事件循环占用主线程, 因此 `systray.Run` 在主 goroutine 中阻塞运行, HTTP 与轮询在后台 goroutine.
- 收到 SIGINT / SIGTERM 或托盘 Quit 时, 统一走关闭流程: 停止 HTTP, 取消轮询, 删除 runtime 文件, 调用 `systray.Quit`, 退出.
- 无显示环境 (SSH, 无 Desktop, systemd 用户服务) 默认不启用托盘, 用 `--tray=off` 或 `--no-tray` 明确关闭.
- 托盘与 Web GUI 不通过额外端口通信, 直接读取进程内状态缓存.

## 3. 职责边界

| 组件 | 负责 | 不负责 |
|------|------|--------|
| 服务端 | 采集, 存储, 归档, 鉴权, 上游查询代理 | 浏览器交互, 系统通知, 桌面窗口 |
| 客户端 CLI | 配置管理, 登录辅助, 命令行查询, 诊断 | 常驻采集 |
| 本地守护进程 | 静态页面, 本地 API, 上游代理, 状态缓存, 托盘图标 | 数据库, 归档事务 |
| Web GUI | 展示, 图表, 表单, 浏览器通知 | 直接访问上游, 持有服务端 token |

状态所有权:

- 服务端拥有: 房间表, 样本, 归档, ECNU 会话, token, 采集状态.
- 客户端拥有: 服务端基址, 服务端 token, 本地监听地址, 展示时区, 电量阈值, 轮询周期.
- 浏览器拥有: 仅 UI 偏好 (主题, 图表缩放), 通过 `localStorage` 保存.

## 4. 模块划分

```
ecnu-dorm-watt/
  cmd/
    dormwatt/
      main.go                 # 只做参数入口
  internal/
    cli/
      root.go                 # cobra 根命令
      server/                 # server run, token, purge, backup
      client/                 # client serve, status, degree, login, ...
    config/
      config.go               # 服务端与客户端配置结构
      load.go                 # 路径解析, 默认值, 环境变量覆盖
      migrate.go              # 配置版本迁移
      validate.go             # 校验与 base path 规范化
    logging/
      logging.go              # slog 初始化, 文件轮转, 脱敏
    version/
      version.go              # ldflags 注入
    store/
      store.go                # 打开数据库, PRAGMA, 关闭
      migrate.go              # 数据库迁移
      tokens.go               # token 表
      rooms.go                # 房间表与活动房间
      samples.go              # 样本读写
      archives.go             # 归档事务
      session.go              # 会话持久化
      stats.go                # 状态统计
      models.go               # 内部数据模型
    server/
      server.go               # 组装依赖, 启动, 优雅关闭
      router.go               # 子路径挂载
      api/
        problem.go            # problem+json
        middleware.go         # request id, 日志, 恢复, 限速, 认证
        health.go
        status.go
        degree.go
        records.go
        rooms.go
        session.go
        archives.go
        catalog.go
      auth/
        token.go              # 生成, 摘要, 校验
        scopes.go
      monitor/
        monitor.go            # 采集循环
        state.go              # 运行状态与错误
      ecnu/
        client.go             # 上游 HTTP 客户端
        types.go              # 上游响应结构
        query.go              # 电量与目录查询
        errors.go             # 上游错误分类
      catalog/
        cache.go              # 目录缓存
      roominfo/
        resolve.go            # room_no 解析与名称补全
    client/
      config/                 # 客户端配置
      upstream/               # 服务端 HTTP 客户端
      localapi/
        server.go             # 本地监听
        auth.go               # 本地 token 与 Cookie
        handlers.go           # 代理与设置
      webui/
        embed.go              # go:embed 静态资源
      tray/
        tray.go               # 托盘事件循环与菜单
        status.go             # 托盘状态与图标状态映射
        icons/                # 三平台图标资源
      login/
        browser.go            # go-rod 登录辅助
        manual.go             # 手动粘贴
      doctor/
        doctor.go             # 诊断
    appdirs/
      appdirs.go              # 跨平台配置/数据/日志目录
    lockfile/
      lockfile.go             # 单实例锁
  web/
    package.json
    vite.config.ts
    src/
  docs/
    design/
  assets/
```

约束:

- `cmd/` 不放业务逻辑.
- `internal/server/api` 只做协议转换, 不直接访问 SQLite.
- `internal/store` 不引用 HTTP 与 CLI.
- `internal/client` 不引用服务端内部包, 只共享 `internal/protocol` 中的线格式类型.
- 登录辅助与浏览器自动化隔离在 `internal/client/login`, 不污染核心流程.

补充一个共享包:

- `internal/protocol`: 服务端与客户端共享的请求/响应结构, 错误码常量, 字段命名. 保证两端不会各自漂移.

## 5. 技术选型

| 领域 | 选择 | 说明 |
|------|------|------|
| Go | 1.27 | 与当前环境一致, 使用 `net/http` 1.22+ 路由模式 |
| CLI | `spf13/cobra` | 子命令与帮助信息成熟 |
| 配置 | `BurntSushi/toml` | 与旧配置格式一致, 迁移成本低 |
| 数据库 | `modernc.org/sqlite` | 纯 Go, 免 CGO, 交叉编译简单 |
| 日志 | `log/slog` + `lumberjack` | 标准库结构化日志, 文件轮转 |
| HTTP 客户端 | 标准库 `net/http` | 控制超时, 重定向, TLS 足够 |
| 登录辅助 | `go-rod/rod` | 复用用户 Chrome 完成 SSO, 可选依赖 |
| 系统托盘 | `fyne.io/systray` | Windows / macOS / Linux, 需要 cgo, Linux 走 DBus StatusNotifier |
| 前端 | Vue 3 + TypeScript + Vite + ECharts | 复用旧界面思路与图表经验 |
| 静态资源 | `embed.FS` | 单二进制交付 |
| 文件锁 | `gofrs/flock` | 跨平台单实例保护 |

不使用:

- Gin/Chi: 标准库路由已支持方法与通配段, 少一个依赖.
- bbolt: 归档需要多表事务与时间范围查询, SQL 更合适.
- JWT: 见鉴权文档.
- electron/tauri: 用户明确不迁移 GUI 框架.

## 6. 数据流

### 6.1 采集

```
ticker
  -> 读取活动房间 (无则空闲)
  -> 读取会话 (无则标记 missing 并空闲)
  -> ECNU queryelectricbill
  -> 成功: 更新 last_degree, 与上一条样本比较
       变化 >= 0.01 -> INSERT sample
       无变化       -> 跳过
  -> 失败: 记录 typed error, 更新 status, 退避
```

- 采集 goroutine 是唯一写入样本的路径.
- HTTP 的 `/degree` 使用同一 ECNU 客户端与缓存, 不直接写样本.
- 房间切换或会话更新通过 channel 通知采集循环立即重试, 不等下一个 tick.

### 6.2 登录

```
CLI --browser
  -> go-rod 启动/连接 Chrome
  -> 打开 ECNU 电费页
  -> 等待 SSO 跳转结束
  -> 读取 cookies (JSESSIONID, cookie) 与 meta[4] 的 csrf
  -> PUT /v1/session
  -> GET /v1/degree 验证
  -> 输出状态
```

- 服务端不打开浏览器.
- 手动模式由 CLI 提示输入三个值, 从 stdin 读取, 不进 shell 历史.
- 会话上传后服务端立即做一次验证查询, 结果反映在 `/status`.

### 6.3 Web 查询

```
浏览器 -> 本地 /api/status
       -> 本地 /api/records?from=&to=
              -> 服务端 /v1/records
                    -> SQLite
       -> 本地 /api/degree (可选实时)
```

- 浏览器不直连服务端, 不需要 CORS.
- 本地守护进程缓存最近一次 status, 页面轮询本地接口即可.
- 记录与归档请求按需转发, 不在本地持久化.

### 6.4 归档

```
Web/CLI -> POST /v1/archives
             -> BEGIN IMMEDIATE
             -> 选出样本, 计算起止
             -> 插入 archives + archive_samples
             -> 删除 samples
             -> COMMIT
          <- 201 ArchiveMeta
```

- 失败回滚, 样本不丢.
- 归档下载流式读 `archive_samples`, 不拼接大字符串.

## 7. 一致性与并发

- SQLite 使用 WAL, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`.
- 写事务使用 `BEGIN IMMEDIATE`, 避免写锁升级死锁.
- 单实例锁: 服务端启动时对数据目录 `server.lock` 加排他锁, 失败则退出. CLI 的 token 命令不加该锁, 通过 SQLite 并发写与重试工作.
- 活动房间与采集状态保存在内存, 同时持久化到 `settings` 表; 启动时恢复.
- 实时电量查询用 singleflight 合并并发请求, 缓存 15 秒.
- 目录查询按父级 id 缓存 10 分钟, 并发请求合并.
- 归档创建持有数据库写事务, 但不在事务内做上游 HTTP 请求.
- 优雅关闭: 停止 HTTP 接收, cancel 采集 context, 等待 goroutine, 关闭数据库.

## 8. 配置与目录

| 类型 | Linux | macOS | Windows |
|------|-------|-------|---------|
| 配置 | `$XDG_CONFIG_HOME/dormwatt` 或 `~/.config/dormwatt` | `~/Library/Application Support/dormwatt` | `%AppData%\dormwatt` |
| 数据 | `$XDG_DATA_HOME/dormwatt` 或 `~/.local/share/dormwatt` | `~/Library/Application Support/dormwatt` | `%LocalAppData%\dormwatt` |
| 日志 | 数据目录 `logs/` | 数据目录 `logs/` | 数据目录 `logs/` |
| 缓存 | `$XDG_CACHE_HOME/dormwatt` | `~/Library/Caches/dormwatt` | `%LocalAppData%\dormwatt\cache` |

- debug 构建追加 `-debug` 后缀, 与旧实现一致, 便于并存.
- 配置文件: 服务端 `server.toml`, 客户端 `client.toml`.
- 数据文件: `dormwatt.db`, 日志文件 `server-YYYYMMDD.log` / `client-YYYYMMDD.log`.
- 目录权限 0700, 数据库与配置 0600.

配置优先级:

1. CLI flag.
2. 环境变量, 前缀 `DORMWATT_`.
3. 配置文件.
4. 默认值.

仅暴露必要环境变量: `DORMWATT_SERVER_BIND`, `DORMWATT_SERVER_BASE_PATH`, `DORMWATT_DB_PATH`, `DORMWATT_LOG_LEVEL`.

## 9. 错误与日志

- 内部错误使用 `%w` 包装, 保留调用链.
- 对外错误只暴露 problem+json 中的 `code` 与安全文案.
- 上游错误分为: 网络, TLS, 重定向到 SSO, 非 JSON 响应, 业务码非成功, 缺少字段.
- 每个组件使用 `slog.With("component", "...")`.
- 请求日志由中间件统一输出, handler 内不重复打印.
- 采集日志只在状态变化时输出 info, 每次 tick 输出 debug.
- 日志轮转按天, 保留 14 天, 单文件上限 50 MiB.

## 10. 构建与发布

- 构建命令注入版本, 提交 hash 与构建时间.
- `dormwatt version` 输出三者, 服务端 `/v1/health` 返回版本.
- Web 静态资源先 `vite build`, 再 `go build`, 通过 `embed.FS` 打入二进制.
- 托盘依赖 cgo, 因此桌面版必须在目标平台上原生构建, 不能像纯 Go 服务端那样随意交叉编译.
- 构建变体:
  - `default`: 含托盘与浏览器登录, `CGO_ENABLED=1`.
  - `notray`: 去掉 `fyne.io/systray`, 可 `CGO_ENABLED=0` 静态构建, 用于服务端, 容器与无桌面环境.
  - `nobrowser`: 去掉 `go-rod`, 用于不需要浏览器自动登录的构建.
  - `notray,nobrowser`: 完全纯 Go, 用于最小服务端镜像.
- 构建标签只影响可选能力, 不影响 CLI 与服务端核心逻辑.
- 发布矩阵:
  | 平台 | 产物 | 说明 |
  |------|------|------|
  | Linux amd64/arm64 | `dormwatt` | cgo 构建, 托盘依赖桌面 StatusNotifier |
  | Linux amd64/arm64 | `dormwatt-notray` | 静态构建, 服务端优先 |
  | macOS amd64/arm64 | `dormwatt`, `DormWatt.app` | app bundle 便于托盘与隐藏 Dock |
  | Windows amd64 | `dormwatt.exe` | 控制台子系统, CLI 与服务端 |
  | Windows amd64 | `dormwatt-tray.exe` | `-H=windowsgui`, 开机自启用 |
  | 容器 | `dormwatt` (notray) | distroless 静态镜像 |
- CI 使用原生 runner 构建对应平台, 不在 macOS 上交叉编译 Linux cgo 产物.

## 11. 测试策略

- 单元测试:
  - base path 规范化与路由挂载.
  - token 摘要与 scope 判定.
  - room_no 解析.
  - 时间范围边界与归档事务.
  - 配置与数据库迁移.
  - ECNU 响应解析, 包括异常 HTML 与非 JSON.
  - 托盘状态到图标状态的映射 (纯函数).
- 集成测试:
  - `httptest` 起服务端, 使用临时 SQLite 与假 ECNU 上游.
  - 走完整 HTTP 流程: 认证, 上传会话, 设置房间, 采集, 归档, 下载.
  - 服务端固定使用 `/v1/...`; 外部子路径由反向代理剥离.
- 本地 API 测试:
  - Host/Origin/自定义头校验.
  - 无 CORS 头.
  - CLI token 与 Cookie 两种路径.
- 托盘测试:
  - 无法在 CI 中稳定模拟桌面托盘, 只做编译检查与状态映射单测.
  - 发布前在三平台各自做人工冒烟: 图标出现, 菜单可用, 打开 Web, 退出干净.
- 不做纯文案测试, 不追求覆盖率数字, 重点覆盖边界与回归点.
- 详细测试矩阵与人工冒烟清单见 [11-test-plan.md](11-test-plan.md).

## 12. 已知风险

| 风险 | 影响 | 应对 |
|------|------|------|
| ECNU 页面或接口改版 | 无法取数 | typed error + 日志上下文, 集中封装查询 |
| SSO 流程变化 | 自动登录失效 | 手动粘贴 cookie 作为兜底 |
| 子路径拼接错误 | 全站 404 | 统一 joinBaseURL, 集成测试覆盖 |
| SQLite 并发写 | 偶发 busy | WAL, busy_timeout, 短事务 |
| 反向代理未剥离前缀 | 路由不匹配 | 部署要求代理剥离, 服务端固定 `/v1` |
| 本地端口被占用 | serve 启动失败 | 明确错误并提示改端口 |
| Chrome 不存在 | 自动登录不可用 | 手动模式兜底, 文档写明依赖 |
| 服务端时钟漂移 | 样本时间异常 | 启动时记录时间, 展示 NTP 提示 |
| Linux 无托盘宿主 | 图标不显示 | 安装 StatusNotifier 扩展或 snixembed, 或 `--no-tray` |
| macOS 裸二进制无 bundle | Dock 图标异常, 托盘行为不稳定 | 提供 `.app` 包装, `LSUIElement=1` |
| Windows 控制台窗口 | 托盘常驻时出现黑窗 | 使用 `dormwatt-tray.exe` 的 GUI 子系统构建 |
| cgo 交叉编译 | CI 构建失败 | 各平台原生 runner 构建 |
| 托盘主线程阻塞 | HTTP 或退出卡死 | 托盘循环独占主线程, 其余逻辑放 goroutine |
