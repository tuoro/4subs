# 4subs

Go + PrimeVue subtitle manager scaffold inspired by ChineseSubFinder.

## Current Scope (MVP Scaffold)

- Go backend with SQLite persistence
- PrimeVue frontend (dashboard + settings)
- Provider management for ASSRT + OpenSubtitles.com
- Language priority settings (default `bilingual > zh-cn > zh-tw`)
- No auto replace by default (manual intervention only)
- Job endpoint + SSE event stream
- Docker-first deployment (`Dockerfile` + `docker-compose.yml`)

## Quick Start (Docker)

1. Copy env template:

```bash
cp .env.example .env
```

2. Edit `.env` with your tokens/keys.

3. Start:

```bash
docker compose up -d --build
```

4. Open:

- UI: `http://localhost:8080`
- Health: `http://localhost:8080/api/v1/health`

## Volumes

- `./deploy/config -> /app/config`
- `./deploy/data -> /app/data`
- `./deploy/subtitles -> /app/subtitles`
- `${MEDIA_HOST_PATH} -> /media` (readonly)

## Local Dev

### Backend

```bash
go run ./cmd/server
```

### Frontend

```bash
cd web
npm install
npm run dev
```

Vite proxies `/api` to `http://localhost:8080`.

## API (Implemented)

- `GET /api/v1/health`
- `GET /api/v1/settings`
- `PUT /api/v1/settings`
- `GET /api/v1/providers`
- `PUT /api/v1/providers/{name}/credential` (`assrt` or `opensubtitles`)
- `GET /api/v1/jobs`
- `POST /api/v1/scan`
- `GET /api/v1/events` (SSE)

## Notes

- OpenSubtitles integration target is `.com` only.
- If `APP_SECRET` is empty, credentials are stored in base64 plain mode (`plain:` prefix). Set `APP_SECRET` in production.
- Scanner/matcher/provider download flows are still scaffold-level and will be implemented in subsequent iterations.
