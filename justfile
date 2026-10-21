[private]
default:
    @just --list

# 下载 Go 依赖
deps:
    go mod download

# 格式化 Go 代码
fmt:
    gofmt -w cmd internal

# 静态检查
vet:
    go vet ./...

# 前端依赖与构建, 并同步到 embed 目录
web:
    cd web && bun install && bun run build
    python3 scripts/embed_web.py

# 构建桌面版, 含托盘, 需要 cgo
build: web
    CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o dist/dormwatt ./cmd/dormwatt

# 构建服务端静态版, 不含托盘
build-server: web
    CGO_ENABLED=0 go build -tags notray -trimpath -ldflags "-s -w" -o dist/dormwatt-server ./cmd/dormwatt

# 生成当前平台发布归档, 包含桌面版与服务端版
dist:
    uv run --no-project python scripts/dist.py

# 运行服务端
server:
    @mkdir -p .tmp
    @CGO_ENABLED=0 go build -tags notray -o .tmp/dormwatt-server-dev ./cmd/dormwatt
    @exec ./.tmp/dormwatt-server-dev server run

# 运行本地客户端, 含托盘
client:
    @mkdir -p .tmp
    @CGO_ENABLED=1 go build -o .tmp/dormwatt-client-dev ./cmd/dormwatt
    @exec ./.tmp/dormwatt-client-dev client serve

# 创建服务端 token
token name scope="read,write":
    go run ./cmd/dormwatt server token create --name {{name}} --scope {{scope}}

# 运行全部测试
test:
    go test ./...

# 运行竞态测试
test-race:
    go test -race ./...

# 运行子路径集成测试
test-subpath:
    go test ./internal/server/... -run BasePath
