# InterruptRail 实现计划 (9.20)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 InterruptRail 的三个核心类（BaseInterruptRail / AskUserRail / ConfirmInterruptRail），为 DeepAgent 提供中断-恢复（HITL）能力。

**Architecture:** 在 `rails/interrupt/` 独立子包中实现三个 Rail。BaseInterruptRail 嵌入 BaseRail（非 DeepAgentRail），在 BeforeToolCall 钩子中拦截已注册工具名，通过 resolveInterrupt 获取决策（approve/reject/interrupt），applyDecision 执行对应动作。AskUserRail 和 ConfirmInterruptRail 继承 BaseInterruptRail 实现各自的 resolveInterrupt 逻辑。

**Tech Stack:** Go 1.22+, 复用已有的 AbortError/ToolInterruptException/InterruptRequest/InteractiveInput 等核心层类型

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| Create | `internal/agentcore/harness/rails/interrupt/doc.go` | 包文档 |
| Create | `internal/agentcore/harness/rails/interrupt/interrupt_base.go` | BaseInterruptRail + 决策类型 |
| Create | `internal/agentcore/harness/rails/interrupt/ask_user_rail.go` | AskUserRail + AskUserPayload/AskUserRequest |
| Create | `internal/agentcore/harness/rails/interrupt/confirm_rail.go` | ConfirmInterruptRail + ConfirmPayload/ConfirmRequest |
| Create | `internal/agentcore/harness/rails/interrupt/interrupt_base_test.go` | BaseInterruptRail 测试 |
| Create | `internal/agentcore/harness/rails/interrupt/ask_user_rail_test.go` | AskUserRail 测试 |
| Create | `internal/agentcore/harness/rails/interrupt/confirm_rail_test.go` | ConfirmInterruptRail 测试 |
| Modify | `internal/agentcore/harness/rails/doc.go` | 添加 interrupt 子包说明 |
| Modify | `internal/agentcore/harness/factory.go` | addDefaultRails 添加 AskUserRail/ConfirmInterruptRail |
| Modify | `IMPLEMENTATION_PLAN.md` | 更新 9.20 状态 |

---

### Task 1: doc.go 包文档

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package interrupt 提供中断-恢复（HITL）Rail 实现。
//
// 在 BeforeToolCall 钩子中拦截特定工具调用，暂停 ReAct 循环
// 等待用户输入（Human-in-the-loop），然后恢复执行。
// 是 Agent 人工审批、用户交互的核心机制。
//
// 三种决策类型：
//   - ApproveResult：放行工具执行（可修改参数）
//   - RejectResult：跳过工具执行（预设返回结果）
//   - InterruptResult：中断等待用户输入
//
// 文件目录：
//
//	interrupt/
//	├── doc.go              # 包文档
//	├── interrupt_base.go   # BaseInterruptRail + 决策类型
//	├── ask_user_rail.go    # AskUserRail + AskUserPayload/AskUserRequest
//	└── confirm_rail.go     # ConfirmInterruptRail + ConfirmPayload/ConfirmRequest
//
// 对应 Python 代码：openjiuwen/harness/rails/interrupt/
package interrupt
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/interrupt/`
Expected: PASS

---

### Task 2: interrupt_base.go — 决策类型

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/interrupt_base.go`

- [ ] **Step 1: 编写决策类型和 BaseInterruptRail 结构体**

关键要点：
- `InterruptDecision` 接口——三种决策都实现此接口（含未导出标记方法 `isInterruptDecision()`）
- `ApproveResult` — `NewArgs string`（可选，替换工具参数）
- `RejectResult` — `ToolResult any` + `ToolMessage *llmschema.ToolMessage`（可选）
- `InterruptResult` — `Request *saschema.InterruptRequest`
- `BaseInterruptRail` — 嵌入 `agentinterfaces.BaseRail`，字段 `toolNames map[string]struct{}`，优先级 90

Import 路径：
- `agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`
- `saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"`
- `llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`
- `cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"`
- `sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"`

结构体定义遵循项目规范（结构体→常量→全局变量→导出函数→非导出函数，中文注释）。

- [ ] **Step 2: 编写 BaseInterruptRail 核心方法**

```go
// NewBaseInterruptRail 创建 BaseInterruptRail 实例。
// toolNames 为需拦截的工具名列表，可为空。
func NewBaseInterruptRail(toolNames ...string) *BaseInterruptRail {
    r := &BaseInterruptRail{
        BaseRail: *agentinterfaces.NewBaseRail(),
        toolNames: make(map[string]struct{}, len(toolNames)),
    }
    for _, name := range toolNames {
        r.toolNames[name] = struct{}{}
    }
    r.WithPriority(baseInterruptRailPriority) // 90
    return r
}

// Approve 创建允许决策。
func (r *BaseInterruptRail) Approve(newArgs string) *ApproveResult {
    return &ApproveResult{NewArgs: newArgs}
}

// Reject 创建拒绝决策。
func (r *BaseInterruptRail) Reject(toolResult any) *RejectResult {
    return &RejectResult{ToolResult: toolResult}
}

// Interrupt 创建中断决策。
func (r *BaseInterruptRail) Interrupt(request *saschema.InterruptRequest) *InterruptResult {
    return &InterruptResult{Request: request}
}

// AddTool 注册需拦截的工具名。
func (r *BaseInterruptRail) AddTool(toolName string) {
    r.toolNames[toolName] = struct{}{}
}

// AddTools 批量注册需拦截的工具名。
func (r *BaseInterruptRail) AddTools(toolNames []string) {
    for _, name := range toolNames {
        r.toolNames[name] = struct{}{}
    }
}

// GetTools 返回所有已注册的工具名列表。
func (r *BaseInterruptRail) GetTools() []string {
    names := make([]string, 0, len(r.toolNames))
    for name := range r.toolNames {
        names = append(names, name)
    }
    return names
}
```

- [ ] **Step 3: 编写 BeforeToolCall 拦截入口**

核心逻辑（对齐 Python `BaseInterruptRail.before_tool_call`）：

1. 从 `cbc.Inputs()` 做 type switch 获取 `*agentinterfaces.ToolCallInputs`
2. 检查 `toolInputs.ToolName` 是否在 `r.toolNames` 中，不在则 return nil
3. 调用 `r.resolveToolCallID(toolInputs.ToolCall)` 获取 toolCallID
4. 调用 `r.getUserInput(cbc, toolCallID)` 提取用户输入
5. 从 session 获取 auto_confirm_config：`cbc.Session().GetState(state.StringKey(saschema.InterruptAutoConfirmKey))`
6. 调用 `r.resolveInterrupt(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)` 获取决策
7. 调用 `r.applyDecision(cbc, toolInputs, decision)` 执行决策

注意：`resolveInterrupt` 是未导出抽象方法，由子类覆盖。BaseInterruptRail 提供默认实现返回 Interrupt（中断等待输入）。

- [ ] **Step 4: 编写 applyDecision / raiseInterrupt / skipTool**

```go
// applyDecision 根据决策类型执行对应动作。
func (r *BaseInterruptRail) applyDecision(
    cbc *agentinterfaces.AgentCallbackContext,
    toolInputs *agentinterfaces.ToolCallInputs,
    decision InterruptDecision,
) {
    switch d := decision.(type) {
    case *ApproveResult:
        if d.NewArgs != "" {
            toolInputs.ToolArgs = d.NewArgs
        }
    case *RejectResult:
        r.skipTool(cbc, toolInputs, d)
    case *InterruptResult:
        r.raiseInterrupt(toolInputs.ToolName, toolInputs.ToolCall, d.Request)
    }
}

// raiseInterrupt 抛出 AbortError 中断执行。
func (r *BaseInterruptRail) raiseInterrupt(
    toolName string,
    toolCall *llmschema.ToolCall,
    request *saschema.InterruptRequest,
) {
    exc := &saschema.ToolInterruptException{
        Request:  request,
        ToolCall: toolCall,
    }
    panic(cb.NewAbortError(
        fmt.Sprintf("工具执行中断: %s", toolName),
        exc,
    ))
}

// skipTool 跳过工具执行，设置预设返回结果。
func (r *BaseInterruptRail) skipTool(
    cbc *agentinterfaces.AgentCallbackContext,
    toolInputs *agentinterfaces.ToolCallInputs,
    reject *RejectResult,
) {
    toolCallID := r.resolveToolCallID(toolInputs.ToolCall)
    cbc.Extra()["_skip_tool"] = true
    toolInputs.ToolResult = reject.ToolResult
    if reject.ToolMessage != nil {
        toolInputs.ToolMsg = reject.ToolMessage
    } else {
        toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, fmt.Sprintf("%v", reject.ToolResult))
    }
}
```

- [ ] **Step 5: 编写 getUserInput**

对齐 Python `_get_user_input`，从 `cbc.Extra()[ResumeUserInputKey]` 提取用户输入：
- 如果是 `*sessioninteraction.InteractiveInput`，按 `toolCallID` 从 `UserInputs` 查找
- 如果是 `map[string]any`，按 `toolCallID` 查找，找不到返回整个 map
- 其他类型直接返回

- [ ] **Step 6: 编写 GetCallbacks**

BaseInterruptRail 嵌入 BaseRail（非 DeepAgentRail），只需注册 BeforeToolCall：

```go
func (r *BaseInterruptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.BaseRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
        return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    return callbacks
}
```

- [ ] **Step 7: 编写 resolveInterrupt 默认实现（子类覆盖）**

```go
// resolveInterrupt 解析中断逻辑，子类覆盖。
// 默认实现：无用户输入→中断；有输入→允许。
func (r *BaseInterruptRail) resolveInterrupt(
    ctx context.Context,
    cbc *agentinterfaces.AgentCallbackContext,
    toolCall *llmschema.ToolCall,
    userInput any,
    autoConfirmConfig map[string]any,
) InterruptDecision {
    if userInput == nil {
        return r.Interrupt(&saschema.InterruptRequest{
            Message: "等待用户确认",
        })
    }
    return r.Approve("")
}
```

- [ ] **Step 8: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/interrupt/`
Expected: PASS

---

### Task 3: interrupt_base_test.go — BaseInterruptRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/interrupt_base_test.go`

- [ ] **Step 1: 编写测试**

测试用例覆盖：
1. `TestNewBaseInterruptRail` — 构造函数、默认优先级(90)、工具名注册
2. `TestBaseInterruptRail_AddTool/AddTools/GetTools` — 工具名增删查
3. `TestBaseInterruptRail_Approve/Reject/Interrupt` — 三种决策构造
4. `TestBaseInterruptRail_BeforeToolCall_未注册工具` — 不拦截，返回 nil
5. `TestBaseInterruptRail_BeforeToolCall_中断` — 无用户输入时抛出 AbortError(ToolInterruptException)
6. `TestBaseInterruptRail_BeforeToolCall_允许` — 有用户输入时放行
7. `TestBaseInterruptRail_BeforeToolCall_拒绝` — 返回 RejectResult 时设置 _skip_tool
8. `TestBaseInterruptRail_GetCallbacks` — 回调映射包含 BeforeToolCall
9. `TestBaseInterruptRail_getUserInput_InteractiveInput` — 从 InteractiveInput 提取
10. `TestBaseInterruptRail_getUserInput_Dict` — 从 map 提取
11. `TestBaseInterruptRail_applyDecision_ApproveResult_修改参数` — NewArgs 非空时修改 ToolArgs

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/interrupt/ -v -run TestBaseInterruptRail`
Expected: PASS

---

### Task 4: ask_user_rail.go — AskUserRail

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/ask_user_rail.go`

- [ ] **Step 1: 编写 AskUserPayload 和 AskUserRequest**

```go
// AskUserPayload 用户回答载荷。
// 对齐 Python: AskUserPayload
type AskUserPayload struct {
    // Answers 问题文本到回答的映射
    Answers map[string]string `json:"answers"`
}

// AskUserRequest 扩展 InterruptRequest，携带问题列表。
// 对齐 Python: AskUserRequest(InterruptRequest)
type AskUserRequest struct {
    // InterruptRequest 嵌入基础中断请求
    saschema.InterruptRequest
    // Questions 要向用户展示的问题列表
    Questions []map[string]any `json:"questions"`
}
```

- [ ] **Step 2: 编写 AskUserRail 结构体和构造函数**

```go
type AskUserRail struct {
    BaseInterruptRail
    // tools 已注册的 AskUserTool 引用，供 Uninit 注销
    tools []tool.Tool
}

func NewAskUserRail(toolNames ...string) *AskUserRail {
    // 默认拦截 "ask_user" 工具
    if len(toolNames) == 0 {
        toolNames = []string{"ask_user"}
    }
    r := &AskUserRail{
        BaseInterruptRail: *NewBaseInterruptRail(toolNames...),
    }
    return r
}
```

- [ ] **Step 3: 编写 Init/Uninit**

Init 模式对齐 McpRail：
1. 获取 language（从 agent.SystemPromptBuilder().Language()，fallback "cn"）
2. 获取 agentID（从 agent.Card().ID）
3. 调用 `tools.BuildToolCard("ask_user", "ask_user", language, nil, agentID)` 获取 ToolCard
4. 用 `tool.NewMapFunction` 创建空壳工具（Invoke 返回空 map，Stream 返回 error）
5. 注册到 `runner.GetResourceMgr().AddTool()` + `agent.AbilityManager().Add()`

Uninit 模式对齐 McpRail：
1. 遍历 r.tools，从 AbilityManager.Remove(name) + ResourceMgr.RemoveTool([]string{id})
2. r.tools = nil

- [ ] **Step 4: 编写 resolveInterrupt**

对齐 Python `AskUserRail.resolve_interrupt`：

```go
func (r *AskUserRail) resolveInterrupt(
    ctx context.Context,
    cbc *agentinterfaces.AgentCallbackContext,
    toolCall *llmschema.ToolCall,
    userInput any,
    autoConfirmConfig map[string]any,
) InterruptDecision {
    // 无用户输入 → 中断
    if userInput == nil {
        return r.Interrupt(r.buildAskRequest(toolCall))
    }

    // 解析用户输入为 AskUserPayload
    payload, ok := r.parseUserInput(userInput, toolCall)
    if !ok || len(payload.Answers) == 0 {
        return r.Interrupt(r.buildAskRequest(toolCall))
    }

    // 有有效输入 → Reject（跳过工具执行，返回格式化结果）
    toolResult := r.formatToolResult(toolCall, payload)
    return r.Reject(toolResult)
}
```

- [ ] **Step 5: 编写辅助方法**

- `parseUserInput(userInput, toolCall)` — 支持 AskUserPayload / map[string]any / string 三种输入格式
- `formatToolResult(toolCall, payload)` — 格式化为 `"User has answered your questions: ..."` 文本
- `buildAskRequest(toolCall)` — 构建 AskUserRequest（含 questions 和 payload_schema）
- `parseToolArgs(toolCall)` — 解析 ToolCall.Arguments JSON 为 map

- [ ] **Step 6: 编译时接口验证**

```go
var _ agentinterfaces.AgentRail = (*AskUserRail)(nil)
```

- [ ] **Step 7: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/interrupt/`
Expected: PASS

---

### Task 5: ask_user_rail_test.go — AskUserRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/ask_user_rail_test.go`

- [ ] **Step 1: 编写测试**

测试用例覆盖：
1. `TestNewAskUserRail` — 默认拦截 "ask_user"
2. `TestNewAskUserRail_自定义工具名` — 传入自定义工具名
3. `TestAskUserRail_resolveInterrupt_无输入中断` — userInput=nil → InterruptResult
4. `TestAskUserRail_resolveInterrupt_有效输入拒绝` — userInput=AskUserPayload → RejectResult + 格式化文本
5. `TestAskUserRail_resolveInterrupt_字符串输入` — userInput=string → 解析为 AskUserPayload
6. `TestAskUserRail_resolveInterrupt_空字符串中断` — userInput="" → InterruptResult
7. `TestAskUserRail_resolveInterrupt_Dict输入` — userInput=map → 解析
8. `TestAskUserRail_formatToolResult` — 格式化输出验证
9. `TestAskUserRail_parseToolArgs` — JSON 参数解析
10. `TestAskUserRail_buildAskRequest` — 请求构建验证

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/interrupt/ -v -run TestAskUserRail`
Expected: PASS

---

### Task 6: confirm_rail.go — ConfirmInterruptRail

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/confirm_rail.go`

- [ ] **Step 1: 编写 ConfirmPayload 和 ConfirmRequest**

```go
// ConfirmPayload 用户确认载荷。
// 对齐 Python: ConfirmPayload
type ConfirmPayload struct {
    // Approved 是否批准
    Approved bool `json:"approved"`
    // Feedback 反馈信息
    Feedback string `json:"feedback"`
    // AutoConfirm 是否自动确认（"始终允许"标记）
    AutoConfirm bool `json:"auto_confirm"`
}

// ConfirmRequest 确认请求配置。
// 对齐 Python: ConfirmRequest
type ConfirmRequest struct {
    // Message 向用户展示的确认消息
    Message string `json:"message"`
    // PayloadSchema 用户输入的数据结构定义
    PayloadSchema map[string]any `json:"payload_schema"`
}
```

- [ ] **Step 2: 编写 ConfirmInterruptRail 结构体和构造函数**

```go
type ConfirmInterruptRail struct {
    BaseInterruptRail
    // request 确认请求配置
    request ConfirmRequest
}

func NewConfirmInterruptRail(toolNames ...string) *ConfirmInterruptRail {
    r := &ConfirmInterruptRail{
        BaseInterruptRail: *NewBaseInterruptRail(toolNames...),
        request: ConfirmRequest{
            Message:       "请确认或拒绝?",
            PayloadSchema: confirmPayloadSchema(),
        },
    }
    return r
}
```

- [ ] **Step 3: 编写 resolveInterrupt**

对齐 Python `ConfirmInterruptRail.resolve_interrupt`：

```go
func (r *ConfirmInterruptRail) resolveInterrupt(
    ctx context.Context,
    cbc *agentinterfaces.AgentCallbackContext,
    toolCall *llmschema.ToolCall,
    userInput any,
    autoConfirmConfig map[string]any,
) InterruptDecision {
    autoConfirmKey := r.getAutoConfirmKey(toolCall)

    // 无用户输入
    if userInput == nil {
        // 检查 auto_confirm
        if isAutoConfirmed(autoConfirmConfig, autoConfirmKey) {
            return r.Approve("")
        }
        return r.Interrupt(&saschema.InterruptRequest{
            Message:        r.request.Message,
            PayloadSchema:  r.request.PayloadSchema,
            AutoConfirmKey: autoConfirmKey,
        })
    }

    // 解析用户输入为 ConfirmPayload
    payload, ok := r.parseConfirmInput(userInput)
    if !ok {
        return r.Interrupt(&saschema.InterruptRequest{
            Message:        r.request.Message,
            PayloadSchema:  r.request.PayloadSchema,
            AutoConfirmKey: autoConfirmKey,
        })
    }

    // approved → Approve; !approved → Reject(feedback)
    if payload.Approved {
        return r.Approve("")
    }
    feedback := payload.Feedback
    if feedback == "" {
        feedback = "用户反馈: 拒绝操作"
    }
    return r.Reject(feedback)
}
```

- [ ] **Step 4: 编写辅助方法**

- `getAutoConfirmKey(toolCall)` — 返回 `toolCall.Name` 作为 auto_confirm key
- `parseConfirmInput(userInput)` — 支持 ConfirmPayload / map[string]any 输入格式
- `isAutoConfirmed(config, key)` — 检查 config[key] 是否为 true
- `confirmPayloadSchema()` — 返回 ConfirmPayload 的 JSON Schema

- [ ] **Step 5: 编译时接口验证**

```go
var _ agentinterfaces.AgentRail = (*ConfirmInterruptRail)(nil)
```

- [ ] **Step 6: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/interrupt/`
Expected: PASS

---

### Task 7: confirm_rail_test.go — ConfirmInterruptRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/confirm_rail_test.go`

- [ ] **Step 1: 编写测试**

测试用例覆盖：
1. `TestNewConfirmInterruptRail` — 构造函数、优先级
2. `TestConfirmInterruptRail_resolveInterrupt_无输入无AutoConfirm` → InterruptResult
3. `TestConfirmInterruptRail_resolveInterrupt_无输入有AutoConfirm` → ApproveResult
4. `TestConfirmInterruptRail_resolveInterrupt_批准` → ApproveResult
5. `TestConfirmInterruptRail_resolveInterrupt_拒绝` → RejectResult（含 feedback）
6. `TestConfirmInterruptRail_resolveInterrupt_拒绝无Feedback` → RejectResult（默认消息）
7. `TestConfirmInterruptRail_resolveInterrupt_无效输入` → InterruptResult
8. `TestConfirmInterruptRail_isAutoConfirmed` — auto_confirm 查找逻辑
9. `TestConfirmInterruptRail_getAutoConfirmKey` — key 生成
10. `TestConfirmInterruptRail_parseConfirmInput_Dict` — map 输入解析

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/interrupt/ -v -run TestConfirmInterruptRail`
Expected: PASS

---

### Task 8: 回填 — factory.go / doc.go / IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/harness/factory.go:530-583`
- Modify: `internal/agentcore/harness/rails/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md:566`

- [ ] **Step 1: 修改 factory.go addDefaultRails**

在 SecurityRail 占位之后、SysOperationRail 之前，添加 AskUserRail 和 ConfirmInterruptRail 的自动注册：

```go
// AskUserRail — 始终添加（拦截 ask_user 工具）
if !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&interrupt.AskUserRail{})) {
    agent.AddRail(interrupt.NewAskUserRail())
    logger.Debug(logComponent).Msg("已添加 AskUserRail")
}

// ConfirmInterruptRail — 对危险工具添加确认拦截
if !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&interrupt.ConfirmInterruptRail{})) {
    // 不自动添加，由具体场景（如 CLI）显式提供
    // 例如: interrupt.NewConfirmInterruptRail("write_file", "edit_file")
}
```

同时更新 import 添加 interrupt 包引用。

- [ ] **Step 2: 修改 rails/doc.go**

在包注释中添加 interrupt 子包说明，文件目录中添加 `interrupt/` 条目。

- [ ] **Step 3: 修改 IMPLEMENTATION_PLAN.md**

将 `| 9.19-24 | ☐ |` 改为部分完成状态（9.20 InterruptRail 完成，其余仍 ☐）。

- [ ] **Step 4: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`
Expected: PASS

- [ ] **Step 5: 运行全部 interrupt 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/interrupt/ -v -cover`
Expected: PASS, 覆盖率 ≥ 85%

---

### Task 9: 最终验证

- [ ] **Step 1: 运行 harness 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -count=1`
Expected: PASS

- [ ] **Step 2: 运行 interrupt 子包覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/interrupt/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 提交**

```
feat(interrupt): 实现 InterruptRail 中断-恢复护栏 (9.20)

- BaseInterruptRail: 中断-恢复基类，BeforeToolCall 拦截 + 三种决策(approve/reject/interrupt)
- AskUserRail: 拦截 ask_user 工具，向用户提问等待回答
- ConfirmInterruptRail: 确认中断，支持 auto_confirm 机制
- 放在独立子包 rails/interrupt/ 下
- 回填 factory.go addDefaultRails 添加 AskUserRail 自动注册
```
