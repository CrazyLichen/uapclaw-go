package lite

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// TestNewCodingMemoryToolContext_默认值 测试默认值
func TestNewCodingMemoryToolContext_默认值(t *testing.T) {
	ctx := NewCodingMemoryToolContext()
	if ctx.NodeName != "coding_memory" {
		t.Errorf("NodeName = %q, want %q", ctx.NodeName, "coding_memory")
	}
	if ctx.CodingMemoryDir != "" {
		t.Errorf("CodingMemoryDir = %q, want empty", ctx.CodingMemoryDir)
	}
}

// TestCodingMemoryToolContext_WithXxx 测试 Builder 链式调用
func TestCodingMemoryToolContext_WithXxx(t *testing.T) {
	ws := &workspace.Workspace{}
	settings := CreateMemorySettings("/tmp/cm", nil)
	embCfg := &embedding.EmbeddingConfig{}
	var op sysop.SysOperation

	ctx := NewCodingMemoryToolContext().
		WithWorkspace(ws).
		WithSettings(settings).
		WithAgentID("test-agent").
		WithEmbeddingConfig(embCfg).
		WithSysOperation(op).
		WithCodingMemoryDir("/tmp/cm")

	if ctx.Workspace != ws {
		t.Error("WithWorkspace 未设置")
	}
	if ctx.Settings != settings {
		t.Error("WithSettings 未设置")
	}
	if ctx.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", ctx.AgentID, "test-agent")
	}
	if ctx.EmbeddingConfig != embCfg {
		t.Error("WithEmbeddingConfig 未设置")
	}
	if ctx.CodingMemoryDir != "/tmp/cm" {
		t.Errorf("CodingMemoryDir = %q, want %q", ctx.CodingMemoryDir, "/tmp/cm")
	}
}

// TestNewMemoryToolContext_默认值 测试默认值
func TestNewMemoryToolContext_默认值(t *testing.T) {
	ctx := NewMemoryToolContext()
	if ctx.NodeName != "memory" {
		t.Errorf("NodeName = %q, want %q", ctx.NodeName, "memory")
	}
}

// TestMemoryToolContext_WithXxx 测试 Builder 链式调用
func TestMemoryToolContext_WithXxx(t *testing.T) {
	ws := &workspace.Workspace{}
	settings := CreateMemorySettings("/tmp/mem", nil)
	embCfg := &embedding.EmbeddingConfig{}
	var op sysop.SysOperation

	ctx := NewMemoryToolContext().
		WithWorkspace(ws).
		WithSettings(settings).
		WithAgentID("test-agent").
		WithEmbeddingConfig(embCfg).
		WithSysOperation(op).
		WithManager(nil)

	if ctx.Workspace != ws {
		t.Error("WithWorkspace 未设置")
	}
	if ctx.Settings != settings {
		t.Error("WithSettings 未设置")
	}
	if ctx.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", ctx.AgentID, "test-agent")
	}
	if ctx.EmbeddingConfig != embCfg {
		t.Error("WithEmbeddingConfig 未设置")
	}
}

// TestLiteMemoryToolContextBase_WithNodeName 测试 WithNodeName
func TestLiteMemoryToolContextBase_WithNodeName(t *testing.T) {
	base := &LiteMemoryToolContextBase{}
	result := base.WithNodeName("custom_node")
	if base.NodeName != "custom_node" {
		t.Errorf("NodeName = %q, want %q", base.NodeName, "custom_node")
	}
	if result != base {
		t.Error("WithNodeName 应返回自身指针")
	}
}
