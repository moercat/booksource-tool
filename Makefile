# booksource-tool Makefile
# 一键 build / test / run / deploy / release
#
# 常用命令：
#   make help        所有命令
#   make build       编译
#   make test        跑测试
#   make run         一次性使用（要 --input 参数）
#   make daemon      常驻模式（需 MongoDB）
#   make docker      构建镜像
#   make up          docker compose up -d
#   make down        停
#   make logs        看日志
#   make release TAG=v1.x.x   打 tag 触发 GitHub Actions 自动发布

GO       ?= go
BINARY   := booksource-tool
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE     ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS  ?= -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help
help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## 编译二进制
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: run
run: ## 一次性使用（go run . --input X --output-dir Y）
	$(GO) run .

.PHONY: daemon
daemon: ## 常驻模式（要 MongoDB，环境变量 MONGO_URI）
	MONGO_URI=$${MONGO_URI:-mongodb://localhost:27017} $(GO) run . --daemon

.PHONY: test
test: ## 跑测试
	$(GO) test -race -coverprofile=coverage.out ./...

.PHONY: vet
vet: ## 静态检查
	$(GO) vet ./...

.PHONY: check
check: vet test ## vet + test

.PHONY: tidy
tidy: ## 整理依赖
	$(GO) mod tidy

.PHONY: clean
clean: ## 清理构建产物
	rm -f $(BINARY) coverage.out
	$(GO) clean -cache

# === Docker ===

.PHONY: docker
docker: ## 构建 docker 镜像
	docker build -t booksource-tool/$(BINARY):$(VERSION) -t booksource-tool/$(BINARY):latest .

.PHONY: docker-buildx
docker-buildx: ## 多架构构建并推送
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t booksource-tool/$(BINARY):$(VERSION) \
		-t booksource-tool/$(BINARY):latest \
		--push .

.PHONY: up
up: ## docker compose up -d
	docker compose up -d

.PHONY: down
down: ## docker compose down
	docker compose down

.PHONY: logs
logs: ## 看 docker 日志
	docker compose logs -f

.PHONY: restart
restart: ## 重启容器
	docker compose restart

# === 发布 ===

.PHONY: release
release: ## 打 tag 并推送（触发 GitHub Actions 自动发布）
	@if [ -z "$(TAG)" ]; then \
		echo "用法: make release TAG=v1.0.0"; \
		exit 1; \
	fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)
	@echo "tag pushed, GitHub Actions will build & publish"

.PHONY: snapshot
snapshot: ## goreleaser 本地试发布（不发 GitHub）
	@if [ -z "$$(command -v goreleaser)" ]; then \
		echo "需要安装 goreleaser: go install github.com/goreleaser/goreleaser@latest"; \
		exit 1; \
	fi
	goreleaser release --snapshot --clean