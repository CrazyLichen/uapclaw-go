# 上游缺陷同步 A 组 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把上游 agent-core A 组 12 条缺陷修复同步到 Go 实现，使 ReAct 循环的终止语义、流式异常处理、中断持久化与防挂死守卫与上游 HEAD 一致。

**Architecture:** 按包分层落地——`single_agent/{agents,ability,interfaces}` 承担循环与工具执行语义，`foundation/llm` 承担流式包装与响应校验所需的数据，`harness/{schema,task_loop,rails/skills,deep_agent.go}` 承担配置语义与任务循环守卫。每条缺陷一个任务，TDD 顺序：失败测试 → 最小实现 → 通过 → 提交。

**Tech Stack:** Go 1.22+、`testify`、项目内 `internal/common/exception`、`internal/common/logger`（`logger.Msg` 英文、注释中文）。

**Spec:** `docs/superpowers/specs/2026-11-04-upstream-defect-group-a-design.md`

---

## 文件结构（改动面）

| 文件 | 责任 | 涉及任务 |
| --- | --- | --- |
| `internal/agentcore/single_agent/interfaces/callback.go` | 回调事件枚举 + AgentRail 钩子 + 事件→方法映射 | 1 |
| `internal/agentcore/single_agent/agents/react_prompt.go` | 系统提示词区段（技能小节） | 2 |
| `internal/agentcore/single_agent/agents/react_invoke.go` | ReAct 主循环、边界收尾、空响应重试、中断持久化 | 1, 4, 5, 8 |
| `internal/agentcore/single_agent/agents/react_model_call.go` | 模型调用 + 响应校验 + 拒图降级 | 3, 4, 11 |
| `internal/agentcore/single_agent/ability/ability_manager.go` | 工具批量执行与 force-finish 传播 | 6 |
| `internal/agentcore/foundation/llm/model.go` | 模型门面流包装 | 7 |
| `internal/agentcore/session/tracer/decorator/decorator.go` | 追踪层流包装 | 7 |
| `internal/agentcore/foundation/llm/schema/message.go` | 消息来源元数据常量（从 harness 搬迁） | 4 |
| `internal/agentcore/foundation/llm/model_clients/openai/{types.go,client.go}` | 流内 `error` 事件 + provider code 保留 | 11 |
| `internal/agentcore/harness/schema/config.go` | DeepAgent 迭代语义 + 任务循环守卫 + 技能预算配置 | 9, 10, 12 |
| `internal/agentcore/harness/deep_agent.go` | 内层 ReAct 装配 + 外层轮次上限 + 评估器装配 | 9, 10 |
| `internal/agentcore/harness/task_loop/stop_condition.go` | 无进展停止评估器 | 9 |
| `internal/agentcore/harness/{plan,explore,research,code,browser}_agent_factory.go` | 子代理默认迭代数 | 10 |
| `internal/agentcore/harness/rails/skills/skill_use_rail.go` | 技能预算 | 12 |
| `docs/sync/defect-plan-2026-11-04.md`、`IMPLEMENTATION_PLAN.md` | 台账与计划状态 | 13 |

**编译/测试前必读（项目规则 5）**：`export GOPROXY=https://goproxy.cn,direct`；每次编译前 `pgrep -f 'go (build|test)'` 确认无残留进程。

---

## Task 1: A-01 迭代预算耗尽时消费 boundary force_finish

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/callback.go`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:639-766`（`reactLoop`）
- Test: `internal/agentcore/single_agent/agents/react_agent_test.go`（追加用例）

- [ ] **Step 1: 新增事件枚举与 Rail 钩子**

`callback.go` 常量区（`CallbackOnToolException` 之后）：

```go
	// CallbackAfterReactIteration 内层 ReAct 单次迭代完全成功后（模型 + 全部工具 + ToolMessage 均已写入）
	CallbackAfterReactIteration AgentCallbackEvent = "after_react_iteration"
```

`AgentRail` 接口在 `OnToolException` 之后追加：

```go
	// AfterReactIteration 内层 ReAct 单次迭代完全成功后
	AfterReactIteration(ctx context.Context, cbc *AgentCallbackContext) error
```

`BaseRail` 非导出区追加空实现：

```go
// AfterReactIteration 默认空实现。
func (r *BaseRail) AfterReactIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }
```

`BaseEventMethodMap` 追加一行：

```go
	CallbackAfterReactIteration: "AfterReactIteration",
```

`AllCallbackEvents()` 在 `CallbackOnToolException` 之后追加 `CallbackAfterReactIteration,`。

- [ ] **Step 2: 运行枚举测试，确认失败**

Run: `go test ./internal/agentcore/single_agent/interfaces/ -run TestAllCallbackEvents -v`
Expected: FAIL（断言事件数量/method map 覆盖的用例未包含新值）

- [ ] **Step 3: 更新既有枚举测试**

`event_test.go` 的枚举表追加：

```go
		{CallbackAfterReactIteration, "after_react_iteration"},
```

并把任何断言 `len(AllCallbackEvents()) == 11` 改为 `12`（`grep -n "AllCallbackEvents()" internal/agentcore/single_agent/interfaces/*_test.go` 定位全部断言）。

- [ ] **Step 4: 改 `reactLoop` —— 轮首消费 + 轮末触发 + 边界消费**

`react_invoke.go` 中 `reactLoop` 的循环体结构改为（保留原有 callModel / 工具执行 / HITL 逻辑，仅插入三处）：

```go
	for iteration := startIteration; iteration < maxIter; iteration++ {
		logger.Info(logComponent).
			Int("iteration", iteration+1).
			Msg("ReAct iteration")

		// 轮首消费：上一轮 after_react_iteration 钩子请求的 force_finish
		// Python: react_agent.py L3100-3108
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(ctx, sess)
			iterResult = finish.Result
			break
		}

		// steering 注入 ...（原逻辑不动）

		aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools, sess)
		// ... 原逻辑不动（含强制完成 #1、AddMessages、无工具调用分支、工具执行、强制完成 #2、HITL）...

		// 单轮完全成功（模型 + 全部工具 + ToolMessage 已写入）后触发
		// Python: react_agent.py L3241
		if err := cbc.Fire(ctx, interfaces.CallbackAfterReactIteration); err != nil {
			logger.Warn(logComponent).
				Str("event", string(interfaces.CallbackAfterReactIteration)).
				Err(err).
				Msg("after_react_iteration callback errored")
		}
	}
```

循环尾部（`if iterResult == nil` 处）替换为：

```go
	if iterResult == nil {
		// for-else 等价物：预算耗尽时先消费最后一轮 after_react_iteration 设置的 force_finish
		// Python: react_agent.py L3243-3252
		a.saveContexts(ctx, sess)
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			iterResult = finish.Result
		} else {
			iterResult = map[string]any{
				"output":                      "Max iterations reached without completion",
				"result_type":                 "error",
				"termination_reason":          "max_iterations",
				"iterations_used":             maxIter,
				"truncated_by_max_iterations": true,
				"max_iterations":              maxIter,
				"finalization_attempted":      false,
				"finalization_succeeded":      false,
			}
			logger.Warn(logComponent).
				Int("iterations_used", maxIter).
				Int("max_iterations", maxIter).
				Str("strategy", "error").
				Msg("ReAct iteration limit reached")
		}
	}
```

- [ ] **Step 5: 写失败测试**

注意：`after_react_iteration` 只在「模型产出 tool_calls 且工具全部执行完」的轮次触发（无工具调用时直接 answer/break），因此测试必须构造一个**工具轮**。用附录 A.6 的 `stubAbilityManager` 注入一个不落盘的桩，模型脚本返回带 tool_calls 的消息。

```go
// TestReActAgent_reactLoop_boundaryForceFinish 验证最后一轮 after_react_iteration 的 force_finish 不被丢弃
func TestReActAgent_reactLoop_boundaryForceFinish(t *testing.T) {
	toolCallMsg := llmschema.NewAssistantMessage("")
	toolCallMsg.ToolCalls = []*llmschema.ToolCall{{ID: "call_1", Name: "read_file", Arguments: "{}"}}

	agent := newAgentWithScriptedModel([]*llmschema.AssistantMessage{toolCallMsg}, 1)
	agent.SetAbilityManager(newStubAbilityManager()) // 附录 A.6
	fmc := &fakeModelContext{}
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}
	cbc, sess := newLoopCBC(t, agent, "loop_boundary_ff_sess")

	expected := map[string]any{"output": "graceful abort", "result_type": "answer"}
	agent.CallbackManager().RegisterCallback(context.Background(),
		interfaces.CallbackAfterReactIteration,
		func(_ context.Context, railCtx any) error {
			if c, ok := railCtx.(*interfaces.AgentCallbackContext); ok {
				c.RequestForceFinish(expected)
			}
			return nil
		})

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

// TestReActAgent_reactLoop_maxIterationsResultFields 验证耗尽预算时的错误结果字段
func TestReActAgent_reactLoop_maxIterationsResultFields(t *testing.T) {
	toolCallMsg := llmschema.NewAssistantMessage("")
	toolCallMsg.ToolCalls = []*llmschema.ToolCall{{ID: "call_1", Name: "read_file", Arguments: "{}"}}

	agent := newAgentWithScriptedModel([]*llmschema.AssistantMessage{toolCallMsg}, 1)
	agent.SetAbilityManager(newStubAbilityManager())
	fmc := &fakeModelContext{}
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}
	cbc, sess := newLoopCBC(t, agent, "loop_max_fields_sess")

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.Equal(t, "error", result["result_type"])
	assert.Equal(t, "Max iterations reached without completion", result["output"])
	assert.Equal(t, "max_iterations", result["termination_reason"])
	assert.Equal(t, true, result["truncated_by_max_iterations"])
	assert.Equal(t, 1, result["max_iterations"])
	assert.Equal(t, 1, result["iterations_used"])
	assert.Equal(t, false, result["finalization_attempted"])
	assert.Equal(t, false, result["finalization_succeeded"])
}

// TestReActAgent_afterReactIteration_firesOnToolRound 验证工具轮结束时事件恰好触发一次
// 同上构造（不注册 ff 回调），用计数器断言 fireCount == 1
```

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestReActAgent_reactLoop_boundaryForceFinish|TestReActAgent_reactLoop_maxIterationsResultFields' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/single_agent/interfaces/callback.go internal/agentcore/single_agent/interfaces/event_test.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "fix(react): 消费 boundary force_finish 并对齐迭代耗尽结果字段"
```

---

## Task 2: A-11 system prompt 为空仍保留 skills section

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_prompt.go:185-197`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:422`
- Test: `internal/agentcore/single_agent/agents/react_agent_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestReActAgent_skillSection_保留于空系统提示词 验证 system prompt 为空时技能小节仍注入
func TestReActAgent_skillSection_保留于空系统提示词(t *testing.T) {
	dir := writeDemoSkill(t)                            // 见附录 A.4：t.TempDir() 下写 SKILL.md
	agent := newAgentWithSkillUtil(t, dir)              // SystemPrompt 留空
	agent.updateSkillPromptBuilderSection(context.Background())
	assert.Contains(t, agent.promptBuilder.Build(), "demo-skill")
}

// TestReActAgent_skillSection_无技能时移除 验证无技能时移除小节
func TestReActAgent_skillSection_无技能时移除(t *testing.T) {
	agent := newAgentWithSkillUtil(t, "")               // 空目录 → HasSkill()=false
	agent.updateSkillPromptBuilderSection(context.Background())
	assert.NotContains(t, agent.promptBuilder.Build(), "demo-skill")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestReActAgent_skillSection' -v`
Expected: FAIL（`updateSkillPromptBuilderSection` 签名不匹配 / 断言未注入）

- [ ] **Step 3: 改实现**

```go
// updateSkillPromptBuilderSection 更新技能提示词区段。
//
// 技能小节是否注入只由「是否有已注册技能」决定：渲染后的系统提示词为空时
// 不得静默移除 skills section（否则 register_skill 后 agent 看不到技能）。
//
// Python: ReActAgent._update_skill_prompt_builder_section()
func (a *ReActAgent) updateSkillPromptBuilderSection(ctx context.Context) {
	if a.skillUtil == nil {
		return
	}
	if !a.skillUtil.HasSkill() {
		a.promptBuilder.RemoveSection(skillsSection)
		return
	}
	a.warnMissingSkillReadFileTool(ctx)
	a.AddPromptBuilderSection(skillsSection, a.skillUtil.GetSkillPrompt(), skillsSectionPriority)
}
```

调用点 `react_invoke.go:422` 改为 `a.updateSkillPromptBuilderSection(ctx)`。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestReActAgent_skillSection' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_prompt.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "fix(react): system prompt 为空时保留 skills section"
```

---

## Task 3: A-06 输出上限命中且无答案时判定失败

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`（新增校验函数 + `railedModelCall` 单出口）
- Test: `internal/agentcore/single_agent/agents/react_model_call_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestRaiseForModelResponseError 校验响应错误判定（两段）
func TestRaiseForModelResponseError(t *testing.T) {
	cases := []struct {
		name         string
		finishReason string
		content      string
		toolCalls    int
		usageCode    int
		wantErr      bool
	}{
		{"length+空", "length", "", 0, 0, true},
		{"length+空白", "length", "   ", 0, 0, true},
		{"max_tokens+空", "max_tokens", "", 0, 0, true},
		{"LENGTH 大写", "LENGTH", "", 0, 0, true},
		{"length+有正文", "length", "部分答案", 0, 0, false},
		{"length+有工具", "length", "", 1, 0, false},
		{"stop+空", "stop", "", 0, 0, false},
		{"provider code 非 0", "stop", "x", 0, 123, true},
		{"finish_reason=error", "error", "x", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := llmschema.NewAssistantMessage(tc.content)
			msg.FinishReason = tc.finishReason
			msg.UsageMetadata = &llmschema.UsageMetadata{Code: tc.usageCode}
			for i := 0; i < tc.toolCalls; i++ {
				msg.ToolCalls = append(msg.ToolCalls, &llmschema.ToolCall{ID: "c1", Name: "read_file"})
			}
			err := raiseForModelResponseError(msg)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run TestRaiseForModelResponseError -v`
Expected: FAIL with `undefined: raiseForModelResponseError`

- [ ] **Step 3: 实现校验函数**

`react_model_call.go` 非导出函数区：

```go
// raiseForModelResponseError 把 provider 错误响应与"输出上限命中且无答案"转成异常。
//
// Python: ReActAgent._raise_for_model_response_error() (react_agent.py L2021-2057)
func raiseForModelResponseError(msg *llmschema.AssistantMessage) error {
	if msg == nil {
		return nil
	}
	finishReason := strings.ToLower(strings.TrimSpace(msg.FinishReason))
	code := 0
	outputTokens := 0
	if msg.UsageMetadata != nil {
		code = msg.UsageMetadata.Code
		outputTokens = msg.UsageMetadata.OutputTokens
	}
	content := msg.Content.Text()
	noAnswer := len(msg.ToolCalls) == 0 && strings.TrimSpace(content) == ""

	// ① 推理模型把输出预算花在思考上、没有任何答案或工具调用
	if (finishReason == "length" || finishReason == "max_tokens") && noAnswer {
		return exception.NewBaseError(exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf(
				"model hit its output limit (finish_reason=%s, output_tokens=%d) before producing an answer or a tool call",
				finishReason, outputTokens)),
			exception.WithParams(map[string]any{
				"finish_reason": finishReason,
				"output_tokens": outputTokens,
			}),
		)
	}

	// ② provider 返回错误响应
	if finishReason != "error" && finishReason != "failed" && code == 0 {
		return nil
	}
	providerMessage := ""
	if msg.UsageMetadata != nil {
		providerMessage = msg.UsageMetadata.ErrMsg
	}
	if providerMessage == "" {
		providerMessage = content
	}
	if len(providerMessage) > 500 {
		providerMessage = providerMessage[:500]
	}
	detail := "provider returned no error detail"
	if providerMessage != "" {
		detail = providerMessage
	}
	shownFinishReason := finishReason
	if shownFinishReason == "" {
		shownFinishReason = "unknown"
	}
	return exception.NewBaseError(exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf(
			"provider response error: code=%d, finish_reason=%s, message=%s",
			code, shownFinishReason, detail)),
		exception.WithParams(map[string]any{
			"provider_code":    code,
			"finish_reason":    shownFinishReason,
			"provider_message": providerMessage,
		}),
	)
}
```

`react_model_call.go` 的 import 需补 `"strings"` 与 `"github.com/uapclaw/uapclaw-go/internal/common/exception"`。

- [ ] **Step 4: `railedModelCall` 收成单一出口**

把 `isStreaming` 之后的两段 `return` 改为：

```go
	var aiMsg *llmschema.AssistantMessage
	if isStreaming {
		aiMsg, err = a.callLLMStream(ctx, llmModel, modelName, messages, contextTools, sess, extraKVPairs)
	} else {
		aiMsg, err = a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools, extraKVPairs)
	}
	if err != nil {
		return nil, err
	}
	// 对齐 Python: ctx.inputs.response = ai_message; self._raise_for_model_response_error(ai_message)
	if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
		inputs.Response = aiMsg
	}
	if err := raiseForModelResponseError(aiMsg); err != nil {
		return nil, err
	}
	return aiMsg, nil
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestRaiseForModelResponseError|TestCallModel' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_model_call.go internal/agentcore/single_agent/agents/react_model_call_test.go
git commit -m "fix(react): 输出上限无答案与 provider 错误响应判定为失败"
```

---

## Task 4: A-07 空回答重试一次再失败

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message.go`（新增元数据常量）
- Modify: `internal/agentcore/harness/prompts/attachment_types.go:118-119`（删除重复常量）
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`（常量 + 循环分支）
- Test: `internal/agentcore/single_agent/agents/react_agent_test.go`

- [ ] **Step 1: 搬迁元数据常量**

`foundation/llm/schema/message.go` 常量区：

```go
// MetadataKeyMessageSourceKind 消息来源细分（query / steering / empty_response / attachment）
//
// Python: OPENJIUWEN_MESSAGE_SOURCE_KIND_METADATA (core/foundation/llm/schema/message.py:12)
const MetadataKeyMessageSourceKind = "_openjiuwen_message_source_kind"
```

删除 `harness/prompts/attachment_types.go` 中的 `MetadataKeyMessageSourceKind` 定义（该常量零消费者，删除后 `go build ./...` 验证）。

- [ ] **Step 2: 写失败测试**

```go
// TestReActAgent_emptyResponse_retryThenAnswer 验证空回答重试一次后返回真实答案
func TestReActAgent_emptyResponse_retryThenAnswer(t *testing.T) {
	agent := newAgentWithScriptedModel([]*llmschema.AssistantMessage{
		llmschema.NewAssistantMessage(""),        // 第一轮空
		llmschema.NewAssistantMessage("真实答案"), // 重试成功
	}, 3)
	fmc := &fakeModelContext{}
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}

	cbc, sess := newLoopCBC(t, agent, "empty_retry_sess")
	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.Equal(t, "真实答案", result["output"])
	assert.Equal(t, "answer", result["result_type"])
	assert.True(t, fmc.containsContent("[EMPTY_RESPONSE]"), "重试前应注入 [EMPTY_RESPONSE] 提示")
}

// TestReActAgent_emptyResponse_twoEmptiesFail 验证连续两空返回 error
func TestReActAgent_emptyResponse_twoEmptiesFail(t *testing.T) {
	agent := newAgentWithScriptedModel([]*llmschema.AssistantMessage{
		llmschema.NewAssistantMessage(""),
		llmschema.NewAssistantMessage("   "),
	}, 3)
	fmc := &fakeModelContext{}
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}
	cbc, sess := newLoopCBC(t, agent, "empty_fail_sess")

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.Equal(t, "error", result["result_type"])
	assert.Equal(t, "模型未返回有效内容（空响应），请重试或检查上下文。", result["output"])
	assert.Equal(t, false, result["reasoning_present"])
}

// TestReActAgent_emptyResponse_lastIterationNoRetry 验证最后一轮不重试
func TestReActAgent_emptyResponse_lastIterationNoRetry(t *testing.T) {
	agent := newAgentWithScriptedModel([]*llmschema.AssistantMessage{
		llmschema.NewAssistantMessage(""),
	}, 1) // MaxIterations(1)：首轮即最后一轮
	fmc := &fakeModelContext{}
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}
	cbc, sess := newLoopCBC(t, agent, "empty_last_sess")

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.Equal(t, "error", result["result_type"])
	assert.False(t, fmc.containsContent("[EMPTY_RESPONSE]"), "最后一轮不应注入重试提示")
}

`newAgentWithScriptedModel` / `newLoopCBC` / `newLoopSession` / `fmc.containsContent` 为测试文件内新助手，实现参照 `TestReActAgent_reactLoop_forceFinish` 的构造流程。

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run TestReActAgent_emptyResponse -v`
Expected: FAIL（第一例 `result["output"]` 为空串而非"真实答案"）

- [ ] **Step 4: 实现**

`react_invoke.go` 常量区：

```go
const (
	// emptyResponseMsg 空响应最终错误的用户可见文案
	// Python: EMPTY_RESPONSE_MSG (react_agent.py L125)
	emptyResponseMsg = "模型未返回有效内容（空响应），请重试或检查上下文。"
	// emptyResponseMaxRetries 空响应重试次数上限
	// Python: EMPTY_RESPONSE_MAX_RETRIES (react_agent.py L126)
	emptyResponseMaxRetries = 1
	// emptyResponseNotice 重试前注入的提示消息
	// Python: _EMPTY_RESPONSE_NOTICE (react_agent.py L127-130)
	emptyResponseNotice = "[EMPTY_RESPONSE] The previous turn had no content and no tool_calls. Reply with user-visible content or a tool call."
)
```

`reactLoop` 循环前：`emptyResponseRetries := 0`。

无工具调用分支改为（插在 steering 判断之后、正常 answer 之前）：

```go
		content := ""
		finishReason := ""
		thinking := ""
		if aiMsg != nil {
			content = aiMsg.Content.Text()
			finishReason = aiMsg.FinishReason
			thinking = aiMsg.ReasoningContent
		}

		if strings.TrimSpace(content) == "" {
			if emptyResponseRetries < emptyResponseMaxRetries && iteration+1 < maxIter {
				emptyResponseRetries++
				if modelCtx != nil {
					_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{
						llmschema.NewUserMessage(emptyResponseNotice, llmschema.WithMetadata(map[string]any{
							llmschema.MetadataKeyMessageSourceKind: "empty_response",
						})),
					})
				}
				logger.Info(logComponent).
					Int("retry", emptyResponseRetries).
					Int("max_retries", emptyResponseMaxRetries).
					Int("iteration", iteration+1).
					Bool("reasoning_present", thinking != "").
					Msg("empty no-tool turn, retrying")
				continue
			}
			a.saveContexts(ctx, sess)
			shownFinishReason := finishReason
			if shownFinishReason == "" {
				shownFinishReason = "null"
			}
			iterResult = map[string]any{
				"output":            emptyResponseMsg,
				"result_type":       "error",
				"finish_reason":     shownFinishReason,
				"reasoning_present": thinking != "",
			}
			break
		}

		a.saveContexts(ctx, sess)
		iterResult = map[string]any{"output": content, "result_type": "answer"}
		break
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -run TestReActAgent_emptyResponse -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/message.go internal/agentcore/harness/prompts/attachment_types.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "fix(react): 空回答重试一次，仍空则返回显式错误"
```

---

## Task 5: A-02 before_tool_call 请求 force_finish 后补 ToolMessage

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go:394-466`（`Execute`）
- Test: `internal/agentcore/single_agent/ability/ability_manager_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestAbilityManager_Execute_forceFinishProducesToolMessage 验证 force_finish 短路仍产出 ToolMessage
func TestAbilityManager_Execute_forceFinishProducesToolMessage(t *testing.T) {
	am, cbc, execCount := newManagerWithForceFinishRail(t, "read_file")
	results := am.Execute(context.Background(), cbc, []*llmschema.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: "{}"},
	}, nil, "")

	assert.Equal(t, 0, *execCount, "force_finish 后工具不应执行")
	assert.Len(t, results, 1)
	require.NotNil(t, results[0].ToolMsg)
	assert.Equal(t, "call_1", results[0].ToolMsg.ToolCallID)
	assert.True(t, cbc.HasForceFinishRequest(), "force_finish 应传播到父 ctx")
}

// TestAbilityManager_Execute_parallelForceFinishUsesMinIndex 验证并行场景取最小索引传播
// 3 个工具调用，中间那个 ff（结果 {"output":"mid"}）：3 条 ToolMsg 齐全，
// cbc 的 ff 结果为 mid。

// TestAbilityManager_Execute_forceFinishPrefersInputs 验证 inputs 优先
// rail 在 before_tool_call 里同时写 inputs.ToolResult/ToolMsg 并请求 ff：
// 采用 inputs 的值。
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run TestAbilityManager_Execute_forceFinish -v`
Expected: FAIL（`results[0].ToolMsg` 为 nil）

- [ ] **Step 3: 重写 `Execute` 的结果处理段**

`wg.Wait()` 之后整段替换（删除原 454-463 的 break 式传播）：

```go
	// ① 先按 tool-call 顺序一次性收敛子 ctx 上的 force-finish。
	//    ConsumeForceFinish 是清空式消费，每个 index 至多一条，全文唯一写 map 的地方。
	// Python: execute() 末尾 for tool_ctx in tool_contexts 收敛
	finishByIndex := make(map[int]map[string]any, len(toolCtxs))
	for i := range toolCtxs {
		if ff := toolCtxs[i].ConsumeForceFinish(); ff != nil {
			finishByIndex[i] = ff.Result
		}
	}
	if len(finishByIndex) > 0 {
		// Python: ctx.request_force_finish(force_finish_requests[min(force_finish_requests)])
		cbc.RequestForceFinish(finishByIndex[minIndexKey(finishByIndex)])
	}

	// ② 结果处理（Python ①②③④ 分支）
	for i := range results {
		res := &results[i]
		toolCall := toolCalls[i]

		// 错误兜底：ToolMsg 缺失时用 Python 同款文案补
		if res.ToolMsg == nil && res.Result != nil {
			if err, ok := res.Result.(error); ok {
				res.ToolMsg = llmschema.NewToolMessage(toolCallIDOf(toolCall),
					"Ability execution error: "+err.Error())
			}
		}

		// before/after 钩子请求了 force_finish：必须补 ToolMessage，否则 tool_calls 悬空
		if ffResult, hasFF := finishByIndex[i]; hasFF && res.ToolMsg == nil {
			res.Result = ffResult
			body, marshalErr := json.Marshal(ffResult)
			if marshalErr != nil {
				body = []byte("{}")
			}
			res.ToolMsg = llmschema.NewToolMessage(toolCallIDOf(toolCall), string(body))
			if inputs, ok := toolCtxs[i].Inputs().(*interfaces.ToolCallInputs); ok {
				if inputs.ToolResult != nil {
					res.Result = inputs.ToolResult
				}
				if inputs.ToolMsg != nil {
					res.ToolMsg = inputs.ToolMsg
				}
			}
		}
	}
```

非导出函数区新增：

```go
// minIndexKey 返回 map 中最小 key，用于 force-finish 按 tool-call 顺序取首个。
//
// Python: min(force_finish_requests)
func minIndexKey(m map[int]map[string]any) int {
	minKey := math.MaxInt
	for k := range m {
		if k < minKey {
			minKey = k
		}
	}
	return minKey
}
```

import 需补 `"encoding/json"` 与 `"math"`（若未引入）。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/agentcore/single_agent/ability/ -run TestAbilityManager_Execute -v`
Expected: PASS（含既有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/ability/ability_manager.go internal/agentcore/single_agent/ability/ability_manager_test.go
git commit -m "fix(ability): force_finish 短路补 ToolMessage 并按最小索引传播"
```

---

## Task 6: A-03 流式异常必须把流消费干净

**Files:**
- Modify: `internal/agentcore/foundation/llm/model.go:266-320`
- Modify: `internal/agentcore/session/tracer/decorator/decorator.go:162-176`
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`（`callLLMStream` 排空）
- Test: `internal/agentcore/foundation/llm/model_test.go`、`internal/agentcore/session/tracer/decorator/decorator_test.go`

- [ ] **Step 1: 写失败测试（消费方提前放弃 → 生产端退出）**

`model_test.go`：

```go
// TestModel_Stream_消费者放弃后排空 验证消费方提前退出时生产端不悬挂
func TestModel_Stream_消费者放弃后排空(t *testing.T) {
	producerDone := make(chan struct{})
	client := &fakeStreamClient{chunks: 200, onDone: func() { close(producerDone) }}
	m := NewModelWithClient(client) // 使用测试内已有的 fake 注入方式

	out, err := m.Stream(context.Background(), model_clients.NewMessagesParam(
		llmschema.NewUserMessage("hi")))
	require.NoError(t, err)
	<-out // 只读一个 chunk 就放弃

	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("生产端未在消费方放弃后收尾（goroutine/连接泄漏）")
	}
}
```

`decorator_test.go`：同构用例（`TracedModelClient.Stream` 只读一个 chunk 后放弃 → inner 生产端收尾）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/llm/ -run TestModel_Stream_消费者放弃后排空 -v`
Expected: FAIL with `生产端未在消费方放弃后收尾`

- [ ] **Step 3: 改 `model.go` 包装 goroutine**

```go
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		// 包装层兜底：钩子 panic 不能带走整个进程，转 StreamError 哨兵交消费方处理
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(logComponent).
					Str("event_type", "llm_stream_panic").
					Str("layer", "model").
					Str("stack", string(debug.Stack())).
					Err(fmt.Errorf("%v", rec)).
					Msg("stream wrapper panicked, converted to stream error")
				select {
				case out <- llmschema.NewAssistantMessageChunk("",
					llmschema.WithChunkStreamError(fmt.Errorf("stream wrapper panic: %v", rec))):
				case <-ctx.Done():
				}
			}
		}()

		for chunk := range chunkChan {
			// ...transform / trigger 原逻辑不动...

			select {
			case out <- chunk:
			case <-ctx.Done():
				drained := 0
				for range chunkChan {
					drained++
				}
				logger.Info(logComponent).
					Str("event_type", "llm_stream_consumer_abandoned").
					Str("layer", "model").
					Int("drained", drained).
					Msg("consumer abandoned stream, drained remaining chunks")
				return
			}
		}
	}()
```

import 需补 `"runtime/debug"`、`"fmt"`。

- [ ] **Step 4: 改 `decorator.go` 同构**

```go
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(logComponent).
					Str("event_type", "llm_stream_panic").
					Str("layer", "tracer").
					Str("stack", string(debug.Stack())).
					Err(fmt.Errorf("%v", rec)).
					Msg("stream tracer wrapper panicked, converted to stream error")
				select {
				case out <- llmschema.NewAssistantMessageChunk("",
					llmschema.WithChunkStreamError(fmt.Errorf("stream wrapper panic: %v", rec))):
				case <-ctx.Done():
				}
			}
		}()
		for chunk := range chunkChan {
			select {
			case out <- chunk:
			case <-ctx.Done():
				drained := 0
				for range chunkChan {
					drained++
				}
				logger.Info(logComponent).
					Str("event_type", "llm_stream_consumer_abandoned").
					Str("layer", "tracer").
					Int("drained", drained).
					Msg("consumer abandoned traced stream, drained remaining chunks")
				return
			}
		}
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMEnd, &tracer.TriggerParams{Span: &span.Span})
	}()
```

- [ ] **Step 5: `callLLMStream` 遇 StreamError 前显式排空**

```go
		if chunk != nil && chunk.StreamError != nil {
			// 对齐 Python: async for _chunk in stream_iter（异常路径必须消费完剩余流）
			drained := 0
			for range chunkCh {
				drained++
			}
			logger.Debug(logComponent).
				Str("event_type", "llm_stream_error_drained").
				Int("drained", drained).
				Err(chunk.StreamError).
				Msg("drained remaining stream after error")
			return nil, chunk.StreamError
		}
```

- [ ] **Step 6: 运行测试**

Run: `go test -race ./internal/agentcore/foundation/llm/... ./internal/agentcore/session/tracer/... ./internal/agentcore/single_agent/agents/`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/llm/model.go internal/agentcore/foundation/llm/model_test.go internal/agentcore/session/tracer/decorator/decorator.go internal/agentcore/session/tracer/decorator/decorator_test.go internal/agentcore/single_agent/agents/react_model_call.go
git commit -m "fix(llm): 流包装层消费方放弃时排空并兜底 panic，异常路径排空剩余 chunk"
```

---

## Task 7: A-05 内层 ReAct 未配置时视为不限

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go:199-200,255-260,425-445`
- Modify: `internal/agentcore/harness/deep_agent.go:1574-1581,1766-1772`
- Modify: `internal/agentcore/harness/{plan,explore,research,code,browser}_agent_factory.go`
- Test: `internal/agentcore/harness/schema/config_test.go`、`internal/agentcore/harness/deep_agent_test.go`

- [ ] **Step 1: 写失败测试**

`config_test.go`：

```go
// TestDeepAgentConfig_ResolveInnerReactMaxIterations 验证 0 → 不限
func TestDeepAgentConfig_ResolveInnerReactMaxIterations(t *testing.T) {
	assert.Equal(t, math.MaxInt, (&hschema.DeepAgentConfig{}).ResolveInnerReactMaxIterations())
	assert.Equal(t, 7, (&hschema.DeepAgentConfig{MaxIterations: 7}).ResolveInnerReactMaxIterations())
	assert.Equal(t, math.MaxInt, (&hschema.DeepAgentConfig{MaxIterations: -3}).ResolveInnerReactMaxIterations())
}
```

`deep_agent_test.go`（对齐上游 `test_inner_react_max_iterations.py` 意图）：

```go
// TestDeepAgent_innerReActMaxIterations_显式值优先于TaskLoop 验证显式配置不被 task loop 覆盖
func TestDeepAgent_innerReActMaxIterations_显式值优先于TaskLoop(t *testing.T) {
	cfg := hschema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	cfg.MaxIterations = 7
	d := newDeepAgentForTest(t, cfg)
	react := d.reactAgentForTest() // 或通过既有 getter/字段断言
	assert.Equal(t, 7, react.Config().(*config.ReActAgentConfig).MaxIterations)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/schema/ -run TestDeepAgentConfig_ResolveInnerReactMaxIterations -v`
Expected: FAIL with `undefined: ResolveInnerReactMaxIterations`

- [ ] **Step 3: 改 `schema/config.go`**

删除常量 `DefaultMaxIterations`；`NewDeepAgentConfig()` 删除 `MaxIterations: DefaultMaxIterations` 行；`EffectiveMaxIterations` 整体替换为：

```go
// ResolveInnerReactMaxIterations 解析内层 ReAct 循环的迭代上限。
//
// 0 表示未配置 → 不限（math.MaxInt）；>0 原样使用（即使启用了外层 task loop）；
// 负数属无效配置，按未配置处理并告警。
//
// Python: resolve_inner_react_max_iterations() (harness/schema/config.py)
func (c *DeepAgentConfig) ResolveInnerReactMaxIterations() int {
	if c.MaxIterations > 0 {
		return c.MaxIterations
	}
	if c.MaxIterations < 0 {
		logger.Warn(logComponent).
			Int("max_iterations", c.MaxIterations).
			Msg("negative max_iterations treated as unbounded")
	}
	return math.MaxInt
}
```

字段注释改为：

```go
	// MaxIterations 内层 ReAct 单次 invoke 的迭代上限，0（未配置）表示不限
	MaxIterations int `json:"max_iterations,omitempty"`
```

import 需补 `"math"`（若未引入）与 harness/schema 的 logger（若无则改用 `logger.Warn(logger.ComponentAgentCore)`；若该包不能引入 logger，则改为静默忽略并只返回 MaxInt，并在注释说明）。

- [ ] **Step 4: 改两处装配**

`deep_agent.go:1574-1581` 与 `:1766-1772` 均替换为：

```go
	// 内层 ReAct 迭代上限：未配置=不限，显式配置优先（与 task loop 无关）
	reactConfig.MaxIterations = cfg.ResolveInnerReactMaxIterations()
	if cfg.MaxIterations > 0 {
		logger.Info(logComponent).Int("max_iterations", reactConfig.MaxIterations).Msg("[DeepAgent] inner ReAct max_iterations configured")
	} else {
		logger.Info(logComponent).Msg("[DeepAgent] inner ReAct max_iterations unbounded (default)")
	}
```

（第一处变量名为 `config`，按现场变量名替换；`math` import 若因此不再使用需删除。）

- [ ] **Step 5: 子代理默认值对齐**

```go
// plan_agent_factory.go:56   maxIterations = 50
// explore_agent_factory.go:55 maxIterations = 50
// research_agent_factory.go:63 maxIterations = 50
// code_agent_factory.go:94   maxIterations = 50
// browser_agent_factory.go:89 maxIterations = 100
```
同时把各自的 `// Python: max_iterations=NN` 注释更新为对应上游值。

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/agentcore/harness/... -run 'TestDeepAgentConfig|TestDeepAgent_innerReAct|AgentFactory' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/schema/config.go internal/agentcore/harness/schema/config_test.go internal/agentcore/harness/deep_agent.go internal/agentcore/harness/*_agent_factory.go
git commit -m "fix(harness): 内层 ReAct 未配置时迭代不限，并对齐子代理默认值"
```

---

## Task 8: A-10 中断后 ReAct context 持久化

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`（新增清洗/持久化 + 替换取消与异常分支 + `innerStream`）
- Test: `internal/agentcore/single_agent/agents/react_agent_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestSanitizeAbortedTurnMessages 验证被中断轮次的清洗规则
func TestSanitizeAbortedTurnMessages(t *testing.T) {
	user := llmschema.NewUserMessage("read a.txt")
	assistantWithTools := llmschema.NewAssistantMessage("")
	assistantWithTools.ToolCalls = []*llmschema.ToolCall{{ID: "c1", Name: "read_file"}}
	completeTool := llmschema.NewToolMessage("c1", "content")

	cases := []struct {
		name string
		in   []llmschema.BaseMessage
		want []llmschema.BaseMessage
	}{
		{
			name: "完整 tool 对保留",
			in:   []llmschema.BaseMessage{user, assistantWithTools, completeTool},
			want: []llmschema.BaseMessage{user, assistantWithTools, completeTool},
		},
		{
			name: "半截 tool 块整块丢弃并补 marker",
			in:   []llmschema.BaseMessage{user, assistantWithTools},
			want: []llmschema.BaseMessage{user, llmschema.NewAssistantMessage("[Request cancelled by user]")},
		},
		{
			name: "孤立 tool 消息丢弃",
			in:   []llmschema.BaseMessage{user, completeTool},
			want: []llmschema.BaseMessage{user, llmschema.NewAssistantMessage("[Request cancelled by user]")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeAbortedTurnMessages(tc.in, "[Request cancelled by user]")
			assert.Equal(t, len(tc.want), len(got))
			for i := range tc.want {
				assert.Equal(t, tc.want[i].GetRole(), got[i].GetRole())
			}
		})
	}
}

// TestReActAgent_cancel_persistsContext 验证取消后本轮 UserMessage 被持久化并带 marker
// 构造：fakeModelContext 已含 [user "hello"]，ctx 取消后调用 handleContextAbort
// 断言：contextEngine.saveContextsCalled == true；fmc 消息尾为 AssistantMessage(marker)；
//      UserMessage("hello") 仍在。
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestSanitizeAbortedTurnMessages|TestReActAgent_cancel_persistsContext' -v`
Expected: FAIL with `undefined: sanitizeAbortedTurnMessages`

- [ ] **Step 3: 实现清洗与持久化**

```go
// sanitizeAbortedTurnMessages 返回被中断轮次的 LLM 安全前缀。
//
// 保留用户消息与完整的 assistant/tool 对；丢弃未完成的 tool_call 块与孤立 ToolMessage；
// 结尾若为 UserMessage 则补一条 marker 形式的 AssistantMessage，避免"只有提问没有回复"。
//
// Python: ReActAgent._sanitize_cancelled_turn_messages() (react_agent.py L3721-3788)
func sanitizeAbortedTurnMessages(messages []llmschema.BaseMessage, marker string) []llmschema.BaseMessage {
	kept := make([]llmschema.BaseMessage, 0, len(messages))
	for i := 0; i < len(messages); {
		msg := messages[i]
		if am, ok := msg.(*llmschema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
			needed := make(map[string]bool, len(am.ToolCalls))
			for _, tc := range am.ToolCalls {
				if tc != nil && tc.ID != "" {
					needed[tc.ID] = true
				}
			}
			found := make(map[string]*llmschema.ToolMessage, len(needed))
			j := i + 1
			for ; j < len(messages); j++ {
				tm, ok := messages[j].(*llmschema.ToolMessage)
				if !ok {
					break
				}
				found[tm.ToolCallID] = tm
			}
			complete := len(needed) > 0
			for id := range needed {
				if found[id] == nil {
					complete = false
					break
				}
			}
			if complete {
				kept = append(kept, am)
				for _, tc := range am.ToolCalls {
					if tc != nil && found[tc.ID] != nil {
						kept = append(kept, found[tc.ID])
					}
				}
			}
			i = j
			continue
		}
		if _, ok := msg.(*llmschema.ToolMessage); ok {
			i++ // 孤立 tool 消息丢弃
			continue
		}
		kept = append(kept, msg)
		i++
	}
	if n := len(kept); n > 0 {
		if _, ok := kept[n-1].(*llmschema.UserMessage); ok {
			kept = append(kept, llmschema.NewAssistantMessage(marker))
		}
	}
	return kept
}

// handleContextAbort 在取消/异常后清洗并持久化本轮上下文。
//
// 返回 true 表示已持久化（调用方据此抑制 finally 里的重复 save/commit）。
//
// Python: ReActAgent._handle_context_abort() (react_agent.py L3593-3624)
func (a *ReActAgent) handleContextAbort(ctx context.Context, sess sessioninterfaces.SessionFacade, marker string) bool {
	if a.contextEngine == nil || sess == nil {
		return false
	}
	sessionID := sess.GetSessionID()
	mc := a.contextEngine.GetContext("default_context", sessionID)
	if mc != nil {
		if current, err := mc.GetMessages(0, false); err == nil && len(current) > 0 {
			mc.SetMessages(sanitizeAbortedTurnMessages(current, marker), false)
		}
	}
	a.saveContexts(ctx, sess)
	if as, ok := sess.(*session.Session); ok {
		if err := as.Commit(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("commit session after abort failed")
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: 替换取消/异常分支**

`invokeImpl` 的 `err != nil` 段替换为：

```go
	abortPersisted := false
	if err != nil {
		switch {
		case utils.Canceled(ctx):
			abortPersisted = a.handleContextAbort(ctx, sess, "[Request cancelled by user]")
		default:
			abortPersisted = a.handleContextAbort(ctx, sess, "[Request interrupted by an unexpected error]")
		}
		return nil, err
	}
	_ = abortPersisted
```

`needCleanup` 的 defer 改为：

```go
	if needCleanup {
		defer func() {
			if !abortPersisted {
				a.saveContexts(ctx, sess)
			}
			if as, ok := sess.(*session.Session); ok {
				_ = as.CloseStream()
				if !abortPersisted {
					_ = as.Commit(ctx)
				}
			}
		}()
	}
```

（`abortPersisted` 需在 defer 注册前声明；Go 闭包捕获变量，声明顺序必须早于 defer。）

`innerStream` 的 `streamProcess` 同样处理：`a.Invoke` 返回 err 时按取消/非取消调用 `handleContextAbort`，并把 `abortPersisted` 传入其 defer 以抑制重复 save/commit。

- [ ] **Step 5: 运行测试**

Run: `go test -race ./internal/agentcore/single_agent/agents/ -run 'TestSanitizeAbortedTurnMessages|TestReActAgent_cancel|TestReActAgent_invoke|TestReActAgent_stream' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "fix(react): 取消与异常后清洗并持久化本轮上下文"
```

---

## Task 9: A-08-1 无进展停止评估器

**Files:**
- Modify: `internal/agentcore/harness/task_loop/stop_condition.go`
- Modify: `internal/agentcore/harness/schema/config.go`（`TaskLoopNoProgressGuardConfig`）
- Modify: `internal/agentcore/harness/deep_agent.go:2625-2660`（`buildTaskLoopEvaluators`）与循环结束后 yield
- Test: `internal/agentcore/harness/task_loop/stop_condition_test.go`、`internal/agentcore/harness/deep_agent_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestNoProgressAnswerEvaluator_ShouldStop 验证连续短答案触发停止
func TestNoProgressAnswerEvaluator_ShouldStop(t *testing.T) {
	e := NewNoProgressAnswerEvaluator(3, 20)
	short := map[string]any{"result_type": "answer", "output": "ok"}
	for i := 0; i < 2; i++ {
		assert.False(t, e.ShouldStop(StopEvaluationContext{LastResult: short}))
	}
	assert.True(t, e.ShouldStop(StopEvaluationContext{LastResult: short}))
	assert.Equal(t, "NoProgressAnswerEvaluator", e.Name())

	// 长答案清零
	e.Reset()
	assert.False(t, e.ShouldStop(StopEvaluationContext{LastResult: short}))
	assert.False(t, e.ShouldStop(StopEvaluationContext{LastResult: map[string]any{
		"result_type": "answer",
		"output":      strings.Repeat("x", 30),
	}}))
	assert.False(t, e.ShouldStop(StopEvaluationContext{LastResult: short}))

	// error 结果不计入
	assert.False(t, e.ShouldStop(StopEvaluationContext{LastResult: map[string]any{
		"result_type": "error", "output": "",
	}}))
}

// TestNoProgressAnswerEvaluator_StateRoundTrip 验证状态导出/导入
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/task_loop/ -run TestNoProgressAnswerEvaluator -v`
Expected: FAIL with `undefined: NewNoProgressAnswerEvaluator`

- [ ] **Step 3: 实现评估器**

`stop_condition.go`（结构体区块放在其他 Evaluator 之后，导出函数放在构造器区块）：

```go
// NoProgressAnswerEvaluator 连续多轮"有 answer 但内容过短"时判定无进展。
//
// Python: NoProgressAnswerEvaluator (harness/schema/stop_condition.py)
type NoProgressAnswerEvaluator struct {
	// maxConsecutiveEmptyAnswers 连续短答案轮次阈值
	maxConsecutiveEmptyAnswers int
	// minAnswerChars 视为"有实质内容"的最小字符数
	minAnswerChars int
	// consecutiveEmptyAnswers 当前连续计数
	consecutiveEmptyAnswers int
}
```

```go
// NewNoProgressAnswerEvaluator 创建无进展停止评估器。
func NewNoProgressAnswerEvaluator(maxConsecutiveEmptyAnswers, minAnswerChars int) *NoProgressAnswerEvaluator {
	if maxConsecutiveEmptyAnswers < 1 {
		maxConsecutiveEmptyAnswers = 1
	}
	if minAnswerChars < 0 {
		minAnswerChars = 0
	}
	return &NoProgressAnswerEvaluator{
		maxConsecutiveEmptyAnswers: maxConsecutiveEmptyAnswers,
		minAnswerChars:             minAnswerChars,
	}
}
```

```go
// ShouldStop 连续达到阈值时返回 true。
func (e *NoProgressAnswerEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	result := ctx.LastResult
	resultType, _ := result["result_type"].(string)
	output, _ := result["output"].(string)
	outputLen := len(strings.TrimSpace(output))

	if strings.ToLower(strings.TrimSpace(resultType)) == "answer" && outputLen < e.minAnswerChars {
		e.consecutiveEmptyAnswers++
	} else {
		e.consecutiveEmptyAnswers = 0
	}
	if e.consecutiveEmptyAnswers >= e.maxConsecutiveEmptyAnswers {
		logger.Info(logComponent).
			Int("iteration", ctx.Iteration).
			Int("output_len", outputLen).
			Int("consecutive_empty_answers", e.consecutiveEmptyAnswers).
			Int("max_consecutive_empty_answers", e.maxConsecutiveEmptyAnswers).
			Int("min_answer_chars", e.minAnswerChars).
			Msg("no progress answer guard triggered")
		return true
	}
	return false
}

// Name 返回评估器名称（同时作为 stop_reason）。
func (e *NoProgressAnswerEvaluator) Name() string { return "NoProgressAnswerEvaluator" }

// ExportState 导出连续计数。
func (e *NoProgressAnswerEvaluator) ExportState() map[string]any {
	return map[string]any{
		"consecutive_empty_answers":     e.consecutiveEmptyAnswers,
		"max_consecutive_empty_answers": e.maxConsecutiveEmptyAnswers,
		"min_answer_chars":              e.minAnswerChars,
	}
}

// ImportState 恢复连续计数。
func (e *NoProgressAnswerEvaluator) ImportState(data map[string]any) {
	if v, ok := data["consecutive_empty_answers"].(int); ok && v > 0 {
		e.consecutiveEmptyAnswers = v
	}
}

// Reset 重置连续计数。
func (e *NoProgressAnswerEvaluator) Reset() { e.consecutiveEmptyAnswers = 0 }
```

- [ ] **Step 4: 加配置字段**

`schema/config.go`：

```go
// TaskLoopNoProgressGuardConfig 任务循环无进展守卫配置。
//
// Python: TaskLoopNoProgressGuardConfig (harness/schema/config.py)
type TaskLoopNoProgressGuardConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// MaxConsecutiveEmptyAnswers 连续短答案轮次阈值
	MaxConsecutiveEmptyAnswers int `json:"max_consecutive_empty_answers"`
	// MinAnswerChars 视为有实质内容的最小字符数
	MinAnswerChars int `json:"min_answer_chars"`
}

// NewTaskLoopNoProgressGuardConfig 创建默认配置（启用 / 3 / 20）。
func NewTaskLoopNoProgressGuardConfig() TaskLoopNoProgressGuardConfig {
	return TaskLoopNoProgressGuardConfig{Enabled: true, MaxConsecutiveEmptyAnswers: 3, MinAnswerChars: 20}
}
```

`DeepAgentConfig` 加字段并在 `NewDeepAgentConfig()` 里赋值 `TaskLoopNoProgressGuard: NewTaskLoopNoProgressGuardConfig(),`：

```go
	// TaskLoopNoProgressGuard 任务循环无进展守卫
	TaskLoopNoProgressGuard TaskLoopNoProgressGuardConfig `json:"task_loop_no_progress_guard"`
```

- [ ] **Step 5: 装配与停止结果**

`deep_agent.go` 非导出函数区新增（替换两处重复的 `taskCompRail.BuildEvaluators()`）：

```go
// buildTaskLoopEvaluators 构建外层任务循环的停止条件评估器。
//
// Python: DeepAgent._build_task_loop_evaluators() (deep_agent.py L2279-2311)
func (d *DeepAgent) buildTaskLoopEvaluators() []taskloop.StopConditionEvaluator {
	var evaluators []taskloop.StopConditionEvaluator
	if d.taskCompletionRail != nil {
		evaluators = d.taskCompletionRail.BuildEvaluators()
	}
	d.configMu.RLock()
	guard := d.deepConfig.TaskLoopNoProgressGuard
	d.configMu.RUnlock()
	if guard.Enabled {
		evaluators = append(evaluators,
			taskloop.NewNoProgressAnswerEvaluator(guard.MaxConsecutiveEmptyAnswers, guard.MinAnswerChars))
	}
	return evaluators
}
```

循环结束后（原 `logLoop("loop stopped by: %s", ...)` 之后）：

```go
		if stopReason == "NoProgressAnswerEvaluator" {
			select {
			case outCh <- map[string]any{
				"output":      "Task loop stopped after repeated empty or near-empty no-tool answers. This indicates the model is not making observable progress.",
				"result_type": "error",
				"stop_reason": stopReason,
			}:
			case <-ctx.Done():
			}
		}
```

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/agentcore/harness/... -run 'TestNoProgressAnswerEvaluator|TestDeepAgent_taskLoop' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/task_loop/stop_condition.go internal/agentcore/harness/task_loop/stop_condition_test.go internal/agentcore/harness/schema/config.go internal/agentcore/harness/deep_agent.go
git commit -m "feat(harness): 新增无进展停止评估器并接入任务循环"
```

---

## Task 10: A-08-3 外层轮次硬上限

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go:2388-2400`（`runTaskLoop`）
- Test: `internal/agentcore/harness/deep_agent_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestDeepAgent_runTaskLoop_maxOuterRounds 验证超过 50 轮产出 MaxOuterRounds 错误
func TestDeepAgent_runTaskLoop_maxOuterRounds(t *testing.T) {
	// 构造永不满足退出条件的 controller（每轮都返回 completed 且 hasRemainingTasks 恒 true）
	// 期望：第 51 轮产出 {"result_type":"error","stop_reason":"MaxOuterRounds"} 且循环结束
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/ -run TestDeepAgent_runTaskLoop_maxOuterRounds -v`
Expected: FAIL（超时或未产出错误结果）

- [ ] **Step 3: 实现**

`runTaskLoop` 中 `outerRound := 0` 之后加 `const maxOuterRounds = 50`，循环体开头：

```go
		for coord.ShouldContinue() {
			outerRound++
			// 外层轮次硬上限：评估器可能一个都没配（ShouldContinue 恒真），这是最后兜底
			// Python: deep_agent.py L2664-2677
			if outerRound > maxOuterRounds {
				logLoop("round=%d exceeded max_outer_rounds=%d, forcing stop", "", outerRound, maxOuterRounds)
				select {
				case outCh <- map[string]any{
					"output":      "Task loop stopped after exceeding the maximum number of outer rounds (50). This usually indicates the model is not making observable progress.",
					"result_type": "error",
					"stop_reason": "MaxOuterRounds",
				}:
				case <-ctx.Done():
					return
				}
				break
			}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/agentcore/harness/ -run TestDeepAgent_runTaskLoop -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go internal/agentcore/harness/deep_agent_test.go
git commit -m "feat(harness): 外层任务循环增加 50 轮硬上限"
```

---

## Task 11: A-08-2 视觉降级（含流内 error 事件识别）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/types.go:99-112`
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go:322-360,542-570`
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`（循环继续分支）
- Test: 对应 `_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestParseStreamChunk_errorEvent 验证流内 error 事件被识别
func TestParseStreamChunk_errorEvent(t *testing.T) {
	// client 层：构造 SSE 流，第二条为 data: {"error":{"message":"image input is not supported"}}
	// 期望：消费方拿到 StreamError 且 Error() 含 "image input is not supported"

// TestIsImageInputUnsupportedError 验证 6 个 code + 14 条 pattern
// 表驱动：{err: errors.New("OpenAI API HTTP 400: image input is not supported"), want: true}
//        {err: errors.New("OpenAI API HTTP 400: {\"code\":\"invalid_image_input\"}"), want: true}
//        {err: errors.New("rate limit exceeded"), want: false}

// TestReActAgent_imageUnsupported_continueLoop 验证拒图后循环继续而非 break
// 脚本模型：第一轮返回 HTTP 400 拒图错误，第二轮返回正常答案
// 期望：最终 answer 为第二轮内容，且上下文含 "[Vision Input Unsupported]"
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestIsImageInputUnsupportedError|TestReActAgent_imageUnsupported' -v`
Expected: FAIL with `undefined: isImageInputUnsupportedError`

- [ ] **Step 3: client 层识别流内 error**

`types.go` 的 `ChatCompletionChunkResponse` 追加字段：

```go
	// Error 流内错误事件（部分 provider 以 200 + error 帧返回错误）
	Error *ErrorDetail `json:"error,omitempty"`
```

`client.go` 解析后、`ParseStreamChunk` 之前：

```go
			var chunkResp ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunkResp); err != nil {
				// ...原逻辑
			}
			// 流内错误帧：必须转成 StreamError，否则被 ParseStreamChunk 当空块丢弃
			if chunkResp.Error != nil && chunkResp.Error.Message != "" {
				c.FailStream(ctx, chunkChan, modelName, fmt.Errorf("stream error event: %s (code=%s, type=%s)",
					chunkResp.Error.Message, chunkResp.Error.Code, chunkResp.Error.Type))
				return
			}
```

`HandleHTTPError` 的 JSON 分支保留 provider 码：

```go
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d: %s", resp.StatusCode, errResp.Error.Message)),
			exception.WithParams(map[string]any{
				"http_status":   resp.StatusCode,
				"provider_code": errResp.Error.Code,
				"provider_type": errResp.Error.Type,
			}),
		)
	}
```

- [ ] **Step 4: agents 层识别与降级**

`react_model_call.go` 新增：

```go
// imageInputUnsupportedPatterns provider 拒图文案（与上游逐一对应）。
//
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_PATTERNS (react_agent.py L139-153)
var imageInputUnsupportedPatterns = []string{
	"no endpoints found that support image input",
	"does not accept images",
	"does not support image",
	"doesn't accept images",
	"doesn't support image",
	"do not support image",
	"image input is not supported",
	"image input not supported",
	"image_url is not supported",
	"images are not supported",
	"multimodal input is not supported",
	"not support image input",
	"unsupported image",
	"vision is not supported",
}

// imageInputUnsupportedCodes 结构化错误码（与上游逐一对应）。
//
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_CODES (react_agent.py L131-138)
var imageInputUnsupportedCodes = []string{
	"invalid_image_input",
	"image_input_unsupported",
	"unsupported_content_type",
	"unsupported_image",
	"unsupported_image_input",
	"unsupported_message_content_type",
}

// collectErrorTexts 收集错误链上所有可匹配文本。
//
// Python: _extract_exception_error_values() (react_agent.py L2100-2145)
func collectErrorTexts(err error) []string {
	var texts []string
	for err != nil {
		texts = append(texts, err.Error())
		var be *exception.BaseError
		if errors.As(err, &be) {
			for _, v := range be.Params() {
				texts = append(texts, fmt.Sprintf("%v", v))
			}
			if be.Details() != nil {
				texts = append(texts, fmt.Sprintf("%v", be.Details()))
			}
		}
		err = errors.Unwrap(err)
	}
	return texts
}

// isImageInputUnsupportedError 判断错误是否属于"模型/端点不支持图片输入"。
//
// Python: ReActAgent._is_image_input_unsupported_error() (react_agent.py L2147-2164)
func isImageInputUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	texts := collectErrorTexts(err)
	lowered := make([]string, 0, len(texts))
	for _, t := range texts {
		lowered = append(lowered, strings.ToLower(t))
	}
	for _, v := range lowered {
		normalized := strings.NewReplacer("-", "_", " ", "_").Replace(v)
		for _, code := range imageInputUnsupportedCodes {
			if normalized == code || strings.HasSuffix(normalized, "_"+code) {
				return true
			}
		}
	}
	joined := strings.Join(lowered, "\n")
	for _, p := range imageInputUnsupportedPatterns {
		if strings.Contains(joined, p) {
			return true
		}
	}
	return false
}

// messagesContainImageInput 判断请求消息里是否含图片内容块。
//
// Python: ReActAgent._messages_contain_image_input()
func messagesContainImageInput(messages []llmschema.BaseMessage) bool {
	for _, m := range messages {
		for _, part := range m.GetContent().Parts() {
			if part.Type == "image_url" && part.ImageURL != nil && part.ImageURL.URL != "" {
				return true
			}
		}
	}
	return false
}

// buildImageInputUnsupportedMessage 构造拒图降级提示消息。
//
// Python: ReActAgent._build_image_input_unsupported_message() (react_agent.py L2166-2186)
func buildImageInputUnsupportedMessage() *llmschema.AssistantMessage {
	return llmschema.NewAssistantMessage("[Vision Input Unsupported] The current model or endpoint does not support image input.\n" +
		"当前主模型或模型服务端点不支持图片输入。\n\n" +
		"Suggested alternatives / 建议替代方案：\n" +
		"1. If reading a PDF, use `read_file` in text mode or use `pypdf`/`pdfplumber` to extract text and form fields directly, without rendering pages to images.\n" +
		"   如果读取的是 PDF，请使用文本模式读取，或用 `pypdf` 提取文本和表单字段。\n" +
		"2. If reading an image (JPG/PNG), describe what information you need and ask the user for a text transcription, or use an available OCR tool if one is configured.\n" +
		"   如果读取的是图片，请描述所需信息并请用户提供文字转录。\n" +
		"3. If the task absolutely requires vision, configure a `vision_model_config` with a vision-capable endpoint.\n" +
		"   如果任务必须依赖视觉，请配置支持视觉输入的模型。")
}
```

`railedModelCall` 单一出口处（Task 3 已建立）改为：

```go
	if err != nil {
		if messagesContainImageInput(messages) && isImageInputUnsupportedError(err) {
			degraded := buildImageInputUnsupportedMessage()
			cbc.SetExtra("_last_image_unsupported", true)
			if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
				inputs.Response = degraded
			}
			return degraded, nil
		}
		return nil, err
	}
```

`react_invoke.go` 无工具调用分支，在 steering 判断之后、空响应判断之前插入：

```go
		if v, ok := cbc.GetExtra("_last_image_unsupported"); ok {
			if flagged, _ := v.(bool); flagged {
				cbc.SetExtra("_last_image_unsupported", false)
				a.saveContexts(ctx, sess)
				continue
			}
		}
```

（`MessageContent.Parts() []ContentPart` 已存在，`ContentPart{Type, Text, ImageURL}` 见 `foundation/llm/schema/message.go:54-68`，无需另找访问方式。）

- [ ] **Step 5: 运行测试**

Run: `go test -race ./internal/agentcore/foundation/llm/model_clients/openai/ ./internal/agentcore/single_agent/agents/ -run 'TestParseStreamChunk|TestIsImageInputUnsupportedError|TestReActAgent_imageUnsupported' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/types.go internal/agentcore/foundation/llm/model_clients/openai/client.go internal/agentcore/single_agent/agents/react_model_call.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_model_call_test.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "feat(react): 拒图错误降级为提示消息并继续循环，识别流内 error 事件"
```

---

## Task 12: A-08-4 技能提示词预算

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go`（`buildSkillsSection` + `applySkillBudget`）
- Modify: `internal/agentcore/harness/schema/config.go`（两个配置字段）
- Modify: `internal/agentcore/harness/factory.go`（透传）
- Test: `internal/agentcore/harness/rails/skills/skill_use_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestApplySkillBudget 验证按关键词重叠排序后整块丢弃
func TestApplySkillBudget(t *testing.T) {
	skills := []*skillpkg.Skill{
		{Name: "pdf-reader", Description: "read pdf files"},
		{Name: "excel", Description: "spreadsheet helper"},
		{Name: "unrelated", Description: "cooking recipes"},
	}
	got := applySkillBudget(skills, "read a pdf file", 2, 0)
	assert.Len(t, got, 2)
	assert.Equal(t, "pdf-reader", got[0].Name, "与 query 重叠度最高的技能排首位")

	got = applySkillBudget(skills, "read a pdf file", 2, 5)
	assert.LessOrEqual(t, len(got), 2, "字符预算也应生效且不截断单个技能内容")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/rails/skills/ -run TestApplySkillBudget -v`
Expected: FAIL with `undefined: applySkillBudget`

- [ ] **Step 3: 实现预算**

```go
// applySkillBudget 按与当前 query 的关键词重叠度对技能排序，并按数量/字符预算整块丢弃。
//
// 丢弃以"整个技能"为粒度，绝不截断到半截文本（半截说明会让模型误用技能）。
//
// Python: SkillUseRail._apply_skill_budget()
func applySkillBudget(skills []*skillpkg.Skill, query string, maxSkills, maxTotalChars int) []*skillpkg.Skill {
	if maxSkills <= 0 && maxTotalChars <= 0 {
		return skills
	}
	queryWords := keywordSet(query)
	ranked := make([]*skillpkg.Skill, len(skills))
	copy(ranked, skills)
	sort.SliceStable(ranked, func(i, j int) bool {
		return overlapScore(ranked[i], queryWords) > overlapScore(ranked[j], queryWords)
	})
	out := make([]*skillpkg.Skill, 0, len(ranked))
	totalChars := 0
	for _, s := range ranked {
		if maxSkills > 0 && len(out) >= maxSkills {
			break
		}
		size := len(s.Name) + len(descriptionOf(s))
		if maxTotalChars > 0 && totalChars+size > maxTotalChars {
			break
		}
		out = append(out, s)
		totalChars += size
	}
	return out
}

// keywordSet 由 query 构建小写关键词集合（长度 >= 2 的字母数字片段）。
func keywordSet(query string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !(r >= 0x4e00 && r <= 0x9fff)
	}) {
		if len([]rune(w)) >= 2 {
			set[w] = true
		}
	}
	return set
}

// overlapScore 计算技能与 query 的关键词重叠数。
func overlapScore(s *skillpkg.Skill, queryWords map[string]bool) int {
	score := 0
	for w := range keywordSet(s.Name + " " + descriptionOf(s)) {
		if queryWords[w] {
			score++
		}
	}
	return score
}
```

`buildSkillsSection()` 改为接收 query，并在 `filterSkills` 之后调用预算：

```go
	skills := r.filterSkills(r.collectSkillsInOrder())
	skills = applySkillBudget(skills, query, r.maxSkills, r.maxTotalChars)
```

`BeforeModelCall` 传入 `cbc` 中的当前 query（`interfaces.InvokeQuery.PlainText()`；不可用时传空串）。

- [ ] **Step 4: 配置与透传**

`schema/config.go` 的 `DeepAgentConfig` 追加：

```go
	// SkillBudgetMaxSkills 技能提示词最大技能数，0 表示不限
	SkillBudgetMaxSkills int `json:"skill_budget_max_skills,omitempty"`
	// SkillBudgetMaxTotalChars 技能提示词字符总预算，0 表示不限
	SkillBudgetMaxTotalChars int `json:"skill_budget_max_total_chars,omitempty"`
```

`factory.go` 组装 config 处追加两行赋值；SkillUseRail 构造处追加 `WithSkillBudget(cfg.SkillBudgetMaxSkills, cfg.SkillBudgetMaxTotalChars)`（新增 Option）。

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/harness/rails/skills/ ./internal/agentcore/harness/ -run 'TestApplySkillBudget|TestSkillUseRail' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail.go internal/agentcore/harness/rails/skills/skill_use_rail_test.go internal/agentcore/harness/schema/config.go internal/agentcore/harness/factory.go
git commit -m "feat(harness): 技能提示词支持数量与字符预算"
```

---

## Task 13: 台账与计划状态同步

**Files:**
- Modify: `docs/sync/defect-plan-2026-11-04.md`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新缺陷台账 A 组状态**

按 spec §3 表格把 A-01~A-12 的「状态」列改为 ✅ / ⤵️ / ➖，A-09 行删除无效 SHA `1c4f5603` 并注明「⤵️ 领域九 9.30」，A-04 行注明「⤵️ 领域二十三」，A-12 行注明「wikipedia 部分随工具实现」。

- [ ] **Step 2: 更新实现计划**

- `IMPLEMENTATION_PLAN.md` 6.4 条目「10 种事件」改为「12 种事件（含 on_user_message / after_react_iteration）」
- 领域九「9.x 功能 Rails」新增一行：`| 9.30 | ☐ | ModelAnomalyDetectionRail | 瞬时失败分类 + 退避重试 + 退化重复检测 + 流超时识别 + 工具循环压缩（须背靠背相邻性判定，589da12a）+ bailout | openjiuwen/harness/rails/model_anomaly_detection_rail.py |`
- 领域二十三追加 A-04 对应条目（application 层 LLMController 工作流恢复）

- [ ] **Step 3: 提交**

```bash
git add docs/sync/defect-plan-2026-11-04.md IMPLEMENTATION_PLAN.md
git commit -m "docs(sync): 同步 A 组缺陷处置状态与计划条目"
```

---

## 验收（全部任务完成后）

```bash
# 1) 单包与 -race
go test -race ./internal/agentcore/single_agent/... ./internal/agentcore/harness/... \
  ./internal/agentcore/foundation/llm/... ./internal/agentcore/session/tracer/...

# 2) 集成合规（两层门禁）
go run ./cmd/integration-audit -verify

# 3) 覆盖率（改动行门禁 85% / 5%）
go test -coverprofile=coverage.out ./internal/agentcore/single_agent/... ./internal/agentcore/harness/...

# 4) 推送并盯 CI 四个 job（Build / Lint / Unit / Integration）全绿
git push && gh run list --limit 3
```

**完成标准**：CI 四个 job 全 SUCCESS。本地通过不算完成（项目规则 6）。

---

## 附录 A：测试助手（Task 2 / 4 / 5 / 9 共用）

以下助手统一加在对应包的 `_test.go` 里。**先 grep 复用现成 fake，不要新造同类**：现有 `fakeModelClient`、`fakeModelContext`、`fakeContextEngine`、`initFakeModelClient()` 位于 `internal/agentcore/single_agent/agents/react_agent_test.go:1046-1180`。

### A.1 脚本化模型客户端（Task 4 / 11 需要按轮次返回不同响应）

现有 `fakeModelClient` 的注册工厂固定返回 `"测试回复"`，无法控制多轮响应。扩展 fake 的字段与 `Invoke`：

```go
// 追加到 fakeModelClient 结构体
	// script 按顺序返回的脚本响应（streamChunks 之外的非流式脚本）
	script []*llmschema.AssistantMessage
	// scriptIdx 当前脚本下标
	scriptIdx *int
```

```go
// 在 fakeModelClient.Invoke 的 invokeErr 判断之后插入
	if len(f.script) > 0 {
		idx := 0
		if f.scriptIdx != nil {
			idx = *f.scriptIdx
		}
		if idx >= len(f.script) {
			idx = len(f.script) - 1
		}
		if f.scriptIdx != nil {
			*f.scriptIdx = idx + 1
		}
		return f.script[idx], nil
	}
```

独立 provider + 构造助手（避免与 `sync.Once` 的默认 provider 冲突）：

```go
var (
	scriptedMu       sync.Mutex
	scriptedScript   []*llmschema.AssistantMessage
	scriptedOnce     sync.Once
	scriptedProvider = "fake_scripted_provider"
)

func initScriptedModelClient() {
	scriptedOnce.Do(func() {
		model_clients.GetClientRegistry().Register(scriptedProvider, "llm",
			func(*llmschema.ModelRequestConfig, *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
				scriptedMu.Lock()
				defer scriptedMu.Unlock()
				idx := new(int)
				return &fakeModelClient{script: scriptedScript, scriptIdx: idx}, nil
			})
	})
}

// newAgentWithScriptedModel 构造按轮次返回脚本响应的 agent。
func newAgentWithScriptedModel(script []*llmschema.AssistantMessage, maxIterations int) *ReActAgent {
	scriptedMu.Lock()
	scriptedScript = script
	scriptedMu.Unlock()
	initScriptedModelClient()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("scripted_agent"),
		agentschema.WithAgentDescription("脚本模型"),
	)
	cfg := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(scriptedProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(maxIterations),
	)
	agent := NewReActAgent(card, cfg)
	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: scriptedProvider, ClientID: scriptedProvider},
		&llmschema.ModelRequestConfig{ModelName: "test-model"},
	)
	if err != nil {
		panic(err)
	}
	agent.llm = model
	return agent
}
```

### A.2 循环调用样板（Task 4 / 1）

```go
// newLoopCBC 构造 reactLoop 需要的回调上下文。
func newLoopCBC(t *testing.T, agent *ReActAgent, sessionID string) (*interfaces.AgentCallbackContext, *session.Session) {
	t.Helper()
	sess := session.NewSession(session.WithSessionID(sessionID))
	cbc := interfaces.NewAgentCallbackContext(agent,
		&interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}, sess)
	return cbc, sess
}
```

### A.3 fakeModelContext 内容断言

```go
// containsContent 判断上下文中是否出现指定内容片段。
func (f *fakeModelContext) containsContent(substr string) bool {
	msgs, _ := f.GetMessages(0, true)
	for _, m := range msgs {
		if strings.Contains(m.GetContent().String(), substr) {
			return true
		}
	}
	return false
}
```

### A.4 技能小节测试（Task 2）

`skillUtil` 字段类型为 `*skills.SkillUtil`（`single_agent/skills/skill_util.go:19`）。构造方式复用 skills 包测试里已有的 fake provider：

```go
// newAgentWithSkillUtil 构造带 SkillUtil 的 agent；skillDir 为空表示未注册技能。
func newAgentWithSkillUtil(t *testing.T, skillDir string) *ReActAgent {
	t.Helper()
	agent := newAgentWithFakeLLM("skill_section_"+strings.ReplaceAll(t.Name(), "/", "_"),
		&fakeModelClient{})
	agent.skillUtil = skills.NewSkillUtilWithProvider("test-op", newTestFsProvider(skillDir))
	return agent
}
```

`newTestFsProvider(dir)` 从 `internal/agentcore/single_agent/skills/*_test.go` 中已有的 fake `FsProvider` 复制（`grep -n "FsProvider" internal/agentcore/single_agent/skills/*_test.go` 定位），指向 `t.TempDir()` 下写入的 `SKILL.md`（front-matter 含 `name: demo-skill`）。

### A.5 工具执行 force-finish 测试（Task 5）

```go
// newManagerWithForceFinishRail 构造带"before_tool_call 请求 ff"回调的 AbilityManager。
// 返回的 *int 记录工具真实执行次数。
func newManagerWithForceFinishRail(t *testing.T, toolName string) (*AbilityManager, *interfaces.AgentCallbackContext, *int) {
	t.Helper()
	am := NewAbilityManager()
	am.Add(newCountingTool(toolName)) // 复用本包测试已有的计数工具构造；无则用 tool.NewToolCardWithID + fake 执行器

	sess := session.NewSession(session.WithSessionID("ff_sess"))
	cbc := interfaces.NewAgentCallbackContext(nil,
		&interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hi")}, sess)
	// 通过 ForkForToolCall 之后的回调管理器注册 before_tool_call
	// （具体注册路径参照 ability_manager_test.go:1163 已有 ff 用例）
	return am, cbc, new(int)
}
```

**若本包已有 force-finish 相关测试**（`ability_manager_test.go:1163` 处用了 `cbc.RequestForceFinish`），优先扩展该用例而不是新建助手。

### A.6 能力管理器桩（Task 1 需要构造"工具轮"）

```go
// stubAbilityManager 提供最小可用的能力管理器：ListToolInfo 返回 1 个工具，
// Execute 直接返回带 ToolMsg 的成功结果，不触达真实工具与资源管理器。
type stubAbilityManager struct{}

func newStubAbilityManager() *stubAbilityManager { return &stubAbilityManager{} }

func (s *stubAbilityManager) Add(schema.Ability) agentschema.AddAbilityResult { return agentschema.AddAbilityResult{} }
func (s *stubAbilityManager) AddMany([]schema.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (s *stubAbilityManager) Remove(string) schema.Ability        { return nil }
func (s *stubAbilityManager) RemoveMany([]string) []schema.Ability { return nil }
func (s *stubAbilityManager) Get(string) schema.Ability            { return nil }
func (s *stubAbilityManager) List() []schema.Ability               { return nil }

func (s *stubAbilityManager) ListToolInfo(context.Context, []string, ...string) ([]schema.ToolInfoInterface, error) {
	return []schema.ToolInfoInterface{
		schema.NewToolInfo("read_file", "read a file", map[string]any{"type": "object"}),
	}, nil
}

func (s *stubAbilityManager) Execute(
	_ context.Context,
	_ *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	_ sessioninterfaces.SessionFacade,
	_ string,
) []agentschema.ExecuteResult {
	results := make([]agentschema.ExecuteResult, 0, len(toolCalls))
	for _, tc := range toolCalls {
		results = append(results, agentschema.ExecuteResult{
			Result:  map[string]any{"ok": true},
			ToolMsg: llmschema.NewToolMessage(tc.ID, "ok"),
		})
	}
	return results
}

func (s *stubAbilityManager) SetContextEngine(ceinterface.ContextEngine) {}
func (s *stubAbilityManager) ReorderTools([]string)                     {}
```

`schema.NewToolInfo` 若签名不同，用 `schema.NewToolInfoWithParameters(...)` 或直接构造 `&schema.ToolInfo{Name: "read_file", Description: "read a file"}`（以 `grep -n "func NewToolInfo" internal/agentcore/foundation/tool/schema/*.go` 为准）。

