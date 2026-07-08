# Handoff — Upstream Sync Batch 2 + P3 (#3394) (2026-07-07)

## 状态
Batch 2 在 worktree 中待合并；P3 billing fallback warn dedup (#3394) 已 staged 待提交。
当前 HEAD = origin/main @ 48da3457（Merge PR #13 snapshot-usage-worker-inputs）。
本地分支 `merge/billing-fallback-warn-dedup` 仅承载工作区，未推进 HEAD。

## 本次完成的上游 fix

| Commit | 功能 | 实现文件 | 状态 |
|--------|------|---------|------|
| 9f5b57fc | 防止余额透支 (balanceBelowEligibilityThreshold) | billing_cache_service.go | worktree |
| 510adf70 | prefer soonest reset 调度 | gateway_service.go, config.go | worktree |
| 8ce7b9a8 | Claude OAuth system prompt blocks | setting_service.go, settings_view.go, SettingsView.vue, zh.ts, en.ts | worktree |
| 59e9356c | dateline 隐写指纹抹除 | gateway_service.go | worktree |
| 2caee9d8 | Codex usage snapshot | openai_gateway_service.go | worktree |
| 7869b7fe | API Key Bearer 认证 | anthropic_apikey_auth.go | worktree |
| a31b5074 | 模型 404 仅冷却组合 | ratelimit_service.go | worktree |
| 21033dce | 配置化 pool-mode retry codes | account.go, account_pool_retry_status_codes_test.go | worktree |
| 7c2fee6c | dedup fallback pricing warn (#3394) | billing_service.go, billing_service_test.go, channel_service.go | **staged 待提交** |

## 验证
- `cd backend && go build ./...`: PASS
- `cd backend && go test -tags unit ./internal/service -run TestGetModelPricing -count=1 -v`: PASS（包含 3 个新 fallback warn dedup 用例 + 1 个回归）

## 未合入（观望项）
- 619e5ae6: isolate anthropic body rewrites
- 2eb622f2: Remove ops retry replay storage
- b1c4be4a: remove parsed request object graphs
- f7f5e338 + 06fca662 + 6b39b344: 用户×平台配额系列
- upstream 新增（2026-07 上半月）：easypay 自定义支付、OpenAI 高级调度器、API Key 请求头覆写、订阅 CNY opt-in、Claude 7d OI Fable rate limit 等

## 代码约定变更
1. FilterThinkingBlocks 需传 mappedModel
2. readUpstreamErrorBody 替代 io.ReadAll+LimitReader
3. MarkResponseCommitted 在 service 层写错误响应后必须调用
4. **billing block 不含 cch**：buildBillingAttributionBlockJSON 输出不含 `cch=00000`
5. **fallback warn 去重**（#3394）：BillingService.fallbackWarnSeen sync.Map 按小写模型名记录，每模型每进程至多一条 warn

## 下一步
1. 提交当前 staged 的 P3 fix (#3394)：
   ```
   cd /Users/qc/projects/sub2api-kiro
   git commit -m "fix(billing): dedup fallback pricing warn to stop per-request log spam (#3394)"
   ```
2. 评估 Batch 2 worktree 中尚未合入 main 的 P2/P3 commits，逐批做独立 worktree 验证
3. 对照 upstream-sync-report-v3.md 处理 P3 列表 + upstream 新增 commits

