package prompts

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestParsePRContent_单角色 测试单个角色的模板解析
func TestParsePRContent_单角色(t *testing.T) {
	content := "`#system#`你是助手。"
	msgs := ParsePRContent(content)
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llmschema.RoleTypeSystem {
		t.Errorf("期望 role=system，实际 %s", msgs[0].GetRole().String())
	}
}

// TestParsePRContent_多角色 测试多角色模板解析
func TestParsePRContent_多角色(t *testing.T) {
	content := "`#system#`你是助手。`#user#`请提取实体。"
	msgs := ParsePRContent(content)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llmschema.RoleTypeSystem {
		t.Errorf("消息[0] 期望 role=system，实际 %s", msgs[0].GetRole().String())
	}
	if msgs[1].GetRole() != llmschema.RoleTypeUser {
		t.Errorf("消息[1] 期望 role=user，实际 %s", msgs[1].GetRole().String())
	}
}

// TestParsePRContent_空内容 测试空内容输入
func TestParsePRContent_空内容(t *testing.T) {
	msgs := ParsePRContent("")
	if len(msgs) != 0 {
		t.Errorf("期望 0 条消息，实际 %d", len(msgs))
	}
}

// TestParsePRContent_未知角色跳过 测试未知角色被跳过
func TestParsePRContent_未知角色跳过(t *testing.T) {
	content := "`#system#`你是助手。`#unknown#`无效内容`#user#`请提取。"
	msgs := ParsePRContent(content)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条消息（跳过 unknown），实际 %d", len(msgs))
	}
}

// TestParsePRContent_三角色 测试三个角色的完整解析
func TestParsePRContent_三角色(t *testing.T) {
	content := "`#system#`系统指令`#user#`用户输入`#assistant#`助手输出"
	msgs := ParsePRContent(content)
	if len(msgs) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llmschema.RoleTypeSystem {
		t.Errorf("消息[0] 期望 role=system，实际 %s", msgs[0].GetRole().String())
	}
	if msgs[1].GetRole() != llmschema.RoleTypeUser {
		t.Errorf("消息[1] 期望 role=user，实际 %s", msgs[1].GetRole().String())
	}
	if msgs[2].GetRole() != llmschema.RoleTypeAssistant {
		t.Errorf("消息[2] 期望 role=assistant，实际 %s", msgs[2].GetRole().String())
	}
}

// TestParsePRContent_tool角色 测试 tool 角色
func TestParsePRContent_tool角色(t *testing.T) {
	content := "`#tool#`工具返回结果"
	msgs := ParsePRContent(content)
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llmschema.RoleTypeTool {
		t.Errorf("期望 role=tool，实际 %s", msgs[0].GetRole().String())
	}
}

// TestParsePRContent_含占位符 测试含模板占位符的内容
func TestParsePRContent_含占位符(t *testing.T) {
	content := "`#system#`你是助手。`#user#`请从以下内容中提取实体：{{source_description}}"
	msgs := ParsePRContent(content)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(msgs))
	}
}
