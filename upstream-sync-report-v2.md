# fm365/sub2api-kiro 上游同步工作交接报告 v2

> 更新日期: 2026-07-06 | 上一版: 2026-07-05

## 背景

本仓库 `fm365/sub2api-kiro` fork 自 `xiangking/sub2api-kiro`，后者 fork 自 `Wei-Shaw/sub2api`。

核心难点：kiro 对 `gateway_service.go` 等核心文件有大量定制（Kiro 平台支持、TLS 指纹伪装、OAuth 模拟等），导致直接 cherry-pick 冲突严重。

## 整体进度

| 类别 | 已合并 | 剩余 | 进度 |
|------|--------|------|------|
| 后端核心 fix | ~55 commits | ~26 | 68% |
| BedrockCC 兼容层 | 全部 | 0 | 100% |
| 支付模块 | 主要 fix | 少量细节 | 85% |
| 前端 i18n | 6 个 | 0 | 100% |
| 前端 Vue 组件 | 0 | ~168 | 0% (低优先级) |
| gateway_service.go | 17 个 | ~22 | 44% |
| CI / 文档 | 0 | ~43 | 低优先级 |

## 已合并的 PR（共 11 个）

| PR | 标题 | 合并日期 |
|---|------|---------|
| #1 | Fix Kiro tools, usage, and model discovery | 07-04 |
| #2 | Sync upstream fixes | 07-04 |
| #4 | 18 个后端 fix + BedrockCC 兼容层 | 07-04 |
| #5 | 12 个 gateway_service.go 上游 fix | 07-05 |
| #6 | 13 个 upstream fix (streaming, Codex, Gemini) | 07-05 |
| #7 | 支付模块 fix (多币种、退款、邮件通知) | 07-05 |
| #8 | 前端 i18n 文案修复 | 07-05 |
| #9 | 安全凭证脱敏 + 容量重试测试 | 07-05 |
| #10 | stream keepalive Timer fix + drop cch signature | 07-05 |
| #11 | 基础设施 + thinking protocol + prevent double-write | 07-05 |

另有一个直接推送到 main 的测试修复 commit (28dc8d35)。

## 当前 main 分支状态

- **编译**: `go build ./...` ✅
- **测试**: 38/39 包 PASS
- **唯一失败**: `TestOpenAIResponses_RejectsHTTPContinuationPreviousResponseID` — pre-existing, 测试 mock 不完整导致 502 而非期望的 400，不影响功能

## 本轮新增的关键改动

### 1. Stream Keepalive Timer Fix (PR#10)
- `time.Ticker` → `time.Timer` + 手动 `Reset()`
- 解决 Claude Code 长时间推理时连接断开问题
- 影响: `handleStreamingResponseAnthropicAPIKeyPassthrough` 和 `handleStreamingResponse`

### 2. Drop CCH Signature (PR#10)
- 移除 billing attribution block 中的 `cch=00000` 字段
- 新版 Claude Code CLI (2.1.193+) 不再发送此字段
- `signBillingHeaderCCH` 函数保留为死代码，`enable_cch_signing` 设置为 no-op

### 3. parseRawJSONView + readUpstreamErrorBody (PR#11)
- 零拷贝 gjson 解析函数 (unsafe pointer cast)
- 错误体读取默认限制 512KiB (configurable)
- 替换了 16 处硬编码的 `io.ReadAll(io.LimitReader(resp.Body, 2<<20))`

### 4. Thinking Protocol 感知过滤 (PR#11)
- 新文件: `thinking_protocol.go` + `thinking_protocol_test.go`
- `ResolveThinkingProtocol(modelID)` 按模型前缀分类:
  - `ThinkingProtocolAnthropicStrict`: claude-*/opus-*/sonnet-*/haiku-*
  - `ThinkingProtocolPassbackRequired`: deepseek-*/kimi-*/moonshot-*/glm-*/minimax-m*/qwen*-thinking
- `FilterThinkingBlocks(body, mappedModel)` 和 `FilterThinkingBlocksForRetry(body, mappedModel)` 现在是协议感知的
- `NormalizeChineseLLMThinking`: MiniMax M 系列 thinking.type=enabled → adaptive
- Forward 函数中新增预过滤调用点

### 5. Prevent Double-Write (PR#11)
- 新增 `ResponseCommittedKey` + `MarkResponseCommitted`/`IsResponseCommitted` (ops_upstream_context.go)
- 所有错误响应写入函数标记 committed
- Handler 的 `ensureForwardErrorResponse` 检查 committed 后跳过

## 未合并的上游 fix（~22 个 gateway_service.go commit）

### 需要 Account 模型变更的:
| Commit | 描述 | 阻塞原因 |
|--------|------|---------|
| 7869b7fe | fix(anthropic): 支持 API Key Bearer 认证 | 需要 `GetAnthropicAPIKeyAuthScheme()` 方法 |
| 21033dce | feat: configurable pool-mode retry status codes | 需要 `Account.IsPoolModeRetryableStatus()` 方法 |
| a31b5074 | fix(scheduler): 模型404仅冷却账号模型组合 | `handleFailoverSideEffects` 和 `handleErrorResponse` 签名变更 |

### 需要配置/分组模型变更的:
| Commit | 描述 |
|--------|------|
| 915c60b1 | feat(group): 高峰时段倍率 |
| 1034f576 | fix: 高峰倍率全链路透传 |
| 11a3da65 | fix(group): harden peak-rate config handling |
| 9f5b57fc | fix(billing): 防止余额计费持续透支 |

### 大规模 refactor（建议观望）:
| Commit | 描述 | 改动量 |
|--------|------|--------|
| b1c4be4a | remove parsed request object graphs | 16 files, -1219/+690 |
| 619e5ae6 | isolate anthropic body rewrites | 8 files |
| 2caee9d8 | snapshot usage worker inputs | 5 files |

### 功能特性（按需引入）:
| Commit | 描述 |
|--------|------|
| 510adf70 | feat(scheduling): prefer soonest reset account selection |
| 8ce7b9a8 | feat: configure Claude OAuth system prompt blocks |
| 59e9356c | feat: 抹除 Anthropic OAuth dateline 隐写指纹 |
| 6cfb7898 + a5781fe3 | (已合入) |
| f7f5e338 | feat(quota): user×platform 配额 DB 写聚合 flusher |
| 06fca662 | feat(quota): sentinel 回填 |
| 6b39b344 | feat(quota): 用户 × 平台 USD 配额 |
| 2eb622f2 | Remove ops retry replay storage |

## 后续合并建议

### 优先级 1（高价值，中等难度）
1. **API Key Bearer 认证** (7869b7fe) — 在 Account 上添加 `credentials.api_key_auth_scheme` 字段和方法
2. **dateline 隐写指纹抹除** (59e9356c) — 独立的 `internal/pkg/anthropicfp/dateline.go` 包 + 设置开关
3. **模型 404 仅冷却模型组合** (a31b5074) — 需要给 `handleFailoverSideEffects`/`handleErrorResponse` 添加 requestedModel 参数

### 优先级 2（中等价值）
4. **防止余额透支** (9f5b57fc) — 需要 `BillingCacheService.balanceBelowEligibilityThreshold` 和 `minimumBalanceReserve` 配置
5. **configurable pool retry codes** (21033dce) — Account credentials 扩展
6. **prefer soonest reset scheduling** (510adf70)

### 优先级 3（大量工作，建议等上游稳定后批量引入）
7. 高峰倍率系列 (3 commits)
8. 用户×平台配额系列 (3 commits)
9. request object graphs refactor (b1c4be4a)

## 操作指南

### 验证当前 main

```bash
cd backend
go build ./...
go test ./internal/service/... -count=1
go test ./internal/repository/... -count=1
go test ./internal/handler/admin -count=1
go test ./internal/pkg/... -count=1
# 已知 pre-existing 失败:
# TestOpenAIResponses_RejectsHTTPContinuationPreviousResponseID (handler包)
```

### 合并新的上游 fix 的工作流

```bash
# 1. 拉取上游最新
git fetch upstream

# 2. 创建工作分支
git checkout main && git pull origin main
git checkout -b merge/<描述>

# 3. 尝试 cherry-pick（大概率冲突）
git cherry-pick --no-commit <commit-hash>

# 4. 冲突严重时放弃，改为手动移植
git reset --hard HEAD
# 手动阅读 git show <commit> 的 diff，将逻辑移植到我们的代码

# 5. 验证
go build ./...
go test ./internal/service/... -count=1

# 6. 推送 + 创建 PR
git push -u origin merge/<描述>
HTTP_PROXY="" HTTPS_PROXY="" gh pr create --title "..." --body "..." --base main

# 7. 合并
HTTP_PROXY="" HTTPS_PROXY="" gh pr merge <N> --merge --delete-branch
```

### 注意事项

1. **gh CLI 需清除代理**: 本地 copilot-hub 进程可能劫持 HTTPS，执行 gh 前清除代理变量
2. **git push 可能需要清除代理**: 如果 git config 中有 http.proxy/https.proxy，先 `git config --global --unset http.proxy`
3. **gateway_service.go 近 10000 行**: 手动移植时注意定位正确的函数（有两处 streaming handler）
4. **FilterThinkingBlocks 签名已变**: 现在需要传 `mappedModel` 参数
5. **readUpstreamErrorBody 已替换所有硬编码**: 新代码应使用 `s.readUpstreamErrorBody(resp)` 而非 `io.ReadAll(io.LimitReader(...))`

## Git 分支状态

| 分支 | 状态 |
|------|------|
| `main` | 当前主分支，含所有 PR 合并 |
| `remotes/upstream/main` | 上游最新 |
| 其他 merge/* 分支 | 已合并后删除 |

## 文件修改索引（本轮涉及的主要文件）

| 文件 | 改动内容 |
|------|---------|
| `backend/internal/service/gateway_service.go` | keepalive Timer, cch 移除, readUpstreamErrorBody, thinking pre-filter, MarkResponseCommitted |
| `backend/internal/service/gateway_billing_block.go` | 移除 cch=00000 |
| `backend/internal/service/thinking_protocol.go` | 新文件: 协议族分类 |
| `backend/internal/service/thinking_protocol_test.go` | 新文件: 35 个测试 case |
| `backend/internal/service/gateway_request.go` | NormalizeChineseLLMThinking, FilterThinkingBlocks 签名变更 |
| `backend/internal/service/ops_upstream_context.go` | ResponseCommittedKey + helpers |
| `backend/internal/handler/gateway_handler.go` | ensureForwardErrorResponse 加 IsResponseCommitted 检查 |
| `backend/internal/handler/openai_gateway_handler.go` | 同上 |
| `backend/internal/service/openai_gateway_service.go` | MarkResponseCommitted 标记点 |
| `backend/internal/service/gemini_messages_compat_service.go` | 同上 + FilterThinkingBlocksForRetry 签名更新 |
| `backend/internal/service/antigravity_gateway_service.go` | MarkResponseCommitted 标记点 |
| `backend/internal/service/ops_retry.go` | FilterThinkingBlocksForRetry 签名更新 |
