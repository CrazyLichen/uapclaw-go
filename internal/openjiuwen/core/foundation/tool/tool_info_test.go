package tool

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolCard_ToolInfo_无参数(t *testing.T) {
	card := NewToolCard("test_tool", "测试工具", nil, nil)
	info := card.ToolInfo()
	if info.GetType() != "function" {
		t.Errorf("GetType() = %q, want function", info.GetType())
	}
	if info.GetName() != "test_tool" {
		t.Errorf("GetName() = %q, want test_tool", info.GetName())
	}
	if info.GetDescription() != "测试工具" {
		t.Errorf("GetDescription() = %q, want 测试工具", info.GetDescription())
	}
	params, ok := info.GetParameters()["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters.properties 类型不正确")
	}
	if len(params) != 0 {
		t.Errorf("Parameters.properties 应为空，实际有 %d 项", len(params))
	}
}

func TestToolCard_ToolInfo_带参数(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("city", "城市名", true),
		schema.NewIntegerParam("days", "预报天数", false),
	}
	card := NewToolCard("weather", "查询天气", params, nil)
	info := card.ToolInfo()

	props := info.GetParameters()["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("properties 数量 = %d, want 2", len(props))
	}

	citySchema := props["city"].(map[string]any)
	if citySchema["type"] != "string" {
		t.Errorf("city type = %v, want string", citySchema["type"])
	}

	required := info.GetParameters()["required"].([]string)
	if len(required) != 1 || required[0] != "city" {
		t.Errorf("required = %v, want [city]", required)
	}
}
