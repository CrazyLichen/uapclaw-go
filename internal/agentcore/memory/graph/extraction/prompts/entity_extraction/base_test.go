package entity_extraction

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"

	// 空白导入触发 cn/en 的 init() 注册多语言数据
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/cn"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/en"
)

// TestFormatSourceDescription_有描述 测试有描述时的格式化
func TestFormatSourceDescription_有描述(t *testing.T) {
	result := FormatSourceDescription("测试数据源", "cn")
	if result == "" {
		t.Error("有描述时应返回非空字符串")
	}
	if !contains(result, "测试数据源") {
		t.Error("结果应包含原始描述")
	}
}

// TestFormatSourceDescription_无描述 测试无描述时的格式化
func TestFormatSourceDescription_无描述(t *testing.T) {
	result := FormatSourceDescription("", "cn")
	if result != "" {
		t.Error("无描述时应返回空字符串")
	}
}

// TestFormatSourceDescription_英文 测试英文格式化
func TestFormatSourceDescription_英文(t *testing.T) {
	result := FormatSourceDescription("test source", "en")
	if result == "" {
		t.Error("英文描述应返回非空字符串")
	}
}

// TestFormatExistingEntities_基本 测试实体列表格式化
func TestFormatExistingEntities_基本(t *testing.T) {
	entities := []map[string]any{
		{"name": "张三", "content": "工程师"},
	}
	result := FormatExistingEntities(entities, 1, "cn")
	if result == "" {
		t.Error("应返回格式化后的实体字符串")
	}
	if !contains(result, "张三") {
		t.Error("结果应包含实体名称")
	}
}

// TestFormatExistingEntities_空列表 测试空列表
func TestFormatExistingEntities_空列表(t *testing.T) {
	result := FormatExistingEntities(nil, 1, "cn")
	if result != "" {
		t.Error("空列表应返回空字符串")
	}
}

// TestFormatExistingRelations_基本 测试关系列表格式化
func TestFormatExistingRelations_基本(t *testing.T) {
	relations := []map[string]any{
		{"content": "张三在华为工作"},
	}
	result := FormatExistingRelations(relations, 1, true)
	if result == "" {
		t.Error("应返回格式化后的关系字符串")
	}
}

// TestFormatExistingRelations_空列表 测试空列表
func TestFormatExistingRelations_空列表(t *testing.T) {
	result := FormatExistingRelations(nil, 1, true)
	if result != "" {
		t.Error("空列表应返回空字符串")
	}
}

// TestEnsureValidLanguage_有效 测试有效语言
func TestEnsureValidLanguage_有效(t *testing.T) {
	lang, err := EnsureValidLanguage("cn", 10)
	if err != nil {
		t.Errorf("cn 应为有效语言，报错: %v", err)
	}
	if lang != "cn" {
		t.Errorf("期望 cn，实际 %s", lang)
	}
}

// TestEnsureValidLanguage_英文 测试英文
func TestEnsureValidLanguage_英文(t *testing.T) {
	_, err := EnsureValidLanguage("en", 10)
	if err != nil {
		t.Errorf("en 应为有效语言，报错: %v", err)
	}
}

// TestEnsureValidLanguage_无效 测试无效语言
func TestEnsureValidLanguage_无效(t *testing.T) {
	_, err := EnsureValidLanguage("xx", 10)
	if err == nil {
		t.Error("无效语言应返回错误")
	}
}

// TestEnsureValidLanguage_超长 测试超长语言
func TestEnsureValidLanguage_超长(t *testing.T) {
	_, err := EnsureValidLanguage("cn", 1)
	if err == nil {
		t.Error("超长语言应返回错误")
	}
}

// TestEnsureValidLanguage_空 测试空语言
func TestEnsureValidLanguage_空(t *testing.T) {
	_, err := EnsureValidLanguage("", 10)
	if err == nil {
		t.Error("空语言应返回错误")
	}
}

// TestFormatRelationDefinitions_有类型 测试有类型时的格式化
func TestFormatRelationDefinitions_有类型(t *testing.T) {
	relationTypes := []registry.RelationDef{
		{
			Name:        "工作于",
			Description: map[string]string{"cn": "描述工作关系"},
			LHS:         registry.DefaultEntity,
			RHS:         registry.DefaultEntity,
		},
	}
	result := FormatRelationDefinitions(relationTypes, "cn")
	if result == "" {
		t.Error("应返回格式化后的关系定义")
	}
	if !contains(result, "工作于") {
		t.Error("结果应包含关系名称")
	}
}

// TestFormatRelationDefinitions_空列表 测试空列表
func TestFormatRelationDefinitions_空列表(t *testing.T) {
	result := FormatRelationDefinitions(nil, "cn")
	if result != "无" {
		t.Errorf("空列表应返回'无'，实际 %s", result)
	}
}

// contains 辅助函数：检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
