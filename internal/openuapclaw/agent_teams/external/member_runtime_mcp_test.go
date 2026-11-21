package external

import (
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// TestMergeHostMcpServers_合并与能力声明 对齐 Python _host_context 的 mcp
// 半区（member_runtime.py:492-506）：bind_mcp_servers 追加的 server 合并进
// 下发 context，非空即声明宿主 MCP_SERVERS 能力。
func TestMergeHostMcpServers_合并与能力声明(t *testing.T) {
	runtime := &ExternalHarnessMemberRuntime{}
	url := "http://127.0.0.1:9/mcp"
	runtime.BindMCPServers([]harnessprotocol.McpServerConfig{
		{Name: "openuapclaw-team", Transport: harnessprotocol.McpTransportStdio,
			Command: []string{"uapclawmcpserver"}},
	})
	hctx := &harnessprotocol.HarnessContext{
		AgentName: "m", AgentID: "m1", HostSessionID: "h1",
		McpServers: []harnessprotocol.McpServerConfig{
			{Name: "remote", Transport: harnessprotocol.McpTransportHTTP, URL: &url},
		},
	}
	merged := runtime.mergeHostMcpServers(hctx)
	if len(merged.McpServers) != 2 {
		t.Fatalf("应合并为 2 个 server: %+v", merged.McpServers)
	}
	if merged.McpServers[0].Name != "remote" || merged.McpServers[1].Name != "openuapclaw-team" {
		t.Fatalf("context 在前 extra 在后: %+v", merged.McpServers)
	}
	if !merged.HostCapabilities[harnessprotocol.HostCapabilityMcpServers] {
		t.Fatalf("应声明 MCP_SERVERS 宿主能力: %+v", merged.HostCapabilities)
	}
}

// TestMergeHostMcpServers_空追加不改context 无追加时 context 原样返回且
// 不声明宿主能力（对齐 Python `if mcp_servers:` 不命中分支）。
func TestMergeHostMcpServers_空追加不改context(t *testing.T) {
	runtime := &ExternalHarnessMemberRuntime{}
	hctx := &harnessprotocol.HarnessContext{
		AgentName: "m", AgentID: "m1", HostSessionID: "h1",
	}
	merged := runtime.mergeHostMcpServers(hctx)
	if len(merged.McpServers) != 0 {
		t.Fatalf("不应注入 server: %+v", merged.McpServers)
	}
	if merged.HostCapabilities[harnessprotocol.HostCapabilityMcpServers] {
		t.Fatal("不应声明 MCP_SERVERS 宿主能力")
	}
}
