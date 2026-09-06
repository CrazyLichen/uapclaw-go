package tools

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestNewTeamTool 测试 TeamTool 构造
func TestNewTeamTool(t *testing.T) {
	card := tool.NewToolCard("测试工具", "用于测试", nil, nil)
	tt := NewTeamTool(card)
	if tt.Card() != card {
		t.Error("Card() 应返回构造时传入的 ToolCard")
	}
	if tt.Card().GetName() != "测试工具" {
		t.Errorf("Card().GetName() 期望 '测试工具'，实际 %s", tt.Card().GetName())
	}
}

// TestTeamTool_Card_Nil 测试 Card 为 nil 时返回 nil
func TestTeamTool_Card_Nil(t *testing.T) {
	tt := TeamTool{}
	if tt.Card() != nil {
		t.Error("未初始化的 TeamTool Card() 应返回 nil")
	}
}
