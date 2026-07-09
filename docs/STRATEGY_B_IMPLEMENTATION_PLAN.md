# 方案B（上游基准 + Kiro移植）实施计划与操作规范

**版本**: 1.0
**日期**: 2026-07-09
**状态**: `待确认`

> 本文档详细阐述了如何以最上游 `Wei-Shaw/sub2api` 作为干净基准，将本仓库独有的 Kiro 渠道功能作为高内聚定制插件逆向移植回去的实施计划、技术路线与核心操作规范。

---

## 一、 基本现状与核心重构背景

通过深入检索和对比，我们发现了最上游 `Wei-Shaw/sub2api` 与原本定制版的几个关键架构差异：

1.  **`gateway_service.go` 已经完成了解耦重构**：最上游已经将原本上万行的单体 `gateway_service.go` 彻底解耦，拆分成了 **30 多个职责单一的子文件**（如 `gateway_billing_block.go`, `gateway_record_usage.go`, `thinking_protocol.go` 等）。
2.  **Kiro 代码的优秀物理隔离性**：原本的 Kiro 渠道代码在物理结构上已经具有非常良好的解耦性。其核心核心调用和网关转换分布在独占文件（如 `internal/service/kiro_gateway.go`、`internal/pkg/kiro/` 等）中。
3.  **集成侵入极小**：核心网关服务 `GatewayService` 仅需要在一两处关键路径进行分流，即可将 Kiro 渠道请求路由至 `kiro_gateway.go`，几乎不影响最上游拆分后的 30 多个高内聚子文件。

基于此，采用**方案B**（以上游为干净基准，将 Kiro 作为高内聚定制插件逆向 port 回去）是绝对正确且在未来极易维护的选择。

---

## 二、 实施计划 (Roadmap)

我们将整个实施过程划分为 **5 个标准化阶段**，每个阶段都有明确的交付物和验证点。

### 阶段 1：基础脚手架移植（`port/scaffold`）

-   **目标**: 建立一个包含纯净上游代码和 Kiro 独立代码的基准分支。
-   **动作**:
    1.  `git fetch upstream main`
    2.  `git checkout -b merge/strategy-b-kiro-port upstream/main`
    3.  使用 `git checkout main -- <file>` 命令，从原 `main` 分支无损提取 18 个 Kiro 独有文件。
    4.  提交一个独立的 commit `port(kiro): step 1 - copy 18 independent kiro files`。
-   **状态**: `已完成` ✅

### 阶段 2：轻量级集成点对齐与 DI 注入（`port/di-and-routes`）

-   **目标**: 将 Kiro 的服务和处理器注入到上游最新的依赖注入（DI）系统和路由表中。
-   **动作**:
    1.  **集成常量**: 在 `internal/domain/constants.go` 中补全 `PlatformKiro = "kiro"` 常量。
    2.  **DI 注入**:
        -   在 `internal/service/wire.go` 中注册 `NewKiroOAuthService`。
        -   在 `internal/handler/handler.go` 与 `internal/handler/wire.go` 中注册 `KiroOAuth` 处理器。
    3.  **重新生成依赖图**: 在 `backend/` 目录下运行 `go run github.com/google/wire/cmd/wire`。
    4.  **路由绑定**: 在 `internal/server/routes/admin.go` 中调用 `registerKiroOAuthRoutes` 函数，绑定 Kiro OAuth 的后台 API 路由。
-   **验证**: 编译通过 `go build ./...`。

### 阶段 3：网关分流与核心实体对齐（`port/gateway-and-entities`）

-   **目标**: 在网关核心路径上实现 Kiro 渠道的请求分流，并适配 `Account` 实体。
-   **动作**:
    1.  **分流机制**: 在上游最新的网关总入口（预计为 `internal/service/gateway_service.go`）的 `Forward` 方法中，注入极简 Kiro 分流逻辑：
        ```go
        if account != nil && account.Platform == PlatformKiro {
            return s.forwardKiro(ctx, c, account, parsed, startTime)
        }
        ```
    2.  **实体定义对齐**: 在 `internal/service/account.go` 中，从原 `main` 分支移植 `Account` 模型的 Kiro 适配函数，包括：
        -   `IsKiroPassthroughEnabled()`
        -   `IsKiroWebPortalEnabled()`
        -   `IsKiroStripToolsOnFailEnabled()`
        -   `copyKiroModelMapping()`
-   **验证**: 核心业务逻辑编译通过。

### 阶段 4：单元测试与快照订正（`port/tests`）

-   **目标**: 确保 Kiro 自身逻辑和上游最新 API 契约的正确性。
-   **动作**:
    1.  运行 `internal/pkg/kiro/...` 及 `internal/service/kiro_gateway_test.go` 等 Kiro 渠道原生单元测试，并修复所有失败。
    2.  运行 `internal/server/api_contract_test.go`，对因上游字段增加（如 `claude_oauth_system_prompt` 等）产生的快照契约测试失败进行订正。
-   **验证**: `go test -tags unit -count=1 ./...` 全部通过。

### 阶段 5：前端 UI 补齐与灰度验证（`port/frontend`）

-   **目标**: 在前端管理界面提供完整的 Kiro 渠道配置能力。
-   **动作**:
    1.  引入 `frontend/src/api/admin/kiro.ts` 和 `frontend/src/composables/useKiroOAuth.ts` 前端依赖。
    2.  在前端账号、分组相关表单和 View 中，优雅展现 Kiro 渠道特有的授权与属性选项。
-   **验证**: 手动或 E2E 测试 Kiro 账号的创建、编辑、OAuth 授权流程。

---

## 三、 核心操作规范 (Core Invariants)

在推进以上各阶段时，**必须严格遵循**以下代码集成规范，以防止引发 Regression 事故：

1.  **绝对不可破坏最上游的解耦结构**：
    -   Kiro 专用的前置拦截、报文解析、Web Portal 协议伪装等方法，必须全量收拢在 `internal/service/kiro_gateway.go` 中，**严禁**向 `gateway_service.go` 或其他上游解耦子文件里塞入 Kiro 专属的私有辅助方法。
2.  **遵守 FilterThinkingBlocks 统一调用规范**：
    -   过滤思维块的方法在最新合并的 main 中已经升级为 `FilterThinkingBlocks(body, mappedModel)`，移植时若涉及该方法调用，必须传递当前映射后的具体模型参数。
3.  **安全上报（MarkResponseCommitted）**：
    -   在网关向客户端写入错误响应（如 `c.JSON` / `c.AbortWithStatus`）后，**必须**调用 `MarkResponseCommitted(c)` 标记当前响应已提交，避免触发上游底层中间件的 `Double-Write` 重复写入警告或死锁。
4.  **日志与计费分离**：
    -   保证 Kiro 的内部消费日志统计字段（如 `KiroCreditUsage`）仅作为调试与 Trace 使用，严禁干扰 Anthropic 的原生 `input_tokens`/`output_tokens` 扣费统计链路。

---

## 四、 确认与协作

我已做好全面开始执行上述步骤的准备。

**请你确认：**

1.  该计划及代码高内聚解耦方案（将 Kiro 视为插件式扩展，不破坏上游拆分后的 30 个子文件）是否符合你的预期？
2.  是否同意立刻开始**阶段 2：轻量级集成点对齐与 DI 注入**？

期待你的反馈！一旦确认，我们将以最饱满、最严谨的标准开启代码移植！
