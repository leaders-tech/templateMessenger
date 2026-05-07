# tlfpaas Messenger Stack Example

This is a copyable demo project for tlfpaas autodeploy.

It contains:

- React frontend;
- Go backend;
- WebSocket updates served by the same public backend service;
- NATS JetStream for live message events;
- Redis for a small latest-message cache;
- Postgres for durable message storage.

The project is intentionally small, but it exercises the important tlfpaas paths:
multi-service Compose, private dependencies, named volumes, runtime secrets,
frontend/backend routing, WebSockets, logs and metrics.

## tlfpaas Deploy Contract

The main [`docker-compose.yml`](./docker-compose.yml) is platform-safe:

- no `ports`;
- no Compose `user`;
- no `privileged`, `network_mode`, `cap_add`, `devices`;
- no Compose `secrets` or `configs`;
- no external networks or volumes;
- no raw `build.args`;
- only `frontend` and `backend` have `tlfpaas.route` labels.
- every service image has an explicit non-root final `USER`, including the
  Postgres, Redis and NATS wrapper images under `infra/`.

Public routing:

```text
/*    -> frontend:8080
/api* -> backend:8081
/ws*  -> backend:8081
```

Private service names:

```text
postgres:5432
redis:6379
nats:4222
```

## Required tlfpaas Secrets

Add these in the tlfpaas Secrets UI before the first deploy:

| Key | Example value | Notes |
| --- | --- | --- |
| `APP_SECRET` | long random string | Required. The backend refuses production startup without it. |
| `POSTGRES_PASSWORD` | long random string | Required by Postgres and the backend. |

After adding or changing secrets, click **Redeploy now**.

Do not put these values in `.docker.env`.

## Copy To A New Repository

From the tlfpaas repository:

```bash
cp -R examples/messenger-stack /tmp/messenger-demo
cd /tmp/messenger-demo
git init
git add .
git commit -m "Initial messenger stack"
git remote add origin git@github.com:leaders-tech/<repo-name>.git
git push -u origin main
```

For a personal project, `<repo-name>` may be just the project name, for example:

```text
messenger-demo
```

For a group project, use:

```text
<group-slug>-messenger-demo
```

## Local Docker Run

Create local-only secrets:

```bash
cp .local.secrets.env.example .local.secrets.env
```

Start the stack:

```bash
docker compose \
  --env-file .docker.env \
  --env-file .local.secrets.env \
  -f docker-compose.yml \
  -f docker-compose.local.yml \
  up --build
```

Open:

```text
http://localhost:5100
```

Backend health:

```bash
curl -fsS http://localhost:5101/api/health
```

Post one message:

```bash
curl -fsS http://localhost:5101/api/messages \
  -H 'content-type: application/json' \
  -d '{"author":"Local Tester","text":"hello from curl"}'
```

The message should appear in the frontend without refreshing the page.

## Smoke Checklist After tlfpaas Deploy

1. Open the public project URL.
2. Send a message from the browser.
3. Open a second browser tab and confirm the message appears live.
4. Check `/api/health`:

   ```text
   https://<public-host>/api/health
   ```

5. Check Dozzle for `frontend`, `backend`, `postgres`, `redis`, and `nats` containers.
6. Check Grafana for CPU, memory and HTTP request metrics.

## Non-Secret Configuration

`.docker.env` contains only non-secret values.

Frontend build-time variables are intentionally limited to `VITE_*`, because
tlfpaas only forwards client-visible build args with public prefixes.

Runtime secrets must be configured through tlfpaas.
