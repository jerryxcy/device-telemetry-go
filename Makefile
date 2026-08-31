.DEFAULT_GOAL := help
DB_URL ?= postgres://postgres:postgres@localhost:5432/telemetry?sslmode=disable

help: ## 顯示可用指令
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

up: ## 用容器起全部:postgres、device-service、telemetry-service、Prometheus、Grafana
	docker compose up -d --build
	@echo ""
	@echo "  五個服務都在容器裡跑:"
	@echo ""
	@echo "    postgres           :5432"
	@echo "    device-service     :8080 HTTP(/metrics 在這)  :9090 gRPC"
	@echo "    telemetry-service  :8081 管理面(/metrics 在這) :9091 gRPC"
	@echo "    prometheus         http://localhost:9092  (Status > Targets 看抓取狀態)"
	@echo "    grafana            http://localhost:3000  (免登入,dashboard 已備好)"
	@echo ""
	@echo "  跑 make load 產生流量,圖上才有東西。收工用 make down。"
	@echo ""
	@echo "  注意:這會佔用 8080/9090/9091,不能跟 make run-device / run-telemetry 同時跑。"

db: ## 只起 postgres,給 run-device / run-telemetry 當後盾
	docker compose up -d postgres

down: ## 關掉所有容器(up 與 db 都用這個收工)
	docker compose down

db-reset: ## 砍掉 DB volume 重建(改了 migrations 之後要跑)
	docker compose down -v && docker compose up -d postgres

run-device: db ## 本機跑 device-service(HTTP :8080 / gRPC :9090)
	DATABASE_URL="$(DB_URL)" go run ./cmd/device-service

run-telemetry: db ## 本機跑 telemetry-service(gRPC :9091,需要 device-service 也在跑)
	DATABASE_URL="$(DB_URL)" go run ./cmd/telemetry-service

load: ## 持續打流量餵儀表板(Ctrl-C 停),需要 make up 先跑起來
	@echo "持續打流量,Ctrl-C 停止…"
	@while true; do \
		./scripts/smoke-device-http.sh >/dev/null 2>&1 || true; \
		./scripts/smoke-telemetry-grpc.sh >/dev/null 2>&1 || true; \
		sleep 1; \
	done

test: ## 單元測試
	go test ./... -race -count=1

test-int: ## 整合測試(需要 docker)
	go test ./... -race -count=1 -tags=integration

smoke-device-http: ## 煙霧測試 device-service 的 HTTP API
	@./scripts/smoke-device-http.sh

smoke-device-grpc: ## 煙霧測試 device-service 的 gRPC API(需要 grpcurl)
	@./scripts/smoke-device-grpc.sh

smoke-telemetry-grpc: ## 煙霧測試 telemetry-service(兩個服務都要跑)
	@./scripts/smoke-telemetry-grpc.sh

lint: ## vet + gofmt 檢查
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

proto: ## 由 proto/ 產生 Go 程式碼(改了 .proto 之後要跑)
	buf lint
	buf generate

proto-breaking: ## 檢查 proto 有沒有破壞相容性的改動
	buf breaking --against '.git#branch=main'

build: ## 編譯全部
	go build ./...

.PHONY: help up db down db-reset run-device run-telemetry load test test-int smoke-device-http smoke-device-grpc smoke-telemetry-grpc lint proto proto-breaking build
