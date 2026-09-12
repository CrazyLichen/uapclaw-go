# 10.6.6 Interrupt Rail 对齐修复设计

## 背景

10.6.6 Interrupt Rail 的核心类层次（BaseInterruptRail / AskUserRail / ConfirmInterruptRail / PermissionInterruptRail / StructuredAskUserRail）在 Go 中已完整实现。但深入对比 Python 和 Go 后发现 3 处差异需要修复。

## 流程位置

Interrupt Rail 位于 Agent ReAct 循环的工具调用环节：

```
用户消息 → Gateway → E2A → AgentServer → DeepAdapter → DeepAgent.Run()
  ┌─── ReAct 循环 ─────────────────┐
  │  1. 构造 Prompt (含 Rails 注入)  │
  │  2. LLM 调用                    │
  │  3. 工具调用 → ★ Interrupt Rail 拦截 ★
  │  4. 工具执行 / 中断返回          │
  │  5. 继续循环 / 结束              │
  └────────────────────────────────┘
流式输出 → __interaction__ → convert → chat.ask_user_question
用户响应 → 恢复执行（Resume）
```

核心作用：HITL（Human-in-the-Loop）机制，让 AI Agent 在执行危险/需要确认/需要用户输入的操作时暂停、等待人类响应、然后恢复。

## 差异清单与修复方案

### #4（🔴 高优先级）：makeDeepAgentConfig 未赋值 Rails，热重载全量清空

**问题**：`makeDeepAgentConfig` 接收 `railsList` 参数但未赋值到 `DeepAgentConfig.Rails`。热重载时 `hotReloadRails` 发现 `config.Rails == nil`，走 else 分支全量清空所有已注册 Rail（PermissionRail / AvatarRail / HeartbeatRail 等全部失效）。

**修复**：补 `Rails: railsList`，对齐 Python `rails=rails`（interface_deep.py L2280）。

**文件**：`internal/swarm/server/adapter/deep_adapter.go:1948-1958`

### #1（⚠️ 中优先级）：activate_confirm 流式分支缺失

**问题**：Python 处理 `__interaction__` 时先检查 `interaction_type == "activate_confirm"` 走 `harness.activate_interaction` 事件（含 interaction_id / extension_name / options 等字段），其余才走 converter。Go 直接委托 converter，丢失了 Extension 审批分支。

**修复**：在 `stream_utils.go` 的 `__interaction__` case 中增加 `activate_confirm` 前置判断。

**文件**：`internal/swarm/server/utils/stream_utils.go:154-155`

### #2（低优先级）：ACP 通道权限确认降级日志不足

**问题**：ACP 通道 JSON-RPC 权限确认未实现，当前降级为 `ConfirmActionInterrupt`。日志级别为 Info，缺少 tool_name 等上下文。

**修复**：`logger.Info` → `logger.Warn`，补充 `tool_name` 上下文。

**文件**：`internal/swarm/server/adapter/deep_adapter_rails.go:557-558`

## 不需要修复的项

| 项 | 原因 |
|----|------|
| code_agent 子代理缺少 ConfirmInterruptRail | `CreateCodeAgent` 内置 `mergeRailsWithRequired` 自动注入 4 个必需 Rail |
| `_ensure_rail_type` Go 对应 | Python 中不存在此函数，实际使用 `_merge_rails_with_required`，Go 已实现为 `mergeRailsWithRequired` |
