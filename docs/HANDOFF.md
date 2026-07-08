# Handoff — 下游定制 & 上游同步指南

> 本文档记录 `fm365/sub2api-kiro` 相对于上游 `Wei-Shaw/sub2api` 的定制逻辑、代码约定，以及后续跟进上游更新的标准化流程（SOP）。

---

## 一、仓库层级关系

```
Wei-Shaw/sub2api  (最上游，git remote: upstream)
  ↑ fork
xiangking/sub2api-kiro  (中间上游，git remote: xiangking)
  ↑ fork
fm365/sub2api-kiro  (我们的仓库，git remote: origin)
```

- Go module path 仍为 `github.com/Wei-Shaw/sub2api`
- 所有 Git remote 已在本地配置：`origin`、`upstream`、`xiangking`

---

## 二、Downstream Customizations（下游定制）

以下是我们相对于上游的核心定制，同步上游时必须**特别保护**这些逻辑不被覆盖：

### 2.1 Billing Wrapper（计费包装层）

- 我们在上游 gateway 之上封装了独立的 billing 逻辑层，包括：
  - `billing_service.go` — 统一计费入口
  - `billing_cache_service.go` — 余额缓存与透支防护
  - `deductUsageBillingBalance` — 余额扣减前置充足性校验（防透支）
  - `buildBillingAttributionBlockJSON` — 构建计费 attribution block
- **关键约定：billing block 不含 `cch=00000`** — 上游可能引入 `cch` 字段，我们必须过滤掉。

### 2.2 API Key Auth Cache（API Key 鉴权缓存）

- 我们在上游基础上增加了 `APIKeyAuthGroupSnapshot` 缓存层，用于加速鉴权路径下的 group 配置读取。
- Snapshot 携带了 `PeakRateEnabled`、`PeakStart`、`PeakEnd`、`PeakRateMultiplier` 等高峰时段倍率字段。

### 2.3 Peak Rate Multiplier（高峰时段倍率）

- 新增 `Group` 实体字段：`PeakRateEnabled`、`PeakStart`、`PeakEnd`、`PeakRateMultiplier`
- 新增验证/归一化函数：`NormalizePeakRateConfig`、`ValidatePeakRateConfig`、`PeakMultiplierAt`
- 计费路径通过 `computePeakAwareMultipliers` 应用峰值感知倍率

### 2.4 No-Account Error Classification（无账号错误分类）

- 当 account selection 返回空池时，区分两种场景：
  - **404 Model Not Found**：group 有账户但没有配置该模型映射（配置错误/拼写错误/不支持的模型）
  - **503 Service Unavailable**：账户存在但暂时耗尽（限流、配额自动暂停、运行时阻塞）
- 引入 `ModelAvailabilityDiagnoser` 接口和 `DiagnoseModelAvailabilityForPlatform` 方法，忽略瞬态状态（限流、配额等）仅检查模型映射配置。
- 避免 503 误伤反向代理健康检查。

### 2.5 Server Timezone（服务器时区）

- `PublicSettings` 新增 `ServerTimezone`（IANA 名称）和 `ServerUTCOffset`（UTC 偏移）字段
- 前端据此标注高峰时段等按服务器本地时间判定的窗口，避免用户按浏览器本地时间误读

### 2.6 其他定制

- Dateline 隐写指纹抹除（`anthropicfp/dateline.go`）
- Claude OAuth system prompt blocks 配置化
- Pool-mode 可配置化 retry status codes
- Prefer soonest reset 调度优化

---

## 三、代码约定变更

同步上游时，以下约定**必须遵循**：

1. **FilterThinkingBlocks 签名**：`FilterThinkingBlocks(body, mappedModel)` 和 `FilterThinkingBlocksForRetry(body, mappedModel)` — 必须传 `mappedModel`
2. **readUpstreamErrorBody**：读上游错误体用 `s.readUpstreamErrorBody(resp)` 而非 `io.ReadAll(io.LimitReader(...))`
3. **MarkResponseCommitted**：service 层写完错误响应后必须调用 `MarkResponseCommitted(c)`
4. **billing block 不含 cch**：`buildBillingAttributionBlockJSON` 输出不含 `cch=00000`
5. **fallback warn 去重**（#3394）：`BillingService.fallbackWarnSeen` sync.Map 按小写模型名记录，每模型每进程至多一条 warn

---

## 四、后续上游跟进 SOP（Standard Operating Procedure）

当 `Wei-Shaw/sub2api` 或 `xiangking/sub2api-kiro` 有新更新时，请按以下步骤跟进：

### 4.1 获取最新上游代码（无害获取）

```bash
# 确保在 main 分支且工作区干净
git checkout main
git fetch --all
```

### 4.2 对比与识别新 commit

```bash
# 查看源头最上游比我们多的 commit
git log origin/main..upstream/main --oneline

# 或查看中间上游
git log origin/main..xiangking/main --oneline
```

### 4.3 按需手工移植（Cherry-pick）

**绝对不要直接 `git pull` 强行合并！** 这会覆盖我们的定制逻辑。

```bash
# 1. 创建临时同步分支
git checkout main
git checkout -b merge/sync-upstream-YYYYMMDD

# 2. 将上游 commit 捡拾过来（不自动提交）
git cherry-pick --no-commit <commit_hash>
```

> `--no-commit` 的好处：即使有冲突，也只停留在暂存区。你可以从容地打开编辑器手工合并，并随时调整代码。

### 4.4 解决冲突并遵循代码约定

编辑冲突文件时，务必检查是否破坏了第二节所述的下游定制逻辑。特别关注：
- `billing_service.go`、`billing_cache_service.go` 中的 billing wrapper 逻辑
- `api_key_auth_cache.go` 中的 snapshot 缓存
- `filter_thinking_blocks.go` 中的 mappedModel 参数
- `read_upstream_error_body.go` 中的错误体读取方式

### 4.5 本地完整验证

```bash
source /root/.codex/set-proxy.sh
export PATH=$PATH:/usr/local/go/bin
cd backend

# 编译验证
go build ./...

# service 单测验证
go test -tags unit ./internal/service/... -count=1
```

### 4.6 推送并创建 PR

```bash
git push -u origin merge/sync-upstream-YYYYMMDD
gh pr create --title "sync: port upstream fixes" --body "..." --base main
```

### 4.7 合并后清理本地

```bash
git checkout main
git pull origin main
git branch -d merge/sync-upstream-YYYYMMDD
```

### ⚠️ 核心建议

- **不要积攒太多再同步**：建议每隔 1~2 周看一眼上游 commit。小步快跑（每次同步 1-3 个重要功能）最容易避免冲突。
- **善用本文档**：凡是碰到修改 `docs/HANDOFF.md` 中"下游定制"部分的文件时，一定要加倍小心，对照恢复我们的定制逻辑。
- **临时报告文件不同步**：`upstream-sync-report-v*.md`、`HANDOFF_CONTAINER_*.md` 等是本地开发过程中的阶段性备忘录，**不应提交到仓库**。如需归档，请移至 `/tmp/` 或删除。

---

## 五、已合并批次记录

### Batch 1 — Peak Rate Multiplier
- Commit: `102db22c` feat(group): add peak-rate multiplier for subscription groups
- 来源上游: `915c60b1`、`1034f576`、`11a3da65`

### Batch 2 — Settings Server Timezone
- Commit: `69648c47` feat(settings): expose server timezone info in PublicSettings
- 来源上游: `11a3da65` 中的 ServerTimezone 部分

### Batch 3 — No-Account Error Classification
- Commit: `bcc7f310` feat(gateway): add model availability diagnosis on empty pool
- 来源上游: upstream no-account error refactoring series

### Batch 4 — Docs
- Commit: `537728e8` docs: finalize merge documentation

### PR #15
- 链接: https://github.com/fm365/sub2api-kiro/pull/15
- 包含以上全部批次

---

*最后更新: 2026-07-08*
