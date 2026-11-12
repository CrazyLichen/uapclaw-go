package harness_providers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 本文件对应 Python tests 里 mcp_tool_naming_preamble 的输出形状
// （mcp_naming.py:27-46）。

// TestMcpToolNamingPreamble_空集合返回空串 对齐 mcp_naming.py:38-39。
func TestMcpToolNamingPreamble_空集合返回空串(t *testing.T) {
	assert.Equal(t, "", McpToolNamingPreamble(nil))
	assert.Equal(t, "", McpToolNamingPreamble(map[string]string{}))
}

// TestMcpToolNamingPreamble_单服务器 逐字比对完整输出。
func TestMcpToolNamingPreamble_单服务器(t *testing.T) {
	got := McpToolNamingPreamble(map[string]string{"fs": "mcp__fs__{tool}"})
	want := "<mcp-tools>\n" +
		`<server name="fs" tool-name="mcp__fs__{tool}"/>` + "\n" +
		"</mcp-tools>\n" +
		"Each line above is how you address that server's tools: put the tool's own name where " +
		"{tool} is. Where an instruction names one of those tools without that prefix, " +
		"it means the tool of that server, never a built-in tool of yours whose name resembles it."
	assert.Equal(t, want, got)
}

// TestMcpToolNamingPreamble_多服务器顺序稳定 Go 的 map 无序，
// 实现按服务器名升序渲染。Python 的 dict 保插入顺序，Go 用排序换确定性。
func TestMcpToolNamingPreamble_多服务器顺序稳定(t *testing.T) {
	patterns := map[string]string{
		"beta":  "mcp__beta__{tool}",
		"alpha": "mcp__alpha__{tool}",
	}
	first := McpToolNamingPreamble(patterns)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, McpToolNamingPreamble(patterns), "第 %d 次输出不一致", i)
	}
	alphaIdx := strings.Index(first, "alpha")
	betaIdx := strings.Index(first, "beta")
	assert.Less(t, alphaIdx, betaIdx, "server 必须按名字升序排列")
}

// TestToolPlaceholder_字面量 TOOL_PLACEHOLDER 是协议值，不得改。
func TestToolPlaceholder_字面量(t *testing.T) {
	assert.Equal(t, "{tool}", ToolPlaceholder)
}

// TestMcpToolNamingPreamble_每行独立 server 元素数量与输入一致。
func TestMcpToolNamingPreamble_每行独立(t *testing.T) {
	got := McpToolNamingPreamble(map[string]string{
		"a": "mcp__a__{tool}",
		"b": "mcp__b.{tool}",
		"c": "mcp__c__{tool}",
	})
	assert.Equal(t, 3, strings.Count(got, "<server "), "三个服务器应有三行 <server>")
	assert.Equal(t, 1, strings.Count(got, "<mcp-tools>"))
	assert.Equal(t, 1, strings.Count(got, "</mcp-tools>"))
}
