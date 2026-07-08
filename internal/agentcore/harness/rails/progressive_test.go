package rails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewProgressiveToolRail 验证构造函数正确初始化各字段
func TestNewProgressiveToolRail(t *testing.T) {
	config := schema.NewDeepAgentConfig()
	config.ProgressiveToolDefaultVisibleTools = []string{"read_file", "bash"}
	config.ProgressiveToolAlwaysVisibleTools = []string{"search_tools", "load_tools"}
	config.ProgressiveToolMaxLoadedTools = 20

	rail := NewProgressiveToolRail(config)

	assert.Equal(t, 90, rail.Priority())
	assert.Equal(t, 20, rail.maxLoadedTools)
	assert.Contains(t, rail.defaultVisible, "read_file")
	assert.Contains(t, rail.defaultVisible, "bash")
	assert.Contains(t, rail.alwaysVisible, "search_tools")
	assert.Contains(t, rail.alwaysVisible, "load_tools")
	assert.Empty(t, rail.metaToolNames)
	assert.Empty(t, rail.ownedToolNames)
	assert.Empty(t, rail.ownedToolIDs)
	assert.Nil(t, rail.cachedAllTools)
}

// TestNewProgressiveToolRail_默认值 验证默认配置下的行为
func TestNewProgressiveToolRail_默认值(t *testing.T) {
	config := schema.NewDeepAgentConfig()
	rail := NewProgressiveToolRail(config)

	assert.Equal(t, 12, rail.maxLoadedTools)
	assert.Empty(t, rail.defaultVisible)
	assert.Empty(t, rail.alwaysVisible)
}

// ──────────────────────────── 搜索评分 ────────────────────────────

// TestSearchTools_精确匹配 验证精确名称匹配得最高分
func TestSearchTools_精确匹配(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件内容", nil),
		cschema.NewToolInfo("write_file", "写入文件内容", nil),
		cschema.NewToolInfo("bash", "执行 shell 命令", nil),
	}

	results, err := rail.searchTools(context.Background(), "read_file", 10, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "read_file", results[0]["name"])
}

// TestSearchTools_部分匹配 验证名称包含查询得较高分
func TestSearchTools_部分匹配(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件内容", nil),
		cschema.NewToolInfo("write_file", "写入文件内容", nil),
		cschema.NewToolInfo("bash", "执行 shell 命令", nil),
	}

	results, err := rail.searchTools(context.Background(), "file", 10, 1)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestSearchTools_描述匹配 验证描述包含查询得分
func TestSearchTools_描述匹配(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("tool_a", "PDF 文档处理工具", nil),
		cschema.NewToolInfo("tool_b", "表格数据处理工具", nil),
	}

	results, err := rail.searchTools(context.Background(), "pdf", 10, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool_a", results[0]["name"])
}

// TestSearchTools_空查询 验证空查询返回空结果
func TestSearchTools_空查询(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("tool_a", "工具 A", nil),
	}

	results, err := rail.searchTools(context.Background(), "", 10, 1)
	require.NoError(t, err)
	assert.Nil(t, results)
}

// TestSearchTools_限制数量 验证 limit 参数生效
func TestSearchTools_限制数量(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("file_read", "读取", nil),
		cschema.NewToolInfo("file_write", "写入", nil),
		cschema.NewToolInfo("file_edit", "编辑", nil),
	}

	results, err := rail.searchTools(context.Background(), "file", 2, 1)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestSearchTools_详情级别 验证 detail_level 控制返回字段
func TestSearchTools_详情级别(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	params := map[string]any{
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", params),
	}

	// detail_level=1: 只有 name + description
	results1, err := rail.searchTools(context.Background(), "read_file", 1, 1)
	require.NoError(t, err)
	require.Len(t, results1, 1)
	assert.NotContains(t, results1[0], "parameter_summary")
	assert.NotContains(t, results1[0], "parameters")

	// detail_level=2: +parameter_summary
	results2, err := rail.searchTools(context.Background(), "read_file", 1, 2)
	require.NoError(t, err)
	require.Len(t, results2, 1)
	assert.Contains(t, results2[0], "parameter_summary")
	assert.NotContains(t, results2[0], "parameters")

	// detail_level=3: +parameters
	results3, err := rail.searchTools(context.Background(), "read_file", 1, 3)
	require.NoError(t, err)
	require.Len(t, results3, 1)
	assert.Contains(t, results3[0], "parameter_summary")
	assert.Contains(t, results3[0], "parameters")
}

// TestSearchTools_排除元工具 验证搜索结果不包含元工具
func TestSearchTools_排除元工具(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.metaToolNames = map[string]struct{}{
		"search_tools": {},
		"load_tools":   {},
	}
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("search_tools", "搜索工具", nil),
		cschema.NewToolInfo("load_tools", "加载工具", nil),
		cschema.NewToolInfo("read_file", "读取文件", nil),
	}

	results, err := rail.searchTools(context.Background(), "tools", 10, 1)
	require.NoError(t, err)
	for _, r := range results {
		assert.NotEqual(t, "search_tools", r["name"])
		assert.NotEqual(t, "load_tools", r["name"])
	}
}

// TestSearchTools_token匹配 验证 token 级匹配
func TestSearchTools_token匹配(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("pdf_reader", "读取 PDF 文件内容", nil),
	}

	results, err := rail.searchTools(context.Background(), "pdf read", 10, 1)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// ──────────────────────────── 可见工具管理 ────────────────────────────

// TestGetVisibleTools_无会话 验证 session 为 nil 返回空
func TestGetVisibleTools_无会话(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	assert.Nil(t, rail.getVisibleTools(nil))
}

// TestGetVisibleTools_正常读取 验证从会话状态读取可见工具列表
func TestGetVisibleTools_正常读取(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	sess := newMockSession()
	sess.state[visibleToolsKey] = []string{"read_file", "bash"}

	result := rail.getVisibleTools(sess)
	assert.Equal(t, []string{"read_file", "bash"}, result)
}

// TestSetVisibleTools_正常写入 验证写入去重保序
func TestSetVisibleTools_正常写入(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	sess := newMockSession()

	rail.setVisibleTools(sess, []string{"bash", "read_file", "bash", "edit_file"})

	result := rail.getVisibleTools(sess)
	assert.Equal(t, []string{"bash", "read_file", "edit_file"}, result)
}

// TestInitVisibleTools_首次初始化 验证首次调用正确设置可见工具
func TestInitVisibleTools_首次初始化(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.alwaysVisible = map[string]struct{}{"search_tools": {}, "load_tools": {}}
	sess := newMockSession()

	rail.initVisibleTools(sess, map[string]struct{}{"read_file": {}, "bash": {}})

	result := rail.getVisibleTools(sess)
	assert.NotEmpty(t, result)
	resultSet := toSet(result)
	assert.Contains(t, resultSet, "search_tools")
	assert.Contains(t, resultSet, "load_tools")
	assert.Contains(t, resultSet, "read_file")
	assert.Contains(t, resultSet, "bash")
}

// TestInitVisibleTools_已初始化跳过 验证重复调用不覆盖已有状态
func TestInitVisibleTools_已初始化跳过(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	sess := newMockSession()

	rail.setVisibleTools(sess, []string{"tool_a"})

	rail.initVisibleTools(sess, map[string]struct{}{"tool_b": {}})

	result := rail.getVisibleTools(sess)
	assert.Equal(t, []string{"tool_a"}, result)
}

// ──────────────────────────── loadTools ────────────────────────────

// TestLoadTools_无会话 验证 session 为 nil 返回错误信息
func TestLoadTools_无会话(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())

	result, err := rail.loadTools(context.Background(), nil, []string{"read_file"}, false)
	require.NoError(t, err)
	assert.Equal(t, "session is required for load_tools", result["message"])
}

// TestLoadTools_合并模式 验证默认合并模式
func TestLoadTools_合并模式(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.maxLoadedTools = 20
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	sess := newMockSession()
	rail.setVisibleTools(sess, []string{"read_file"})

	result, err := rail.loadTools(context.Background(), sess, []string{"write_file"}, false)
	require.NoError(t, err)

	visibleTools, ok := result["visible_tools"].([]string)
	require.True(t, ok)
	assert.Contains(t, visibleTools, "read_file")
	assert.Contains(t, visibleTools, "write_file")
}

// TestLoadTools_替换模式 验证 replace=true 替换可见集合
func TestLoadTools_替换模式(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.maxLoadedTools = 20
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	sess := newMockSession()
	rail.setVisibleTools(sess, []string{"read_file"})

	result, err := rail.loadTools(context.Background(), sess, []string{"write_file"}, true)
	require.NoError(t, err)

	visibleTools, ok := result["visible_tools"].([]string)
	require.True(t, ok)
	assert.NotContains(t, visibleTools, "read_file")
	assert.Contains(t, visibleTools, "write_file")
}

// TestLoadTools_超限截断 验证超过 maxLoadedTools 时截断
func TestLoadTools_超限截断(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.maxLoadedTools = 2
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	sess := newMockSession()

	result, err := rail.loadTools(context.Background(), sess, []string{"read_file", "write_file", "bash"}, false)
	require.NoError(t, err)

	visibleTools, ok := result["visible_tools"].([]string)
	require.True(t, ok)
	assert.Len(t, visibleTools, 2)

	skippedTools, ok := result["skipped_tools"].([]string)
	require.True(t, ok)
	assert.NotEmpty(t, skippedTools)
}

// TestLoadTools_不存在的工具 验证请求不存在的工具被跳过
func TestLoadTools_不存在的工具(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.maxLoadedTools = 20
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", nil),
	}
	sess := newMockSession()

	result, err := rail.loadTools(context.Background(), sess, []string{"nonexistent_tool"}, false)
	require.NoError(t, err)

	skippedTools, ok := result["skipped_tools"].([]string)
	require.True(t, ok)
	assert.Contains(t, skippedTools, "nonexistent_tool")
}

// ──────────────────────────── 工具分组 ────────────────────────────

// TestToolGroupForNavigation 验证工具分组推断
func TestToolGroupForNavigation(t *testing.T) {
	tests := []struct {
		name     string
		desc     string
		expected string
	}{
		{"read_file", "读取文件", "runtime"},
		{"write_file", "写入文件", "runtime"},
		{"bash", "执行命令", "runtime"},
		{"code_execute", "执行代码", "runtime"},
		{"pdf_reader", "读取PDF", "runtime"}, // "read" in name matches runtime first
		{"invoice_parser", "解析发票", "document"},
		{"pdf_process", "处理PDF文档", "document"},
		{"xlsx_generate", "生成表格", "spreadsheet"},
		{"xlsx_writer", "写入表格", "runtime"},     // "write" in name matches runtime first
		{"excel_reader", "读取Excel", "runtime"}, // "read" in name matches runtime first
		{"list_skill", "列出技能", "skill"},
		{"general_tool", "通用工具", "general"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := cschema.NewToolInfo(tc.name, tc.desc, nil)
			result := toolGroupForNavigation(info)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestToolGroupToCN 验证分组中文翻译
func TestToolGroupToCN(t *testing.T) {
	assert.Equal(t, "技能", toolGroupToCN("skill"))
	assert.Equal(t, "运行时", toolGroupToCN("runtime"))
	assert.Equal(t, "文档", toolGroupToCN("document"))
	assert.Equal(t, "表格", toolGroupToCN("spreadsheet"))
	assert.Equal(t, "通用", toolGroupToCN("general"))
	assert.Equal(t, "通用", toolGroupToCN("unknown"))
}

// TestToolSummaryForNavigation 验证导航摘要提取
func TestToolSummaryForNavigation(t *testing.T) {
	info := cschema.NewToolInfo("test", "这是一个工具描述\n第二行", nil)
	summary := toolSummaryForNavigation(info)
	assert.Equal(t, "这是一个工具描述", summary)
}

// TestToolSummaryForNavigation_空描述 验证空描述返回默认文本
func TestToolSummaryForNavigation_空描述(t *testing.T) {
	info := cschema.NewToolInfo("test", "", nil)
	summary := toolSummaryForNavigation(info)
	assert.Equal(t, "No summary available.", summary)
}

// TestToolSummaryForNavigation_截断 验证超长摘要被截断
func TestToolSummaryForNavigation_截断(t *testing.T) {
	longDesc := make([]byte, 200)
	for i := range longDesc {
		longDesc[i] = 'a'
	}
	info := cschema.NewToolInfo("test", string(longDesc), nil)
	summary := toolSummaryForNavigation(info)
	assert.LessOrEqual(t, len(summary), 160)
}

// ──────────────────────────── 参数处理 ────────────────────────────

// TestParametersSummary 验证参数摘要
func TestParametersSummary(t *testing.T) {
	assert.Equal(t, "no parameters", parametersSummary(nil))
	assert.Equal(t, "empty schema", parametersSummary(map[string]any{}))

	params := map[string]any{
		"properties": map[string]any{
			"path":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
	}
	summary := parametersSummary(params)
	assert.Contains(t, summary, "fields:")
	assert.Contains(t, summary, "path")
	assert.Contains(t, summary, "count")
}

// TestParametersToText 验证参数搜索文本
func TestParametersToText(t *testing.T) {
	params := map[string]any{
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}
	text := parametersToText(params)
	assert.Contains(t, text, "fields:")
	assert.Contains(t, text, "path")
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestToSet 验证切片转 set
func TestToSet(t *testing.T) {
	s := toSet([]string{"a", "b", "a", "c"})
	assert.Len(t, s, 3)
	assert.Contains(t, s, "a")
	assert.Contains(t, s, "b")
	assert.Contains(t, s, "c")
}

// TestDedupPreserveOrder 验证去重保序
func TestDedupPreserveOrder(t *testing.T) {
	result := dedupPreserveOrder([]string{"c", "a", "b", "a", "c"})
	assert.Equal(t, []string{"c", "a", "b"}, result)
}

// TestDedupPreserveOrder_空输入 验证空输入
func TestDedupPreserveOrder_空输入(t *testing.T) {
	result := dedupPreserveOrder(nil)
	assert.Empty(t, result)
}

// TestBuildToolSummary 验证工具摘要构建
func TestBuildToolSummary(t *testing.T) {
	info := cschema.NewToolInfo("test", "测试工具", map[string]any{
		"properties": map[string]any{
			"input": map[string]any{"type": "string"},
		},
	})

	s1 := buildToolSummary(info, 1)
	assert.Equal(t, "test", s1["name"])
	assert.Equal(t, "测试工具", s1["description"])
	assert.NotContains(t, s1, "parameter_summary")
	assert.NotContains(t, s1, "parameters")

	s2 := buildToolSummary(info, 2)
	assert.Contains(t, s2, "parameter_summary")

	s3 := buildToolSummary(info, 3)
	assert.Contains(t, s3, "parameters")
}

// ──────────────────────────── GetCallbacks ────────────────────────────

// TestProgressiveToolRail_GetCallbacks 验证回调映射包含 BeforeInvoke 和 BeforeModelCall
func TestProgressiveToolRail_GetCallbacks(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	callbacks := rail.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeInvoke)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// ──────────────────────────── BeforeModelCall 工具过滤 ────────────────────────────

// TestBeforeModelCall_工具过滤 验证 BeforeModelCall 正确过滤不可见工具
func TestBeforeModelCall_工具过滤(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.alwaysVisible = map[string]struct{}{"search_tools": {}, "load_tools": {}}
	rail.metaToolNames = map[string]struct{}{"search_tools": {}, "load_tools": {}}
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("search_tools", "搜索工具", nil),
		cschema.NewToolInfo("load_tools", "加载工具", nil),
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}

	sess := newMockSession()
	rail.setVisibleTools(sess, []string{"read_file"})

	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("search_tools", "搜索工具", nil),
		cschema.NewToolInfo("load_tools", "加载工具", nil),
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
	}
	modelInputs := &agentinterfaces.ModelCallInputs{
		Tools: tools,
	}

	agent := newFakeBaseAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, modelInputs, sess)

	err := rail.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	filtered := modelInputs.Tools
	names := make([]string, len(filtered))
	for i, t := range filtered {
		names[i] = t.GetName()
	}

	assert.Contains(t, names, "search_tools")
	assert.Contains(t, names, "load_tools")
	assert.Contains(t, names, "read_file")
	assert.NotContains(t, names, "bash")
	assert.NotContains(t, names, "write_file")
}

// TestBeforeModelCall_alwaysVisible 验证 always_visible 工具始终通过过滤
func TestBeforeModelCall_alwaysVisible(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.alwaysVisible = map[string]struct{}{"bash": {}}
	rail.metaToolNames = map[string]struct{}{}
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("bash", "执行命令", nil),
	}

	sess := newMockSession()

	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("bash", "执行命令", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
	}
	modelInputs := &agentinterfaces.ModelCallInputs{
		Tools: tools,
	}

	agent := newFakeBaseAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, modelInputs, sess)

	err := rail.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	filtered := modelInputs.Tools
	names := make([]string, len(filtered))
	for i, t := range filtered {
		names[i] = t.GetName()
	}
	assert.Contains(t, names, "bash")
	assert.NotContains(t, names, "write_file")
}

// ──────────────────────────── 导航条目构建 ────────────────────────────

// TestBuildNavigationEntries 验证导航条目包含 baseline 和 loaded 工具
func TestBuildNavigationEntries(t *testing.T) {
	rail := NewProgressiveToolRail(schema.NewDeepAgentConfig())
	rail.alwaysVisible = map[string]struct{}{"search_tools": {}}
	rail.defaultVisible = map[string]struct{}{"read_file": {}}
	rail.cachedAllTools = []cschema.ToolInfoInterface{
		cschema.NewToolInfo("read_file", "读取文件", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
		cschema.NewToolInfo("write_file", "写入文件", nil),
	}

	sess := newMockSession()
	rail.setVisibleTools(sess, []string{"write_file"})

	entries := rail.buildNavigationEntries(sess, "cn")
	assert.NotEmpty(t, entries)
	joined := strings.Join(entries, " ")
	assert.Contains(t, joined, "read_file")
	assert.Contains(t, joined, "write_file")
}

// ──────────────────────────── Mock 实现 ────────────────────────────

// mockSession 简单的 SessionFacade mock
type mockSession struct {
	state map[string]any
}

func newMockSession() *mockSession {
	return &mockSession{state: make(map[string]any)}
}

func (m *mockSession) GetSessionID() string { return "test-session" }

func (m *mockSession) UpdateState(data map[string]any) {
	for k, v := range data {
		m.state[k] = v
	}
}

func (m *mockSession) GetState(key state.StateKey) (any, error) {
	if key.Type() == state.StateKeyString {
		path := key.String()
		// StateKey.String() 格式为 "StateKey(path)"
		const prefix = "StateKey("
		const suffix = ")"
		if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) {
			path = path[len(prefix) : len(path)-len(suffix)]
		}
		val, ok := m.state[path]
		if !ok {
			return nil, nil
		}
		return val, nil
	}
	return nil, nil
}

func (m *mockSession) DumpState() map[string]any                        { return m.state }
func (m *mockSession) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *mockSession) Interact(_ context.Context, _ any) error          { return nil }

// 编译时验证 mockSession 满足 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockSession)(nil)

// fakeBaseAgent 满足 BaseAgent 接口的测试 mock
type fakeBaseAgent struct {
	cbMgr   *agentinterfaces.AgentCallbackManager
	builder *saprompt.SystemPromptBuilder
}

func newFakeBaseAgent() *fakeBaseAgent {
	return &fakeBaseAgent{
		cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
		builder: saprompt.NewSystemPromptBuilder(),
	}
}

func (f *fakeBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{
		BaseCard: cschema.BaseCard{ID: "test-agent", Name: "TestAgent"},
	}
}
func (f *fakeBaseAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if f.builder == nil {
		return nil
	}
	return f.builder
}
func (f *fakeBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeBaseAgent)(nil)
