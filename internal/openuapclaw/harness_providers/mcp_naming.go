package harness_providers

import (
	"sort"
	"strings"
)

// 本文件对应 Python openjiuwen/harness_providers/mcp_naming.py（49 行）：
// provider 声明「裸工具名如何解析到它的 MCP 工具」。
//
// 宿主按自己注册时的名字写提示词，而 CLI 不按那些名字显示工具——
// 它把每个 MCP 服务器放进自己的命名空间，还可能自带一个名字相似的内置工具。
// 提示词里的裸名字于是有两种读法，小模型会挑错。
//
// 只有 provider 知道自己的 CLI 怎么拼写工具（Claude Code 的
// mcp__<server>__<tool> 与 Codex 的 mcp__<server>.<tool> 连服务器名都不一致），
// 所以由 provider 声明它，紧挨着它刚注册的服务器。宿主声明自己的裸名字属于
// 哪个服务器；两边在服务器名上会合，谁都不用知道对方的规则。

// ──────────────────────────── 常量 ────────────────────────────

// ToolPlaceholder 一条服务器声明里工具自身名字的占位符。
// Python: mcp_naming.py:24 TOOL_PLACEHOLDER
//
// 这是协议值：provider 与宿主都按它拼接/替换，不得改动。
const ToolPlaceholder = "{tool}"

// ──────────────────────────── 导出函数 ────────────────────────────

// McpToolNamingPreamble 为一个会话的 MCP 服务器渲染命名声明。
// Python: mcp_naming.py:27-46 mcp_tool_naming_preamble
//
// patterns：服务器名 → 模型调用其工具时用的名字模式，每项在工具自身名字处
// 携带 ToolPlaceholder。
//
// 返回声明块；未注册任何 MCP 服务器时返回空串（mcp_naming.py:38-39）。
//
// 与上游的差异（Go 特有，已登记）：Python 的 dict 保持插入顺序，Go 的 map
// 遍历无序，故此处按服务器名升序渲染以保证输出确定——否则 golden 测试与
// 上层缓存都不可复现。
func McpToolNamingPreamble(patterns map[string]string) string {
	if len(patterns) == 0 {
		return ""
	}
	names := make([]string, 0, len(patterns))
	for name := range patterns {
		names = append(names, name)
	}
	sort.Strings(names)

	servers := make([]string, 0, len(names))
	for _, name := range names {
		servers = append(servers, `<server name="`+name+`" tool-name="`+patterns[name]+`"/>`)
	}
	// 逐字对齐 mcp_naming.py:41-46 的 f-string 拼接
	return "<mcp-tools>\n" + strings.Join(servers, "\n") + "\n</mcp-tools>\n" +
		"Each line above is how you address that server's tools: put the tool's own name where " +
		ToolPlaceholder + " is. Where an instruction names one of those tools without that prefix, " +
		"it means the tool of that server, never a built-in tool of yours whose name resembles it."
}
