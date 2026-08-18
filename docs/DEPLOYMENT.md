# Deployment

## Production server

The target is an existing 4 vCPU / 4 GB Ubuntu host. Docker services expose only loopback ports;
the host Nginx terminates HTTPS and forwards the four MHED hostnames.

Keep the deployment in `/opt/mhed-platform` as the single host-side project directory. Copy `.env.example`
to a mode `0600` environment file and replace the PostgreSQL password, report HMAC key and 32-byte Base64
email encryption key. Application,
database, game data and backups stay in the `mhed` Compose project and Docker named volumes; do not install
Go, Node.js or PostgreSQL on the host. Never expose port 5432 in the cloud firewall or host port mappings.

Import the verified runtime database into its named volume before starting the API:

```bash
docker compose --profile ops run --rm game-data-import
docker compose up -d
```

After import, remove the temporary `game-data/runtime` host files. The API mounts the resulting
`mhed-game-data` volume read-only. The `mhed-backup` container writes daily custom-format dumps to the
`mhed-backups` volume and retains seven days.

The balanced hard limits are:

| Service | CPU | Memory |
| --- | ---: | ---: |
| API | 1.25 | 640 MiB |
| PostgreSQL | 0.75 | 640 MiB |
| Frontend | 0.20 | 128 MiB |
| Backup | 0.10 | 96 MiB |
| Migration (one-shot) | 0.30 | 192 MiB |
| Game data import (one-shot) | 0.20 | 96 MiB |

The host currently uses Nginx and Certbot. Render `deploy/nginx/mhed.conf.template` for
`mhed.web.65h26i.top`, `mhed.api.65h26i.top`, `mhed.admin.65h26i.top` and `mhed.desk.65h26i.top`, request one
SAN certificate named `mhed-platform`, validate with `nginx -t`, and reload only after validation. The site
template also applies separate login and general API rate limits.

## Operations

- Keep PostgreSQL on the private Compose network.
- Store JSON logs on stdout; Compose retains three 10 MiB files per container.
- Keep the backup volume private and add encrypted off-host retention before public registration is enabled.
- Treat game-data artifacts as immutable and verify their manifest and file hashes during image build.
- Increase Compose limits only after measuring sustained load; do not change database engines during scaling.
