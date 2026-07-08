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

## 六、上游未合入 fix 清单（2026-07-08 抓取）

> 数据来源：`Wei-Shaw/sub2api` (`upstream`) 的 `main` 分支
> 抓取命令：`git log --no-merges origin/main..upstream/main`
> 当前基线：`origin/main` = `933f4d01 docs: write definitive handoff SOP and untrack sync-report-v2`
> **xiangking/main 与 origin/main 处于完全相同的基线（merge-base = 9541e0e9f），所有落后均来自最上游 Wei-Shaw/sub2api。**

### 6.1 已在本项目手工移植的 fix（无需再做）

下列上游 commit 已经在本项目内以相同或更安全的方式手工实现（hash 不同）：

| 上游 commit | 上游标题 | 本项目对应 |
|-------------|---------|-----------|
| `11a3da65c` | fix(group): harden peak-rate config handling + server timezone labels | Batch 1 + Batch 2 (`69648c47`) |
| `1034f576d` | fix: peak-rate 全链路透传 + 计费术语修正 | Batch 1 (`102db22c`) |
| `915c60b1` | feat(group): subscription groups add optional peak-rate multiplier | Batch 1 (`102db22c`) |
| `9f5b57fc9` | fix(billing): 防止余额计费持续透支 | 已合入 main |
| `7c2fee6c9` | fix(billing): dedup fallback pricing warn (#3394) | PR #14 已合入 main |
| `21033dceb` | feat(account): configurable pool-mode same-account retry status codes | P2 批次已合入 main |
| `510adf70`  | feat(scheduling): opt-in prefer soonest reset account selection | P2 批次已合入 main |
| `8ce7b9a8`  | feat: configure Claude OAuth system prompt blocks | P2 批次已合入 main |
| `7869b7fe3` | fix(anthropic): 支持 API Key Bearer 认证 | P1 批次已合入 main |
| `59e9356c`  | feat: 抹除 dateline 隐写指纹 | P1 批次已合入 main |
| `a31b50748` | fix(scheduler): 模型404仅冷却账号模型组合 | P1 批次已合入 main |
| `296dc85b`  | refactor(gateway): snapshot usage worker inputs | P2 批次已合入 main |
| `fcd3bc127` | fix: return 404 model_not_found instead of 503 | Batch 3 (`bcc7f310`) |
| `b3f796972` | feat(anthropic): treat 7d_oi (Fable) window 429 as model-level rate limit | 已合入 main |
| `089a7b7fa` | feat(keys): add api key concurrency stats | 已合入 main |

### 6.2 必修高风险（Critical，需立即评估移植）

#### 安全 / 凭证（CWE）
- **`0f8e2d093`** `fix(security): 屏蔽 admin 账号接口返回的敏感凭证字段` — 12 文件 510 行新增，引入 `account_credentials_redact.go` + `credentials_redact.go`，admin 端账号数据接口必须过滤敏感字段。
- **`11b601717`** `fix: return 404 instead of 403 for unauthorized key access to prevent ID oracle (CWE-204)` — 鉴权绕过信息泄露。
- **`0ae332961`** `fix: sanitize API key name with html.EscapeString to prevent stored XSS (CWE-79)` — 前端 XSS。
- **`bbd970249`** `fix(frontend): bump form-data to >=4.0.6 via pnpm override` — 前端依赖安全。

#### 依赖与稳定性
- **`a4f942d8a`** `fix(deps): 升级 AWS SDK 修复 govulncheck 报告的 GO-2026-5764` — 漏洞升级。
- **`a1b2b32e0`** `fix: prevent silent usage_logs drops under queue overflow (#3656)` — usage_log 在队列溢出时静默丢失（计费关键链路）。
- **`f3a3a0869`** `优化并发槽位清理` — 资源回收。
- **`44995404e`** `fix(docker): pin frontend builder pnpm to v9` — 锁定 pnpm。

#### 与本项目约定 / Downstream 兼容性高度相关
- **`6cfb7898d`** `fix(claude-mimicry): drop the cch sign to match new Claude Code CLI` — **与约定 #4 (billing block 不含 cch) 强相关**：上游已完全去掉 cch，本项目需要在 billing 链路确认无 cch 残留。
- **`20f3f2049`** `fix(gateway): complete MarkResponseCommitted coverage for all platforms` — **与约定 #3 (MarkResponseCommitted 必须调用) 强相关**：补齐全平台覆盖。
- **`efbf6d209`** `fix(test): update FilterThinkingBlocksForRetry call to use mappedModel param` — **与约定 #1 (mappedModel 必传) 强相关**。
- **`5cb8cdd36`** `test(claude-code): detection recognizes the new-CLI billing block (no cch)` — Claude Code CLI 指纹识别测试。
- **`6c7203d83`** `fix(gateway): preserve SSE event:error body so ops logs reflect real upstream errors` — 配合约定 #2 (readUpstreamErrorBody) 一并核对。

### 6.3 高优先级（Important，建议近期处理）

#### Gateway / 网关
- **`40e1cc14b`** `fix(gateway): filter anthropic-beta on the Vertex Anthropic path (#3358)` — Vertex 路径必须过滤 anthropic-beta。
- **`6baf00d78`** `fix(gateway): protocol-aware thinking-block filtering for Anthropic-compatible upstreams` — 思考块协议感知过滤。
- **`142d8c361`** `fix(gateway): normalize DeepSeek reasoning_effort 'max' to 'xhigh'` — DeepSeek 推理强度归一化。
- **`2a58a57a7`** `fix(frontend): use configured API base for direct requests` — 前端直连。
- **`6acb46c11` / `429adbc72` / `ae6ee23e2`** — 通用网关 / OpenAI 本地调度容量错误标记 + Ops SLA 排除逻辑（与 Batch 3 强相关，建议合成第二批 no-account 强化）。

#### Billing / 计费
- **`4f5f2788e`** `fix(billing): add kimi-for-coding fallback pricing` + **`a4ce73391`** `feat(billing): add GLM / Kimi / MiniMax fallback pricing for Chinese LLM providers` — 国产模型 fallback 定价（多文件计费链）。
- **`ed2aac25a`** `fix(billing): apply long-context multiplier to cache_creation price` + **`b9509e823`** `fix(billing): apply long-context multiplier to cache_read price` — 长上下文倍率应用到缓存读/写。

#### Scheduler / 调度
- **`f26ca5661`** `feat: add OpenAI advanced scheduler controls` + **`0fd2e9216`** `fix(scheduler): 修复 OpenAI 高级调度器审计发现的正确性与性能问题` — OpenAI 高级调度器（含评分门控 `6ae5fc31b`）。

#### Account / 账号
- **`ec7b20649`** `feat: apikey 账号支持请求头覆写（Anthropic/OpenAI）` + **`31b6e0d94`** `fix: 请求头覆写审计问题修复（禁止名单缺口/beta 对称性/批量清空防护）` — 账号级请求头覆写功能 + 审计修复。
- **`bec1e2b69`** `fix(openai): 永久禁用缺失 refresh_token 的 OAuth 账号` — token 缺失的账号永久禁用。
- **`0a97a5f46`** `fix(token-refresh): treat refresh_token_invalidated as non-retryable` + **`fa8f1749f`** `fix: treat invalid_refresh_token as non-retryable` + **`727ac3f68`** `fix: add app_session_terminated to non-retryable refresh errors` — token 刷新非可重试错误集合。
- **`bf3787de1`** `fix(gateway): allow Claude Code count_tokens` — count_tokens 允许 Claude Code。
- **`b2e2c7e69`** `fix: harden grok oauth gateway paths` + **`b3a07aeae`** `fix: align grok oauth exchange with xai` — grok OAuth 安全强化。
- **`9a0e43980`** `fix(openai): 跨组会话失配保护移到生效的 WSv2 路径并补测` + **`87dd5f5d7`** `fix(openai): 切组后剥离失配的 previous_response_id` — 跨组会话安全。
- **`16bc87693`** `fix(usage): sync 5h ResetsAt to SessionWindowEnd and zero expired window` — 5h 用量窗口重置。

#### Gateway 错误体 / 双写
- **`914c059f4`** `fix: avoid double-writing error frame on non-stream upstream errors` + **`6c8863169`** `fix(gateway): prevent double-write on error passthrough responses` + **`46bd7968a`** `fix: reuse OpenAI failover error body` + **`2c45f91d3`** `fix openai failover model body replacement` — 错误体与 failover 双写修复簇。

### 6.4 中优先级（Medium，按需评估）

#### Grok 媒体路由（约 6 个 commit）
- `c34db70a8` bridge grok composer image inputs
- `3b2099350` route text-only grok video requests
- `42e471f59` harden grok media routing
- `aac3261c6` convert grok image edit uploads
- `c3e860607` include official grok media model ids
- `2fe756e4b` recognize grok media models
- `3b5d812f7` route grok media endpoints
- `0435417f4` enable grok media generation groups
- `9934bd257` default grok group media generation

#### Batch Image 工作流
- `80a229bce` fix(batch-image): 修复审计发现的计费死锁、状态机与队列原子性缺陷
- `aff148167` fix: address batch image ci failures
- `d8e96f0f9` fix: bound batch image settlement retries
- `89edba802` fix: restrict batch image groups to gemini
- `616cf17d9` fix: hide batch image entry without allowed key
- `0b729496e` fix: center batch image empty state
- `8fab63699` feat: complete batch image workflow
- `a994fbd77` feat: add batch image MVP

#### OpenAI / Responses / Compact
- `2fb212b7d` fix(openai): 区分 responses compact 入站端点
- `2dd2be992` fix(compact): 识别 /v1/responses body 中的 compaction_trigger 信号
- `438f17be5` fix(openai): avoid compact usage loss from json sse heuristic
- `a56eb5b4d` fix(compact): body-signal 提升上移到 handler 层并对齐 path-based 链路
- `5c0e580fb` fix(messages): /v1/messages 入站支持不兼容 Responses API 的 OpenAI 上游
- `c797159bf` fix(openai): skip Codex image bridge injections for /responses/compact

#### EasyPay 自定义支付方式
- `bf76168ba` feat: add custom easypay payment methods
- `0dc6e56aa` fix: harden easypay custom method validation
- `a5a2fea04` Polish EasyPay custom method UI

#### 订阅 CNY 换算
- `d56e94b87` feat(payment): 订阅 CNY 换算改为独立汇率配置的显式 opt-in
- `b408edf97` fix(payment): convert subscription CNY pay amount
- `a23a26351` feat(payment): preview subscription CNY charge in plan editor

#### Compact & OAuth
- `cbfeab964` fix(antigravity): default gateway forward base URL to the production endpoint
- `1c0ccb477` fix: add missing Codex CLI headers for OAuth account test
- `cb151e36e` fix: respect custom User-Agent in OAuth account test

#### Antigravity / Vertex
- `df2cedeea` fix: normalize antigravity gemini 3.1 pro routing
- `650c50e34` fix(antigravity): add project fallback for standard tier
- `2a17c0b22` fix(gemini): route Vertex token exchange through account proxy
- `b23475ac0` fix(antigravity): refresh server-invalidated tokens

#### 其他
- `3f2ef6046` fix: optimize ops realtime account stats
- `72ccd1b11` fix: batch group capacity summaries
- `83455a3fe` fix(frontend): harden account data batch import
- `2dc1387b5` fix(promo): allow clearing promo code expiry on edit
- `11fe7de92` fix(account): 重新授权不再清空 Extra 配置
- `c620ad6a3` fix: align group capacity SQL with target schema
- `f8c80bf03` fix(auth): apply promo codes to oauth signups
- `e9a2db8e8` fix: normalize responses streaming terminal output
- `b15375dfb` fix(admin): handle already up-to-date updates
- `27600b1d2` fix(gateway): filter count_tokens generation fields
- `e9a25e7b9` fix(apicompat): preserve empty streaming thinking blocks

### 6.5 低优先级（Low，可选）

#### 前端 UI/UX
- `c7e44a83a` + `a86c534cb` fix(frontend): 路由切换后保持侧边栏滚动位置 + 测试
- `26ca73a4c` fix: hide model scopes for non-antigravity plans
- `446792219` fix: add autocomplete="one-time-code" for TOTP autofill support
- `3ca232ad0` fix(frontend): 编辑弹窗回退旧 credentials 结构以兼容旧后端
- `72c112164` fix(frontend): bedrock_cc_compat toggle not persisting on reload
- `b0c772339` fix(admin/settings): make tab shell readable in dark mode
- `20008264f` feat: 点击侧边栏 Logo/站点名返回首页
- `360f8dec1` fix: 修复管理后台分组页可用账号数显示错误
- `4d51e53d2` fix(redeem): 修复批量复制兑换码兼容性
- `728bb1bc9` feat(frontend): 支持账号数据拖拽和批量导入

#### Chore / Skip
- 所有 `chore: update sponsors`
- `9d5f1b73a` / `76bb7b033` / `0b8e5eec3` / `b650bdd68` `chore: sync VERSION`
- 各种 `docs: ...`、`test: ...` 纯文档/测试增强

### 6.6 重构类（暂不建议 cherry-pick）

> 这些是大文件纯移动拆分（5000+ 行 → 几百行），与本项目下游定制（billing wrapper、API key auth cache、gateway no-account 等）有大量交叉，**直接 cherry-pick 几乎必定产生巨型冲突**。
> 建议在后续业务稳定后，针对每个 service/handler 做一次整体重写时再考虑合入。

- `bb5d2e84a` refactor(handler): 纯移动拆分 setting_handler.go（3957→468行）
- `f013bc114` refactor(service): 纯移动拆分 admin_service.go（4409→642行）
- `2a4c28e8f` refactor(service): 纯移动拆分 antigravity_gateway_service.go（4664→639行）
- `d0f669338` refactor(service): 纯移动拆分 openai_ws_forwarder.go（4675→399行）
- `db3bd9971` refactor(repository): 纯移动拆分 usage_log_repo.go（4701→212行）
- `4d23ad4ba` refactor(service): 纯移动拆分 openai_gateway_service.go（4872→1095行）
- `50043b117` refactor(service): 纯移动拆分 setting_service.go（5471→263行）
- `084d26cbd` refactor(service): 纯移动拆分 gateway_service.go（7294→1289行）
- `d9e514f98` refactor(i18n): 拆分 zh/en 语言包为域模块
- `d754be0d8` refactor(gateway): 抽取 CC forwarder 公共管线并拆分两大 service 文件

### 6.7 同步建议

1. **每周 1 次**执行 `git fetch --all` + 对比上面三类清单。
2. **Critical（6.2）**部分应作为最高优先级逐步合入，每个 fix 单独成 PR 便于回滚。
3. **Important（6.3）**按主题（Gateway / Billing / Scheduler / Account）打包 3-5 个一组做批次合入。
4. **Medium（6.4）**按 Grok / Batch Image / OpenAI 三个特性域分批。
5. **Low（6.5）**前端小修可批量合入；chore 类跳过。
6. **Refactor（6.6）**整服务重写时再统一合入，不要混入特性 PR。

### 6.8 同步时必读

同步任何 fix 之前，**必须先看 §三、代码约定变更**：
1. `FilterThinkingBlocks(body, mappedModel)` 必须传 mappedModel
2. 读上游错误体一律 `s.readUpstreamErrorBody(resp)`
3. service 层写错误响应后必须调用 `MarkResponseCommitted(c)`
4. `buildBillingAttributionBlockJSON` 输出不含 `cch=00000`
5. `BillingService.fallbackWarnSeen` dedup

移植时遇到以下文件必须加倍小心，参考 §二、Downstream Customizations：
- `billing_service.go` / `billing_cache_service.go` — billing wrapper
- `api_key_auth_cache.go` — snapshot 缓存
- `gateway_service.go` / `openai_gateway_service.go` — 错误分类与峰值倍率
- `ops_error_logger.go` — ops 日志（已含 `markOpsRoutingCapacityLimited*` 等）
- `setting_service.go` — PublicSettings / ServerTimezone

---

*最后更新: 2026-07-08（新增 §六、上游未合入 fix 清单）*
