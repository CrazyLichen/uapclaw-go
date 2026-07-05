package harness_config

import (
	"os"
	"testing"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sasc "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuiltinToolGroups 测试内置工具组注册表
func TestBuiltinToolGroups(t *testing.T) {
	expectedGroups := []string{"filesystem", "shell", "code", "web_search", "web_fetch"}
	for _, group := range expectedGroups {
		def, ok := builtinToolGroups[group]
		assert.True(t, ok, "缺少内置工具组: %s", group)
		assert.NotEmpty(t, def.ModulePath)
		assert.NotEmpty(t, def.ClassNames)
	}
}

// TestBuiltinRailRegistry 测试内置 Rail 注册表
func TestBuiltinRailRegistry(t *testing.T) {
	_, ok := builtinRailRegistry["task_planning"]
	assert.True(t, ok, "缺少内置 Rail: task_planning")
}

// TestToolDottedToGroup 测试反转工具注册表
func TestToolDottedToGroup(t *testing.T) {
	key := "openjiuwen.harness.tools.filesystem.ReadFileTool"
	group, ok := toolDottedToGroup[key]
	assert.True(t, ok)
	assert.Equal(t, "filesystem", group)

	key2 := "openjiuwen.harness.tools.shell.BashTool"
	group2, ok := toolDottedToGroup[key2]
	assert.True(t, ok)
	assert.Equal(t, "shell", group2)
}

// TestRailDottedToName 测试反转 Rail 注册表
func TestRailDottedToName(t *testing.T) {
	key := "openjiuwen.harness.rails.task_planning_rail.TaskPlanningRail"
	name, ok := railDottedToName[key]
	assert.True(t, ok)
	assert.Equal(t, "task_planning", name)
}

// TestResolveBuiltinTools_未知组 测试未知内置工具组
func TestResolveBuiltinTools_未知组(t *testing.T) {
	_, err := resolveBuiltinTools("nonexistent", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的内置工具组")
}

// TestResolveBuiltinTools_已知组 测试已知内置工具组（当前返回未实现错误）
func TestResolveBuiltinTools_已知组(t *testing.T) {
	_, err := resolveBuiltinTools("filesystem", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "尚未实现")
}

// TestResolveTools_nil 测试 nil 资源
func TestResolveTools_nil(t *testing.T) {
	tools, err := resolveTools(nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, tools)
}

// TestResolveTools_builtin 测试内置工具解析
func TestResolveTools_builtin(t *testing.T) {
	resources := &ResourcesSchema{
		Tools: []ToolResourceSchema{
			{Type: "builtin", Names: []string{"filesystem"}},
		},
	}
	_, err := resolveTools(resources, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "尚未实现")
}

// TestResolveTools_package 测试 package 类型工具
func TestResolveTools_package(t *testing.T) {
	module := "my.module"
	className := "MyTool"
	resources := &ResourcesSchema{
		Tools: []ToolResourceSchema{
			{Type: "package", Module: &module, ClassName: &className},
		},
	}
	_, err := resolveTools(resources, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "package 类型工具加载尚未实现")
}

// TestResolveTools_entryPoint 测试 entry_point 类型工具
func TestResolveTools_entryPoint(t *testing.T) {
	name := "my_tool"
	resources := &ResourcesSchema{
		Tools: []ToolResourceSchema{
			{Type: "entry_point", Name: &name},
		},
	}
	_, err := resolveTools(resources, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entry_point 类型工具加载尚未实现")
}

// TestResolveRails_nil 测试 nil 资源 Rail 解析
func TestResolveRails_nil(t *testing.T) {
	rails, err := resolveRails(nil)
	assert.NoError(t, err)
	assert.Nil(t, rails)
}

// TestResolveRails_未知Rail 测试未知内置 Rail
func TestResolveRails_未知Rail(t *testing.T) {
	name := "nonexistent_rail"
	resources := &ResourcesSchema{
		Rails: []RailResourceSchema{
			{Type: "builtin", Name: &name},
		},
	}
	_, err := resolveRails(resources)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的内置 Rail")
}

// TestResolveRails_已知Rail 测试已知内置 Rail（当前返回未实现错误）
func TestResolveRails_已知Rail(t *testing.T) {
	name := "task_planning"
	resources := &ResourcesSchema{
		Rails: []RailResourceSchema{
			{Type: "builtin", Name: &name},
		},
	}
	_, err := resolveRails(resources)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "尚未实现")
}

// TestResolveMcps_nil 测试 nil 资源 MCP 解析
func TestResolveMcps_nil(t *testing.T) {
	mcps, err := resolveMcps(nil)
	assert.NoError(t, err)
	assert.Nil(t, mcps)
}

// TestResolveMcps_基本 测试基本 MCP 解析
func TestResolveMcps_基本(t *testing.T) {
	resources := &ResourcesSchema{
		Mcps: []McpResourceSchema{
			{
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
				Env:     map[string]string{"NODE_PATH": "/usr/lib"},
			},
		},
	}
	mcps, err := resolveMcps(resources)
	require.NoError(t, err)
	require.Len(t, mcps, 1)
	assert.Equal(t, "npx", mcps[0].ServerName)
	assert.Equal(t, "stdio", mcps[0].ClientType)
	assert.Contains(t, mcps[0].ServerPath, "npx")
}

// TestResolveMcps_空命令 测试空命令 MCP
func TestResolveMcps_空命令(t *testing.T) {
	resources := &ResourcesSchema{
		Mcps: []McpResourceSchema{
			{Type: "sse"},
		},
	}
	mcps, err := resolveMcps(resources)
	require.NoError(t, err)
	require.Len(t, mcps, 1)
	assert.Equal(t, "mcp_server", mcps[0].ServerName)
	assert.Equal(t, "sse", mcps[0].ClientType)
}

// TestWriteFileSections_基本 测试基本文件段写入
func TestWriteFileSections_基本(t *testing.T) {
	dir := t.TempDir()

	fileSections := []ResolvedFileSection{
		{
			Filename: "AGENT.md",
			Content:  map[string]string{"cn": "# Agent 指南", "en": "# Agent Guide"},
		},
	}

	err := writeFileSections(fileSections, dir, "cn")
	require.NoError(t, err)

	data, err := os.ReadFile(dir + "/AGENT.md")
	require.NoError(t, err)
	assert.Equal(t, "# Agent 指南", string(data))
}

// TestWriteFileSections_英文 测试英文文件段写入
func TestWriteFileSections_英文(t *testing.T) {
	dir := t.TempDir()

	fileSections := []ResolvedFileSection{
		{
			Filename: "AGENT.md",
			Content:  map[string]string{"cn": "# Agent 指南", "en": "# Agent Guide"},
		},
	}

	err := writeFileSections(fileSections, dir, "en")
	require.NoError(t, err)

	data, err := os.ReadFile(dir + "/AGENT.md")
	require.NoError(t, err)
	assert.Equal(t, "# Agent Guide", string(data))
}

// TestWriteFileSections_空内容 测试空内容文件段跳过
func TestWriteFileSections_空内容(t *testing.T) {
	dir := t.TempDir()

	fileSections := []ResolvedFileSection{
		{
			Filename: "AGENT.md",
			Content:  map[string]string{"cn": "", "en": ""},
		},
	}

	err := writeFileSections(fileSections, dir, "cn")
	require.NoError(t, err)

	// 空内容不应创建文件
	_, err = os.ReadFile(dir + "/AGENT.md")
	assert.True(t, os.IsNotExist(err))
}

// TestWriteFileSections_语言回退 测试语言回退（cn → en）
func TestWriteFileSections_语言回退(t *testing.T) {
	dir := t.TempDir()

	fileSections := []ResolvedFileSection{
		{
			Filename: "AGENT.md",
			Content:  map[string]string{"cn": "# 中文指南", "en": "# English Guide"},
		},
	}

	// 请求日语（不存在），回退到 cn
	err := writeFileSections(fileSections, dir, "ja")
	require.NoError(t, err)

	data, err := os.ReadFile(dir + "/AGENT.md")
	require.NoError(t, err)
	assert.Equal(t, "# 中文指南", string(data))
}

// TestToolsToYAMLSpecs 测试工具反向映射
func TestToolsToYAMLSpecs(t *testing.T) {
	tools := []*tool.ToolCard{
		{BaseCard: *cschema.NewBaseCard(cschema.WithID("ReadFileTool"), cschema.WithName("filesystem"))},
	}
	specs := toolsToYAMLSpecs(tools)
	assert.NotEmpty(t, specs)
}

// TestToolsToYAMLSpecs_空 测试空工具列表
func TestToolsToYAMLSpecs_空(t *testing.T) {
	specs := toolsToYAMLSpecs(nil)
	assert.Empty(t, specs)
}

// TestRailsToYAMLSpecs_空 测试空 Rail 列表
func TestRailsToYAMLSpecs_空(t *testing.T) {
	specs := railsToYAMLSpecs(nil)
	assert.Empty(t, specs)
}

// TestHarnessConfigBuilder_Build 测试 Builder.Build 桩实现
func TestHarnessConfigBuilder_Build(t *testing.T) {
	builder := HarnessConfigBuilder{}
	resolved := &ResolvedHarnessConfig{
		Config: &HarnessConfig{SchemaVersion: DefaultSchemaVersion, Language: "cn"},
	}
	err := builder.Build(resolved, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create_deep_agent 尚未实现")
}

// TestCreateSysOperation 测试 createSysOperation 桩实现
func TestCreateSysOperation(t *testing.T) {
	_, err := createSysOperation(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "尚未实现")
}

// TestGenerateHarnessConfigYAML_基本 测试基本 YAML 生成
func TestGenerateHarnessConfigYAML_基本(t *testing.T) {
	yamlStr, err := GenerateHarnessConfigYAML(nil, nil, nil, nil, "cn", nil, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "schema_version")
	assert.Contains(t, yamlStr, "cn")
}

// TestGenerateHarnessConfigYAML_带AgentCard 测试带 AgentCard 的 YAML 生成
func TestGenerateHarnessConfigYAML_带AgentCard(t *testing.T) {
	card := sasc.NewAgentCard(sasc.WithAgentID("my-agent"), sasc.WithAgentName("My Agent"), sasc.WithAgentDescription("测试描述"))
	yamlStr, err := GenerateHarnessConfigYAML(card, nil, nil, nil, "en", nil, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "my-agent")
	assert.Contains(t, yamlStr, "My Agent")
	assert.Contains(t, yamlStr, "en")
}

// TestGenerateHarnessConfigYAML_带系统提示词 测试带系统提示词的 YAML 生成
func TestGenerateHarnessConfigYAML_带系统提示词(t *testing.T) {
	yamlStr, err := GenerateHarnessConfigYAML(nil, "你是一个助手", nil, nil, "cn", nil, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "identity")
	assert.Contains(t, yamlStr, "你是一个助手")
}

// TestGenerateHarnessConfigYAML_带迭代次数 测试带迭代次数的 YAML 生成
func TestGenerateHarnessConfigYAML_带迭代次数(t *testing.T) {
	maxIter := 25
	timeout := 300.0
	yamlStr, err := GenerateHarnessConfigYAML(nil, nil, nil, nil, "cn", &maxIter, &timeout, nil)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "25")
	assert.Contains(t, yamlStr, "300")
}

// TestGenerateHarnessConfigYAML_写入文件 测试 YAML 生成并写入文件
func TestGenerateHarnessConfigYAML_写入文件(t *testing.T) {
	outputPath := t.TempDir() + "/generated.yaml"
	yamlStr, err := GenerateHarnessConfigYAML(nil, nil, nil, nil, "cn", nil, nil, nil, outputPath)
	require.NoError(t, err)
	assert.NotEmpty(t, yamlStr)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "schema_version")
}

// TestGenerateHarnessConfigYAML_带ExtraSections 测试带额外段的 YAML 生成
func TestGenerateHarnessConfigYAML_带ExtraSections(t *testing.T) {
	extraSections := []map[string]any{
		{
			"name":     "custom_section",
			"priority": 20,
			"content":  map[string]string{"cn": "自定义内容"},
		},
	}
	yamlStr, err := GenerateHarnessConfigYAML(nil, nil, nil, nil, "cn", nil, nil, extraSections)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "custom_section")
}
