# Go ReActAgent Stream/Invoke 与 Python 语义对齐 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Go ReActAgent 的 stream/invoke 流程与 Python 语义对齐，修复 9 项差异

**Architecture:** 在现有 ReActAgent 代码基础上，逐项修改 react_invoke.go、react_helpers.go、react_model_call.go，每项修改后补充测试并验证

**Tech Stack:** Go 1.22+, testify/assert, 项目内 logger/rail/session/stream 包

---

## File Structure

| 文件 | 变更类型 | 职责 |
|------|---------|------|
| `internal/agentcore/single_agent/agents/react_invoke.go` | 修改 | 差异 4/7/9/10/11/12 的主要修改点 |
| `internal/agentcore/single_agent/agents/react_helpers.go` | 修改 | 差异 15（initContext context_processors + reloader） |
| `internal/agentcore/single_agent/agents/react_model_call.go` | 修改 | 差异 13/14（preview messages + LLM 诊断日志） |
| `internal/agentcore/single_agent/agents/react_agent_test.go` | 修改 | 补充测试 |

---

### Task 1: 差异 7 — Cleanup 始终执行（用 defer）

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:24-176`

- [ ] **Step 1: 在 InvokeImpl 中用 defer 实现 cleanup 始终执行**

将 L147-163 的 cleanup 逻辑改为 defer，放在 FireLifecycle 调用之前。修改 `InvokeImpl` 方法体：

```go
func (a *ReActAgent) InvokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	conversationID, _ := inputs["conversation_id"].(string)

	needCleanup := false
	if sess == nil {
		sessionID := conversationID
		if sessionID == "" {
			sessionID = "default_session"
		}
		newSess := session.NewSession(session.WithSessionID(sessionID))
		if err := newSess.PreRun(ctx, inputs); err != nil {
			return nil, err
		}
		sess = newSess
		needCleanup = true
	}

	invokeQuery := rail.QueryFromInputs(inputs)
	invokeInputs := &rail.InvokeInputs{
		Query:          invokeQuery,
		ConversationID: sess.GetSessionID(),
	}
	cbc := rail.NewAgentCallbackContext(a, invokeInputs, sess)

	if userID, ok := inputs["user_id"].(string); ok {
		cbc.Extra()["user_id"] = userID
	}
	if runKind, ok := inputs["run_kind"].(string); ok {
		cbc.Extra()["run_kind"] = runKind
	}
	if runContext, ok := inputs["run_context"].(string); ok {
		cbc.Extra()["run_context"] = runContext
	}
	if streaming, ok := inputs["_streaming"].(bool); ok {
		cbc.Extra()["_streaming"] = streaming
	} else {
		cbc.Extra()["_streaming"] = false
	}
	if sq, ok := inputs["_steering_queue"]; ok {
		if ch, ok2 := sq.(chan string); ok2 {
			cbc.BindSteeringQueue(ch)
		}
	}

	// 对齐 Python finally: cleanup 始终执行（无论 FireLifecycle 是否返回错误）
	if needCleanup {
		defer func() {
			a.saveContexts(sess)
			if as, ok := sess.(*session.Session); ok {
				_ = as.CloseStream()
				_ = as.Commit(ctx)
			}
		}()
	}

	var result map[string]any
	var loopErr error

	err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
		// ... (FireLifecycle 内部逻辑不变)
		// 此处省略，仅展示关键修改点
		return loopErr
	})

	// context.Canceled 时清除上下文消息
	if err != nil && ctx.Err() == context.Canceled {
		a.ClearContextMessages(sess)
	}
	if err != nil {
		return nil, err
	}

	// 对齐 Python L1434: return ctx.extra.get("invoke_result", invoke_inputs.result)
	if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
		if r, ok2 := invokeResult.(map[string]any); ok2 {
			return r, nil
		}
	}

	if invokeInputs.Result != nil {
		return invokeInputs.Result, nil
	}
	return result, nil
}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "fix: InvokeImpl 用 defer 实现 cleanup 始终执行，对齐 Python try/finally 语义"
```

---

### Task 2: 差异 10 — 工具执行整体错误终止循环

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:444-447`

- [ ] **Step 1: 修改 reactLoop 中 executeToolCalls 的错误处理**

将 L444-447 从"只记日志继续循环"改为"终止循环"：

```go
		// 执行工具
		results, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx)
		if err != nil {
			logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("工具执行失败")
			return nil, fmt.Errorf("工具执行失败: %w", err)
		}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "fix: reactLoop 中工具执行整体错误终止循环，对齐 Python"
```

---

### Task 3: 差异 11 — 迭代计数日志

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:404`

- [ ] **Step 1: 在 reactLoop 循环体开头添加迭代计数日志**

在 `for iteration := startIteration; iteration < maxIter; iteration++ {` 之后、steering 注入之前插入：

```go
		logger.Info(logComponent).
			Int("iteration", iteration+1).
			Int("max_iterations", maxIter).
			Msg("ReAct 迭代")
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "feat: reactLoop 补充迭代计数日志，对齐 Python L1355"
```

---

### Task 4: 差异 4 — System prompt 构建移到 invoke 入口

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:78-144`

- [ ] **Step 1: 在 FireLifecycle 内部、initContext 之后、reactLoop 之前添加 system prompt 构建**

在 InvokeImpl 的 FireLifecycle 闭包内，`cbc.SetModelContext(modelCtx)` 之后、`startIteration` 逻辑之前，插入：

```go
		// 对齐 Python L1317-1326: 在 invoke 入口构建 system prompt
		renderedPrompt := a.promptBuilder.Build()
		a.promptBuilder.AddSection(_IdentitySection, renderedPrompt, 0)
		// ⤵️ Skill: a.updateSkillPromptBuilderSection(renderedPrompt)
```

其中 `_IdentitySection` 常量已在 `react_prompt.go` 中定义（需确认常量名和优先级值）。同时将 `reactLoop` 中 `tools` 的获取移到此位置（如果 reactLoop 内也有 getTools，提取为参数避免重复调用）。

- [ ] **Step 2: 确认 _IdentitySection 常量和 AddSection 方法存在**

检查 `react_prompt.go` 中是否已有 `_IdentitySection` 和 `AddSection` 方法。如果不存在，需要定义：

```go
const _IdentitySection = "identity"
const _IdentitySectionPriority = 0
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_prompt.go
git commit -m "feat: InvokeImpl 中在 lifecycle 内构建 system prompt，对齐 Python L1317-1326"
```

---

### Task 5: 差异 9 — StreamModes 传递

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:190-202`（StreamImpl）

- [ ] **Step 1: 在 StreamImpl 中自建 session 时提取 StreamModes 并传给 StreamWriterManager**

修改 StreamImpl 中 `if sess == nil` 分支：

```go
	if sess == nil {
		sessionID := conversationID
		if sessionID == "" {
			sessionID = "default_session"
		}
		// 对齐 Python: stream_modes 传入 StreamWriterManager
		agentOptsForModes := interfaces.NewAgentOptions(opts...)
		modes := agentOptsForModes.StreamModes

		var newSess *session.Session
		if len(modes) > 0 {
			emitter := stream.NewStreamEmitter()
			mgr := stream.NewStreamWriterManager(emitter, modes...)
			newSess = session.NewSession(
				session.WithSessionID(sessionID),
				session.WithStreamWriterManager(mgr),
			)
		} else {
			newSess = session.NewSession(session.WithSessionID(sessionID))
		}
		if err := newSess.PreRun(ctx, inputs); err != nil {
			return nil, err
		}
		sess = newSess
		opts = append(opts, interfaces.WithSession(sess))
		needCleanup = true
	}
```

- [ ] **Step 2: 确认 StreamEmitter/NewStreamWriterManager 的 import 路径**

确认 `stream.NewStreamEmitter` 和 `stream.NewStreamWriterManager` 在当前文件 import 中是否已存在。如果不存在需要添加。

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "feat: StreamImpl 提取 StreamModes 传给 StreamWriterManager，对齐 Python stream_modes"
```

---

### Task 6: 差异 12 — 多模态工具结果消息

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:491-529`（executeToolCalls）

- [ ] **Step 1: 新增 buildMultimodalToolResultsMessage 方法**

在 `react_invoke.go` 的非导出函数区域新增：

```go
// buildMultimodalToolResultsMessage 从工具结果中提取多模态图片数据，
// 构建包含 image_url content blocks 的 UserMessage。
//
// 对应 Python: ReActAgent._build_multimodal_tool_results_message()
func (a *ReActAgent) buildMultimodalToolResultsMessage(results []ability.ExecuteResult) llmschema.BaseMessage {
	var content []map[string]any
	var loadedPaths []string

	for _, r := range results {
		for _, item := range a.iterMultimodalImageItems(r.Result) {
			sourcePath, _ := item["source_path"].(string)
			if sourcePath == "" {
				sourcePath = "unknown image"
			}
			dataURL, _ := item["data_url"].(string)
			loadedPaths = append(loadedPaths, sourcePath)
			content = append(content, map[string]any{
				"type": "text",
				"text": fmt.Sprintf("Image loaded from read_file: %s", sourcePath),
			})
			content = append(content, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": dataURL,
				},
			})
		}
	}

	if len(content) == 0 {
		return nil
	}

	if len(loadedPaths) > 1 {
		summaryLines := []string{"Images loaded by tool results:"}
		for i, path := range loadedPaths {
			summaryLines = append(summaryLines, fmt.Sprintf("%d. %s", i+1, path))
		}
		content = append([]map[string]any{{
			"type": "text",
			"text": strings.Join(summaryLines, "\n"),
		}}, content...)
	}

	return llmschema.NewUserMessageWithContent(content)
}

// iterMultimodalImageItems 从工具结果中迭代多模态图片项。
//
// 对应 Python: ReActAgent._iter_multimodal_image_items()
func (a *ReActAgent) iterMultimodalImageItems(toolResult any) []map[string]any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return nil
	}
	data, _ := resultMap["data"].(map[string]any)
	if data == nil {
		return nil
	}
	multimodalItems, _ := data["multimodal"].([]any)
	if multimodalItems == nil {
		return nil
	}

	var imageItems []map[string]any
	for _, itemAny := range multimodalItems {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		if itemType, _ := item["type"].(string); itemType != "image" {
			continue
		}
		dataURL, _ := item["data_url"].(string)
		if !strings.HasPrefix(dataURL, "data:image/") {
			continue
		}
		imageItems = append(imageItems, item)
	}
	return imageItems
}
```

- [ ] **Step 2: 在 executeToolCalls 末尾调用 buildMultimodalToolResultsMessage**

在 `executeToolCalls` 的 `for _, r := range results` 循环后添加：

```go
	// 对齐 Python L866-870: 多模态工具结果写入上下文
	multimodalMsg := a.buildMultimodalToolResultsMessage(results)
	if multimodalMsg != nil && modelCtx != nil {
		_, _ = modelCtx.AddMessages(ctx, multimodalMsg)
	}
```

- [ ] **Step 3: 确认 NewUserMessageWithContent 方法存在**

检查 `llmschema` 包中是否已有 `NewUserMessageWithContent` 方法（接受 `[]map[string]any` content blocks 的构造函数）。如果不存在需要添加。

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestExecuteToolCalls ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "feat: executeToolCalls 支持多模态工具结果消息，对齐 Python _build_multimodal_tool_results_message"
```

---

### Task 7: 差异 13 — Preview messages 加 system prompt 前缀

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go:30-33`

- [ ] **Step 1: 修改 callModel 中 preview messages 构建**

将 L30-33 改为在 preview messages 前插入 system prompt：

```go
	// 对齐 Python L619-625: preview messages 包含 system prompt 前缀
	previewMsgs := make([]llmschema.BaseMessage, 0)
	previewPrompt := a.promptBuilder.Build()
	if previewPrompt != "" {
		previewMsgs = append(previewMsgs, llmschema.NewSystemMessage(previewPrompt))
	}
	if modelCtx != nil {
		previewMsgs = append(previewMsgs, modelCtx.GetMessages(0, true)...)
	}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestCallModel ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_model_call.go
git commit -m "fix: callModel preview messages 加入 system prompt 前缀，对齐 Python _build_preview_messages"
```

---

### Task 8: 差异 14 — LLM 请求/响应诊断日志

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`

- [ ] **Step 1: 新增 logLLMRequest 和 logLLMResponse 辅助函数**

在 `react_model_call.go` 非导出函数区域新增：

```go
// logLLMRequest 记录 LLM 请求诊断日志。
//
// 对应 Python: log_llm_request()
func logLLMRequest(messages []llmschema.BaseMessage, tools []*cschema.ToolInfo) {
	msgCount := len(messages)
	toolCount := len(tools)
	logger.Info(logComponent).
		Int("msg_count", msgCount).
		Int("tool_count", toolCount).
		Msg("[LLM] >>> request")

	for idx, msg := range messages {
		role := msg.Role()
		contentStr := msg.Content().Text()
		if len(contentStr) > 300 {
			contentStr = contentStr[:300]
		}
		fields := []any{
			Str("msg_idx", fmt.Sprintf("[%d]", idx)),
			Str("role", role),
		}
		if contentStr != "" {
			fields = append(fields, Str("content", contentStr))
		}
		logger.Info(logComponent).
			Fields(fields).
			Msg("[LLM]   msg")
	}
}

// logLLMResponse 记录 LLM 响应诊断日志。
//
// 对应 Python: log_llm_response()
func logLLMResponse(aiMsg *llmschema.AssistantMessage) {
	if aiMsg == nil {
		return
	}
	contentLen := len(aiMsg.Content.Text())
	tcCount := len(aiMsg.ToolCalls)

	fields := []any{
		Int("content_len", contentLen),
		Int("tool_call_count", tcCount),
	}
	if aiMsg.UsageMetadata != nil {
		fields = append(fields,
			Int("input_tokens", aiMsg.UsageMetadata.InputTokens),
			Int("output_tokens", aiMsg.UsageMetadata.OutputTokens),
		)
	}
	logger.Info(logComponent).
		Fields(fields).
		Msg("[LLM] <<< response")

	for _, tc := range aiMsg.ToolCalls {
		logger.Info(logComponent).
			Str("tool_name", tc.Name).
			Str("args", tc.Arguments).
			Msg("[LLM]   tool_call")
	}
}
```

- [ ] **Step 2: 在 railedModelCall 中 LLM 调用前插入 logLLMRequest**

在 `railedModelCall` 中 `log_llm_request` 的等价位置（GetContextWindow 之后、实际 LLM 调用之前），插入：

```go
	// 对齐 Python L730: log_llm_request
	logLLMRequest(messages, contextTools)
```

- [ ] **Step 3: 在 callModel 中 LLM 调用后插入 logLLMResponse**

在 `callModel` 的 `rail.ModelCallRail.Execute` 返回后，插入：

```go
	// 对齐 Python L659: log_llm_response
	if result != nil {
		logLLMResponse(result)
	}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_model_call.go
git commit -m "feat: 补充 LLM 请求/响应诊断日志，对齐 Python log_llm_request/log_llm_response"
```

---

### Task 9: 差异 15 — initContext 使用 context_processors + reloader

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go:20-26`

- [ ] **Step 1: 修改 initContext 方法**

将当前简单的 `CreateContext` 调用替换为完整逻辑：

```go
// initContext 初始化上下文引擎。
//
// 对应 Python: ReActAgent._init_context()
func (a *ReActAgent) initContext(ctx context.Context, sess sessioninterfaces.SessionFacade) (ceinterface.ModelContext, error) {
	if a.contextEngine == nil {
		return nil, nil
	}

	// 1. 对齐 Python L1225-1229: 传递 context_processors
	var opts []ceinterface.CreateContextOption
	if a.config != nil && len(a.config.ContextProcessors) > 0 {
		opts = append(opts, ceinterface.WithProcessors(a.config.ContextProcessors))
	}
	modelCtx, err := a.contextEngine.CreateContext(ctx, "default_context", sess, opts...)
	if err != nil {
		return nil, fmt.Errorf("创建上下文失败: %w", err)
	}

	// 2. 对齐 Python L1234-1241: reloader tool 动态注册/注销
	reloaderTool := modelCtx.ReloaderTool()
	am := a.getAbilityManager()
	if a.config != nil && a.config.ContextEngineConfig.EnableReload {
		if am != nil && reloaderTool != nil {
			// 对齐 Python: self.ability_manager.add(context_reloader.card)
			am.Add(reloaderTool)
		}
		// ⤵️ Runner.resource_mgr 注册（需要 Runner 集成）
	} else {
		if am != nil && reloaderTool != nil {
			// 对齐 Python: self.ability_manager.remove(context_reloader.card.name)
			am.Remove(reloaderTool.Card().Name)
		}
	}

	return modelCtx, nil
}
```

- [ ] **Step 2: 确认 AbilityManager.Add/Remove 方法接受 tool.Tool**

检查 `ability.AbilityManager.Add()` 方法签名是否接受 `tool.Tool` 参数，以及 `Remove()` 方法签名是否接受 `string`（tool name）。

- [ ] **Step 3: 确认 tool.Tool 接口有 Card() 方法**

检查 `tool.Tool` 接口是否有 `Card()` 方法返回包含 `Name` 字段的结构体。

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 -run TestReActAgent ./internal/agentcore/single_agent/agents/... -v 2>&1 | head -50`
Expected: 现有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_helpers.go
git commit -m "feat: initContext 传递 context_processors 和管理 reloader tool，对齐 Python _init_context"
```

---

### Task 10: 编译验证 + 全量测试

**Files:**
- 无新文件

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 2: 运行 agents 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 ./internal/agentcore/single_agent/agents/... -v 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 3: 运行单 agent 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test -count=1 ./internal/agentcore/single_agent/... -v 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 4: 最终提交（如有遗漏修复）**

```bash
git add -A
git commit -m "chore: ReActAgent stream/invoke Python 语义对齐完成"
```
