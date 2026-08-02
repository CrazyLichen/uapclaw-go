//go:build test

package utils

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGetChatID_顶层字段(t *testing.T) {
	chatID := "chat-123"
	req := &schema.AgentRequest{ChatID: &chatID}
	result := GetChatID(req)
	if result != "chat-123" {
		t.Errorf("期望 chat-123, 实际 %s", result)
	}
}

func TestGetChatID_顶层字段为空(t *testing.T) {
	chatID := ""
	req := &schema.AgentRequest{ChatID: &chatID}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("期望空, 实际 %s", result)
	}
}

func TestGetChatID_ChatID为nil(t *testing.T) {
	req := &schema.AgentRequest{}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("期望空, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_feishu(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"feishu_chat_id": "fs-456"},
	}
	result := GetChatID(req)
	if result != "fs-456" {
		t.Errorf("期望 fs-456, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_wecom(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"wecom_chat_id": "wc-789"},
	}
	result := GetChatID(req)
	if result != "wc-789" {
		t.Errorf("期望 wc-789, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_dingtalk(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"dingtalk_chat_id": "dt-012"},
	}
	result := GetChatID(req)
	if result != "dt-012" {
		t.Errorf("期望 dt-012, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_xiaoyi(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"xiaoyi_session_id": "xy-345"},
	}
	result := GetChatID(req)
	if result != "xy-345" {
		t.Errorf("期望 xy-345, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退优先级(t *testing.T) {
	// feishu 优先于 wecom
	req := &schema.AgentRequest{
		Metadata: map[string]any{"feishu_chat_id": "fs-1", "wecom_chat_id": "wc-2"},
	}
	result := GetChatID(req)
	if result != "fs-1" {
		t.Errorf("期望 feishu 优先, 实际 %s", result)
	}
}

func TestGetChatID_Metadata值为非字符串(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"feishu_chat_id": 123},
	}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("非字符串值应跳过, 实际 %s", result)
	}
}

func TestGetChatID_全部为空(t *testing.T) {
	req := &schema.AgentRequest{}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("期望空, 实际 %s", result)
	}
}

func TestGetChatID_顶层优先于Metadata(t *testing.T) {
	chatID := "top-level"
	req := &schema.AgentRequest{
		ChatID:   &chatID,
		Metadata: map[string]any{"feishu_chat_id": "fs-456"},
	}
	result := GetChatID(req)
	if result != "top-level" {
		t.Errorf("期望 top-level 优先, 实际 %s", result)
	}
}

func TestIsTeamParams_nil(t *testing.T) {
	result := IsTeamParams(nil)
	if result {
		t.Error("nil 应返回 false")
	}
}

func TestIsTeamParams_team键为true(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": true})
	if !result {
		t.Error("team=true 应返回 true")
	}
}

func TestIsTeamParams_team键为字符串(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": "yes"})
	if !result {
		t.Error("team='yes' 应返回 true")
	}
}

func TestIsTeamParams_team键为非零整数(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": 1})
	if !result {
		t.Error("team=1 应返回 true")
	}
}

func TestIsTeamParams_team键为false(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": false})
	if result {
		t.Error("team=false 应返回 false")
	}
}

func TestIsTeamParams_team键为空字符串(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": ""})
	if result {
		t.Error("team='' 应返回 false")
	}
}

func TestIsTeamParams_mode为team(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "team"})
	if !result {
		t.Error("mode=team 应返回 true")
	}
}

func TestIsTeamParams_mode为teamPlan(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "team.plan"})
	if !result {
		t.Error("mode=team.plan 应返回 true")
	}
}

func TestIsTeamParams_mode为codeTeam(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "code.team"})
	if !result {
		t.Error("mode=code.team 应返回 true")
	}
}

func TestIsTeamParams_mode为其他(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "code"})
	if result {
		t.Error("mode=code 应返回 false")
	}
}

func TestIsTeamParams_mode大小写不敏感(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "TEAM"})
	if !result {
		t.Error("mode=TEAM 应返回 true（大小写不敏感）")
	}
}

func TestIsTeamParams_mode前后空格(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": " team.plan "})
	if !result {
		t.Error("mode=' team.plan ' 应返回 true（trim）")
	}
}

func TestIsTeamParams_mode为非字符串(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": 123})
	if result {
		t.Error("mode=123 应返回 false")
	}
}

func TestIsTeamParams_team和mode都无(t *testing.T) {
	result := IsTeamParams(map[string]any{"other": "value"})
	if result {
		t.Error("无 team/mode 应返回 false")
	}
}
