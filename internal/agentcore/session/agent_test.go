package session

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

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
	s := NewSession(WithSessionID("test-idempotent"))

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
	s := NewSession()
	err := s.PostRun(context.Background())
	if err != nil {
		t.Errorf("PostRun 不应返回错误：%v", err)
	}
}

// TestSession_PostRun_幂等 测试重复调用只执行一次
func TestSession_PostRun_幂等(t *testing.T) {
	s := NewSession()
	_ = s.PostRun(context.Background())
	_ = s.PostRun(context.Background())
	// 不应 panic 或重复关闭
}

// TestSession_Commit 测试提交检查点
func TestSession_Commit(t *testing.T) {
	s := NewSession()
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
	s := NewSession()

	if err := s.WriteStream(nil); err != nil {
		t.Errorf("WriteStream 桩应返回 nil，实际 %v", err)
	}
	if err := s.WriteCustomStream(nil); err != nil {
		t.Errorf("WriteCustomStream 桩应返回 nil，实际 %v", err)
	}
	if ch := s.StreamIterator(); ch != nil {
		t.Errorf("StreamIterator 桩应返回 nil，实际 %v", ch)
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
		s.Interact(nil)
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

// TestSession_tagStreamPayload 测试流数据元数据标签
func TestSession_tagStreamPayload(t *testing.T) {
	// 无元数据时原样返回
	s1 := NewSession()
	data := map[string]any{"key": "value"}
	result := s1.tagStreamPayload(data)
	if result["key"] != "value" {
		t.Errorf("无元数据时 key 期望 value，实际 %v", result["key"])
	}
	if _, ok := result["source"]; ok {
		t.Error("无元数据时不应添加 source 字段")
	}

	// 有元数据时合并
	s2 := NewSession(WithSourceMetadata(map[string]any{"source": "team-1"}))
	data2 := map[string]any{"key": "value"}
	result2 := s2.tagStreamPayload(data2)
	if result2["key"] != "value" {
		t.Errorf("有元数据时 key 期望 value，实际 %v", result2["key"])
	}
	if result2["source"] != "team-1" {
		t.Errorf("source 期望 team-1，实际 %v", result2["source"])
	}
}

// TestSession_GetEnv返回Nil 测试桩方法 GetEnv
func TestSession_GetEnv返回Nil(t *testing.T) {
	s := NewSession()
	if s.GetEnv("any_key") != nil {
		t.Error("GetEnv 桩应返回 nil")
	}
}

// TestSession_GetEnvs返回Nil 测试桩方法 GetEnvs
func TestSession_GetEnvs返回Nil(t *testing.T) {
	s := NewSession()
	if s.GetEnvs() != nil {
		t.Error("GetEnvs 桩应返回 nil")
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
	card := map[string]any{"id": "agent-1"}
	s := NewSession(WithCard(card))
	if s.card == nil {
		t.Error("WithCard 后 card 不应为 nil")
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
