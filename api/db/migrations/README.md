# Database migrations

PostgreSQL schema changes are append-only Goose migrations. Run them with `mhedctl migrate up`; do not edit
an applied migration in place.
