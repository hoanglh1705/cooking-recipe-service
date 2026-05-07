SHELL := /bin/bash

GO ?= go
BIN_DIR := bin

# Cho phép load .env vào môi trường khi chạy run-* / seed local.
ifneq (,$(wildcard .env))
	include .env
	export
endif

.PHONY: tidy build api worker seed run-api run-worker run-seed \
        infra-up infra-down infra-logs infra-reset \
        fmt vet test clean

# --- Build ---

tidy:
	$(GO) mod tidy

build: api worker seed

api:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/api ./cmd/api

worker:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/worker ./cmd/worker

seed:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/seed ./cmd/seed

# --- Run ---

run-api:
	$(GO) run ./cmd/api

run-worker:
	$(GO) run ./cmd/worker

# Seed ~20 món Việt phổ biến vào bảng dishes (idempotent).
run-seed:
	$(GO) run ./cmd/seed

# --- Infra (Postgres + Redis + Asynqmon) ---

infra-up:
	docker compose up -d
	@echo ""
	@echo "Postgres : localhost:5432  (user=postgres pass=postgres db=cooking_recipe)"
	@echo "Redis    : localhost:6379"
	@echo "Asynqmon : http://localhost:8081"

infra-down:
	docker compose down

infra-logs:
	docker compose logs -f

# Xoá volume — chạy lại sẽ là DB trống. Cẩn thận khi chạy.
infra-reset:
	docker compose down -v

# --- Quality ---

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

clean:
	rm -rf $(BIN_DIR)
