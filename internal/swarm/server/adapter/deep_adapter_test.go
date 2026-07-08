package adapter

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewDeepAdapter 测试 DeepAdapter 构造函数默认值。
func TestNewDeepAdapter(t *testing.T) {
	d := NewDeepAdapter()
	if d.agentName != "main_agent" {
		t.Errorf("agentName = %q, want %q", d.agentName, "main_agent")
	}
	if d.isCodeAgent != false {
		t.Errorf("isCodeAgent = %v, want false", d.isCodeAgent)
	}
	if d.activeSessionIDs == nil {
		t.Error("activeSessionIDs is nil, want initialized map")
	}
}

// TestDeepAdapter_接口满足性 编译期检查 DeepAdapter 实现 AgentAdapter 接口。
func TestDeepAdapter_接口满足性(t *testing.T) {
	var _ AgentAdapter = (*DeepAdapter)(nil)
}

// TestDeepAdapter_SessionActive 测试 session 活跃计数四个方法的完整语义。
func TestDeepAdapter_SessionActive(t *testing.T) {
	d := NewDeepAdapter()

	// 初始状态：无活跃 session
	if d.isSessionActive("s1") {
		t.Error("初始状态 s1 不应活跃")
	}

	// mark 一次
	d.markSessionActive("s1")
	if !d.isSessionActive("s1") {
		t.Error("mark 后 s1 应活跃")
	}
	if d.otherActiveSessions("s1") != 0 {
		t.Error("仅 s1 活跃，otherActiveSessions(s1) 应为 0")
	}

	// mark 两次（Counter 语义）
	d.markSessionActive("s1")
	d.markSessionActive("s2")
	if d.otherActiveSessions("s1") != 1 {
		t.Errorf("s1 计数2 + s2 计数1，otherActiveSessions(s1) = %d, want 1", d.otherActiveSessions("s1"))
	}

	// unmark 一次 s1（计数从2降到1，不应删除）
	d.unmarkSessionActive("s1")
	if !d.isSessionActive("s1") {
		t.Error("unmark 一次后 s1 仍应活跃（计数=1）")
	}

	// unmark 再次 s1（计数归零，应删除）
	d.unmarkSessionActive("s1")
	if d.isSessionActive("s1") {
		t.Error("unmark 归零后 s1 不应活跃")
	}

	// unmark 不存在的 session 不应 panic
	d.unmarkSessionActive("nonexistent")
}

// TestDeepAdapter_ProcessInterrupt_Intent分支 测试 pause/resume/cancel/supplement 分支。
func TestDeepAdapter_ProcessInterrupt_Intent分支(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	tests := []struct {
		name   string
		intent string
	}{
		{"pause", "pause"},
		{"resume", "resume"},
		{"cancel", "cancel"},
		{"supplement", "supplement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid := "test-session"
			params, _ := json.Marshal(map[string]any{"intent": tt.intent})
			req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params,
				schema.WithAgentSessionID(sid),
			)
			resp, err := d.ProcessInterrupt(ctx, req)
			if err != nil {
				t.Errorf("ProcessInterrupt intent=%s error: %v", tt.intent, err)
			}
			if resp == nil {
				t.Errorf("ProcessInterrupt intent=%s 返回 nil 响应", tt.intent)
			}
		})
	}
}

// TestDeepAdapter_ProcessInterrupt_未初始化 测试 instance=nil 时不 panic。
func TestDeepAdapter_ProcessInterrupt_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	// instance 为 nil，但 ProcessInterrupt 不检查 instance，应正常返回
	ctx := t.Context()
	params, _ := json.Marshal(map[string]any{"intent": "cancel"})
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params)
	resp, err := d.ProcessInterrupt(ctx, req)
	if err != nil {
		t.Errorf("ProcessInterrupt error: %v", err)
	}
	if resp == nil {
		t.Error("ProcessInterrupt 返回 nil 响应")
	}
}

// TestDeepAdapter_HandleUserAnswer_前缀分发 测试 request_id 前缀分发逻辑。
func TestDeepAdapter_HandleUserAnswer_前缀分发(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	tests := []struct {
		name      string
		requestID string
		resolved  bool
	}{
		{"team_skill_evolve_前缀", "team_skill_evolve_123", false},
		{"evolve_simplify_前缀", "evolve_simplify_456", false},
		{"skill_evolve_前缀", "skill_evolve_789", false},
		{"未知前缀", "unknown_prefix", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{
				"request_id": tt.requestID,
				"answers":    []any{},
			})
			req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.user_answer"), params)
			resp, err := d.HandleUserAnswer(ctx, req)
			if err != nil {
				t.Errorf("HandleUserAnswer error: %v", err)
			}
			if resp == nil {
				t.Fatal("HandleUserAnswer 返回 nil 响应")
			}
			if resp.OK != true {
				t.Errorf("OK = %v, want true", resp.OK)
			}
			if resp.Payload["accepted"] != true {
				t.Errorf("accepted = %v, want true", resp.Payload["accepted"])
			}
			if resp.Payload["resolved"] != tt.resolved {
				t.Errorf("resolved = %v, want %v", resp.Payload["resolved"], tt.resolved)
			}
		})
	}
}

// TestDeepAdapter_HandleHeartbeat_前缀检查 测试 heartbeat 前缀判断。
func TestDeepAdapter_HandleHeartbeat_前缀检查(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	t.Run("heartbeat前缀", func(t *testing.T) {
		sid := "heartbeat_session_1"
		req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
			schema.WithAgentSessionID(sid),
		)
		resp, err := d.HandleHeartbeat(ctx, req)
		if err != nil {
			t.Errorf("HandleHeartbeat error: %v", err)
		}
		// heartbeat 返回 nil（继续正常流程）
		if resp != nil {
			t.Errorf("heartbeat 应返回 nil，got %v", resp)
		}
	})

	t.Run("非heartbeat前缀", func(t *testing.T) {
		sid := "normal_session_1"
		req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
			schema.WithAgentSessionID(sid),
		)
		resp, err := d.HandleHeartbeat(ctx, req)
		if err != nil {
			t.Errorf("HandleHeartbeat error: %v", err)
		}
		// 非 heartbeat 返回 nil
		if resp != nil {
			t.Errorf("非 heartbeat 应返回 nil，got %v", resp)
		}
	})
}

// TestDeepAdapter_ProcessMessageImpl_未初始化 测试 instance=nil 时返回错误。
func TestDeepAdapter_ProcessMessageImpl_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := d.ProcessMessageImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestDeepAdapter_ProcessMessageStreamImpl_未初始化 测试 instance=nil 时返回错误。
func TestDeepAdapter_ProcessMessageStreamImpl_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := d.ProcessMessageStreamImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestParseParams 测试 json.RawMessage 解析。
func TestParseParams(t *testing.T) {
	t.Run("空输入", func(t *testing.T) {
		m := parseParams(nil)
		if len(m) != 0 {
			t.Errorf("nil 输入应返回空 map，got %d items", len(m))
		}
	})

	t.Run("空JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(``))
		if len(m) != 0 {
			t.Errorf("空 JSON 应返回空 map，got %d items", len(m))
		}
	})

	t.Run("无效JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(`invalid`))
		if len(m) != 0 {
			t.Errorf("无效 JSON 应返回空 map，got %d items", len(m))
		}
	})

	t.Run("有效JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(`{"query":"hello","mode":"agent.plan"}`))
		if m["query"] != "hello" {
			t.Errorf("query = %v, want hello", m["query"])
		}
		if m["mode"] != "agent.plan" {
			t.Errorf("mode = %v, want agent.plan", m["mode"])
		}
	})
}

// TestParamsString 测试字符串取值。
func TestParamsString(t *testing.T) {
	params := map[string]any{"key": "value", "num": 123}

	t.Run("存在字符串值", func(t *testing.T) {
		if v := paramsString(params, "key", "default"); v != "value" {
			t.Errorf("got %q, want %q", v, "value")
		}
	})

	t.Run("键不存在", func(t *testing.T) {
		if v := paramsString(params, "missing", "default"); v != "default" {
			t.Errorf("got %q, want %q", v, "default")
		}
	})

	t.Run("值非字符串", func(t *testing.T) {
		if v := paramsString(params, "num", "default"); v != "default" {
			t.Errorf("非字符串值应返回默认值，got %q", v)
		}
	})
}

// TestDeepAdapter_CreateInstance_默认值 测试 CreateInstance 基本参数处理。
func TestDeepAdapter_CreateInstance_默认值(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	// 不传 config，mode="agent.plan"
	err := d.CreateInstance(ctx, nil, "agent.plan", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if d.mode != "agent.plan" {
		t.Errorf("mode = %q, want %q", d.mode, "agent.plan")
	}
	if d.dreamingMode != "agent.plan" {
		t.Errorf("dreamingMode = %q, want %q", d.dreamingMode, "agent.plan")
	}
}

// TestDeepAdapter_CreateInstance_dreamingMode 测试 dreaming_mode 按 mode 前缀判断。
func TestDeepAdapter_CreateInstance_dreamingMode(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		mode      string
		wantDream string
	}{
		{"agent.plan", "agent.plan"},
		{"agent.fast", "agent.fast"},
		{"code", "agent"}, // 非 "agent" 前缀 → 默认 "agent"
		{"", "agent"},     // 空 mode → 默认 "agent"
		{"team", "agent"}, // 非 "agent" 前缀 → 默认 "agent"
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("mode=%s", tt.mode), func(t *testing.T) {
			d := NewDeepAdapter()
			err := d.CreateInstance(ctx, nil, tt.mode, "")
			if err != nil {
				t.Fatalf("CreateInstance error: %v", err)
			}
			if d.dreamingMode != tt.wantDream {
				t.Errorf("dreamingMode = %q, want %q", d.dreamingMode, tt.wantDream)
			}
		})
	}
}

// TestDeepAdapter_CreateInstance_config覆盖 测试 config 字典中 agent_name/project_dir 提取。
func TestDeepAdapter_CreateInstance_config覆盖(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	config := map[string]any{
		"agent_name":  "custom_agent",
		"project_dir": "/tmp/project",
	}
	err := d.CreateInstance(ctx, config, "agent.plan", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if d.agentName != "custom_agent" {
		t.Errorf("agentName = %q, want %q", d.agentName, "custom_agent")
	}
	if d.projectDir != "/tmp/project" {
		t.Errorf("projectDir = %q, want %q", d.projectDir, "/tmp/project")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHasValidModelConfig_空字符串 验证空字符串返回 true
func TestHasValidModelConfig_空字符串(t *testing.T) {
	d := NewDeepAdapter()
	if !d.hasValidModelConfig("") {
		t.Error("空字符串应返回 true（使用默认模型）")
	}
}

// TestHasValidModelConfig_缓存中存在 验证缓存中存在的模型返回 true
func TestHasValidModelConfig_缓存中存在(t *testing.T) {
	d := NewDeepAdapter()
	// 模拟 modelCache 中存在模型
	d.modelCache = map[string]*llm.Model{"gpt-4": nil}
	if !d.hasValidModelConfig("gpt-4") {
		t.Error("缓存中存在的模型应返回 true")
	}
}

// TestHasValidModelConfig_缓存中不存在 验证缓存中不存在的模型返回 false
func TestHasValidModelConfig_缓存中不存在(t *testing.T) {
	d := NewDeepAdapter()
	if d.hasValidModelConfig("nonexistent") {
		t.Error("缓存中不存在的模型应返回 false")
	}
}
