# Sub2API Kiro

AI API gateway for Claude Code style clients, Kiro accounts, and multi-platform account scheduling.

[中文](README_CN.md)

> This project is an independently maintained fork of [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api). It keeps the original LGPL-3.0-or-later license and attribution, while adding a Kiro-focused product direction.

## Overview

Sub2API Kiro is a self-hosted AI API gateway for distributing subscription quotas across teams and clients. It keeps Sub2API's account scheduling, grouping, billing, admin UI, and OpenAI/Anthropic-compatible endpoints, and adds first-class Kiro support for Claude Code style workflows.

The project is maintained for practical self-hosted deployments where Kiro accounts need to work alongside Anthropic, OpenAI, Gemini, and Antigravity accounts under one gateway.

## Highlights

- **Kiro platform support**: Kiro account authorization, token refresh, request forwarding, model mapping, and usage/quota display.
- **Claude Code friendly**: Kiro accounts can be exposed through Claude Messages style endpoints for coding-agent clients.
- **Multi-platform gateway**: Anthropic, OpenAI, Gemini, Antigravity, and Kiro accounts can be grouped, scheduled, tested, and monitored.
- **Account scheduling**: Sticky sessions, model routing, concurrency control, rate limits, and temporary cooldown logic.
- **Billing and management UI**: API key distribution, user/group management, usage logs, payment integration, and admin dashboards.
- **Docker-first deployment**: Compose files include Kiro server-deployment notes and optional Social Auth callback settings.

## Kiro Notes

Kiro does not behave like a simple copy-paste API key provider. It normally uses OAuth-style credentials that must be refreshed.

Supported Kiro account flows:

- Device authorization for server/Docker deployments.
- Social Auth with Google/GitHub/Cognito and manual callback/code paste fallback.
- Existing token import when credentials are already available to the server.

For remote Docker servers, device authorization is recommended. Kiro Social Auth uses the callback URL `http://127.0.0.1:49153/oauth/callback`; on a remote server, the browser's `127.0.0.1` is the user's machine, not the container. Use manual callback paste, SSH tunneling, or explicitly publish port `49153` only when you understand that tradeoff.

## Supported Platforms

| Platform | Typical Use |
|----------|-------------|
| Anthropic | Claude API and Claude Code compatible routing |
| OpenAI | OpenAI-compatible APIs, Responses, Chat Completions |
| Gemini | Gemini / Code Assist account routing |
| Antigravity | Claude / Gemini through Antigravity accounts |
| Kiro | Claude Code style access through Kiro credentials |

Available models depend on each upstream account and the model mappings configured by the admin.

## Docker Quick Start

The recommended image for this fork is:

```bash
ghcr.io/xiangking/sub2api-kiro:latest
```

Compose deployment:

```bash
mkdir -p sub2api-kiro-deploy && cd sub2api-kiro-deploy
curl -sSL https://raw.githubusercontent.com/xiangking/sub2api-kiro/main/deploy/docker-deploy.sh | bash
docker compose up -d
docker compose logs -f sub2api
```

Manual clone:

```bash
git clone https://github.com/xiangking/sub2api-kiro.git
cd sub2api-kiro/deploy
cp .env.example .env
docker compose -f docker-compose.local.yml up -d
```

For Kiro Social Auth auto-callback in local Docker, optionally publish port `49153` and set:

```env
KIRO_SOCIAL_CALLBACK_LISTEN_HOST=0.0.0.0
KIRO_SOCIAL_CALLBACK_BIND_HOST=127.0.0.1
```

Leave these blank for normal remote-server device authorization.

## Development

Backend:

```bash
cd backend
go test ./internal/pkg/kiro ./internal/handler/admin ./internal/server/routes ./cmd/server
go run ./cmd/server
```

Frontend:

```bash
cd frontend
pnpm install
pnpm run typecheck
pnpm run dev
```

Docker build:

```bash
docker build -t sub2api:kiro-dev .
```

## Upstream Sync Policy

This fork is product-independent, but it should still selectively track useful upstream fixes from `Wei-Shaw/sub2api`, especially security, scheduler, billing, and provider compatibility fixes.

Suggested workflow:

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git fetch upstream
git cherry-pick <upstream-fix-commit>
```

Avoid blind merges when they conflict with the Kiro-specific product direction.

## Attribution

This project is derived from [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api). Thanks to the original author and contributors for the foundation.

The Go module path currently remains `github.com/Wei-Shaw/sub2api` to avoid a high-risk import-path churn. Public repository, Docker image, and documentation are maintained under `xiangking/sub2api-kiro`.

## Disclaimer

This project is for technical learning, research, and self-hosted gateway experimentation. Using subscription-account gateways may violate terms of service of upstream providers. You are responsible for account risk, quota usage, billing behavior, and compliance with each provider's terms.

## License

Licensed under the [GNU Lesser General Public License v3.0](LICENSE) or later.

Original copyright:

Copyright (c) 2026 Wesley Liddick
