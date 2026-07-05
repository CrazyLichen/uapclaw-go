package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewToolInfo_基本创建 验证 NewToolInfo 创建实例及默认 Type
func TestNewToolInfo_基本创建(t *testing.T) {
	info := NewToolInfo("search", "搜索工具", map[string]any{"type": "object"})
	if info.Type != "function" {
		t.Errorf("Type = %q, want %q", info.Type, "function")
	}
	if info.Name != "search" {
		t.Errorf("Name = %q, want %q", info.Name, "search")
	}
	if info.Description != "搜索工具" {
		t.Errorf("Description = %q, want %q", info.Description, "搜索工具")
	}
	if info.Parameters["type"] != "object" {
		t.Errorf("Parameters[type] = %v, want object", info.Parameters["type"])
	}
}

// TestNewToolInfo_nil参数 验证 parameters 为 nil 时初始化为空 map
func TestNewToolInfo_nil参数(t *testing.T) {
	info := NewToolInfo("tool", "desc", nil)
	if info.Parameters == nil {
		t.Error("Parameters 不应为 nil")
	}
	if len(info.Parameters) != 0 {
		t.Errorf("Parameters 长度 = %d, want 0", len(info.Parameters))
	}
}

// TestToolInfo_Getter方法 验证 ToolInfo 的 ToolInfoInterface getter 方法
func TestToolInfo_Getter方法(t *testing.T) {
	info := NewToolInfo("tool", "描述", map[string]any{"type": "object"})
	if info.GetType() != "function" {
		t.Errorf("GetType() = %q, want function", info.GetType())
	}
	if info.GetName() != "tool" {
		t.Errorf("GetName() = %q, want tool", info.GetName())
	}
	if info.GetDescription() != "描述" {
		t.Errorf("GetDescription() = %q, want 描述", info.GetDescription())
	}
	if info.GetParameters()["type"] != "object" {
		t.Errorf("GetParameters()[type] = %v, want object", info.GetParameters()["type"])
	}
}

// TestToolInfo_ToolInfoInterface接口 验证 ToolInfo 满足 ToolInfoInterface 接口
func TestToolInfo_ToolInfoInterface接口(t *testing.T) {
	var _ ToolInfoInterface = &ToolInfo{}
	var _ ToolInfoInterface = NewToolInfo("tool", "desc", nil)
}

// TestNewMcpToolInfo_基本创建 验证 NewMcpToolInfo 创建实例及字段赋值
func TestNewMcpToolInfo_基本创建(t *testing.T) {
	params := map[string]any{"type": "object"}
	info := NewMcpToolInfo("weather", "天气查询", "weather-server", params)
	if info.Type != "function" {
		t.Errorf("Type = %q, want %q", info.Type, "function")
	}
	if info.Name != "weather" {
		t.Errorf("Name = %q, want %q", info.Name, "weather")
	}
	if info.Description != "天气查询" {
		t.Errorf("Description = %q, want %q", info.Description, "天气查询")
	}
	if info.ServerName != "weather-server" {
		t.Errorf("ServerName = %q, want %q", info.ServerName, "weather-server")
	}
	if info.Parameters["type"] != "object" {
		t.Errorf("Parameters[type] = %v, want object", info.Parameters["type"])
	}
}

// TestNewMcpToolInfo_nil参数 验证 parameters 为 nil 时初始化为空 map
func TestNewMcpToolInfo_nil参数(t *testing.T) {
	info := NewMcpToolInfo("tool", "desc", "srv", nil)
	if info.Parameters == nil {
		t.Error("Parameters 不应为 nil")
	}
	if len(info.Parameters) != 0 {
		t.Errorf("Parameters 长度 = %d, want 0", len(info.Parameters))
	}
}

// TestMcpToolInfo_Getter方法 验证 McpToolInfo 通过嵌入 ToolInfo 自动满足 ToolInfoInterface
func TestMcpToolInfo_Getter方法(t *testing.T) {
	info := NewMcpToolInfo("tool", "描述", "srv", map[string]any{"type": "object"})
	if info.GetType() != "function" {
		t.Errorf("GetType() = %q, want function", info.GetType())
	}
	if info.GetName() != "tool" {
		t.Errorf("GetName() = %q, want tool", info.GetName())
	}
	if info.GetDescription() != "描述" {
		t.Errorf("GetDescription() = %q, want 描述", info.GetDescription())
	}
	if info.GetParameters()["type"] != "object" {
		t.Errorf("GetParameters()[type] = %v, want object", info.GetParameters()["type"])
	}
}

// TestMcpToolInfo_ToolInfoInterface接口 验证 McpToolInfo 满足 ToolInfoInterface 接口
func TestMcpToolInfo_ToolInfoInterface接口(t *testing.T) {
	var _ ToolInfoInterface = &McpToolInfo{}
	var _ ToolInfoInterface = NewMcpToolInfo("tool", "desc", "srv", nil)
}

// TestToolInfo_JSON序列化 验证 ToolInfo JSON 序列化输出
func TestToolInfo_JSON序列化(t *testing.T) {
	info := NewToolInfo("tool", "描述", map[string]any{"type": "object"})
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if result["type"] != "function" {
		t.Errorf("type = %v, want function", result["type"])
	}
	if result["name"] != "tool" {
		t.Errorf("name = %v, want tool", result["name"])
	}
}

// TestMcpToolInfo_JSON序列化 验证 McpToolInfo JSON 序列化时 ServerName 正确输出
func TestMcpToolInfo_JSON序列化(t *testing.T) {
	info := NewMcpToolInfo("tool", "描述", "my-server", nil)
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if result["server_name"] != "my-server" {
		t.Errorf("server_name = %v, want my-server", result["server_name"])
	}
}

// TestMcpToolInfo_JSON序列化_ServerName为空时省略 验证 ServerName 为空时 JSON 中 omitempty 生效
func TestMcpToolInfo_JSON序列化_ServerName为空时省略(t *testing.T) {
	info := &McpToolInfo{
		ToolInfo: ToolInfo{
			Type:        "function",
			Name:        "tool",
			Description: "desc",
			Parameters:  map[string]any{},
		},
		ServerName: "",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if _, exists := result["server_name"]; exists {
		t.Error("server_name 应被 omitempty 省略")
	}
}
