# Development

## Requirements

- Docker Compose
- Node.js 24 and pnpm 11 for frontend-only development
- Go 1.24 for API development, or the provided Go Docker image for tests

## Commands

```bash
cp .env.example .env
pnpm install
pnpm check
pnpm build
make test
docker compose up --build
```

Do not commit `.env`, generated credentials, database volumes or production game-data files. Database changes
must use append-only migrations. Update OpenAPI in the same change as an externally visible handler.

The account and loadout schema is intentionally absent from the initial scaffold. Add it only after the v1
authentication and `MH_LOADOUT` contracts are reviewed.
