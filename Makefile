.PHONY: build run test lint docker-up docker-down migrate-up migrate-down swagger clean

APP_NAME := api
BUILD_DIR := ./bin
MAIN_PATH := ./cmd/api/main.go
MIGRATE_DIR := ./migrations
DATABASE_URL ?= postgres://notification:notification@localhost:5432/notification?sslmode=disable

build:
	CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)

run: build
	$(BUILD_DIR)/$(APP_NAME)

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down -v

migrate-up:
	migrate -path $(MIGRATE_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATE_DIR) -database "$(DATABASE_URL)" down

swagger:
	swag init -g $(MAIN_PATH) -o ./docs

clean:
	rm -rf $(BUILD_DIR)
	go clean -cache -testcache
