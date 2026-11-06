# ModelAnomalyDetectionRail（9.86 + 2.18）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Go 侧具备与上游一致的模型异常护栏——退化重复输出重试、流帧超时重试、瞬时失败退避重试、连续相同工具调用压缩/兜底中止——并在产品两条装配路径上生效。

**Architecture:** 三层交付。①地基：给 `AgentCallbackContext` 加"流式分片检查器"通道与"可见输出已发"标志，内层 ReAct 流循环逐 chunk 调用；②LLM 层：`Model.Stream` 用 select 接收超时实现首帧/空闲预算，超时投递 `StreamError` 哨兵；③rail：`ModelAnomalyDetectionRail`（priority 70）挂 4 个钩子，靠 `RequestRetry` 复用 `RailExecutor` 的整调用重试。装配走 factory 默认 + adapter `execution_guard` + 团队成员三处，成对传 `*bool` 开关避免"配置关不掉/开不了"。

**Tech Stack:** Go 1.22+、`internal/agentcore/harness/rails`（DeepAgentRail）、`single_agent/rail.RailExecutor`、`runner/callback.AbortError`、`foundation/llm` Model 门面、`testify`、`tests/integration/mockllm`。

**上游对照：** `agent-core@5a35c39f`：`openjiuwen/harness/rails/model_anomaly_detection_rail.py`（917 行）、`core/foundation/llm/model.py:228-330`、`schema/config.py:152-166`、`schema/message_chunk.py:242-256`、`agents/react_agent.py:1877-1905`；`jiuwenswarm`：`interface_deep.py:11036-11078/11532/13239`、`member_rails.py:613`、`config_specs.py:113`、`resources/config.yaml:1028-1048`。

**设计文档：** `docs/superpowers/specs/2026-11-06-model-anomaly-detection-rail-design.md`

---

## 文件结构

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/agentcore/single_agent/interfaces/callback.go` | 检查器通道与可见输出标志的访问器 | 修改 |
| `internal/agentcore/single_agent/interfaces/callback_stream_inspector_test.go` | 上述访问器单测 | 新建 |
| `internal/agentcore/single_agent/agents/react_model_call.go` | 流循环接 `cbc`、逐 chunk 调检查器、置可见输出标志 | 修改 |
| `internal/agentcore/single_agent/agents/react_model_call_test.go` | 上述行为单测 | 修改 |
| `internal/agentcore/foundation/llm/schema/config.go` | 两个超时配置字段 + 选项 + 构造默认 | 修改 |
| `internal/agentcore/foundation/llm/schema/config_test.go` | 配置三态单测 | 修改 |
| `internal/agentcore/foundation/llm/schema/message_chunk.go` | `CarriesOutputToken` | 修改 |
| `internal/agentcore/foundation/llm/schema/message_chunk_test.go` | 上述单测 | 修改 |
| `internal/agentcore/foundation/llm/model.go` | 首帧/空闲超时 | 修改 |
| `internal/agentcore/foundation/llm/model_timeout_test.go` | 超时矩阵单测 | 新建 |
| `internal/agentcore/foundation/llm/model_clients/openai/client.go` | HTTP 错误头部落 params | 修改 |
| `internal/agentcore/foundation/llm/model_clients/openai/client_test.go` | 上述单测 | 修改 |
| `internal/agentcore/harness/rails/model_anomaly.go` | rail 本体（1:1 移植） | 新建 |
| `internal/agentcore/harness/rails/model_anomaly_test.go` | rail 单测 | 新建 |
| `internal/agentcore/harness/harness_config/params.go` | `EnableModelAnomalyDetectionRail *bool` | 修改 |
| `internal/agentcore/harness/factory.go` | 默认注册 + 去重 | 修改 |
| `internal/agentcore/harness/factory_anomaly_rail_test.go` | 工厂注册单测 | 新建 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | `buildModelAnomalyDetectionRail` + 顺序 | 修改 |
| `internal/swarm/server/adapter/deep_adapter.go` | 传 `*bool` + 存字段 | 修改 |
| `internal/swarm/agents/harness/team/runtime_inheritance.go` | 团队成员 builder | 修改 |
| `resources/config.yaml` | `execution_guard` 段 | 修改 |
| `tests/integration/mockllm/mock_client.go` | 重复流/停滞两种脚本化流 | 修改 |
| `tests/integration/agentcore/model_anomaly/*.go` | 三个集成闭环 | 新建 |

提交边界：Task 1-2 → commit 1；Task 3-6 → commit 2；Task 7-11 → commit 3；Task 12-16 → commit 4。

---

## Task 1: 检查器通道 + 可见输出标志（interfaces）

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/callback.go`（常量区、`AgentCallbackContext` 方法区）
- Test: `internal/agentcore/single_agent/interfaces/callback_stream_inspector_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package interfaces

import (
	"context"
	"errors"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// inspectorA / inspectorB 代码指针不同的两个检查器，用于验证去重语义。
func inspectorA(_ context.Context, _ *AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
	return nil
}

func inspectorB(_ context.Context, _ *AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
	return errors.New("b")
}

func TestStreamChunkInspectors_读写往返(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	if got := cbc.StreamChunkInspectors(); len(got) != 0 {
		t.Fatalf("初始应为空，实际 %d 条", len(got))
	}

	cbc.SetStreamChunkInspectors([]StreamChunkInspectorFunc{inspectorA, inspectorB})
	got := cbc.StreamChunkInspectors()
	if len(got) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(got))
	}

	// 返回副本：调用方修改切片不得影响内部状态
	got[0] = nil
	if cbc.StreamChunkInspectors()[0] == nil {
		t.Fatal("StreamChunkInspectors 必须返回副本")
	}

	cbc.SetStreamChunkInspectors(nil)
	if len(cbc.StreamChunkInspectors()) != 0 {
		t.Fatal("Set(nil) 应清空")
	}
}

func TestModelVisibleOutputEmitted_默认false(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	if cbc.ModelVisibleOutputEmitted() {
		t.Fatal("默认应为 false")
	}
	cbc.SetModelVisibleOutputEmitted(true)
	if !cbc.ModelVisibleOutputEmitted() {
		t.Fatal("设置后应为 true")
	}
	cbc.SetModelVisibleOutputEmitted(false)
	if cbc.ModelVisibleOutputEmitted() {
		t.Fatal("复位后应为 false")
	}
}
```

- [ ] **Step 2: 跑测试，确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestStreamChunkInspectors|TestModelVisibleOutputEmitted' ./internal/agentcore/single_agent/interfaces/ -v
```
Expected: FAIL —— `undefined: StreamChunkInspectorFunc` / `cbc.StreamChunkInspectors undefined`

- [ ] **Step 3: 实现**

在 `callback.go` 的「常量」区块加：

```go
// ExtraKeyStreamChunkInspectors 流式分片检查器列表的 extra 键。
// 字符串与上游一致，便于与 Python 侧对拍。
// Python: model_anomaly_detection_rail._STREAM_CHUNK_INSPECTORS_KEY
const ExtraKeyStreamChunkInspectors = "_stream_chunk_inspectors"

// ExtraKeyModelVisibleOutputEmitted 本次模型调用是否已向用户输出可见内容。
// Python: rail/base.py MODEL_VISIBLE_OUTPUT_EMITTED_KEY
const ExtraKeyModelVisibleOutputEmitted = "_model_visible_output_emitted"
```

在「结构体」区块（`AgentCallbackContext` 定义之后）加类型：

```go
// StreamChunkInspectorFunc 流式分片检查器。
//
// Python 把 bound method 存进 ctx.extra["_stream_chunk_inspectors"] 列表；
// Go 用函数值等价（method value 即绑定 receiver 的函数值）。去重由调用方
// （rail）按代码指针判断，对齐 Python 的 __self__ 比较。
type StreamChunkInspectorFunc func(ctx context.Context, cbc *AgentCallbackContext, chunk *llmschema.AssistantMessageChunk) error
```

在 `AgentCallbackContext` 方法区（`DeleteExtra` 之后）加：

```go
// StreamChunkInspectors 返回当前已注册的流式分片检查器（副本）。
//
// Python: react_agent.py L1897 — ctx.extra.get("_stream_chunk_inspectors") or []
func (c *AgentCallbackContext) StreamChunkInspectors() []StreamChunkInspectorFunc {
	v, ok := c.GetExtra(ExtraKeyStreamChunkInspectors)
	if !ok {
		return nil
	}
	list, ok := v.([]StreamChunkInspectorFunc)
	if !ok {
		return nil
	}
	out := make([]StreamChunkInspectorFunc, len(list))
	copy(out, list)
	return out
}

// SetStreamChunkInspectors 覆盖流式分片检查器列表；空列表等价于清除。
func (c *AgentCallbackContext) SetStreamChunkInspectors(list []StreamChunkInspectorFunc) {
	if len(list) == 0 {
		c.DeleteExtra(ExtraKeyStreamChunkInspectors)
		return
	}
	cp := make([]StreamChunkInspectorFunc, len(list))
	copy(cp, list)
	c.SetExtra(ExtraKeyStreamChunkInspectors, cp)
}

// SetModelVisibleOutputEmitted 标记本次模型调用是否已输出可见内容。
//
// Python: react_agent.py L1908/L1918 — ctx.extra[MODEL_VISIBLE_OUTPUT_EMITTED_KEY] = True
func (c *AgentCallbackContext) SetModelVisibleOutputEmitted(emitted bool) {
	c.SetExtra(ExtraKeyModelVisibleOutputEmitted, emitted)
}

// ModelVisibleOutputEmitted 返回本次模型调用是否已输出可见内容。
func (c *AgentCallbackContext) ModelVisibleOutputEmitted() bool {
	v, ok := c.GetExtra(ExtraKeyModelVisibleOutputEmitted)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestStreamChunkInspectors|TestModelVisibleOutputEmitted' ./internal/agentcore/single_agent/interfaces/ -v
```
Expected: PASS（2 个 Test）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/interfaces/callback.go internal/agentcore/single_agent/interfaces/callback_stream_inspector_test.go
git commit -m "feat(agentcore): 流式分片检查器与可见输出标志访问器"
```

---

## Task 2: 流循环接检查器（react_model_call）

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go:186-192`（调用处）、`:320-431`（`callLLMStream`）
- Test: `internal/agentcore/single_agent/agents/react_model_call_test.go`（追加）

- [ ] **Step 1: 写失败测试**（追加到 `react_model_call_test.go`，复用文件里已有的 `fakeStreamClient` / `newFakeLLMModel`；测试文件需新增导入 `"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`）

```go
func TestCallLLMStream_检查器逐chunk调用(t *testing.T) {
	a := &ReActAgent{}
	client := &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("a"),
		llmschema.NewAssistantMessageChunk("b"),
	}}
	model := newFakeLLMModel(t, client)
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)

	seen := 0
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, _ *interfaces.AgentCallbackContext, chunk *llmschema.AssistantMessageChunk) error {
			seen++
			if chunk.Content.Text() == "" {
				t.Errorf("检查器收到的 chunk 内容为空")
			}
			return nil
		},
	})

	if _, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil); err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if seen != 2 {
		t.Fatalf("检查器应被调用 2 次，实际 %d", seen)
	}
}

func TestCallLLMStream_检查器报错先排空再返回(t *testing.T) {
	a := &ReActAgent{}
	ch := make(chan *llmschema.AssistantMessageChunk, 3)
	ch <- llmschema.NewAssistantMessageChunk("a")
	ch <- llmschema.NewAssistantMessageChunk("b")
	ch <- llmschema.NewAssistantMessageChunk("c")
	close(ch)
	client := &fakeStreamClient{chanOverride: ch}
	model := newFakeLLMModel(t, client)

	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	boom := errors.New("repeat detected")
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, _ *interfaces.AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error { return boom },
	})

	_, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("应透传检查器错误，实际 %v", err)
	}
	if rest := len(ch); rest != 0 {
		t.Fatalf("报错后必须排空 channel，剩余 %d 个 chunk", rest)
	}
}

func TestCallLLMStream_流式输出置可见标志(t *testing.T) {
	a := &ReActAgent{}
	model := newFakeLLMModel(t, &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("hello"),
	}})
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetModelVisibleOutputEmitted(true) // 模拟上一轮的残留

	var atChunk bool
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, c *interfaces.AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
			atChunk = c.ModelVisibleOutputEmitted()
			return nil
		},
	})

	if _, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil); err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if atChunk {
		t.Fatal("每次流式调用开始时必须把可见输出标志复位为 false")
	}
	if !cbc.ModelVisibleOutputEmitted() {
		t.Fatal("写出可见内容后标志应为 true")
	}
}
```

`fakeStreamClient` 需要新增字段 `chanOverride chan *llmschema.AssistantMessageChunk`（若已存在同名机制则复用），并在其 `Stream` 中优先返回它：

```go
func (f *fakeStreamClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	if f.chanOverride != nil {
		return f.chanOverride, nil
	}
	ch := make(chan *llmschema.AssistantMessageChunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestCallLLMStream' ./internal/agentcore/single_agent/agents/ -v
```
Expected: FAIL —— `too many arguments in call to a.callLLMStream`

- [ ] **Step 3: 实现**

① `railedModelCall` 调用处（`:189`）改为传 `cbc`：

```go
	if isStreaming {
		aiMsg, err = a.callLLMStream(ctx, cbc, llmModel, modelName, messages, contextTools, sess, extraKVPairs)
	} else {
		aiMsg, err = a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools, extraKVPairs)
	}
```

② `callLLMStream` 签名加 `cbc`（放在 `ctx` 之后，对齐 `_railed_model_call` 的第一个参数）：

```go
func (a *ReActAgent) callLLMStream(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []cschema.ToolInfoInterface,
	sess sessioninterfaces.SessionFacade,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
```

③ 循环内，在 `if chunk != nil && chunk.StreamError != nil {` 分支**之后**、`accumulated` 合并之前插入检查器调用：

```go
		// 流式分片检查器（对齐 Python: react_agent.py L1897-1905）：
		// 在写流之前跑，命中即中止——被判为重复的文本不应再发给用户。
		// 返回前必须排空 chunkCh，否则客户端 goroutine 卡在 send、连接不关。
		for _, inspector := range cbc.StreamChunkInspectors() {
			if inspector == nil {
				continue
			}
			if inspectErr := inspector(ctx, cbc, chunk); inspectErr != nil {
				drained := 0
				for range chunkCh {
					drained++
				}
				logger.Debug(logComponent).
					Str("event_type", "stream_chunk_inspector_aborted").
					Int("drained", drained).
					Err(inspectErr).
					Msg("stream chunk inspector aborted the stream")
				return nil, inspectErr
			}
		}
```

④ 在进入 `chunkCh` 循环之前复位标志（对齐 `react_agent.py:1878`）：

```go
	cbc.SetModelVisibleOutputEmitted(false)
```

⑤ 在写 `llm_reasoning` / `llm_output` 之前置 true（两处，均在 `if sess != nil {` 内对应分支开头）：

```go
			if chunk.ReasoningContent != "" {
				cbc.SetModelVisibleOutputEmitted(true)
				_ = sess.WriteStream(ctx, &stream.OutputSchema{ ... })
			}
			if chunk.Content.Text() != "" {
				cbc.SetModelVisibleOutputEmitted(true)
				_ = sess.WriteStream(ctx, &stream.OutputSchema{ ... })
			}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestCallLLMStream' ./internal/agentcore/single_agent/agents/ -v
```
Expected: PASS（含既有 3 个用例；既有调用点需同步补 `nil` 实参）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_model_call.go internal/agentcore/single_agent/agents/react_model_call_test.go
git commit -m "feat(agentcore): 流循环逐 chunk 调用检查器并维护可见输出标志"
```

---

## Task 3: 两个流超时配置字段（2.18 地基）

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/config.go:23-44`（结构体）、`:200-260`（选项与构造）
- Test: `internal/agentcore/foundation/llm/schema/config_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func TestModelClientConfig_流超时默认值与三态(t *testing.T) {
	desc := 300.0
	idle := 60.0

	base, err := NewModelClientConfig("OpenAI", "k", "http://localhost")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if base.StreamFirstChunkTimeout == nil || *base.StreamFirstChunkTimeout != desc {
		t.Fatalf("默认首帧超时应为 300，实际 %v", base.StreamFirstChunkTimeout)
	}
	if base.StreamIdleTimeout == nil || *base.StreamIdleTimeout != idle {
		t.Fatalf("默认空闲超时应为 60，实际 %v", base.StreamIdleTimeout)
	}

	disabled, err := NewModelClientConfig("OpenAI", "k", "http://localhost",
		WithStreamFirstChunkTimeout(nil), WithStreamIdleTimeout(nil))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if disabled.StreamFirstChunkTimeout != nil || disabled.StreamIdleTimeout != nil {
		t.Fatal("显式 nil 应表示禁用")
	}

	custom := 5.5
	over, err := NewModelClientConfig("OpenAI", "k", "http://localhost", WithStreamIdleTimeout(&custom))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if over.StreamIdleTimeout == nil || *over.StreamIdleTimeout != 5.5 {
		t.Fatalf("自定义空闲超时应为 5.5，实际 %v", over.StreamIdleTimeout)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestModelClientConfig_流超时' ./internal/agentcore/foundation/llm/schema/ -v
```
Expected: FAIL —— `unknown field StreamFirstChunkTimeout` / `undefined: WithStreamFirstChunkTimeout`

- [ ] **Step 3: 实现**

结构体加两字段（`Timeout` 之后）：

```go
	// StreamFirstChunkTimeout 首个携带模型输出的流式分片的最长等待（秒）。
	// nil 表示禁用；默认 300。README 语义：只有携带输出（content/reasoning/tool_call）的
	// 分片才算首帧，role-only/heartbeat/usage-only 帧不算。
	// Python: schema/config.py stream_first_chunk_timeout: Optional[float] = 300.0
	StreamFirstChunkTimeout *float64 `json:"stream_first_chunk_timeout,omitempty"`
	// StreamIdleTimeout 相邻两个携带输出分片之间的最长等待（秒）。nil 表示禁用；默认 60。
	// Python: schema/config.py stream_idle_timeout: Optional[float] = 60.0
	StreamIdleTimeout *float64 `json:"stream_idle_timeout,omitempty"`
```

选项函数（`WithTimeout` 之后）：

```go
// WithStreamFirstChunkTimeout 设置首帧流超时（秒）；nil 表示禁用。
func WithStreamFirstChunkTimeout(seconds *float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.StreamFirstChunkTimeout = seconds }
}

// WithStreamIdleTimeout 设置空闲流超时（秒）；nil 表示禁用。
func WithStreamIdleTimeout(seconds *float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.StreamIdleTimeout = seconds }
}
```

`NewModelClientConfig` 的默认值块加：

```go
	defaultFirstChunk := 300.0
	defaultIdle := 60.0
	cfg := &ModelClientConfig{
		...
		StreamFirstChunkTimeout: &defaultFirstChunk,
		StreamIdleTimeout:       &defaultIdle,
	}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestModelClientConfig' ./internal/agentcore/foundation/llm/schema/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/config.go internal/agentcore/foundation/llm/schema/config_test.go
git commit -m "feat(llm): ModelClientConfig 增加首帧/空闲流超时三态字段"
```

---

## Task 4: `CarriesOutputToken`

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message_chunk.go`
- Test: `internal/agentcore/foundation/llm/schema/message_chunk_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func TestAssistantMessageChunk_CarriesOutputToken(t *testing.T) {
	cases := []struct {
		name  string
		chunk *llmschema.AssistantMessageChunk // 同包测试可省略包名，写本包类型
		want  bool
	}{
		{"纯文本", llmschema.NewAssistantMessageChunk("hi"), true},
		{"仅推理", &llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{ReasoningContent: "think"}}, true},
		{"空白文本也算", llmschema.NewAssistantMessageChunk(" "), true},
		{"工具名片段", &llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{ToolCalls: []*llmschema.ToolCall{{Name: "read_file"}}}}, true},
		{"工具参数片段", &llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{ToolCalls: []*llmschema.ToolCall{{Arguments: "{\"pa"}}}}, true},
		{"空 chunk", &llmschema.AssistantMessageChunk{}, false},
		{"仅 role", &llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{DefaultMessage: llmschema.DefaultMessage{Role: llmschema.RoleTypeAssistant}}}, false},
		{"仅 usage", &llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{UsageMetadata: &llmschema.UsageMetadata{}}}, false},
	}
	for _, c := range cases {
		if got := c.chunk.CarriesOutputToken(); got != c.want {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}
```
（同包测试直接用 `AssistantMessageChunk` / `NewAssistantMessageChunk`，去掉 `llmschema.` 前缀。）

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestAssistantMessageChunk_CarriesOutputToken' ./internal/agentcore/foundation/llm/schema/ -v
```
Expected: FAIL —— `chunk.CarriesOutputToken undefined`

- [ ] **Step 3: 实现**（`message_chunk.go`，`AssistantMessageChunk` 之后）

```go
// CarriesOutputToken 判断该分片是否携带模型输出（而非流式框架帧）。
//
// 携带：content 或 reasoning_content 非空（空白也算，它是生成的 token），
// 或任一 tool_call 的 name/arguments 非空。
// 不携带：role-only、heartbeat、usage-only、finish-only。
// 首 token 延迟统计与空闲超时共用此定义，保证量的是同一件事。
//
// Python: message_chunk.py L242-256 carries_output_token()
func (c *AssistantMessageChunk) CarriesOutputToken() bool {
	if c == nil {
		return false
	}
	if c.Content.Text() != "" {
		return true
	}
	if c.ReasoningContent != "" {
		return true
	}
	for _, call := range c.ToolCalls {
		if call == nil {
			continue
		}
		if call.Name != "" || call.Arguments != "" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestAssistantMessageChunk_CarriesOutputToken' ./internal/agentcore/foundation/llm/schema/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/message_chunk.go internal/agentcore/foundation/llm/schema/message_chunk_test.go
git commit -m "feat(llm): AssistantMessageChunk 增加 CarriesOutputToken 判定"
```

---

## Task 5: `Model.Stream` 首帧/空闲超时

**Files:**
- Modify: `internal/agentcore/foundation/llm/model.go:252-344`
- Test: `internal/agentcore/foundation/llm/model_timeout_test.go`（新建）

- [ ] **Step 1: 写失败测试**（同包，直接用 `&Model{client: ...}`，参考 `model_test.go:220-262`）

```go
package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// slowStreamClient 按脚本在发送前 sleep，用于制造首帧/空闲超时。
type slowStreamClient struct {
	delays []time.Duration
	chunks []*llmschema.AssistantMessageChunk
}

func (s *slowStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}

func (s *slowStreamClient) Stream(ctx context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, len(s.chunks))
	go func() {
		defer close(ch)
		for i, c := range s.chunks {
			select {
			case <-time.After(s.delays[i]):
			case <-ctx.Done():
				return
			}
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func floatPtr(f float64) *float64 { return &f }

func newTimeoutModel(client model_clients.BaseModelClient, first, idle *float64) *Model {
	cfg := &llmschema.ModelClientConfig{
		ClientProvider:          "OpenAI",
		APIKey:                  "k",
		APIBase:                 "http://localhost",
		Timeout:                 60,
		StreamFirstChunkTimeout: first,
		StreamIdleTimeout:       idle,
	}
	return &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("m")),
		ClientConfig:      cfg,
		client:            client,
		callbackFramework: newTestCallbackFramework(),
	}
}

func TestModel_Stream_首帧超时(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{300 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("x")},
	}, floatPtr(0.05), floatPtr(5))

	ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatalf("Stream 不应立即报错: %v", err)
	}
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.StreamError != nil {
			streamErr = chunk.StreamError
		}
	}
	if streamErr == nil {
		t.Fatal("应收到超时错误")
	}
	msg := streamErr.Error()
	for _, want := range []string{"LLM stream timeout", "stream frame timeout", "stage=first_token", "token_chunk_count=0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误文案缺少 %q: %s", want, msg)
		}
	}
}

func TestModel_Stream_空闲超时(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{0, 400 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{
			llmschema.NewAssistantMessageChunk("a"),
			llmschema.NewAssistantMessageChunk("b"),
		},
	}, floatPtr(1), floatPtr(0.05))

	ch, _ := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.StreamError != nil {
			streamErr = chunk.StreamError
		}
	}
	if streamErr == nil || !strings.Contains(streamErr.Error(), "stage=idle_token") {
		t.Fatalf("期望 idle_token 超时，实际 %v", streamErr)
	}
}

func TestModel_Stream_框架帧不重置空闲预算(t *testing.T) {
	// 第一帧带输出 → 第二帧 role-only（不重置）→ 第三帧超出 idle → 超时
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{0, 30 * time.Millisecond, 60 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{
			llmschema.NewAssistantMessageChunk("a"),
			{AssistantMessage: llmschema.AssistantMessage{DefaultMessage: llmschema.DefaultMessage{Role: llmschema.RoleTypeAssistant}}},
			llmschema.NewAssistantMessageChunk("b"),
		},
	}, floatPtr(1), floatPtr(0.08))

	ch, _ := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.StreamError != nil {
			streamErr = chunk.StreamError
		}
	}
	if streamErr == nil {
		t.Fatal("框架帧不应重置空闲预算，应超时")
	}
}

func TestModel_Stream_禁用超时不报错(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{80 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("a")},
	}, nil, nil)

	ch, _ := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.StreamError != nil {
			streamErr = chunk.StreamError
		}
	}
	if streamErr != nil {
		t.Fatalf("禁用后不应超时: %v", streamErr)
	}
}
```

`newTestCallbackFramework()` 若不存在，用 `model_test.go` 的方式构造：`callback.NewCallbackFramework()`（该包已被既有测试导入）。

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestModel_Stream_首帧|TestModel_Stream_空闲|TestModel_Stream_框架帧|TestModel_Stream_禁用' ./internal/agentcore/foundation/llm/ -v
```
Expected: FAIL —— `应收到超时错误`

- [ ] **Step 3: 实现**

① `Stream` 里把底层调用改为派生 ctx（`:253`）：

```go
	// 帧超时需要主动断开底层流：派生可取消 ctx，超时即 cancel。
	streamCtx, cancelStream := context.WithCancel(ctx)
	chunkChan, err := m.client.Stream(streamCtx, messages, opts...)
	if err != nil {
		cancelStream()
		...（原错误分支不变）
	}
```
（`defer` 不能放在 `Stream` 函数体里——它在返回 channel 时就会执行。`cancelStream` 交给包装 goroutine 的 `defer`。）

② 包装 goroutine 开头加 `defer cancelStream()`，并把 `for chunk := range chunkChan {` 改成带预算的接收循环：

```go
	firstBudget := m.ClientConfig.StreamFirstChunkTimeout
	idleBudget := m.ClientConfig.StreamIdleTimeout
	chunkCount := 0
	tokenChunkCount := 0
	waitedSinceProgress := 0.0
	startedAt := time.Now()
	accumulated := (*llmschema.AssistantMessageChunk)(nil)

	for {
		budget := idleBudget
		stage := "idle_token"
		elapsedLabel := "idle_elapsed"
		if tokenChunkCount == 0 {
			budget, stage, elapsedLabel = firstBudget, "first_token", "first_token_elapsed"
		}

		waitStartedAt := time.Now()
		var chunk *llmschema.AssistantMessageChunk
		var ok bool
		if budget == nil {
			chunk, ok = <-chunkChan
		} else {
			remaining := *budget - waitedSinceProgress
			if remaining < 0 {
				remaining = 0
			}
			timer := time.NewTimer(time.Duration(remaining * float64(time.Second)))
			select {
			case chunk, ok = <-chunkChan:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
				now := time.Now()
				totalElapsed := now.Sub(startedAt).Seconds()
				waitElapsed := waitedSinceProgress + now.Sub(waitStartedAt).Seconds()
				if ctx.Err() != nil {
					// 父 ctx 取消优先：交给下面的 ctx.Done 分支收尾，不构造超时错误
					for range chunkChan {
					}
					return
				}
				cancelStream()
				for range chunkChan {
				}
				timeoutErr := exception.BuildError(exception.StatusModelCallFailed,
					exception.WithMsg(fmt.Sprintf(
						"LLM stream timeout: stream frame timeout: stage=%s, timeout=%ss, chunk_count=%d, token_chunk_count=%d, %s=%.2fs, total_elapsed=%.2fs, model=%s",
						stage, utils.PyFloatStr(*budget), chunkCount, tokenChunkCount, elapsedLabel, waitElapsed, totalElapsed, modelName)))
				_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
					Event:         callback.LLMCallError,
					ModelName:     modelName,
					ModelProvider: m.ClientConfig.ClientProvider,
					IsStream:      true,
					Error:         timeoutErr,
					Extra:         map[string]any{"model_config": m.ModelConfig, "model_client_config": m.ClientConfig},
				})
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("stage", stage).
					Float64("timeout", *budget).
					Int("chunk_count", chunkCount).
					Int("token_chunk_count", tokenChunkCount).
					Msg("LLM stream timeout.")
				select {
				case out <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(timeoutErr)):
				case <-ctx.Done():
				}
				return
			}
		}
		if !ok {
			break
		}

		chunkCount++
		waitedSinceProgress += time.Since(waitStartedAt).Seconds()
		if chunk == nil || chunk.CarriesOutputToken() {
			tokenChunkCount++
			waitedSinceProgress = 0
		}
		if chunk != nil {
			if accumulated == nil {
				accumulated = chunk
			} else {
				accumulated = accumulated.Merge(chunk)
			}
		}

		// ③④ 原样保留 model.go:292-322 的 transform_io 输出变换与 emit_after 触发代码
		// （逐字搬进本循环体，不改字段与顺序）
		select {
		case out <- chunk:
		case <-ctx.Done():
			...（既有排空逻辑）
			return
		}
	}
```

注意：`accumulated` 是本函数内新变量，与上层 `callLLMStream` 的累积无关（`Model.Stream` 只用它在 `LLM_STREAM_COMPLETED` 场景；本仓当前未使用累积值，可只保留赋值不改返回值）。

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestModel_Stream' ./internal/agentcore/foundation/llm/ -v
```
Expected: PASS（含既有 `TestModel_Stream_回调事件`；若既有用例因默认超时变化受影响，说明脚本过慢，改为显式 `nil` 禁用）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model.go internal/agentcore/foundation/llm/model_timeout_test.go
git commit -m "feat(llm): Model.Stream 首帧与空闲帧超时（2.18）"
```

---

## Task 6: HTTP 错误头部落 params

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go:552-580`
- Test: `internal/agentcore/foundation/llm/model_clients/openai/client_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func TestHandleHTTPError_保留重试相关头部(t *testing.T) {
	body := `{"error":{"message":"rate limited","code":"rate_limit","type":"rate_limit_error"}}`
	resp := &http.Response{
		StatusCode: 429,
		Header: http.Header{
			"Retry-After":   []string{"3"},
			"X-Should-Retry": []string{"false"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
	c := &OpenAIModelClient{}
	err := c.HandleHTTPError(resp)
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("应返回 BaseError，实际 %T", err)
	}
	params := baseErr.Params()
	if got := params["http_status"]; got != 429 {
		t.Fatalf("http_status 期望 429，实际 %v", got)
	}
	if got := params["retry_after"]; got != "3" {
		t.Fatalf("retry_after 期望 \"3\"，实际 %v", got)
	}
	if got := params["x_should_retry"]; got != "false" {
		t.Fatalf("x_should_retry 期望 \"false\"，实际 %v", got)
	}
}

func TestHandleHTTPError_非标准体也保留头部(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Header:     http.Header{"Retry-After-Ms": []string{"1500"}},
		Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
	}
	err := (&OpenAIModelClient{}).HandleHTTPError(resp)
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("应返回 BaseError，实际 %T", err)
	}
	params := baseErr.Params()
	if got := params["http_status"]; got != 503 {
		t.Fatalf("http_status 期望 503，实际 %v", got)
	}
	if got := params["retry_after_ms"]; got != "1500" {
		t.Fatalf("retry_after_ms 期望 \"1500\"，实际 %v", got)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestHandleHTTPError' ./internal/agentcore/foundation/llm/model_clients/openai/ -v
```
Expected: FAIL —— `retry_after` 为 nil

- [ ] **Step 3: 实现**

两个分支统一构造 params（保留原有键，新增三个）：

```go
// httpErrorParams 汇总 HTTP 错误的结构化上下文，供上层做瞬时失败分类。
//
// 除状态码与 provider 错误码外，还保留重试决策相关头部：
// retry_after / retry_after_ms（服务端建议的退避时长）、x_should_retry。
// Python: 异常对象的 headers/response.headers 由 rail 侧读取；Go 侧扁平进 params。
func httpErrorParams(resp *http.Response) map[string]any {
	params := map[string]any{"http_status": resp.StatusCode}
	add := func(key, header string) {
		if v := strings.TrimSpace(resp.Header.Get(header)); v != "" {
			params[key] = v
		}
	}
	add("retry_after", "Retry-After")
	add("retry_after_ms", "Retry-After-Ms")
	add("x_should_retry", "X-Should-Retry")
	return params
}
```
`HandleHTTPError` 两个 `NewBaseError` 调用都改为 `exception.WithParams(params)`，其中成功解析分支再补 `provider_code`/`provider_type`：

```go
	params := httpErrorParams(resp)
	params["provider_code"] = errResp.Error.Code
	params["provider_type"] = errResp.Error.Type
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestHandleHTTPError' ./internal/agentcore/foundation/llm/model_clients/openai/ -v
```
Expected: PASS（既有 3 个用例的 params 断言需同步补新键）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/client.go internal/agentcore/foundation/llm/model_clients/openai/client_test.go
git commit -m "feat(llm): HTTP 错误保留 Retry-After 与 x-should-retry 头部"
```

---

## Task 7: rail 骨架 + 配置校验

**Files:**
- Create: `internal/agentcore/harness/rails/model_anomaly.go`
- Test: `internal/agentcore/harness/rails/model_anomaly_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package rails

import (
	"strings"
	"testing"
)

func TestNewModelAnomalyDetectionRail_默认值(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if r.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", r.Priority())
	}
	if r.maxRetries != 2 || r.repeatMinPatternChars != 2 || r.repeatMaxPatternChars != 64 ||
		r.repeatMinCount != 6 || r.repeatMinTotalChars != 160 || r.repeatWindowChars != 1024 ||
		r.singleCharRepeatCount != 100 {
		t.Fatalf("默认参数与上游不一致: %+v", r)
	}
	if r.transientMaxRetries != 3 || r.transientBaseDelay != 2.0 {
		t.Fatalf("瞬时重试默认值与上游不一致: %d / %v", r.transientMaxRetries, r.transientBaseDelay)
	}
	if len(r.backoffSeconds) != 3 || r.backoffSeconds[0] != 0.5 || r.backoffSeconds[2] != 2.0 {
		t.Fatalf("退避表默认值应为 0.5/1/2，实际 %v", r.backoffSeconds)
	}
	if r.toolLoopCompact.Enabled {
		t.Fatal("工具循环压缩默认必须关闭（对齐上游 enabled=False）")
	}
	if r.toolLoopCompact.ConsecutiveThreshold != 4 || r.toolLoopCompact.BailoutThreshold != 3 {
		t.Fatalf("压缩阈值默认值应为 4/3，实际 %d/%d",
			r.toolLoopCompact.ConsecutiveThreshold, r.toolLoopCompact.BailoutThreshold)
	}
}

func TestNewModelAnomalyDetectionRail_十条校验(t *testing.T) {
	cases := []struct {
		name string
		opt  ModelAnomalyOption
		want string
	}{
		{"max_retries 为负", WithMaxRetries(-1), "max_retries must be >= 0"},
		{"transient_max_retries 为负", WithTransientMaxRetries(-1), "transient_max_retries must be >= 0"},
		{"base_delay 为负", WithTransientBaseDelaySeconds(-0.1), "transient_base_delay_seconds must be >= 0"},
		{"退避表含负数", WithBackoffSeconds([]float64{0.5, -1}), "backoff_seconds entries must be >= 0"},
		{"重复片段下限为 0", WithRepeatMinPatternChars(0), "repeat_min_pattern_chars must be >= 1"},
		{"上限小于下限", WithRepeatParams(8, 4, 6, 160, 1024), "repeat_max_pattern_chars must be >= repeat_min_pattern_chars"},
		{"重复次数为 1", WithRepeatMinCount(1), "repeat_min_count must be >= 2"},
		{"最小总长为 0", WithRepeatMinTotalChars(0), "repeat_min_total_chars must be >= 1"},
		{"单字符阈值为 1", WithSingleCharRepeatCount(1), "single_char_repeat_count must be >= 2"},
		{"窗口过小", WithRepeatWindowChars(100), "repeat_window_chars is too small"},
	}
	for _, c := range cases {
		_, err := NewModelAnomalyDetectionRail(c.opt)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 期望错误含 %q，实际 %v", c.name, c.want, err)
		}
	}
}

func TestNewModelAnomalyDetectionRail_退避表越界取末项(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail(WithBackoffSeconds([]float64{0.5, 1.0}))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := r.backoffDelay(0); got != 0.5 {
		t.Fatalf("第 0 次退避应为 0.5，实际 %v", got)
	}
	if got := r.backoffDelay(5); got != 1.0 {
		t.Fatalf("越界应取末项 1.0，实际 %v", got)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestNewModelAnomalyDetectionRail' ./internal/agentcore/harness/rails/ -v
```
Expected: FAIL —— `undefined: NewModelAnomalyDetectionRail`

- [ ] **Step 3: 实现骨架**

```go
package rails

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolLoopCompactConfig 连续相同工具调用（名称+入参）的压缩配置。
//
// 返回值不参与匹配；折叠时只把最新一轮的调用与结果写进警告文本。
// Python: model_anomaly_detection_rail.py ToolLoopCompactConfig
type ToolLoopCompactConfig struct {
	// Enabled 是否启用压缩，默认 false（上游默认关闭）
	Enabled bool
	// ConsecutiveThreshold 连续多少轮相同即触发压缩，默认 4（>=2）
	ConsecutiveThreshold int
	// BailoutThreshold 第几次命中时兜底中止，默认 3；0 表示不中止
	BailoutThreshold int
}

// transientDecision 一次瞬时失败的分类结论。
type transientDecision struct {
	retryable         bool
	statusCode        *int
	retryAfterSeconds *float64
}

// toolFingerprintItem 单次工具调用的匹配指纹（名称 + 规范化入参）。
type toolFingerprintItem struct {
	name string
	args string
}

// toolRound 一轮完整的工具调用区间 [start, end)。
type toolRound struct {
	start       int
	end         int
	fingerprint []toolFingerprintItem
}

// anomalyState 单次模型调用的重复检测尾窗（两个字段各自独立）。
type anomalyState struct {
	reasoningContent string
	content          string
}

// retryCounters 单个会话的三类重试计数。
type retryCounters struct {
	repeat        int
	streamTimeout int
	transient     int
}

// ModelAnomalyDetectionRail 模型异常检测 Rail。
//
// 处理四类故障：流式退化重复输出、流帧超时、瞬时 provider 失败、连续相同工具调用。
// 重试复用 RailExecutor 的整调用重试（on_model_exception → RequestRetry）。
//
// Python: model_anomaly_detection_rail.py ModelAnomalyDetectionRail（priority 70）
type ModelAnomalyDetectionRail struct {
	DeepAgentRail

	maxRetries            int
	repeatMinPatternChars int
	repeatMaxPatternChars int
	repeatMinCount        int
	repeatMinTotalChars   int
	repeatWindowChars     int
	singleCharRepeatCount int
	backoffSeconds        []float64
	transientMaxRetries   int
	transientBaseDelay    float64
	toolLoopCompact       ToolLoopCompactConfig

	// mu 保护两张会话表：同一 rail 实例会被同一 channel 下的多个会话共享。
	mu            sync.RWMutex
	counters      map[string]*retryCounters
	compactCounts map[string]int
}

// ModelAnomalyOption Rail 构造选项。
type ModelAnomalyOption func(*ModelAnomalyDetectionRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// modelAnomalyRailPriority 与上游 priority=70 一致
	modelAnomalyRailPriority = 70
	// modelAnomalyStateKey 重复检测尾窗的 extra 键
	modelAnomalyStateKey = "_model_anomaly_detection_state"
	// repeatErrorMarker 重复输出错误消息前缀，也是识别"是否重复异常"的判据
	repeatErrorMarker = "LLM repeated stream output detected"
	// maxRetryAfterSeconds 服务端建议退避超过该值则放弃重试
	maxRetryAfterSeconds = 60.0
	// defaultTransientMaxRetries 瞬时失败默认重试次数
	defaultTransientMaxRetries = 3
	// defaultTransientBaseDelay 瞬时失败指数退避基数（秒）
	defaultTransientBaseDelay = 2.0
	// toolLoopBailoutMessage 兜底中止文案（与上游逐字一致）
	toolLoopBailoutMessage = "Detected that the model fell into repeated tool calls; this task has been automatically aborted."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// streamTimeoutMarkers 流帧超时错误标记（上游 Model.stream 产出的文案）
var streamTimeoutMarkers = []string{"LLM stream timeout", "stream frame timeout"}

// defaultBackoffSeconds 重复输出/流超时的退避表（秒）
var defaultBackoffSeconds = []float64{0.5, 1.0, 2.0}

// retryableStatusCodes 可重试的 HTTP 状态码
var retryableStatusCodes = map[int]bool{408: true, 409: true, 429: true}

// nonRetryableTextMarkers 命中即不重试的文本标记
var nonRetryableTextMarkers = []string{
	"insufficient_quota", "out of budget", "quota exceeded", "billing",
	"invalid api key", "context length", "maximum context",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelAnomalyDetectionRail 创建 Rail 并校验参数。
//
// Python: ModelAnomalyDetectionRail.__init__（10 条 ValueError 校验 → 此处返回 error）
func NewModelAnomalyDetectionRail(opts ...ModelAnomalyOption) (*ModelAnomalyDetectionRail, error) {
	r := &ModelAnomalyDetectionRail{
		DeepAgentRail:         *NewDeepAgentRail(),
		maxRetries:            2,
		repeatMinPatternChars: 2,
		repeatMaxPatternChars: 64,
		repeatMinCount:        6,
		repeatMinTotalChars:   160,
		repeatWindowChars:     1024,
		singleCharRepeatCount: 100,
		backoffSeconds:        append([]float64(nil), defaultBackoffSeconds...),
		transientMaxRetries:   defaultTransientMaxRetries,
		transientBaseDelay:    defaultTransientBaseDelay,
		toolLoopCompact:       ToolLoopCompactConfig{ConsecutiveThreshold: 4, BailoutThreshold: 3},
		counters:              make(map[string]*retryCounters),
		compactCounts:         make(map[string]int),
	}
	for _, opt := range opts {
		opt(r)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	r.WithPriority(modelAnomalyRailPriority)
	return r, nil
}

// WithMaxRetries 设置重复输出/流超时的最大重试次数。
func WithMaxRetries(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.maxRetries = n }
}

// WithRepeatParams 设置重复片段检测的上下限、次数与窗口。
func WithRepeatParams(minPattern, maxPattern, minCount, minTotal, window int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) {
		r.repeatMinPatternChars = minPattern
		r.repeatMaxPatternChars = maxPattern
		r.repeatMinCount = minCount
		r.repeatMinTotalChars = minTotal
		r.repeatWindowChars = window
	}
}

// WithRepeatMinPatternChars 设置重复片段最短长度。
func WithRepeatMinPatternChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinPatternChars = n }
}

// WithRepeatMaxPatternChars 设置重复片段最长长度。
func WithRepeatMaxPatternChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMaxPatternChars = n }
}

// WithRepeatMinCount 设置判定循环所需的最少连续重复次数。
func WithRepeatMinCount(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinCount = n }
}

// WithRepeatMinTotalChars 设置多字符重复的最小总长度。
func WithRepeatMinTotalChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinTotalChars = n }
}

// WithRepeatWindowChars 设置重复检测滑动窗口大小。
func WithRepeatWindowChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatWindowChars = n }
}

// WithSingleCharRepeatCount 设置单字符连续重复阈值。
func WithSingleCharRepeatCount(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.singleCharRepeatCount = n }
}

// WithBackoffSeconds 设置重复输出重试的退避表（越界取末项）。
func WithBackoffSeconds(seconds []float64) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) {
		r.backoffSeconds = append([]float64(nil), seconds...)
	}
}

// WithTransientMaxRetries 设置瞬时失败的最大重试次数。
func WithTransientMaxRetries(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.transientMaxRetries = n }
}

// WithTransientBaseDelaySeconds 设置瞬时失败指数退避基数（秒）。
func WithTransientBaseDelaySeconds(seconds float64) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.transientBaseDelay = seconds }
}

// WithToolLoopCompact 设置工具循环压缩配置。
func WithToolLoopCompact(cfg ToolLoopCompactConfig) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.toolLoopCompact = cfg }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validate 对齐上游 __init__ 的 10 条参数校验。
func (r *ModelAnomalyDetectionRail) validate() error {
	if r.maxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0")
	}
	if r.transientMaxRetries < 0 {
		return fmt.Errorf("transient_max_retries must be >= 0")
	}
	if r.transientBaseDelay < 0 {
		return fmt.Errorf("transient_base_delay_seconds must be >= 0")
	}
	for _, d := range r.backoffSeconds {
		if d < 0 {
			return fmt.Errorf("backoff_seconds entries must be >= 0")
		}
	}
	if r.repeatMinPatternChars < 1 {
		return fmt.Errorf("repeat_min_pattern_chars must be >= 1")
	}
	if r.repeatMaxPatternChars < r.repeatMinPatternChars {
		return fmt.Errorf("repeat_max_pattern_chars must be >= repeat_min_pattern_chars")
	}
	if r.repeatMinCount < 2 {
		return fmt.Errorf("repeat_min_count must be >= 2")
	}
	if r.repeatMinTotalChars < 1 {
		return fmt.Errorf("repeat_min_total_chars must be >= 1")
	}
	if r.singleCharRepeatCount < 2 {
		return fmt.Errorf("single_char_repeat_count must be >= 2")
	}
	minWindow := r.repeatMaxPatternChars * r.repeatMinCount
	if r.repeatMinTotalChars > minWindow {
		minWindow = r.repeatMinTotalChars
	}
	if r.singleCharRepeatCount > minWindow {
		minWindow = r.singleCharRepeatCount
	}
	if r.repeatWindowChars < minWindow {
		return fmt.Errorf("repeat_window_chars is too small for the configured repetition thresholds")
	}
	if r.toolLoopCompact.ConsecutiveThreshold < 2 {
		r.toolLoopCompact.ConsecutiveThreshold = 2
	}
	if r.toolLoopCompact.BailoutThreshold < 0 {
		r.toolLoopCompact.BailoutThreshold = 0
	}
	return nil
}

// backoffDelay 返回第 retryIndex 次重试前的等待秒数，越界取末项。
//
// Python: backoff_delay
func (r *ModelAnomalyDetectionRail) backoffDelay(retryIndex int) float64 {
	if len(r.backoffSeconds) == 0 {
		return 0
	}
	if retryIndex < 0 {
		retryIndex = 0
	}
	if retryIndex >= len(r.backoffSeconds) {
		retryIndex = len(r.backoffSeconds) - 1
	}
	return r.backoffSeconds[retryIndex]
}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestNewModelAnomalyDetectionRail' ./internal/agentcore/harness/rails/ -v
```
Expected: PASS（3 个 Test）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/model_anomaly.go internal/agentcore/harness/rails/model_anomaly_test.go
git commit -m "feat(harness): ModelAnomalyDetectionRail 骨架与参数校验"
```

---

## Task 8: 重复输出检测

**Files:**
- Modify: `internal/agentcore/harness/rails/model_anomaly.go`（追加方法与非导出函数）
- Test: `internal/agentcore/harness/rails/model_anomaly_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func newTestCBC() *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
}

func feedChunk(t *testing.T, r *ModelAnomalyDetectionRail, cbc *agentinterfaces.AgentCallbackContext, reasoning, content string) error {
	t.Helper()
	return r.InspectStreamChunk(context.Background(), cbc,
		&llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{ReasoningContent: reasoning, Content: llmschema.NewTextContent(content)}})
}

func TestInspectStreamChunk_多字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc) // 建尾窗

	var err error
	for i := 0; i < 54 && err == nil; i++ {
		err = feedChunk(t, r, cbc, "abc", "")
	}
	if err == nil {
		t.Fatal("54 次 abc 应触发重复检测")
	}
	msg := err.Error()
	for _, want := range []string{repeatErrorMarker, "field=reasoning_content", "unit='abc'", "repeat_count="} {
		if !strings.Contains(msg, want) {
			t.Errorf("文案缺少 %q: %s", want, msg)
		}
	}
}

func TestInspectStreamChunk_短重复不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc)

	for i := 0; i < 5; i++ { // 5*3=15 字符 < 160
		if err := feedChunk(t, r, cbc, "", "abc"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
}

func TestInspectStreamChunk_单字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc)

	var err error
	for i := 0; i < 100 && err == nil; i++ {
		err = feedChunk(t, r, cbc, "", "-")
	}
	if err == nil {
		t.Fatal("100 个 '-' 应触发单字符重复检测")
	}
	if !strings.Contains(err.Error(), "unit='-'") {
		t.Fatalf("文案应含 unit='-'，实际 %s", err.Error())
	}
}

func TestInspectStreamChunk_空白后缀不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc)

	for i := 0; i < 200; i++ {
		if err := feedChunk(t, r, cbc, "", " "); err != nil {
			t.Fatalf("空白重复不应触发: %v", err)
		}
	}
}

func TestInspectStreamChunk_两字段独立窗口(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc)

	// content 攒到接近阈值但 reasoning 独立计数，不应被 content 影响
	for i := 0; i < 3; i++ {
		if err := feedChunk(t, r, cbc, "xy", "zz"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
	state, _ := cbc.GetExtra(modelAnomalyStateKey)
	st := state.(*anomalyState)
	if st.content != "zzzzzz" || st.reasoningContent != "xyxyxy" {
		t.Fatalf("两字段应各自累计，实际 content=%q reasoning=%q", st.content, st.reasoningContent)
	}
}

func TestInspectStreamChunk_尾窗滚动仍能检出(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeModelCall(context.Background(), cbc)

	// 先灌 2000 字符噪声，窗口只保留最后 1024
	if err := feedChunk(t, r, cbc, "", strings.Repeat("z", 2000)); err != nil {
		t.Fatalf("噪声不应触发: %v", err)
	}
	var err error
	for i := 0; i < 60 && err == nil; i++ {
		err = feedChunk(t, r, cbc, "", "abc")
	}
	if err == nil {
		t.Fatal("滚动窗口内仍应检出")
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestInspectStreamChunk' ./internal/agentcore/harness/rails/ -v
```
Expected: FAIL —— `r.InspectStreamChunk undefined`

- [ ] **Step 3: 实现**

```go
// BeforeModelCall 压缩工具循环、重置重复检测尾窗并安装流式分片检查器。
//
// Python: before_model_call L427-443
func (r *ModelAnomalyDetectionRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if err := r.maybeCompactOrBailoutToolLoop(cbc); err != nil {
		return err
	}
	cbc.SetExtra(modelAnomalyStateKey, &anomalyState{})

	kept := make([]agentinterfaces.StreamChunkInspectorFunc, 0, 4)
	for _, fn := range cbc.StreamChunkInspectors() {
		if fn == nil || funcPtrEqual(fn, r.InspectStreamChunk) {
			continue
		}
		kept = append(kept, fn)
	}
	kept = append(kept, r.InspectStreamChunk)
	cbc.SetStreamChunkInspectors(kept)
	return nil
}

// InspectStreamChunk 逐分片检查 reasoning_content / content 是否退化重复。
//
// Python: inspect_stream_chunk L445-452 + _append_and_check L483-497
func (r *ModelAnomalyDetectionRail) InspectStreamChunk(_ context.Context, cbc *agentinterfaces.AgentCallbackContext, chunk *llmschema.AssistantMessageChunk) error {
	if chunk == nil {
		return nil
	}
	st := r.anomalyState(cbc)
	if chunk.ReasoningContent != "" {
		if err := r.appendAndCheck(st, "reasoning_content", chunk.ReasoningContent); err != nil {
			return err
		}
	}
	if text := chunk.Content.Text(); text != "" {
		if err := r.appendAndCheck(st, "content", text); err != nil {
			return err
		}
	}
	return nil
}

// anomalyState 取（或创建）本次模型调用的尾窗状态。
func (r *ModelAnomalyDetectionRail) anomalyState(cbc *agentinterfaces.AgentCallbackContext) *anomalyState {
	if v, ok := cbc.GetExtra(modelAnomalyStateKey); ok {
		if st, ok := v.(*anomalyState); ok && st != nil {
			return st
		}
	}
	st := &anomalyState{}
	cbc.SetExtra(modelAnomalyStateKey, st)
	return st
}

// appendAndCheck 追加文本、截断尾窗并做重复检测。
func (r *ModelAnomalyDetectionRail) appendAndCheck(st *anomalyState, field, text string) error {
	var tail string
	if field == "reasoning_content" {
		tail = lastRunes(st.reasoningContent+text, r.repeatWindowChars)
		st.reasoningContent = tail
	} else {
		tail = lastRunes(st.content+text, r.repeatWindowChars)
		st.content = tail
	}

	unit, count, ok := r.detectRepeatedSuffix(tail)
	if !ok {
		return nil
	}
	return exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("%s: field=%s, unit=%s, repeat_count=%d",
			repeatErrorMarker, field, pyReprString(formatUnit(unit)), count)))
}

// detectRepeatedSuffix 检测文本尾部的退化重复（先单字符，再多字符）。
//
// Python: _detect_repeated_suffix L499-523
func (r *ModelAnomalyDetectionRail) detectRepeatedSuffix(text string) (string, int, bool) {
	runes := []rune(text)
	if len(runes) == 0 {
		return "", 0, false
	}
	if unit, count, ok := r.detectSingleCharSuffix(runes); ok {
		return unit, count, true
	}
	maxUnitLen := r.repeatMaxPatternChars
	if limit := len(runes) / r.repeatMinCount; limit < maxUnitLen {
		maxUnitLen = limit
	}
	for unitLen := r.repeatMinPatternChars; unitLen <= maxUnitLen; unitLen++ {
		unit := string(runes[len(runes)-unitLen:])
		if strings.TrimSpace(unit) == "" || isSingleCharPattern(unit) {
			continue
		}
		required := (r.repeatMinTotalChars + unitLen - 1) / unitLen
		if required < r.repeatMinCount {
			required = r.repeatMinCount
		}
		count := 1
		for pos := len(runes) - unitLen*2; pos >= 0; pos -= unitLen {
			if string(runes[pos:pos+unitLen]) != unit {
				break
			}
			count++
		}
		if count >= required {
			return unit, count, true
		}
	}
	return "", 0, false
}

// detectSingleCharSuffix 检测末尾同字符连续重复。
//
// Python: _detect_single_char_suffix L525-539
func (r *ModelAnomalyDetectionRail) detectSingleCharSuffix(runes []rune) (string, int, bool) {
	last := runes[len(runes)-1]
	if strings.TrimSpace(string(last)) == "" {
		return "", 0, false
	}
	count := 1
	for i := len(runes) - 2; i >= 0; i-- {
		if runes[i] != last {
			break
		}
		count++
		if count >= r.singleCharRepeatCount {
			return string(last), count, true
		}
	}
	return "", 0, false
}

// lastRunes 取末尾 n 个字符（按 rune，与上游按字符切片一致）。
func lastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// isSingleCharPattern 判断片段是否由同一个字符组成。
func isSingleCharPattern(unit string) bool {
	runes := []rune(unit)
	for _, ch := range runes[1:] {
		if ch != runes[0] {
			return false
		}
	}
	return true
}

// formatUnit 转义换行并截断到 limit 个字符。
//
// Python: _format_unit L545-550
func formatUnit(unit string) string {
	formatted := strings.ReplaceAll(strings.ReplaceAll(unit, "\r", `\r`), "\n", `\n`)
	runes := []rune(formatted)
	if len(runes) <= 80 {
		return formatted
	}
	return string(runes[:80]) + "..."
}

// pyReprString 按 Python str.__repr__ 的常用分支渲染（单引号 + 转义）。
//
// 用于错误文案与上游逐字一致（上游用 {unit!r}）。非可打印控制字符不展开为 \xNN，
// 因为入参已由 formatUnit 处理过 \r/\n。
func pyReprString(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, ch := range s {
		switch ch {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// funcPtrEqual 比较两个检查器的代码指针。
//
// Go 函数值不可 == 比较，只能取代码指针；同一方法的不同 receiver 代码指针相同，
// 因此"同一 rail 双实例"会被判相等（可接受：双实例已被 factory 去重禁止）。
// 先例：agentbuilder/utils/progress.go funcPtrEqual、sys_operation/registry.go:73
func funcPtrEqual(a, b agentinterfaces.StreamChunkInspectorFunc) bool {
	if a == nil || b == nil {
		return false
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}
```
（新增导入：`reflect`。）

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestInspectStreamChunk' ./internal/agentcore/harness/rails/ -v
```
Expected: PASS（6 个 Test）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/model_anomaly.go internal/agentcore/harness/rails/model_anomaly_test.go
git commit -m "feat(harness): 重复输出检测（尾窗+单字符+多字符）"
```

---

## Task 9: 瞬时失败分类与异常链

**Files:**
- Modify: `internal/agentcore/harness/rails/model_anomaly.go`
- Test: `internal/agentcore/harness/rails/model_anomaly_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func httpErr(status int, headers map[string]any, msg string) error {
	params := map[string]any{"http_status": status}
	for k, v := range headers {
		params[k] = v
	}
	return exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(msg), exception.WithParams(params))
}

func TestClassifyTransientFailure(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		wantRetry     bool
		wantStatus    int
		wantRetryAfter float64
	}{
		{"429 可重试", httpErr(429, nil, "too many requests"), true, 429, 0},
		{"408 可重试", httpErr(408, nil, "timeout"), true, 408, 0},
		{"409 可重试", httpErr(409, nil, "conflict"), true, 409, 0},
		{"500 可重试", httpErr(500, nil, "boom"), true, 500, 0},
		{"503 可重试", httpErr(503, nil, "unavailable"), true, 503, 0},
		{"400 不重试", httpErr(400, nil, "bad request"), false, 400, 0},
		{"401 不重试", httpErr(401, nil, "unauthorized"), false, 401, 0},
		{"无状态码（连接断）可重试", errors.New("connection reset by peer"), true, 0, 0},
		{"配额文本不重试", errors.New("Error: insufficient_quota"), false, 0, 0},
		{"上下文过长不重试", errors.New("maximum context length exceeded"), false, 0, 0},
		{"x-should-retry false 挡住 429", httpErr(429, map[string]any{"x_should_retry": "false"}, "429"), false, 429, 0},
		{"x-should-retry true 放行 400", httpErr(400, map[string]any{"x_should_retry": "true"}, "400"), true, 400, 0},
		{"retry-after 数字", httpErr(429, map[string]any{"retry_after": "3"}, "429"), true, 429, 3},
		{"retry-after-ms", httpErr(429, map[string]any{"retry_after_ms": "1500"}, "429"), true, 429, 1.5},
		{"retry-after 超上限不重试", httpErr(429, map[string]any{"retry_after": "120"}, "429"), false, 429, 120},
	}
	for _, c := range cases {
		d := classifyTransientFailure(c.err)
		if d.retryable != c.wantRetry {
			t.Errorf("%s: retryable 期望 %v，实际 %v", c.name, c.wantRetry, d.retryable)
		}
		if c.wantStatus != 0 && (d.statusCode == nil || *d.statusCode != c.wantStatus) {
			t.Errorf("%s: status 期望 %d，实际 %v", c.name, c.wantStatus, d.statusCode)
		}
		if c.wantRetryAfter != 0 {
			if d.retryAfterSeconds == nil || *d.retryAfterSeconds != c.wantRetryAfter {
				t.Errorf("%s: retryAfter 期望 %v，实际 %v", c.name, c.wantRetryAfter, d.retryAfterSeconds)
			}
		}
	}
}

func TestClassifyTransientFailure_客户端决策优先(t *testing.T) {
	// details.should_retry=false 覆盖"无状态码即重试"的默认
	err := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("connection reset"),
		exception.WithDetails(map[string]any{"should_retry": false}))
	if d := classifyTransientFailure(err); d.retryable {
		t.Fatal("details.should_retry=false 应优先")
	}

	err2 := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("boom"),
		exception.WithDetails(map[string]any{"should_retry": true, "retry_after_seconds": 1.5}))
	d := classifyTransientFailure(err2)
	if !d.retryable || d.retryAfterSeconds == nil || *d.retryAfterSeconds != 1.5 {
		t.Fatalf("details.should_retry=true 应被采纳，实际 %+v", d)
	}
}

func TestParseRetryAfterSeconds_HTTP日期(t *testing.T) {
	when := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfterSeconds(map[string]string{"retry-after": when})
	if got == nil || *got < 3 || *got > 6 {
		t.Fatalf("HTTP-date 应解析为约 5s，实际 %v", got)
	}
}

func TestExceptionChain_沿Cause展开(t *testing.T) {
	inner := errors.New("inner")
	mid := exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg("mid"), exception.WithCause(inner))
	outer := exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg("outer"), exception.WithCause(mid))
	chain := exceptionChain(outer)
	if len(chain) != 3 {
		t.Fatalf("异常链应为 3 层，实际 %d", len(chain))
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestClassifyTransientFailure|TestParseRetryAfterSeconds|TestExceptionChain' ./internal/agentcore/harness/rails/ -v
```
Expected: FAIL —— `undefined: classifyTransientFailure`

- [ ] **Step 3: 实现**

```go
// classifyTransientFailure 判定一次 provider 失败是否值得整调用重试。
//
// 优先级：client 自带的 details.should_retry > 不可重试文本 > x-should-retry:false
// > Retry-After 超上限 > x-should-retry:true > 无状态码即重试 > 408/409/429/5xx > 其余不重试。
//
// Python: _classify_transient_failure L195-242
func classifyTransientFailure(err error) transientDecision {
	if err == nil {
		return transientDecision{}
	}
	chain := exceptionChain(err)
	if decision, ok := clientRetryDecision(chain); ok {
		return decision
	}

	var sb strings.Builder
	for _, e := range chain {
		sb.WriteString(e.Error())
		sb.WriteByte(' ')
	}
	message := strings.ToLower(sb.String())
	for _, marker := range nonRetryableTextMarkers {
		if strings.Contains(message, marker) {
			return transientDecision{}
		}
	}

	shouldRetry := ""
	var statusCode *int
	var retryAfter *float64
	for _, e := range chain {
		headers := headersOf(e)
		if shouldRetry == "" {
			shouldRetry = strings.ToLower(headerValue(headers, "x-should-retry"))
		}
		if retryAfter == nil {
			retryAfter = parseRetryAfterSeconds(headers)
		}
		if statusCode == nil {
			statusCode = statusCodeOf(e)
		}
	}

	if shouldRetry == "false" {
		return transientDecision{statusCode: statusCode}
	}
	if retryAfter != nil && *retryAfter > maxRetryAfterSeconds {
		return transientDecision{statusCode: statusCode, retryAfterSeconds: retryAfter}
	}

	retryable := false
	switch {
	case shouldRetry == "true":
		retryable = true
	case statusCode == nil:
		retryable = true
	case retryableStatusCodes[*statusCode] || *statusCode >= 500:
		retryable = true
	}
	return transientDecision{retryable: retryable, statusCode: statusCode, retryAfterSeconds: retryAfter}
}

// clientRetryDecision 采纳模型客户端自己写入的 details.should_retry。
//
// Python: _client_retry_decision L168-192
func clientRetryDecision(chain []error) (transientDecision, bool) {
	for _, e := range chain {
		details := errorDetails(e)
		if details == nil {
			continue
		}
		shouldRetry, ok := details["should_retry"].(bool)
		if !ok {
			continue
		}
		decision := transientDecision{retryable: shouldRetry, statusCode: coerceStatusCode(details["status_code"])}
		if raw, exists := details["retry_after_seconds"]; exists && raw != nil {
			if f, ok := toFloat(raw); ok && f >= 0 {
				decision.retryAfterSeconds = &f
			}
		}
		return decision, true
	}
	return transientDecision{}, false
}

// exceptionChain 沿 Unwrap 展开异常链（最多 16 层，防环）。
//
// Python: _exception_chain L63-77（额外看 __cause__/__context__；Go 的 Unwrap 覆盖 cause）
func exceptionChain(err error) []error {
	var chain []error
	for current := err; current != nil && len(chain) < 16; current = errors.Unwrap(current) {
		chain = append(chain, current)
	}
	return chain
}

// errorDetails 取 BaseError.Details() 的 map 形态。
func errorDetails(err error) map[string]any {
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		return nil
	}
	details, _ := baseErr.Details().(map[string]any)
	return details
}

// headersOf 汇总错误里携带的重试相关头部。
//
// Python 走异常对象的 headers/response.headers；Go 的客户端把头部扁平进 params，
// 故此处只认三个键（见 openai.HandleHTTPError）。
func headersOf(err error) map[string]string {
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		return nil
	}
	params := baseErr.Params()
	if params == nil {
		return nil
	}
	headers := make(map[string]string, 3)
	for _, key := range []string{"retry_after", "retry_after_ms", "x_should_retry"} {
		if v, ok := params[key].(string); ok && v != "" {
			headers[key] = v
		}
	}
	return headers
}

// statusCodeOf 从详细的 params/details 中提取 HTTP 状态码。
//
// Python: _status_code_of L127-138
func statusCodeOf(err error) *int {
	if details := errorDetails(err); details != nil {
		for _, key := range []string{"status_code", "provider_code"} {
			if code := coerceStatusCode(details[key]); code != nil {
				return code
			}
		}
	}
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		return nil
	}
	params := baseErr.Params()
	for _, key := range []string{"http_status", "status_code", "provider_code"} {
		if code := coerceStatusCode(params[key]); code != nil {
			return code
		}
	}
	return nil
}

// headerValue 大小写不敏感地取头部值。
func headerValue(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	wanted := strings.ToLower(name)
	if v, ok := headers[wanted]; ok {
		return strings.TrimSpace(v)
	}
	for k, v := range headers {
		if strings.ToLower(k) == wanted {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseRetryAfterSeconds 解析 Retry-After（毫秒优先，其次秒，再次 HTTP-date）。
//
// Python: _parse_retry_after_seconds L141-165
func parseRetryAfterSeconds(headers map[string]string) *float64 {
	if ms := headerValue(headers, "retry_after_ms"); ms != "" {
		if f, err := strconv.ParseFloat(ms, 64); err == nil {
			v := f / 1000
			if v < 0 {
				v = 0
			}
			return &v
		}
	}
	raw := headerValue(headers, "retry_after")
	if raw == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		if f < 0 {
			f = 0
		}
		return &f
	}
	parsed, err := http.ParseTime(raw)
	if err != nil {
		return nil
	}
	delay := time.Until(parsed).Seconds()
	if delay < 0 {
		delay = 0
	}
	return &delay
}

// coerceStatusCode 把任意标量转成正整数状态码。
func coerceStatusCode(value any) *int {
	f, ok := toFloat(value)
	if !ok {
		return nil
	}
	code := int(f)
	if code <= 0 {
		return nil
	}
	return &code
}

// toFloat 兼容 int/int64/float64/数字字符串四种形态。
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
```
（新增导入：`errors`、`net/http`、`strconv`。）

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestClassifyTransientFailure|TestParseRetryAfterSeconds|TestExceptionChain' ./internal/agentcore/harness/rails/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/model_anomaly.go internal/agentcore/harness/rails/model_anomaly_test.go
git commit -m "feat(harness): 瞬时失败分类与异常链解析"
```

---

## Task 10: 重试记账与钩子闭环

**Files:**
- Modify: `internal/agentcore/harness/rails/model_anomaly.go`
- Test: `internal/agentcore/harness/rails/model_anomaly_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
func TestOnModelException_重复异常按退避表重试(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithBackoffSeconds([]float64{0.5, 1.0}))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	repeatErr := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(repeatErrorMarker+": field=content"))

	r.OnModelException(context.Background(), withException(cbc, repeatErr))
	req := cbc.ConsumeRetryRequest()
	if req == nil || req.DelaySeconds != 0.5 {
		t.Fatalf("第 1 次应请求 0.5s 重试，实际 %+v", req)
	}

	r.OnModelException(context.Background(), withException(cbc, repeatErr))
	req = cbc.ConsumeRetryRequest()
	if req == nil || req.DelaySeconds != 1.0 {
		t.Fatalf("第 2 次应请求 1.0s 重试，实际 %+v", req)
	}

	r.OnModelException(context.Background(), withException(cbc, repeatErr)) // 达到 max_retries
	if req = cbc.ConsumeRetryRequest(); req != nil {
		t.Fatalf("超过 max_retries 后不应再重试，实际 %+v", req)
	}
	// 计数已归零：再来一次重新获得预算
	r.OnModelException(context.Background(), withException(cbc, repeatErr))
	if req = cbc.ConsumeRetryRequest(); req == nil {
		t.Fatal("计数归零后应重新重试")
	}
}

func TestOnModelException_流超时与重复各自计数(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithMaxRetries(1), WithBackoffSeconds([]float64{0}))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	timeoutErr := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("LLM stream timeout: stream frame timeout: stage=idle_token"))

	r.OnModelException(context.Background(), withException(cbc, timeoutErr))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("流超时第 1 次应重试")
	}
	r.OnModelException(context.Background(), withException(cbc, timeoutErr))
	if cbc.ConsumeRetryRequest() != nil {
		t.Fatal("max_retries=1 时第 2 次不应重试")
	}
	// 重复异常有独立预算
	repeatErr := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(repeatErrorMarker))
	r.OnModelException(context.Background(), withException(cbc, repeatErr))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("重复异常应使用独立计数")
	}
}

func TestOnModelException_已输出可见内容时不做瞬时重试(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)
	cbc.SetModelVisibleOutputEmitted(true)

	r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if cbc.ConsumeRetryRequest() != nil {
		t.Fatal("已输出内容后不应做瞬时重试")
	}

	// 但重复异常仍要重试（上游顺序如此）
	repeatErr := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(repeatErrorMarker))
	r.OnModelException(context.Background(), withException(cbc, repeatErr))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("重复异常不受可见输出闸门限制")
	}
}

func TestOnModelException_瞬时重试指数退避与耗尽(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithTransientMaxRetries(2), WithTransientBaseDelaySeconds(2))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 2 {
		t.Fatalf("第 1 次应为 2s，实际 %+v", req)
	}
	r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 4 {
		t.Fatalf("第 2 次应为 4s，实际 %+v", req)
	}
	r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req != nil {
		t.Fatal("耗尽后不应重试")
	}
}

func TestOnModelException_RetryAfter优先(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	r.OnModelException(context.Background(), withException(cbc, httpErr(429, map[string]any{"retry_after": "7"}, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 7 {
		t.Fatalf("应使用 Retry-After=7，实际 %+v", req)
	}
}

func TestRetryCounters_按会话隔离且归零即删(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithMaxRetries(1), WithBackoffSeconds([]float64{0}))
	cbcA := newTestCBCWithSession("session-a")
	cbcB := newTestCBCWithSession("session-b")
	r.BeforeInvoke(context.Background(), cbcA)
	r.BeforeInvoke(context.Background(), cbcB)

	repeatErr := exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg(repeatErrorMarker))
	r.OnModelException(context.Background(), withException(cbcA, repeatErr))
	cbcA.ConsumeRetryRequest()

	if r.counterValue("session-b", "repeat") != 0 {
		t.Fatal("会话 A 的计数不应影响会话 B")
	}

	r.AfterInvoke(context.Background(), cbcA)
	if _, ok := r.counters["session-a"]; ok {
		t.Fatal("AfterInvoke 应删除该会话条目")
	}
}
```

测试辅助（放在 `model_anomaly_test.go` 顶部）：

```go
// withException 把错误塞进 cbc 再返回（OnModelException 读 cbc.Exception()）。
func withException(cbc *agentinterfaces.AgentCallbackContext, err error) *agentinterfaces.AgentCallbackContext {
	cbc.SetException(err)
	return cbc
}

// fakeSession 最小 SessionFacade 实现，仅提供 GetSessionID。
type fakeSession struct {
	agentinterfaces.UnimplementedSessionFacade // 若仓库无此内嵌类型，改为实现所需方法或使用 session.NewStub
	id string
}

func (f *fakeSession) GetSessionID() string { return f.id }

func newTestCBCWithSession(id string) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, &fakeSession{id: id})
}
```
**实施提示**：先 `grep -n "type .*SessionFacade" internal/agentcore/session/` 与 `grep -rn "实现 SessionFacade" internal/` 找一个现成的测试桩（既有 rail 测试有先例）；没有桩时用最小的结构体实现 `interfaces.SessionFacade` 的全部方法（返回零值），不要新增生产代码。

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestOnModelException|TestRetryCounters' ./internal/agentcore/harness/rails/ -v
```
Expected: FAIL —— `r.BeforeInvoke undefined` / `r.OnModelException undefined`

- [ ] **Step 3: 实现**

```go
// GetCallbacks 注册 4 个钩子。
//
// Python: DeepAgentRail 子类按事件自动注册；Go 需显式声明。
func (r *ModelAnomalyDetectionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		return nil
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		return nil
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackOnModelException] = func(ctx context.Context, railCtx any) error {
		return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// BeforeInvoke 清零本次 invoke 的三类重试计数与压缩计数。
//
// Python: before_invoke L417-422
func (r *ModelAnomalyDetectionRail) BeforeInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	r.resetRetryCounters(cbc)
	r.resetCompactCount(cbc)
}

// AfterInvoke 释放本会话条目，避免映射随会话数增长。
//
// Python: after_invoke L424-425（只清压缩计数；Go 侧一并清重试计数，语义更严格）
func (r *ModelAnomalyDetectionRail) AfterInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	r.resetRetryCounters(cbc)
	r.resetCompactCount(cbc)
}

// OnModelException 对可重试的模型异常请求整调用重试。
//
// 判定顺序即优先级：重复输出 → 流帧超时 → 可见输出闸门 → 瞬时失败。
// Python: on_model_exception L454-481
func (r *ModelAnomalyDetectionRail) OnModelException(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	err := cbc.Exception()
	if isRepeatException(err) {
		r.requestRetryOrReset(cbc, "repeat")
		return nil
	}
	if isStreamTimeoutException(err) {
		r.requestRetryOrReset(cbc, "stream_timeout")
		return nil
	}
	if cbc.ModelVisibleOutputEmitted() {
		return nil
	}

	decision := classifyTransientFailure(err)
	if !decision.retryable && decision.retryAfterSeconds != nil && *decision.retryAfterSeconds > maxRetryAfterSeconds {
		logger.Warn(logComponent).
			Str("event_type", "model_anomaly_retry_skipped").
			Float64("retry_after_seconds", *decision.retryAfterSeconds).
			Float64("max_retry_after_seconds", maxRetryAfterSeconds).
			Msg("server requested retry delay beyond the cap; not retrying")
		return nil
	}
	if decision.retryable {
		r.requestTransientRetry(cbc, decision)
	}
	return nil
}

// isRepeatException 判断是否为重复输出异常。
func isRepeatException(err error) bool {
	return err != nil && strings.Contains(err.Error(), repeatErrorMarker)
}

// isStreamTimeoutException 判断是否为流帧超时异常。
func isStreamTimeoutException(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, marker := range streamTimeoutMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// requestRetryOrReset 重复输出/流超时共用的重试记账。
//
// Python: _request_retry_or_reset L552-576
func (r *ModelAnomalyDetectionRail) requestRetryOrReset(cbc *agentinterfaces.AgentCallbackContext, reason string) {
	var field, label string
	switch reason {
	case "repeat":
		field, label = "repeat", "repeated stream output"
	case "stream_timeout":
		field, label = "stream_timeout", "stream frame timeout"
	default:
		return
	}

	key := sessionKey(cbc)
	current := r.counterValue(key, field)
	if current >= r.maxRetries {
		r.resetCounter(key, field)
		return
	}
	delay := r.backoffDelay(current)
	r.setCounterValue(key, field, current+1)
	logger.Warn(logComponent).
		Str("event_type", "model_anomaly_retry").
		Str("reason", label).
		Int("attempt", current+1).
		Int("max_retries", r.maxRetries).
		Float64("delay_seconds", delay).
		Msg("retrying model call after anomaly")
	cbc.RequestRetry(delay)
}

// requestTransientRetry 瞬时失败重试记账（Retry-After 优先，否则指数退避）。
//
// Python: _request_transient_retry L578-600
func (r *ModelAnomalyDetectionRail) requestTransientRetry(cbc *agentinterfaces.AgentCallbackContext, decision transientDecision) {
	key := sessionKey(cbc)
	current := r.counterValue(key, "transient")
	if current >= r.transientMaxRetries {
		r.resetCounter(key, "transient")
		return
	}
	delay := r.transientBaseDelay * float64(int(1)<<current)
	if decision.retryAfterSeconds != nil {
		delay = *decision.retryAfterSeconds
	}
	r.setCounterValue(key, "transient", current+1)
	status := "none"
	if decision.statusCode != nil {
		status = strconv.Itoa(*decision.statusCode)
	}
	logger.Warn(logComponent).
		Str("event_type", "model_anomaly_retry").
		Str("reason", "transient provider failure").
		Str("status", status).
		Int("attempt", current+1).
		Int("max_retries", r.transientMaxRetries).
		Float64("delay_seconds", delay).
		Msg("retrying model call after transient provider failure")
	cbc.RequestRetry(delay)
}

// sessionKey 取会话标识；无会话时用指针兜底（对齐上游 id(session) 兜底）。
func sessionKey(cbc *agentinterfaces.AgentCallbackContext) string {
	sess := cbc.Session()
	if sess == nil {
		return ""
	}
	if id := sess.GetSessionID(); id != "" {
		return id
	}
	return fmt.Sprintf("session@%p", sess)
}

// counterValue 读某个会话某类计数。
func (r *ModelAnomalyDetectionRail) counterValue(key, field string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counters, ok := r.counters[key]
	if !ok || counters == nil {
		return 0
	}
	switch field {
	case "repeat":
		return counters.repeat
	case "stream_timeout":
		return counters.streamTimeout
	default:
		return counters.transient
	}
}

// setCounterValue 写某个会话某类计数（正数才建条目）。
func (r *ModelAnomalyDetectionRail) setCounterValue(key, field string, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters, ok := r.counters[key]
	if !ok {
		counters = &retryCounters{}
		r.counters[key] = counters
	}
	switch field {
	case "repeat":
		counters.repeat = value
	case "stream_timeout":
		counters.streamTimeout = value
	default:
		counters.transient = value
	}
}

// resetCounter 归零某类计数：三类全零时删除条目（只存正计数）。
func (r *ModelAnomalyDetectionRail) resetCounter(key, field string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters, ok := r.counters[key]
	if !ok {
		return
	}
	switch field {
	case "repeat":
		counters.repeat = 0
	case "stream_timeout":
		counters.streamTimeout = 0
	default:
		counters.transient = 0
	}
	if counters.repeat == 0 && counters.streamTimeout == 0 && counters.transient == 0 {
		delete(r.counters, key)
	}
}

// resetRetryCounters 清空某会话全部重试计数。
func (r *ModelAnomalyDetectionRail) resetRetryCounters(cbc *agentinterfaces.AgentCallbackContext) {
	key := sessionKey(cbc)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.counters, key)
}

// resetCompactCount 清空某会话的压缩命中计数。
func (r *ModelAnomalyDetectionRail) resetCompactCount(cbc *agentinterfaces.AgentCallbackContext) {
	key := sessionKey(cbc)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.compactCounts, key)
}

// compactCount 读某会话的压缩命中计数。
func (r *ModelAnomalyDetectionRail) compactCount(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.compactCounts[key]
}

// setCompactCount 写某会话的压缩命中计数（<=0 即删）。
func (r *ModelAnomalyDetectionRail) setCompactCount(key string, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value <= 0 {
		delete(r.compactCounts, key)
		return
	}
	r.compactCounts[key] = value
}
```
（新增导入：`cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"`。异常读取用 `cbc.Exception()`，`SetException`/`Exception` 均已存在于 `interfaces/callback.go:475-478`。）

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestOnModelException|TestRetryCounters' ./internal/agentcore/harness/rails/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/model_anomaly.go internal/agentcore/harness/rails/model_anomaly_test.go
git commit -m "feat(harness): 异常分类接入重试记账（会话分桶+退避）"
```

---

## Task 11: 工具循环压缩 / bailout

**Files:**
- Modify: `internal/agentcore/harness/rails/model_anomaly.go`
- Test: `internal/agentcore/harness/rails/model_anomaly_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
// fakeModelContext 最小 ModelContext，只需 GetMessages/SetMessages。
type fakeModelContext struct {
	ceinterface.ModelContext // 内嵌接口，仅实现用到的方法
	msgs                    []llmschema.BaseMessage
}

func (f *fakeModelContext) GetMessages(int, bool) ([]llmschema.BaseMessage, error) { return f.msgs, nil }
func (f *fakeModelContext) SetMessages(msgs []llmschema.BaseMessage, _ bool)      { f.msgs = msgs }

func toolRoundMessages(args string, rounds int) []llmschema.BaseMessage {
	var msgs []llmschema.BaseMessage
	for i := 0; i < rounds; i++ {
		call := llmschema.NewToolCall(fmt.Sprintf("id-%d", i), "read_file", args)
		msgs = append(msgs, llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{call})))
		msgs = append(msgs, llmschema.NewToolMessage(fmt.Sprintf("id-%d", i), "content"))
	}
	return msgs
}

func newCompactRail(t *testing.T, threshold, bailout int) (*ModelAnomalyDetectionRail, *agentinterfaces.AgentCallbackContext, *fakeModelContext) {
	t.Helper()
	r, err := NewModelAnomalyDetectionRail(WithToolLoopCompact(ToolLoopCompactConfig{
		Enabled: true, ConsecutiveThreshold: threshold, BailoutThreshold: bailout,
	}))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	cbc := newTestCBC()
	mc := &fakeModelContext{}
	cbc.SetModelContext(mc)
	return r, cbc, mc
}

func TestToolLoopCompact_三轮相同折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	mc.msgs = toolRoundMessages(`{"path":"a.txt"}`, 3)

	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(mc.msgs) != 1 {
		t.Fatalf("3 轮应折叠为 1 条警告，实际 %d 条", len(mc.msgs))
	}
	userMsg, ok := mc.msgs[0].(*llmschema.UserMessage)
	if !ok {
		t.Fatalf("折叠结果应为 UserMessage，实际 %T", mc.msgs[0])
	}
	if !strings.Contains(userMsg.Content.Text(), "请跳出重复工具调用") {
		t.Fatalf("应使用中文模板（默认 cn），实际 %s", userMsg.Content.Text())
	}
}

func TestToolLoopCompact_参数不同不折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	mc.msgs = append(toolRoundMessages(`{"path":"a"}`, 1), toolRoundMessages(`{"path":"b"}`, 2)...)

	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 6 {
		t.Fatalf("参数不同不应折叠，实际 %d 条", len(mc.msgs))
	}
}

func TestToolLoopCompact_返回值不同仍折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	for i := 0; i < 3; i++ {
		call := llmschema.NewToolCall(fmt.Sprintf("id-%d", i), "read_file", `{"path":"a"}`)
		mc.msgs = append(mc.msgs, llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{call})))
		mc.msgs = append(mc.msgs, llmschema.NewToolMessage(fmt.Sprintf("id-%d", i), fmt.Sprintf("result-%d", i)))
	}
	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 1 {
		t.Fatalf("返回值不参与匹配，应折叠，实际 %d 条", len(mc.msgs))
	}
}

func TestToolLoopCompact_用户输入隔开不折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 2, 0)
	// 轮1 + 用户输入 + 轮2 + 轮3（只有轮2/轮3 背靠背）
	mc.msgs = toolRoundMessages(`{"path":"a"}`, 1)
	mc.msgs = append(mc.msgs, llmschema.NewUserMessage("继续"))
	mc.msgs = append(mc.msgs, toolRoundMessages(`{"path":"a"}`, 2)...)

	_ = r.BeforeModelCall(context.Background(), cbc)
	// 折叠后：轮1 + 用户输入 + 警告
	if len(mc.msgs) != 3 {
		t.Fatalf("只应折叠背靠背尾段，实际 %d 条", len(mc.msgs))
	}
	if _, ok := mc.msgs[1].(*llmschema.UserMessage); !ok || mc.msgs[1].(*llmschema.UserMessage).Content.Text() != "继续" {
		t.Fatal("用户输入必须保留（A-09 回归护栏）")
	}
}

func TestToolLoopCompact_第三次命中bailout(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 2, 3)
	for i := 0; i < 3; i++ {
		mc.msgs = toolRoundMessages(`{"path":"a"}`, 2)
		err := r.BeforeModelCall(context.Background(), cbc)
		if i < 2 && err != nil {
			t.Fatalf("第 %d 次不应中止: %v", i+1, err)
		}
		if i == 2 {
			if err == nil {
				t.Fatal("第 3 次应 bailout")
			}
			var abortErr *cb.AbortError
			if !errors.As(err, &abortErr) {
				t.Fatalf("应返回 *AbortError，实际 %T", err)
			}
			if abortErr.Cause == nil || !strings.Contains(abortErr.Cause.Error(), "automatically aborted") {
				t.Fatalf("cause 应为兜底错误，实际 %v", abortErr.Cause)
			}
		}
	}
}

func TestToolLoopCompact_默认关闭不动作(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail() // tool_loop_compact 默认 disabled
	cbc := newTestCBC()
	mc := &fakeModelContext{msgs: toolRoundMessages(`{"path":"a"}`, 5)}
	cbc.SetModelContext(mc)

	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 10 {
		t.Fatalf("默认关闭时不应改动消息，实际 %d 条", len(mc.msgs))
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestToolLoopCompact' ./internal/agentcore/harness/rails/ -v
```
Expected: FAIL —— 折叠未发生（消息条数不变）

- [ ] **Step 3: 实现**

```go
// maybeCompactOrBailoutToolLoop 折叠连续相同工具调用，或第 N 次命中时兜底中止。
//
// 必须在 before_model_call 执行：此时上一轮工具结果刚提交进上下文，
// 晚一轮会把最新一轮也折进去却看不到它。
// Python: _maybe_compact_or_bailout_tool_loop L648-718
func (r *ModelAnomalyDetectionRail) maybeCompactOrBailoutToolLoop(cbc *agentinterfaces.AgentCallbackContext) error {
	cfg := r.toolLoopCompact
	if !cfg.Enabled {
		return nil
	}
	mc := cbc.ModelContext()
	if mc == nil {
		return nil
	}
	messages, err := mc.GetMessages(0, false)
	if err != nil || len(messages) == 0 {
		return nil
	}

	foldStart, foldEnd, rounds, ok := findToolLoopCompactRange(messages, cfg.ConsecutiveThreshold)
	if !ok {
		return nil
	}

	key := sessionKey(cbc)
	next := r.compactCount(key) + 1
	if cfg.BailoutThreshold > 0 && next >= cfg.BailoutThreshold {
		r.setCompactCount(key, 0)
		logger.Warn(logComponent).
			Str("event_type", "tool_loop_bailout").
			Int("next_compaction", next).
			Int("threshold", cfg.BailoutThreshold).
			Msg("tool-loop bailout")
		return cb.NewAbortError(toolLoopBailoutMessage,
			exception.BuildError(exception.StatusContextExecutionError,
				exception.WithMsg(toolLoopBailoutMessage)))
	}

	latest := rounds[len(rounds)-1]
	callsText, resultsText := collectCallAndResultSets(messages[latest.start:latest.end])
	warning := llmschema.NewUserMessage(buildToolLoopWarningContent(
		resolveToolLoopWarningLanguage(cbc), callsText, resultsText))

	compacted := make([]llmschema.BaseMessage, 0, len(messages)-(foldEnd-foldStart)+1)
	compacted = append(compacted, messages[:foldStart]...)
	compacted = append(compacted, warning)
	compacted = append(compacted, messages[foldEnd:]...)
	mc.SetMessages(compacted, false)
	r.setCompactCount(key, next)

	logger.Info(logComponent).
		Str("event_type", "tool_loop_compacted").
		Int("before", len(messages)).
		Int("after", len(compacted)).
		Int("compact_count", next).
		Msg("compacted consecutive identical tool loops")
	return nil
}

// findToolLoopCompactRange 找出尾部背靠背相邻的相同工具轮次区间。
//
// 两个 break 条件缺一不可：指纹不同，或两轮之间夹了消息（end != 下一轮 start）。
// 后者是 A-09（589da12a）的核心：读文件 → 用户新输入 → 再读同一文件不是循环，
// 折了会把用户输入一起删掉。
// Python: _find_tool_loop_compact_range L728-757
func findToolLoopCompactRange(messages []llmschema.BaseMessage, threshold int) (int, int, []toolRound, bool) {
	if threshold < 2 {
		return 0, 0, nil, false
	}
	rounds := collectToolRounds(messages)
	if len(rounds) < threshold {
		return 0, 0, nil, false
	}

	eligible := make([]toolRound, 0, len(rounds))
	for i := len(rounds) - 1; i >= 0; i-- {
		current := rounds[i]
		if len(eligible) == 0 {
			eligible = append(eligible, current)
			continue
		}
		previous := eligible[len(eligible)-1]
		if !sameFingerprint(current.fingerprint, previous.fingerprint) {
			break
		}
		if current.end != previous.start {
			break
		}
		eligible = append(eligible, current)
	}
	for i, j := 0, len(eligible)-1; i < j; i, j = i+1, j-1 {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}
	if len(eligible) < threshold {
		return 0, 0, nil, false
	}
	return eligible[0].start, eligible[len(eligible)-1].end, eligible, true
}

// collectToolRounds 收集所有"完整"的工具调用轮次。
//
// 完整性要求：assistant 的每个 tool_call 后面紧跟对应 tool 消息；缺一个就不算一轮。
// 匹配只看工具名 + 规范化入参，返回值不参与。
// Python: _collect_tool_rounds L760-829
func collectToolRounds(messages []llmschema.BaseMessage) []toolRound {
	var rounds []toolRound
	index := 0
	total := len(messages)
	for index < total {
		assistant, ok := messages[index].(*llmschema.AssistantMessage)
		if !ok || len(assistant.ToolCalls) == 0 {
			index++
			continue
		}
		pending := make(map[string]bool, len(assistant.ToolCalls))
		for _, call := range assistant.ToolCalls {
			if call != nil && call.ID != "" {
				pending[call.ID] = true
			}
		}
		if len(pending) == 0 {
			index++
			continue
		}

		callByID := make(map[string]toolFingerprintItem, len(assistant.ToolCalls))
		for _, call := range assistant.ToolCalls {
			if call == nil {
				continue
			}
			name := strings.TrimSpace(call.Name)
			if call.ID == "" || name == "" {
				continue
			}
			callByID[call.ID] = toolFingerprintItem{name: name, args: canonicalArgumentsText(call.Arguments)}
		}
		if len(callByID) == 0 {
			index++
			continue
		}

		cursor := index + 1
		for cursor < total && len(pending) > 0 {
			toolMsg, ok := messages[cursor].(*llmschema.ToolMessage)
			if !ok {
				break
			}
			if !pending[toolMsg.ToolCallID] {
				break
			}
			delete(pending, toolMsg.ToolCallID)
			cursor++
		}
		if len(pending) > 0 {
			index++
			continue
		}

		fingerprint := make([]toolFingerprintItem, 0, len(callByID))
		for _, item := range callByID {
			fingerprint = append(fingerprint, item)
		}
		sort.Slice(fingerprint, func(i, j int) bool {
			if fingerprint[i].name != fingerprint[j].name {
				return fingerprint[i].name < fingerprint[j].name
			}
			return fingerprint[i].args < fingerprint[j].args
		})
		rounds = append(rounds, toolRound{start: index, end: cursor, fingerprint: fingerprint})
		index = cursor
	}
	return rounds
}

// sameFingerprint 比较两轮指纹是否完全一致。
func sameFingerprint(a, b []toolFingerprintItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// callItem 折叠警告里的工具调用条目（字段顺序影响 JSON 文本，必须用 struct）。
type callItem struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

// resultItem 折叠警告里的工具结果条目。
type resultItem struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// collectCallAndResultSets 把折叠区间的调用与结果序列化为 JSON 文本。
//
// Python: _collect_call_and_result_sets L832-861
func collectCallAndResultSets(folded []llmschema.BaseMessage) (string, string) {
	var callItems []callItem
	var resultItems []resultItem
	nameByID := make(map[string]string)

	for _, message := range folded {
		switch msg := message.(type) {
		case *llmschema.AssistantMessage:
			for _, call := range msg.ToolCalls {
				if call == nil {
					continue
				}
				name := strings.TrimSpace(call.Name)
				if name == "" {
					name = "(unknown)"
				}
				if call.ID != "" {
					nameByID[call.ID] = name
				}
				callItems = append(callItems, callItem{Name: name, Arguments: parseToolArguments(call.Arguments)})
			}
		case *llmschema.ToolMessage:
			content := msg.Content.Text()
			if strings.TrimSpace(content) == "" {
				continue
			}
			name := nameByID[msg.ToolCallID]
			if name == "" {
				name = "(unknown)"
			}
			resultItems = append(resultItems, resultItem{Name: name, Content: strings.TrimSpace(content)})
		}
	}
	return dumpsJSONList(callItems), dumpsJSONList(resultItems)
}

// parseToolArguments 解析工具入参；非法 JSON 原样返回文本。
//
// Python: _parse_tool_arguments L877-888
func parseToolArguments(raw string) any {
	text := strings.TrimSpace(raw)
	if text == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return text
	}
	return parsed
}

// canonicalArgumentsText 规范化入参文本（键有序、无多余空格）。
//
// Python: _canonical_arguments_text L891-896
func canonicalArgumentsText(raw string) string {
	return dumpsJSONCompact(parseToolArguments(raw))
}

// dumpsJSONList 渲染 JSON 数组（缩进 2、不转义 HTML）；空列表返回 "[]"。
func dumpsJSONList[T any](items []T) string {
	if len(items) == 0 {
		return "[]"
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(items); err != nil {
		return "[]"
	}
	return strings.TrimRight(buf.String(), "\n")
}

// dumpsJSONCompact 渲染紧凑 JSON（对应 Python separators=(",",":")）。
func dumpsJSONCompact(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprint(value)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// buildToolLoopWarningContent 渲染双语警告正文。
//
// Python: _build_tool_loop_warning_content L864-874
func buildToolLoopWarningContent(language, toolCalls, toolResults string) string {
	template := toolLoopWarningTemplateCN
	if language == "en" {
		template = toolLoopWarningTemplateEN
	}
	content := strings.ReplaceAll(template, "{tool_calls}", toolCalls)
	content = strings.ReplaceAll(content, "{tool_results}", toolResults)
	return strings.TrimSpace(content)
}

// resolveToolLoopWarningLanguage 取提示词构建器语言并归一化。
//
// Python: _resolve_tool_loop_warning_language L721-725
func resolveToolLoopWarningLanguage(cbc *agentinterfaces.AgentCallbackContext) string {
	agent := cbc.Agent()
	if agent == nil {
		return "cn"
	}
	builder := agent.SystemPromptBuilder()
	if builder == nil {
		return "cn"
	}
	return normalizeLanguage(builder.Language())
}

// normalizeLanguage 归一化语言标记（zh* / cn → cn，en → en，其余 cn）。
//
// Python: _normalize_language L905-911
func normalizeLanguage(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if normalized == "" {
		return "cn"
	}
	if strings.HasPrefix(normalized, "zh") || normalized == "cn" {
		return "cn"
	}
	if normalized == "en" {
		return "en"
	}
	return "cn"
}
```

双语模板常量（「常量」区块，逐字复刻上游 `L244-268`）：

```go
// toolLoopWarningTemplateCN 中文警告模板
const toolLoopWarningTemplateCN = `检测到连续多轮调用了相同的工具集，且每个工具的入参也相同。
最后一轮工具调用命令如下：
---
{tool_calls}
---
最后一轮工具执行结果如下：
---
{tool_results}
---
**请跳出重复工具调用**`

// toolLoopWarningTemplateEN 英文警告模板
const toolLoopWarningTemplateEN = `Detected consecutive turns calling the same tool set with identical arguments.
Latest-round tool calls:
---
{tool_calls}
---
Latest-round tool results:
---
{tool_results}
---
**Please break out of the repeated tool calls**`
```
（新增导入：`bytes`、`encoding/json`。）

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestToolLoopCompact' ./internal/agentcore/harness/rails/ -v
```
Expected: PASS（6 个 Test）

- [ ] **Step 5: 全包回归 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/harness/rails/ -v
git add internal/agentcore/harness/rails/model_anomaly.go internal/agentcore/harness/rails/model_anomaly_test.go
git commit -m "feat(harness): 工具循环压缩与 bailout（背靠背相邻性判定）"
```

---

## Task 12: factory 默认注册

**Files:**
- Modify: `internal/agentcore/harness/harness_config/params.go:23-103`、`internal/agentcore/harness/factory.go:551-600`
- Test: `internal/agentcore/harness/factory_anomaly_rail_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package harness

import (
	"reflect"
	"testing"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
)

func TestAddDefaultRails_默认注册模型异常Rail(t *testing.T) {
	agent := NewDeepAgent(nil)
	addDefaultRails(agent, hconfig.CreateDeepAgentParams{}, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if !hasRailType(agent, reflect.TypeOf(&rails.ModelAnomalyDetectionRail{})) {
		t.Fatal("默认应注册 ModelAnomalyDetectionRail")
	}
}

func TestAddDefaultRails_显式关闭不注册(t *testing.T) {
	agent := NewDeepAgent(nil)
	disabled := false
	addDefaultRails(agent, hconfig.CreateDeepAgentParams{EnableModelAnomalyDetectionRail: &disabled}, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if hasRailType(agent, reflect.TypeOf(&rails.ModelAnomalyDetectionRail{})) {
		t.Fatal("显式关闭时不应注册")
	}
}

func TestAddDefaultRails_调用方已提供不重复(t *testing.T) {
	rail, err := rails.NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	agent := NewDeepAgent(nil)
	addDefaultRails(agent, hconfig.CreateDeepAgentParams{Rails: []interfaces.AgentRail{rail}}, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	count := 0
	for _, r := range agent.Rails() {
		if _, ok := r.(*rails.ModelAnomalyDetectionRail); ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("同类型 rail 只能有一个，实际 %d", count)
	}
}
```
`hasRailType` / `agent.Rails()` 若不存在，按 `factory_test.go` 里已有的断言方式改写（`grep -n "Rails()" internal/agentcore/harness/factory_test.go`）。

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestAddDefaultRails_.*模型异常|TestAddDefaultRails_显式关闭|TestAddDefaultRails_调用方已提供' ./internal/agentcore/harness/ -v
```
Expected: FAIL —— `unknown field EnableModelAnomalyDetectionRail`

- [ ] **Step 3: 实现**

`params.go` 加字段（`EnableTaskPlanning` 附近）：

```go
	// EnableModelAnomalyDetectionRail 是否注册默认的模型异常检测 Rail。
	// nil 表示跟随上游默认（true）。用指针是为了区分"未设置"与"显式关闭"。
	// Python: factory.py create_deep_agent(enable_model_anomaly_detection_rail: bool = True)
	EnableModelAnomalyDetectionRail *bool
```

`factory.go` 的 `addDefaultRails`，在 SafetyPromptRail 之后插入：

```go
	// ModelAnomalyDetectionRail — 默认开启（Python factory.py default_rails）。
	// nil → true；显式 false 时不注册。调用方已提供同类型实例时不重复追加。
	enableAnomaly := true
	if params.EnableModelAnomalyDetectionRail != nil {
		enableAnomaly = *params.EnableModelAnomalyDetectionRail
	}
	if enableAnomaly && !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&rails.ModelAnomalyDetectionRail{})) {
		anomalyRail, err := rails.NewModelAnomalyDetectionRail()
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("ModelAnomalyDetectionRail construction failed, skipped")
		} else {
			agent.AddRail(anomalyRail)
			logger.Debug(logComponent).Msg("ModelAnomalyDetectionRail added")
		}
	}
```

- [ ] **Step 4: 跑测试，确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestAddDefaultRails' ./internal/agentcore/harness/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/harness_config/params.go internal/agentcore/harness/factory.go internal/agentcore/harness/factory_anomaly_rail_test.go
git commit -m "feat(harness): factory 默认注册 ModelAnomalyDetectionRail（可显式关闭）"
```

---

## Task 13: adapter 接线 + execution_guard 配置

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:50-190`、`internal/swarm/server/adapter/deep_adapter.go`（字段 + CreateInstance 传参）
- Modify: `resources/config.yaml`（追加 `execution_guard` 段）

- [ ] **Step 1: 写失败测试**

```go
func TestBuildModelAnomalyDetectionRail_配置开关(t *testing.T) {
	d := NewDeepAdapter()

	// enabled: false → 不构建
	off := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{"enabled": false},
	}}
	if rail := d.buildModelAnomalyDetectionRail(off); rail != nil {
		t.Fatal("enabled=false 时不应构建")
	}

	// 缺省 → 不构建（对齐上游 retry_cfg.get("enabled", False)）
	if rail := d.buildModelAnomalyDetectionRail(map[string]any{}); rail != nil {
		t.Fatal("缺省时不应构建")
	}

	// enabled: true + 自定义阈值 → 构建且参数生效
	on := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{
			"enabled":                 true,
			"max_retries":             1,
			"single_char_repeat_count": 90,
			"tool_loop_compact": map[string]any{
				"enabled":               true,
				"consecutive_threshold": 5,
				"bailout_threshold":     2,
			},
		},
	}}
	rail := d.buildModelAnomalyDetectionRail(on)
	if rail == nil {
		t.Fatal("enabled=true 时应构建")
	}
	if rail.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", rail.Priority())
	}
}
```
（若 `maxRetries` 等字段不可见，改用行为断言：构造后经 `BeforeModelCall` + 三次重复异常观察重试次数；或把断言收敛为"非 nil + priority"。实施时以能编译为准，不要为此导出字段。）

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestBuildModelAnomalyDetectionRail' ./internal/swarm/server/adapter/ -v
```
Expected: FAIL —— `d.buildModelAnomalyDetectionRail undefined`

- [ ] **Step 3: 实现**

`deep_adapter_rails.go`：

```go
// buildModelAnomalyDetectionRail 按 execution_guard 配置构建模型异常检测 Rail。
//
// Python: interface_deep.py L11036-11078 _build_model_anomaly_detection_rail
func (d *DeepAdapter) buildModelAnomalyDetectionRail(configBase map[string]any) *rails.ModelAnomalyDetectionRail {
	guard, _ := configBase["execution_guard"].(map[string]any)
	cfg, _ := guard["model_anomaly_detection_rail"].(map[string]any)
	if enabled, _ := cfg["enabled"].(bool); !enabled {
		logger.Info(logComponent).Msg("[DeepAdapter] ModelAnomalyDetectionRail disabled by config")
		return nil
	}

	opts := []rails.ModelAnomalyOption{
		rails.WithMaxRetries(intOr(cfg, "max_retries", 2)),
		rails.WithRepeatMinPatternChars(intOr(cfg, "repeat_min_pattern_chars", 2)),
		rails.WithRepeatMaxPatternChars(intOr(cfg, "repeat_max_pattern_chars", 64)),
		rails.WithRepeatMinCount(intOr(cfg, "repeat_min_count", 6)),
		rails.WithRepeatMinTotalChars(intOr(cfg, "repeat_min_total_chars", 160)),
		rails.WithRepeatWindowChars(intOr(cfg, "repeat_window_chars", 1024)),
		rails.WithSingleCharRepeatCount(intOr(cfg, "single_char_repeat_count", 100)),
	}
	if compact, ok := cfg["tool_loop_compact"].(map[string]any); ok && compact != nil {
		opts = append(opts, rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
			Enabled:              boolOr(compact, "enabled", false),
			ConsecutiveThreshold: intOr(compact, "consecutive_threshold", 4),
			BailoutThreshold:     intOr(compact, "bailout_threshold", 3),
		}))
	}

	rail, err := rails.NewModelAnomalyDetectionRail(opts...)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[DeepAdapter] ModelAnomalyDetectionRail create failed")
		return nil
	}
	logger.Info(logComponent).Msg("[DeepAdapter] ModelAnomalyDetectionRail create success")
	return rail
}

// intOr 读 map 里的整数（兼容 YAML 的 int 与 JSON 的 float64）。
func intOr(m map[string]any, key string, fallback int) int {
	if f, ok := configutil.Float(m, key); ok {
		return int(f)
	}
	return fallback
}

// boolOr 读 map 里的布尔值。
func boolOr(m map[string]any, key string, fallback bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return fallback
}
```
（`configutil` 的导入路径以文件里既有 alias 为准；`deep_adapter.go` 已用 `config.Float`。）

在 `buildAgentRails` 的步骤 7（security）之后插入步骤 8，并把原步骤 8~12 注释改为 9~13：

```go
	// 步骤 8: modelAnomalyDetectionRail — Python: _model_anomaly_detection_rail
	if anomaly := d.buildModelAnomalyDetectionRail(configBase); anomaly != nil {
		d.modelAnomalyRail = anomaly
		railsList = append(railsList, anomaly)
	}
```

`deep_adapter.go`：
- 结构体加字段 `modelAnomalyRail *rails.ModelAnomalyDetectionRail`（放在 `securityRail` 附近）
- `CreateInstance` 组装 `create_deep_agent` 参数处（`:13239` 对应逻辑）加：

```go
	guardCfg, _ := configBase["execution_guard"].(map[string]any)
	anomalyCfg, _ := guardCfg["model_anomaly_detection_rail"].(map[string]any)
	enableAnomaly, _ := anomalyCfg["enabled"].(bool)
	if createParams != nil {
		createParams.EnableModelAnomalyDetectionRail = &enableAnomaly
	}
```
（字段名以该处实际的 params 变量为准；若该函数直接调用 `harness.CreateDeepAgent(params)`，把 `&enableAnomaly` 赋给 `params.EnableModelAnomalyDetectionRail`。）

`resources/config.yaml` 追加（放在 `heartbeat` 段之前）：

```yaml
# 执行护栏：模型异常检测
#
# 代码默认：enabled（true）、max_retries=2、repeat_min_pattern_chars=2、
# repeat_max_pattern_chars=64、repeat_min_count=6、repeat_min_total_chars=160、
# repeat_window_chars=1024、single_char_repeat_count=100、
# tool_loop_compact{enabled=false, consecutive_threshold=4, bailout_threshold=3}
# 对齐上游 jiuwenswarm/resources/config.yaml:1028-1048（上游此处 enabled 与压缩均为 true）
execution_guard:
  model_anomaly_detection_rail:
    enabled: true
    max_retries: 2
    repeat_min_pattern_chars: 2
    repeat_max_pattern_chars: 64
    repeat_min_count: 6
    repeat_min_total_chars: 160
    repeat_window_chars: 1024
    single_char_repeat_count: 90
    tool_loop_compact:
      enabled: true
      consecutive_threshold: 4
      bailout_threshold: 3
```

- [ ] **Step 4: 跑测试 + 编译**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestBuildModelAnomalyDetectionRail' ./internal/swarm/server/adapter/ -v
go build ./...
```
Expected: PASS + build 无输出

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter.go resources/config.yaml
git commit -m "feat(swarm): execution_guard 接线 ModelAnomalyDetectionRail"
```

---

## Task 14: 团队成员 rail

**Files:**
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance.go`（builder + 组装序列）

- [ ] **Step 1: 写失败测试**

```go
func TestBuildModelAnomalyDetectionRail_团队成员(t *testing.T) {
	cfg := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{"enabled": true},
	}}
	if rail := BuildModelAnomalyDetectionRail(cfg); rail == nil {
		t.Fatal("enabled=true 时应构建")
	}
	if rail := BuildModelAnomalyDetectionRail(map[string]any{}); rail != nil {
		t.Fatal("缺省时不应构建")
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestBuildModelAnomalyDetectionRail_团队成员' ./internal/swarm/agents/harness/team/ -v
```
Expected: FAIL —— `undefined: BuildModelAnomalyDetectionRail`

- [ ] **Step 3: 实现**

```go
// BuildModelAnomalyDetectionRail 为团队成员构建模型异常检测 Rail。
//
// 与单 agent 路径读同一段配置（execution_guard.model_anomaly_detection_rail），
// 保证 leader 与成员的护栏参数一致。
// Python: agents/swarm/providers/member_rails.py:613 _build_model_anomaly_detection
func BuildModelAnomalyDetectionRail(config map[string]any) *rails.ModelAnomalyDetectionRail {
	guard, _ := config["execution_guard"].(map[string]any)
	cfg, _ := guard["model_anomaly_detection_rail"].(map[string]any)
	if enabled, _ := cfg["enabled"].(bool); !enabled {
		return nil
	}
	opts := []rails.ModelAnomalyOption{
		rails.WithMaxRetries(intFieldOr(cfg, "max_retries", 2)),
		rails.WithRepeatMinPatternChars(intFieldOr(cfg, "repeat_min_pattern_chars", 2)),
		rails.WithRepeatMaxPatternChars(intFieldOr(cfg, "repeat_max_pattern_chars", 64)),
		rails.WithRepeatMinCount(intFieldOr(cfg, "repeat_min_count", 6)),
		rails.WithRepeatMinTotalChars(intFieldOr(cfg, "repeat_min_total_chars", 160)),
		rails.WithRepeatWindowChars(intFieldOr(cfg, "repeat_window_chars", 1024)),
		rails.WithSingleCharRepeatCount(intFieldOr(cfg, "single_char_repeat_count", 100)),
	}
	if compact, ok := cfg["tool_loop_compact"].(map[string]any); ok && compact != nil {
		opts = append(opts, rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
			Enabled:              boolFieldOr(compact, "enabled", false),
			ConsecutiveThreshold: intFieldOr(compact, "consecutive_threshold", 4),
			BailoutThreshold:     intFieldOr(compact, "bailout_threshold", 3),
		}))
	}
	rail, err := rails.NewModelAnomalyDetectionRail(opts...)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamRuntime] ModelAnomalyDetectionRail create failed")
		return nil
	}
	logger.Info(logComponent).Msg("[TeamRuntime] ModelAnomalyDetectionRail created")
	return rail
}
```
（`intFieldOr`/`boolFieldOr` 若本文件已有等价助手则复用，不新增重复实现。）

在 `BuildTeamMemberRails` 的组装序列中，插在 heartbeat（步骤编号以文件现状为准）之前：

```go
	// N. ModelAnomalyDetectionRail（对齐上游 _COMMON_RAIL_NAMES：SECURITY → MODEL_ANOMALY_DETECTION → HEARTBEAT）
	if anomaly := BuildModelAnomalyDetectionRail(config); anomaly != nil {
		railsList = append(railsList, anomaly)
	}
```

`railWhitelist` 保持不动（上游 `RAIL_WHITELIST` 未收录）。

- [ ] **Step 4: 跑测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run 'TestBuildModelAnomalyDetectionRail' ./internal/swarm/agents/harness/team/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/team/runtime_inheritance.go internal/swarm/agents/harness/team/runtime_inheritance_test.go
git commit -m "feat(swarm): 团队成员挂载 ModelAnomalyDetectionRail"
```

---

## Task 15: 集成测试（mockllm 扩展 + 三个闭环）

**Files:**
- Modify: `tests/integration/mockllm/mock_client.go`（新增脚本化流能力）
- Create: `tests/integration/agentcore/model_anomaly/suite_test.go`、`anomaly_test.go`

- [ ] **Step 1: 扩展 mock（仅新增）**

```go
// StreamChunkScript 一次 Stream 调用的脚本化输出；nil 表示沿用 responses 队列。
type StreamChunkScript struct {
	// Chunks 依次发出的分片
	Chunks []*llmschema.AssistantMessageChunk
	// StallBefore 发出第一个分片前的等待（模拟首帧卡死）
	StallBefore time.Duration
	// GapBetween 分片之间的等待（模拟空闲卡死）
	GapBetween time.Duration
}

// WithStreamScripts 追加脚本化流响应队列（按调用顺序消费）。
func (m *MockModelClient) WithStreamScripts(scripts ...StreamChunkScript) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamScripts = append(m.streamScripts, scripts...)
	return m
}

// CreateRepeatStreamScript 构造"重复同一片段 N 次"的脚本，用于重复检测。
func CreateRepeatStreamScript(unit string, times int) StreamChunkScript {
	chunks := make([]*llmschema.AssistantMessageChunk, 0, times)
	for i := 0; i < times; i++ {
		chunks = append(chunks, llmschema.NewAssistantMessageChunk(unit))
	}
	return StreamChunkScript{Chunks: chunks}
}
```

`Stream` 实现改为：先取脚本（有则用），否则走既有 `responses` 队列；脚本路径按 `StallBefore`/`GapBetween` sleep，并尊重 `ctx.Done()`。

- [ ] **Step 2: 写三个集成用例**

```go
//go:build integration

package model_anomaly

import (
	"strings"
	"testing"
)

// 用例 1：首轮循环吐 "abc" → 重复检测 → rail 重试 → 第二轮正常
func (s *AnomalySuite) TestStreamingRepeatTriggersRetry() {
	s.SetupAgent( // 以 suite 基类实际方法为准
		s.WithMockScripts(mockllm.CreateRepeatStreamScript("abc", 60)),
		s.WithMockResponses(mockllm.CreateTextResponse("done")),
	)
	out := s.StreamUntilDone()
	if got := s.Mock().StreamCallCount(); got != 2 {
		s.T().Fatalf("应重试 1 次（共 2 次流调用），实际 %d", got)
	}
	if !strings.Contains(out, "done") {
		s.T().Fatalf("最终输出应含 done，实际 %q", out)
	}
}

// 用例 2：首帧卡死超过 idle/首帧预算 → 超时错误 → 重试成功
func (s *AnomalySuite) TestStreamFrameTimeoutTriggersRetry() {
	s.SetStreamTimeouts(0.05, 0.05) // StreamFirstChunkTimeout / StreamIdleTimeout
	s.SetupAgent(
		s.WithMockScripts(mockllm.StreamChunkScript{StallBefore: 200 * time.Millisecond,
			Chunks: []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("late")}}),
		s.WithMockResponses(mockllm.CreateTextResponse("recovered")),
	)
	out := s.StreamUntilDone()
	if s.Mock().StreamCallCount() != 2 {
		s.T().Fatalf("超时后应重试，实际 %d 次", s.Mock().StreamCallCount())
	}
	if !strings.Contains(out, "recovered") {
		s.T().Fatalf("最终输出应含 recovered，实际 %q", out)
	}
}

// 用例 3：连续 5 轮相同 tool_calls → 折叠警告 + 第 3 次命中 bailout
func (s *AnomalySuite) TestToolLoopCompactAndBailout() {
	s.SetupAgentWithToolLoopCompact(4, 3)
	s.Mock().WithStreamScripts(/* 5 轮相同 tool_calls 的脚本 + 收尾文本 */)
	s.StreamUntilDone()
	s.AssertContextContainsUserWarning("请跳出重复工具调用")
}
```
**实施要求**：`AnomalySuite` 直接复用 `tests/integration/suite` 的既有 AgentSuite 基类与 `mockllm` 装配方式（`grep -rn "MockModelClient" tests/integration/agentcore/` 找最近似用例照抄）。哪个用例在基线结构里做不到（例如拿不到 context 消息），就把该断言降级为"`StreamCallCount` + 输出文本"两级，并在用例注释里写明原因——不允许留空断言或 `Skip`。

- [ ] **Step 3: 跑集成测试，确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/model_anomaly/ -v
```
Expected: FAIL（用例未实现/断言不成立）

- [ ] **Step 4: 实现到通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/model_anomaly/ -v
go run ./cmd/integration-audit -verify
```
Expected: PASS + 审计通过（跨 ≥3 子系统）

- [ ] **Step 5: 提交**

```bash
git add tests/integration/mockllm/mock_client.go tests/integration/agentcore/model_anomaly/
git commit -m "test(integration): 重复输出/流超时/工具循环压缩三个闭环"
```

---

## Task 16: 文档回填 + 全量验收

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（9.86、新增 2.18、9.3、adapter 行、25.x 行）
- Modify: `docs/sync/defect-plan-2026-11-04.md`（A-09）
- Modify: `docs/superpowers/plans/2026-11-04-upstream-defect-group-a.md:1860`（勘误）

- [ ] **Step 1: 回填 9.86 行**

把 `IMPLEMENTATION_PLAN.md:584` 改为（保留原文关键约束与 SHA，只加结果）：

```markdown
| 9.86 | ✅ | ModelAnomalyDetectionRail | 瞬时失败分类 + 退避重试 + 退化重复检测（尾窗 1024 / 单字符 90 / 多字符 6 次·160 字符，`pyReprString` 对齐错误文案）+ 流超时识别（←2.18）+ 工具循环压缩（**按背靠背相邻性判定**，`589da12a`，`end != 下一轮 start` 即断）+ bailout（第 3 次命中 `AbortError`，cause=CONTEXT_EXECUTION_ERROR）；接线：factory 默认开关（`*bool`，nil→true）+ adapter `execution_guard.model_anomaly_detection_rail` + 团队成员 rail + `resources/config.yaml`；偏差：检查器去重靠代码指针、计数器按 session 分桶、超时走 `StreamError` 哨兵、默认值仅在 `NewModelClientConfig` 生效 | `openjiuwen/harness/rails/model_anomaly_detection_rail.py` |
```

- [ ] **Step 2: 新增 2.18 行**

在 `IMPLEMENTATION_PLAN.md` 领域二（`2.16` 行之后）插入：

```markdown
| 2.18 | ✅ | Model 门面流帧超时 | `ModelClientConfig.StreamFirstChunkTimeout/StreamIdleTimeout`（`*float64`，nil=禁用，默认 300/60）+ `AssistantMessageChunk.CarriesOutputToken` + `Model.Stream` select 接收超时（`waited_since_progress` 只累计等待耗时，框架帧不重置预算，超时 cancel 派生 ctx 并排空）+ `LLM_CALL_ERROR` 回调与`LLM stream timeout: stream frame timeout: stage=...` 文案逐字对齐；⤴️ 2026-11-06 已回填 9.86 流超时识别与 2.14 门面 | `openjiuwen/core/foundation/llm/model.py` · `schema/config.py` · `schema/message_chunk.py` |
```

- [ ] **Step 3: 级联标记**

- `9.3 DeepAgent Factory` 行末追加：`；⤴️ 9.86 默认注册 ModelAnomalyDetectionRail（EnableModelAnomalyDetectionRail *bool，nil→true）`
- adapter 相关行（`10.6.x` 中 `_build_agent_rails` 那条）行末追加：`；⤴️ 9.86 execution_guard.model_anomaly_detection_rail 接线`
- 领域二十五「团队成员」行末追加：`；⤴️ 9.86 BuildModelAnomalyDetectionRail`

- [ ] **Step 4: 台账与勘误**

- `docs/sync/defect-plan-2026-11-04.md` A-09 状态改 `✅`，证据列追加：`实现位置 internal/agentcore/harness/rails/model_anomaly.go findToolLoopCompactRange（end != 下一轮 start 即 break）`
- `docs/superpowers/plans/2026-11-04-upstream-defect-group-a.md:1860` 的 `9.30` 改为 `9.86`，并加一行：`> 勘误（2026-11-06）：9.30 已被 ExploreAgent 占用，本条实际落位 9.86。`

- [ ] **Step 5: 全量验收**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/... ./internal/swarm/... -count=1
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/... -count=1
go vet ./...
golangci-lint run ./internal/agentcore/harness/rails/... ./internal/swarm/server/adapter/...
/usr/local/go/bin/gofmt -l internal/agentcore internal/swarm tests
```
Expected: 全部通过；`gofmt -l` 无输出

- [ ] **Step 6: 提交 + 推送等 CI**

```bash
git add IMPLEMENTATION_PLAN.md docs/sync/defect-plan-2026-11-04.md docs/superpowers/plans/2026-11-04-upstream-defect-group-a.md
git commit -m "docs: 回填 9.86 与 2.18（含 A-09 台账与 9.30 勘误）"
git push
gh run list --limit 3
```
Expected: CI 四 job（Build / Lint / Unit / Integration）全绿；红灯就修到绿，不允许带红收工。

---

## 自查记录（写完计划后回看 spec）

| spec 章节 | 对应任务 |
|---|---|
| §1.2 地基 5 处 | Task 1/2（检查器、可见输出）、Task 3/4/5（超时与 `CarriesOutputToken`）、Task 6（HTTP 头） |
| §4 决策 D1-D4 | Task 1（裸 func + `funcPtrEqual`）、Task 10（会话分桶+归零即删）、Task 5（select 超时）、Task 3（三态指针） |
| §5.2 修改 11 个文件 | Task 1-6（agentcore）、Task 12（factory）、Task 13-14（swarm） |
| §6.1-6.7 语义 | Task 8（重复检测/尾窗）、Task 9（分类优先级）、Task 10（判定顺序与计数）、Task 11（压缩/背靠背/bailout/语言模板） |
| §7.1 A~E | Task 8/9/10/11（rail）、Task 1/2/3/4/5/6（地基） |
| §7.2 三个集成闭环 | Task 15 |
| §8 回填 7 处 | Task 16 |
| §9 四段提交 | 各 Task 的 commit 步骤 |
| §10 本批不做 | 计划中未建任务（符合） |
