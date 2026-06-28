package runner

import (
	"context"
	"sync"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// mockAgent 模拟 BaseAgent
type mockAgent struct {
	card      *agentschema.AgentCard
	result    any
	err       error
	streamCh  <-chan stream.Schema
	streamErr error
}

func (m *mockAgent) Configure(ctx context.Context, config interfaces.AgentConfig) error {
	return nil
}

func (m *mockAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	return m.result, m.err
}

func (m *mockAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	return m.streamCh, m.streamErr
}

func (m *mockAgent) Card() *agentschema.AgentCard {
	return m.card
}

func (m *mockAgent) Config() interfaces.AgentConfig {
	return nil
}

func (m *mockAgent) AbilityManager() any {
	return nil
}

func (m *mockAgent) CallbackManager() *rail.AgentCallbackManager {
	return nil
}

func (m *mockAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...cb.CallbackOption) error {
	return nil
}

func (m *mockAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...cb.CallbackOption) error {
	return nil
}

func (m *mockAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	return nil
}

// mockWorkflow 模拟 Workflow
type mockWorkflow struct {
	card      *schema.WorkflowCard
	result    any
	err       error
	streamCh  <-chan stream.Schema
	streamErr error
}

func (m *mockWorkflow) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.WorkflowOption) (any, error) {
	return m.result, m.err
}

func (m *mockWorkflow) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.WorkflowOption) (<-chan stream.Schema, error) {
	return m.streamCh, m.streamErr
}

func (m *mockWorkflow) Card() *schema.WorkflowCard {
	return m.card
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestRunner 创建独立的测试 Runner（不修改全局实例）
func newTestRunner() *Runner {
	return &Runner{
		runnerID:          "test-runner",
		resourceMgr:       resources_manager.NewResourceMgr(),
		messageQueue:      message_queue.NewMessageQueueInMemory(100, 30*1e9),
		callbackFramework: cb.NewCallbackFramework(),
	}
}

// newTestAgentCard 创建测试用最小 AgentCard（仅含 ID）。
func newTestAgentCard(agentID string) *agentschema.AgentCard {
	return &agentschema.AgentCard{
		BaseCard: schema.BaseCard{ID: agentID},
	}
}

// makeStreamCh 创建包含指定数据的流通道
func makeStreamCh(items ...stream.Schema) <-chan stream.Schema {
	ch := make(chan stream.Schema, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestRunAgent_正常调用 测试 RunAgent 正常调用路径
func TestRunAgent_正常调用(t *testing.T) {
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("test-agent"), schema.WithDescription("测试 Agent")),
		result: map[string]any{"output": "hello"},
	}
	sess := session.CreateAgentSession("test-session", newTestAgentCard("test-agent"), nil)

	result, err := RunAgent(context.Background(), ByAgent(ag), map[string]any{"input": "test"}, sess, nil, nil)
	if err != nil {
		t.Fatalf("RunAgent 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result 类型错误: %T", result)
	}
	if m["output"] != "hello" {
		t.Errorf("output = %v, want hello", m["output"])
	}
}

// TestRunAgent_执行错误 测试 RunAgent 执行出错
func TestRunAgent_执行错误(t *testing.T) {
	ag := &mockAgent{
		card: agentschema.NewAgentCard(schema.WithName("err-agent"), schema.WithDescription("错误 Agent")),
		err:  context.DeadlineExceeded,
	}
	sess := session.CreateAgentSession("err-session", newTestAgentCard("err-agent"), nil)

	_, err := RunAgent(context.Background(), ByAgent(ag), nil, sess, nil, nil)
	if err == nil {
		t.Error("RunAgent 应返回错误")
	}
}

// TestRunAgent_无会话 测试 RunAgent 无会话时自动创建会话
func TestRunAgent_无会话(t *testing.T) {
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("no-sess-agent"), schema.WithDescription("无会话 Agent")),
		result: "ok",
	}

	result, err := RunAgent(context.Background(), ByAgent(ag), map[string]any{"input": "test"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunAgent 失败: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

// TestRunWorkflow_正常调用 测试 RunWorkflow 正常调用路径
func TestRunWorkflow_正常调用(t *testing.T) {
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithName("test-wf"), schema.WithDescription("测试 Workflow")),
		result: map[string]any{"status": "completed"},
	}

	result, err := RunWorkflow(context.Background(), ByWorkflow(wf), map[string]any{"input": "test"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunWorkflow 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result 类型错误: %T", result)
	}
	if m["status"] != "completed" {
		t.Errorf("status = %v, want completed", m["status"])
	}
}

// TestRunWorkflow_执行错误 测试 RunWorkflow 执行出错
func TestRunWorkflow_执行错误(t *testing.T) {
	wf := &mockWorkflow{
		card: schema.NewWorkflowCard(schema.WithName("err-wf"), schema.WithDescription("错误 Workflow")),
		err:  context.DeadlineExceeded,
	}

	_, err := RunWorkflow(context.Background(), ByWorkflow(wf), nil, nil, nil, nil)
	if err == nil {
		t.Error("RunWorkflow 应返回错误")
	}
}

// TestRunWorkflow_带ModelCtx 测试 RunWorkflow 传入 modelCtx
func TestRunWorkflow_带ModelCtx(t *testing.T) {
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithName("wf-ctx"), schema.WithDescription("带上下文 Workflow")),
		result: "done",
	}

	result, err := RunWorkflow(context.Background(), ByWorkflow(wf), nil, nil, "model-context", nil)
	if err != nil {
		t.Fatalf("RunWorkflow 失败: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %v, want done", result)
	}
}

// TestRunAgentStreaming_正常调用 测试 RunAgentStreaming 正常流式调用
func TestRunAgentStreaming_正常调用(t *testing.T) {
	ch := makeStreamCh(
		stream.OutputSchema{Type: "output", Index: 0, Payload: "chunk1"},
		stream.OutputSchema{Type: "output", Index: 1, Payload: "chunk2", IsLastSchema: true},
	)

	ag := &mockAgent{
		card:     agentschema.NewAgentCard(schema.WithName("stream-agent"), schema.WithDescription("流式 Agent")),
		streamCh: ch,
	}
	sess := session.CreateAgentSession("stream-session", newTestAgentCard("stream-agent"), nil)

	outCh, err := RunAgentStreaming(context.Background(), ByAgent(ag), nil, sess, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunAgentStreaming 失败: %v", err)
	}

	// 读取所有流数据
	var chunks []stream.Schema
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("期望 2 个 chunk, 实际 %d", len(chunks))
	}
}

// TestRunAgentStreaming_无会话 测试 RunAgentStreaming 无会话时自动创建
func TestRunAgentStreaming_无会话(t *testing.T) {
	ch := makeStreamCh(
		stream.OutputSchema{Type: "output", Index: 0, IsLastSchema: true},
	)

	ag := &mockAgent{
		card:     agentschema.NewAgentCard(schema.WithName("stream-no-sess"), schema.WithDescription("流式无会话 Agent")),
		streamCh: ch,
	}

	outCh, err := RunAgentStreaming(context.Background(), ByAgent(ag), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunAgentStreaming 失败: %v", err)
	}

	var count int
	for range outCh {
		count++
	}
	if count != 1 {
		t.Errorf("期望 1 个 chunk, 实际 %d", count)
	}
}

// TestRunAgentStreaming_流式错误 测试 RunAgentStreaming 流式调用出错
func TestRunAgentStreaming_流式错误(t *testing.T) {
	ag := &mockAgent{
		card:      agentschema.NewAgentCard(schema.WithName("stream-err"), schema.WithDescription("流式错误 Agent")),
		streamErr: context.DeadlineExceeded,
	}
	sess := session.CreateAgentSession("err-session", newTestAgentCard("stream-err"), nil)

	_, err := RunAgentStreaming(context.Background(), ByAgent(ag), nil, sess, nil, nil, nil)
	if err == nil {
		t.Error("RunAgentStreaming 应返回错误")
	}
}

// TestRunAgentStreaming_准备错误 测试 RunAgentStreaming prepareAgent 失败
func TestRunAgentStreaming_准备错误(t *testing.T) {
	// 使用 ByAgentID 但 agent 不存在
	_, err := RunAgentStreaming(context.Background(), ByAgentID("nonexistent-agent"), nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("RunAgentStreaming 应返回错误（Agent不存在）")
	}
}

// TestRunWorkflowStreaming_正常调用 测试 RunWorkflowStreaming 正常流式调用
func TestRunWorkflowStreaming_正常调用(t *testing.T) {
	ch := makeStreamCh(
		stream.OutputSchema{Type: "output", Index: 0, Payload: "wf-chunk1"},
		stream.OutputSchema{Type: "output", Index: 1, Payload: "wf-chunk2", IsLastSchema: true},
	)

	wf := &mockWorkflow{
		card:     schema.NewWorkflowCard(schema.WithName("stream-wf"), schema.WithDescription("流式 Workflow")),
		streamCh: ch,
	}

	outCh, err := RunWorkflowStreaming(context.Background(), ByWorkflow(wf), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunWorkflowStreaming 失败: %v", err)
	}

	var chunks []stream.Schema
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("期望 2 个 chunk, 实际 %d", len(chunks))
	}
}

// TestRunWorkflowStreaming_流式错误 测试 RunWorkflowStreaming 流式调用出错
func TestRunWorkflowStreaming_流式错误(t *testing.T) {
	wf := &mockWorkflow{
		card:      schema.NewWorkflowCard(schema.WithName("stream-err-wf"), schema.WithDescription("流式错误 Workflow")),
		streamErr: context.DeadlineExceeded,
	}

	_, err := RunWorkflowStreaming(context.Background(), ByWorkflow(wf), nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("RunWorkflowStreaming 应返回错误")
	}
}

// TestRunWorkflowStreaming_准备错误 测试 RunWorkflowStreaming prepareWorkflow 失败
func TestRunWorkflowStreaming_准备错误(t *testing.T) {
	// 使用 ByWorkflowID 但 workflow 不存在
	_, err := RunWorkflowStreaming(context.Background(), ByWorkflowID("nonexistent-wf"), nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("RunWorkflowStreaming 应返回错误（Workflow不存在）")
	}
}

// TestRunWorkflowStreaming_带Session和ModelCtx 测试 RunWorkflowStreaming 传入 session 和 modelCtx
func TestRunWorkflowStreaming_带Session和ModelCtx(t *testing.T) {
	ch := makeStreamCh(
		stream.OutputSchema{Type: "output", Index: 0, IsLastSchema: true},
	)

	wf := &mockWorkflow{
		card:     schema.NewWorkflowCard(schema.WithName("wf-full"), schema.WithDescription("完整参数 Workflow")),
		streamCh: ch,
	}
	ws := session.NewWorkflowSession()

	outCh, err := RunWorkflowStreaming(context.Background(), ByWorkflow(wf), nil, ws, "model-ctx", nil, nil)
	if err != nil {
		t.Fatalf("RunWorkflowStreaming 失败: %v", err)
	}

	var count int
	for range outCh {
		count++
	}
	if count != 1 {
		t.Errorf("期望 1 个 chunk, 实际 %d", count)
	}
}

// TestStart_正常启动 测试 Start 正常启动
func TestStart_正常启动(t *testing.T) {
	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
}

// TestStop_正常停止 测试 Stop 正常停止
func TestStop_正常停止(t *testing.T) {
	if err := Stop(context.Background()); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
}

// TestGetResourceMgr 测试 GetResourceMgr 返回非 nil
func TestGetResourceMgr(t *testing.T) {
	mgr := GetResourceMgr()
	if mgr == nil {
		t.Error("GetResourceMgr 返回 nil, 期望非 nil")
	}
}

// TestGetCallbackFramework 测试 GetCallbackFramework 返回非 nil
func TestGetCallbackFramework(t *testing.T) {
	fw := GetCallbackFramework()
	if fw == nil {
		t.Error("GetCallbackFramework 返回 nil, 期望非 nil")
	}
}

// TestGetPubSub 测试 GetPubSub 返回非 nil
func TestGetPubSub(t *testing.T) {
	mq := GetPubSub()
	if mq == nil {
		t.Error("GetPubSub 返回 nil, 期望非 nil")
	}
}

// TestRelease_正常释放 测试 Release 正常释放
func TestRelease_正常释放(t *testing.T) {
	if err := Release(context.Background(), "test-session", false); err != nil {
		t.Fatalf("Release 失败: %v", err)
	}
}

// TestSpawnAgent_配置验证 测试 SpawnAgent 配置验证
func TestSpawnAgent_配置验证(t *testing.T) {
	_, err := SpawnAgent(
		context.Background(),
		spawn.SpawnAgentConfig{AgentKind: spawn.SpawnAgentKindClassAgent},
		nil, nil, nil,
	)
	// 在没有真实子进程的环境中，预期会因启动子进程失败而返回错误
	if err == nil {
		t.Log("SpawnAgent 在当前环境返回 nil（子进程可用）")
	} else {
		t.Logf("SpawnAgent 返回错误（预期在无子进程环境）: %v", err)
	}
}

// TestSpawnAgentStreaming_配置验证 测试 SpawnAgentStreaming 配置验证
func TestSpawnAgentStreaming_配置验证(t *testing.T) {
	_, err := SpawnAgentStreaming(
		context.Background(),
		spawn.SpawnAgentConfig{AgentKind: spawn.SpawnAgentKindClassAgent},
		nil, nil, nil, nil,
	)
	// 在没有真实子进程的环境中，预期会因启动子进程失败而返回错误
	if err == nil {
		t.Log("SpawnAgentStreaming 在当前环境返回 nil（子进程可用）")
	} else {
		t.Logf("SpawnAgentStreaming 返回错误（预期在无子进程环境）: %v", err)
	}
}

// TestSetConfig_GetConfig 测试 SetConfig 和 GetConfig 往返
func TestSetConfig_GetConfig(t *testing.T) {
	cfg := &config.RunnerConfig{
		DistributedMode: false,
		EnvPrefix:       "test-prefix",
		InstanceID:      "test-instance",
	}
	SetConfig(cfg)
	got := GetConfig()
	if got == nil {
		t.Fatal("GetConfig 返回 nil")
	}
	if got.EnvPrefix != "test-prefix" {
		t.Errorf("EnvPrefix = %q, want %q", got.EnvPrefix, "test-prefix")
	}
	if got.InstanceID != "test-instance" {
		t.Errorf("InstanceID = %q, want %q", got.InstanceID, "test-instance")
	}
}

// TestSetGlobalRunner 测试 SetGlobalRunner 替换全局 Runner
func TestSetGlobalRunner(t *testing.T) {
	// 保存原始 Runner
	runnerMu.RLock()
	orig := globalRunner
	runnerMu.RUnlock()

	// 替换为自定义 Runner
	custom := &Runner{
		runnerID:          "custom-runner",
		resourceMgr:       resources_manager.NewResourceMgr(),
		messageQueue:      message_queue.NewMessageQueueInMemory(10, 30*1e9),
		callbackFramework: cb.NewCallbackFramework(),
	}
	SetGlobalRunner(custom)

	// 验证替换成功
	runnerMu.RLock()
	cur := globalRunner
	runnerMu.RUnlock()
	if cur != custom {
		t.Error("SetGlobalRunner 未替换全局 Runner")
	}

	// 恢复原始 Runner
	SetGlobalRunner(orig)
}

// TestGetRunner_懒初始化 测试 getRunner 懒初始化行为
func TestGetRunner_懒初始化(t *testing.T) {
	// 保存并清除全局 Runner
	runnerMu.Lock()
	orig := globalRunner
	globalRunner = nil
	runnerMu.Unlock()
	// 重置 sync.Once 以触发重新初始化
	runnerOnce = sync.Once{}

	r := getRunner()
	if r == nil {
		t.Error("getRunner 应返回非 nil")
	}
	if r.runnerID != defaultRunnerID {
		t.Errorf("runnerID = %q, want %q", r.runnerID, defaultRunnerID)
	}

	// 恢复
	SetGlobalRunner(orig)
}

// TestIsRemoteAgent 测试 IsRemoteAgent 始终返回 false
func TestIsRemoteAgent(t *testing.T) {
	ag := &mockAgent{
		card: agentschema.NewAgentCard(schema.WithName("remote-check"), schema.WithDescription("远程判断")),
	}
	if IsRemoteAgent(ag) {
		t.Error("IsRemoteAgent 应返回 false（当前未实现远程Agent）")
	}
}

// TestGenerateWorkflowKey 测试 generateWorkflowKey
func TestGenerateWorkflowKey(t *testing.T) {
	// 带版本号
	key := generateWorkflowKey("my-wf", "1.0")
	if key != "my-wf:1.0" {
		t.Errorf("带版本号: got %q, want %q", key, "my-wf:1.0")
	}

	// 空版本号
	key = generateWorkflowKey("my-wf", "")
	if key != "my-wf" {
		t.Errorf("空版本号: got %q, want %q", key, "my-wf")
	}
}

// TestCreateWorkflowSession 测试 createWorkflowSession 不同 session 类型
func TestCreateWorkflowSession(t *testing.T) {
	r := newTestRunner()

	// nil → 新建空 WorkflowSession
	ws := r.createWorkflowSession(nil)
	if ws == nil {
		t.Error("nil 参数应返回非 nil WorkflowSession")
	}

	// string → 带指定 sessionID 的 WorkflowSession
	ws = r.createWorkflowSession("my-session-id")
	if ws == nil {
		t.Error("string 参数应返回非 nil WorkflowSession")
	}

	// *session.WorkflowSession → 原样返回
	originalWS := session.NewWorkflowSession()
	ws = r.createWorkflowSession(originalWS)
	if ws != originalWS {
		t.Error("*WorkflowSession 参数应返回原实例")
	}

	// *session.Session → 调用 CreateWorkflowSession
	agentSess := session.CreateAgentSession("test-sess", newTestAgentCard("test-agent"), nil)
	ws = r.createWorkflowSession(agentSess)
	if ws == nil {
		t.Error("*Session 参数应返回非 nil WorkflowSession")
	}

	// 其他类型（如 int） → 新建空 WorkflowSession
	ws = r.createWorkflowSession(42)
	if ws == nil {
		t.Error("未知类型参数应返回非 nil WorkflowSession")
	}
}

// TestCreateAgentSession 测试 createAgentSession
func TestCreateAgentSession(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card: agentschema.NewAgentCard(schema.WithName("sess-agent"), schema.WithDescription("会话 Agent")),
	}

	sess := r.createAgentSession(ag, "my-session")
	if sess == nil {
		t.Error("createAgentSession 应返回非 nil")
	}
}

// TestPrepareAgent_按实例有会话 测试 prepareAgent 按实例传入且已有会话
func TestPrepareAgent_按实例有会话(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("pa-inst-sess"), schema.WithDescription("按实例有会话")),
		result: "ok",
	}
	sess := session.CreateAgentSession("existing-session", newTestAgentCard("pa-inst-sess"), nil)

	agentInst, agentSess, err := r.prepareAgent(context.Background(), ByAgent(ag), nil, sess)
	if err != nil {
		t.Fatalf("prepareAgent 失败: %v", err)
	}
	if agentInst != ag {
		t.Error("应返回原始 Agent 实例")
	}
	if agentSess != sess {
		t.Error("应返回原始会话")
	}
}

// TestPrepareAgent_按ID有会话 测试 prepareAgent 按ID查找且已有会话
func TestPrepareAgent_按ID有会话(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithID("registered-agent"), schema.WithName("已注册 Agent"), schema.WithDescription("按ID有会话")),
		result: "ok",
	}

	// 注册 Agent 到资源管理器
	provider := func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return ag, nil
	}
	if err := r.resourceMgr.AddAgent(ag.Card(), provider); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	sess := session.CreateAgentSession("existing-session", newTestAgentCard("registered-agent"), nil)
	agentInst, agentSess, err := r.prepareAgent(context.Background(), ByAgentID("registered-agent"), nil, sess)
	if err != nil {
		t.Fatalf("prepareAgent 失败: %v", err)
	}
	if agentInst == nil {
		t.Error("应返回 Agent 实例")
	}
	if agentSess != sess {
		t.Error("应返回原始会话")
	}
}

// TestPrepareAgent_按ID无会话 测试 prepareAgent 按ID查找且无会话
func TestPrepareAgent_按ID无会话(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithID("byid-no-sess"), schema.WithName("按ID无会话"), schema.WithDescription("按ID无会话")),
		result: "ok",
	}

	provider := func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return ag, nil
	}
	if err := r.resourceMgr.AddAgent(ag.Card(), provider); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	agentInst, agentSess, err := r.prepareAgent(context.Background(), ByAgentID("byid-no-sess"), map[string]any{"input": "test"}, nil)
	if err != nil {
		t.Fatalf("prepareAgent 失败: %v", err)
	}
	if agentInst == nil {
		t.Error("应返回 Agent 实例")
	}
	if agentSess == nil {
		t.Error("应创建新会话")
	}
}

// TestPrepareAgent_按实例无会话 测试 prepareAgent 按实例传入且无会话
func TestPrepareAgent_按实例无会话(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("inst-no-sess"), schema.WithDescription("按实例无会话")),
		result: "ok",
	}

	agentInst, agentSess, err := r.prepareAgent(context.Background(), ByAgent(ag), map[string]any{"input": "test"}, nil)
	if err != nil {
		t.Fatalf("prepareAgent 失败: %v", err)
	}
	if agentInst != ag {
		t.Error("应返回原始 Agent 实例")
	}
	if agentSess == nil {
		t.Error("应创建新会话")
	}
}

// TestPrepareAgent_按ID不存在 测试 prepareAgent 按 ID 查找不存在的 Agent
func TestPrepareAgent_按ID不存在(t *testing.T) {
	r := newTestRunner()

	_, _, err := r.prepareAgent(context.Background(), ByAgentID("nonexistent"), nil, nil)
	if err == nil {
		t.Error("应返回错误（Agent不存在）")
	}
}

// TestPrepareAgent_按ID不存在有会话 测试 prepareAgent 按 ID 查找不存在但有会话
func TestPrepareAgent_按ID不存在有会话(t *testing.T) {
	r := newTestRunner()
	sess := session.CreateAgentSession("ghost-session", newTestAgentCard("ghost-agent"), nil)

	_, _, err := r.prepareAgent(context.Background(), ByAgentID("ghost-agent"), nil, sess)
	if err == nil {
		t.Error("应返回错误（Agent不存在）")
	}
}

// TestPrepareAgent_自定义会话ID 测试 prepareAgent 从 inputs 提取 conversation_id
func TestPrepareAgent_自定义会话ID(t *testing.T) {
	r := newTestRunner()
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("custom-conv"), schema.WithDescription("自定义会话ID")),
		result: "ok",
	}

	inputs := map[string]any{
		"conversation_id": "custom-conv-123",
	}
	_, agentSess, err := r.prepareAgent(context.Background(), ByAgent(ag), inputs, nil)
	if err != nil {
		t.Fatalf("prepareAgent 失败: %v", err)
	}
	if agentSess == nil {
		t.Fatal("应创建新会话")
	}
	// 验证 sessionID 被正确传递
	sessFacade, ok := agentSess.(*session.Session)
	if !ok {
		t.Fatalf("会话类型错误: %T", agentSess)
	}
	if sessFacade.GetSessionID() != "custom-conv-123" {
		t.Errorf("sessionID = %q, want %q", sessFacade.GetSessionID(), "custom-conv-123")
	}
}

// TestPrepareWorkflow_按实例 测试 prepareWorkflow 按实例传入
func TestPrepareWorkflow_按实例(t *testing.T) {
	r := newTestRunner()
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithID("wf-inst"), schema.WithName("按实例 Workflow"), schema.WithDescription("按实例")),
		result: "ok",
	}

	workflowInst, workflowSess, err := r.prepareWorkflow(context.Background(), ByWorkflow(wf), nil)
	if err != nil {
		t.Fatalf("prepareWorkflow 失败: %v", err)
	}
	if workflowInst != wf {
		t.Error("应返回原始 Workflow 实例")
	}
	if workflowSess == nil {
		t.Error("应创建 WorkflowSession")
	}
}

// TestPrepareWorkflow_按ID 测试 prepareWorkflow 按 ID 查找
func TestPrepareWorkflow_按ID(t *testing.T) {
	r := newTestRunner()
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithID("wf-byid"), schema.WithName("按ID Workflow"), schema.WithDescription("按ID")),
		result: "ok",
	}

	provider := func(ctx context.Context, card *schema.WorkflowCard) (interfaces.Workflow, error) {
		return wf, nil
	}
	if err := r.resourceMgr.AddWorkflow(wf.Card(), provider); err != nil {
		t.Fatalf("AddWorkflow 失败: %v", err)
	}

	workflowInst, workflowSess, err := r.prepareWorkflow(context.Background(), ByWorkflowID("wf-byid"), nil)
	if err != nil {
		t.Fatalf("prepareWorkflow 失败: %v", err)
	}
	if workflowInst == nil {
		t.Error("应返回 Workflow 实例")
	}
	if workflowSess == nil {
		t.Error("应创建 WorkflowSession")
	}
}

// TestPrepareWorkflow_按ID不存在 测试 prepareWorkflow 按 ID 查找不存在的 Workflow
func TestPrepareWorkflow_按ID不存在(t *testing.T) {
	r := newTestRunner()

	_, _, err := r.prepareWorkflow(context.Background(), ByWorkflowID("nonexistent-wf"), nil)
	if err == nil {
		t.Error("应返回错误（Workflow不存在）")
	}
}

// TestPrepareWorkflow_按实例带Card 测试 prepareWorkflow 按实例传入且有 Card 含版本号
func TestPrepareWorkflow_按实例带Card(t *testing.T) {
	r := newTestRunner()
	wfCard := schema.NewWorkflowCard(schema.WithID("wf-card"), schema.WithName("带Card Workflow"), schema.WithDescription("带Card"))
	wfCard.Version = "2.0"
	wf := &mockWorkflow{
		card:   wfCard,
		result: "ok",
	}

	workflowInst, _, err := r.prepareWorkflow(context.Background(), ByWorkflow(wf), nil)
	if err != nil {
		t.Fatalf("prepareWorkflow 失败: %v", err)
	}
	if workflowInst != wf {
		t.Error("应返回原始 Workflow 实例")
	}
}

// TestPrepareWorkflow_带WorkflowSession 测试 prepareWorkflow 传入 WorkflowSession
func TestPrepareWorkflow_带WorkflowSession(t *testing.T) {
	r := newTestRunner()
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithName("wf-ws"), schema.WithDescription("带 WorkflowSession")),
		result: "ok",
	}
	existingWS := session.NewWorkflowSession()

	_, workflowSess, err := r.prepareWorkflow(context.Background(), ByWorkflow(wf), existingWS)
	if err != nil {
		t.Fatalf("prepareWorkflow 失败: %v", err)
	}
	if workflowSess != existingWS {
		t.Error("应返回原始 WorkflowSession")
	}
}

// TestPrepareWorkflow_带字符串会话 测试 prepareWorkflow 传入字符串 session
func TestPrepareWorkflow_带字符串会话(t *testing.T) {
	r := newTestRunner()
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithName("wf-str"), schema.WithDescription("字符串会话")),
		result: "ok",
	}

	_, workflowSess, err := r.prepareWorkflow(context.Background(), ByWorkflow(wf), "my-session-id")
	if err != nil {
		t.Fatalf("prepareWorkflow 失败: %v", err)
	}
	if workflowSess == nil {
		t.Error("应创建 WorkflowSession")
	}
}
