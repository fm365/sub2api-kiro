# 上游同步策略分析（2026-07-09）

> 评估本项目（fm365/sub2api-kiro）跟进 Wei-Shaw/sub2api 的两种主要策略，
> 包括实测数据、工作量评估、风险分析和决策建议。

**分析日期**: 2026-07-09
**前置状态**: 分歧 707 commits / 1363 文件差异 / 20 个 FM365 独有 kiro 文件

---

## 1. 仓库拓扑与关键事实

```
Wei-Shaw/sub2api (upstream, 最源头)
        │
        │  xiangking fork + kiro edition
        ▼
xiangking/sub2api-kiro (中间 fork)
        │
        │  fm365 fork + 多批 cherry-pick
        ▼
fm365/sub2api-kiro (本项目)
```

**关键事实**：Wei-Shaw/sub2api **完全没有 kiro 代码**。kiro 是 xiangking/fm365 fork 的独家功能。这意味着同步 upstream 时所有 kiro 相关文件会被自动保留（modify/delete 冲突默认保留 ours）。

| 仓库 | 角色 | 含 kiro？ |
|------|------|-----------|
| `Wei-Shaw/sub2api` | 最上游 | ❌ 无 |
| `xiangking/sub2api-kiro` | 中间 fork | ✅ 14 个 kiro 文件 |
| `fm365/sub2api-kiro` | 本项目 | ✅ 18 个 kiro 文件 |

---

## 2. 分歧现状（截至 2026-07-09）

| 指标 | 值 |
|------|---|
| Merge base（main vs upstream/main） | `dbc8ae658` (2026-05-08) |
| FM365/main 领先 merge base | 122 commits |
| Upstream/main 领先 merge base | 707 commits |
| File-level diff 总数 | 1363 文件 |
| FM365 独有文件 | 20（全部 kiro 相关） |
| Upstream 独有文件 | 1060（含 ent regenerated） |

---

## 3. 方案 A — 持续 Cherry-Pick（当前策略）

### 3.1 工作模式

以 FM365/main 为基础，按功能域分批 cherry-pick upstream 的关键 commits。

### 3.2 已完成批次

| PR | 批次 | 标题 |
|----|------|------|
| #15 | Batch I | 早期 critical fixes |
| #16 | Batch II | AWS SDK v1.41.5（GO-2026-5764）|
| #17 | Batch II | CWE-204 + CWE-79 安全修复 |
| #18 | Batch II | MarkResponseCommitted 全覆盖 |
| #19 | Batch II | form-data 升级到 >=4.0.6 |
| #20 | Batch III | gateway-doublewrite |
| #21 | Batch III | billing-fallback + long-context + GLM/Kimi/MiniMax 兜底定价 |
| #22 | Batch IV | Gateway thinking/Vertex/DeepSeek |
| #23 | Batch V | Ops SLA + capacity markers |
| #24 | docs | 跟踪 §6.3 同步进度 |

### 3.3 Cherry-Pick 失败案例

| 尝试 | Commit | 失败原因 |
|------|--------|---------|
| Batch VI | `f26ca5661` / `0fd2e9216` / `6ae5fc31b` (OpenAI scheduler 重构) | 21 个冲突文件，引用了 `xai/openai.AllowedClientEntry`、`CodexRestrictionPolicy`、`GrokTokenProvider`、`UserPlatformQuota` 等类型（来自 15+ 上游累积 commit）|
| Batch VII | `ec7b20649` / `31b6e0d94` (account header override) | 同样依赖 `GrokTokenProvider`、`UserPlatformQuotaRepository`、`OpenAIEndpointCapability` 等类型 |

**根因**：upstream 做了大量并行特性开发（拆分单体 `gateway_service.go`、引入 Grok/xai、Codex restriction、user platform quota、plugin system），单 commit 引用了其他 PR 的类型，无法单独 cherry-pick。

### 3.4 持续投入

- 每次 batch: 2-4 小时
- 频率: upstream 每次发布/累积修复后
- 风险: 中（cherry-pick 冲突 + 漏修引用）

---

## 4. 方案 B — 以 Upstream 为基础 Port Kiro

### 4.1 核心思路

`git checkout upstream/main -b v2/upstream-base-kiro-port` 后，把 FM365 的 kiro 体系完整移植过去。

### 4.2 工作量分解（精确评估）

| 步骤 | 内容 | 估时 |
|------|------|------|
| Step 1 | 基于 upstream/main 创建新分支 | 5 分钟 |
| Step 2 | 直接复制 18 个 kiro 独有文件 | 30 分钟 |
| Step 3.1 | 简单集成点（10 个文件，每个 5-10 分钟） | 2-3 小时 |
| Step 3.2 | 复杂集成点（15 个文件，每个 30-60 分钟） | 6-10 小时 |
| Step 3.3 | **gateway_service.go 重构 port** | **8-16 小时** |
| Step 4 | 前端集成（kiro 字段显示） | 1-2 小时 |
| Step 5 | Build + 测试修复 | 4-8 小时 |
| Step 6 | 文档更新 | 1-2 小时 |
| **合计** | | **18-36 小时** |

### 4.3 Kiro 集成点清单（31 个文件）

**简单集成点**（每个 5-10 分钟）：
- `backend/internal/domain/constants.go` — 加 `PlatformKiro = "kiro"`
- `backend/internal/handler/handler.go` — 加 `KiroOAuth` 字段
- `backend/internal/service/wire.go` — 加 `NewKiroOAuthService`
- `backend/internal/server/routes/admin.go` — 加 `registerKiroOAuthRoutes`
- `backend/internal/server/routes/gateway.go` — kiro gateway 路由
- `backend/internal/server/middleware/security_headers.go` — kiro headers
- `backend/internal/repository/scheduler_cache.go` — kiro 调度
- `backend/internal/repository/simple_mode_default_groups.go` — kiro 默认分组
- `backend/internal/handler/endpoint.go` — kiro endpoints
- `backend/internal/web/embed_on.go` — kiro web assets

**复杂集成点**（每个 30-60 分钟）：
- `backend/internal/service/account.go` — **17 处 kiro 处理**（model mapping、passthrough、webportal、strip-tools-on-fail 等）
- `backend/internal/service/gateway_service.go` — ⚠️ 核心难点
- `backend/internal/service/gateway_forward_as_chat_completions.go`
- `backend/internal/service/account_service.go` — kiro 服务实例化
- `backend/internal/service/account_usage_service.go` — kiro usage
- `backend/internal/service/account_test_service.go`
- `backend/internal/service/openai_gateway_service*.go` — kiro model mapping
- `backend/internal/handler/admin/account_handler.go` — kiro 账号增删改查
- `backend/internal/handler/admin/group_handler.go` — kiro 分组
- `backend/internal/handler/gateway_handler.go` — kiro gateway dispatch
- `backend/internal/handler/endpoint_test.go`
- 多个 `_test.go` 文件
- `backend/migrations/136_ops_error_logs_retry_capture_backfill.sql`
- `backend/migrations/137_ops_error_logs_request_capture_idem.sql`

### 4.4 核心风险 — gateway_service.go 重构

```
FM365 main:    10138 行（未重构，kiro + antigravity + 全部功能）
upstream main: 1289 行 （已重构为多个小文件）
```

upstream 把单体 `gateway_service.go` 拆成 ~30 个职责单一的文件：

```
gateway_record_usage.go
gateway_billing_block.go
gateway_request.go
gateway_prompt.go
gateway_model_availability.go
gateway_forward_as_chat_completions.go
image_generation_intent.go
image_output_accounting.go
thinking_protocol.go
... (约 30 个文件)
```

**方案 B 的 gateway_service.go port 工作**：
1. 把 FM365 10138 行的所有功能（kiro dispatch、antigravity、billing block、...）拆分成 upstream 风格的多文件
2. 同时添加 kiro 集成到这些新文件
3. 风险极高 — 容易引入 regression，因为核心计费/调度逻辑分散在 30 个文件中

### 4.5 全量 Merge 实测（已 abort）

2026-07-09 实测 `git merge upstream/main`，结果：
- **168 个文件冲突**
- **135 个 both-modified (UU) 冲突**
- **14 个 ent/ 自动生成冲突**（可机械用 theirs 解决）
- 平均每个冲突文件 2-14 个 conflict markers

**机械 ours/theirs 不可行**：多数冲突是双方独立添加新平台/功能：

```go
<<<<<<< HEAD (FM365)
if a.Platform == domain.PlatformKiro {
    return copyKiroModelMapping()
}
=======
if a.Platform == domain.PlatformGrok {
    return xai.DefaultModelMapping()
}
>>>>>>> upstream/main
```

- 选 ours → 丢 Grok
- 选 theirs → 丢 kiro
- 必须手工合并两边功能

**手工 merge 工作量估算**：168 文件 × 平均 10 分钟 = **28 小时**。

---

## 5. 方案对比

| 维度 | 方案 A（Cherry-Pick） | 方案 B（Upstream Base + Port） |
|------|----------------------|--------------------------------|
| **一次性工作量** | 0（持续投入） | **18-36 小时** |
| **持续维护成本** | 每次 upstream 更新 2-4h | rebase 冲突解决 |
| **立即收益** | 累积的零碎关键修复 | **707 个 upstream commits** |
| **gateway_service.go** | 保持现状（10138 行未重构） | 享受 upstream 重构（拆成 ~30 文件） |
| **核心风险** | cherry-pick 失败 + 漏修引用 | gateway_service.go 大重构引入 regression |
| **测试风险** | 低（只引入部分功能） | 高（整个 gateway 重新组装） |
| **生产风险** | 低（增量改进） | 高（一次性大幅重构） |
| **适合场景** | upstream 增量稳定 | upstream 重构类 commit 累积到必须跟上 |

---

## 6. 决策建议

### 6.1 短期（推荐方案 A 持续）

**理由**：
1. 707 commits 中大部分是 feature commits（Grok、Antigravity、plugin、Codex 限制、BatchImage、EasyPay 自定义支付），与 kiro 无直接关系
2. 真正关键的安全/性能修复（CVE、性能回归）已经分批 cherry-pick 进 main
3. 方案 B 的 gateway_service.go 重构风险太高（10138 → 1289 行 + 拆 30 文件），可能引入生产事故
4. 当前测试 100% 通过，cherry-pick 失败案例可被快速识别和回滚

### 6.2 长期触发方案 B 的时机

以下任一条件出现时，应该启动方案 B：
1. **gateway_service.go 维护成本危机**：FM365 的 10138 行版本已无法维护（多人协作冲突严重）
2. **upstream 重构稳定**：upstream 的 gateway_service.go 拆分经过 6+ 月稳定期
3. **重大新功能需求**：需要最新 ent schema 或某个只有 upstream 有的能力
4. **计划重写 gateway**：反正要重写，干脆对齐 upstream 结构

### 6.3 折中方案 — 渐进重构

如果想逐步靠拢 upstream，可分阶段：

| Phase | 内容 | 工作量 |
|-------|------|--------|
| Phase 1 | cherry-pick `account_header_override` 相关独立小修复 | 2-3 小时 |
| Phase 2 | cherry-pick `openai_scheduler` 非破坏性部分 | 4-6 小时 |
| Phase 3 | 评估是否启动方案 B | 评估 |

### 6.4 已确认不采用方案

- ❌ **全量 `git merge upstream/main` 然后 ours/theirs 处理**：168 冲突无法机械处理
- ❌ **全量 `git merge upstream/main` 然后逐文件手工 merge**：28 小时 + 高风险
- ❌ **等待 upstream 重新引入 kiro**：upstream 永远不会引入 kiro（这是 fork 的独家功能）

---

## 7. 操作清单（按选定方案）

### 7.1 如果选方案 A（继续 Cherry-Pick）

```bash
cd /workspace/sub2api-kiro
source /root/.codex/set-proxy.sh
git fetch upstream main

# 查看新增 commits
git log --oneline main..upstream/main --no-merges | head -30

# 按域筛选（CVE > 性能 > 特性）
# - 安全/CVE: 立即 cherry-pick
# - 性能/重构: 评估后批量
# - Grok/Antigravity/Plugin: 跳过（与 kiro 无关）

# 创建同步分支
git checkout -b merge/upstream-<topic>-$(date +%Y%m%d) main
git cherry-pick -n <commit-hash>

# 处理冲突（重点关注 kiro 引用）

# 测试
cd backend
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache \
  /usr/local/go/bin/go test -tags unit -count=1 -timeout 300s ./...

# 推送 + PR
git push -u origin merge/upstream-<topic>-<date>
gh pr create
```

### 7.2 如果选方案 B（Upstream Base + Port）

```bash
cd /workspace/sub2api-kiro
source /root/.codex/set-proxy.sh
git fetch upstream main

# 基于 upstream 创建新分支
git checkout upstream/main
git checkout -b v2/upstream-base-kiro-port

# Step 2: 复制 18 个 kiro 文件
# （从 fm365/main 复制到当前分支）

# Step 3: 修改 30+ 集成点
# （参考 §4.3 清单）

# Step 4: 前端
# （参考 frontend/src/api/admin/kiro.ts 和 useKiroOAuth.ts）

# 大量测试 + 修复
# （预期多轮 build/test 失败）
```

### 7.3 关键环境配置

- **Go binary**: `/usr/local/go/bin/go`
- **Module root**: `/workspace/sub2api-kiro/backend`（go.mod 不在仓库根）
- **Caches**: `GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache`
- **Proxy**: `source /root/.codex/set-proxy.sh`

---

## 8. 历史决策记录

| 日期 | 决策 | 结果 |
|------|------|------|
| 2026-07-08 | 开始分批 cherry-pick | PR #15-#19 合并成功 |
| 2026-07-08 | Cherry-pick OpenAI scheduler Batch VI | 失败（21 冲突 + 缺失类型）|
| 2026-07-09 | Cherry-pick account header override Batch VII | 失败（缺失 GrokTokenProvider 等）|
| 2026-07-09 | 实测全量 merge upstream/main | 168 冲突，abort |
| 2026-07-09 | 完成测试快照修复 + push | 测试 100% 通过 |
| 2026-07-09 | 文档化策略分析 | 本文档 |

---

*本文档维护者: dev-codex sub-agent*
