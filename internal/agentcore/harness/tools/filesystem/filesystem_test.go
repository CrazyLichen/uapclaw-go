package filesystem

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── NewGlobTool 测试 ────────────────────────────

// TestNewGlobTool 测试创建 GlobTool 实例
func TestNewGlobTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewGlobTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewGlobTool 不应返回 nil")
	}
}

// ──────────────────────────── NewGrepTool 测试 ────────────────────────────

// TestNewGrepTool 测试创建 GrepTool 实例
func TestNewGrepTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewGrepTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewGrepTool 不应返回 nil")
	}
}

// ──────────────────────────── NewListDirTool 测试 ────────────────────────────

// TestNewListDirTool 测试创建 ListDirTool 实例
func TestNewListDirTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewListDirTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewListDirTool 不应返回 nil")
	}
}

// ──────────────────────────── NewReadFileTool 测试 ────────────────────────────

// TestNewReadFileTool 测试创建 ReadFileTool 实例
func TestNewReadFileTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewReadFileTool(op, "cn", "test-agent-id", false)
	if tool == nil {
		t.Error("NewReadFileTool 不应返回 nil")
	}
}

// ──────────────────────────── NewWriteFileTool 测试 ────────────────────────────

// TestNewWriteFileTool 测试创建 WriteFileTool 实例
func TestNewWriteFileTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewWriteFileTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewWriteFileTool 不应返回 nil")
	}
}

// ──────────────────────────── NewEditFileTool 测试 ────────────────────────────

// TestNewEditFileTool 测试创建 EditFileTool 实例
func TestNewEditFileTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewEditFileTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewEditFileTool 不应返回 nil")
	}
}
