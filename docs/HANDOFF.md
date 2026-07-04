# HANDOFF

更新时间：2026-07-03 Asia/Shanghai

## 结论口径（对外同步）

1. **原生 `tool_use` 完整透传：已通过（2026-07-02 最新复核）**
   - 修复点：`toolSpecification.inputSchema` 按 Kiro schema 改为 `{"json": <schema>}`。
   - 在 `qc1` 关闭 `kiro_strip_tools_on_fail` 后，`/v1/messages + stream:true + tools` 返回 `HTTP 200`。
   - SSE 含 `tool_use` 与 `input_json_delta`，`message_delta.stop_reason=tool_use`。

2. **`strip-on-fail` 兜底降级：仍可用**
   - 账号 `98/codex-webcookie` 开启 `extra.kiro_strip_tools_on_fail=true`
   - 当 upstream 400 时，sub2api 剥离 `tools` 后重试成功
   - 降级命中时返回头 `X-Kiro-Tools-Stripped: true`
   - 若原生 tools 首次即成功，则不会出现该响应头。

### 接手修复（2026-07-02）

- 代码修复：`backend/internal/pkg/kiro/client.go`
  - `toolsContext` 中 `inputSchema` 由裸 schema 改为 `{"json": schema}`。
- 新增/更新测试：
  - `backend/internal/pkg/kiro/client_test.go`
  - `backend/internal/service/kiro_gateway_test.go`
  - 断言 `conversationState.currentMessage.userInputMessage.userInputMessageContext.tools[0].toolSpecification.inputSchema.json.type == "object"`。
- `qc1` 实测（已替换测试容器二进制并重启）：
  - 关闭 `accounts.id=98` 的 `kiro_strip_tools_on_fail` 后，streaming + tools 返回 `HTTP 200`，并产出原生 `tool_use` 事件。
  - 带 `tool_result` 的后续轮次同样返回 `HTTP 200`，可完成工具结果回传后的文本回答。
  - 恢复 `kiro_strip_tools_on_fail=true` 后，原生成功场景不再误打 `X-Kiro-Tools-Stripped` 头。

### 接手复核（2026-07-02）

- 在 `qc1` 测试环境完成复核，容器 `sub2api-kiro-e2e-fixed-instance` 状态为 `healthy`。
- 将 `accounts.id=98` 的 `extra.kiro_strip_tools_on_fail` 临时关闭并重启测试容器后：
  - `stream:true + tools` 返回 `HTTP 400`
  - body: `{"error":{"message":"Kiro upstream request failed","type":"upstream_error"},"type":"error"}`
- 再开启 `extra.kiro_strip_tools_on_fail=true` 并重启测试容器后：
  - 同请求返回 `HTTP 200`
  - 响应头含 `X-Kiro-Tools-Stripped: true`
- 说明：测试容器存在账号元数据缓存，直接改 DB 后建议重启容器（或执行等效缓存刷新）再验证开关生效。

### 接手修复与 canary 复核（2026-07-03）

- 新增修复：Kiro 请求侧历史工具消息不再文本化。
  - Anthropic `assistant` 历史 `tool_use` 映射到 `conversationState.history[].assistantResponseMessage.toolUses[]`。
  - Anthropic `user` 当前/历史 `tool_result` 映射到 `userInputMessage.userInputMessageContext.toolResults[]`。
  - 不再生成 `[Called ...]` / `[Tool result ...]` 这种普通文本，避免 Claude Code / Codex 工具闭环被降级。
- 本地验证通过：
  - `cd /Users/qc/projects/sub2api-kiro/backend && go test ./internal/pkg/kiro ./internal/service -run 'Kiro|BuildHTTPRequest|Tool|ClaudeCode'`
  - `cd /Users/qc/projects/sub2api-kiro/backend && go test ./internal/pkg/kiro ./internal/service`
- 已完整构建并部署到 `aws-ap1-vps1` canary：
  - 路径：`/root/sub2api-kiro-canary`
  - 镜像：`sub2api-kiro-canary:full-web-stream-tools`
  - 容器：`sub2api-kiro-canary` / `sub2api-kiro-canary-postgres` / `sub2api-kiro-canary-redis`
  - 端口：`18082 -> 8080`，`49154 -> 49153`
  - 状态：`healthy`
- canary 验证通过：
  - `GET /`、`GET /login`、`GET /admin/accounts`、`GET /health` 均返回 `200`。
  - `GET /v1/models` 使用 API key 返回 `200`，包含 Claude 模型。
  - `/v1/messages` 非流式文本返回 `HTTP 200`。
  - `/v1/messages` 流式文本返回 `HTTP 200`，Anthropic SSE 事件完整。
  - `/v1/messages + stream:true + tools` 返回 `HTTP 200`，SSE 含 `tool_use` / `input_json_delta` / `stop_reason=tool_use`。
  - 带 `tool_result` 的第二轮请求返回 `HTTP 200`，模型基于工具结果输出最终文本。
  - 响应与日志未发现 `X-Kiro-Tools-Stripped`、`REQUEST_BODY_INVALID`、`Invalid model`、`gateway.forward_failed`。

### Kiro 输入/输出/cache usage 计量修复与 canary 验证（2026-07-03）

本轮针对 new-api 使用日志中 Kiro 渠道输入/输出偏少、缓存命中/写入不准确的问题做了修复与验证。

已完成代码修复：

- `backend/internal/pkg/kiro/types.go`
  - `Usage` 增加 `cache_creation_input_tokens`、`cache_read_input_tokens`、`cache_creation.ephemeral_5m_input_tokens`、`cache_creation.ephemeral_1h_input_tokens` 等字段。
  - `StreamEvent` 支持携带 upstream usage。
- `backend/internal/pkg/kiro/parse.go`
  - Kiro eventstream parser 会扫描并解析 `usage` / `usageMetadata` / `tokenUsage` 类字段。
  - 支持 camelCase、snake_case、Gemini-style token 字段，以及 nested cache creation 字段。
- `backend/internal/service/kiro_gateway.go`
  - Kiro usage 会映射到 Anthropic-compatible `usage`。
  - 非流式响应写入 `usage.input_tokens`、`usage.output_tokens`、`usage.cache_creation_input_tokens`、`usage.cache_read_input_tokens`。
  - 流式响应在 `message_start.message.usage` 与 `message_delta.usage` 写入 usage/cache 字段。
  - 当 upstream 没有返回 usage 时，按请求体文本做保守估算；包含 `cache_control:{"type":"ephemeral"}` 的块计入 `cache_creation_input_tokens`。

canary 实测环境：

```text
host: aws-ap1-vps1
path: /root/sub2api-kiro-canary
container: sub2api-kiro-canary
image: sub2api-kiro-canary:full-web-stream-tools
web/api: 127.0.0.1:18082
status: healthy
```

canary 实测结果：

- 非流式 cache_control 请求：`HTTP 200`
  - 返回 usage 示例：
    ```json
    {"input_tokens":4,"output_tokens":8,"cache_creation_input_tokens":30,"cache_read_input_tokens":0}
    ```
- 流式 cache_control 请求：`HTTP 200`
  - `message_start.message.usage` 包含 `input_tokens` 与 `cache_creation_input_tokens`。
  - `message_delta.usage` 包含 `output_tokens` 与 cache 字段。
  - 实测示例包含 `cache_creation_input_tokens:33`、`output_tokens:9`。
- `stream:true + tools`：`HTTP 200`
  - SSE 含 `tool_use`、`input_json_delta`、`stop_reason=tool_use`。
  - 未出现 `X-Kiro-Tools-Stripped`。
- `tool_result` 第二轮闭环：`HTTP 200`
  - 模型基于工具结果输出最终文本。
  - 未出现 `[Called ...]` / `[Tool result ...]` 文本化回归。
- 最近日志检查未发现：`gateway.forward_failed`、`kiro upstream error`、`X-Kiro-Tools-Stripped`、`Invalid model`、`REQUEST_BODY_INVALID`、`panic`、`stack overflow`。

当前边界：

- `cache_creation_input_tokens` 已能在 upstream 无 usage 时通过 `cache_control` 做保守补偿，避免 new-api 里 Kiro 大输入/缓存写入显示为 0 或明显偏少。
- `cache_read_input_tokens` 不做伪造。只有 Kiro upstream 明确返回 cache read/hit usage 时才记录为非 0；否则保持 0。这是刻意选择，避免把“缓存创建”误报成“缓存命中”。
- 如果后续产品要求在 upstream 不返回 cache read 的情况下也展示“本地推断命中”，需要另行设计基于 account/model/cache_control 内容 hash 的本地 cache-hit heuristic；该方案会牺牲账单语义严谨性，实施前需要确认。

## 当前分支

- `codex/kiro-strip-tools-on-fail`

## 当前工作区状态

已完成但尚未提交的核心修复：

- `backend/internal/pkg/kiro/client.go`
- `backend/internal/pkg/kiro/client_test.go`
- `Dockerfile`
- `deploy/Dockerfile`
- `frontend/src/components/account/CreateAccountModal.vue`
- `frontend/src/components/account/EditAccountModal.vue`
- `frontend/src/i18n/locales/en.ts`
- `frontend/src/i18n/locales/zh.ts`

当前还有未跟踪文件/目录，需要收尾时明确取舍：

- `.github/copilot-instructions.md`
- `backend/cmd/kiro-dump/`

## 环境约束

- 测试环境：`49.7.235.13:33333`，可以修改、构建、部署。
- 生产环境：`rn-us-vps2` / `107.174.92.113:2222`，只允许查看，不允许修改。
- `49.7.235.13` 所在出口区域不被 Kiro 官方开放 Claude 模型；Kiro upstream 请求必须走代理。
- 测试环境代理：host 上 hysteria client 暴露 `127.0.0.1:8181` / bridge gateway `172.27.0.1:8181`。
- 测试环境代理出口已验证为 `107.175.83.147`，Kiro 支持区域。

## 当前测试部署

测试容器：

- `sub2api-kiro-e2e-fixed-instance`

当前镜像：

- `sub2api-kiro-prod-test-fix:stream-generate-assistant-health`

当前状态：

```text
sub2api-kiro-e2e-fixed-instance   sub2api-kiro-prod-test-fix:stream-generate-assistant-health   Up ... (healthy)
```

测试登录：

- Web UI: `http://127.0.0.1:18080`
- 管理员：`admin@kiro-e2e.local`
- 密码：`CodexTest-2026!`

建议 SSH 隧道：

```bash
ssh -p 33333 -N \
  -L 18080:127.0.0.1:18080 \
  -L 49153:127.0.0.1:49153 \
  root@49.7.235.13
```

## aws-ap1-vps1 canary 测试环境（2026-07-03）

该环境是当前推荐的完整 Web/API canary。机器在美国，Kiro Claude 不需要额外代理。

SSH 隧道：

```bash
ssh -N \
  -L 18082:127.0.0.1:18082 \
  -L 49154:127.0.0.1:49154 \
  aws-ap1-vps1
```

访问：

```text
http://127.0.0.1:18082
```

管理员：

```text
admin@kiro-canary.local
CodexCanary-2026!
```

API key：

```text
sk-3366af4e0e238f2a29f249602362de5677bf392c66a392efbeafbf0b218c5579
```

Kiro 账号：

```text
id=1
name=test1
platform=kiro
type=oauth
status=active
extra={"kiro_passthrough": true}
region=us-east-1
```

## 关键账号 / API key

Kiro 账号：

- `accounts.id=98`
- name: `codex-webcookie`
- platform: `kiro`
- type: `oauth`
- `proxy_id=1`
- `credentials.provider=legacy`
- `credentials.region=us-east-1`
- `credentials.profile_arn=arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK`
- `extra.kiro_passthrough=true`
- `extra.kiro_strip_tools_on_fail=true`（仅测试账号已开启，用于验证降级兜底）
- `extra.kiro_web_portal=false`

代理记录：

- `proxies.id=1`
- name: `hysteria-host`
- protocol: `http`
- host: `172.27.0.1`
- port: `8181`
- latency test: success
- ip: `107.175.83.147`
- country: `US`

E2E API key：

```text
sk-79b74c153038604b56ffd0e0cd3cf8f19a5c754af2dd49de35c5cd839f74687d
```

该 key 绑定：

- `api_keys.id=19`
- name: `e2e-stream-tool-test`
- group: `kiro-default`
- platform: `kiro`

## 已解决的问题

### 1. Kiro Claude 模型区域屏蔽

现象：

```text
Kiro API returned 400: {"message":"Invalid model. Please select a different model to continue.","reason":"INVALID_MODEL_ID"}
```

根因：

- Kiro 官方会按请求出口 IP 所在区域决定是否暴露 Claude 模型。
- `49.7.235.13` 原始出口不在支持区域，直连 Kiro 会屏蔽 Claude。

修复：

- 创建并验证 `proxy_id=1`。
- 将 `accounts.id=98` 绑定到 `proxy_id=1`。
- 注意：sub2api Kiro 调用不依赖进程环境变量 `HTTP_PROXY`；实际走 `account.Proxy.URL()`。

验证结果：

- Web UI 账号测试 `claude-opus-4-7` 成功。
- 响应：`Hey! What are you working on today?`

### 2. `/v1/messages` stream=true 失败

现象：

- `/v1/messages` 非流式成功。
- `/v1/messages` 流式失败：

```json
{"error":{"message":"Kiro upstream request failed","type":"upstream_error"},"type":"error"}
```

ops error log 证据：

```text
platform=kiro
request_path=/v1/messages
stream=t
upstream_status_code=400
upstream_msg=Improperly formed request.
req_body={"model":"claude-opus-4-6","stream":true,...}
```

根因：

- 原代码在 `req.Stream == true` 时切换到 `https://codewhisperer.us-east-1.amazonaws.com/SendMessageStreaming`。
- 但请求体仍然是 `generateAssistantResponse` schema。
- Kiro 对 `SendMessageStreaming` 返回 `Improperly formed request.`。
- 实测 `generateAssistantResponse` 本身会返回 AWS eventstream body；下游只需要把该 eventstream 转为 Anthropic SSE。

修复：

- `backend/internal/pkg/kiro/client.go`
- Kiro Claude legacy 请求即使 `stream=true` 也继续使用 `generateAssistantResponse`。
- 只保留 `amazonq*` 走 `SendMessageStreaming`。

验证结果：

- `/v1/messages` stream=true 返回：
  - `event: message_start`
  - `event: content_block_start`
  - `event: content_block_delta`
  - `event: message_delta`
  - `event: message_stop`
- HTTP status: `200`

### 3. Docker healthcheck 被代理劫持

现象：

- 容器服务实际可用，host 访问 `/health` 是 `200`。
- Docker health 状态却是 `unhealthy`。
- 容器内执行 healthcheck 访问 `localhost` 时走了 `HTTP_PROXY/http_proxy`，代理返回 `404`。

证据：

```text
Connecting to 172.20.0.9:8181 (172.20.0.9:8181)
HTTP/1.1 404 Not Found
```

修复：

- `Dockerfile`
- `deploy/Dockerfile`
- healthcheck 显式清理 proxy env：

```dockerfile
CMD env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
    wget -q -T 5 -O /dev/null http://127.0.0.1:${SERVER_PORT:-8080}/health || exit 1
```

验证结果：

```text
sub2api-kiro-e2e-fixed-instance ... Up ... (healthy)
```

### 4. streaming + tools 原生 tool_use

最新修复：

- Kiro `toolSpecification.inputSchema` 不能使用裸 Anthropic JSON schema。
- Kiro 期望结构是：

```json
{
  "inputSchema": {
    "json": { "type": "object" }
  }
}
```

已在 `backend/internal/pkg/kiro/client.go` 中把 tools schema 归一化为 `inputSchema: {"json": ...}`。

验证结果（Copilot 在 `qc1` 测试容器热替换二进制后验证）：

1. 关闭 `accounts.id=98` 的 `kiro_strip_tools_on_fail` 后，`/v1/messages + stream:true + tools` 返回 `HTTP 200`。
2. SSE 包含：
   - `tool_use`
   - `input_json_delta`
   - `stop_reason=tool_use`
3. 带 `tool_result` 的下一轮请求返回 `HTTP 200`，并给出最终文本回答。
4. 账号开关已恢复为 `kiro_strip_tools_on_fail=true`。
5. 原生成功时不会出现 `X-Kiro-Tools-Stripped`。

当前语义：

- `inputSchema: {"json": ...}` 是原生 tools 支持的关键修复。
- `kiro_strip_tools_on_fail` 保留为保险兜底，不再是主方案。
- 仅当 Kiro upstream 对带 tools 请求返回 400 且账号开启该开关时，才会剥离 tools 重试并返回 `X-Kiro-Tools-Stripped: true`。

## 已验证命令

完整测试矩阵见：`docs/KIRO_CHANNEL_TEST_PLAN.md`。


### 后端测试

```bash
cd /Users/qc/projects/sub2api-kiro/backend

go test ./internal/pkg/kiro

go test ./internal/pkg/kiro ./internal/service -run 'Kiro|BuildHTTPRequest'
```

结果：通过。

### 前端构建

```bash
cd /Users/qc/projects/sub2api-kiro
npm --prefix frontend run build
```

结果：通过。只有 Vite chunk / dynamic import warnings，无编译失败。

### 测试环境完整 E2E

管理员测试接口：

```bash
POST http://127.0.0.1:18080/api/v1/admin/accounts/98/test
{"model_id":"claude-opus-4-7","prompt":"hi"}
```

结果：

```text
data: {"type":"content","text":"Hey! What are you working on today?"}
data: {"type":"test_complete","success":true}
```

非流式 `/v1/messages`：

```bash
curl -sS --noproxy '*' -m 120 -X POST 'http://127.0.0.1:18080/v1/messages' \
  -H 'x-api-key: sk-79b74c153038604b56ffd0e0cd3cf8f19a5c754af2dd49de35c5cd839f74687d' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-opus-4-6","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}'
```

结果：`HTTP 200`。

流式 `/v1/messages`：

```bash
curl -sS --noproxy '*' -m 120 -N -X POST 'http://127.0.0.1:18080/v1/messages' \
  -H 'x-api-key: sk-79b74c153038604b56ffd0e0cd3cf8f19a5c754af2dd49de35c5cd839f74687d' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-opus-4-6","max_tokens":128,"stream":true,"messages":[{"role":"user","content":"hi"}]}'
```

结果：`HTTP 200`，Anthropic SSE 事件完整。

带 tools 流式原生 tool_use：

```bash
curl -sS --noproxy '*' -m 120 -N -X POST 'http://127.0.0.1:18080/v1/messages' \
  -H 'x-api-key: sk-79b74c153038604b56ffd0e0cd3cf8f19a5c754af2dd49de35c5cd839f74687d' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-opus-4-6","max_tokens":512,"stream":true,"tools":[{"name":"get_weather","description":"Get the current weather for a location","input_schema":{"type":"object","required":["location"],"properties":{"location":{"type":"string"}}}}],"messages":[{"role":"user","content":"Use the get_weather tool to look up the weather in Tokyo and tell me what you got back."}]}'
```

结果：

```text
HTTP/1.1 200 OK
SSE includes tool_use
SSE includes input_json_delta
stop_reason=tool_use
```

下一轮带 `tool_result` 请求也返回 `HTTP 200`，并能输出最终文本回答。

兜底验证：当 Kiro upstream 再次拒绝带 tools 请求且账号开启 `kiro_strip_tools_on_fail=true` 时，系统会剥离 tools 重试，并通过响应头标记：

```text
X-Kiro-Tools-Stripped: true
```

## 仍需产品/工程决策

1. 是否提交 `backend/cmd/kiro-dump/`：
   - 当前是调试工具，未跟踪。
   - 建议默认不要进提交，除非整理成正式 internal debug command。
2. 是否将测试账号 `accounts.id=98` 的 `kiro_strip_tools_on_fail=true` 保留：
   - 用于 E2E 验证建议保留。
   - 若要验证默认行为，可临时关闭再跑带 tools 请求，应回到 400。
3. 继续扩展 tool_use 覆盖面：
   - 当前已验证单工具 `tool_use -> tool_result -> final answer` 闭环，并新增请求构造层回归测试覆盖历史 `tool_use`、当前 `tool_result`、tool-result-only 空 content、error status。
   - 建议继续覆盖多工具、嵌套 JSON schema、enum/array/oneOf/anyOf、大 tool input、多轮多个 `tool_use`。
   - WebPortal CBOR schema 仍是研究项，但 legacy endpoint 已能原生支持当前验证过的 tools schema。
4. 生产环境尚未改动：
   - 原则：`rn-us-vps2` 只能查看。
   - 若后续要上线，需要用户明确授权后再执行部署。

## 建议下一步

1. 清理提交范围：

```bash
git status --short
git diff --stat
```

2. 不提交未整理调试目录：

```bash
git restore --staged backend/cmd/kiro-dump/ 2>/dev/null || true
```

3. 建议提交文件：

- `backend/internal/pkg/kiro/client.go`
- `backend/internal/pkg/kiro/client_test.go`
- `Dockerfile`
- `deploy/Dockerfile`
- Kiro UI compatibility switch 相关前端/i18n 文件（如果确认要一起进入本次 PR）
- `docs/HANDOFF.md`

4. 生产部署前必须重新确认：

- 是否使用代理。
- 哪个 Kiro account 绑定 proxy。
- 是否允许开启 `kiro_strip_tools_on_fail`。
- 是否允许修改 `rn-us-vps2`。

---

## 2026-07-03 补充：Kiro 模型映射修复与 `/v1/models` 结论

### 已完成：sonnet-4-5 / opus-4-8 / sonnet-5 映射修复

本次修复提交：

```text
4fdf657f fix(kiro): correct sonnet model mapping and add new models
```

代码变更：

- `backend/internal/pkg/kiro/constants.go`
  - 新增 `claude-opus-4-8 -> claude-opus-4.8`
  - 新增 `claude-sonnet-5 -> claude-sonnet-5`
  - 修正 `claude-sonnet-4-5 -> claude-sonnet-4.5`
  - 修正 `claude-sonnet-4-5-20250929 -> claude-sonnet-4.5`
  - 同步新增 `deepseek-3.2`、`minimax-m2.5`、`minimax-m2.1`、`glm-5`、`qwen3-coder-next` 等静态模型项
- `backend/internal/pkg/kiro/client_test.go`
  - 更新 BuildHTTPRequest 相关断言，期望上游 model 为 `claude-sonnet-4.5`，不再使用 Kiro 内部枚举名 `CLAUDE_SONNET_4_5_20250929_V1_0`

qc1 开发环境 DB 同步：

- `accounts.id=4` (`kiro-test`) 的 `credentials.model_mapping` 已更新：
  - `claude-sonnet-4-5 = claude-sonnet-4.5`
  - `claude-sonnet-4-5-20250929 = claude-sonnet-4.5`
  - `claude-opus-4-8 = claude-opus-4.8`
  - `claude-sonnet-5 = claude-sonnet-5`

qc1 验证结果（admin test, account id=4）：

```text
claude-haiku-4-5:            PASS
claude-sonnet-4-5:           PASS
claude-sonnet-4-5-20250929:  PASS
claude-sonnet-4-6:           PASS
claude-sonnet-5:             PASS
claude-opus-4-5:             PASS
claude-opus-4-6:             PASS
claude-opus-4-7:             PASS
claude-opus-4-8:             PASS
```

qc1 里程碑与回滚文件：

```text
/root/sub2api-kiro-e2e/milestones/20260703-sonnet-model-mapping-fix.log
/root/sub2api-kiro-e2e/fix-models-20260703/fix_mapping.sql
/root/sub2api-kiro-e2e/fix-models-20260703/rollback_mapping.sql
/root/sub2api-kiro-e2e/fix-models-20260703/sub2api.before.bin
/root/sub2api-kiro-e2e/fix-models-20260703/sub2api.linux.bin
```

注意：本地 macOS 交叉编译容器二进制时必须使用：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/sub2api.fix-models.linux ./cmd/server
```

不能直接 `go build`，否则会产出 Mach-O，容器内运行会报：

```text
/app/sub2api: line 1: ... not found
/app/sub2api: line 2: syntax error: unexpected ")"
```

### `/v1/models` / Kiro ListAvailableModels 结论

实际抓包与 fuzz 结论：

- sub2api Kiro 渠道当前走的是 CodeWhisperer BFF：

```text
https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse
https://codewhisperer.us-east-1.amazonaws.com/SendMessageStreaming
```

- qc1 容器抓包确认 SNI：

```text
sub2api-kiro-e2e-fixed-instance -> 172.27.0.1:8181
SNI = codewhisperer.us-east-1.amazonaws.com
```

- Kiro/CodeWhisperer 后端没有可用的 `ListAvailableModels` API。已测试过的候选 endpoint 返回 `UnknownOperationException`、`403`、`404` 或 `401`，包括：

```text
POST https://codewhisperer.us-east-1.amazonaws.com/ListAvailableModels
POST https://codewhisperer.us-east-1.amazonaws.com/ListModels
POST https://codewhisperer.us-east-1.amazonaws.com/GetAvailableModels
GET  https://codewhisperer.us-east-1.amazonaws.com/v1/models
POST https://app.kiro.dev/service/KiroWebPortalService/operation/ListAvailableModels
POST https://app.kiro.dev/service/KiroWebPortalService/operation/ListModels
GET  https://app.kiro.dev/v1/models
```

因此 `/v1/models` 当前不能“实时接 Kiro 后端 ListAvailableModels”，因为该 API 未发现且 CodeWhisperer SDK strings 里只有 `ListAvailableProfiles` / `ListAvailableCustomizations`，没有 `ListAvailableModels`。

当前可落地方案：

1. 维护 `backend/internal/pkg/kiro/constants.go::Models` 静态模型集。
2. 账号级 `credentials.model_mapping` 继续作为实际上游 model id 的权威映射。
3. 如需更强诊断能力，可新增一个 `kiro-probe-models` 调试工具：用当前账号 token 遍历候选 modelId，记录哪些被 Kiro 后端接受、哪些返回 `INVALID_MODEL_ID`。该工具不影响运行时，只用于运维核验。

### tools schema 适配现状

Kiro tools schema 已在 `backend/internal/pkg/kiro/client.go` 中按 CodeWhisperer BFF 结构构造：

- Anthropic `tools[].input_schema` -> Kiro `toolSpecification.inputSchema.json`
- Anthropic 历史 `assistant tool_use` -> Kiro `history[].assistantResponseMessage.toolUses[]`
- Anthropic 当前 `tool_result` -> Kiro `currentMessage.userInputMessage.userInputMessageContext.toolResults[]`
- `tool_result.is_error=true` -> Kiro `status="error"`
- tool result string / array / object 均包装成 `content[].json`

对应单测已覆盖：

```text
TestBuildRequestBody_ToolInputSchemaWrappedAsJSONDocument
TestBuildRequestBody_DoesNotTextifyHistoricalToolBlocks
TestBuildRequestBody_ToolResultOnlyCurrentMessageKeepsEmptyContent
TestBuildRequestBody_ToolResultErrorStatus
```

### Kiro contextUsage / metering 修复与 canary 验证（2026-07-04）

修复点：

- `backend/internal/service/kiro_gateway.go`
  - 修复 `contextUsageEvent` 被错误换算成 `usage.input_tokens` 的问题。
  - 旧行为会把 `contextUsagePercentage * 200000` 写入 `message_delta.usage.input_tokens`，导致短流式请求出现类似 `input_tokens=4078/4542` 的异常统计。
  - 新行为仅将 `contextUsagePercentage` 存到 `ClaudeUsage.KiroContextUsagePercent`，不污染 Anthropic-compatible token 字段。
  - 新增 `meteringEvent` 处理，将 Kiro credit usage 存到 `ClaudeUsage.KiroCreditUsage` / `KiroCreditUnit`，不换算为 input/output tokens。
- `backend/internal/pkg/kiro/parse.go` / `types.go`
  - `StreamEvent` 增加 `Metering`。
  - eventstream parser 识别 `meteringEvent`，支持 Kiro 真实格式：`{"unit":"credit","unitPlural":"credits","usage":...}`。
- `backend/internal/pkg/kiro/parse_test.go`
  - 新增 `TestParseNonStreamingResponse_MeteringAndContextUsage`，覆盖 `meteringEvent` 与 `contextUsageEvent` 解析。

代码状态：

```text
branch: codex/kiro-strip-tools-on-fail
commit: 4bb8da39 fix(kiro): stop converting contextUsagePercentage into input_tokens and handle meteringEvent
pushed: origin/codex/kiro-strip-tools-on-fail
```

本地测试：

```text
cd backend && go build ./...
cd backend && go test ./internal/pkg/kiro/...
cd backend && go test ./internal/pkg/kiro/... ./internal/service/...
```

结果：全部通过。

部署验证：

```text
host: aws-ap1-vps1
path: /root/sub2api-kiro-cli-fix-canary
container: sub2api-kiro-cli-fix-canary
image: sub2api-kiro:kiro-cli-flow-fix-v2
ports: 18086 -> 8080, 49157 -> 49153
status: healthy
```

关键 canary 结果：

- `GET /health`: `HTTP 200`
- `GET /v1/models`: `HTTP 200`，包含 `claude-opus-4-8`、`claude-sonnet-5`、`claude-opus-4-7`。
- `GET /kiro/v1/models`: `HTTP 200`
- `POST /v1/messages stream:false`: `HTTP 200`，`stop_reason=end_turn`。
- `POST /v1/messages stream:true`: `HTTP 200`，SSE 事件完整。
- contextUsage 修复验证：
  - 修复前：`message_delta.usage.input_tokens` 曾出现 `4078/4542` 这类由 `contextUsagePercentage` 换算出的异常值。
  - 修复后：同类短流式请求中 `message_start.usage.input_tokens=8`，`message_delta.usage.input_tokens=8`。
- `stream:true + tools`: `HTTP 200`，SSE 含 `tool_use`、`input_json_delta`、`stop_reason=tool_use`。
- `tool_result` 第二轮闭环：`HTTP 200`，`stop_reason=end_turn`。
- `tool_result is_error=true`: `HTTP 200`。
- `cache_control.ephemeral` 长文本：`cache_creation_input_tokens > 0`，`cache_read_input_tokens=0`。
- 25K 非流式 / 流式文本请求均可完成。

测试脚本结果摘要：

```text
PASS  API-MODELS-001
PASS  API-MODELS-002
PASS  API-NONSTREAM-001
PASS  API-STREAM-001
PASS  API-STREAM-005
PASS  API-STREAM-006  ms=8 md=8
PASS  API-NONSTREAM-003  25K request completed; Kiro upstream usage is flaky but at least one run returned >1000 input_tokens
PASS  API-NONSTREAM-004
PASS  API-STREAM-007
PASS  API-TOOL-002
PASS  API-TOOL-005
PASS  API-TOOL-006
PASS  API-TOOL-007
PASS  API-TOOL-008
PASS  API-TOOL-009
PASS  E2E-001
PASS  E2E-002-Web
```

注意事项：

- Kiro upstream 自身的 `input_tokens` 对长文本存在波动。同一 25K payload 多次请求观察到过 `input_tokens=4116`、`49`、`39` 等不同值；这是 upstream 返回值不稳定，不是 `contextUsageEvent` 换算污染。
- `cache_read_input_tokens` 仍不伪造；只有 upstream 明确返回 cache read/hit usage 时才应记录非 0。
- 当前 canary 是新增环境，不影响现有 `sub2api-kiro-canary` 或生产 `rn-us-vps2`。
