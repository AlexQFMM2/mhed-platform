# ADR 0001: Platform stack

Status: accepted

MHED uses a Go REST API, PostgreSQL for mutable online state, versioned read-only SQLite for game reference
data, Astro for the public site, React/Vite/Ant Design for administration, and Docker Compose behind the host
Nginx.

SQLite was rejected for accounts, sessions, publishing and likes because those paths contain concurrent
writes and relational constraints. Redis, message brokers, GraphQL, SSR and separate API processes per
hostname are deferred until measured load requires them.

Authorization uses PostgreSQL-backed roles and permissions. Users have no default role; resource ownership
provides ordinary user capabilities. `super_admin` is the only initial administrative role and the only role
allowed to access the Admin application.
