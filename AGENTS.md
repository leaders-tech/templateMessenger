## Назначение

`messenger-stack` — демонстрационный проект для smoke-теста tlfpaas: React frontend, Go backend, NATS JetStream, Redis и Postgres.

## Инварианты

- В `docker-compose.yml` публичными остаются только `frontend` (`tlfpaas.route=frontend`) и `backend` (`tlfpaas.route=backend`).
- `nats`, `redis` и `postgres` остаются private-only: без `tlfpaas.route`, без `ports`, обычно без `expose`.
- Runtime secrets (`APP_SECRET`, `POSTGRES_PASSWORD`) не добавлять в `.docker.env` и не коммитить.
- Frontend build-time переменные должны иметь только префикс `VITE_`.
- Docker final stages должны запускаться от non-root пользователя.

