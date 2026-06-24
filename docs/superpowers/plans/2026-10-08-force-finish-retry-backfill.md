# 6.10 ForceFinishRequest / RetryRequest 回填实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 回填 6.10 小节中 3 处 Python 未对齐项 + 1 组冗余类型清理，使 Go 的 ForceFinishRequest / RetryRequest 完全对齐 Python。

**Architecture:** A 项在 `rail/context.go` 添加负数保护；B 项为 `AbilityManager.Execute` 引入 `AgentCallbackContext` 参数、子上下文 Fork、force-finish 传播；C 项用 `ToolCallRail.Execute` 包装工具调用；D 项删除从未使用的 ToolRail/ToolCallContext/ToolCallResult。

**Tech Stack:** Go 1.23+, testify/assert, project rail/ability/agents 包

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/agentcore/single_agent/rail/context.go` | A: RequestRetry 负数保护；B-2: ForkForToolCall |
| Modify | `internal/agentcore/single_agent/rail/context_test.go` | A/B-2 测试 |
| Modify | `internal/agentcore/single_agent/ability/ability_types.go` | D: 删除 ToolRail/ToolCallContext/ToolCallResult |
| Modify | `internal/agentcore/single_agent/ability/ability_manager.go` | B-1: Execute 签名加 cbc；B-3: 传播；C: Rail 包装；D: 删 rail 字段 |
| Modify | `internal/agentcore/single_agent/ability/ability_manager_test.go` | B/C/D 测试；更新现有 Execute 测试 |
| Modify | `internal/agentcore/single_agent/agents/react_agent.go` | B-1: 调用方传入 cbc |
| Modify | `internal/agentcore/single_agent/base.go` | D: 删除 ToolRail re-export |
| Modify | `internal/agentcore/single_agent/ability/doc.go` | D: 更新文档 |
| Modify | `internal/agentcore/single_agent/doc.go` | D: 更新文档 |

---

### Task 1: A — RequestRetry 负数保护

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go:281-283`
- Test: `internal/agentcore/single_agent/rail/context_test.go`

- [ ] **Step 1: 写失败测试**

在 `rail/context_test.go` 的 `Retry / ForceFinish` 区块末尾添加：

```go
// TestRequestRetry_负数归零 验证负数 delaySeconds 被静默归零
func TestRequestRetry_负数归零(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	ctx.RequestRetry(-1.5)
	req := ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 0.0, req.DelaySeconds)

	// 正数不受影响
	ctx.RequestRetry(3.0)
	req = ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 3.0, req.DelaySeconds)

	// 零不受影响
	ctx.RequestRetry(0.0)
	req = ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 0.0, req.DelaySeconds)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/ -run TestRequestRetry_负数归零 -v`
Expected: FAIL（负数输入返回 -1.5 而非 0.0）

- [ ] **Step 3: 实现**

修改 `rail/context.go` 第 281-283 行：

```go
// RequestRetry 请求重试。
//
// 在 on_model_exception / on_tool_exception 钩子内调用。
// 负数 delaySeconds 被静默归零（对齐 Python: if delay_seconds < 0: delay_seconds = 0.0）。
// 对应 Python: AgentCallbackContext.request_retry(delay_seconds)
func (c *AgentCallbackContext) RequestRetry(delaySeconds float64) {
	if delaySeconds < 0 {
		delaySeconds = 0
	}
	c.retryRequest = &RetryRequest{DelaySeconds: delaySeconds}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/ -run TestRequestRetry -v`
Expected: PASS（所有 RequestRetry 测试通过）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): RequestRetry 负数归零保护，对齐 Python request_retry"
```

---

### Task 2: B-2 — ForkForToolCall 子上下文方法

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go`
- Test: `internal/agentcore/single_agent/rail/context_test.go`

- [ ] **Step 1: 写失败测试**

在 `rail/context_test.go` 末尾（非导出函数区块前）添加：

```go
// TestForkForToolCall_字段共享与隔离 验证 ForkForToolCall 的共享/独立语义
func TestForkForToolCall_字段共享与隔离(t *testing.T) {
	agent := &fakeRailAgent{agentID: "test-agent"}
	sess := session.NewSession()
	parentInputs := &InvokeInputs{}
	parent := NewAgentCallbackContext(agent, parentInputs, sess)
	q := make(chan string, steeringQueueSize)
	parent.BindSteeringQueue(q)
	parent.Extra()["shared_key"] = "shared_val"

	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "search", Arguments: `{"q":"hello"}`}
	child := parent.ForkForToolCall(toolCall)

	// 共享字段
	assert.Equal(t, agent, child.Agent())                    // agent 引用共享
	assert.Equal(t, sess, child.Session())                   // session 引用共享
	assert.Equal(t, parent.Extra(), child.Extra())           // extra 字典引用共享
	assert.Equal(t, parent.SteeringQueue(), child.SteeringQueue()) // steeringQueue 引用共享

	// 独立字段
	assert.Nil(t, child.ConsumeRetryRequest())               // retryRequest 独立零值
	assert.False(t, child.HasForceFinishRequest())           // forceFinishRequest 独立零值
	assert.Nil(t, child.Exception())                         // exception 独立零值
	assert.Equal(t, 0, child.RetryAttempt())                 // retryAttempt 独立零值

	// inputs 为 ToolCallInputs
	inputs, ok := child.Inputs().(*ToolCallInputs)
	assert.True(t, ok)
	assert.Equal(t, toolCall, inputs.ToolCall)
	assert.Equal(t, "search", inputs.ToolName)

	// extra 修改互相可见（引用共享）
	child.Extra()["child_key"] = "child_val"
	assert.Equal(t, "child_val", parent.Extra()["child_key"])

	// force-finish 独立：子 ctx 设置不影响父
	child.RequestForceFinish(map[string]any{"reason": "budget_exceeded"})
	assert.True(t, child.HasForceFinishRequest())
	assert.False(t, parent.HasForceFinishRequest())
}
```

需要在测试文件顶部 import 中添加：
```go
import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/ -run TestForkForToolCall -v`
Expected: FAIL（ForkForToolCall 方法不存在，编译错误）

- [ ] **Step 3: 实现**

在 `rail/context.go` 的 `HasForceFinishRequest` 方法之后（导出函数区块）、非导出函数区块之前添加：

```go
// ForkForToolCall 为单个工具调用创建隔离的子上下文。
//
// 共享字段（引用共享，跨 rail 通信）：
//   - agent、extra、steeringQueue、session、config、modelContext
//
// 独立字段（每个工具调用各自持有零值）：
//   - retryRequest、forceFinishRequest、exception、retryAttempt、event、inputs
//
// 对应 Python: AbilityManager.execute 中 tool_ctx = AgentCallbackContext(
//
//	agent=ctx.agent, inputs=ToolCallInputs(...), config=ctx.config,
//	session=session, context=ctx.context, extra=ctx.extra,
//
// )
func (c *AgentCallbackContext) ForkForToolCall(toolCall *llmschema.ToolCall) *AgentCallbackContext {
	return &AgentCallbackContext{
		agent:   c.agent,
		inputs: &ToolCallInputs{
			ToolCall: toolCall,
			ToolName: toolCall.Name,
			ToolArgs: toolCall.Arguments,
		},
		config:        c.config,
		session:       c.session,
		modelContext:  c.modelContext,
		extra:         c.extra,
		steeringQueue: c.steeringQueue,
	}
}
```

在 `rail/context.go` 顶部 import 中添加：
```go
llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/ -run TestForkForToolCall -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): ForkForToolCall 子上下文方法，共享 extra/steering，独立 retry/forceFinish"
```

---

### Task 3: D — 删除冗余类型（ToolRail + ToolCallContext + ToolCallResult）

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_types.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager_test.go`
- Modify: `internal/agentcore/single_agent/base.go`
- Modify: `internal/agentcore/single_agent/ability/doc.go`
- Modify: `internal/agentcore/single_agent/doc.go`

- [ ] **Step 1: 修改 ability_types.go**

删除 `ToolRail` 接口（L14-22）、`ToolCallContext` 结构体（L55-71）、`ToolCallResult` 结构体（L73-79）。
删除 `rail` 导入（L8）。最终 `ability_types.go` 变为：

```go
package ability

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AddAbilityResult 添加能力的返回结果。
//
// 对应 Python: AddAbilityResult
type AddAbilityResult struct {
	// Name 能力名称
	Name string
	// Added 是否成功添加
	Added bool
	// Reason 未添加的原因（如 "duplicate_tool"、"added_tool"）
	Reason string
}

// AbilityExecutionError 能力执行统一异常，嵌入 BaseError 并关联 ToolMessage。
//
// 对应 Python: AbilityExecutionError
type AbilityExecutionError struct {
	*exception.BaseError
	// ToolMessage 关联的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
	// Result 执行结果
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
	// Err 执行错误（如有）
	Err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityExecutionError 创建能力执行错误。
//
// 对应 Python: AbilityExecutionError(status=..., msg=..., tool_message=...)
func NewAbilityExecutionError(
	status exception.StatusCode,
	toolCallID string,
	msg string,
	opts ...exception.ErrorOption,
) *AbilityExecutionError {
	allOpts := append([]exception.ErrorOption{exception.WithMsg(msg)}, opts...)
	return &AbilityExecutionError{
		BaseError:   exception.NewBaseError(status, allOpts...),
		ToolMessage: llmschema.NewToolMessage(toolCallID, msg),
	}
}

// BuildToolMessageContent 从执行结果中提取 ToolMessage 的 content 字段。
//
// 提取逻辑（对齐 Python _build_tool_message_content）：
//  1. 结果有 data.content 字段 → 返回 content
//  2. 结果 success=false 且有 error → 返回 error
//  3. 其他 → 字符串化结果
func BuildToolMessageContent(result any) string {
	if m, ok := result.(map[string]any); ok {
		if data, ok := m["data"].(map[string]any); ok {
			if content, ok := data["content"]; ok {
				s := fmt.Sprintf("%v", content)
				if s != "" {
					return s
				}
			}
		}
		if success, ok := m["success"].(bool); ok && !success {
			if errVal, ok := m["error"]; ok {
				return fmt.Sprintf("%v", errVal)
			}
		}
	}
	return fmt.Sprintf("%v", result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 修改 ability_manager.go**

删除 `rail ToolRail` 字段（L48-49）和 `SetRail` 方法（L87-90）。

结构体变为：

```go
type AbilityManager struct {
	// mu 读写锁
	mu sync.RWMutex
	// tools 工具注册表
	tools map[string]*tool.ToolCard
	// workflows 工作流注册表
	workflows map[string]*schema.WorkflowCard
	// agents Agent 注册表
	agents map[string]*agentschema.AgentCard
	// mcpServers MCP 服务器注册表
	mcpServers map[string]*mcp.McpServerConfig
	// contextEngine 上下文引擎
	contextEngine iface.ContextEngine
	// resourceMgr 资源管理器
	resourceMgr resource.ResourceManager
}
```

删除 `SetRail` 方法。`NewAbilityManager` 不变。

- [ ] **Step 3: 修改 ability_manager_test.go**

删除 `TestAbilityManager_SetRail` 测试（L519-522）。

- [ ] **Step 4: 修改 base.go**

删除 L63-64 的 ToolRail re-export：

```go
// 删除这两行：
// ToolRail 工具调用钩子接口（re-export from ability 子包）
ToolRail = ability.ToolRail
```

- [ ] **Step 5: 修改 ability/doc.go**

将 L12 的 `ToolRail 预留` 描述更新：

```go
//	ability/
//	├── doc.go               # 包文档
//	├── ability_manager.go   # AbilityManager 核心结构 + 注册/查询/执行
//	├── ability_types.go     # Ability 联合类型 + AddAbilityResult + AbilityExecutionError
//	└── json_repair.go       # RepairToolArgumentsJSON + ParseToolArguments
```

- [ ] **Step 6: 修改 single_agent/doc.go**

将 L18 的 `ToolRail 预留` 描述更新：

```go
//	│   ├── ability_types.go   # Ability 联合类型 + AddAbilityResult + AbilityExecutionError
```

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译通过（无 ToolRail/ToolCallContext/ToolCallResult 引用残留）

- [ ] **Step 8: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/ability/ -v`
Expected: PASS（SetRail 测试已删除，其他测试不受影响）

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor(ability): 删除冗余 ToolRail/ToolCallContext/ToolCallResult，统一用 AgentRail + AgentCallbackContext"
```

---

### Task 4: B-1 + B-3 + C — AbilityManager.Execute 接收 cbc + Rail 包装 + force-finish 传播

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager_test.go`
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`

- [ ] **Step 1: 修改 ability_manager.go 的 import**

在 import 块中添加 `rail` 包：

```go
import (
	"context"
	"fmt"
	"strings"
	"sync"

	iface "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

- [ ] **Step 2: 修改 Execute 方法签名和实现**

替换 `Execute` 方法（L361-393）：

```go
// Execute 并行执行多个 ToolCall，返回每个调用的结果。
// 使用 WaitGroup + 按 index 写切片，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误作为 ExecuteResult.Err 返回。
// 结果顺序与输入 toolCalls 顺序一致。
//
// cbc 为 Rail 系统的 AgentCallbackContext，用于：
//   - 为每个 tool_call 创建隔离子上下文（ForkForToolCall）
//   - 传播子上下文的 force-finish 信号回父 cbc
//
// 对应 Python: AbilityManager.execute(ctx, tool_call, session, tag)
func (am *AbilityManager) Execute(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess *session.Session,
	tag string,
) []ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	am.mu.RLock()
	results := make([]ExecuteResult, len(toolCalls))

	// 为每个 tool_call 创建隔离子上下文
	// 对应 Python: tool_ctx = AgentCallbackContext(agent=ctx.agent, inputs=ToolCallInputs(...), extra=ctx.extra, ...)
	toolCtxs := make([]*rail.AgentCallbackContext, len(toolCalls))
	for i, tc := range toolCalls {
		toolCtxs[i] = cbc.ForkForToolCall(tc)
	}

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall *llmschema.ToolCall, toolCtx *rail.AgentCallbackContext) {
			defer wg.Done()
			results[idx] = am.railedExecuteSingleToolCall(ctx, toolCtx, toolCall, sess, tag)
		}(i, tc, toolCtxs[i])
	}
	am.mu.RUnlock()

	wg.Wait()

	// force-finish 信号传播：子 toolCtx → 父 cbc
	// 对应 Python: for tool_ctx in tool_contexts:
	//   ff = tool_ctx.consume_force_finish()
	//   if ff is not None: ctx.request_force_finish(ff.result); break
	for _, toolCtx := range toolCtxs {
		if ff := toolCtx.ConsumeForceFinish(); ff != nil {
			cbc.RequestForceFinish(ff.Result)
			break
		}
	}

	return results
}
```

- [ ] **Step 3: 修改 railedExecuteSingleToolCall 方法**

替换 `railedExecuteSingleToolCall` 方法（L397-414）：

```go
// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
//
// 使用 ToolCallRail.Execute 包装，自动提供：
//   - fire(BEFORE_TOOL_CALL) → before 钩子
//   - force-finish 门控 → 可跳过工具执行
//   - 异常 → fire(ON_TOOL_EXCEPTION) → 可 request_retry() 重试
//   - fire(AFTER_TOOL_CALL) → after 钩子
//
// 对应 Python: @rail(before=BEFORE_TOOL_CALL, after=AFTER_TOOL_CALL, on_exception=ON_TOOL_EXCEPTION)
//
//	async def _railed_execute_single_tool_call(self, ctx, tool_call, session, tag=None): ...
func (am *AbilityManager) railedExecuteSingleToolCall(
	ctx context.Context,
	toolCtx *rail.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess *session.Session,
	tag string,
) ExecuteResult {
	var result ExecuteResult
	_ = rail.ToolCallRail.Execute(ctx, toolCtx, func() error {
		result = am.executeSingleToolCall(ctx, toolCall, sess, tag)
		return result.Err
	})
	return result
}
```

- [ ] **Step 4: 修改 react_agent.go 调用方**

修改 `executeToolCalls` 方法中 L502 行：

```go
// 当前：results, err := am.Execute(ctx, toolCalls, sess)
// 改为：
results, err := am.Execute(ctx, cbc, toolCalls, sess)
```

但注意 `Execute` 当前返回 `[]ExecuteResult`，不是 `([]ExecuteResult, error)`。查看 L502 的实际代码：

```go
results := am.Execute(ctx, cbc, toolCalls, sess, "")
```

需确认实际调用代码。当前 L502 为 `results, err := am.Execute(ctx, toolCalls, sess)`——但 Execute 返回 `[]ExecuteResult` 没有 error。需要重新检查：

实际 react_agent.go L502 为：
```go
results, err := am.Execute(ctx, toolCalls, sess)
```

但当前 `Execute` 签名返回 `[]ExecuteResult`（无 error）。这说明可能有一个旧签名或编译不通过。改为正确调用：

```go
results := am.Execute(ctx, cbc, toolCalls, sess, "")
```

同时删除下方的 `if err != nil` 错误处理块（L503-505），因为 `Execute` 不返回 error。

修改后 `executeToolCalls` 方法为：

```go
// executeToolCalls 执行工具调用列表。
func (a *ReActAgent) executeToolCalls(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []llmschema.ToolCall,
	sess *session.Session,
	modelCtx ceinterface.ModelContext,
) ([]ability.ExecuteResult, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	for _, tc := range toolCalls {
		argsPreview := tc.Arguments
		if len(argsPreview) > 100 {
			argsPreview = argsPreview[:100]
		}
		logger.Info(logComponent).
			Str("event_type", "tool_call").
			Str("tool_name", tc.Name).
			Str("args_preview", argsPreview).
			Msg("执行工具调用")
	}

	am := a.getAbilityManager()
	if am == nil {
		return nil, fmt.Errorf("AbilityManager 未初始化")
	}

	results := am.Execute(ctx, cbc, toolCalls, sess, "")

	for _, r := range results {
		if r.ToolMsg != nil && modelCtx != nil {
			modelCtx.AddMessage(r.ToolMsg)
		}
	}

	return results, nil
}
```

- [ ] **Step 5: 更新 ability_manager_test.go 中现有 Execute 测试**

所有现有 `Execute` 测试调用（如 `TestAbilityManager_Execute_单工具成功` 等）需要更新签名——传入 `nil` 作为 `cbc` 参数（不使用 Rail 功能的简单测试不需要 AgentCallbackContext）。

需要逐个搜索并更新 `am.Execute(ctx,` 调用。搜索模式：`am.Execute(ctx, toolCalls,` → `am.Execute(ctx, nil, toolCalls,`。

具体修改：在测试文件中，所有 `am.Execute(ctx, toolCalls, sess, "")` 改为 `am.Execute(ctx, nil, toolCalls, sess, "")`。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译通过

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/ability/ -v && go test ./internal/agentcore/single_agent/agents/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(ability): Execute 接收 AgentCallbackContext + ToolCallRail 包装 + force-finish 传播"
```

---

### Task 5: B/C 测试补充 + 全量回归

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager_test.go`

- [ ] **Step 1: 添加 force-finish 传播测试**

在 `ability_manager_test.go` 末尾添加：

```go
// TestAbilityManager_Execute_forceFinish传播 验证子 toolCtx 的 force-finish 信号传播到父 cbc
func TestAbilityManager_Execute_forceFinish传播(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_ff_prop")
	defer mgr.Clear()

	// 注册 after_tool_call 钩子，在第一个工具调用后设置 force-finish
	callCount := 0
	mgr.RegisterCallback(context.Background(), rail.CallbackAfterToolCall, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*rail.AgentCallbackContext)
		callCount++
		if callCount == 1 {
			cbc.RequestForceFinish(map[string]any{"reason": "budget_exceeded"})
		}
		return nil
	})

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	results := am.Execute(context.Background(), cbc, toolCalls, nil, "")
	// 工具调用应正常执行（即使触发了 force-finish）
	_ = results

	// 父 cbc 应收到 force-finish 信号
	assert.True(t, cbc.HasForceFinishRequest())
	finish := cbc.ConsumeForceFinish()
	assert.NotNil(t, finish)
	assert.Equal(t, "budget_exceeded", finish.Result["reason"])
}
```

需要在测试文件中定义 `fakeRailAgentForAbility` 辅助类型（如果不存在）：

```go
// fakeRailAgentForAbility 实现 rail.RailAgent 接口，用于 ability 测试
type fakeRailAgentForAbility struct {
	cbMgr   *rail.AgentCallbackManager
	agentID string
}

func (f *fakeRailAgentForAbility) CallbackManager() *rail.AgentCallbackManager { return f.cbMgr }
func (f *fakeRailAgentForAbility) AgentID() string                            { return f.agentID }
```

- [ ] **Step 2: 添加 Rail 包装测试**

```go
// TestAbilityManager_Execute_Rail包装 验证 ToolCallRail 自动触发 before/after 钩子
func TestAbilityManager_Execute_Rail包装(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_rail_wrap")
	defer mgr.Clear()

	var firedEvents []rail.AgentCallbackEvent
	registerHook := func(event rail.AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, railCtx any) error {
			cbc := railCtx.(*rail.AgentCallbackContext)
			firedEvents = append(firedEvents, cbc.Event())
			return nil
		})
	}
	registerHook(rail.CallbackBeforeToolCall)
	registerHook(rail.CallbackAfterToolCall)

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	_ = am.Execute(context.Background(), cbc, toolCalls, nil, "")

	assert.Contains(t, firedEvents, rail.CallbackBeforeToolCall)
	assert.Contains(t, firedEvents, rail.CallbackAfterToolCall)
}
```

- [ ] **Step 3: 运行新测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/ability/ -run "TestAbilityManager_Execute_forceFinish传播|TestAbilityManager_Execute_Rail包装" -v`
Expected: PASS

- [ ] **Step 4: 全量回归测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/ability/ability_manager_test.go
git commit -m "test(ability): force-finish 传播 + ToolCallRail 包装集成测试"
```

---

### Task 6: IMPLEMENTATION_PLAN.md 状态同步 + doc.go 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

6.10 小节当前已完成（✅），本回填不改变状态标记，但需要确认 ⤵️ 标记已清除。
搜索 `6.10` 相关行，确认状态仍为 ✅。
搜索 `⤵️ 预留` 中与 ToolRail / force_finish 传播 / BeforeToolCall / AfterToolCall 相关的标记，确认代码中已清除。

- [ ] **Step 2: 运行全量编译+测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test ./internal/agentcore/single_agent/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 同步 6.10 回填完成状态"
```
