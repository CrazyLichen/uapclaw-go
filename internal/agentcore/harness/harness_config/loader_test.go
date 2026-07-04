package harness_config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNormalizeContent_nil 测试 nil 内容规范化
func TestNormalizeContent_nil(t *testing.T) {
	result := normalizeContent(nil)
	assert.Equal(t, map[string]string{}, result)
}

// TestNormalizeContent_string 测试字符串内容规范化
func TestNormalizeContent_string(t *testing.T) {
	result := normalizeContent("hello")
	assert.Equal(t, map[string]string{"cn": "hello", "en": "hello"}, result)
}

// TestNormalizeContent_map 测试 map[string]string 内容规范化
func TestNormalizeContent_map(t *testing.T) {
	result := normalizeContent(map[string]string{"cn": "你好", "en": "hello"})
	assert.Equal(t, map[string]string{"cn": "你好", "en": "hello"}, result)
}

// TestNormalizeContent_mapAny 测试 map[string]any 内容规范化
func TestNormalizeContent_mapAny(t *testing.T) {
	result := normalizeContent(map[string]any{"cn": "你好", "en": "hello"})
	assert.Equal(t, map[string]string{"cn": "你好", "en": "hello"}, result)
}

// TestNormalizeContent_空字符串 测试空字符串内容规范化
func TestNormalizeContent_空字符串(t *testing.T) {
	result := normalizeContent("")
	assert.Equal(t, map[string]string{"cn": "", "en": ""}, result)
}

// TestRenderTemplate_无占位符 测试无占位符的文本渲染
func TestRenderTemplate_无占位符(t *testing.T) {
	result, err := renderTemplate("hello world", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

// TestRenderTemplate_简单替换 测试简单变量替换
func TestRenderTemplate_简单替换(t *testing.T) {
	result, err := renderTemplate("path: {{ workspace_root }}", map[string]any{"workspace_root": "/tmp"})
	require.NoError(t, err)
	assert.Equal(t, "path: /tmp", result)
}

// TestRenderTemplate_多个变量 测试多个变量替换
func TestRenderTemplate_多个变量(t *testing.T) {
	result, err := renderTemplate("{{ name }} in {{ location }}", map[string]any{
		"name":     "agent",
		"location": "/workspace",
	})
	require.NoError(t, err)
	assert.Equal(t, "agent in /workspace", result)
}

// TestRenderTemplate_空文本 测试空文本渲染
func TestRenderTemplate_空文本(t *testing.T) {
	result, err := renderTemplate("", nil)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// TestCapitalize 测试首字母大写
func TestCapitalize(t *testing.T) {
	assert.Equal(t, "Workspace_root", capitalize("workspace_root"))
	assert.Equal(t, "Name", capitalize("name"))
	assert.Equal(t, "", capitalize(""))
}

// TestDirOf 测试路径目录提取
func TestDirOf(t *testing.T) {
	assert.Equal(t, "/home/user", dirOf("/home/user/config.yaml"))
	assert.Equal(t, "/tmp", dirOf("/tmp/test.yaml"))
	assert.Equal(t, ".", dirOf("file.yaml"))
}

// TestHarnessConfigLoader_Load_基本 测试基本加载流程
func TestHarnessConfigLoader_Load_基本(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
name: 测试Agent
language: cn
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.Config)
	assert.Equal(t, "harness_config.v0.1", resolved.Config.SchemaVersion)
	assert.NotNil(t, resolved.Config.ID)
	assert.Equal(t, "test-agent", *resolved.Config.ID)
	assert.NotNil(t, resolved.Config.Name)
	assert.Equal(t, "测试Agent", *resolved.Config.Name)
	assert.Equal(t, "cn", resolved.Config.Language)
}

// TestHarnessConfigLoader_Load_带提示词 测试带提示词段的加载
func TestHarnessConfigLoader_Load_带提示词(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: identity
      content:
        cn: 你是一个编码助手
        en: You are a coding assistant
    - name: rules
      priority: 20
      content: 遵循编码规范
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)

	// identity 段 → system_prompt
	require.NotNil(t, resolved.SystemPrompt)
	assert.Equal(t, "你是一个编码助手", *resolved.SystemPrompt)

	// rules 段 → extra_sections
	require.Len(t, resolved.ExtraSections, 1)
	assert.Equal(t, "rules", resolved.ExtraSections[0].Name)
	assert.Equal(t, 20, resolved.ExtraSections[0].Priority)
}

// TestHarnessConfigLoader_Load_文件型段 测试文件型段加载
func TestHarnessConfigLoader_Load_文件型段(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: agent_md
      file: AGENT.md
      content:
        cn: "# Agent 指南"
        en: "# Agent Guide"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)

	require.Len(t, resolved.FileSections, 1)
	assert.Equal(t, "AGENT.md", resolved.FileSections[0].Filename)
	assert.Equal(t, "# Agent 指南", resolved.FileSections[0].Content["cn"])
}

// TestHarnessConfigLoader_Load_模板渲染 测试模板渲染
func TestHarnessConfigLoader_Load_模板渲染(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: identity
      content: "工作目录: {{ workspace_root }}"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.SystemPrompt)
	// workspace_root 应被替换为配置文件所在目录
	assert.Contains(t, *resolved.SystemPrompt, "工作目录:")
	assert.NotContains(t, *resolved.SystemPrompt, "{{ workspace_root }}")
}

// TestHarnessConfigLoader_Load_自定义参数 测试自定义渲染参数
func TestHarnessConfigLoader_Load_自定义参数(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: identity
      content: "Agent: {{ agent_name }}"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, map[string]any{"agent_name": "DeepAgent"})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.SystemPrompt)
	assert.Contains(t, *resolved.SystemPrompt, "DeepAgent")
}

// TestHarnessConfigLoader_Load_资源 测试资源配置加载
func TestHarnessConfigLoader_Load_资源(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
resources:
  tools:
    - type: builtin
      names:
        - filesystem
        - shell
  rails:
    - type: builtin
      name: task_planning
  mcps:
    - type: stdio
      command: npx
      args:
        - -y
        - "@modelcontextprotocol/server-filesystem"
      env:
        NODE_PATH: /usr/local/lib/node_modules
  skills:
    dirs:
      - ./skills
    mode: all
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.Config.Resources)

	// 工具
	require.Len(t, resolved.Config.Resources.Tools, 1)
	assert.Equal(t, "builtin", resolved.Config.Resources.Tools[0].Type)
	assert.Equal(t, []string{"filesystem", "shell"}, resolved.Config.Resources.Tools[0].Names)

	// Rails
	require.Len(t, resolved.Config.Resources.Rails, 1)
	assert.Equal(t, "builtin", resolved.Config.Resources.Rails[0].Type)

	// MCPs
	require.Len(t, resolved.Config.Resources.Mcps, 1)
	assert.Equal(t, "stdio", resolved.Config.Resources.Mcps[0].Type)
	assert.Equal(t, "npx", resolved.Config.Resources.Mcps[0].Command)

	// Skills
	require.NotNil(t, resolved.Config.Resources.Skills)
	assert.Equal(t, []string{"./skills"}, resolved.Config.Resources.Skills.Dirs)
	assert.Equal(t, "all", resolved.Config.Resources.Skills.Mode)
}

// TestHarnessConfigLoader_Load_英文语言 测试英文语言系统提示词选择
func TestHarnessConfigLoader_Load_英文语言(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: en
prompts:
  sections:
    - name: identity
      content:
        cn: 你是一个编码助手
        en: You are a coding assistant
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.SystemPrompt)
	assert.Equal(t, "You are a coding assistant", *resolved.SystemPrompt)
}

// TestHarnessConfigLoader_Load_文件不存在 测试文件不存在
func TestHarnessConfigLoader_Load_文件不存在(t *testing.T) {
	loader := HarnessConfigLoader{}
	_, err := loader.Load("/nonexistent/path.yaml", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "读取")
}

// TestHarnessConfigLoader_Load_无效YAML 测试无效 YAML
func TestHarnessConfigLoader_Load_无效YAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("invalid: [yaml: content"), 0644))

	loader := HarnessConfigLoader{}
	_, err := loader.Load(cfgPath, nil)
	assert.Error(t, err)
}

// TestHarnessConfigLoader_Load_默认优先级 测试段默认优先级
func TestHarnessConfigLoader_Load_默认优先级(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: custom_section
      content: 自定义内容
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.Len(t, resolved.ExtraSections, 1)
	assert.Equal(t, DefaultSectionPriority, resolved.ExtraSections[0].Priority)
}

// TestResolvedHarnessConfig_完整管线 测试从 YAML 到 ResolvedHarnessConfig 的完整管线
func TestResolvedHarnessConfig_完整管线(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: full-pipeline
name: 完整管线测试
description: 测试所有功能
language: cn
max_iterations: 25
completion_timeout: 300.0
meta:
  owner: test-owner
  tags:
    - test
    - pipeline
  visibility: internal
workspace:
  root_path: ./
prompts:
  sections:
    - name: identity
      priority: 10
      content:
        cn: 你是一个编码助手
        en: You are a coding assistant
    - name: rules
      priority: 20
      content:
        cn: 遵循编码规范
        en: Follow coding standards
    - name: agent_md
      file: AGENT.md
      content:
        cn: "# Agent 指南"
        en: "# Agent Guide"
resources:
  tools:
    - type: builtin
      names:
        - filesystem
  rails:
    - type: builtin
      name: task_planning
  mcps:
    - type: stdio
      command: npx
      args:
        - "-y"
        - "@modelcontextprotocol/server-filesystem"
  skills:
    dirs:
      - ./skills
    mode: all
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)

	// 配置
	assert.Equal(t, "harness_config.v0.1", resolved.Config.SchemaVersion)
	assert.Equal(t, "cn", resolved.Config.Language)
	require.NotNil(t, resolved.Config.MaxIterations)
	assert.Equal(t, 25, *resolved.Config.MaxIterations)

	// 系统提示词
	require.NotNil(t, resolved.SystemPrompt)
	assert.Equal(t, "你是一个编码助手", *resolved.SystemPrompt)

	// 额外段
	require.Len(t, resolved.ExtraSections, 1)
	assert.Equal(t, "rules", resolved.ExtraSections[0].Name)
	assert.Equal(t, 20, resolved.ExtraSections[0].Priority)

	// 文件型段
	require.Len(t, resolved.FileSections, 1)
	assert.Equal(t, "AGENT.md", resolved.FileSections[0].Filename)

	// 源路径
	assert.NotEmpty(t, resolved.SourcePath)
}

// TestHarnessConfigLoader_Load_字符串内容段 测试字符串型段内容
func TestHarnessConfigLoader_Load_字符串内容段(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	// 注意：YAML 中 content 为字符串时需要特殊处理
	yamlContent := `schema_version: harness_config.v0.1
id: string-content
language: cn
prompts:
  sections:
    - name: identity
      content: "你是一个助手"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.SystemPrompt)
	assert.Equal(t, "你是一个助手", *resolved.SystemPrompt)
}

// TestHarnessConfigLoader_Load_workspaceRoot参数 测试 workspaceRoot 参数覆盖
func TestHarnessConfigLoader_Load_workspaceRoot参数(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")

	yamlContent := `schema_version: harness_config.v0.1
id: test-agent
language: cn
prompts:
  sections:
    - name: identity
      content: "root: {{ workspace_root }}"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(cfgPath, map[string]any{}, "/custom/workspace")
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.SystemPrompt)
	assert.Contains(t, *resolved.SystemPrompt, "/custom/workspace")
}
