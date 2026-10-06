.PHONY: all help dev build test vet run stop restart logs status shell compose-up compose-down compose-logs

APP_NAME ?= oronbox-server
IMAGE_NAME ?= $(APP_NAME)
CONTAINER_NAME ?= $(APP_NAME)

help:
	@echo "OronBox Server 常用指令:"
	@echo "  make dev          - 本地直接运行服务 (读取 .env)"
	@echo "  make build        - 本地构建二进制至 bin/oronbox-server"
	@echo "  make test         - 运行 Go 测试套件"
	@echo "  make vet          - 执行代码静态检查"
	@echo "  make run          - 构建 Docker 镜像并启动容器"
	@echo "  make stop         - 停止并删除 Docker 容器"
	@echo "  make restart      - 重启 Docker 容器"
	@echo "  make logs         - 实时跟踪 Docker 容器日志"
	@echo "  make status       - 查看当前容器状态"
	@echo "  make shell        - 登录进入容器终端"
	@echo "  make compose-up   - 通过 docker compose 启动服务"
	@echo "  make compose-down - 停止 docker compose 服务"
	@echo "  make compose-logs - 跟踪 compose 服务日志"

# --- 本地开发工作流 ---

dev:
	go run ./cmd/server

build:
	./tool/build.sh

test:
	go test -count=1 ./...

vet:
	go vet ./...

# --- Docker 容器化运维 ---

run:
	@if [ ! -f .env ]; then \
		echo "错误: 未找到 .env 配置文件，请从 .env.example 复制并填写所需配置"; \
		exit 1; \
	fi
	docker stop $(CONTAINER_NAME) 2>/dev/null || true
	docker rm $(CONTAINER_NAME) 2>/dev/null || true
	docker build -t $(IMAGE_NAME) .
	docker run -d \
		--name $(CONTAINER_NAME) \
		--env-file .env \
		--network host \
		-v oronbox-data:/var/lib/oronbox \
		-w /var/lib/oronbox \
		--restart unless-stopped \
		$(IMAGE_NAME)
	@echo "Started: $$(docker ps -q -f name=$(CONTAINER_NAME))"

stop:
	docker stop $(CONTAINER_NAME) 2>/dev/null || true
	docker rm $(CONTAINER_NAME) 2>/dev/null || true

restart:
	docker restart $(CONTAINER_NAME)

logs:
	docker logs -f $(CONTAINER_NAME) --tail 100

status:
	docker ps -f name=$(CONTAINER_NAME)

shell:
	docker exec -it $(CONTAINER_NAME) sh

# --- Docker Compose ---

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f --tail 100
