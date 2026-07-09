# ModelBridge 项目迁移交接文档

> 面向新的 Codex 会话 / 新本地目录 `/Users/qc/projects/model-bridge` 的完整移交说明。
>
> 目标：新建一个独立项目 **ModelBridge**，以最上游 `Wei-Shaw/sub2api` 最新代码为基础，迁移 `fm365/sub2api-kiro` 中的 Kiro 能力，并完成品牌重塑。

**最后更新**: 2026-07-09  
**源项目**: `github.com/fm365/sub2api-kiro`  
**目标项目**: `github.com/fm365/model-bridge`  
**建议新 Codex 首先阅读**: 本文档 + `docs/STRATEGY_B_IMPLEMENTATION_PLAN.md` + `docs/UPSTREAM_SYNC_STRATEGY.md`

---

## 1. 新项目命名规范

| 项 | 值 |
|----|----|
| Product | `ModelBridge` |
| Repo | `model-bridge` |
| GitHub | `github.com/fm365/model-bridge` |
| Go module | `github.com/fm365/model-bridge` |
| Docker image | `fm365/model-bridge` |
| Binary | `model-bridge` |
| Service | `model-bridge` |
| 本地目录 | `/Users/qc/projects/model-bridge` |

### 1.1 命名替换原则

新项目必须作为独立产品维护，不能长期保留 `sub2api` 作为对外名称。

推荐替换：

| 原名称 | 新名称 | 说明 |
|--------|--------|------|
| `sub2api` | `model-bridge` | repo、binary、service、docker compose 服务名 |
| `Sub2API` | `ModelBridge` | README、前端标题、服务展示名 |
| `github.com/Wei-Shaw/sub2api` | `github.com/fm365/model-bridge` | Go module 与所有内部 import |
| `fm365/sub2api-kiro` | `fm365/model-bridge` | 文档/README 中的新项目仓库 |

保留说明：
- 许可证、致谢、上游来源说明中可以保留 `Wei-Shaw/sub2api`，但应明确标注 ModelBridge 基于该项目演进。
- Kiro 历史分析文档中出现 `sub2api-kiro` 可保留作为历史背景。

---

## 2. 新仓库初始化推荐流程

在你的本地 macOS：

```bash
mkdir -p /Users/qc/projects
cd /Users/qc/projects

# 方式 A：如果 GitHub 新仓库已创建，直接 clone upstream 后改 remote
# 或者先 clone Wei-Shaw/sub2api 为基础

git clone https://github.com/Wei-Shaw/sub2api.git model-bridge
cd model-bridge

git remote rename origin upstream
git remote add origin https://github.com/fm365/model-bridge.git

git checkout -b main upstream/main
```

如果 `github.com/fm365/model-bridge` 已经创建为空仓库：

```bash
git push -u origin main
```

> 如果仓库已有 README / LICENSE 初始化提交，请不要强推；先由 Codex 检查 `git log --oneline --graph --all -20`，再决定 rebase 或合并。

---

## 3. Rebranding 操作规范

### 3.1 Go module 重命名

在新项目根目录：

```bash
cd /Users/qc/projects/model-bridge/backend

go mod edit -module github.com/fm365/model-bridge
```

### 3.2 Go import 批量替换

macOS BSD sed 版本：

```bash
cd /Users/qc/projects/model-bridge

find backend -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -print0 \
  | xargs -0 sed -i '' 's|github.com/Wei-Shaw/sub2api|github.com/fm365/model-bridge|g'
```

Linux/GNU sed 版本：

```bash
find backend -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -print0 \
  | xargs -0 sed -i 's|github.com/Wei-Shaw/sub2api|github.com/fm365/model-bridge|g'
```

### 3.3 产品名/服务名替换建议

谨慎批量替换，先 grep 再改：

```bash
grep -RIn "Sub2API\|sub2api\|sub2api-kiro" . \
  --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=coverage \
  | head -200
```

推荐优先处理：

| 路径 | 操作 |
|------|------|
| `backend/cmd/server/VERSION` | 保留版本或改为 ModelBridge 版本策略 |
| `Dockerfile` / `deploy/*.sh` / `deploy/*.yml` | binary/image/service 名改成 `model-bridge` |
| `deploy/sub2api.service` | 改名为 `deploy/model-bridge.service`，内容中的服务名同步替换 |
| `README*.md` | 标题改成 ModelBridge，并保留 upstream attribution |
| `frontend/package.json` | package name 视情况改为 `model-bridge-frontend` |
| 前端站点标题/默认 site_name | `ModelBridge` |

---

## 4. Kiro 文件迁移清单

从 `github.com/fm365/sub2api-kiro:main` 复制下列 18 个 Kiro 独立文件到新项目。

### 4.1 后端 Kiro 核心包

```text
backend/internal/pkg/kiro/client.go
backend/internal/pkg/kiro/client_test.go
backend/internal/pkg/kiro/constants.go
backend/internal/pkg/kiro/oauth.go
backend/internal/pkg/kiro/parse.go
backend/internal/pkg/kiro/parse_test.go
backend/internal/pkg/kiro/types.go
backend/internal/pkg/kiro/usage_test.go
```

### 4.2 后端 Service / Handler

```text
backend/internal/service/kiro_gateway.go
backend/internal/service/kiro_gateway_file_test.go
backend/internal/service/kiro_gateway_test.go
backend/internal/service/kiro_oauth_service.go
backend/internal/service/kiro_oauth_service_test.go
backend/internal/service/account_kiro_passthrough_test.go
backend/internal/handler/admin/kiro_oauth_handler.go
backend/internal/handler/admin/account_handler_kiro_models_test.go
```

### 4.3 前端 Kiro API / Composable

```text
frontend/src/api/admin/kiro.ts
frontend/src/composables/useKiroOAuth.ts
```

### 4.4 推荐复制方法

如果本地同时有 `/Users/qc/projects/sub2api-kiro` 与 `/Users/qc/projects/model-bridge`：

```bash
SRC=/Users/qc/projects/sub2api-kiro
DST=/Users/qc/projects/model-bridge

cd "$DST"

while read -r f; do
  mkdir -p "$(dirname "$f")"
  cp "$SRC/$f" "$f"
done <<'FILES'
backend/internal/handler/admin/account_handler_kiro_models_test.go
backend/internal/handler/admin/kiro_oauth_handler.go
backend/internal/pkg/kiro/client.go
backend/internal/pkg/kiro/client_test.go
backend/internal/pkg/kiro/constants.go
backend/internal/pkg/kiro/oauth.go
backend/internal/pkg/kiro/parse.go
backend/internal/pkg/kiro/parse_test.go
backend/internal/pkg/kiro/types.go
backend/internal/pkg/kiro/usage_test.go
backend/internal/service/account_kiro_passthrough_test.go
backend/internal/service/kiro_gateway.go
backend/internal/service/kiro_gateway_file_test.go
backend/internal/service/kiro_gateway_test.go
backend/internal/service/kiro_oauth_service.go
backend/internal/service/kiro_oauth_service_test.go
frontend/src/api/admin/kiro.ts
frontend/src/composables/useKiroOAuth.ts
FILES
```

复制后需要替换 Go import：

```bash
find backend -type f -name "*.go" -print0 \
  | xargs -0 sed -i '' 's|github.com/Wei-Shaw/sub2api|github.com/fm365/model-bridge|g'
```

---

## 5. Kiro 集成点清单

仅复制 18 个文件不能完成编译，还必须把 Kiro 接入上游最新架构。

### 5.1 最小必要集成点

| 文件 | 需要做什么 |
|------|------------|
| `backend/internal/domain/constants.go` | 加 `PlatformKiro = "kiro"` |
| `backend/internal/service/domain_constants.go` | 如上游 service 层也定义平台常量，需补齐 `PlatformKiro` |
| `backend/internal/service/account.go` | 补 Kiro model mapping / passthrough / webportal / strip-tools-on-fail 辅助方法 |
| `backend/internal/service/gateway_service.go` | 在主 Forward 路径加入 Kiro 分流到 `s.forwardKiro(...)` |
| `backend/internal/service/wire.go` | 注入 `NewKiroOAuthService` |
| `backend/internal/handler/handler.go` | `Admin` handler 结构中增加 `KiroOAuth` |
| `backend/internal/handler/wire.go` | 注入 `admin.NewKiroOAuthHandler` |
| `backend/internal/server/routes/admin.go` | 注册 `/admin/kiro/oauth/*`、`/admin/kiro/tokens/scan` 路由 |
| `backend/internal/server/routes/gateway.go` | 如 Kiro 有专门 gateway endpoint，注册之 |
| `backend/internal/handler/admin/account_handler.go` | 账号创建/更新/展示时支持 Kiro extra/credentials |
| `backend/internal/handler/admin/group_handler.go` | 如分组平台枚举限制需补 Kiro |
| `backend/internal/handler/endpoint.go` | 可用模型/endpoint 列表补 Kiro |

### 5.2 保持高内聚的关键设计

**不要把 Kiro 私有实现塞进 upstream 拆分后的 30+ gateway 子文件。**

应采用 Go 同 package 扩展方式：

```text
backend/internal/service/kiro_gateway.go
```

在该文件中继续声明：

```go
func (s *GatewayService) forwardKiro(...)
func (s *GatewayService) GetKiroAvailableModels(...)
```

主网关只做极小分流：

```go
if account != nil && account.Platform == PlatformKiro {
    return s.forwardKiro(ctx, c, account, parsed, startTime)
}
```

这样可以最大限度保留 upstream 的 gateway 解耦结构。

---

## 6. 核心不变量（必须遵守）

### 6.1 不破坏 upstream 解耦结构

Kiro 私有逻辑应集中在：

```text
backend/internal/pkg/kiro/*
backend/internal/service/kiro_gateway.go
backend/internal/service/kiro_oauth_service.go
backend/internal/handler/admin/kiro_oauth_handler.go
```

不应把大量 Kiro helper 分散到：

```text
gateway_billing_block.go
gateway_record_usage.go
gateway_request.go
thinking_protocol.go
...
```

### 6.2 `FilterThinkingBlocks` 规范

如果迁移中涉及 thinking blocks 过滤，必须使用：

```go
FilterThinkingBlocks(body, mappedModel)
FilterThinkingBlocksForRetry(body, mappedModel)
```

不能回退到旧签名。

### 6.3 错误响应后必须 MarkResponseCommitted

凡是 service/handler 写响应：

```go
c.JSON(...)
c.AbortWithStatusJSON(...)
c.Data(...)
```

如果后续还有统一错误处理链路，必须标记：

```go
MarkResponseCommitted(c)
```

避免 double-write。

### 6.4 Kiro 计费字段不能污染 Anthropic token 计费

`KiroCreditUsage` / `KiroCreditUnit` / `KiroContextUsagePercent` 只用于日志和参考，不能直接参与 Anthropic `input_tokens` / `output_tokens` 计费。

---

## 7. 推荐实施阶段

### Phase 0 — 新仓库初始化与重命名

交付：
- `go.mod` module = `github.com/fm365/model-bridge`
- Go import 全部替换
- Docker/binary/service 初步改名
- `go build ./...` 尽可能通过（未迁移 Kiro 前应通过）

### Phase 1 — Kiro 独立文件复制

交付：
- 18 个 Kiro 文件落地
- import 路径替换为 `github.com/fm365/model-bridge`
- 允许暂时编译失败，因为集成点未补齐

### Phase 2 — Kiro 后端最小编译闭环

交付：
- 平台常量补齐
- `Account` Kiro 辅助方法补齐
- `GatewayService.forwardKiro` 能编译
- `KiroOAuthService` / handler / wire / routes 能编译
- `go build ./...` 通过

### Phase 3 — 测试闭环

交付：

```bash
cd backend
GOCACHE=/tmp/model-bridge-gocache GOMODCACHE=/tmp/model-bridge-gomodcache \
  /usr/local/go/bin/go test -tags unit -count=1 -timeout 300s ./...
```

全部通过。

### Phase 4 — 前端闭环

交付：
- `pnpm install`
- `pnpm build`
- 前端 Kiro OAuth UI 可用

### Phase 5 — Docker / 服务闭环

交付：
- Docker image 名称 `fm365/model-bridge`
- binary `model-bridge`
- systemd service `model-bridge.service`
- docker-compose service/image 名称同步

---

## 8. 验证命令

### 8.1 Backend

```bash
cd /Users/qc/projects/model-bridge/backend

go mod tidy
go build ./...
go test -tags unit -count=1 -timeout 300s ./...
```

如果本地 Go 不在 PATH：

```bash
/usr/local/go/bin/go build ./...
/usr/local/go/bin/go test -tags unit -count=1 -timeout 300s ./...
```

### 8.2 Frontend

```bash
cd /Users/qc/projects/model-bridge/frontend
pnpm install
pnpm build
```

### 8.3 全局残留检查

```bash
cd /Users/qc/projects/model-bridge

grep -RIn "github.com/Wei-Shaw/sub2api" backend --exclude-dir=.git || true

grep -RIn "sub2api\|Sub2API\|sub2api-kiro" . \
  --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=coverage \
  | head -200
```

残留分两类处理：
- 产品/服务名残留：应替换为 ModelBridge/model-bridge
- 历史来源/致谢/迁移文档：可保留，但应说明上下文

---

## 9. Git 工作规范

建议小步提交：

```text
chore(rename): rebrand module path to github.com/fm365/model-bridge
chore(rename): update docker, binary and service names to model-bridge
port(kiro): add independent kiro package and service files
port(kiro): wire PlatformKiro into account and gateway dispatch
port(kiro): add admin oauth routes and handlers
test(kiro): restore kiro unit coverage on model-bridge base
docs: add model-bridge migration and upstream attribution
```

避免一个 commit 同时做 rebrand + Kiro 业务迁移 + 测试修复。

---

## 10. 与现有 fm365/sub2api-kiro 的关系

`fm365/sub2api-kiro` 暂时作为旧项目维护和 Kiro 代码来源。`fm365/model-bridge` 是新项目：

- 以 upstream 最新代码为基准
- 品牌名 ModelBridge
- Kiro 作为插件式扩展移植
- 后续 upstream 同步优先在 ModelBridge 上进行

在 ModelBridge 完成 build/test/docker/frontend 闭环前，不建议停止维护旧项目。

---

## 11. 给新 Codex 的启动 Prompt

你可以在 `/Users/qc/projects/model-bridge` 启动新的 Codex 后，把以下内容作为第一条任务：

```markdown
请阅读 docs/MODEL_BRIDGE_HANDOFF.md，并严格按照文档执行 ModelBridge 迁移工作。

目标：
1. 以 Wei-Shaw/sub2api upstream/main 为基础代码。
2. 将项目完整重命名为 ModelBridge / model-bridge / github.com/fm365/model-bridge。
3. 从 fm365/sub2api-kiro 迁移 Kiro 核心代码与集成点。
4. 保持 upstream gateway 解耦结构，不要把 Kiro 私有逻辑塞进 gateway 拆分文件。
5. 后端 go build ./... 与 go test -tags unit ./... 通过。
6. 前端 pnpm build 通过。
7. Docker image/binary/service 名称统一为 model-bridge。

请先输出计划和将要修改的文件清单，等我确认后再动代码。
```

---

## 12. 参考文档

在旧仓库 `fm365/sub2api-kiro` 中：

| 文档 | 作用 |
|------|------|
| `docs/STRATEGY_B_IMPLEMENTATION_PLAN.md` | Strategy B 分阶段实施计划 |
| `docs/UPSTREAM_SYNC_STRATEGY.md` | A/B 两种 upstream 同步策略对比 |
| `docs/UPSTREAM_SYNC_TRACKER.md` | 已完成 PR 与待同步事项跟踪 |
| `docs/HANDOFF.md` | 旧项目总 handoff |
| `docs/KIRO_CHANNEL_TEST_PLAN.md` | Kiro 渠道测试计划 |
| `docs/KIRO_CLI_FLOW_ANALYSIS.md` | Kiro CLI 流程分析 |
| `docs/KIRO_WEBPORTAL_PACKET_ANALYSIS.md` | Kiro WebPortal 包分析 |

---

*本文档用于 ModelBridge 新项目迁移交接。若本文档与当前代码状态冲突，以当前代码和实际测试结果为准，并同步更新本文档。*
