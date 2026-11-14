package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 15.15 Plugin / AgentTemplate 包装载测试。
// Python: extension_loader.py:183-1132

// TestLoadPluginPackage_manifestJSON Python: :253-280 _load_plugin_manifest_json
func TestLoadPluginPackage_manifestJSON(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{
		"package_type": "plugin",
		"id": "p1",
		"name": "示例插件",
		"description": "测试",
		"prompt_sections": [{"name": "sops", "content": {"cn": "守则"}, "priority": 20}],
		"tools": [{"file": "tools/my_tool.py", "class": "MyTool"}],
		"rails": [{"file": "rails/my_rail.py", "class_name": "MyRail"}],
		"skills": [{"dir": "skills/a", "mode": "auto_list", "enabled_skills": ["x"]}],
		"metadata": {"publisher": "ops"}
	}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tools"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rails"), 0o755))
	writeFile(t, filepath.Join(dir, "tools", "my_tool.py"), "")
	writeFile(t, filepath.Join(dir, "rails", "my_rail.py"), "")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "a"), 0o755))
	writeFile(t, filepath.Join(dir, "skills", "a", "SKILL.md"), "# skill")

	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	assert.Equal(t, "p1", spec.ID)
	assert.Equal(t, "示例插件", spec.Name)
	require.Len(t, spec.PromptSections, 1)
	assert.Equal(t, 20, spec.PromptSections[0].Priority)
	require.Len(t, spec.Tools, 1)
	assert.Equal(t, "harness.tool.file", spec.Tools[0].Type)
	assert.Equal(t, filepath.Join(dir, "tools", "my_tool.py"), spec.Tools[0].Params["file_path"])
	assert.Equal(t, "MyTool", spec.Tools[0].Params["class_name"])
	require.Len(t, spec.Rails, 1)
	assert.Equal(t, "harness.rail.file", spec.Rails[0].Type)
	require.Len(t, spec.Skills, 1)
	assert.Equal(t, filepath.Join(dir, "skills", "a"), spec.Skills[0].Dir)
	assert.Equal(t, "auto_list", spec.Skills[0].Mode)
	assert.Equal(t, "ops", spec.Metadata["publisher"])
}

// TestLoadPluginPackage_packageType错误 Python: :255-257。
func TestLoadPluginPackage_packageType错误(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type": "agent_template", "id": "x"}`)
	_, err := LoadPluginPackage(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 'plugin'")
}

// TestLoadPluginPackage_禁用键 Python: :258-260 persona/agent_card/model/subagents/memories/rubrics。
func TestLoadPluginPackage_禁用键(t *testing.T) {
	for _, key := range []string{"persona", "agent_card", "model", "subagents", "memories", "rubrics"} {
		dir := t.TempDir()
		manifest := filepath.Join(dir, "manifest.json")
		writeFile(t, manifest, `{"package_type":"plugin","id":"p1","`+key+`":{"x":1}}`)
		_, err := LoadPluginPackage(manifest)
		require.Error(t, err, "key=%s", key)
		assert.Contains(t, err.Error(), "must not declare")
	}
}

// TestLoadPluginPackage_id必填 Python: :262-264。
func TestLoadPluginPackage_id必填(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type": "plugin"}`)
	_, err := LoadPluginPackage(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required 'id'")
}

// TestLoadPluginPackage_promptSectionFile Python: :456-485 _build_prompt_section_specs
// file 条目从 prompt_sections/*.md 装载，name 取 markdown 词干。
func TestLoadPluginPackage_promptSectionFile(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type":"plugin","id":"p1",
		"prompt_sections": [{"file": "prompt_sections/sops.md", "priority": 15}]}`)
	writeFile(t, filepath.Join(dir, "prompt_sections", "sops.md"), "按守则执行")

	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	require.Len(t, spec.PromptSections, 1)
	assert.Equal(t, "sops", spec.PromptSections[0].Name)
	assert.Equal(t, "按守则执行", spec.PromptSections[0].Content["cn"])
	assert.Equal(t, 15, spec.PromptSections[0].Priority)
}

// TestLoadPluginPackage_legacyYaml Python: :283-310 _load_plugin_legacy_yaml + :678-723 归一化。
func TestLoadPluginPackage_legacyYaml(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "harness_config.yaml")
	writeFile(t, manifest, `
name: legacy_plugin
description: 旧形态
language: cn
max_iterations: 50
resources:
  tools:
    - my_builtin
  skills:
    - dirs: [skills/a, skills/b]
prompts:
  sections:
    - name: identity
      content: 你是助手
`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "b"), 0o755))

	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	// id 缺省取 name（Python: normalized.setdefault("id", ...) :700）
	assert.Equal(t, "legacy_plugin", spec.ID)
	// legacy agent control 字段被丢弃（language/max_iterations 不在 canonical 字段集）
	require.Len(t, spec.Skills, 2)
	assert.Equal(t, filepath.Join(dir, "skills", "a"), spec.Skills[0].Dir)
	// prompts.sections → prompt_sections 归一化（identity 默认 priority 10）
	require.Len(t, spec.PromptSections, 1)
	assert.Equal(t, "identity", spec.PromptSections[0].Name)
	assert.Equal(t, 10, spec.PromptSections[0].Priority)
}

// TestLoadPluginPackage_legacyYaml_工具重映射 Python: :726-754 _remap_legacy_tools_to_rails。
func TestLoadPluginPackage_legacyYaml_工具重映射(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "harness_config.yaml")
	writeFile(t, manifest, `
name: remap
tools: [todo, ask_user, my_custom]
`)
	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	// todo/ask_user 是 rail provider 别名 → 移入 rails；my_custom 保留在 tools
	require.Len(t, spec.Rails, 2)
	assert.Equal(t, "core.task_planning", spec.Rails[0].Type)
	assert.Equal(t, "core.ask_user", spec.Rails[1].Type)
	require.Len(t, spec.Tools, 1)
	assert.Equal(t, "core.my_custom", spec.Tools[0].Type)
}

// TestLoadPluginPackage_expertHarnessYaml schema_version=expert_harness.v1 分支。
// Python: :681-684（不做 legacy 聚合，只归一化 mcps）
func TestLoadPluginPackage_expertHarnessYaml(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "expert_harness.yaml")
	writeFile(t, manifest, `
schema_version: expert_harness.v1
id: expert
mcps:
  - transport: sse
    name: web
    url: https://example.com/sse
`)
	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	assert.Equal(t, "expert", spec.ID)
	require.Len(t, spec.Mcps, 1)
	assert.Equal(t, "web", spec.Mcps[0].ServerName)
	assert.Equal(t, "sse", spec.Mcps[0].Type)
	assert.Equal(t, "https://example.com/sse", spec.Mcps[0].URL)
}

// TestLoadPluginPackage_mcpFile引用 Python: :830-842 file 引用展开（mcpServers 市场形态）。
func TestLoadPluginPackage_mcpFile引用(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type":"plugin","id":"p1","mcps":[{"file":"mcps.json"}]}`)
	writeFile(t, filepath.Join(dir, "mcps.json"), `{"mcpServers": {"fs": {"command": "node", "args": ["fs.js"]}}}`)

	spec, err := LoadPluginPackage(manifest)
	require.NoError(t, err)
	require.Len(t, spec.Mcps, 1)
	assert.Equal(t, "fs", spec.Mcps[0].ServerName)
	assert.Equal(t, "node", spec.Mcps[0].Command)
	// cwd 缺省取 mcp 文件所在目录（Python: entry["cwd"] = str(mcp_file.parent) :840）
	assert.Equal(t, dir, spec.Mcps[0].Cwd)
}

// TestLoadAgentTemplatePackage_全量 Python: :196-245 load_agent_template_package。
func TestLoadAgentTemplatePackage_全量(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{
		"package_type": "agent_template",
		"name": "领域专家",
		"description": "模板角色",
		"persona": {"dir": "persona"},
		"model": {"file": "model.json"},
		"tools": [{"file": "tools/t.py", "class": "T"}],
		"skills": [{"dir": "skills/s1"}],
		"subagents": [{"dir": "subagents/helper"}],
		"metadata": {"owner": "x"}
	}`)
	writeFile(t, filepath.Join(dir, "persona", "intro.md"), "# 身份")
	writeFile(t, filepath.Join(dir, "model.json"), `{"model": {"model_client_config": {"api_base": "https://x/v1"}}}`)
	writeFile(t, filepath.Join(dir, "tools", "t.py"), "")
	writeFile(t, filepath.Join(dir, "skills", "s1", "SKILL.md"), "# s")
	writeFile(t, filepath.Join(dir, "subagents", "helper", ".subagent.json"), `{
		"agent_name": "helper",
		"display_description": {"en": "helper agent", "zh": "助手"}
	}`)

	spec, err := LoadAgentTemplatePackage(manifest)
	require.NoError(t, err)
	assert.Equal(t, "领域专家", spec.AgentCard.ID)
	assert.Equal(t, "领域专家", spec.AgentCard.Name)
	assert.Equal(t, "模板角色", spec.AgentCard.Description)
	require.Len(t, spec.PromptSections, 1)
	assert.Equal(t, "identity", spec.PromptSections[0].Name)
	assert.Equal(t, "# 身份", spec.PromptSections[0].Content["cn"])
	assert.Equal(t, 10, spec.PromptSections[0].Priority)
	require.NotNil(t, spec.Model)
	require.NotNil(t, spec.Model.ModelClientConfig)
	assert.Equal(t, "https://x/v1", spec.Model.ModelClientConfig.APIBase)
	require.Len(t, spec.Tools, 1)
	require.Len(t, spec.Skills, 1)
	require.Len(t, spec.Subagents, 1)
	assert.Equal(t, "helper", spec.Subagents[0].AgentCard.Name)
	assert.Equal(t, "helper agent", spec.Subagents[0].AgentCard.Description)
	assert.Equal(t, "x", spec.Metadata["owner"])
}

// TestLoadAgentTemplatePackage_subagent禁止嵌套subagents Python: :318-322。
func TestLoadAgentTemplatePackage_subagent禁止嵌套subagents(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type":"agent_template","name":"root","description":"d",
		"subagents": [{"dir": "sub/helper"}]}`)
	writeFile(t, filepath.Join(dir, "sub", "helper", ".subagent.json"),
		`{"agent_name": "h", "subagents": [{"dir": "x"}]}`)
	_, err := LoadAgentTemplatePackage(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not declare 'subagents'")
}

// TestLoadAgentTemplatePackage_subagent禁止runtime Python: :323-327。
func TestLoadAgentTemplatePackage_subagent禁止runtime(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	writeFile(t, manifest, `{"package_type":"agent_template","name":"root","description":"d",
		"subagents": [{"dir": "sub/helper"}]}`)
	writeFile(t, filepath.Join(dir, "sub", "helper", ".subagent.json"),
		`{"agent_name": "h", "runtime": {"provider_name": "codex"}}`)
	_, err := LoadAgentTemplatePackage(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not declare 'runtime'")
}

// TestNormalizePackageMcps 模块级函数（Plugin 与 AgentTemplate 共用）。
// Python: extension_loader.py:126-132 normalize_package_mcps
func TestNormalizePackageMcps(t *testing.T) {
	dir := t.TempDir()
	normalized := NormalizePackageMcps([]any{
		map[string]any{"transport": "http", "name": "api", "url": "https://x"},
		map[string]any{"enabled": false, "name": "off"},
	}, dir)
	require.Len(t, normalized, 1)
	assert.Equal(t, "streamable_http", normalized[0].Type)
	assert.Equal(t, "api", normalized[0].ServerName)
}

// TestSectionContent cn/en 同值。Python: :1009-1010。
func TestSectionContent(t *testing.T) {
	content := sectionContent("正文")
	assert.Equal(t, map[string]string{"cn": "正文", "en": "正文"}, content)
}
