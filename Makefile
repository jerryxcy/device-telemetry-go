.DEFAULT_GOAL := help
DB_URL ?= postgres://postgres:postgres@localhost:5432/telemetry?sslmode=disable

help: ## 顯示可用指令
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## 起 postgres(背景)
	docker compose up -d postgres

down: ## 關掉所有容器
	docker compose down

db-reset: ## 砍掉 DB volume 重建(改了 migrations 之後要跑)
	docker compose down -v && docker compose up -d postgres

run: up ## 本機跑 device-service
	DATABASE_URL="$(DB_URL)" go run ./cmd/device-service

test: ## 單元測試
	go test ./... -race -count=1

test-int: ## 整合測試(需要 docker)
	go test ./... -race -count=1 -tags=integration

smoke: ## 端到端煙霧測試(服務要先跑起來)
	@./scripts/smoke.sh

lint: ## vet + gofmt 檢查
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

build: ## 編譯全部
	go build ./...

.PHONY: help up down db-reset run test test-int smoke lint build
