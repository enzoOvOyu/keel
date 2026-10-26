SHELL := /bin/sh
APP_VERSION ?= development
.PHONY: check build build-agent build-preflight build-installer-helpers generate-agent-proto integration dev-api dev-web dev-worker up logs
check:
	python3 scripts/check-design.py
	python3 -m unittest discover -s poc/h1 -p 'test_*.py'
	go vet ./...
	go test -race ./...
	npm run check
	npm run build
integration:
	sh scripts/check-integration.sh
build:
	npm run build
	mkdir -p bin
	go build -trimpath -o bin/keel-api ./cmd/keel-api
	go build -trimpath -o bin/keel-admin ./cmd/keel-admin
	go build -trimpath -o bin/keel-worker ./cmd/keel-worker
	go build -trimpath -o bin/keel-sim ./cmd/keel-sim
	go build -trimpath -o bin/keel-ssh-runner ./cmd/keel-ssh-runner
	go build -trimpath -ldflags="-X main.version=$(APP_VERSION)" -o bin/keel-agent ./cmd/keel-agent
build-agent:
	mkdir -p bin/linux-amd64 bin/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=$(APP_VERSION)" -o bin/linux-amd64/keel-agent ./cmd/keel-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.version=$(APP_VERSION)" -o bin/linux-arm64/keel-agent ./cmd/keel-agent
build-preflight:
	mkdir -p bin/linux-amd64 bin/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/linux-amd64/keel-preflight ./cmd/keel-preflight
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/linux-arm64/keel-preflight ./cmd/keel-preflight
build-installer-helpers: build-preflight
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/linux-amd64/keel-ssh-runner ./cmd/keel-ssh-runner
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/linux-arm64/keel-ssh-runner ./cmd/keel-ssh-runner
generate-agent-proto:
	test "$$(protoc --version)" = 'libprotoc 3.21.12'
	test "$$(protoc-gen-go --version)" = 'protoc-gen-go v1.36.12'
	test "$$(protoc-gen-go-grpc --version)" = 'protoc-gen-go-grpc 1.5.1'
	protoc --go_out=. --go_opt=module=github.com/enzoOvOyu/keel --go-grpc_out=. --go-grpc_opt=module=github.com/enzoOvOyu/keel api/agent/v1/agent.proto
dev-api:
	sh scripts/dev-api.sh
dev-worker:
	sh scripts/dev-worker.sh
dev-web:
	npm run dev:web
up:
	docker compose --profile data --profile execution up -d --build --wait
logs:
	docker compose logs --tail=100 api
