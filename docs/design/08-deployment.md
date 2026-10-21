# 部署与子路径挂载

本文档给出服务端部署, 反向代理, 子路径挂载, 守护进程与升级步骤. 目标环境是能访问 `epay.ecnu.edu.cn` 的 Linux 小主机, NAS, 云服务器或容器.

## 1. 安装

发布产物分两类:

| 产物 | 用途 | 构建约束 |
|------|------|----------|
| `dormwatt` | 桌面客户端 + CLI + 服务端, 含系统托盘 | 需要 cgo |
| `dormwatt-notray` | 服务端, 容器, 无桌面环境 | 纯 Go 静态构建 |
| `dormwatt-tray.exe` | Windows 托盘常驻 | `-H=windowsgui` |
| `DormWatt.app` | macOS 托盘应用包 | 含 `LSUIElement` |

```shell
# 从 release 下载桌面版后
install -m 0755 dormwatt /usr/local/bin/dormwatt

# 服务端可以安装无托盘版本
install -m 0755 dormwatt-notray /usr/local/bin/dormwatt
```

从源码构建:

```shell
# 桌面版, 含托盘, 需要 C 工具链
CGO_ENABLED=1 go build -trimpath -o dist/dormwatt ./cmd/dormwatt

# 服务端静态版, 无托盘
CGO_ENABLED=0 go build -tags notray -trimpath -o dist/dormwatt-notray ./cmd/dormwatt
```

平台构建依赖:

| 平台 | 依赖 | 说明 |
|------|------|------|
| Linux | `gcc` 等 C 工具链 | 托盘通过 DBus StatusNotifier 工作, 不链接 GTK |
| macOS | Xcode Command Line Tools | 托盘建议放在 `.app` 中 |
| Windows | MinGW-w64 gcc | 托盘常驻使用 GUI 子系统构建 |

验证:

```shell
dormwatt version
dormwatt server run --help
dormwatt client --help
```

## 2. 服务端初始化

默认端口只在 [04-data-model.md](04-data-model.md) 维护. 下文用 `<server_port>` 表示服务端监听端口, 用 `<local_port>` 表示客户端本地 Web 端口.

### 2.1 创建配置

```shell
sudo install -d -m 0700 -o dormwatt -g dormwatt /etc/dormwatt
sudo install -d -m 0700 -o dormwatt -g dormwatt /var/lib/dormwatt

sudo -u dormwatt tee /etc/dormwatt/server.toml <<'EOF'
version = 1

[server]
allow_insecure_http = false

[auth]
trusted_proxies = ["127.0.0.1/32", "::1/128"]

[ecnu]
poll_interval = "300s"
request_timeout = "15s"

[ui]
timezone = "Asia/Shanghai"

[log]
level = "info"
file = true
EOF
```

说明:

- 服务端默认只监听回环, 由反向代理对外.
- 如果直接对外提供 HTTPS, 增加 `[server.tls]` 的 `cert` 与 `key`.
- 如果确实要在内网明文监听, 设置 `allow_insecure_http = true`, 并确认网络边界可信.

### 2.2 创建首个 token

```shell
sudo -u dormwatt dormwatt server token create \
  --name alice-laptop \
  --scope read,write \
  --config /etc/dormwatt/server.toml
```

输出:

```
token created. store it now, it will not be shown again:

dw_7Q3k...

name:   alice-laptop
scopes: read,write
```

- 只显示一次.
- 服务端运行中也可以创建, 立即生效.
- 只读监控 token 使用 `--scope read`.

### 2.3 启动

```shell
sudo -u dormwatt dormwatt server run --config /etc/dormwatt/server.toml
```

启动日志应包含:

```
bind=127.0.0.1:<server_port>
database=/var/lib/dormwatt/dormwatt.db
```

### 2.4 验证

```shell
curl -sS http://127.0.0.1:<server_port>/v1/health

curl -sS \
  -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:<server_port>/v1/status
```

## 3. 子路径挂载

### 3.1 规则

- 服务端只注册 `/v1/...`, 不读取外部挂载前缀.
- 反向代理负责把外部 `/dormwatt/api/...` 剥成后端 `/v1/...`.
- 代码内部不硬编码域名或外部前缀.
- 服务端内部注册:

```
/v1/health
/v1/status
/v1/records
...
```

- 代理必须去掉外部前缀.
- 旧配置中的 `server.base_path` 与 `server.strict_path` 会被忽略, 应删除.
- 客户端配置的 `base_url` 必须包含外部前缀, 并且不能以 `/` 结尾.
- 如果代理保留前缀, 后端会收到 `/dormwatt/api/v1/...` 并返回 404.

### 3.2 Caddy

```
example.com {
    encode gzip

    handle_path /dormwatt/api/* {
        reverse_proxy 127.0.0.1:<server_port>
    }

    handle {
        respond "not found" 404
    }
}
```

要点:

- `handle_path` 会剥掉 `/dormwatt/api`, 后端收到 `/v1/...`.
- 不要使用不带剥离的 `handle`, 否则后端会收到 `/dormwatt/api/v1/...` 并返回 404.
- Caddy 自动处理 HTTPS.
- 如果未来启用 SSE, 在 `reverse_proxy` 中关闭缓冲:

```
reverse_proxy 127.0.0.1:<server_port> {
    flush_interval -1
}
```

### 3.3 Nginx

```nginx
server {
    listen 443 ssl;
    server_name example.com;

    ssl_certificate     /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;

    client_max_body_size 64k;

    location /dormwatt/api/ {
        # 末尾斜杠必须保留: 把 /dormwatt/api/v1/... 改成 /v1/...
        proxy_pass http://127.0.0.1:<server_port>/;

        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_connect_timeout 5s;
        proxy_read_timeout 65s;
        proxy_send_timeout 65s;

        proxy_buffering off;
    }

    location / {
        return 404;
    }
}
```

要点:

- `proxy_pass` 末尾的 `/` 表示剥掉 `location` 前缀.
- 如果去掉末尾斜杠, 后端会收到 `/dormwatt/api/v1/...` 并返回 404.
- 如果使用 `rewrite` 剥离, 必须保证最终转发路径是 `/v1/...`.

### 3.4 子路径核对清单

- [ ] 反向代理把外部 `/dormwatt/api/...` 剥成后端 `/v1/...`.
- [ ] 客户端 `server.base_url` 写外部入口, 以 `/dormwatt/api` 结尾, 无尾部斜杠.
- [ ] 代理使用 `handle_path` 或带斜杠的 `proxy_pass`.
- [ ] `Authorization` 头没有被代理丢弃.
- [ ] 代理允许 `PUT` 与 `DELETE` 方法.
- [ ] 请求体上限不小于 64 KiB.
- [ ] 超时时间大于服务端的 60 秒写超时.
- [ ] 直接访问 `http://127.0.0.1:<server_port>/v1/health` 返回 200.
- [ ] 外部访问 `https://example.com/dormwatt/api/v1/health` 返回 200.

### 3.5 客户端拼接

客户端必须按以下规则拼接:

```
base_url = https://example.com/dormwatt/api
endpoint = https://example.com/dormwatt/api/v1/records
```

禁止:

```
url.ResolveReference("/v1/records")   # 会丢掉 /dormwatt/api
path.Join(base, "/v1/records")        # 可能丢失 URL 结构
```

实现建议:

```go
func joinBaseURL(base, suffix string) (string, error) {
    u, err := url.Parse(base)
    if err != nil { return "", err }
    u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(suffix, "/")
    return u.String(), nil
}
```

## 4. 系统服务

### 4.1 systemd

`/etc/systemd/system/dormwatt.service`:

```ini
[Unit]
Description=DormWatt server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=dormwatt
Group=dormwatt
ExecStart=/usr/local/bin/dormwatt server run --config /etc/dormwatt/server.toml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/dormwatt
LimitNOFILE=4096

[Install]
WantedBy=multi-user.target
```

启用:

```shell
sudo systemctl daemon-reload
sudo systemctl enable --now dormwatt
sudo systemctl status dormwatt
journalctl -u dormwatt -f
```

### 4.2 macOS launchd

`~/Library/LaunchAgents/com.example.dormwatt.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.example.dormwatt</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/dormwatt</string>
    <string>server</string>
    <string>run</string>
    <string>--config</string>
    <string>/Users/alice/.config/dormwatt/server.toml</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/Users/alice/Library/Logs/dormwatt.out.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/alice/Library/Logs/dormwatt.err.log</string>
</dict>
</plist>
```

加载:

```shell
launchctl load ~/Library/LaunchAgents/com.example.dormwatt.plist
```

### 4.3 Windows

- 使用 `sc.exe` 或 NSSM 注册为服务.
- 数据目录与配置目录使用用户目录, 避免 `Program Files` 写权限问题.
- 具体服务包装命令见项目 README, 不在设计文档展开.

## 5. 容器部署

Dockerfile 草图:

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -tags notray -trimpath -ldflags="-s -w" -o /out/dormwatt ./cmd/dormwatt

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/dormwatt /usr/local/bin/dormwatt
USER nonroot:nonroot
VOLUME ["/data"]
EXPOSE <server_port>
ENTRYPOINT ["/usr/local/bin/dormwatt", "server", "run", "--config", "/data/server.toml"]
```

运行:

```shell
docker run -d \
  --name dormwatt \
  -p 127.0.0.1:<server_port>:<server_port> \
  -v dormwatt-data:/data \
  -v "$PWD/server.toml:/data/server.toml:ro" \
  dormwatt:latest
```

注意:

- 容器内 `bind` 使用 `0.0.0.0:<server_port>` 才能被宿主访问, 此时需要 `allow_insecure_http = true` 或挂载证书.
- 更推荐让容器只暴露到宿主回环, 由宿主反向代理终止 TLS.
- 创建 token:

```shell
docker exec -it dormwatt dormwatt server token create --name alice --scope read,write
```

## 6. 客户端部署

### 6.1 仅用 CLI

```shell
dormwatt client config set-server https://example.com/dormwatt/api
dormwatt client config set-token
dormwatt client doctor
dormwatt client login
dormwatt client status
```

### 6.2 使用 Web GUI 与托盘

```shell
dormwatt client serve --open
```

- 守护进程前台运行, 关闭终端即停止.
- 桌面环境会自动显示托盘图标, 可用 `--tray=on` 或 `--no-tray` 覆盖.
- 浏览器打开 `http://127.0.0.1:<local_port>/`.
- 设置页填入服务端地址与 token, 或提前用 CLI 配置好.
- 托盘菜单可以打开页面, 立即刷新, 查看登录帮助与退出.

如果需要以系统服务常驻, 使用 `--no-tray`:

```ini
[Unit]
Description=DormWatt local client
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/dormwatt client serve --no-tray
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
```

启用:

```shell
systemctl --user enable --now dormwatt-client
```

说明:

- systemd 用户服务通常没有桌面会话与 DBus session, 强制开启托盘会启动失败或图标不显示.
- 托盘常驻应使用桌面自启动, 见 6.3.
- 服务模式仍可通过浏览器访问本地端口, 也可用 `dormwatt client open`.

### 6.3 系统托盘

托盘由 `client serve` 内嵌, 不需要额外进程. 三平台差异如下.

#### Linux

- 构建需要 C 工具链与 cgo.
- 运行时通过 DBus StatusNotifier / AppIndicator 与桌面通信.
- KDE, XFCE, Cinnamon 等通常自带托盘宿主.
- GNOME 默认不显示 StatusNotifier, 需要安装 AppIndicator 扩展, 或使用 snixembed 之类的代理.
- 无法提供托盘宿主时, 使用 `--no-tray`, Web GUI 仍可用.
- 桌面自启动示例 `~/.config/autostart/dormwatt.desktop`:

```ini
[Desktop Entry]
Type=Application
Name=DormWatt
Comment=DormWatt local client
Exec=/home/alice/.local/bin/dormwatt client serve --no-open
Terminal=false
X-GNOME-Autostart-enabled=true
```

- 自启动文件中的路径需要是绝对路径.

#### macOS

- 建议使用 `DormWatt.app` 包装, 而不是裸二进制.
- 最小结构:

```
DormWatt.app/
  Contents/
    Info.plist
    MacOS/
      dormwatt
    Resources/
      DormWatt.icns
```

- `Info.plist` 关键项:

```xml
<key>LSUIElement</key>
<string>1</string>
<key>NSHighResolutionCapable</key>
<string>True</string>
```

- `LSUIElement=1` 表示不显示 Dock 图标, 只在菜单栏显示.
- 登录时启动:
  - 系统设置 -> 通用 -> 登录项, 添加 `DormWatt.app`.
  - 或使用 launchd plist 启动 `DormWatt.app/Contents/MacOS/dormwatt client serve`.
- 未签名或未公证的应用首次运行会被 Gatekeeper 拦截, 需要右键打开或在系统设置中允许.
- 托盘图标使用模板图标, 自动适配浅色与深色菜单栏.

#### Windows

- 发布两个可执行文件:
  - `dormwatt.exe`: 控制台子系统, 用于 CLI 与服务端.
  - `dormwatt-tray.exe`: `-H=windowsgui`, 用于托盘常驻, 不弹控制台窗口.
- 两者来自同一代码库, 只是链接参数不同.
- 开机自启动:
  - 把 `dormwatt-tray.exe client serve` 的快捷方式放入
    `%AppData%\Microsoft\Windows\Start Menu\Programs\Startup`.
  - 或用任务计划程序设置 "登录时" 触发.
- Windows 通知区域始终可用, 不需要额外依赖.
- 退出使用托盘菜单, 或在终端执行 `dormwatt client quit`.

#### 共同行为

- `--tray=auto` 默认在检测到桌面会话时启用.
- `--tray=on` 强制启用, 失败时报错.
- `--tray=off` 或 `--no-tray` 关闭.
- 托盘退出会停止 HTTP 服务, 取消轮询, 删除 `runtime.json`.
- 托盘不显示凭据, 只显示电量与健康状态.

### 6.4 浏览器登录辅助

- `dormwatt client login` 会启动 Chrome, 需要图形会话.
- 在无图形环境的服务器上使用 `--manual`, 从已登录的浏览器复制三个值.
- 不要把 cookie 或 CSRF 值粘贴到聊天工具或工单中.

## 7. 备份与升级

### 7.1 备份

```shell
dormwatt server backup --out /backup/dormwatt-$(date +%Y%m%d-%H%M%S).db
```

或每日 cron:

```cron
15 3 * * * /usr/local/bin/dormwatt server backup --out /backup/dormwatt-$(date +\%F).db
```

- 备份使用 `VACUUM INTO`, 不需要停止服务.
- 定期把备份复制到另一台机器.
- 同时备份 `server.toml`, 但注意它不含 token 明文.

### 7.2 升级

```shell
sudo systemctl stop dormwatt
sudo cp /var/lib/dormwatt/dormwatt.db /var/lib/dormwatt/dormwatt.db.pre-upgrade
sudo install -m 0755 dormwatt-new /usr/local/bin/dormwatt
sudo systemctl start dormwatt
```

- 启动时自动执行数据库迁移.
- 迁移前自动备份到 `backups/`.
- 不支持自动降级. 回滚需要恢复旧二进制与旧数据库备份.
- 升级后检查 `/v1/health` 与日志中的迁移记录.

## 8. 监控与排障

### 8.1 健康检查

```shell
curl -fsS https://example.com/dormwatt/api/v1/health
```

- 返回 200 视为存活.
- 不检查数据库与上游, 避免健康检查本身造成压力.

### 8.2 常见问题

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| 所有请求 404 | 代理未剥离外部前缀 | 检查 `proxy_pass` 末尾斜杠或 Caddy `handle_path`; 直接访问后端 `/v1/health` |
| 401 | token 缺失, 错误或已撤销 | `dormwatt server token list`, 重新创建并更新客户端 |
| 403 | token 缺少 write scope | 创建带 `write` 的 token |
| 502 `ecnu_not_logged_in` | 会话过期 | `dormwatt client login` |
| 502 `ecnu_upstream_error` | 学校接口或网络异常 | 查看服务端日志中的上游错误分类 |
| 429 | 触发限速 | 降低轮询频率, 检查是否多个客户端 |
| 503 | 启动中或关闭中 | 等待启动完成 |
| 客户端连不上本地端口 | 守护进程未运行或端口冲突 | `dormwatt client serve`, 检查 `runtime.json` |
| 浏览器通知不出现 | 未授权或页面关闭 | 设置页授权, 保持页面打开 |
| 自动登录失败 | 未安装 Chrome 或 SSO 页面变化 | 使用 `--manual`, 查看日志 |
| 时间显示偏移 | `ui.timezone` 配置错误 | 改为 IANA 时区名并重启客户端 |
| Linux 托盘图标不出现 | 无 StatusNotifier 宿主 | 安装 AppIndicator 扩展或 snixembed, 或 `--no-tray` |
| macOS 托盘行为异常 | 使用裸二进制而非 app bundle | 使用 `DormWatt.app`, 检查 `LSUIElement` |
| Windows 托盘常驻出现黑窗 | 使用控制台子系统构建 | 改用 `dormwatt-tray.exe` |
| 托盘退出后端口仍占用 | 关闭流程卡住或旧进程残留 | `dormwatt client quit`, 检查日志与 PID |
| systemd 用户服务无托盘 | 服务会话没有桌面 DBus | 使用 `--no-tray`, 托盘改用桌面自启动 |
| `go build` 报 cgo 错误 | 缺少 C 工具链或禁用 cgo | 安装 gcc, 设置 `CGO_ENABLED=1`, 或使用 `notray` 构建 |

### 8.3 日志位置

- Linux: `/var/lib/dormwatt/logs/` 或 `~/.local/share/dormwatt/logs/`.
- macOS: `~/Library/Application Support/dormwatt/logs/`, 具体以启动日志打印的路径为准.
- Windows: `%LocalAppData%\dormwatt\logs\`.

日志按天轮转, 默认保留 14 天.

## 9. 安全核对清单

- [ ] 服务端只监听回环, 或配置了 TLS.
- [ ] 反向代理强制 HTTPS.
- [ ] token 使用 `write` 最小化分发, 监控使用 `read`.
- [ ] 配置文件与数据库权限为 0600.
- [ ] 数据目录权限为 0700.
- [ ] 反向代理不记录 `Authorization` 头.
- [ ] 服务端日志不包含 cookie 与 token.
- [ ] 托盘菜单与提示不显示凭据.
- [ ] 旧 mTLS 证书与配置已移除.
- [ ] 定期备份并验证可恢复.
- [ ] 系统时间同步正常.

## 10. just 命令

以仓库根目录的 `justfile` 为准, 常用:

- `just server`: 构建无托盘服务端并启动.
- `just client`: 构建客户端并启动本地 Web 与托盘.
- `just token name`: 创建服务端 token.
- `just test`, `just test-race`: 运行测试.

- 前端使用 bun, Go 依赖使用 `go mod`.
- 发布前先构建前端, 再构建二进制.
