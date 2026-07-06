# AGENTS.md

---

##环境信息：

qc1 -- 开发环境
HostName 49.7.235.13
Port 33333
User root
容器内使用代理:172.27.0.1:8181 才能访问kiro claude模型

aws-ap1-vps1 -- 测试环境
HostName 18.143.195.253
Port 22
User root

rn-us-vps2 -- 生产环境
HostName 107.174.92.113
Port 2222
User root
---

##开发流程：
1. 在开发环境做：编写测试用例，开发、编译、单元测试，打包docker镜像，上传到dockerhub。
2. 在测试环境 拉取镜像，部署环境，做功能测试。
3. 开发和测试环境都没有问题后，等用户确认后，在生产环境拉取镜像，按照用户的要求进行部署。

---

## 上游同步工作上下文（更新于 2026-07-06）

### 仓库关系
- `fm365/sub2api-kiro` (我们) → fork 自 `xiangking/sub2api-kiro` → fork 自 `Wei-Shaw/sub2api` (upstream)
- Go module path 仍为 `github.com/Wei-Shaw/sub2api`
- git remote: `origin` = fm365, `upstream` = Wei-Shaw, `xiangking` = xiangking

### main 分支当前状态
- **编译**: `go build ./...` ✅
- **测试**: 38/39 包 PASS
- **唯一 pre-existing 失败**: `TestOpenAIResponses_RejectsHTTPContinuationPreviousResponseID`（handler 包，mock 不完整导致 502 而非期望 400，不影响功能）
- **已合并 PR**: #1-#11 全部 merged

### 已完成的合并工作（共 ~55 commits）
- BedrockCC 兼容层 (100%)
- 前端 i18n (100%)
- 支付模块主要 fix (85%)
- 安全凭证脱敏
- Stream keepalive Timer fix（Ticker→Timer 防止连接 stall）
- Drop CCH signature（对齐新版 Claude Code CLI 2.1.193+）
- parseRawJSONView 零拷贝解析 + readUpstreamErrorBody 限制读取
- Thinking Protocol 感知过滤（DeepSeek/Kimi/GLM/MiniMax 多轮不再 400）
- Prevent double-write on error passthrough

### 代码约定变更（接手者必须遵循）
1. **FilterThinkingBlocks 签名已变**: `FilterThinkingBlocks(body, mappedModel)` 和 `FilterThinkingBlocksForRetry(body, mappedModel)` — 必须传 mappedModel
2. **readUpstreamErrorBody**: 所有读上游错误体的代码应使用 `s.readUpstreamErrorBody(resp)` 而非 `io.ReadAll(io.LimitReader(...))`
3. **MarkResponseCommitted**: service 层写完错误响应后必须调用 `MarkResponseCommitted(c)`，handler 层的 `ensureForwardErrorResponse` 会检查跳过
4. **billing block 不再含 cch**: `buildBillingAttributionBlockJSON` 输出不含 `cch=00000`，`signBillingHeaderCCH` 是死代码

### 未合并的上游 fix（~22 个 gateway_service.go commit）

#### 优先级 1（高价值，中等难度）
| Commit | 描述 | 阻塞原因 |
|--------|------|---------|
| 7869b7fe | fix(anthropic): 支持 API Key Bearer 认证 | 需 Account 添加 `GetAnthropicAPIKeyAuthScheme()` |
| 59e9356c | feat: 抹除 dateline 隐写指纹 | 独立包 `anthropicfp/dateline.go` + 设置开关，涉及 15 文件 |
| a31b5074 | fix(scheduler): 模型404仅冷却模型组合 | `handleFailoverSideEffects`/`handleErrorResponse` 签名变更 |

#### 优先级 2（中等价值）
| Commit | 描述 |
|--------|------|
| 9f5b57fc | fix(billing): 防止余额计费持续透支 |
| 21033dce | feat: configurable pool-mode retry status codes |
| 510adf70 | feat(scheduling): prefer soonest reset |
| 8ce7b9a8 | feat: configure Claude OAuth system prompt blocks |

#### 优先级 3（大量工作，建议等上游稳定后批量引入）
| Commit | 描述 |
|--------|------|
| 915c60b1 + 1034f576 + 11a3da65 | 高峰时段倍率系列 (3 commits) |
| f7f5e338 + 06fca662 + 6b39b344 | 用户×平台配额系列 (3 commits) |
| b1c4be4a | remove parsed request object graphs (16 files, 大重构) |
| 619e5ae6 | isolate anthropic body rewrites |
| 2caee9d8 | snapshot usage worker inputs |
| 2eb622f2 | Remove ops retry replay storage |

### 合并操作指南

```bash
# cherry-pick 流程（大概率冲突，改为手动移植）
git fetch upstream
git checkout main && git pull origin main
git checkout -b merge/<描述>
git cherry-pick --no-commit <hash>  # 尝试
git reset --hard HEAD               # 冲突时放弃
# 手动移植: git show <hash> 看 diff，把逻辑写入我们的代码

# 验证
cd backend && go build ./... && go test ./internal/service/... -count=1

# 推送
git push -u origin merge/<描述>
HTTP_PROXY="" HTTPS_PROXY="" gh pr create --title "..." --body "..." --base main
HTTP_PROXY="" HTTPS_PROXY="" gh pr merge <N> --merge --delete-branch
```

### 注意事项
- `gh` CLI 和 `git push` 有时需清除代理变量（copilot-hub 进程可能劫持 HTTPS）
- `gateway_service.go` 近 10000 行，有两处 streaming handler（API Key passthrough 和 OAuth）
- 前端 Vue 组件 (~168 commits) 因 API 不兼容暂不合并，需前端团队逐步适配
- 详细报告见 `upstream-sync-report-v2.md`
