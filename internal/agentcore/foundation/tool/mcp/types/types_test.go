package types

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// fakeMcpClient 用于编译检查 McpClient 接口的模拟实现
type fakeMcpClient struct{}

func (f *fakeMcpClient) Connect(_ context.Context, _ ...ConnectOption) error { return nil }
func (f *fakeMcpClient) Disconnect(_ context.Context) error                  { return nil }
func (f *fakeMcpClient) ListTools(_ context.Context) ([]*McpToolCard, error) { return nil, nil }
func (f *fakeMcpClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}
func (f *fakeMcpClient) GetToolInfo(_ context.Context, _ string) (*McpToolCard, error) {
	return nil, nil
}
func (f *fakeMcpClient) ListResources(_ context.Context) ([]any, error) { return nil, nil }
func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}
func (f *fakeMcpClient) Close() error { return nil }

// TestMcpClient_接口编译检查 验证 fakeMcpClient 满足 McpClient 接口
func TestMcpClient_接口编译检查(t *testing.T) {
	var _ McpClient = &fakeMcpClient{}
}

// TestNewConnectOptions_默认值 验证无选项时 Timeout 默认为 NoTimeout
func TestNewConnectOptions_默认值(t *testing.T) {
	opts := NewConnectOptions()
	if opts.Timeout != NoTimeout {
		t.Errorf("Timeout = %v, want %v", opts.Timeout, NoTimeout)
	}
	if opts.RetryTimes != 0 {
		t.Errorf("RetryTimes = %v, want 0", opts.RetryTimes)
	}
}

// TestNewConnectOptions_设置重试次数 验证 WithRetryTimes 选项生效
func TestNewConnectOptions_设置重试次数(t *testing.T) {
	opts := NewConnectOptions(WithRetryTimes(3))
	if opts.RetryTimes != 3 {
		t.Errorf("RetryTimes = %v, want 3", opts.RetryTimes)
	}
}

// TestNewConnectOptions_设置超时时间 验证 WithConnectTimeout 选项生效
func TestNewConnectOptions_设置超时时间(t *testing.T) {
	opts := NewConnectOptions(WithConnectTimeout(30.5))
	if opts.Timeout != 30.5 {
		t.Errorf("Timeout = %v, want 30.5", opts.Timeout)
	}
}

// TestNewConnectOptions_多个选项 验证多个选项同时生效
func TestNewConnectOptions_多个选项(t *testing.T) {
	opts := NewConnectOptions(WithRetryTimes(5), WithConnectTimeout(10.0))
	if opts.RetryTimes != 5 {
		t.Errorf("RetryTimes = %v, want 5", opts.RetryTimes)
	}
	if opts.Timeout != 10.0 {
		t.Errorf("Timeout = %v, want 10.0", opts.Timeout)
	}
}

// TestNewMcpServerConfig_默认值 验证 ClientType 默认为 "sse"，ServerID 自动生成
func TestNewMcpServerConfig_默认值(t *testing.T) {
	cfg := NewMcpServerConfig("test-server", "http://localhost:8080/sse", "")
	if cfg.ClientType != "sse" {
		t.Errorf("ClientType = %q, want %q", cfg.ClientType, "sse")
	}
	if cfg.ServerID == "" {
		t.Error("ServerID 不应为空")
	}
	if cfg.ServerName != "test-server" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "test-server")
	}
	if cfg.ServerPath != "http://localhost:8080/sse" {
		t.Errorf("ServerPath = %q, want %q", cfg.ServerPath, "http://localhost:8080/sse")
	}
}

// TestNewMcpServerConfig_指定ClientType 验证显式指定 ClientType 生效
func TestNewMcpServerConfig_指定ClientType(t *testing.T) {
	cfg := NewMcpServerConfig("my-server", "npx", "stdio")
	if cfg.ClientType != "stdio" {
		t.Errorf("ClientType = %q, want %q", cfg.ClientType, "stdio")
	}
}

// TestNewMcpServerConfig_选项生效 验证 WithServerID/WithParams/WithAuthHeaders/WithAuthQueryParams 选项
func TestNewMcpServerConfig_选项生效(t *testing.T) {
	params := map[string]any{"args": []string{"--port", "3000"}}
	headers := map[string]string{"Authorization": "Bearer token"}
	queryParams := map[string]string{"key": "value"}

	cfg := NewMcpServerConfig("srv", "path", "stdio",
		WithServerID("custom-id"),
		WithParams(params),
		WithAuthHeaders(headers),
		WithAuthQueryParams(queryParams),
	)

	if cfg.ServerID != "custom-id" {
		t.Errorf("ServerID = %q, want %q", cfg.ServerID, "custom-id")
	}
	if cfg.Params["args"] == nil {
		t.Error("Params 应包含 args")
	}
	if cfg.AuthHeaders["Authorization"] != "Bearer token" {
		t.Error("AuthHeaders 未正确设置")
	}
	if cfg.AuthQueryParams["key"] != "value" {
		t.Error("AuthQueryParams 未正确设置")
	}
}

// TestMcpServerConfig_Ability接口 验证 McpServerConfig 实现 schema.Ability 接口
func TestMcpServerConfig_Ability接口(t *testing.T) {
	cfg := NewMcpServerConfig("my-srv", "path", "sse", WithServerID("id-1"))

	if cfg.AbilityName() != "my-srv" {
		t.Errorf("AbilityName = %q, want %q", cfg.AbilityName(), "my-srv")
	}
	if cfg.AbilityID() != "id-1" {
		t.Errorf("AbilityID = %q, want %q", cfg.AbilityID(), "id-1")
	}
	if cfg.AbilityKind() != schema.AbilityKindMcpServer {
		t.Errorf("AbilityKind = %v, want AbilityKindMcpServer", cfg.AbilityKind())
	}
}

// TestNewMcpToolCard_基本创建 验证 McpToolCard 创建及字段赋值
func TestNewMcpToolCard_基本创建(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("city", "城市名", true),
	}
	card := NewMcpToolCard("weather", "查询天气", "weather-server", params)

	if card.Name != "weather" {
		t.Errorf("Name = %q, want %q", card.Name, "weather")
	}
	if card.Description != "查询天气" {
		t.Errorf("Description = %q, want %q", card.Description, "查询天气")
	}
	if card.ServerName != "weather-server" {
		t.Errorf("ServerName = %q, want %q", card.ServerName, "weather-server")
	}
	if card.ServerID != "" {
		t.Errorf("ServerID 应为空，got %q", card.ServerID)
	}
	if len(card.InputParams) != 1 {
		t.Errorf("InputParams 长度 = %d, want 1", len(card.InputParams))
	}
}

// TestNewMcpToolCard_设置ServerID 验证 WithMcpToolCardServerID 选项生效
func TestNewMcpToolCard_设置ServerID(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "srv", nil, WithMcpToolCardServerID("sid-123"))
	if card.ServerID != "sid-123" {
		t.Errorf("ServerID = %q, want %q", card.ServerID, "sid-123")
	}
}

// TestMcpToolCard_ToolInfo 验证 ToolInfo 返回 McpToolInfo 且 ServerName 正确
func TestMcpToolCard_ToolInfo(t *testing.T) {
	card := NewMcpToolCard("tool1", "描述", "my-server", nil, WithMcpToolCardServerID("sid"))
	info := card.ToolInfo()

	if info == nil {
		t.Fatal("ToolInfo 不应为 nil")
	}
	if info.Name != "tool1" {
		t.Errorf("Name = %q, want %q", info.Name, "tool1")
	}
	if info.ServerName != "my-server" {
		t.Errorf("ServerName = %q, want %q", info.ServerName, "my-server")
	}
	if info.Type != "function" {
		t.Errorf("Type = %q, want %q", info.Type, "function")
	}
}

// TestMcpToolCard_ToolInfo_带参数 验证 ToolInfo 的 Parameters 字段正确生成
func TestMcpToolCard_ToolInfo_带参数(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("q", "查询关键词", true),
		schema.NewIntegerParam("limit", "结果数量", false),
	}
	card := NewMcpToolCard("search", "搜索", "search-srv", params)
	info := card.ToolInfo()

	if info.Parameters == nil {
		t.Fatal("Parameters 不应为 nil")
	}
	props, ok := info.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("Properties 类型断言失败")
	}
	if len(props) != 2 {
		t.Errorf("Properties 长度 = %d, want 2", len(props))
	}
}

// TestNoTimeout_常量值 验证 NoTimeout 常量值为 -1
func TestNoTimeout_常量值(t *testing.T) {
	if NoTimeout != -1 {
		t.Errorf("NoTimeout = %v, want -1", NoTimeout)
	}
}
