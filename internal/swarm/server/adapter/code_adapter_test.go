package adapter

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewCodeAdapter 测试 CodeAdapter 构造函数。
func TestNewCodeAdapter(t *testing.T) {
	c := NewCodeAdapter()

	// deep 非空
	if c.deep == nil {
		t.Error("deep 不应为 nil")
	}

	// isCodeAgent = true
	if !c.deep.isCodeAgent {
		t.Error("isCodeAgent 应为 true")
	}

	// forceEnglishRuntimePrompt = true
	if !c.forceEnglishRuntimePrompt {
		t.Error("forceEnglishRuntimePrompt 应为 true")
	}

	// agentName 继承 DeepAdapter 默认值
	if c.deep.agentName != "main_agent" {
		t.Errorf("agentName = %q, want %q", c.deep.agentName, "main_agent")
	}
}

// TestCodeAdapter_接口满足性 编译期检查 CodeAdapter 实现 AgentAdapter 接口。
func TestCodeAdapter_接口满足性(t *testing.T) {
	var _ AgentAdapter = (*CodeAdapter)(nil)
}

// TestCodeAdapter_CreateInstance_dreamingMode 测试 CodeAdapter 固定 dreaming_mode="code"。
func TestCodeAdapter_CreateInstance_dreamingMode(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()

	err := c.CreateInstance(ctx, nil, "code", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if c.deep.dreamingMode != "code" {
		t.Errorf("dreamingMode = %q, want %q", c.deep.dreamingMode, "code")
	}
	if c.deep.isCodeAgent != true {
		t.Errorf("isCodeAgent = %v, want true", c.deep.isCodeAgent)
	}
}

// TestCodeAdapter_CreateInstance_mode存储 测试 mode/subMode 存储。
func TestCodeAdapter_CreateInstance_mode存储(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()

	err := c.CreateInstance(ctx, nil, "code.plan", "plan")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if c.deep.mode != "code.plan" {
		t.Errorf("mode = %q, want %q", c.deep.mode, "code.plan")
	}
	if c.deep.subMode != "plan" {
		t.Errorf("subMode = %q, want %q", c.deep.subMode, "plan")
	}
}
