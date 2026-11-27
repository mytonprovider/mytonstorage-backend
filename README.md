# mytonstorage-backend

**[Russian version](README.ru.md)**

Backend for [mytonstorage.org](https://mytonstorage.org).

## Description

Backend for uploading and managing files on TON Storage:
- File uploads to TON Storage
- Storage contract lifecycle (init, top-up, withdrawal, provider updates)
- TON Connect authentication
- Monitors storage contracts and notifies providers about new bags
- REST API for the frontend
- Prometheus metrics

## Local development (Docker)

**Requires:** [Docker](https://docs.docker.com/), [Task](https://taskfile.dev), Go 1.26+ (for local builds outside containers).

Container stack: **PostgreSQL**, **tonutils-storage** (v1.5.1), **backend**.

```bash
task deploy:up       # creates deploy/.env on first run
task deploy:health
task deploy:logs
task deploy:down
task deploy:reset    # stop and remove volumes
```

API: `http://localhost:9092` (`BACKEND_PORT` in `deploy/.env`).

Copy `deploy/.env.example` to `deploy/.env`, or run `task deploy:init`.  
`DB_USER` must be **pguser** (see `db/init.sql`).

### Frontend

[mytonstorage-frontend](https://github.com/mytonprovider/mytonstorage-frontend): its dev server proxies `/api`, point it at this backend with `BACKEND_PROXY_TARGET=http://localhost:9092`.
Setup and TON Connect notes are in its README.

## VPS deploy

The image is published by the `publish` job in `CI` after checks on every push to `master`: `ghcr.io/mytonprovider/mytonstorage-backend:<tag>`, the tag is the first 7 characters of the commit. Manual `task image:build:push` stays as a fallback.

The tonutils-storage image is not built here: `TONUTILS_STORAGE_IMAGE=ghcr.io/mytonprovider/tonutils-storage:v1.5.1`.

For a release backend image leave `BUILD_TAGS` unset (or empty).

On the VPS (`deploy/docker-compose.hub.yml`, `deploy/.env.hub`, `deploy/secrets/agents-ca.crt`):

```bash
task hub:init
nano deploy/.env.hub
task hub:up
task hub:health
```

Set `BACKEND_IMAGE`, `TONUTILS_STORAGE_IMAGE`, `SYSTEM_HOST=mytonstorage.org`, `AGENT_*`, and `TONUTILS_STORAGE_EXTERNAL_IP` in `.env.hub`.

Frontend is deployed separately: [mytonstorage-frontend](https://github.com/mytonprovider/mytonstorage-frontend).

More detail: [deploy/README.md](deploy/README.md).

## Local build (no Docker)

```bash
task build
task test:build
```

For the full flow use `task deploy:up`.

## Project layout

```
├── cmd/
├── pkg/
├── db/
├── deploy/
├── Dockerfile
└── Taskfile.yml
```

## License

Apache-2.0

Created for the TON Foundation community.
