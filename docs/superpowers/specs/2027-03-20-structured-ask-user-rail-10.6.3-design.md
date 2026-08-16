# 10.6.3 StructuredAskUserRail 实现设计

## 概述

实现 10.6.3 小节的 StructuredAskUserRail，对齐 Python 的 `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py`。

StructuredAskUserRail 是 AskUserRail 的扩展，支持结构化选项问答：当 LLM 调用 `ask_user` 工具时携带 `questions` 参数（包含 header/options/multi_select），前端可渲染为可点击选项而非纯文本输入框。

## 在 Agent 会话中的流程位置与作用

```
用户消息 → Gateway → E2A → AgentServer → JiuWenClaw → DeepAdapter/CodeAdapter
                                                       │
                                                       ├─ CreateInstance() 构建 Rails
                                                       │   └─ CodeAdapter: buildStructuredAskUserRail()
                                                       │
                                                       └─ ProcessMessage/Stream()
                                                           └─ ReAct Loop
                                                               ├─ BeforeToolCall
                                                               │   └─ StructuredAskUserRail.resolveStructuredInterrupt
                                                               │       ├─ 无用户输入 → Interrupt(AskUserRequest)
                                                               │       ├─ 有 questions → 结构化路径 → Reject
                                                               │       └─ 无 questions → 回退父类 AskUserRail
                                                               └─ Tool 执行
```

**作用**：StructuredAskUserRail 是 Agent 与用户交互的"结构化问答中间件"，在 ReAct 循环的 BeforeToolCall 钩子中拦截 `ask_user` 工具调用，根据是否携带结构化选项选择不同的中断/恢复路径。

## Python vs Go 对齐分析

### Python 层级结构

```
agent-core (openjiuwen/)
  harness/rails/interrupt/interrupt_base.py  → BaseInterruptRail
  harness/rails/interrupt/ask_user_rail.py   → AskUserRail

jiuwenswarm/ (Swarm 侧扩展)
  agents/harness/common/rails/ask_user_rail.py → StructuredAskUserRail(AskUserRail)
```

### Go 当前结构

```
agentcore/harness/rails/interrupt/
  interrupt_base.go  → BaseInterruptRail     ✅ 对齐
  ask_user_rail.go   → AskUserRail           ✅ 对齐
  confirm_rail.go    → ConfirmInterruptRail   ✅ 对齐
  helpers.go         → ConvertInteractionsToAskUserQuestion ✅ 对齐
```

### Go 缺失部分

| 能力 | Python StructuredAskUserRail | Go 当前 |
|---|---|---|
| 结构化 questions 参数 | ✅ | ❌ |
| StructuredAskUserPayload | ✅ | ❌ |
| StructuredAskUserTool (扩展 schema) | ✅ | ❌（AskUserTool schema 已有 questions 但描述不同） |
| extract_questions 方法 | ✅ | ❌ |
| 中英文工具描述 | ✅ | ❌（需调整描述文本） |
| 结构化路径 reject 格式化 | ✅ | ❌ |

## 设计决策

### 决策 1：实现方式 — 新建 StructuredAskUserRail 嵌入 AskUserRail

对齐 Python 的继承模式。新建 `StructuredAskUserRail` 结构体嵌入 `interrupt.AskUserRail`，覆盖 Init/Uninit/resolveInterruptFn。

### 决策 2：文件位置 — swarm/server/rails/ 新包

对齐 Python 的"Swarm 侧扩展"语义（Python 的 StructuredAskUserRail 在 jiuwenswarm/ 下而非 agent-core/ 下）。

### 决策 3：未导出函数处理 — 导出必要函数

- `buildAskRequest` → `BuildAskRequest`（导出，StructuredAskUserRail 调用父类方法）
- `resolveInterruptFn` 字段 → `ResolveInterruptFn`（导出，StructuredAskUserRail 需覆盖）
- `resolveInterruptFn` 类型 → `ResolveInterruptFn`（导出，类型定义）
- `parseToolArgs`、`joinStrings`、`askUserPayloadSchema` 保持未导出（StructuredAskUserRail 不需要这些，Python 的 StructuredAskUserRail 也不调用父类的对应方法）

### 决策 4：Python 的 StructuredAskUserRail 对父类方法的调用分析

| 调用 | Python 方式 | Go 实现 |
|---|---|---|
| `super().__init__()` | 继承 | 嵌入 AskUserRail |
| `self.interrupt(self._build_ask_request(tool_call))` | 调用父类方法 | `r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))` |
| `self.extract_questions(tool_call)` | 自身新增方法 | `r.extractQuestions(toolCall)` |
| `self.reject(tool_result=...)` | 调用 BaseInterruptRail 方法 | `r.BaseInterruptRail.Reject(...)` |
| `await super().resolve_interrupt(...)` | 调用父类方法 | `r.parentResolve(ctx, cbc, toolCall, userInput, autoConfirmConfig)` |
| `super()._build_ask_request(tool_call)` | 调用父类方法 | `r.AskUserRail.BuildAskRequest(toolCall)` |

## 修改文件清单

### 1. interrupt 包导出修改

**`interrupt/interrupt_base.go`**：
- `resolveInterruptFn` 字段 → `ResolveInterruptFn`
- `resolveInterruptFn` 类型 → `ResolveInterruptFn`
- `NewBaseInterruptRail` 中 `r.resolveInterruptFn = ...` → `r.ResolveInterruptFn = ...`
- `BeforeToolCall` 中 `r.resolveInterruptFn(...)` → `r.ResolveInterruptFn(...)`
- 注释同步更新

**`interrupt/ask_user_rail.go`**：
- `buildAskRequest` → `BuildAskRequest`
- `r.resolveInterruptFn = r.resolveAskUserInterrupt` → `r.ResolveInterruptFn = r.resolveAskUserInterrupt`

**`interrupt/confirm_rail.go`**：
- `r.resolveInterruptFn = r.resolveConfirmInterrupt` → `r.ResolveInterruptFn = r.resolveConfirmInterrupt`

**`interrupt/interrupt_base_test.go`**：
- `r.resolveInterruptFn = ...` → `r.ResolveInterruptFn = ...`

### 2. 新包 `swarm/server/rails/`

**目录结构**：

```
swarm/server/rails/
├── doc.go
├── structured_ask_user_rail.go    # StructuredAskUserRail 主体
├── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
├── structured_ask_user_rail_test.go
└── structured_ask_user_tool_test.go
```

### 3. StructuredAskUserRail 核心结构

```go
// StructuredAskUserPayload 结构化用户回答载荷
type StructuredAskUserPayload struct {
    Answers map[string]string `json:"answers"`
}

// StructuredAskUserRail 扩展 AskUserRail，支持结构化选项问答
type StructuredAskUserRail struct {
    interrupt.AskUserRail
    structuredTools []tool.Tool
    language        string
    parentResolve   interrupt.ResolveInterruptFn  // 保存父类 resolve 函数
}
```

### 4. StructuredAskUserRail 核心逻辑

```
NewStructuredAskUserRail(language)
  ├─ NewAskUserRail() → 嵌入
  ├─ 保存 parentResolve = r.AskUserRail.BaseInterruptRail.ResolveInterruptFn
  └─ 覆盖 ResolveInterruptFn = r.resolveStructuredInterrupt

resolveStructuredInterrupt:
  ├─ user_input == nil → Interrupt(BuildAskRequest(toolCall))
  ├─ extractQuestions(toolCall) → 有 questions → 结构化路径
  │   ├─ 解析为 StructuredAskUserPayload
  │   ├─ 格式化为 "question: answer" 文本
  │   └─ Reject(answer_text)
  └─ 无 questions → 回退 parentResolve（对齐 Python super().resolve_interrupt()）
```

### 5. StructuredAskUserTool

对齐 Python 的 `StructuredAskUserTool`：
- 使用 `GetAskUserMetadataProviderInputParams(language)` 获取 questions schema（Go 已有，比 Python 的基础版更完善）
- 描述文本对齐 Python 的 `EXTENDED_DESCRIPTION_EN/CN`，强调两种模式：纯文本 vs 结构化选项
- 工具名仍为 `ask_user`，ID 为 `ask_user_{agentID}`
- invoke/stream 返回空 map{}（对齐 Python，真正逻辑在 Rail 中通过中断机制完成）

### 6. CodeAdapter 回填

**`swarm/server/adapter/code_adapter.go`**：

```go
func (c *CodeAdapter) buildStructuredAskUserRail() sainterfaces.AgentRail {
    // 对齐 Python: JiuwenClawCodeAdapter._build_structured_ask_user_rail()
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("panic", r).
                Msg("StructuredAskUserRail 创建失败")
        }
    }()
    return rails.NewStructuredAskUserRail(c.deep.resolveRuntimeLanguage())
}
```

### 7. IMPLEMENTATION_PLAN.md 更新

10.6.3 的 `☐` → `✅`（AskUser / StructuredAskUserRail 完成）

## 交互流程

```
LLM 调用 ask_user(questions=[...])
  → BeforeToolCall → StructuredAskUserRail.resolveStructuredInterrupt
    → user_input == nil → Interrupt(AskUserRequest{questions=...})
    → 前端收到 chat.ask_user_question 事件
    → 用户选择选项 → resume(user_input)
    → resolveStructuredInterrupt → 结构化路径
      → StructuredAskUserPayload{answers: {q: selected}}
      → Reject("question: selected\n...")
```

## 对应 Python 代码

- `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py` — StructuredAskUserRail + StructuredAskUserTool + StructuredAskUserPayload
- `openjiuwen/harness/rails/interrupt/ask_user_rail.py` — 基类 AskUserRail
- `openjiuwen/harness/rails/interrupt/interrupt_base.py` — 基类 BaseInterruptRail
- `jiuwenswarm/server/runtime/agent_adapter/interface_code.py` — CodeAdapter._build_structured_ask_user_rail()
