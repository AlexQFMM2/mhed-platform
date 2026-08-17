.PHONY: build dev down logs test check compose-config

build:
	pnpm build

dev:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f --tail=200

test:
	docker run --rm -v "$(CURDIR)/api:/src" -w /src golang:1.24-alpine go test ./...
	pnpm check

check:
	pnpm check

compose-config:
	docker compose config --quiet
