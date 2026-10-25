# GoChat 常用开发命令。
# 作用：把"要记住的长命令"变成"要记住的短命令"，同时让 CI 和本地用同一套入口。

APP_NAME  := gochat
SERVER_PKG := ./cmd/server
BIN_DIR   := bin

.PHONY: help
help: ## 显示所有可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F ':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## 启动服务（自动加载 .env）
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	go run $(SERVER_PKG)

.PHONY: build
build: ## 编译可执行文件到 bin/
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP_NAME) $(SERVER_PKG)

.PHONY: test
test: ## 运行全部测试
	go test ./...

.PHONY: test-race
test-race: ## 开启数据竞争检测运行测试
	go test -race ./...

.PHONY: vet
vet: ## 静态检查
	go vet ./...

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -w .

.PHONY: tidy
tidy: ## 整理 go.mod
	go mod tidy

.PHONY: check
check: vet test-race ## 提交前自检：静态检查 + 竞争检测测试

.PHONY: migrate-up
migrate-up: ## 应用所有未执行的迁移
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	migrate -path migrations \
		-database "mysql://root:$$MYSQL_ROOT_PASSWORD@tcp(127.0.0.1:3306)/gochat?charset=utf8mb4&multiStatements=true" \
		up

.PHONY: migrate-down
migrate-down: ## 回滚最近一次迁移
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	migrate -path migrations \
		-database "mysql://root:$$MYSQL_ROOT_PASSWORD@tcp(127.0.0.1:3306)/gochat?charset=utf8mb4&multiStatements=true" \
		down 1

.PHONY: migrate-version
migrate-version: ## 查看当前迁移版本
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	migrate -path migrations \
		-database "mysql://root:$$MYSQL_ROOT_PASSWORD@tcp(127.0.0.1:3306)/gochat?charset=utf8mb4&multiStatements=true" \
		version
