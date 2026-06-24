package single_agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubInvoker 实现 agentInvoker 接口，持有可配置的返回值
type stubInvoker struct {
	// invokeResult invokeImpl 返回的结果
	invokeResult any
	// invokeErr invokeImpl 返回的错误
	invokeErr error
	// streamCh streamImpl 返回的 channel
	streamCh <-chan stream.Schema
	// streamErr streamImpl 返回的错误
	streamErr error
}

func (s *stubInvoker) invokeImpl(_ context.Context, _ map[string]any, _ ...AgentOption) (any, error) {
	return s.invokeResult, s.invokeErr
}

func (s *stubInvoker) streamImpl(_ context.Context, _ map[string]any, _ ...AgentOption) (<-chan stream.Schema, error) {
	return s.streamCh, s.streamErr
}

// testSubAgent 内嵌 WarpBaseAgent 并实现 agentInvoker，验证虚分发
type testSubAgent struct {
	*WarpBaseAgent
	// invokeCalled 是否调用了 invokeImpl
	invokeCalled bool
	// streamCalled 是否调用了 streamImpl
	streamCalled bool
	// mu 保护并发字段
	mu sync.Mutex
}

func (a *testSubAgent) invokeImpl(_ context.Context, inputs map[string]any, _ ...AgentOption) (any, error) {
	a.mu.Lock()
	a.invokeCalled = true
	a.mu.Unlock()
	return map[string]any{"echo": inputs}, nil
}

func (a *testSubAgent) streamImpl(_ context.Context, _ map[string]any, _ ...AgentOption) (<-chan stream.Schema, error) {
	a.mu.Lock()
	a.streamCalled = true
	a.mu.Unlock()
	ch := make(chan stream.Schema, 1)
	ch <- stream.OutputSchema{Type: "output", Index: 0, Payload: "sub_stream"}
	close(ch)
	return ch, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewWarpBaseAgent 构造函数验证
func TestNewWarpBaseAgent(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("test_agent"), schema.WithDescription("测试 Agent"))
	agent := NewWarpBaseAgent(card, nil)

	if agent == nil {
		t.Fatal("NewWarpBaseAgent 不应返回 nil")
	}
	if agent.Card() == nil {
		t.Fatal("Card 不应为 nil")
	}
	if agent.Card().Name != "test_agent" {
		t.Errorf("Card.Name = %q, want test_agent", agent.Card().Name)
	}
	if agent.AbilityManager() == nil {
		t.Error("AbilityManager 不应为 nil")
	}
	// 新构造的 agent invoker 为 nil
	if agent.invoker != nil {
		t.Error("新构造的 agent invoker 应为 nil")
	}
}

// TestWarpBaseAgent_Invoke_正常调用 invoker 设置后，Invoke 返回 invokeImpl 结果
func TestWarpBaseAgent_Invoke_正常调用(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("inv_agent"), schema.WithDescription("调用测试"))
	agent := NewWarpBaseAgent(card, nil)
	agent.invoker = &stubInvoker{
		invokeResult: map[string]any{"answer": 42},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{"q": "hello"})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型应为 map[string]any，实际 %T", result)
	}
	if m["answer"] != 42 {
		t.Errorf("answer = %v, want 42", m["answer"])
	}
}

// TestWarpBaseAgent_Invoke_invoker未设置 invoker 为 nil 时返回 StatusAgentNotConfigured 错误
func TestWarpBaseAgent_Invoke_invoker未设置(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("nil_invoker"), schema.WithDescription("未设置 invoker"))
	agent := NewWarpBaseAgent(card, nil)

	_, err := agent.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("应有错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Status() != exception.StatusAgentNotConfigured {
		t.Errorf("Status = %v, want StatusAgentNotConfigured", baseErr.Status())
	}
}

// TestWarpBaseAgent_Invoke_触发回调 注册 GlobalAgentCallbackFunc，验证 TriggerGlobalAgent 被调用（before + after）
func TestWarpBaseAgent_Invoke_触发回调(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("cb_agent"), schema.WithDescription("回调测试"))
	agent := NewWarpBaseAgent(card, nil)
	agent.invoker = &stubInvoker{invokeResult: "ok"}

	var mu sync.Mutex
	var beforeTriggered, afterTriggered bool

	fw := callback.GetCallbackFramework()

	beforeFn := func(_ context.Context, data *callback.GlobalAgentEventData) any {
		if data.Event == callback.GlobalAgentInvokeInput {
			mu.Lock()
			beforeTriggered = true
			mu.Unlock()
		}
		return nil
	}
	afterFn := func(_ context.Context, data *callback.GlobalAgentEventData) any {
		if data.Event == callback.GlobalAgentInvokeOutput {
			mu.Lock()
			afterTriggered = true
			mu.Unlock()
		}
		return nil
	}

	fw.OnGlobalAgent(callback.GlobalAgentInvokeInput, beforeFn)
	fw.OnGlobalAgent(callback.GlobalAgentInvokeOutput, afterFn)
	defer fw.OffGlobalAgent(callback.GlobalAgentInvokeInput, beforeFn)
	defer fw.OffGlobalAgent(callback.GlobalAgentInvokeOutput, afterFn)

	_, err := agent.Invoke(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !beforeTriggered {
		t.Error("AgentInvokeInput 回调应被触发")
	}
	if !afterTriggered {
		t.Error("AgentInvokeOutput 回调应被触发")
	}
}

// TestWarpBaseAgent_Invoke_子类错误透传 invokeImpl 返回 BaseError 时直接透传
func TestWarpBaseAgent_Invoke_子类错误透传(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("base_err"), schema.WithDescription("BaseError 透传"))
	agent := NewWarpBaseAgent(card, nil)

	origErr := exception.NewBaseError(exception.StatusAgentNotConfigured, exception.WithMsg("子类错误"))
	agent.invoker = &stubInvoker{invokeErr: origErr}

	_, err := agent.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("应有错误")
	}
	// 应直接透传同一个 BaseError 实例
	if err != origErr {
		t.Errorf("应透传原始 BaseError，实际 %v", err)
	}
}

// TestWarpBaseAgent_Invoke_普通错误包装 invokeImpl 返回普通 error 时包装为 BaseError
func TestWarpBaseAgent_Invoke_普通错误包装(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("wrap_err"), schema.WithDescription("普通错误包装"))
	agent := NewWarpBaseAgent(card, nil)

	plainErr := errors.New("something went wrong")
	agent.invoker = &stubInvoker{invokeErr: plainErr}

	_, err := agent.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("应有错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Status() != exception.StatusAgentControllerRuntimeError {
		t.Errorf("Status = %v, want StatusAgentControllerRuntimeError", baseErr.Status())
	}
}

// TestWarpBaseAgent_Stream_正常调用 invoker 设置后，Stream 返回 channel 且数据正确
func TestWarpBaseAgent_Stream_正常调用(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("stream_ok"), schema.WithDescription("流式正常"))
	agent := NewWarpBaseAgent(card, nil)

	ch := make(chan stream.Schema, 2)
	ch <- stream.OutputSchema{Type: "output", Index: 0, Payload: "chunk1"}
	ch <- stream.OutputSchema{Type: "output", Index: 1, Payload: "chunk2"}
	close(ch)

	agent.invoker = &stubInvoker{streamCh: ch}

	outCh, err := agent.Stream(context.Background(), map[string]any{"q": "hi"})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}

	var items []stream.Schema
	for item := range outCh {
		items = append(items, item)
	}
	if len(items) != 2 {
		t.Fatalf("应有 2 个 item，实际 %d", len(items))
	}
	o1, ok := items[0].(stream.OutputSchema)
	if !ok || o1.Payload != "chunk1" {
		t.Errorf("第一个 item payload 应为 chunk1，实际 %v", items[0])
	}
	o2, ok := items[1].(stream.OutputSchema)
	if !ok || o2.Payload != "chunk2" {
		t.Errorf("第二个 item payload 应为 chunk2，实际 %v", items[1])
	}
}

// TestWarpBaseAgent_Stream_invoker未设置 invoker 为 nil 时返回错误
func TestWarpBaseAgent_Stream_invoker未设置(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("no_stream"), schema.WithDescription("未设置 invoker"))
	agent := NewWarpBaseAgent(card, nil)

	_, err := agent.Stream(context.Background(), nil)
	if err == nil {
		t.Fatal("应有错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Status() != exception.StatusAgentNotConfigured {
		t.Errorf("Status = %v, want StatusAgentNotConfigured", baseErr.Status())
	}
}

// TestWarpBaseAgent_Stream_每项触发回调 每个 stream item 触发一次 TriggerGlobalAgent
func TestWarpBaseAgent_Stream_每项触发回调(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("stream_cb"), schema.WithDescription("流式回调"))
	agent := NewWarpBaseAgent(card, nil)

	ch := make(chan stream.Schema, 2)
	ch <- stream.OutputSchema{Type: "output", Index: 0, Payload: "a"}
	ch <- stream.OutputSchema{Type: "output", Index: 1, Payload: "b"}
	close(ch)
	agent.invoker = &stubInvoker{streamCh: ch}

	var mu sync.Mutex
	var outputCount int

	fw := callback.GetCallbackFramework()
	afterFn := func(_ context.Context, data *callback.GlobalAgentEventData) any {
		if data.Event == callback.GlobalAgentStreamOutput {
			mu.Lock()
			outputCount++
			mu.Unlock()
		}
		return nil
	}

	fw.OnGlobalAgent(callback.GlobalAgentStreamOutput, afterFn)
	defer fw.OffGlobalAgent(callback.GlobalAgentStreamOutput, afterFn)

	outCh, err := agent.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	// 消费完 channel
	for range outCh {
	}

	mu.Lock()
	defer mu.Unlock()
	if outputCount != 2 {
		t.Errorf("AgentStreamOutput 应触发 2 次，实际 %d 次", outputCount)
	}
}

// TestWarpBaseAgent_Configure Configure 设置 config 成功
func TestWarpBaseAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("cfg_agent"), schema.WithDescription("配置测试"))
	agent := NewWarpBaseAgent(card, nil)

	cfg := agentconfig.NewReActAgentConfig(
		agentconfig.WithModelName("qwen-max"),
		agentconfig.WithMaxIterations(10),
	)
	err := agent.Configure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	got, ok := agent.Config().(*agentconfig.ReActAgentConfig)
	if !ok {
		t.Fatalf("Config 类型应为 *ReActAgentConfig，实际 %T", agent.Config())
	}
	if got.ModelName() != "qwen-max" {
		t.Errorf("ModelName() = %v, want qwen-max", got.ModelName())
	}
}

// TestWarpBaseAgent_访问器 Card/Config/AbilityManager/CallbackManager 返回正确值
func TestWarpBaseAgent_访问器(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("acc_agent"), schema.WithDescription("访问器测试"))
	agent := NewWarpBaseAgent(card, nil)

	// Card
	if agent.Card() != card {
		t.Error("Card() 应返回构造时传入的 card")
	}

	// Config 默认为 nil
	if agent.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}

	// AbilityManager 不为 nil
	if agent.AbilityManager() == nil {
		t.Error("AbilityManager() 不应为 nil")
	}
	am, ok := agent.AbilityManager().(*AbilityManager)
	if !ok {
		t.Fatalf("AbilityManager 类型应为 *AbilityManager，实际 %T", agent.AbilityManager())
	}
	if am == nil {
		t.Error("AbilityManager 内部值不应为 nil")
	}

	// CallbackManager 不为 nil（构造时初始化）
	if agent.CallbackManager() == nil {
		t.Error("CallbackManager 不应为 nil")
	}
	cm, ok := agent.CallbackManager().(*rail.AgentCallbackManager)
	if !ok {
		t.Fatalf("CallbackManager 类型应为 *rail.AgentCallbackManager，实际 %T", agent.CallbackManager())
	}
	if cm == nil {
		t.Error("CallbackManager 内部值不应为 nil")
	}
}

// TestWarpBaseAgent_虚分发 定义内嵌 WarpBaseAgent 的子类型，实现 agentInvoker，验证 invokeImpl 走子类实现
func TestWarpBaseAgent_虚分发(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("sub_agent"), schema.WithDescription("虚分发测试"))
	base := NewWarpBaseAgent(card, nil)
	sub := &testSubAgent{WarpBaseAgent: base}
	// 关键：将 invoker 指向自身，实现虚分发
	sub.invoker = sub

	// 验证 Invoke 走子类的 invokeImpl
	result, err := sub.Invoke(context.Background(), map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	sub.mu.Lock()
	called := sub.invokeCalled
	sub.mu.Unlock()
	if !called {
		t.Error("子类 invokeImpl 应被调用")
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型应为 map[string]any，实际 %T", result)
	}
	echo, ok := m["echo"].(map[string]any)
	if !ok {
		t.Fatalf("echo 类型应为 map[string]any，实际 %T", m["echo"])
	}
	if echo["key"] != "val" {
		t.Errorf("echo[key] = %v, want val", echo["key"])
	}

	// 验证 Stream 走子类的 streamImpl
	outCh, err := sub.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	sub.mu.Lock()
	streamCalled := sub.streamCalled
	sub.mu.Unlock()
	if !streamCalled {
		t.Error("子类 streamImpl 应被调用")
	}
	var items []stream.Schema
	for item := range outCh {
		items = append(items, item)
	}
	if len(items) != 1 {
		t.Fatalf("应有 1 个 stream item，实际 %d", len(items))
	}
	o, ok := items[0].(stream.OutputSchema)
	if !ok || o.Payload != "sub_stream" {
		t.Errorf("stream item payload 应为 sub_stream，实际 %v", items[0])
	}
}

// TestGlobalAgentEventType_事件名对齐Python 验证事件名与 Python AgentEvents 对齐
func TestGlobalAgentEventType_事件名对齐Python(t *testing.T) {
	// 对应 Python: openjiuwen/core/runner/callback/events.py AgentEvents
	tests := []struct {
		got  callback.GlobalAgentEventType
		want string
	}{
		{callback.GlobalAgentStarted, "_framework:agent_started"},
		{callback.GlobalAgentInvokeInput, "_framework:agent_invoke_input"},
		{callback.GlobalAgentInvokeOutput, "_framework:agent_invoke_output"},
		{callback.GlobalAgentStreamInput, "_framework:agent_stream_input"},
		{callback.GlobalAgentStreamOutput, "_framework:agent_stream_output"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("事件名 = %q, want %q", tt.got, tt.want)
		}
	}
}
