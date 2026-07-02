package session

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// noOpTeamCheckpointer 空操作检查点器，用于 AgentTeamSession 测试
type noOpTeamCheckpointer struct{}

func (n *noOpTeamCheckpointer) PreWorkflowExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpTeamCheckpointer) PostWorkflowExecute(ctx context.Context, session interfaces.InnerSession, result any, exception error) error {
	return nil
}
func (n *noOpTeamCheckpointer) PreAgentExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpTeamCheckpointer) PreAgentTeamExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	return nil
}
func (n *noOpTeamCheckpointer) InterruptAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpTeamCheckpointer) PostAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpTeamCheckpointer) PostAgentTeamExecute(ctx context.Context, session interfaces.InnerSession) error {
	return nil
}
func (n *noOpTeamCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (n *noOpTeamCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	return nil
}
func (n *noOpTeamCheckpointer) GraphStore() any { return nil }

// TestNewAgentTeamSession 测试构造函数
func TestNewAgentTeamSession(t *testing.T) {
	s := NewAgentTeamSession()
	if s == nil {
		t.Fatal("NewAgentTeamSession 返回 nil")
	}
	if s.GetSessionID() == "" {
		t.Error("SessionID 不应为空")
	}
	if s.GetTeamID() != "agent_team" {
		t.Errorf("默认 TeamID 期望 agent_team，实际 %s", s.GetTeamID())
	}
}

// TestNewAgentTeamSession_自定义ID 测试自定义 sessionID 和 teamID
func TestNewAgentTeamSession_自定义ID(t *testing.T) {
	s := NewAgentTeamSession(
		WithAgentTeamSessionID("my-session"),
		WithAgentTeamTeamID("my-team"),
	)
	if s.GetSessionID() != "my-session" {
		t.Errorf("SessionID 期望 my-session，实际 %s", s.GetSessionID())
	}
	if s.GetTeamID() != "my-team" {
		t.Errorf("TeamID 期望 my-team，实际 %s", s.GetTeamID())
	}
}

// TestCreateAgentTeamSession 测试工厂函数
func TestCreateAgentTeamSession(t *testing.T) {
	envs := map[string]any{"key1": "val1"}
	s := CreateAgentTeamSession("factory-session", envs, "factory-team")
	if s == nil {
		t.Fatal("CreateAgentTeamSession 返回 nil")
	}
	if s.GetSessionID() != "factory-session" {
		t.Errorf("SessionID 期望 factory-session，实际 %s", s.GetSessionID())
	}
	if s.GetTeamID() != "factory-team" {
		t.Errorf("TeamID 期望 factory-team，实际 %s", s.GetTeamID())
	}
	// 验证 envs 被设置
	if s.GetEnv("key1") != "val1" {
		t.Errorf("GetEnv('key1') 期望 val1，实际 %v", s.GetEnv("key1"))
	}
}

// TestAgentTeamSession_GetSessionID 测试获取会话 ID
func TestAgentTeamSession_GetSessionID(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamSessionID("abc-123"))
	if s.GetSessionID() != "abc-123" {
		t.Errorf("期望 abc-123，实际 %s", s.GetSessionID())
	}
}

// TestAgentTeamSession_UpdateState_GetState_DumpState 测试状态读写
func TestAgentTeamSession_UpdateState_GetState_DumpState(t *testing.T) {
	s := NewAgentTeamSession()

	// UpdateState + GetState
	s.UpdateState(map[string]any{"key": "value"})
	result, err := s.GetState(state.StringKey("key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "value" {
		t.Errorf("期望 value，实际 %v", result)
	}

	// DumpState
	dump := s.DumpState()
	if dump == nil {
		t.Fatal("DumpState 不应返回 nil")
	}
}

// TestAgentTeamSession_WriteStream_WriteCustomStream 测试流写入
func TestAgentTeamSession_WriteStream_WriteCustomStream(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamSessionID("team-sw-test"))

	// WriteStream
	err := s.WriteStream(context.Background(), map[string]any{"text": "hello"})
	if err != nil {
		t.Errorf("WriteStream 不应返回错误：%v", err)
	}

	// WriteCustomStream
	err = s.WriteCustomStream(context.Background(), map[string]any{"key": "value"})
	if err != nil {
		t.Errorf("WriteCustomStream 不应返回错误：%v", err)
	}
}

// TestAgentTeamSession_WriteStream_触发CustomCallback 测试 WriteStream 触发自定义回调
func TestAgentTeamSession_WriteStream_触发CustomCallback(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamSessionID("team-sw31-test"))

	var triggered bool
	fn := func(_ context.Context, data map[string]any) any {
		triggered = true
		return nil
	}

	fw := callback.GetCallbackFramework()
	fw.OnCustom("team-sw31-testwrite_stream", fn)
	defer fw.OffAllCustom("team-sw31-testwrite_stream")

	_ = s.WriteStream(context.Background(), map[string]any{"text": "hello"})

	if !triggered {
		t.Error("WriteStream 应触发自定义 StreamWrite 回调")
	}
}

// TestAgentTeamSession_WriteCustomStream_触发CustomCallback 测试 WriteCustomStream 触发自定义回调
func TestAgentTeamSession_WriteCustomStream_触发CustomCallback(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamSessionID("team-sw32-test"))

	var triggered bool
	fn := func(_ context.Context, data map[string]any) any {
		triggered = true
		return nil
	}

	fw := callback.GetCallbackFramework()
	fw.OnCustom("team-sw32-testwrite_stream", fn)
	defer fw.OffAllCustom("team-sw32-testwrite_stream")

	_ = s.WriteCustomStream(context.Background(), map[string]any{"key": "value"})

	if !triggered {
		t.Error("WriteCustomStream 应触发自定义 StreamWrite 回调")
	}
}

// TestAgentTeamSession_GetEnv_GetEnvs 测试环境变量
func TestAgentTeamSession_GetEnv_GetEnvs(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamEnvs(map[string]any{"my_key": "my_val"}))

	// GetEnv
	if s.GetEnv("my_key") != "my_val" {
		t.Errorf("GetEnv('my_key') 期望 my_val，实际 %v", s.GetEnv("my_key"))
	}
	if s.GetEnv("missing_key") != nil {
		t.Error("GetEnv 无匹配 key 应返回 nil")
	}

	// GetEnv 默认值
	defaultVal := s.GetEnv("missing_key", "default")
	if defaultVal != "default" {
		t.Errorf("GetEnv 默认值期望 'default'，实际 %v", defaultVal)
	}

	// GetEnvs
	envs := s.GetEnvs()
	if envs == nil {
		t.Error("GetEnvs 不应返回 nil")
	}
}

// TestAgentTeamSession_Interact_返回错误 测试 Interact 返回错误
func TestAgentTeamSession_Interact_返回错误(t *testing.T) {
	s := NewAgentTeamSession()
	err := s.Interact(context.Background(), nil)
	if err == nil {
		t.Error("Interact 应返回错误")
	}
}

// TestAgentTeamSession_GetTeamID 测试获取团队 ID
func TestAgentTeamSession_GetTeamID(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamTeamID("custom-team"))
	if s.GetTeamID() != "custom-team" {
		t.Errorf("TeamID 期望 custom-team，实际 %s", s.GetTeamID())
	}
}

// TestAgentTeamSession_PreRun_PostRun_Commit 测试 PreRun/PostRun/Commit
func TestAgentTeamSession_PreRun_PostRun_Commit(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamCheckpointer(&noOpTeamCheckpointer{}))

	// PreRun
	err := s.PreRun(context.Background())
	if err != nil {
		t.Errorf("PreRun 不应返回错误：%v", err)
	}

	// PreRun 幂等
	err = s.PreRun(context.Background())
	if err != nil {
		t.Errorf("幂等 PreRun 不应返回错误：%v", err)
	}

	// Commit
	err = s.Commit(context.Background())
	if err != nil {
		t.Errorf("Commit 不应返回错误：%v", err)
	}

	// PostRun
	err = s.PostRun(context.Background())
	if err != nil {
		t.Errorf("PostRun 不应返回错误：%v", err)
	}

	// PostRun 幂等
	err = s.PostRun(context.Background())
	if err != nil {
		t.Errorf("幂等 PostRun 不应返回错误：%v", err)
	}
}

// TestAgentTeamSession_PreRun_带输入 测试 PreRun 传入输入数据
func TestAgentTeamSession_PreRun_带输入(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamCheckpointer(&noOpTeamCheckpointer{}))

	err := s.PreRun(context.Background(), map[string]any{"input_key": "input_val"})
	if err != nil {
		t.Errorf("PreRun 带输入不应返回错误：%v", err)
	}
}

// TestAgentTeamSession_CloseStream 测试关闭流
func TestAgentTeamSession_CloseStream(t *testing.T) {
	s := NewAgentTeamSession()
	err := s.CloseStream()
	if err != nil {
		t.Errorf("CloseStream 不应返回错误：%v", err)
	}
}

// TestAgentTeamSession_FlushCheckpoint 测试 FlushCheckpoint 等价 Commit
func TestAgentTeamSession_FlushCheckpoint(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamCheckpointer(&noOpTeamCheckpointer{}))

	err := s.FlushCheckpoint(context.Background())
	if err != nil {
		t.Errorf("FlushCheckpoint 不应返回错误：%v", err)
	}
}

// TestAgentTeamSession_CreateAgentSession 测试创建子 AgentSession
func TestAgentTeamSession_CreateAgentSession(t *testing.T) {
	s := NewAgentTeamSession(WithAgentTeamSessionID("team-parent"))
	card := &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "agent-1", Name: "测试Agent"}}

	agentSess := s.CreateAgentSession(card, "agent-1", true)
	if agentSess == nil {
		t.Fatal("CreateAgentSession 返回 nil")
	}
	if agentSess.GetSessionID() == "" {
		t.Error("子 AgentSession 的 SessionID 不应为空")
	}
}

// TestAgentTeamSession_Inner 测试获取内部层
func TestAgentTeamSession_Inner(t *testing.T) {
	s := NewAgentTeamSession()
	inner := s.Inner()
	if inner == nil {
		t.Fatal("Inner 返回 nil")
	}
}

// TestAgentTeamSession_WithAgentTeamCheckpointer 测试 WithAgentTeamCheckpointer 选项
func TestAgentTeamSession_WithAgentTeamCheckpointer(t *testing.T) {
	cp := checkpointer.GetCheckpointer()
	s := NewAgentTeamSession(WithAgentTeamCheckpointer(cp))
	if s.inner.Checkpointer() != cp {
		t.Error("WithAgentTeamCheckpointer 后 checkpointer 应一致")
	}
}

// TestAgentTeamSession_WithAgentTeamStreamWriterManager 测试 WithAgentTeamStreamWriterManager 选项
func TestAgentTeamSession_WithAgentTeamStreamWriterManager(t *testing.T) {
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	s := NewAgentTeamSession(WithAgentTeamStreamWriterManager(swm))
	if s.inner.StreamWriterManager() != swm {
		t.Errorf("inner.StreamWriterManager 期望 %v，实际 %v", swm, s.inner.StreamWriterManager())
	}
}
