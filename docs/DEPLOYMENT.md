# Deployment

## Test server

The initial target is an existing 2 vCPU / 2 GiB Ubuntu host. Docker services expose only loopback ports;
the host Nginx terminates HTTPS and forwards the four MHED hostnames.

Copy `.env.example` to a root-readable deployment environment file, replace the PostgreSQL password, then
start the Compose project. Never expose port 5432 in the cloud firewall or host port mappings.

The initial hard limits are:

| Service | CPU | Memory |
| --- | ---: | ---: |
| API | 0.50 | 256 MiB |
| PostgreSQL | 0.35 | 384 MiB |
| Frontend | 0.10 | 64 MiB |

The host currently uses Nginx and Certbot. Render `deploy/nginx/mhed.conf.template` with complete hostnames,
add certificate directives through Certbot, validate with `nginx -t`, and reload only after validation.

## Operations

- Keep PostgreSQL on the private Compose network.
- Store JSON logs on stdout and enable Docker log rotation at the host level.
- Run encrypted daily `pg_dump` backups with off-host retention before accepting real accounts.
- Treat game-data artifacts as immutable and verify their manifest and file hashes during image build.
- Increase Compose limits after the host upgrade; do not change database engines during scaling.
