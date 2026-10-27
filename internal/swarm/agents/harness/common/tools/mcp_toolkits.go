package tools

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMcpTools 返回当前可用的 MCP 工具集，供 SkillDevService（及 24c MultiSessionToolkit）注入。
// 装配顺序对齐 Python：先条件装配（paid key → mcp_paid_search；free flag → mcp_free_search），
// 再固定追加 mcp_fetch_webpage + mcp_exec_command。
// Python: get_mcp_tools (mcp_toolkits.py L39-47)
func GetMcpTools() []tool.Tool {
	var tools []tool.Tool
	if hasPaidSearchAPIKey() {
		tools = append(tools, McpPaidSearch)
	}
	if isFreeSearchEnabled() {
		tools = append(tools, McpFreeSearch)
	}
	tools = append(tools, McpFetchWebpage, McpExecCommand)
	return tools
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasPaidSearchAPIKey 检查任一付费搜索 API key 已配置。
// 注意：与 mcp_search.go 的 key 检测顺序保持一致（BOCHA→PERPLEXITY→SERPER→JINA）。
// Python: _has_paid_search_api_key (mcp_toolkits.py L15-22)
func hasPaidSearchAPIKey() bool {
	for _, key := range []string{"BOCHA_API_KEY", "PERPLEXITY_API_KEY", "SERPER_API_KEY", "JINA_API_KEY"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// isFreeSearchEnabled 检查免费搜索引擎开关（DDG 或 BING 任一开启）。
// Python: _is_free_search_enabled (mcp_toolkits.py L32-36)
func isFreeSearchEnabled() bool {
	return envFlag("FREE_SEARCH_DDG_ENABLED", false) || envFlag("FREE_SEARCH_BING_ENABLED", false)
}

// envFlag 环境变量布尔解析（strip+lower 后命中 1/true/yes/on/enabled，空值回退默认）。
// 注意：与 mcp_search.go 的 searchEnvFlag 是两个函数——Python 两文件各有一份
// _env_flag（search_tools.py 与 mcp_toolkits.py），对齐 Python 文件边界：两份各自保留；
// Go 同包不能重名故分别命名。两份语义一致（真值集合、strip+lower、空回退默认）。
// Python: _env_flag (mcp_toolkits.py L25-29)
func envFlag(name string, defaultVal bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on" || raw == "enabled"
}
