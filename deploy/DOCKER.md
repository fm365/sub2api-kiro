# Sub2API Kiro Docker Image

Self-hosted AI API gateway with first-class Kiro support for Claude Code style clients.

## Image

```bash
ghcr.io/xiangking/sub2api-kiro:latest
```

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/sub2api" \
  -e REDIS_URL="redis://host:6379" \
  ghcr.io/xiangking/sub2api-kiro:latest
```

## Docker Compose

```yaml
services:
  sub2api:
    image: ghcr.io/xiangking/sub2api-kiro:latest
    ports:
      - "8080:8080"
      # Optional for local Kiro Social Auth auto-callback.
      # - "127.0.0.1:49153:49153"
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/sub2api?sslmode=disable
      - REDIS_URL=redis://redis:6379
      # Set to 0.0.0.0 only when publishing port 49153 above.
      - KIRO_SOCIAL_CALLBACK_LISTEN_HOST=
    depends_on:
      - db
      - redis

  db:
    image: postgres:18-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:8-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Kiro Auth Notes

For remote Docker deployments, use Kiro device authorization in the admin UI. It does not require exposing an extra callback port.

Kiro Social Auth uses `http://127.0.0.1:49153/oauth/callback`. For local Docker deployments, publish port `49153` and set `KIRO_SOCIAL_CALLBACK_LISTEN_HOST=0.0.0.0` to let Sub2API capture the callback. For remote servers, prefer device authorization or manually paste the final callback URL/code back into the UI.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `SERVER_PORT` | Server port | No | `8080` |
| `RUN_MODE` | Runtime mode | No | `standard` |
| `KIRO_SOCIAL_CALLBACK_LISTEN_HOST` | Bind host for Kiro Social Auth callback listener. Use `0.0.0.0` only when publishing port `49153`. | No | blank |

## Links

- [GitHub Repository](https://github.com/xiangking/sub2api-kiro)
- [Docker Compose Guide](https://github.com/xiangking/sub2api-kiro/blob/main/deploy/README.md)
