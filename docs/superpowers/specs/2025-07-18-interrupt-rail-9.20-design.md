# InterruptRail 设计文档 (9.20)

## 流程位置与作用

**位置：** DeepAgent ReAct 循环的 `BeforeToolCall` 钩子点

**作用：** 中断-恢复模式——拦截特定工具调用，暂停 ReAct 循环等待用户输入（HITL: Human-in-the-loop），然后恢复执行。是 Agent 人工审批、用户交互的核心机制。

**调用链：**

```
LLM 产生 tool_call → BeforeToolCall 事件
  → BaseInterruptRail.BeforeToolCall()
    → 检查 tool_name 是否在 toolNames 中
    → 提取 user_input (从 ctx.extra[ResumeUserInputKey])
    → 提取 auto_confirm_config (从 session.state)
    → resolveInterrupt() → 三种决策:
      ├─ ApproveResult: 放行（可修改 tool_args）
      ├─ RejectResult: 跳过（设置 _skip_tool + 预设 tool_result）
      └─ InterruptResult: 抛出 AbortError(cause=ToolInterruptException)
```

## 实现范围

三个类，放在独立子包 `rails/interrupt/`：

1. **BaseInterruptRail** — 中断-恢复基类，嵌入 BaseRail
2. **AskUserRail** — 拦截 ask_user 工具，向用户提问等待回答
3. **ConfirmInterruptRail** — 确认中断，支持 auto_confirm

不含 PermissionInterruptRail（属于 9.19 SecurityRail 范畴）。

## 关键设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| BaseInterruptRail 嵌入 | BaseRail（非 DeepAgentRail） | 与 Python 一致，InterruptRail 不需要 workspace/sysOperation |
| 目录组织 | 独立子包 `rails/interrupt/` | 3 个类属同一 Rail 组，与 Python 结构对齐 |
| AskUserTool 实现 | MapFunction 空壳 | 逻辑在 Rail 中，工具本身是占位 |
| AskUserRail Init 模式 | 与 McpRail 一致 | 注册到 ResourceMgr + AbilityManager |

## 目录结构

```
internal/agentcore/harness/rails/interrupt/
├── doc.go                  # 包文档
├── interrupt_base.go       # BaseInterruptRail + 决策类型
├── ask_user_rail.go        # AskUserRail + AskUserPayload/AskUserRequest
├── confirm_rail.go         # ConfirmInterruptRail + ConfirmPayload/ConfirmRequest
├── interrupt_base_test.go  # BaseInterruptRail 测试
├── ask_user_rail_test.go   # AskUserRail 测试
└── confirm_rail_test.go    # ConfirmInterruptRail 测试
```

## 类设计

### 决策类型（interrupt_base.go）

- `InterruptDecision` — 接口（标记类型，三种决策都实现）
- `ApproveResult` — 允许继续执行，可选 NewArgs 替换工具参数
- `RejectResult` — 拒绝执行，预设 ToolResult + 可选 ToolMessage
- `InterruptResult` — 中断等待用户输入，携带 InterruptRequest

### BaseInterruptRail

嵌入 `BaseRail`，priority=90。核心字段 `toolNames map[string]struct{}`。

核心方法：
- `BeforeToolCall` — 拦截入口：检查工具名→提取用户输入→调用 resolveInterrupt→applyDecision
- `resolveInterrupt` — 抽象方法，子类实现
- `applyDecision` — 三分支处理：approve/reject/interrupt
- `raiseInterrupt` — 抛 AbortError(cause=ToolInterruptException)
- `skipTool` — 设置 `ctx.extra["_skip_tool"]=True` + 预设 tool_result
- `getUserInput` — 从 `ctx.extra[ResumeUserInputKey]` 提取，支持 InteractiveInput 和 dict 两种格式

### AskUserRail

嵌入 `BaseInterruptRail`，默认拦截 `ask_user` 工具。

- `Init` — 创建 AskUserTool（MapFunction 空壳），注册到 ResourceMgr + AbilityManager
- `Uninit` — 从 AbilityManager + ResourceMgr 注销
- `resolveInterrupt` — 无用户输入→Interrupt；有输入→解析为 AskUserPayload→Reject(格式化结果)

### ConfirmInterruptRail

嵌入 `BaseInterruptRail`，无默认拦截工具（由调用方指定）。

- `resolveInterrupt` — auto_confirm→Approve；无输入→Interrupt；approved→Approve；!approved→Reject(feedback)

## 依赖

已有（无需新建）：
- `AbortError` — `runner/callback/errors.go`
- `ToolInterruptException` — `single_agent/schema/exception.go`
- `InterruptRequest` / `ToolCallInterruptRequest` — `single_agent/schema/response.go`
- `InteractiveInput` — `session/interaction/interactive_input.go`
- `ResumeUserInputKey` / `InterruptAutoConfirmKey` — `single_agent/schema/state.go`
- `ToolCall` / `ToolMessage` — `foundation/llm/schema/`
- `AskUserMetadataProvider` — `harness/prompts/tools/ask_user.go`
- `BuildToolCard` — `harness/prompts/tools/registry.go`

## 回填项

1. `factory.go` `addDefaultRails()` — 添加 AskUserRail + ConfirmInterruptRail 自动注册
2. `harness_config/builder.go` `resolveRails()` — 添加 interrupt rail 内置解析
3. `rails/doc.go` — 更新包文档，添加 interrupt 子包说明
4. `IMPLEMENTATION_PLAN.md` — 更新 9.20 状态为 ✅

## 对应 Python 代码

- `openjiuwen/harness/rails/interrupt/interrupt_base.py` (225行)
- `openjiuwen/harness/rails/interrupt/ask_user_rail.py` (165行)
- `openjiuwen/harness/rails/interrupt/confirm_rail.py` (99行)
