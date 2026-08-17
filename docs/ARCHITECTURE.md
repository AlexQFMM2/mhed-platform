# Architecture

## Service boundary

MHED is one deployable Compose project with three long-running containers:

```text
Host Nginx / HTTPS
├── Web and Admin → mhed-front
└── API and Desktop → mhed-api → PostgreSQL
                              └→ versioned read-only game data
```

The API and desktop hostnames reach the same Go process. Routes, authentication policies and rate limits are
separated by API groups rather than duplicated services.

## Data boundary

PostgreSQL owns mutable online state: accounts, verification challenges, sessions, published loadouts, likes,
moderation state and audit logs. SQLite is reserved for immutable game reference data and is never the online
account database.

Published loadouts retain their canonical JSONB payload and relational indexes derived by the matching game
adapter. Clients cannot provide authoritative skill summaries or like counts.

## API boundary

OpenAPI in `contracts/openapi.yaml` is the contract source. Public browser, admin and desktop endpoints use
REST JSON under `/v1`. Desktop outages must not disable local save editing or local loadout files.

Complete save files, character names, raw equipment bytes and platform offsets are never accepted by the API.
