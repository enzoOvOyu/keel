SHELL := /bin/sh
APP_VERSION ?= development
.PHONY: check build build-agent build-preflight build-firstboot build-installer-helpers build-initrd generate-agent-proto integration dev-api dev-web dev-worker up logs
check:
	python3 scripts/check-design.py
	python3 -m unittest discover -s scripts -p 'test_p1_evidence.py'
	python3 -m unittest discover -s poc/h1 -p 'test_*.py'
	python3 -m unittest discover -s poc/bom-h15 -p 'test_*.py'
	python3 -m unittest discover -s poc/ztp-h16 -p 'test_*.py'
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
	go build -trimpath -o bin/keel-provision ./cmd/keel-provision
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
# First-boot helper is an input to the separately approved target rootfs.
# Build binaries before approving that rootfs/UKI, then approve the installer
# runtime. These targets do not create an OS image, rootfs archive or initramfs.
build-firstboot:
	mkdir -p bin/linux-amd64 bin/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/linux-amd64/keel-firstboot ./cmd/keel-firstboot
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/linux-arm64/keel-firstboot ./cmd/keel-firstboot
build-installer-helpers: build-preflight build-firstboot
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/linux-amd64/keel-ssh-runner ./cmd/keel-ssh-runner
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/linux-arm64/keel-ssh-runner ./cmd/keel-ssh-runner
build-initrd: build-installer-helpers
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/linux-amd64/keel-initrd ./cmd/keel-initrd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/linux-arm64/keel-initrd ./cmd/keel-initrd
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
