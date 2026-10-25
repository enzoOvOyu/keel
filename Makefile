SHELL := /bin/sh
.PHONY: check build integration dev-api dev-web dev-worker up logs
check:
	python3 scripts/check-design.py
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
