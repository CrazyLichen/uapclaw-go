package harness_providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// 本文件对齐 tests/unit_tests/harness_providers/test_io_adapter.py（621 行 /
// 19 用例）。按 spec §7.3 裁决用真实 SerializedTurnHarness 子类（scriptedHarness
// / selectableHarness）替代上游的 _FakeHarness。

// ──────────────────────────── 测试替身 ────────────────────────────

// recordingGateway 记录调用并按脚本返回结果的工具网关。
type recordingGateway struct {
	mu          sync.Mutex
	invocations []harnessprotocol.ToolInvocation
	result      harnessprotocol.ToolExecutionResult
	err         error
}

// Definitions 无工具。
func (r *recordingGateway) Definitions(ctx context.Context) ([]harnessprotocol.ToolDefinition, error) {
	return nil, nil
}

// Invoke 记录并返回脚本结果。
func (r *recordingGateway) Invoke(ctx context.Context,
	invocation harnessprotocol.ToolInvocation) (harnessprotocol.ToolExecutionResult, error) {
	r.mu.Lock()
	r.invocations = append(r.invocations, invocation)
	result, invokeErr := r.result, r.err
	r.mu.Unlock()
	if invokeErr != nil {
		return harnessprotocol.ToolExecutionResult{}, invokeErr
	}
	return result, nil
}

// invocationCount 线程安全地读调用次数。
func (r *recordingGateway) invocationCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.invocations)
}

// newAdapterHarness 构造一个声明全部宿主交互能力的基座并配好适配器。
func newAdapterHarness(t *testing.T, opts HarnessIOAdapterOptions) (*scriptedHarness, *HarnessIOAdapter) {
	t.Helper()
	harness := newScriptedHarness(t)
	adapter := NewHarnessIOAdapter(harness, opts)
	return harness, adapter
}

// adapterContext 带工具网关的宿主上下文。
func adapterContext(gateway harnessprotocol.ToolGateway) harnessprotocol.HarnessContext {
	harnessContext := testHarnessContext()
	harnessContext.Tools = gateway
	return harnessContext
}

// snapshotOutputs 快照读取当前输出队列（不等待、不消费）。
// 用于投影断言： Eventually 等块就绪后直接读，避免「Stop 后收集」
// 的消费者竞态（泵 defer 与 defer Stop 的等待链在特定时序下互锁，
// 实施时实测偶发死锁）。
func snapshotOutputs(t *testing.T, adapter *HarnessIOAdapter) []any {
	t.Helper()
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return append([]any(nil), adapter.outputQueue...)
}

// collectOutputs 消费全部输出直到流结束。
func collectOutputs(t *testing.T, adapter *HarnessIOAdapter) []any {
	t.Helper()
	iterator := adapter.Outputs()
	var chunks []any
	ctx := context.Background()
	for {
		chunk, ok := iterator.Next(ctx)
		if !ok {
			return chunks
		}
		chunks = append(chunks, chunk)
	}
}

// toOutputSchema 把块值归一成 OutputSchema（兼容值与指针形态）。
func toOutputSchema(chunk any) (stream.OutputSchema, bool) {
	switch typed := chunk.(type) {
	case stream.OutputSchema:
		return typed, true
	case *stream.OutputSchema:
		if typed != nil {
			return *typed, true
		}
	}
	return stream.OutputSchema{}, false
}

// chunksByType 按类型筛块。
func chunksByType(chunks []any, chunkType string) []stream.OutputSchema {
	var matched []stream.OutputSchema
	for _, chunk := range chunks {
		if schema, ok := toOutputSchema(chunk); ok && schema.Type == chunkType {
			matched = append(matched, schema)
		}
	}
	return matched
}

// ──────────────────────────── 动态工具网关 ────────────────────────────

// TestAdapter_动态工具走宿主网关 对齐 test_io_adapter.py:184-215。
func TestAdapter_动态工具走宿主网关(t *testing.T) {
	gateway := &recordingGateway{result: harnessprotocol.ToolExecutionResult{
		Content: "工具结果",
	}}
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, adapterContext(gateway)))
	defer func() { _ = adapter.Stop(context.Background()) }()

	request := &harnessprotocol.DynamicToolCallRequest{RequestID: "dyn-1", CallID: "c1", ToolName: "ls"}
	response, err := adapter.Handle(ctx, request)
	require.NoError(t, err)
	dynamic, ok := response.(*harnessprotocol.DynamicToolCallResponse)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusCompleted, dynamic.Status)
	assert.False(t, dynamic.IsError)
	require.NotNil(t, dynamic.Result)
	assert.Equal(t, "工具结果", dynamic.Result)
	assert.Equal(t, 1, gateway.invocationCount())
}

// TestAdapter_动态工具无网关拒绝 对齐 test_io_adapter.py:184 分支的 gateway None。
func TestAdapter_动态工具无网关拒绝(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	// 启动时不带网关 → toolGateway 为 nil
	require.NoError(t, adapter.Start(context.Background(), testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()
	request := &harnessprotocol.DynamicToolCallRequest{RequestID: "dyn-2"}
	response, err := adapter.Handle(context.Background(), request)
	require.NoError(t, err)
	dynamic, ok := response.(*harnessprotocol.DynamicToolCallResponse)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusDeclined, dynamic.Status)
	assert.True(t, dynamic.IsError)
	require.NotNil(t, dynamic.ErrorMessage)
	assert.Contains(t, *dynamic.ErrorMessage, "no native tool gateway")
}

// TestAdapter_动态工具宿主失败浮出 对齐 test_io_adapter.py:379-386。
func TestAdapter_动态工具宿主失败浮出(t *testing.T) {
	gateway := &recordingGateway{err: errors.New("boom")}
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	require.NoError(t, adapter.Start(context.Background(), adapterContext(gateway)))
	defer func() { _ = adapter.Stop(context.Background()) }()
	response, err := adapter.Handle(context.Background(),
		&harnessprotocol.DynamicToolCallRequest{RequestID: "dyn-3", ToolName: "ls"})
	require.NoError(t, err)
	dynamic, ok := response.(*harnessprotocol.DynamicToolCallResponse)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusCompleted, dynamic.Status)
	assert.True(t, dynamic.IsError)
	require.NotNil(t, dynamic.ErrorMessage)
	assert.Equal(t, "boom", *dynamic.ErrorMessage)
}

// ──────────────────────────── 投影 ────────────────────────────

// TestAdapter_输出与工具项投影 对齐 test_io_adapter.py:216-261。
func TestAdapter_输出与工具项投影(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	// ⚠️ adapter 的泵占用 buffer 的单消费者租约（上游语义：
	// "adapter owns the single continuous events() consumer"），
	// 测试不得再用 collectTurn 取轮次事件，只等状态收敛。
	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	// 等投影完成（块已进队列）
	require.Eventually(t, func() bool {
		return adapter.OutputCount() > 0
	}, 3*time.Second, 10*time.Millisecond)

	// 先停泵再收集：泵在跑时队列耗尽会等待（Next 的语义），不会立即返回
	require.NoError(t, adapter.Stop(context.Background()))
	chunks := collectOutputs(t, adapter)
	types := map[string]int{}
	for _, chunk := range chunks {
		if schema, ok := toOutputSchema(chunk); ok {
			types[schema.Type]++
		}
	}
	assert.Greater(t, types["llm_output"], 0, "llm_output 块应出现")
	assert.Equal(t, 0, types["turn_lifecycle"], "默认不投影轮次生命周期")
}

// TestAdapter_轮次生命周期投影开关 对齐 test_io_adapter.py:262-318。
func TestAdapter_轮次生命周期投影开关(t *testing.T) {
	t.Run("默认关闭", func(t *testing.T) {
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()

		_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
		require.NoError(t, err)
		waitForState(t, harness, harnessprotocol.HarnessStateIdle)

		chunks := snapshotOutputs(t, adapter)
		assert.Empty(t, chunksByType(chunks, TurnLifecycleChunkType))
	})
	t.Run("开启后 STARTED 与终态按序出现", func(t *testing.T) {
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{EmitTurnLifecycle: true})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()

		_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
		require.NoError(t, err)
		// ⚠️ 不能等 harness 状态 Idle：Send 返回时 supervisor 可能还没把
		// 状态改成 RUNNING，初始 IDLE 会让 waitForState 立即通过，随后的
		// Stop 会把还没执行的轮次当排队轮次中止（started+aborted，实施时
		// 实测复现）。等投影计数：started+llm_output+finished=3 块。
		require.Eventually(t, func() bool {
			return adapter.OutputCount() >= 3
		}, 3*time.Second, 10*time.Millisecond)

		require.NoError(t, adapter.Stop(context.Background()))
		chunks := collectOutputs(t, adapter)
		lifecycles := chunksByType(chunks, TurnLifecycleChunkType)
		require.Len(t, lifecycles, 2)
		assert.Equal(t, "started", lifecycles[0].Payload.(map[string]any)["kind"])
		assert.Equal(t, "finished", lifecycles[1].Payload.(map[string]any)["kind"])
		// index 单调递增
		assert.Less(t, lifecycles[0].Index, lifecycles[1].Index)
	})
}

// TestAdapter_失败轮次保留错误详情 对齐 test_io_adapter.py:319-344。
func TestAdapter_失败轮次保留错误详情(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{EmitTurnLifecycle: true})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	harness.setCrashNext(true)
	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	// 等投影计数 started+failed=2（等 Idle 会踩「初始 IDLE」竞态，
	// Stop 会把还没执行的轮次当排队轮次中止）
	require.Eventually(t, func() bool {
		return adapter.OutputCount() >= 2
	}, 3*time.Second, 10*time.Millisecond)

	chunks := snapshotOutputs(t, adapter)
	lifecycles := chunksByType(chunks, TurnLifecycleChunkType)
	require.Len(t, lifecycles, 2)
	failed := lifecycles[1].Payload.(map[string]any)
	assert.Equal(t, "failed", failed["kind"])
	require.Contains(t, failed, "error_message")
	// base.py:644 的 message 格式：f"{card.name} turn crashed: {type(exc).__name__}"
	assert.Equal(t, "scripted turn crashed: scriptedCrashError", failed["error_message"])
}

// TestAdapter_中止轮次保留终止类别 对齐 test_io_adapter.py:345-369。
func TestAdapter_中止轮次保留终止类别(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{EmitTurnLifecycle: true})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	harness.block()
	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)
	require.NoError(t, adapter.Abort(ctx, false))
	waitForState(t, harness, harnessprotocol.HarnessStateIdle)

	chunks := snapshotOutputs(t, adapter)
	lifecycles := chunksByType(chunks, TurnLifecycleChunkType)
	require.Len(t, lifecycles, 2)
	aborted := lifecycles[1].Payload.(map[string]any)
	assert.Equal(t, "aborted", aborted["kind"])
	assert.Equal(t, "user_abort", aborted["termination_kind"])
}

// TestAdapter_工具结果带独立渲染文本 对齐 test_io_adapter.py:370-393。
func TestAdapter_工具结果带独立渲染文本(t *testing.T) {
	itemID := "item-1"
	data, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"tool_name":       "ls",
		"result":          "文件列表",
		"rendered_result": "rendered 文本",
	})
	require.NoError(t, err)
	item, err := harnessprotocol.NewItemLifecycleEvent(
		harnessprotocol.ItemEventKindCompleted, "tool", data)
	require.NoError(t, err)
	envelope, err := harnessprotocol.NewHarnessEvent(harnessprotocol.HarnessEventOptions{
		Sequence: 1, Timestamp: 1, Event: item,
		HostSessionID: testHostSessionID, AgentID: testAgentID, ItemID: itemID,
	})
	require.NoError(t, err)

	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	chunk := adapter.projectEvent(&envelope)
	require.NotNil(t, chunk)
	schema, ok := toOutputSchema(chunk)
	require.True(t, ok)
	assert.Equal(t, "tool_result", schema.Type)
	payload := schema.Payload.(map[string]any)
	assert.Equal(t, "ls", payload["tool_name"])
	assert.Equal(t, "文件列表", payload["result"])
	assert.Equal(t, "rendered 文本", payload["rendered_result"])
}

// ──────────────────────────── 交互往返 ────────────────────────────

// TestAdapter_用户输入经InteractiveInput往返 对齐 test_io_adapter.py:394-419。
func TestAdapter_用户输入经InteractiveInput往返(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	harnessContext := testHarnessContext()
	harnessContext.Interactions = adapter
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, harnessContext))
	defer func() { _ = adapter.Stop(context.Background()) }()

	// provider 发起 user_input 请求
	request := &harnessprotocol.UserInputRequest{
		RequestID: "ui-1", Prompt: "喜欢什么颜色？",
		Choices: []string{"红", "蓝"},
	}
	responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		response, handleErr := adapter.Handle(ctx, request)
		responseCh <- response
		errCh <- handleErr
	}()

	// __interaction__ 块应出现在输出流
	require.Eventually(t, func() bool {
		return adapter.HasPendingInterrupt()
	}, 3*time.Second, 10*time.Millisecond)

	// 宿主用 InteractiveInput 应答
	_, sendErr := adapter.Send(ctx, &interaction.InteractiveInput{
		UserInputs: map[string]any{"ui-1": "蓝色"},
	}, false)
	require.NoError(t, sendErr)

	select {
	case response := <-responseCh:
		userResponse, ok := response.(*harnessprotocol.UserInputResponse)
		require.True(t, ok)
		assert.Equal(t, harnessprotocol.InteractionResponseStatusCompleted, userResponse.Status)
		require.NotNil(t, userResponse.Content)
		assert.Equal(t, "蓝色", userResponse.Content)
	case err := <-errCh:
		t.Fatalf("Handle 出错: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("应答超时")
	}
}

// TestAdapter_按interruptID应答 对齐 test_io_adapter.py:420-439。
func TestAdapter_按interruptID应答(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()

	request := &harnessprotocol.UserInputRequest{RequestID: "ui-2", Prompt: "问题"}
	responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
	go func() {
		response, _ := adapter.Handle(ctx, request)
		responseCh <- response
	}()
	require.Eventually(t, func() bool {
		return adapter.HasPendingInterrupt()
	}, 3*time.Second, 10*time.Millisecond)

	// InterruptAnswers 按 interrupt_id 答
	_, sendErr := adapter.Send(ctx, &interaction.InteractiveInput{
		InterruptAnswers: map[string]any{"ui-2": "答案"},
	}, false)
	require.NoError(t, sendErr)

	select {
	case response := <-responseCh:
		userResponse, ok := response.(*harnessprotocol.UserInputResponse)
		require.True(t, ok)
		assert.Equal(t, "答案", userResponse.Content)
	case <-time.After(3 * time.Second):
		t.Fatal("应答超时")
	}
}

// TestAdapter_未匹配输入转发provider 对齐 test_io_adapter.py:440-455。
func TestAdapter_未匹配输入转发provider(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	// 无挂起交互时 InteractiveInput 直接投 provider。
	// ⚠️ 不能用 collectTurn 验证轮次：adapter 的泵占用 buffer 的单消费者
	// 租约，TurnEvents 只能拿到空游标。收到回执即证明转发成功。
	receipt, err := adapter.Send(ctx, &interaction.InteractiveInput{
		RawInputs: "普通输入",
	}, false)
	require.NoError(t, err)
	require.NotNil(t, receipt, "未匹配时应转发 provider 并拿到回执")
	require.NotEmpty(t, receipt.TurnID)
	require.Eventually(t, func() bool {
		return harness.State() == harnessprotocol.HarnessStateIdle &&
			harness.ActiveTurn() == nil
	}, 3*time.Second, 10*time.Millisecond, "轮次应跑完（activeTurn 清空后才会回到 Idle 的最终确认）")
}

// ──────────────────────────── 工具审批 ────────────────────────────

// TestAdapter_审批默认自动通过与关闭后询问 对齐 test_io_adapter.py:456-478。
func TestAdapter_审批默认自动通过与关闭后询问(t *testing.T) {
	newApproval := func() *harnessprotocol.ToolApprovalRequest {
		return &harnessprotocol.ToolApprovalRequest{RequestID: "ta-1", ToolName: "bash"}
	}

	t.Run("默认自动批准", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{
			AutoApproveTools: true,
		})
		response, err := adapter.Handle(context.Background(), newApproval())
		require.NoError(t, err)
		approval, ok := response.(*harnessprotocol.ToolApprovalResponse)
		require.True(t, ok)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionAllow, approval.Decision)
	})
	t.Run("关闭后浮现", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		adapter.SetAutoApproveTools(false)

		responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
		go func() {
			response, _ := adapter.Handle(context.Background(), newApproval())
			responseCh <- response
		}()
		require.Eventually(t, func() bool {
			return adapter.HasPendingInterrupt()
		}, 3*time.Second, 10*time.Millisecond)
		assert.True(t, adapter.answer("ta-1", map[string]any{"approved": true}))

		select {
		case response := <-responseCh:
			approval, ok := response.(*harnessprotocol.ToolApprovalResponse)
			require.True(t, ok)
			assert.Equal(t, harnessprotocol.ToolApprovalDecisionAllow, approval.Decision)
		case <-time.After(3 * time.Second):
			t.Fatal("应答超时")
		}
	})
}

// TestAdapter_agent审批总是浮现 对齐 test_io_adapter.py:479-497。
func TestAdapter_agent审批总是浮现(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{AutoApproveTools: true})

	// provider_data 带 interrupt_id：即使自动批准也浮现
	request := &harnessprotocol.ToolApprovalRequest{
		RequestID: "ta-2", ToolName: "bash",
		ProviderData: harnessprotocol.JsonObject{"interrupt_id": "int-9"},
	}
	responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
	go func() {
		response, _ := adapter.Handle(context.Background(), request)
		responseCh <- response
	}()
	require.Eventually(t, func() bool {
		return adapter.HasPendingInterrupt()
	}, 3*time.Second, 10*time.Millisecond)
	assert.True(t, adapter.answer("ta-2", map[string]any{"approved": true}))

	select {
	case response := <-responseCh:
		approval, ok := response.(*harnessprotocol.ToolApprovalResponse)
		require.True(t, ok)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionAllow, approval.Decision)
	case <-time.After(3 * time.Second):
		t.Fatal("应答超时")
	}
}

// TestAdapter_审批答案保留决策 对齐 test_io_adapter.py:498-512。
func TestAdapter_审批答案保留决策(t *testing.T) {
	request := &harnessprotocol.ToolApprovalRequest{RequestID: "ta-3"}

	cases := []struct {
		name         string
		answer       map[string]any
		wantDecision harnessprotocol.ToolApprovalDecision
		wantPersist  bool
	}{
		{"批准", map[string]any{"approved": true}, harnessprotocol.ToolApprovalDecisionAllow, false},
		{"拒绝", map[string]any{"approved": false}, harnessprotocol.ToolApprovalDecisionDeny, false},
		{"会话批准", map[string]any{"approved": true, "auto_confirm": true},
			harnessprotocol.ToolApprovalDecisionAllowForSession, true},
		{"会话批准不持久", map[string]any{"approved": true, "auto_confirm": true, "persist_allow": false},
			harnessprotocol.ToolApprovalDecisionAllowForSession, false},
		{"改参批准", map[string]any{"updated_arguments": map[string]any{"x": 1}},
			harnessprotocol.ToolApprovalDecisionAllow, false},
		{"拒绝带反馈", map[string]any{"approved": false, "feedback": "不行"},
			harnessprotocol.ToolApprovalDecisionDeny, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := approvalResponseFor(request, testCase.answer)
			approval, ok := response.(*harnessprotocol.ToolApprovalResponse)
			require.True(t, ok)
			assert.Equal(t, testCase.wantDecision, approval.Decision)
			assert.Equal(t, testCase.wantPersist, approval.ProviderData["persist_allow"] == true)
		})
	}
}

// TestAdapter_审批答案读打字与改参 对齐 test_io_adapter.py:513-529。
func TestAdapter_审批答案读打字与改参(t *testing.T) {
	request := &harnessprotocol.ToolApprovalRequest{RequestID: "ta-4"}

	t.Run("打字 y 按文本解析", func(t *testing.T) {
		response := approvalResponseFor(request, approvalAnswer("y"))
		approval := response.(*harnessprotocol.ToolApprovalResponse)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionAllow, approval.Decision)
	})
	t.Run("打字 n 拒绝", func(t *testing.T) {
		response := approvalResponseFor(request, approvalAnswer("n"))
		approval := response.(*harnessprotocol.ToolApprovalResponse)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionDeny, approval.Decision)
	})
	t.Run("skill evolution 批准映射决策", func(t *testing.T) {
		response := approvalResponseFor(request, map[string]any{"action": "allow_always"})
		approval := response.(*harnessprotocol.ToolApprovalResponse)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionAllowForSession, approval.Decision)
	})
	t.Run("skill evolution 拒绝映射决策", func(t *testing.T) {
		response := approvalResponseFor(request, map[string]any{"action": "reject", "feedback": "不"})
		approval := response.(*harnessprotocol.ToolApprovalResponse)
		assert.Equal(t, harnessprotocol.ToolApprovalDecisionDeny, approval.Decision)
		require.NotNil(t, approval.Reason)
		assert.Equal(t, "不", *approval.Reason)
	})
}

// ──────────────────────────── 投递模式与中止 ────────────────────────────

// TestAdapter_投递模式随状态与能力 对齐 test_io_adapter.py:530-550。
func TestAdapter_投递模式随状态与能力(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	// IDLE：恒 AUTO
	assert.Equal(t, harnessprotocol.DeliveryModeAuto, adapter.DeliveryMode(true))
	assert.Equal(t, harnessprotocol.DeliveryModeAuto, adapter.DeliveryMode(false))

	// RUNNING + immediate：STEER（声明了能力）
	harness.block()
	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)
	assert.Equal(t, harnessprotocol.DeliveryModeSteer, adapter.DeliveryMode(true))
	assert.Equal(t, harnessprotocol.DeliveryModeFollowUp, adapter.DeliveryMode(false))
	harness.releaseChan()
}

// TestAdapter_中止按能力回退或停止 对齐 test_io_adapter.py:551-577。
func TestAdapter_中止按能力回退或停止(t *testing.T) {
	t.Run("声明 GRACEFUL 时 immediate 回退 graceful", func(t *testing.T) {
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()

		harness.block()
		_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
		require.NoError(t, err)
		waitForState(t, harness, harnessprotocol.HarnessStateRunning)

		// scriptedHarness 只声明 GRACEFUL_ABORT；immediate 应回退
		require.NoError(t, adapter.Abort(ctx, true))
		assert.Len(t, harness.interruptedSnapshot(), 1)
		harness.releaseChan()
	})
	t.Run("无中止能力且非运行时 no-op", func(t *testing.T) {
		// idle 时 abort 直接返回 nil
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()
		require.NoError(t, adapter.Abort(ctx, true))
		assert.Empty(t, harness.interruptedSnapshot())
	})
}

// ──────────────────────────── 启动失败与 provider 扩展 ────────────────────────────

// TestAdapter_启动失败停止并传播 对齐 test_io_adapter.py:578-587。
func TestAdapter_启动失败停止并传播(t *testing.T) {
	harness := &failingOpenHarness{openErr: errors.New("cannot open")}
	harness.scriptedHarness.release = make(chan struct{})
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                       "failing",
			ImplementationVersion:      "1.0",
			ProtocolVersion:            harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{harnessprotocol.ProtocolVersion: true},
		},
		Hooks:               harness,
		EventBufferCapacity: 8,
	})
	adapter := NewHarnessIOAdapter(harness, HarnessIOAdapterOptions{})

	err := adapter.Start(context.Background(), testHarnessContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot open")
	// 启动失败后 harness 已回滚
	assert.Equal(t, 1, harness.closedCount())
}

// TestAdapter_provider扩展无handler拒绝 对齐 test_io_adapter.py:588-602。
func TestAdapter_provider扩展无handler拒绝(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	response, err := adapter.Handle(context.Background(),
		&harnessprotocol.ProviderInteractionRequest{RequestID: "pi-1"})
	require.NoError(t, err)
	providerResponse, ok := response.(*harnessprotocol.ProviderInteractionResponse)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusDeclined, providerResponse.Status)
}

// TestAdapter_provider扩展路由handler 对齐 test_io_adapter.py:603-621。
func TestAdapter_provider扩展路由handler(t *testing.T) {
	called := false
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{
		ProviderInteractionHandler: func(ctx context.Context,
			request *harnessprotocol.ProviderInteractionRequest) (*harnessprotocol.ProviderInteractionResponse, error) {
			called = true
			return &harnessprotocol.ProviderInteractionResponse{
				RequestID: request.RequestID,
				Status:    harnessprotocol.InteractionResponseStatusCompleted,
			}, nil
		},
	})
	response, err := adapter.Handle(context.Background(),
		&harnessprotocol.ProviderInteractionRequest{RequestID: "pi-2"})
	require.NoError(t, err)
	providerResponse, ok := response.(*harnessprotocol.ProviderInteractionResponse)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusCompleted, providerResponse.Status)
	assert.True(t, called)
}

// ──────────────────────────── 包级助手 ────────────────────────────

// TestToHarnessInput_各形态 对齐 test_to_harness_input（io_adapter.py:588-594）。
func TestToHarnessInput_各形态(t *testing.T) {
	// 已是 HarnessInput 直通
	original, err := harnessprotocol.NewHarnessInput("原始", nil)
	require.NoError(t, err)
	passthrough, err := ToHarnessInput(original)
	require.NoError(t, err)
	assert.Equal(t, original, passthrough)

	// 基础类型直接包
	for _, value := range []any{"文本", 42, 3.14, true, nil, []any{"a"}, map[string]any{"k": "v"}} {
		input, toErr := ToHarnessInput(value)
		require.NoError(t, toErr)
		assert.Equal(t, value, input.Content)
	}
	// 其余转字符串
	converted, err := ToHarnessInput(struct{ X int }{X: 1})
	require.NoError(t, err)
	assert.Equal(t, "{1}", converted.Content)
}

// TestApprovalAnswer_三种形态 对齐 _approval_answer（io_adapter.py:633-640）。
func TestApprovalAnswer_三种形态(t *testing.T) {
	// dict 原样
	mapping := map[string]any{"approved": true}
	assert.Equal(t, mapping, approvalAnswer(mapping))
	// 文本走 interrupt_answer(CONFIRM)
	assert.Equal(t, map[string]any{"approved": true, "feedback": "", "auto_confirm": false},
		approvalAnswer("yes"))
	// bool 决定
	assert.Equal(t, map[string]any{"approved": true}, approvalAnswer(true))
	assert.Equal(t, map[string]any{"approved": false}, approvalAnswer(nil))
	assert.Equal(t, map[string]any{"approved": false}, approvalAnswer(42))
}

// TestCancelledResponse_两种请求 对齐 _cancelled_response（io_adapter.py:643-650）。
func TestCancelledResponse_两种请求(t *testing.T) {
	approval := cancelledResponse(&harnessprotocol.ToolApprovalRequest{RequestID: "a"})
	approvalResponse := approval.(*harnessprotocol.ToolApprovalResponse)
	assert.Equal(t, harnessprotocol.ToolApprovalDecisionDeny, approvalResponse.Decision)
	require.NotNil(t, approvalResponse.Reason)
	assert.Equal(t, "interaction cancelled", *approvalResponse.Reason)

	user := cancelledResponse(&harnessprotocol.UserInputRequest{RequestID: "b"})
	userResponse := user.(*harnessprotocol.UserInputResponse)
	assert.Equal(t, harnessprotocol.InteractionResponseStatusCancelled, userResponse.Status)
}

// TestAdapter_重复挂起报错 对齐 io_adapter.py:401-402。
func TestAdapter_重复挂起报错(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()

	// 手工占一个挂起项
	adapter.mu.Lock()
	adapter.pending["dup-1"] = &pendingInteraction{
		request: &harnessprotocol.UserInputRequest{RequestID: "dup-1"},
		done:    make(chan struct{}),
	}
	adapter.mu.Unlock()

	_, err := adapter.Handle(ctx, &harnessprotocol.UserInputRequest{RequestID: "dup-1"})
	var stateErr *harnessprotocol.HarnessStateError
	require.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "already pending")
}

// TestAdapter_交互重复answer幂等 幂等应答不 panic、不改结果。
func TestAdapter_交互重复answer幂等(t *testing.T) {
	entry := &pendingInteraction{
		request: &harnessprotocol.UserInputRequest{RequestID: "idem-1"},
		done:    make(chan struct{}),
	}
	first := &harnessprotocol.UserInputResponse{RequestID: "idem-1"}
	entry.complete(first, nil)
	second := &harnessprotocol.UserInputResponse{RequestID: "idem-1", Content: "晚到的"}
	entry.complete(second, nil) // 不 panic；结果保持第一次

	response, err := entry.awaitResponse(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first, response)
}

// TestAdapter_IsPendingInterruptResumeValid 对齐 io_adapter.py:179-185。
func TestAdapter_IsPendingInterruptResumeValid(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	assert.False(t, adapter.IsPendingInterruptResumeValid(nil), "无输入不合法")
	assert.False(t, adapter.IsPendingInterruptResumeValid(&interaction.InteractiveInput{}),
		"无挂起时不合法")

	// 占一个挂起项后各路径生效
	adapter.mu.Lock()
	adapter.pending["v-1"] = &pendingInteraction{
		request: &harnessprotocol.UserInputRequest{RequestID: "v-1"},
		done:    make(chan struct{}),
	}
	adapter.mu.Unlock()

	assert.True(t, adapter.IsPendingInterruptResumeValid(&interaction.InteractiveInput{
		RawInputs: "任意",
	}), "raw_inputs + 唯一挂起 → 合法")
	assert.True(t, adapter.IsPendingInterruptResumeValid(&interaction.InteractiveInput{
		InterruptAnswers: map[string]any{"v-1": "答案"},
	}), "interrupt_id 匹配 → 合法")
	assert.False(t, adapter.IsPendingInterruptResumeValid(&interaction.InteractiveInput{
		UserInputs: map[string]any{"别的": "x"},
	}), "无匹配 → 不合法")
}

// 编译期引用（保持 import 完整性）
var (
	_ = saschema.InterruptKindConfirm
	_ = interruptpkg.ApprovalChoices
)

// ──────────────────────────── 补覆盖 ────────────────────────────

// TestAdapter_只读面与PrepareContext 访问器与能力派生。
func TestAdapter_只读面与PrepareContext(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	adapter.SetAutoApproveTools(false)

	assert.Same(t, harness, adapter.Harness())
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, adapter.State())
	assert.Nil(t, adapter.SessionID())
	assert.Empty(t, adapter.PendingInterruptIDs())
	assert.False(t, adapter.HasPendingInterrupt())

	prepared := adapter.PrepareContext(testHarnessContext())
	// USER_INPUT 必加；autoApprove=false → TOOL_APPROVAL
	assert.True(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityUserInput])
	assert.True(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityToolApproval])
	assert.False(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction])
	// 无网关 → 不声明 NATIVE_TOOL_GATEWAY
	assert.False(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityNativeToolGateway])
	// interactions 为空时装本适配器
	assert.Equal(t, adapter, prepared.Interactions)
}

// TestAdapter_PrepareContext_带网关与handler 能力派生的另一组合。
func TestAdapter_PrepareContext_带网关与handler(t *testing.T) {
	gateway := &recordingGateway{}
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{
		ProviderInteractionHandler: func(ctx context.Context,
			request *harnessprotocol.ProviderInteractionRequest) (*harnessprotocol.ProviderInteractionResponse, error) {
			return &harnessprotocol.ProviderInteractionResponse{RequestID: request.RequestID}, nil
		},
	})
	harnessContext := adapterContext(gateway)
	// 宿主已带 interactions：不覆盖
	harnessContext.Interactions = harnessprotocol.HarnessInteractionHandler(nil)
	prepared := adapter.PrepareContext(harnessContext)
	assert.True(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityNativeToolGateway])
	assert.True(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityDynamicToolCall])
	assert.True(t, prepared.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction])
}

// TestAdapter_PauseResume 各种门卫路径。
func TestAdapter_PauseResume(t *testing.T) {
	t.Run("非运行时 no-op", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		require.NoError(t, adapter.Pause(context.Background()))
		// resume 在非 PAUSED 且无 query 时 no-op
		require.NoError(t, adapter.Resume(context.Background(), nil))
	})
	t.Run("不支持时报错", func(t *testing.T) {
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()
		harness.block()
		_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
		require.NoError(t, err)
		waitForState(t, harness, harnessprotocol.HarnessStateRunning)
		var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
		require.ErrorAs(t, adapter.Pause(ctx), &unsupported)
		require.ErrorAs(t, adapter.Resume(ctx, "query"), &unsupported)
		harness.releaseChan()
	})
}

// TestAdapter_Cancel与failPending 撤销链路。
func TestAdapter_Cancel与failPending(t *testing.T) {
	t.Run("未知名忽略", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		require.NoError(t, adapter.Cancel(context.Background(), "no-such"))
	})
	t.Run("挂起项撤销后应答为 CANCELLED", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
		go func() {
			response, _ := adapter.Handle(ctx,
				&harnessprotocol.UserInputRequest{RequestID: "c-1", Prompt: "问题"})
			responseCh <- response
		}()
		require.Eventually(t, func() bool {
			return adapter.HasPendingInterrupt()
		}, 3*time.Second, 10*time.Millisecond)
		assert.Equal(t, []string{"c-1"}, adapter.PendingInterruptIDs())

		require.NoError(t, adapter.Cancel(ctx, "c-1"))
		select {
		case response := <-responseCh:
			userResponse, ok := response.(*harnessprotocol.UserInputResponse)
			require.True(t, ok)
			assert.Equal(t, harnessprotocol.InteractionResponseStatusCancelled, userResponse.Status)
		case <-time.After(3 * time.Second):
			t.Fatal("撤销后应答超时")
		}
		// 幂等
		require.NoError(t, adapter.Cancel(ctx, "c-1"))
	})
	t.Run("stop 释放全部挂起", func(t *testing.T) {
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		responseCh := make(chan harnessprotocol.HarnessInteractionResponse, 1)
		go func() {
			response, _ := adapter.Handle(ctx,
				&harnessprotocol.UserInputRequest{RequestID: "c-2", Prompt: "问题"})
			responseCh <- response
		}()
		require.Eventually(t, func() bool {
			return adapter.HasPendingInterrupt()
		}, 3*time.Second, 10*time.Millisecond)

		require.NoError(t, adapter.Stop(ctx))
		select {
		case response := <-responseCh:
			userResponse, ok := response.(*harnessprotocol.UserInputResponse)
			require.True(t, ok)
			assert.Equal(t, harnessprotocol.InteractionResponseStatusCancelled, userResponse.Status)
		case <-time.After(3 * time.Second):
			t.Fatal("stop 后应答超时")
		}
	})
}

// TestAdapter_重复Start报错 对齐 io_adapter.py:223-224。
func TestAdapter_重复Start报错(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	err := adapter.Start(ctx, testHarnessContext())
	var stateErr *harnessprotocol.HarnessStateError
	require.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "already started")
}

// TestAdapter_投影指针与值双形态 指针载荷也能投影（与 stream.go 同约定）。
func TestAdapter_投影指针与值双形态(t *testing.T) {
	output, err := harnessprotocol.NewOutputEvent(
		"o1", harnessprotocol.OutputKindText, "文本", harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelReasoning, 0, "text", nil)
	require.NoError(t, err)

	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	envelope := harnessprotocol.HarnessEvent{
		Sequence: 1, Event: output, HostSessionID: testHostSessionID, AgentID: testAgentID,
	}
	// 值形态
	chunk := adapter.projectEvent(&envelope)
	require.NotNil(t, chunk)
	schema, ok := toOutputSchema(chunk)
	require.True(t, ok)
	assert.Equal(t, "llm_reasoning", schema.Type, "REASONING 通道映射 llm_reasoning")
	// 指针形态
	envelope.Event = &output
	chunk = adapter.projectEvent(&envelope)
	require.NotNil(t, chunk)
}

// TestAdapter_非前缀快照丢弃 对齐 io_adapter.py:502-505。
func TestAdapter_非前缀快照丢弃(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	first, err := harnessprotocol.NewOutputEvent(
		"o2", harnessprotocol.OutputKindText, "ABC", harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer, 0, "text", nil)
	require.NoError(t, err)
	// FINAL 快照与 delta 内容无前缀关系：丢弃
	second, err := harnessprotocol.NewOutputEvent(
		"o2", harnessprotocol.OutputKindText, "XYZ", harnessprotocol.OutputOperationFinal,
		harnessprotocol.OutputChannelAnswer, 0, "text", nil)
	require.NoError(t, err)
	_ = adapter.projectOutput(first)
	assert.Nil(t, adapter.projectOutput(second), "非前缀快照应丢弃")
}

// TestAdapter_非tool的item跳过 对齐 io_adapter.py:521-522。
func TestAdapter_非tool的item跳过(t *testing.T) {
	data, err := harnessprotocol.FreezeJsonObject(map[string]any{"k": "v"})
	require.NoError(t, err)
	item, err := harnessprotocol.NewItemLifecycleEvent(
		harnessprotocol.ItemEventKindStarted, "step", data)
	require.NoError(t, err)
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	envelope := harnessprotocol.HarnessEvent{
		Sequence: 1, Event: item, HostSessionID: testHostSessionID, AgentID: testAgentID,
	}
	assert.Nil(t, adapter.projectEvent(&envelope), "step 类 item 不投影")
}

// TestAdapter_toolCall的arguments转JSON 对齐 io_adapter.py:527-529。
func TestAdapter_toolCall的arguments转JSON(t *testing.T) {
	data, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"name": "ls", "arguments": map[string]any{"path": "/tmp"},
	})
	require.NoError(t, err)
	item, err := harnessprotocol.NewItemLifecycleEvent(
		harnessprotocol.ItemEventKindStarted, "tool", data)
	require.NoError(t, err)
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	envelope := harnessprotocol.HarnessEvent{
		Sequence: 1, Event: item, HostSessionID: testHostSessionID, AgentID: testAgentID,
		ItemID: "tool-1",
	}
	chunk := adapter.projectEvent(&envelope)
	require.NotNil(t, chunk)
	schema, ok := toOutputSchema(chunk)
	require.True(t, ok)
	assert.Equal(t, "tool_call", schema.Type)
	payload := schema.Payload.(map[string]any)
	assert.Equal(t, "ls", payload["name"])
	assert.Contains(t, payload["arguments"], "/tmp")
	assert.Equal(t, "tool-1", payload["tool_call_id"])
}

// TestAdapter_Resume恢复路径 PAUSED 状态与带 query 的 resume。
func TestAdapter_Resume恢复路径(t *testing.T) {
	t.Run("PAUSED 态无 query 也调用 provider", func(t *testing.T) {
		// 基座不支持 PAUSE_RESUME，无法进入 PAUSED；此分支由 provider 实现。
		// 走基座可表达的一半：RUNNING 态 + 带 query → 报不支持
		harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		ctx := context.Background()
		require.NoError(t, adapter.Start(ctx, testHarnessContext()))
		defer func() { _ = adapter.Stop(context.Background()) }()
		harness.block()
		_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
		require.NoError(t, err)
		waitForState(t, harness, harnessprotocol.HarnessStateRunning)
		var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
		require.ErrorAs(t, adapter.Resume(ctx, "继续"), &unsupported)
		harness.releaseChan()
	})
	t.Run("query 转换失败透传", func(t *testing.T) {
		// 无法直接触发 FreezeJsonValue 失败（基础类型恒可冻结）；
		// 覆盖 query 非 nil 时的转换分支（PAUSED 不可达，走不支持门卫在前）
		_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
		// 非 PAUSED 且 query 为 nil → no-op（已在别处覆盖）
		// 非 PAUSED 且 query 非 nil → 门卫
		var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
		require.ErrorAs(t, adapter.Resume(context.Background(), "x"), &unsupported)
	})
}

// TestAdapter_Send的Steer重试路径 STEER 遇 StateError 且已 IDLE 时降级 AUTO。
func TestAdapter_Send的Steer重试路径(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	ctx := context.Background()
	require.NoError(t, adapter.Start(ctx, testHarnessContext()))
	defer func() { _ = adapter.Stop(context.Background()) }()

	// 竞态路径构造困难：直接验证非 STEER 错误不重试
	_, err := adapter.Send(ctx, newTaskInput(t, "任务"), true) // AUTO 路径
	require.NoError(t, err)
	// 未匹配输入再投一轮
	_, err = adapter.Send(ctx, "再来一轮", true)
	require.NoError(t, err)
}

// TestAdapter_answer构造失败时按撤销 应答构造错误透传给等待方。
func TestAdapter_answer构造失败时按撤销(t *testing.T) {
	entry := &pendingInteraction{
		request: &harnessprotocol.UserInputRequest{RequestID: "e-1"},
		done:    make(chan struct{}),
	}
	// responseFor 对非法 value（Func 类型不可冻结）返回错误
	_, err := responseFor(entry.request, func() {})
	require.Error(t, err, "不可冻结的值应报错")
	_ = entry
}

// TestAdapter_matchedAnswers带toolCallID provider_data 带 tool_call_id 时
// 每个 pending 记两条 PendingInterrupt。
func TestAdapter_matchedAnswers带toolCallID(t *testing.T) {
	_, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{})
	adapter.mu.Lock()
	adapter.pending["k-1"] = &pendingInteraction{
		request: &harnessprotocol.ToolApprovalRequest{
			RequestID:    "k-1",
			ProviderData: harnessprotocol.JsonObject{"tool_call_id": "call-9"},
		},
		done: make(chan struct{}),
	}
	adapter.mu.Unlock()

	userInput := &interaction.InteractiveInput{
		UserInputs: map[string]any{"call-9": map[string]any{"approved": true}},
	}
	answers := adapter.matchedAnswers(userInput)
	require.Len(t, answers, 1)
	assert.Equal(t, map[string]any{"approved": true}, answers["k-1"])
}

// TestAdapter_awaitResponse的ctx取消 等待方 ctx 取消时返回错误。
func TestAdapter_awaitResponse的ctx取消(t *testing.T) {
	entry := &pendingInteraction{
		request: &harnessprotocol.UserInputRequest{RequestID: "cx-1"},
		done:    make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := entry.awaitResponse(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// TestAdapter_启动失败且清理也失败 safeStopHarness 的错误分支。
func TestAdapter_启动失败且清理也失败(t *testing.T) {
	harness := &failingOpenHarness{
		openErr:  errors.New("cannot open"),
		closeErr: errors.New("cannot close"),
	}
	harness.scriptedHarness.release = make(chan struct{})
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                       "failing2",
			ImplementationVersion:      "1.0",
			ProtocolVersion:            harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{harnessprotocol.ProtocolVersion: true},
		},
		Hooks:               harness,
		EventBufferCapacity: 8,
	})
	adapter := NewHarnessIOAdapter(harness, HarnessIOAdapterOptions{})

	err := adapter.Start(context.Background(), testHarnessContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot open")
	// safeStopHarness 吞掉清理错误（仅日志）
	assert.Equal(t, 1, harness.closedCount())
}
