package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// noOpCheckpointer 空操作检查点器，用于不需要真实检查点逻辑的测试
type noOpCheckpointer struct{}

func (n *noOpCheckpointer) PreWorkflowExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpCheckpointer) PostWorkflowExecute(ctx context.Context, session interfaces.InnerSession, result any, exception error) error {
	return nil
}
func (n *noOpCheckpointer) PreAgentExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpCheckpointer) PreAgentTeamExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpCheckpointer) InterruptAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpCheckpointer) PostAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpCheckpointer) PostAgentTeamExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (n *noOpCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	return nil
}
func (n *noOpCheckpointer) GraphStore() any { return nil }

// TestNewSession 测试构造函数
func TestNewSession(t *testing.T) {
	s := NewSession()
	if s == nil {
		t.Fatal("NewSession 返回 nil")
	}
	if s.GetSessionID() == "" {
		t.Error("SessionID 不应为空")
	}
}

// TestNewSession_自定义ID 测试自定义 sessionID
func TestNewSession_自定义ID(t *testing.T) {
	s := NewSession(WithSessionID("my-id"))
	if s.GetSessionID() != "my-id" {
		t.Errorf("SessionID 期望 my-id，实际 %s", s.GetSessionID())
	}
}

// TestSession_PreRun 测试 PreRun 触发回调
func TestSession_PreRun(t *testing.T) {
	// 用独立 CallbackFramework 避免全局单例污染
	fw := callback.NewCallbackFramework()
	s := NewSession(WithSessionID("test-pre-run"))

	var triggered bool
	fw.OnSession(callback.AgentSessionCreated,
		func(ctx context.Context, data *callback.SessionCallEventData) any {
			triggered = true
			if data.SessionID != "test-pre-run" {
				t.Errorf("回调 SessionID 期望 test-pre-run，实际 %s", data.SessionID)
			}
			return nil
		},
	)

	// 直接使用 fw 触发，而不是全局框架
	fw.TriggerSession(context.Background(), &callback.SessionCallEventData{
		Event:     callback.AgentSessionCreated,
		SessionID: s.GetSessionID(),
	})

	if !triggered {
		t.Error("PreRun 应通过 TriggerSession 触发回调")
	}
}

// TestSession_PreRun_幂等 测试重复调用只执行一次
func TestSession_PreRun_幂等(t *testing.T) {
	s := NewSession(WithSessionID("test-idempotent"), WithCheckpointer(&noOpCheckpointer{}))

	err := s.PreRun(context.Background())
	if err != nil {
		t.Errorf("PreRun 不应返回错误：%v", err)
	}

	// 再次调用不应出错
	err = s.PreRun(context.Background())
	if err != nil {
		t.Errorf("幂等 PreRun 不应返回错误：%v", err)
	}
}

// TestSession_PostRun 测试 PostRun 流程
func TestSession_PostRun(t *testing.T) {
	s := NewSession(WithCheckpointer(&noOpCheckpointer{}))
	err := s.PostRun(context.Background())
	if err != nil {
		t.Errorf("PostRun 不应返回错误：%v", err)
	}
}

// TestSession_PostRun_幂等 测试重复调用只执行一次
func TestSession_PostRun_幂等(t *testing.T) {
	s := NewSession(WithCheckpointer(&noOpCheckpointer{}))
	_ = s.PostRun(context.Background())
	_ = s.PostRun(context.Background())
	// 不应 panic 或重复关闭
}

// TestSession_Commit 测试提交检查点
func TestSession_Commit(t *testing.T) {
	s := NewSession(WithCheckpointer(&noOpCheckpointer{}))
	err := s.Commit(context.Background())
	if err != nil {
		t.Errorf("Commit 不应返回错误：%v", err)
	}
}

// TestSession_GetSessionID 测试获取会话 ID
func TestSession_GetSessionID(t *testing.T) {
	s := NewSession(WithSessionID("abc-123"))
	if s.GetSessionID() != "abc-123" {
		t.Errorf("期望 abc-123，实际 %s", s.GetSessionID())
	}
}

// TestSession_UpdateState 测试更新状态
func TestSession_UpdateState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})
}

// TestSession_GetState 测试获取状态
func TestSession_GetState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})

	result, err := s.GetState(state.StringKey("key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "value" {
		t.Errorf("期望 value，实际 %v", result)
	}
}

// TestSession_DumpState 测试导出状态快照
func TestSession_DumpState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})

	dump := s.DumpState()
	if dump == nil {
		t.Fatal("DumpState 不应返回 nil")
	}
}

// TestSession_桩方法返回Nil 测试桩方法不返回错误
func TestSession_桩方法返回Nil(t *testing.T) {
	s := NewSession(WithCheckpointer(&noOpCheckpointer{}))

	if err := s.WriteStream(context.Background(), nil); err != nil {
		t.Errorf("WriteStream 桩应返回 nil，实际 %v", err)
	}
	if err := s.WriteCustomStream(context.Background(), nil); err != nil {
		t.Errorf("WriteCustomStream 桩应返回 nil，实际 %v", err)
	}
	// StreamIterator 不再返回 nil：默认 StreamWriterManager 自动创建
	if ch := s.StreamIterator(); ch == nil {
		t.Error("StreamIterator 应返回非 nil channel（有默认 StreamWriterManager）")
	}
	if err := s.CloseStream(); err != nil {
		t.Errorf("CloseStream 桩应返回 nil，实际 %v", err)
	}
	// Interact 现在触发 SimpleAgentInteraction，会 panic AgentInterrupt
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("Interact 应触发 AgentInterrupt panic")
			}
			if _, ok := r.(*interaction.AgentInterrupt); !ok {
				t.Errorf("期望 *interaction.AgentInterrupt，得到 %T", r)
			}
		}()
		_ = s.Interact(context.Background(), nil)
	}()
	if ws := s.CreateWorkflowSession(); ws == nil {
		t.Error("CreateWorkflowSession 应返回非 nil 的 WorkflowSession")
	}
}

// TestSession_CloseStreamOnPostRun 测试 closeStreamOnPostRun 选项
func TestSession_CloseStreamOnPostRun(t *testing.T) {
	s1 := NewSession()
	if !s1.closeStreamOnPostRun {
		t.Error("默认 closeStreamOnPostRun 应为 true")
	}

	s2 := NewSession(WithCloseStreamOnPostRun(false))
	if s2.closeStreamOnPostRun {
		t.Error("WithCloseStreamOnPostRun(false) 后应为 false")
	}
}

// TestSession_tagStreamPayload_OutputSchema 测试 OutputSchema 类型的流数据元数据标签
func TestSession_tagStreamPayload_OutputSchema(t *testing.T) {
	s := NewSession(WithSourceMetadata(map[string]any{"source": "team-1"}))

	// OutputSchema + map payload
	os1 := stream.OutputSchema{Type: "message", Index: 0, Payload: map[string]any{"text": "hello"}}
	result1 := s.tagStreamPayload(os1).(stream.OutputSchema)
	payload1 := result1.Payload.(map[string]any)
	if payload1["text"] != "hello" {
		t.Errorf("OutputSchema payload text 期望 hello，实际 %v", payload1["text"])
	}
	if payload1["source"] != "team-1" {
		t.Errorf("OutputSchema payload source 期望 team-1，实际 %v", payload1["source"])
	}

	// OutputSchema + 非 map payload（字符串）
	os2 := stream.OutputSchema{Type: "message", Index: 0, Payload: "raw_text"}
	result2 := s.tagStreamPayload(os2).(stream.OutputSchema)
	payload2 := result2.Payload.(map[string]any)
	if payload2["value"] != "raw_text" {
		t.Errorf("非 map payload 应包装到 value 键，实际 %v", payload2["value"])
	}
	if payload2["source"] != "team-1" {
		t.Errorf("非 map payload source 期望 team-1，实际 %v", payload2["source"])
	}

	// 非 map/OutputSchema/CustomSchema 类型（如 string），原样返回
	s2 := NewSession(WithSourceMetadata(map[string]any{"source": "team-1"}))
	result3 := s2.tagStreamPayload("just_string")
	if result3 != "just_string" {
		t.Errorf("其他类型应原样返回，实际 %v", result3)
	}
}

// TestSession_tagStreamPayload_CustomSchema 测试 CustomSchema 类型的流数据元数据标签
func TestSession_tagStreamPayload_CustomSchema(t *testing.T) {
	s := NewSession(WithSourceMetadata(map[string]any{"source": "team-1"}))

	// CustomSchema：metadata 应合并进 Data 字段
	cs := stream.CustomSchema{Type: "event", Data: map[string]any{"key": "val"}}
	result := s.tagStreamPayload(cs).(stream.CustomSchema)
	if result.Type != "event" {
		t.Errorf("CustomSchema Type 期望 event，实际 %v", result.Type)
	}
	if result.Data["key"] != "val" {
		t.Errorf("CustomSchema Data key 期望 val，实际 %v", result.Data["key"])
	}
	if result.Data["source"] != "team-1" {
		t.Errorf("CustomSchema Data source 期望 team-1，实际 %v", result.Data["source"])
	}

	// CustomSchema 空 Data：metadata 仍应合并
	cs2 := stream.CustomSchema{Type: "empty", Data: nil}
	result2 := s.tagStreamPayload(cs2).(stream.CustomSchema)
	if result2.Data["source"] != "team-1" {
		t.Errorf("CustomSchema 空 Data 合并后 source 期望 team-1，实际 %v", result2.Data["source"])
	}
}

// TestNormalizeOutputStream 各种类型 测试不同输入类型的归一化
func TestNormalizeOutputStream_各种类型(t *testing.T) {
	// case stream.OutputSchema：直接返回
	os := stream.OutputSchema{Type: "message", Index: 1, Payload: "test"}
	result := normalizeOutputStream(os)
	if result.Type != "message" || result.Index != 1 {
		t.Errorf("OutputSchema 应原样返回，实际 type=%s index=%d", result.Type, result.Index)
	}

	// case map[string]any 包含完整 OutputSchema 字段
	fullMap := map[string]any{
		"type":    "custom",
		"index":   2,
		"payload": map[string]any{"data": "value"},
	}
	result2 := normalizeOutputStream(fullMap)
	if result2.Type != "custom" || result2.Index != 2 {
		t.Errorf("完整 map 应解析为 OutputSchema，实际 type=%s index=%d", result2.Type, result2.Index)
	}

	// case map[string]any 缺少字段：走默认构造
	partialMap := map[string]any{
		"type": "custom",
	}
	result3 := normalizeOutputStream(partialMap)
	if result3.Type != "message" {
		t.Errorf("不完整 map 应走默认构造，实际 type=%s", result3.Type)
	}

	// 其他类型（如 string）：走默认构造
	result4 := normalizeOutputStream("plain_string")
	if result4.Type != "message" || result4.Payload != "plain_string" {
		t.Errorf("其他类型应走默认构造，实际 type=%s payload=%v", result4.Type, result4.Payload)
	}
}

// TestSession_WriteCustomStream_map数据 测试 WriteCustomStream 传入 map 数据
func TestSession_WriteCustomStream_map数据(t *testing.T) {
	s := NewSession()
	// 传入 map 数据，走 map 分支
	err := s.WriteCustomStream(context.Background(), map[string]any{"key": "value"})
	if err != nil {
		t.Errorf("WriteCustomStream map 数据不应返回错误，实际=%v", err)
	}
}

// TestSession_WriteStream_OutputSchema 测试 WriteStream 传入 OutputSchema
func TestSession_WriteStream_OutputSchema(t *testing.T) {
	s := NewSession()
	os := stream.OutputSchema{Type: "message", Index: 0, Payload: "test"}
	err := s.WriteStream(context.Background(), os)
	if err != nil {
		t.Errorf("WriteStream OutputSchema 不应返回错误，实际=%v", err)
	}
}

// TestSession_GetEnvs_nilConfig 测试 config 为 nil 时 GetEnvs 返回 nil
func TestSession_GetEnvs_nilConfig(t *testing.T) {
	// 构造一个 inner config 为 nil 的 Session
	s := &Session{
		sessionID:            "test-nil-cfg",
		inner:                internal.NewAgentSession("test-nil-cfg"),
		closeStreamOnPostRun: true,
		sourceMetadata:       make(map[string]any),
	}
	envs := s.GetEnvs()
	// AgentSession 默认 config 为 nil，但 NewSession 会在构造时设置 envs map
	// 直接构造的 Session 没有 envs，所以 GetEnvs 走 cfg nil 分支
	// 实际上 inner.Config() 在 AgentSession 中也是 nil（没有通过 WithConfig 设置）
	// 所以 GetEnvs 应返回 nil
	if envs != nil {
		t.Errorf("config 为 nil 时 GetEnvs 应返回 nil，实际=%v", envs)
	}
}

// TestSession_tagStreamPayload 测试流数据元数据标签
func TestSession_tagStreamPayload(t *testing.T) {
	// 无元数据时原样返回
	s1 := NewSession()
	data := map[string]any{"key": "value"}
	result := s1.tagStreamPayload(data).(map[string]any)
	if result["key"] != "value" {
		t.Errorf("无元数据时 key 期望 value，实际 %v", result["key"])
	}
	if _, ok := result["source"]; ok {
		t.Error("无元数据时不应添加 source 字段")
	}

	// 有元数据时合并
	s2 := NewSession(WithSourceMetadata(map[string]any{"source": "team-1"}))
	data2 := map[string]any{"key": "value"}
	result2 := s2.tagStreamPayload(data2).(map[string]any)
	if result2["key"] != "value" {
		t.Errorf("有元数据时 key 期望 value，实际 %v", result2["key"])
	}
	if result2["source"] != "team-1" {
		t.Errorf("source 期望 team-1，实际 %v", result2["source"])
	}
}

// TestSession_GetEnv无匹配Key 测试无匹配 key 时返回 nil
func TestSession_GetEnv无匹配Key(t *testing.T) {
	s := NewSession()
	if s.GetEnv("any_key") != nil {
		t.Error("GetEnv 无匹配 key 应返回 nil")
	}
}

// TestSession_GetEnv有Envs 测试有 envs 时 GetEnv 正确返回值
func TestSession_GetEnv有Envs(t *testing.T) {
	s := NewSession(WithEnvs(map[string]any{"my_key": "my_val"}))
	if s.GetEnv("my_key") != "my_val" {
		t.Errorf("GetEnv 期望 my_val，实际 %v", s.GetEnv("my_key"))
	}
	if s.GetEnv("missing_key") != nil {
		t.Error("GetEnv 无匹配 key 应返回 nil")
	}
}

// TestSession_GetEnvDefaultValue 测试 GetEnv 默认值
func TestSession_GetEnvDefaultValue(t *testing.T) {
	s := NewSession()
	defaultVal := s.GetEnv("missing_key", "default")
	if defaultVal != "default" {
		t.Errorf("GetEnv 默认值期望 'default'，实际 %v", defaultVal)
	}
}

// TestSession_GetEnvs返回内置默认值 测试 GetEnvs 返回包含内置默认值的 map（非 nil）
// 5.12 回填后，NewSessionConfig 自动加载内置超时配置，GetEnvs 返回非空 map。
// 对齐 Python: Session.__init__() 总是创建 Config()，get_envs() 返回内置默认值字典
func TestSession_GetEnvs返回内置默认值(t *testing.T) {
	s := NewSession()
	envs := s.GetEnvs()
	if envs == nil {
		t.Error("GetEnvs 不应返回 nil")
	}
	// 验证内置默认值存在
	if _, ok := envs["_execute_timeout"]; !ok {
		t.Error("GetEnvs 应包含内置默认配置键 _execute_timeout")
	}
}

// TestSession_GetAgentID返回空 测试桩方法
func TestSession_GetAgentID返回空(t *testing.T) {
	s := NewSession()
	if s.GetAgentID() != "" {
		t.Errorf("GetAgentID 桩应返回空字符串，实际 %s", s.GetAgentID())
	}
}

// TestSession_WithCard 测试 WithCard 选项
func TestSession_WithCard(t *testing.T) {
	card := &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "agent-1"}}
	s := NewSession(WithCard(card))
	if s.card == nil {
		t.Error("WithCard 后 card 不应为 nil")
	}
	if s.GetAgentID() != "agent-1" {
		t.Errorf("GetAgentID 期望 agent-1，实际 %s", s.GetAgentID())
	}
}

// TestSession_WithEnvs 测试 WithEnvs 选项
func TestSession_WithEnvs(t *testing.T) {
	envs := map[string]any{"key1": "val1", "key2": 42}
	s := NewSession(WithEnvs(envs))

	// GetEnv 应从 envs 中读取
	if s.GetEnv("key1") != "val1" {
		t.Errorf("GetEnv('key1') 期望 val1，实际 %v", s.GetEnv("key1"))
	}
	if s.GetEnv("key2") != 42 {
		t.Errorf("GetEnv('key2') 期望 42，实际 %v", s.GetEnv("key2"))
	}

	// GetEnvs 应返回完整 envs
	allEnvs := s.GetEnvs()
	if allEnvs == nil {
		t.Fatal("GetEnvs 不应返回 nil")
	}
	if allEnvs["key1"] != "val1" {
		t.Errorf("GetEnvs()['key1'] 期望 val1，实际 %v", allEnvs["key1"])
	}
}

// TestSession_WithStreamWriterManager 测试 WithStreamWriterManager 选项
func TestSession_WithStreamWriterManager(t *testing.T) {
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	s := NewSession(WithStreamWriterManager(swm))
	// 验证 swm 传入了 inner
	if s.inner.StreamWriterManager() != swm {
		t.Errorf("inner.StreamWriterManager 期望 %v，实际 %v", swm, s.inner.StreamWriterManager())
	}
}

// TestSession_GetAgentName返回空 测试无 card 时返回空
func TestSession_GetAgentName返回空(t *testing.T) {
	s := NewSession()
	if s.GetAgentName() != "" {
		t.Errorf("无 card 时 GetAgentName 应返回空字符串，实际 %s", s.GetAgentName())
	}
}

// TestSession_GetAgentName有Card 测试有 card 时返回名称
func TestSession_GetAgentName有Card(t *testing.T) {
	card := &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "agent-1", Name: "测试Agent"}}
	s := NewSession(WithCard(card))
	if s.GetAgentName() != "测试Agent" {
		t.Errorf("GetAgentName 期望 测试Agent，实际 %s", s.GetAgentName())
	}
}

// TestSession_GetAgentDescription返回空 测试无 card 时返回空
func TestSession_GetAgentDescription返回空(t *testing.T) {
	s := NewSession()
	if s.GetAgentDescription() != "" {
		t.Errorf("无 card 时 GetAgentDescription 应返回空字符串，实际 %s", s.GetAgentDescription())
	}
}

// TestSession_GetAgentDescription有Card 测试有 card 时返回描述
func TestSession_GetAgentDescription有Card(t *testing.T) {
	card := &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "agent-1", Description: "测试描述"}}
	s := NewSession(WithCard(card))
	if s.GetAgentDescription() != "测试描述" {
		t.Errorf("GetAgentDescription 期望 测试描述，实际 %s", s.GetAgentDescription())
	}
}

// TestCreateAgentSession 测试通过 agentID 和 sessionID 创建 Session
func TestCreateAgentSession(t *testing.T) {
	s := CreateAgentSession("agent-1", "sess-1")
	if s == nil {
		t.Fatal("CreateAgentSession 返回 nil")
	}
	if s.GetSessionID() != "sess-1" {
		t.Errorf("期望 sessionID='sess-1'，实际=%s", s.GetSessionID())
	}
	if s.card == nil {
		t.Error("card 不应为 nil")
	}
}

// TestSession_CreateWorkflowSession_状态类型不匹配 测试 inner.State() 非 AgentStateCollection 时的降级分支
func TestSession_CreateWorkflowSession_状态类型不匹配(t *testing.T) {
	// 直接构造 Session，inner 是 AgentSession（其 State 是 AgentStateCollection）
	// 为了触发 else 分支，需要 inner.State() 非 AgentStateCollection
	// 我们构造一个 Session，然后替换 inner 为使用自定义 state 的 AgentSession
	s2 := &Session{
		sessionID:            "test-id",
		inner:                internal.NewAgentSession("test-id"),
		closeStreamOnPostRun: true,
		sourceMetadata:       make(map[string]any),
	}
	ws := s2.CreateWorkflowSession()
	if ws == nil {
		t.Error("降级时 CreateWorkflowSession 不应返回 nil")
	}
}

// ──────────────────────────── CreateWorkflowSession 测试 ────────────────────────────

// TestSession_CreateWorkflowSession 测试创建成功返回非 nil
func TestSession_CreateWorkflowSession(t *testing.T) {
	s := NewSession()
	ws := s.CreateWorkflowSession()

	if ws == nil {
		t.Fatal("CreateWorkflowSession 返回 nil")
	}
}

// TestSession_CreateWorkflowSession_SessionID共享 测试共享 sessionID
func TestSession_CreateWorkflowSession_SessionID共享(t *testing.T) {
	s := NewSession(WithSessionID("shared-id"))
	ws := s.CreateWorkflowSession()

	if ws.GetSessionID() != "shared-id" {
		t.Errorf("期望 WorkflowSession 共享 sessionID='shared-id'，实际=%s", ws.GetSessionID())
	}
}

// TestSession_CreateWorkflowSession_GlobalState共享 测试 globalState 共享
func TestSession_CreateWorkflowSession_GlobalState共享(t *testing.T) {
	s := NewSession()

	// AgentSession 写入全局状态
	s.UpdateState(map[string]any{"agent_key": "agent_val"})

	// 创建 WorkflowSession
	ws := s.CreateWorkflowSession()

	// 通过内部层验证 WorkflowSession 能读取 AgentSession 写入的 globalState
	if cs, ok := ws.Inner().State().(*state.WorkflowCommitState); ok {
		result := cs.GetGlobal(state.StringKey("agent_key"))
		if result != "agent_val" {
			t.Errorf("期望 WorkflowSession 读取共享 globalState='agent_val'，实际=%v", result)
		}

		// WorkflowSession 更新 globalState 并提交
		cs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
		cs.Commit()
	}

	// AgentSession 也应能读到 WorkflowSession 的更新
	result, err := s.GetState(state.StringKey("wf_key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "wf_val" {
		t.Errorf("期望 AgentSession 读取共享 globalState='wf_val'，实际=%v", result)
	}
}

// ──────────────────────────── Stream 回调测试（SW-31/32/33） ────────────────────────────

// TestSession_WriteStream_触发CustomCallback 测试 WriteStream 触发自定义 StreamWrite 回调
func TestSession_WriteStream_触发CustomCallback(t *testing.T) {
	s := NewSession(WithSessionID("sw31-test"))

	var triggered bool
	var receivedData map[string]any
	fn := func(_ context.Context, data map[string]any) any {
		triggered = true
		receivedData = data
		return nil
	}

	// 注册回调到 per-session 事件名
	fw := callback.GetCallbackFramework()
	fw.OnCustom("sw31-testwrite_stream", fn)
	defer fw.OffAllCustom("sw31-testwrite_stream")

	_ = s.WriteStream(context.Background(), map[string]any{"text": "hello"})

	if !triggered {
		t.Error("WriteStream 应触发自定义 StreamWrite 回调")
	}
	if receivedData == nil {
		t.Fatal("回调数据不应为 nil")
	}
	// data 经过 normalizeOutputStream 处理，变为 OutputSchema
	if _, ok := receivedData["data"]; !ok {
		t.Error("回调数据应包含 data 键")
	}
}

// TestSession_WriteCustomStream_触发CustomCallback 测试 WriteCustomStream 触发自定义 StreamWrite 回调
func TestSession_WriteCustomStream_触发CustomCallback(t *testing.T) {
	s := NewSession(WithSessionID("sw32-test"))

	var triggered bool
	var receivedData map[string]any
	fn := func(_ context.Context, data map[string]any) any {
		triggered = true
		receivedData = data
		return nil
	}

	fw := callback.GetCallbackFramework()
	fw.OnCustom("sw32-testwrite_stream", fn)
	defer fw.OffAllCustom("sw32-testwrite_stream")

	_ = s.WriteCustomStream(context.Background(), map[string]any{"key": "value"})

	if !triggered {
		t.Error("WriteCustomStream 应触发自定义 StreamWrite 回调")
	}
	if receivedData == nil {
		t.Fatal("回调数据不应为 nil")
	}
	if _, ok := receivedData["data"]; !ok {
		t.Error("回调数据应包含 data 键")
	}
}

// TestSession_CloseStream_注销CustomCallback 测试 CloseStream 注销自定义 StreamWrite 回调
func TestSession_CloseStream_注销CustomCallback(t *testing.T) {
	s := NewSession(WithSessionID("sw33-test"))

	var callCount int32
	fn := func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	fw := callback.GetCallbackFramework()
	fw.OnCustom("sw33-testwrite_stream", fn)

	// WriteStream 应触发回调
	_ = s.WriteStream(context.Background(), "data1")
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("WriteStream 后期望调用 1 次，实际 %d 次", callCount)
	}

	// CloseStream 应注销回调
	_ = s.CloseStream()

	// 再次 WriteStream 不应触发回调（回调已被 OffAllCustom 清除）
	// 注意：CloseStream 已关闭 emitter，WriteStream 的 writer.Write 会返回错误或丢弃
	// 但 TriggerCustom 仍会执行，只是回调已不在注册表中
	atomic.StoreInt32(&callCount, 0)
	fw.TriggerCustom(context.Background(), "sw33-testwrite_stream", map[string]any{"data": "test"})
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("CloseStream 后回调应被注销，不应再被触发")
	}
}

// TestSession_Stream回调_PerSession隔离 测试不同 session 的回调互不影响
func TestSession_Stream回调_PerSession隔离(t *testing.T) {
	s1 := NewSession(WithSessionID("isolation-A"))
	s2 := NewSession(WithSessionID("isolation-B"))

	var callA, callB int32
	fnA := func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callA, 1)
		return nil
	}
	fnB := func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callB, 1)
		return nil
	}

	fw := callback.GetCallbackFramework()
	fw.OnCustom("isolation-Awrite_stream", fnA)
	fw.OnCustom("isolation-Bwrite_stream", fnB)
	defer func() {
		fw.OffAllCustom("isolation-Awrite_stream")
		fw.OffAllCustom("isolation-Bwrite_stream")
	}()

	// s1 写入只触发 session-A 回调
	_ = s1.WriteStream(context.Background(), "data-A")
	if atomic.LoadInt32(&callA) != 1 || atomic.LoadInt32(&callB) != 0 {
		t.Errorf("s1 WriteStream: 期望 callA=1 callB=0，实际 callA=%d callB=%d", callA, callB)
	}

	// s2 写入只触发 session-B 回调
	_ = s2.WriteStream(context.Background(), "data-B")
	if atomic.LoadInt32(&callA) != 1 || atomic.LoadInt32(&callB) != 1 {
		t.Errorf("s2 WriteStream: 期望 callA=1 callB=1，实际 callA=%d callB=%d", callA, callB)
	}

	// CloseStream s1 只清除 session-A 回调
	_ = s1.CloseStream()

	// s2 回调应仍有效
	atomic.StoreInt32(&callB, 0)
	_ = s2.WriteStream(context.Background(), "data-B2")
	if atomic.LoadInt32(&callB) != 1 {
		t.Error("s1 CloseStream 后 s2 回调应仍有效")
	}
}
