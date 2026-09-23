package entity_extraction

import (
	"testing"

	// 空白导入触发 cn/en 的 init() 注册多语言数据
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/cn"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/en"
)

// TestFormatNewEntities_基本 测试基本格式化
func TestFormatNewEntities_基本(t *testing.T) {
	entities := []map[string]any{
		{"name": "张三", "content": "工程师"},
		{"name": "华为", "content": "科技公司"},
	}
	result := FormatNewEntities(entities, "cn")
	if result == "" {
		t.Error("应返回格式化后的实体字符串")
	}
	if !contains(result, "张三") {
		t.Error("结果应包含实体名称")
	}
	if !contains(result, "工程师") {
		t.Error("结果应包含实体内容")
	}
}

// TestFormatNewEntities_空列表 测试空列表返回空字符串
func TestFormatNewEntities_空列表(t *testing.T) {
	result := FormatNewEntities(nil, "cn")
	if result != "" {
		t.Error("空列表应返回空字符串")
	}
	result = FormatNewEntities([]map[string]any{}, "cn")
	if result != "" {
		t.Error("空列表应返回空字符串")
	}
}

// TestFormatNewEntities_无content 测试无 content 时用 name 替代
func TestFormatNewEntities_无content(t *testing.T) {
	entities := []map[string]any{
		{"name": "张三"},
	}
	result := FormatNewEntities(entities, "cn")
	if result == "" {
		t.Error("应返回格式化后的实体字符串")
	}
	if !contains(result, "张三") {
		t.Error("无 content 时应用 name 替代")
	}
}
