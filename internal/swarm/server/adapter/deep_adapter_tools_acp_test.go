package adapter

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
)

// ──────────────────── getToolCards acp_chat 测试（10.6.24d 回填点①）────────────────────

// TestGetToolCards_acpChat注册 acp_agents 非空时注册 acp_chat 卡。
// Python: interface_deep.py getToolCards L2501-2510。
func TestGetToolCards_acpChat注册(t *testing.T) {
	d := NewDeepAdapter()
	d.configBase = map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{"command": "codex", "args": []any{"--a"}},
		},
	}
	cards := d.getToolCards("agent-x")
	found := false
	for _, card := range cards {
		if card.Name == "acp_chat" {
			found = true
		}
	}
	if !found {
		t.Error("acp_agents 非空时 getToolCards 应含 acp_chat 卡")
	}
	// ResourceMgr 可查回
	for _, card := range cards {
		if card.Name == "acp_chat" {
			existing, err := runner.GetResourceMgr().GetTool([]string{card.ID})
			if err != nil || len(existing) != 1 {
				t.Errorf("acp_chat 应可从 ResourceMgr 查回, err=%v count=%d", err, len(existing))
			}
		}
	}
}

// TestGetToolCards_acpAgents为空不注册 acp_agents 空/缺失时不注册。
func TestGetToolCards_acpAgents为空不注册(t *testing.T) {
	// 空 map
	d := NewDeepAdapter()
	d.configBase = map[string]any{
		"acp_agents": map[string]any{},
	}
	cards := d.getToolCards("agent-x")
	for _, card := range cards {
		if card.Name == "acp_chat" {
			t.Error("acp_agents 空 map 时不应注册 acp_chat")
		}
	}
	// 缺失键
	d2 := NewDeepAdapter()
	d2.configBase = map[string]any{}
	for _, card := range d2.getToolCards("agent-y") {
		if card.Name == "acp_chat" {
			t.Error("无 acp_agents 键时不应注册 acp_chat")
		}
	}
	// 非 dict
	d3 := NewDeepAdapter()
	d3.configBase = map[string]any{"acp_agents": "bad"}
	for _, card := range d3.getToolCards("agent-z") {
		if card.Name == "acp_chat" {
			t.Error("acp_agents 非 dict 时不应注册 acp_chat")
		}
	}
}

// TestGetToolCards_acpChat幂等 二次调用不重复注册。
func TestGetToolCards_acpChat幂等(t *testing.T) {
	d := NewDeepAdapter()
	d.configBase = map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{"command": "codex"},
		},
	}
	d.getToolCards("agent-w")
	cards2 := d.getToolCards("agent-w")
	count := 0
	for _, card := range cards2 {
		if card.Name == "acp_chat" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("二次调用 acp_chat 卡应仍为 1, got %d", count)
	}
}

// ──────────────────── code case acp_chat 测试（10.6.24d 回填点②）────────────────────

// TestCodeGetToolCards_acpChatCase code 模式 tools 含 acp_chat 且有配置时注册。
// Python: interface_code.py _TOOL_BUILD_NAMES["acp_chat"] → _build_acp_chat_tool。
func TestCodeGetToolCards_acpChatCase(t *testing.T) {
	c := NewCodeAdapter()
	c.deep.configBase = map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{"command": "codex"},
		},
		"modes": map[string]any{
			"code": map[string]any{
				"tools": []any{"acp_chat"},
			},
		},
	}
	cards := c.getToolCards("agent-code-1")
	found := false
	for _, card := range cards {
		if card.Name == "acp_chat" {
			found = true
		}
	}
	if !found {
		t.Fatal("code 模式 tools 含 acp_chat 且 acp_agents 非空 → 应注册")
	}
}

// TestCodeGetToolCards_acpChat无配置跳过 acp_agents 空时 case 跳过。
func TestCodeGetToolCards_acpChat无配置跳过(t *testing.T) {
	c := NewCodeAdapter()
	c.deep.configBase = map[string]any{
		"modes": map[string]any{
			"code": map[string]any{
				"tools": []any{"acp_chat"},
			},
		},
	}
	cards := c.getToolCards("agent-code-2")
	for _, card := range cards {
		if card.Name == "acp_chat" {
			t.Error("acp_agents 无配置时 code case 应跳过 acp_chat")
		}
	}
}
