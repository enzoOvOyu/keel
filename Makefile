SHELL := /bin/sh
.PHONY: check build dev-api dev-web up logs
check:
	python3 scripts/check-design.py
	go vet ./...
	go test -race ./...
	npm run check
	npm run build
build:
	npm run build
	mkdir -p bin
	go build -trimpath -o bin/keel-api ./cmd/keel-api
	go build -trimpath -o bin/keel-admin ./cmd/keel-admin
dev-api:
	sh scripts/dev-api.sh
dev-web:
	npm run dev:web
up:
	docker compose --profile data up -d --build --wait
logs:
	docker compose logs --tail=100 api
