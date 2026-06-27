package resources_manager

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestPromptMgr_添加获取正常 测试 AddPrompt → GetPrompt 正常流程
func TestPromptMgr_添加获取正常(t *testing.T) {
	mgr := NewPromptMgr()

	tmpl := prompt.NewPromptTemplate("test-template", "hello {{name}}")
	err := mgr.AddPrompt("tmpl-1", tmpl)
	if err != nil {
		t.Fatalf("AddPrompt 失败: %v", err)
	}

	result, err := mgr.GetPrompt("tmpl-1")
	if err != nil {
		t.Fatalf("GetPrompt 失败: %v", err)
	}
	if result == nil {
		t.Error("GetPrompt 返回 nil，期望非 nil")
	}
}

// TestPromptMgr_批量添加 测试 AddPrompts 批量添加
func TestPromptMgr_批量添加(t *testing.T) {
	mgr := NewPromptMgr()

	entries := []PromptEntry{
		{ID: "tmpl-1", Template: prompt.NewPromptTemplate("t1", "hello")},
		{ID: "tmpl-2", Template: prompt.NewPromptTemplate("t2", "world")},
	}
	mgr.AddPrompts(entries)

	result1, err := mgr.GetPrompt("tmpl-1")
	if err != nil {
		t.Fatalf("GetPrompt(tmpl-1) 失败: %v", err)
	}
	if result1 == nil {
		t.Error("GetPrompt(tmpl-1) 返回 nil")
	}

	result2, err := mgr.GetPrompt("tmpl-2")
	if err != nil {
		t.Fatalf("GetPrompt(tmpl-2) 失败: %v", err)
	}
	if result2 == nil {
		t.Error("GetPrompt(tmpl-2) 返回 nil")
	}
}

// TestPromptMgr_空ID报错 测试空 templateID 添加报错
func TestPromptMgr_空ID报错(t *testing.T) {
	mgr := NewPromptMgr()

	err := mgr.AddPrompt("", prompt.NewPromptTemplate("t", "hello"))
	if err == nil {
		t.Error("空 templateID 应返回错误")
	}
}

// TestPromptMgr_空模板报错 测试 nil 模板添加报错
func TestPromptMgr_空模板报错(t *testing.T) {
	mgr := NewPromptMgr()

	err := mgr.AddPrompt("tmpl-1", nil)
	if err == nil {
		t.Error("nil 模板应返回错误")
	}
}

// TestPromptMgr_移除后获取返回错误 测试 RemovePrompt 后 GetPrompt 返回错误
func TestPromptMgr_移除后获取返回错误(t *testing.T) {
	mgr := NewPromptMgr()

	tmpl := prompt.NewPromptTemplate("test-template", "hello")
	err := mgr.AddPrompt("tmpl-1", tmpl)
	if err != nil {
		t.Fatalf("AddPrompt 失败: %v", err)
	}

	removed, err := mgr.RemovePrompt("tmpl-1")
	if err != nil {
		t.Fatalf("RemovePrompt 失败: %v", err)
	}
	if removed == nil {
		t.Error("RemovePrompt 应返回被移除的模板")
	}

	_, err = mgr.GetPrompt("tmpl-1")
	if err == nil {
		t.Error("移除后 GetPrompt 应返回错误")
	}
}

// TestPromptMgr_获取不存在返回错误 测试不存在的 templateID 返回错误
func TestPromptMgr_获取不存在返回错误(t *testing.T) {
	mgr := NewPromptMgr()

	_, err := mgr.GetPrompt("not-exist")
	if err == nil {
		t.Error("获取不存在的模板应返回错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
