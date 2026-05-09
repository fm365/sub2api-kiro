# Maintaining Sub2API Kiro

This repository is maintained as an independent product fork of `Wei-Shaw/sub2api`.

## Product Direction

The fork prioritizes:

- Kiro account support for Claude Code style clients.
- Stable Docker/server deployment.
- Multi-platform account scheduling, grouping, usage, and billing.
- Practical admin UI workflows for self-hosted operators.

Upstream compatibility is useful, but not the primary product constraint.

## Branches

Recommended branch policy:

- `main`: stable release branch.
- `dev`: integration branch for day-to-day work.
- `upstream-sync/*`: temporary branches for cherry-picking upstream fixes.
- `feature/*`: scoped feature work.

## Upstream Sync

Add upstream once:

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git fetch upstream
```

Prefer cherry-picking focused upstream fixes:

```bash
git cherry-pick <commit>
```

Good candidates:

- Security fixes.
- Provider compatibility fixes.
- Scheduler and billing correctness fixes.
- Build and release fixes.

Avoid blind upstream merges when they introduce broad product changes or conflict with Kiro workflows.

## Docker Images

Default public image:

```bash
ghcr.io/xiangking/sub2api-kiro:latest
```

Use versioned tags for production:

```bash
ghcr.io/xiangking/sub2api-kiro:vX.Y.Z
```

For local testing:

```bash
docker build -t sub2api:kiro-dev .
```

## Release Checklist

1. Run backend checks:

   ```bash
   cd backend
   go test ./internal/pkg/kiro ./internal/handler/admin ./internal/server/routes ./cmd/server
   ```

2. Run frontend checks:

   ```bash
   cd frontend
   pnpm install
   pnpm run typecheck
   ```

3. Build Docker image on a low-memory host when changing frontend/build tooling:

   ```bash
   docker build -t sub2api:release-test .
   ```

4. Tag and publish through GitHub Actions:

   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```

## Deployment Notes

For remote Docker deployments, prefer Kiro device authorization.

Kiro Social Auth auto-callback should only be enabled when port `49153` is intentionally published or tunneled:

```env
KIRO_SOCIAL_CALLBACK_LISTEN_HOST=0.0.0.0
KIRO_SOCIAL_CALLBACK_BIND_HOST=127.0.0.1
```

Otherwise keep those settings blank and use manual callback/code paste when needed.
