# Sub2API Kiro

面向 Claude Code 类客户端、Kiro 账号和多平台账号调度的 AI API 网关。

[English](README.md)

> 本项目是基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的独立维护分支。项目继续保留原始 LGPL-3.0-or-later 许可证和来源署名，但产品方向会围绕 Kiro、Claude Code 类客户端和自部署网关场景独立演进。

## 项目定位

Sub2API Kiro 是一个自部署 AI API 网关，用于把多个上游订阅账号的额度统一分发给用户和客户端。它保留了 Sub2API 原有的账号调度、分组、计费、管理后台、OpenAI/Anthropic 兼容接口，并加入了完整的 Kiro 平台支持。

这个版本更适合我们自己的生产场景：Kiro 账号需要和 Anthropic、OpenAI、Gemini、Antigravity 等账号一起被统一管理、调度和计费。

## 核心能力

- **Kiro 平台适配**：Kiro 授权、token 刷新、请求转发、模型映射、用量/额度窗口展示。
- **适配 Claude Code 类客户端**：Kiro 账号可通过 Claude Messages 风格接口提供给 Coding Agent 客户端使用。
- **多平台网关**：Anthropic、OpenAI、Gemini、Antigravity、Kiro 账号统一分组、调度、测试和监控。
- **账号调度**：粘性会话、模型路由、并发限制、RPM 限速、临时冷却等能力。
- **计费与管理 UI**：API Key 分发、用户/分组管理、用量日志、支付集成、后台看板。
- **Docker 优先部署**：Compose 配置包含 Kiro 服务器部署说明和可选 Social Auth 回调配置。

## Kiro 使用说明

Kiro 不是普通“复制 API Key”即可使用的平台。它主要依赖可刷新的 OAuth 类凭据。

当前支持的 Kiro 账号接入方式：

- 设备授权，推荐用于服务器和 Docker 部署。
- Google/GitHub/Cognito Social Auth，并支持手动粘贴 callback/code。
- 导入服务端可访问的已有 Kiro token。

远程 Docker 服务器上推荐使用设备授权。Kiro Social Auth 的默认回调地址是 `http://127.0.0.1:49153/oauth/callback`；在远程部署时，浏览器里的 `127.0.0.1` 指的是用户本机，不是服务器容器。可以使用手动粘贴回调、SSH 隧道，或在明确理解风险后映射 `49153` 端口。

## 支持平台

| 平台 | 典型用途 |
|------|----------|
| Anthropic | Claude API / Claude Code 兼容路由 |
| OpenAI | OpenAI 兼容接口、Responses、Chat Completions |
| Gemini | Gemini / Code Assist 账号调度 |
| Antigravity | 通过 Antigravity 账号访问 Claude / Gemini |
| Kiro | 通过 Kiro 凭据为 Claude Code 类客户端提供访问 |

具体可用模型取决于管理员配置的上游账号和模型映射。

## Docker 快速开始

本分支推荐镜像：

```bash
ghcr.io/xiangking/sub2api-kiro:latest
```

Compose 部署：

```bash
mkdir -p sub2api-kiro-deploy && cd sub2api-kiro-deploy
curl -sSL https://raw.githubusercontent.com/xiangking/sub2api-kiro/main/deploy/docker-deploy.sh | bash
docker compose up -d
docker compose logs -f sub2api
```

手动克隆：

```bash
git clone https://github.com/xiangking/sub2api-kiro.git
cd sub2api-kiro/deploy
cp .env.example .env
docker compose -f docker-compose.local.yml up -d
```

本机 Docker 如需 Kiro Social Auth 自动回调，可以额外开放 `49153` 并设置：

```env
KIRO_SOCIAL_CALLBACK_LISTEN_HOST=0.0.0.0
KIRO_SOCIAL_CALLBACK_BIND_HOST=127.0.0.1
```

普通远程服务器部署建议保持为空，使用设备授权。

## 开发

后端：

```bash
cd backend
go test ./internal/pkg/kiro ./internal/handler/admin ./internal/server/routes ./cmd/server
go run ./cmd/server
```

前端：

```bash
cd frontend
pnpm install
pnpm run typecheck
pnpm run dev
```

Docker 构建：

```bash
docker build -t sub2api:kiro-dev .
```

## 上游同步策略

本项目会作为独立产品维护，但仍可选择性同步 `Wei-Shaw/sub2api` 的重要修复，尤其是安全、调度、计费和供应商兼容相关变更。

建议流程：

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git fetch upstream
git cherry-pick <upstream-fix-commit>
```

不要盲目合并上游大版本，避免破坏 Kiro 方向的稳定性。

## 来源说明

本项目派生自 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)。感谢原作者和社区贡献者提供的基础工程。

当前 Go module path 仍保留为 `github.com/Wei-Shaw/sub2api`，目的是避免大规模 import path 改名带来的风险。公开仓库、Docker 镜像和文档由 `xiangking/sub2api-kiro` 独立维护。

## 免责声明

本项目仅用于技术学习、研究和自部署网关实验。使用订阅账号网关可能违反上游服务条款。账号风险、额度使用、计费行为和合规责任由使用者自行承担。

## 许可证

本项目基于 [GNU 宽通用公共许可证 v3.0](LICENSE) 或更高版本授权。

原始版权声明：

Copyright (c) 2026 Wesley Liddick
