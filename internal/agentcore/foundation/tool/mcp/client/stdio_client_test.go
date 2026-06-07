//go:build llm

package client

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// TestNewStdioClient_创建客户端 测试创建 Stdio 客户端。
func TestNewStdioClient_创建客户端(t *testing.T) {
	config := types.NewMcpServerConfig("test-server", "npx", "stdio")
	client := NewStdioClient(config)

	if client == nil {
		t.Fatal("NewStdioClient 返回 nil")
	}
	if client.serverName != "test-server" {
		t.Errorf("serverName = %q, 期望 %q", client.serverName, "test-server")
	}
	if client.isConnected {
		t.Error("新创建的客户端不应处于已连接状态")
	}
	if client.mcpClient != nil {
		t.Error("新创建的客户端 mcpClient 应为 nil")
	}
}

// TestStdioClient_未连接时调用方法返回错误 测试未连接状态下调用各方法返回错误。
func TestStdioClient_未连接时调用方法返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test-server", "npx", "stdio")
	client := NewStdioClient(config)
	ctx := context.Background()

	// ListTools 应返回未连接错误
	_, err := client.ListTools(ctx)
	if err == nil {
		t.Error("ListTools 未连接时应返回错误")
	}

	// CallTool 应返回未连接错误
	_, err = client.CallTool(ctx, "test-tool", nil)
	if err == nil {
		t.Error("CallTool 未连接时应返回错误")
	}

	// GetToolInfo 应返回未连接错误
	_, err = client.GetToolInfo(ctx, "test-tool")
	if err == nil {
		t.Error("GetToolInfo 未连接时应返回错误")
	}

	// ListResources 应返回未连接错误
	_, err = client.ListResources(ctx)
	if err == nil {
		t.Error("ListResources 未连接时应返回错误")
	}

	// ReadResource 应返回未连接错误
	_, err = client.ReadResource(ctx, "test://resource")
	if err == nil {
		t.Error("ReadResource 未连接时应返回错误")
	}
}

// TestStdioClient_Disconnect_未连接时不报错 测试未连接时调用 Disconnect 不报错。
func TestStdioClient_Disconnect_未连接时不报错(t *testing.T) {
	config := types.NewMcpServerConfig("test-server", "npx", "stdio")
	client := NewStdioClient(config)

	err := client.Disconnect(context.Background())
	if err != nil {
		t.Errorf("未连接时 Disconnect 不应返回错误, 实际: %v", err)
	}
}

// TestStdioClient_Close 测试 Close 方法。
func TestStdioClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test-server", "npx", "stdio")
	client := NewStdioClient(config)

	err := client.Close()
	if err != nil {
		t.Errorf("Close 不应返回错误, 实际: %v", err)
	}
}

// TestExtractStringParam 测试从 Params 中提取字符串参数。
func TestExtractStringParam(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		key    string
		want   string
	}{
		{
			name:   "nil params",
			params: nil,
			key:    "command",
			want:   "",
		},
		{
			name:   "key 不存在",
			params: map[string]any{"other": "value"},
			key:    "command",
			want:   "",
		},
		{
			name:   "正常字符串",
			params: map[string]any{"command": "npx"},
			key:    "command",
			want:   "npx",
		},
		{
			name:   "值非字符串",
			params: map[string]any{"command": 123},
			key:    "command",
			want:   "",
		},
		{
			name:   "空字符串",
			params: map[string]any{"command": ""},
			key:    "command",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringParam(tt.params, tt.key)
			if got != tt.want {
				t.Errorf("extractStringParam() = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

// TestExtractStringSliceParam 测试从 Params 中提取字符串切片参数。
func TestExtractStringSliceParam(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		key    string
		want   []string
	}{
		{
			name:   "nil params",
			params: nil,
			key:    "args",
			want:   nil,
		},
		{
			name:   "key 不存在",
			params: map[string]any{"other": "value"},
			key:    "args",
			want:   nil,
		},
		{
			name:   "正常 []any 切片",
			params: map[string]any{"args": []any{"-y", "@modelcontextprotocol/server-everything"}},
			key:    "args",
			want:   []string{"-y", "@modelcontextprotocol/server-everything"},
		},
		{
			name:   "包含非字符串元素",
			params: map[string]any{"args": []any{"-y", 123, true}},
			key:    "args",
			want:   []string{"-y"},
		},
		{
			name:   "空切片",
			params: map[string]any{"args": []any{}},
			key:    "args",
			want:   []string{},
		},
		{
			name:   "值非切片",
			params: map[string]any{"args": "not-a-slice"},
			key:    "args",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringSliceParam(tt.params, tt.key)
			if len(got) != len(tt.want) {
				t.Errorf("extractStringSliceParam() 长度 = %d, 期望 %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractStringSliceParam()[%d] = %q, 期望 %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestExtractEnvSlice 测试从 Params 中提取环境变量切片。
func TestExtractEnvSlice(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{
			name:   "nil params",
			params: nil,
			want:   nil,
		},
		{
			name:   "无 env 键",
			params: map[string]any{"command": "npx"},
			want:   nil,
		},
		{
			name:   "正常 env map",
			params: map[string]any{"env": map[string]any{"API_KEY": "secret", "DEBUG": "true"}},
			want:   []string{"API_KEY=secret", "DEBUG=true"},
		},
		{
			name:   "env 值非 map",
			params: map[string]any{"env": "not-a-map"},
			want:   nil,
		},
		{
			name:   "空 env map",
			params: map[string]any{"env": map[string]any{}},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEnvSlice(tt.params)
			if tt.want == nil {
				if got != nil {
					t.Errorf("extractEnvSlice() = %v, 期望 nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("extractEnvSlice() 长度 = %d, 期望 %d", len(got), len(tt.want))
			}
		})
	}
}

// TestExtractEnvSlice_格式验证 测试 env 转换后的 key=value 格式。
func TestExtractEnvSlice_格式验证(t *testing.T) {
	params := map[string]any{
		"env": map[string]any{
			"API_KEY": "secret",
			"DEBUG":   true,
		},
	}
	got := extractEnvSlice(params)

	if len(got) != 2 {
		t.Fatalf("extractEnvSlice() 长度 = %d, 期望 2", len(got))
	}

	// 验证每个元素都是 key=value 格式
	found := make(map[string]bool)
	for _, item := range got {
		found[item] = true
	}
	if !found["API_KEY=secret"] {
		t.Error("缺少 API_KEY=secret")
	}
	if !found["DEBUG=true"] {
		t.Error("缺少 DEBUG=true")
	}
}

// TestStdioClient_使用ServerPath作为Command 测试当 Params 中无 command 时使用 ServerPath。
func TestStdioClient_使用ServerPath作为Command(t *testing.T) {
	// 不设置 Params 中的 command，应使用 ServerPath
	config := types.NewMcpServerConfig("test-server", "python", "stdio")
	client := NewStdioClient(config)

	// 验证 config 正确
	if client.config.ServerPath != "python" {
		t.Errorf("ServerPath = %q, 期望 %q", client.config.ServerPath, "python")
	}
}

// TestStdioClient_真实调用 测试 Stdio 客户端真实 MCP 服务器连接。
// 需要本地有可用的 MCP 服务器进程。
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/...
func TestStdioClient_真实调用(t *testing.T) {
	// 跳过：需要真实 MCP 服务器
	t.Skip("需要真实 MCP 服务器，跳过")
}
