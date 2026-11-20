package prompts

import (
	"testing"
)

// TestTemplateManager_加载模板 测试模板管理器加载模板
func TestTemplateManager_加载模板(t *testing.T) {
	mgr := GetTemplateManager()
	if mgr == nil {
		t.Fatal("TemplateManager 不应为 nil")
	}
}

// TestTemplateManager_Contains 测试 Contains 方法
func TestTemplateManager_Contains(t *testing.T) {
	mgr := GetTemplateManager()
	// 验证已注册的中文模板名
	if !mgr.Contains("entity_extraction_conversation_cn") {
		t.Error("应包含 entity_extraction_conversation_cn 模板")
	}
	// 验证已注册的英文模板名
	if !mgr.Contains("entity_extraction_conversation_en") {
		t.Error("应包含 entity_extraction_conversation_en 模板")
	}
}

// TestTemplateManager_Get返回模板 测试 Get 方法
func TestTemplateManager_Get返回模板(t *testing.T) {
	mgr := GetTemplateManager()
	tmpl := mgr.Get("entity_extraction_conversation_cn")
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_conversation_cn" {
		t.Errorf("模板名期望 entity_extraction_conversation_cn，实际 %s", tmpl.Name)
	}
}

// TestTemplateManager_Get不存在的模板 测试获取不存在的模板
func TestTemplateManager_Get不存在的模板(t *testing.T) {
	mgr := GetTemplateManager()
	tmpl := mgr.Get("nonexistent_template")
	if tmpl != nil {
		t.Error("不存在的模板应返回 nil")
	}
}

// TestTemplateManager_AllNames 测试 AllNames 方法
func TestTemplateManager_AllNames(t *testing.T) {
	mgr := GetTemplateManager()
	names := mgr.AllNames()
	if len(names) == 0 {
		t.Error("模板列表不应为空")
	}
}

// TestTemplateManager_中文模板数量 测试中文模板数量
func TestTemplateManager_中文模板数量(t *testing.T) {
	mgr := GetTemplateManager()
	names := mgr.AllNames()
	cnCount := 0
	for _, name := range names {
		if len(name) > 3 && name[len(name)-3:] == "_cn" {
			cnCount++
		}
	}
	if cnCount < 11 {
		t.Errorf("中文模板至少应有 11 个，实际 %d", cnCount)
	}
}

// TestTemplateManager_英文模板数量 测试英文模板数量
func TestTemplateManager_英文模板数量(t *testing.T) {
	mgr := GetTemplateManager()
	names := mgr.AllNames()
	enCount := 0
	for _, name := range names {
		if len(name) > 3 && name[len(name)-3:] == "_en" {
			enCount++
		}
	}
	if enCount < 11 {
		t.Errorf("英文模板至少应有 11 个，实际 %d", enCount)
	}
}
