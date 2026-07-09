# Upstream Sync Tracker

> 记录本项目 (fm365/sub2api-kiro) 与上游 Wei-Shaw/sub2api 的同步状态，
> 包括已合并的 PR、待办的 cherry-pick、merge 策略分析。

**最后更新**: 2026-07-09

---

## 1. 仓库层级关系

| 仓库 | 角色 | URL |
|------|------|-----|
| `Wei-Shaw/sub2api` | 上游（最源头） | https://github.com/Wei-Shaw/sub2api |
| `xiangking/sub2api-kiro` | 中间 fork（含 kiro） | https://github.com/xiangking/sub2api-kiro |
| `fm365/sub2api-kiro` | 本项目 | https://github.com/fm365/sub2api-kiro |

- `origin` → `fm365/sub2api-kiro`
- `upstream` → `Wei-Shaw/sub2api.git`
- `xiangking` → `xiangking/sub2api-kiro.git`

**重要事实**: Wei-Shaw/sub2api **完全没有 kiro 代码**。kiro 是 xiangking/fm365 fork 的独家功能。
这意味着同步 upstream 时，所有 kiro 相关文件会被自动保留（modify/delete 冲突 → 默认保留 ours）。

## 2. 分歧状态（截至 2026-07-09）

| 指标 | 值 |
|------|---|
| Merge base（main vs upstream/main） | `dbc8ae658` (2026-05-08) |
| FM365/main 领先 merge base（commits） | 122 |
| Upstream/main 领先 merge base（commits） | 707 |
| File-level diff 总数 | 1363 文件 |
| FM365 独有文件 | 20（全部 kiro 相关） |
| Upstream 独有文件 | 1060（含 ent regenerated） |

## 3. 全量 Merge 策略分析（2026-07-09 实测）

**结论**: 不建议直接 `git merge upstream/main`。

**实测数据**（已 abort，未落地）：
- 文件冲突总数: 168
- 内容冲突（UU）: 135 文件
- ent/ 自动生成冲突: 14
- 平均每个冲突文件 markers: 2-14 个

**为什么不能用 ours/theirs 机械处理**：
多数冲突是双方独立添加新平台/功能，例如：
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
机械选择 ours 会丢掉 Grok，选择 theirs 会丢掉 kiro。**必须手工合并两边功能**。

**手工 merge 工作量估算**:
- 平均每个冲突文件需要 5-15 分钟手工处理
- 168 个文件 × 10 分钟 = **28 小时**
- 需要保留的 kiro 引用分布在 30+ 文件中
- 风险: 高（可能引入新的 regression）

## 4. 推荐策略: 分批 Cherry-Pick

继续沿用 docs/HANDOFF.md §四、的 SOP，分批按功能域 cherry-pick。

### 已完成批次（按时间倒序）

| PR | 批次 | 标题 | 状态 |
|----|------|------|------|
| #24 | docs | track §6.3 sync progress | merged |
| #23 | Batch V | Ops SLA + capacity markers | merged |
| #22 | Batch IV | Gateway thinking/Vertex/DeepSeek | merged |
| #21 | Batch III | billing-fallback-warn-dedup + long-context multiplier + GLM/Kimi/MiniMax fallback pricing | merged |
| #20 | Batch III | gateway-doublewrite (fix openai failover model body replacement) | merged |
| #19 | Batch II | form-data bump to >=4.0.6 | merged |
| #18 | Batch II | upstream-conventions-batch (MarkResponseCommitted) | merged |
| #17 | Batch II | upstream-cwe-security (CWE-204 ID oracle + CWE-79 stored XSS) | merged |
| #16 | Batch II | upstream-critical-deps (AWS SDK v1.41.5 GO-2026-5764) | merged |
| #15 | Batch I | All earlier critical fixes | merged |

## 5. 当前上游未合入 fix（按优先级）

### 5.1 高风险安全/稳定性（持续关注）

参考 docs/HANDOFF.md §六、的清单（2026-07-08 抓取）。

### 5.2 OpenAI Scheduler Batch（曾尝试 cherry-pick 失败）

- `f26ca5661` / `0fd2e9216` / `6ae5fc31b` (OpenAI scheduler refactor)
- **失败原因**: 引用了 xai/openai.AllowedClientEntry/CodexRestrictionPolicy/GrokTokenProvider/UserPlatformQuota 等类型，
  这些类型来自 15+ 上游 commit 的累积改动，无法单独 cherry-pick。

### 5.3 Account Header Override（曾尝试 cherry-pick 失败）

- `ec7b20649` / `31b6e0d94`
- **失败原因**: 同样依赖 GrokTokenProvider / UserPlatformQuotaRepository / OpenAIEndpointCapability 等类型。

### 5.4 后续可能尝试的方向

如果上游又累积了大量 commit，可以考虑：
1. 等上游稳定后，做一次"全量 merge + 重度手工调整"
2. 或者重写 OpenAI scheduler/account header 模块，对齐上游 API
3. 维护一个 `upstream/feat-x` 分支持续 rebase

## 6. 跟进 SOP（每次 upstream 更新时）

```bash
# 1. 获取最新 upstream
cd /workspace/sub2api-kiro
source /root/.codex/set-proxy.sh
git fetch upstream main

# 2. 查看新增的 commit
git log --oneline main..upstream/main --no-merges | head -30

# 3. 按域筛选 commit（参考 docs/HANDOFF.md §六、）
# - 安全/CWE: 立即 cherry-pick
# - 性能/重构: 评估后按月批量
# - Grok/Antigravity: 与本项目无关，跳过

# 4. 创建同步分支
git checkout -b merge/upstream-<topic>-$(date +%Y%m%d) main

# 5. Cherry-pick（带 -n 不自动提交）
git cherry-pick -n <commit-hash>

# 6. 处理冲突（重点关注 kiro 引用）

# 7. 测试
cd backend
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache \
  /usr/local/go/bin/go test -tags unit -count=1 -timeout 300s ./...

# 8. 推送 + 创建 PR
git push -u origin merge/upstream-<topic>-<date>
gh pr create
```

## 7. 关键环境配置

- **Go binary**: `/usr/local/go/bin/go` (PATH 可能不包含)
- **Module root**: `/workspace/sub2api-kiro/backend` (go.mod 在这里，不在仓库根)
- **Caches**: `GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache`
- **Proxy**: `source /root/.codex/set-proxy.sh`（设置 http_proxy=http://192.168.64.1:8080）

---

*本文档维护者: dev-codex sub-agent*
