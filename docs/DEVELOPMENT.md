# Development

## Requirements

- Docker Compose
- Node.js 24 and pnpm 11 for frontend-only development
- Go 1.24 for API development, or the provided Go Docker image for tests

## Commands

```bash
cp .env.example .env
cd api
go run ./cmd/mhedctl import-game-data \
  --source ../../mh3u-se/data/mh3g.sqlite \
  --manifest ../../mh3u-se/data/manifest.json \
  --destination ../game-data/runtime
cd ..
pnpm install
pnpm check
pnpm build
make test
docker compose up --build
```

Do not commit `.env`, generated credentials, database volumes or production game-data files. Database changes
must use append-only migrations. Update OpenAPI in the same change as an externally visible handler.

The initial account, RBAC, session, loadout, report, and audit schema is applied by the one-shot Compose
`migrate` service. Applied Goose migrations are immutable. Runtime game data and generated credentials remain
outside Git.
