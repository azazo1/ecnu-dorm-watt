# 客户端 CLI

客户端由同一个代码库构建, 通过 `client` 子命令提供命令行能力. CLI 默认直连服务端, 不依赖本地守护进程; `client serve` 额外启动本地 Web 宿主与系统托盘. 通常产出一个可执行文件, Windows 托盘常驻版是单独的 GUI 子系统可执行文件, 具体见部署文档.

## 1. 设计原则

- CLI 与 Web GUI 共用同一份客户端配置.
- CLI 不要求守护进程运行, 适合脚本与远程 SSH 场景.
- 凭据从 stdin, 环境变量或 0600 配置文件读取, 不接受命令行明文参数.
- 人读输出默认表格, 机读输出使用 `--json`.
- 所有命令有明确退出码, 便于脚本判断.
- 破坏性操作默认要求确认, `--yes` 跳过.

## 2. 全局参数

```
dormwatt [--config <path>] [--verbose] [--json] <command>
```

| 参数 | 说明 |
|------|------|
| `--config` | 指定配置文件; `client` 子命令默认找 `client.toml`, `server` 子命令默认找 `server.toml` |
| `--verbose` | 输出 debug 日志到 stderr |
| `--json` | 机器可读输出, 错误也以 JSON 输出到 stdout |
| `--no-color` | 关闭颜色, 默认非 TTY 自动关闭 |

环境变量:

| 变量 | 说明 |
|------|------|
| `DORMWATT_CONFIG` | 等价于 `--config` |
| `DORMWATT_SERVER` | 覆盖 `server.base_url` |
| `DORMWATT_TOKEN` | 覆盖 `server.token`, 仅建议用于临时脚本 |
| `DORMWATT_LOG_LEVEL` | 日志级别 |
| `NO_COLOR` | 关闭颜色 |

命令行不提供 `--token`, 避免进入 shell 历史与进程列表.

## 3. 命令树

```
dormwatt
  version
  client
    config
      show
      path
      set-server <url>
      set-token
      set-timezone <tz>
      set-threshold <degree>
    doctor
    whoami
    status
    degree
    records
    room
      list
      current
      set
      select
      info
      clear
    login
    logout
    archive
      list
      create
      download
      delete
    serve
    open
    quit
  server
    run
    token
      create
      list
      revoke
      rotate
    backup
    check
    purge
    import-legacy
```

## 4. 配置命令

### 4.1 client config show

显示配置摘要:

```
config:      /Users/alice/.config/dormwatt/client.toml
server:      https://example.com/dormwatt/api
token:       dw_7Q3k... (set)
timezone:    Asia/Shanghai
local bind:  127.0.0.1:<local_port>
threshold:   10.00
```

- 永远不显示完整 token.
- `--json` 输出 `token_set: true` 与 `token_prefix`.

### 4.2 client config path

输出配置文件路径, 便于脚本定位.

### 4.3 client config set-server

```shell
dormwatt client config set-server https://example.com/dormwatt/api
```

校验:

- 必须是 `http` 或 `https` URL.
- 不允许 query 与 fragment.
- 去掉尾部斜杠后保存.
- 非回环的 `http` 地址给出警告.

### 4.4 client config set-token

```shell
dormwatt client config set-token
```

- 从 stdin 读取一行 token, 不回显.
- 也接受管道:

```shell
printf '%s\n' "$TOKEN" | dormwatt client config set-token
```

- 校验前缀 `dw_` 与长度.
- 写入后设置文件权限 0600.
- 写入采用临时文件加 rename, 防止半写.

### 4.5 其他设置

```shell
dormwatt client config set-timezone Asia/Shanghai
dormwatt client config set-threshold 10
```

- 时区必须是 IANA 名称.
- 阈值必须大于等于 0.

## 5. 查询命令

### 5.1 client status

输出服务端状态摘要:

```
server:    https://example.com/dormwatt/api (0.1.0)
health:    ok
room:      4408_MH_83_257 (4408, 闵行本科生4号楼)
session:   ok, updated 2026-01-24 14:30:00
monitor:   active, last sample 14:35:32, 33.43 度
next poll: 2026-01-24 14:40:30
```

数据来自 `GET /v1/status`, 不触发上游查询.

### 5.2 client degree

```shell
dormwatt client degree
dormwatt client degree --fresh
```

- 默认允许服务端 15 秒缓存.
- `--fresh` 请求服务端跳过缓存.
- 输出度数, 查询时间与来源.

### 5.3 client records

```shell
dormwatt client records
dormwatt client records --from 2026-01-01T00:00:00+08:00 --to 2026-01-31T23:59:59+08:00
dormwatt client records --format csv --out records.csv
```

参数:

| 参数 | 说明 |
|------|------|
| `--from` | 起始时间, RFC3339 带偏移 |
| `--to` | 结束时间, RFC3339 带偏移 |
| `--room` | 房间 id, 默认活动房间 |
| `--format` | `table`, `csv`, `json`, 默认 `table` |
| `--out` | 写入文件而不是 stdout |
| `--header` | CSV 输出表头 |

- `table` 输出时间, 电量, 差值, 每日用量估算.
- `csv` 直接透传服务端 CSV.
- 时间参数必须带时区偏移, 否则报错.

## 6. 房间命令

### 6.1 client room list

```
ID  ROOM_NO           AREA  BUILDING     ACTIVE  SAMPLES  LAST
1   4408_MH_83_257    102   new-83_MH    yes     1234     2026-01-24 14:35:32
2   1101_MH_80_487    102   new-80_MH    no      892      2025-12-31 23:00:00
```

### 6.2 client room current

输出活动房间配置, 没有时退出码 5.

### 6.3 client room set

```shell
dormwatt client room set \
  --room-no 4408_MH_83_257 \
  --area 102 \
  --building new-83_MH
```

- 调用 `POST /v1/rooms`, 默认 `activate=true`.
- 已存在则复用并激活.
- 成功后输出房间 id 与是否切换.

### 6.4 client room select

交互式选择, 不需要浏览器:

```
? 地区: 剑川路公寓
? 建筑: 闵行本科生4号楼
? 楼层: 4
? 房间: 4408
```

- 依次调用 `/v1/ecnu/districts`, `/buildings`, `/floors`, `/rooms`.
- 如果终端不是 TTY, 必须用 `room set` 显式传参.
- 选择完成后调用 `POST /v1/rooms`.
- 这是替代旧版浏览器注入选房的主要方式.

### 6.5 client room info

```shell
dormwatt client room info
dormwatt client room info --room 1
```

输出地区, 小区, 建筑, 楼层, 房间的中文名称.

### 6.6 client room clear

取消活动房间, 需要确认, `--yes` 跳过. 历史数据保留.

## 7. 登录

### 7.1 默认浏览器辅助

```shell
dormwatt client login
```

流程:

1. 检查 `client.toml` 已有服务端地址与 token.
2. 启动本机 Chrome 或 Chromium, 使用临时 profile, 有头模式.
3. 打开 `https://epay.ecnu.edu.cn/epaycas/electric/load4electricbill?elcsysid=1`.
4. 等待用户完成 SSO 登录, 直到页面离开 `sso.ecnu.edu.cn`.
5. 检测错误页 (`#main-frame-error`), 若出现则报错并关闭浏览器.
6. 从浏览器读取 cookies, 取 `JSESSIONID` 与 `cookie`.
7. 读取 `/html/head/meta[4]` 的 `content` 作为 `X-CSRF-TOKEN`.
8. 调用 `PUT /v1/session`.
9. 调用 `GET /v1/degree?fresh=true` 验证.
10. 输出成功信息与当前电量.
11. 无论成功失败都关闭浏览器.

约束:

- 需要本机安装 Chrome 或 Chromium.
- 浏览器路径可通过 `DORMWATT_BROWSER` 指定.
- 登录超时默认 5 分钟, 可用 `--timeout` 调整.
- 临时 profile, 不读取用户日常浏览器的 cookie, 也不污染用户配置.
- 若用户提前关闭浏览器, 命令以退出码 4 结束.
- 浏览器自动化失败时, 输出手动模式提示, 不自动回退到猜测.

### 7.2 手动模式

```shell
dormwatt client login --manual
```

提示输入:

```
JSESSIONID: ****
cookie: ****
X-CSRF-TOKEN: ****
```

- 使用隐藏输入或逐行 stdin, 不回显.
- 也支持 `--from-file <path>` 读取三行, 便于自动化测试.
- 三个值均非空才提交.
- 提交后同样调用 `/v1/degree?fresh=true` 验证.
- 适用于无 Chrome, 服务器无图形环境, 或浏览器自动化失效的场景.

### 7.3 不再自动续期

旧客户端有 `GuardClient` 每隔 10 秒检查, 未登录时自动拉起浏览器重新登录. 新设计不做静默续期:

- SSO 登录必须人工完成, 后台自动拉起浏览器会打断用户.
- 会话失效时, 服务端状态变为 `invalid`, CLI 与 Web 显示明确提示.
- 用户重新执行 `dormwatt client login` 即可.
- 如果未来需要无人值守, 可另行设计凭据托管, 不在 V1 范围.

### 7.4 client logout

调用 `DELETE /v1/session`, 清除服务端会话.

## 8. 归档命令

### 8.1 client archive list

```shell
dormwatt client archive list
dormwatt client archive list --all
dormwatt client archive list --room 2 --include-deleted
```

```
NAME                  START                END                  RECORDS  ROOM
20260101-20260131     2026-01-01 00:20:00  2026-01-31 23:50:00  892      1
```

- 默认只列活动房间.
- `--all` 传 `room_id=all`, 列出所有房间.
- `--room` 指定房间 id.
- `--include-deleted` 包含软删除.

### 8.2 client archive create

```shell
dormwatt client archive create \
  --from 2026-01-01T00:00:00+08:00 \
  --to 2026-01-31T23:59:59+08:00 \
  --name 202601
```

- 展示将归档的样本数与时间范围, 要求确认.
- `--yes` 跳过确认.
- `--dry-run` 只查询范围, 不执行.
- `--room` 指定房间 id, 默认活动房间.
- 归档为破坏性操作, 文档与提示中必须写明.

### 8.3 client archive download

```shell
dormwatt client archive download 202601 --out 202601.csv
dormwatt client archive download 202601 --room 2
```

- 默认输出到当前目录 `<name>.csv`.
- `--out -` 输出到 stdout.
- `--room` 指定归档所属房间, 默认活动房间.

### 8.4 client archive delete

```shell
dormwatt client archive delete 202601
dormwatt client archive delete 202601 --room 2
```

- 默认要求确认.
- 软删除, 数据仍可从数据库恢复.
- `--yes` 跳过确认.
- `--room` 指定归档所属房间, 默认活动房间.

## 9. 本地守护进程

### 9.1 client serve

```shell
dormwatt client serve
dormwatt client serve --open
dormwatt client serve --no-open
dormwatt client serve --port <local_port>
dormwatt client serve --no-tray
dormwatt client serve --tray=on
```

参数:

| 参数 | 说明 |
|------|------|
| `--port` | 覆盖监听端口, 只改本次运行 |
| `--open` | 启动后打开默认浏览器 |
| `--no-open` | 不自动打开浏览器, 覆盖 `local.open_browser` |
| `--tray` | `auto`, `on`, `off`, 默认取配置中的 `local.tray` |
| `--no-tray` | 等价于 `--tray=off` |

行为:

- 检查 `runtime.json`, 如果已有存活实例则报错并输出 PID.
- 绑定 `client.toml` 中的 `local.bind`, 默认 `127.0.0.1:<local_port>`.
- 生成本地 token, 写入 `runtime.json`, 权限 0600.
- 托管 Web 静态资源, 提供本地 API.
- 启动后台状态轮询.
- 根据 `--tray` 决定是否启动托盘. `auto` 在无桌面会话时自动关闭并记录 info 日志.
- `--open` 启动后打开默认浏览器; `--no-open` 覆盖配置中的 `local.open_browser`, 不打开浏览器.
- 收到 SIGINT / SIGTERM, 托盘 Quit 或 `client quit` 时, 统一关闭监听, 删除 `runtime.json`, 退出.
- 前台运行, 日志输出到 stderr 与文件. 是否做成系统服务由用户决定.

### 9.2 托盘

托盘是本地守护进程的一部分, 不额外启动进程. 使用 `fyne.io/systray`, 三平台适配情况:

| 平台 | 形态 | 说明 |
|------|------|------|
| Windows | 通知区域图标 | 建议使用 GUI 子系统构建, 避免控制台窗口 |
| macOS | 菜单栏图标 | 建议放在 `.app` 中, 使用 `LSUIElement=1` 隐藏 Dock |
| Linux | StatusNotifier / AppIndicator | 依赖桌面托盘宿主; GNOME 需要扩展或 snixembed |

菜单:

```
DormWatt
  当前电量: 33.43 度        (禁用项, 只读)
  状态: 正常                (禁用项, 只读)
  ---
  打开 Web 界面
  立即刷新
  登录帮助
  ---
  退出
```

行为:

- 状态项与图标随健康状态变化:
  - 正常: 常规图标.
  - 低电量: 黄色或感叹号图标.
  - 未登录 / 无房间: 灰色图标.
  - 服务端不可达 / 断网 / TLS 错误: 红色图标.
- 打开 Web 界面: 用默认浏览器打开 `http://127.0.0.1:<port>/`.
- 立即刷新: 触发一次本地状态轮询, 不直接打上游 ECNU.
- 登录帮助: 打开 Web 设置页的登录说明, 不自动拉起 Chrome.
- 退出: 走统一关闭流程.
- 托盘提示文本:
  - Windows / macOS 支持 tooltip, 显示 `DormWatt: 33.43 度`.
  - Linux 使用标题文本, 部分桌面不显示 tooltip.
- 托盘不显示 token, 不显示 cookie.
- 托盘图标资源:
  - Windows: `.ico`, 包含 16/32/48 尺寸.
  - macOS: 模板图标 PNG, 适配浅色与深色菜单栏.
  - Linux: 22/24/32 像素 PNG.
- 托盘事件循环必须占用主线程, HTTP 与轮询在后台 goroutine.

平台注意事项:

- Linux 没有 StatusNotifier 宿主时, 图标不会出现. 优先安装桌面扩展, 或使用 `--no-tray`.
- macOS 直接用裸二进制运行时, 可能出现 Dock 图标或菜单栏行为不稳定. 发布物提供 `DormWatt.app`.
- Windows 使用控制台子系统构建时, 常驻会出现黑窗. 发布物提供 `dormwatt-tray.exe`, 使用 `-H=windowsgui`.
- 通过 SSH 或 systemd 用户服务运行时, 通常没有桌面会话, 建议 `--no-tray`.
- 托盘库需要 cgo. 不含托盘的静态构建使用 `notray` 标签, 见部署文档.

### 9.3 client open

```shell
dormwatt client open
```

- 读取 `runtime.json`.
- 检查 PID 存活与端口可连.
- 打开 `http://127.0.0.1:<port>/`.
- 守护进程未运行时给出明确提示: `run dormwatt client serve first`.
- 不自动后台拉起守护进程, 避免进程管理混乱.
- 托盘菜单的 "打开 Web 界面" 使用同一逻辑.

### 9.4 client quit

```shell
dormwatt client quit
```

- 读取 `runtime.json` 与本地 token.
- 调用本地 `POST /api/shutdown`, 需要本地会话与 `X-Dormwatt-Client: 1`.
- 守护进程返回 202 后异步关闭.
- 未运行时退出码 7, 提示没有运行中的守护进程.
- 托盘 Quit 菜单走同一关闭路径.

### 9.5 系统通知

守护进程根据轮询到的状态主动发送系统通知, 不依赖浏览器页面是否打开.

触发规则:

- 健康状态变化: 服务端不可达, 断网, TLS 错误, 未登录, 未选房, 上游异常.
- 状态恢复: 从异常回到 `ok`.
- 低电量: 当前电量低于阈值, 首次立即通知, 之后 30 分钟最多一次.
- 电量回升: 电量增加时通知, 10 分钟最多一次.

实现:

- 使用 `github.com/gen2brain/beeep` 作为通用回退, 并优先使用平台原生命令.
- 通知图标内嵌 256x256 PNG, 使用深蓝圆角底, 白色电池轮廓与琥珀色闪电. 运行目录中的缓存文件名包含图像摘要, 随图标更新自动更换.
- Linux: `notify-send` 的 `--action` 与 `--wait`, 点击后执行 `xdg-open`.
- Windows: PowerShell Toast 的 `launch` 协议, 点击后由系统打开默认浏览器.
- macOS: 优先 `terminal-notifier` 的 `-appIcon` 与 `-execute`; 未安装时使用 `osascript` 的 JXA ObjC 桥注册通知代理, 点击后打开本地 Web, 无需额外安装.
- 无桌面会话时不启用.
- 通知开关来自 `client.toml` 的 `local.notify`, 默认 true.
- 托盘模式与无托盘模式都生效.
- 通知内容不包含 token, cookie 或 CSRF.
- 发送失败只记录 warn 日志, 不影响轮询.

## 10. 诊断

### 10.1 client whoami

调用 `GET /v1/whoami`, 输出 token 名称与 scope.

### 10.2 client doctor

依次检查:

1. 配置文件存在且权限合理.
2. `base_url` 可解析, 是否带子路径, 是否以 `/` 结尾.
3. token 是否存在, 前缀格式是否合法.
4. DNS 与 TCP 连通性.
5. TLS 证书链与主机名.
6. `GET /v1/health` 是否 200.
7. `GET /v1/whoami` 是否 200, scope 是否足够.
8. `GET /v1/status` 中房间与会话状态.
9. 本地守护进程是否运行 (仅提示).
10. 数据库不可见, 但提示服务端返回的 storage 错误.

输出示例:

```
[ok]   config file: /Users/alice/.config/dormwatt/client.toml (0600)
[ok]   base url: https://example.com/dormwatt/api
[ok]   token: dw_7Q3k... (read,write)
[ok]   tls: valid until 2027-01-01
[ok]   health: 0.1.0
[warn] room: not configured
[warn] session: missing, run "dormwatt client login"
```

退出码: 有 `fail` 项为 4, 只有 `warn` 为 0.

## 11. 健康状态映射

客户端在本地守护进程与 `doctor` 中复用旧状态语义:

| 状态 | 判定 | 说明 |
|------|------|------|
| `ok` | `/status` 可访问且 room 与 session 正常 | 正常 |
| `no_room` | 无活动房间 | 需要选房 |
| `not_logged_in` | session state 为 `missing` 或 `invalid` | 需要登录 |
| `server_down` | 传输错误, 但本机可联网 | 服务端不可达 |
| `no_net` | 传输错误且联网探测失败 | 本机断网 |
| `tls_error` | TLS 证书或握手错误 | 证书问题 |
| `upstream_error` | 服务端可达, 上游查询失败 | ECNU 或网络问题 |
| `unknown` | 其他错误 | 需要看日志 |

去抖:

- `server_down` 与 `no_net` 连续 3 次才展示, 与旧实现一致.
- 状态变化时才更新与通知.
- `/status` 每 10 秒查询一次, 不触发上游.

## 12. 退出码

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 参数或配置错误 |
| 3 | 认证失败或权限不足 |
| 4 | 网络或上游不可用 |
| 5 | 未配置房间或会话 |
| 6 | 服务端返回业务错误 |
| 7 | 本地守护进程未运行或端口占用 |
| 130 | 用户中断 |

## 13. 输出约定

- 表格使用等宽字体, 列宽按终端宽度截断.
- `--json` 输出单行 JSON, 错误也输出 JSON:

```json
{
  "ok": false,
  "error": {
    "code": "ecnu_not_logged_in",
    "message": "ECNU session expired"
  }
}
```

- 日志只写 stderr, stdout 只放数据.
- CSV 输出到 stdout 时不夹杂提示信息.

## 14. 服务端管理命令

虽然属于服务端, 但同属一个二进制:

```shell
dormwatt server run
dormwatt server token create --name <name> --scope read,write
dormwatt server token list
dormwatt server token revoke --name <name>
dormwatt server token rotate --name <name>
dormwatt server backup --out <path>
dormwatt server check
dormwatt server purge --deleted-before <RFC3339> --yes
dormwatt server import-legacy --data-dir <path> --config-dir <path>
```

约定:

- `server token *` 与服务端运行实例共用数据库, 不需要重启.
- `server purge` 需要单实例锁, 建议先停止服务端.
- `server import-legacy` 要求数据库为空.
- 所有管理命令支持 `--json` 输出摘要.
