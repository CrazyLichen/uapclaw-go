package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// 本文件覆盖 15.5 新增的 manifest 类型（extension_spec.go）。
// 完整 schema 校验属 15.14 / 15.15a，此处只验 15.5 的 factory 用到的字段与编解码。

// TestAgentTemplateSpec_JSON往返 上游 manifest 的典型形状。
func TestAgentTemplateSpec_JSON往返(t *testing.T) {
	raw := `{
		"agent_card": {"id": "a1", "name": "expert", "description": "领域专家"},
		"model": {
			"model_client_config": {"api_base": "https://llm/v1", "api_key": "k", "client_provider": "openai"},
			"model_request_config": {"model_name": "gpt-x"}
		},
		"prompt_sections": [
			{"name": "identity", "content": {"cn": "你是专家。", "en": "You are an expert."}, "priority": 10}
		],
		"mcps": [{"server_name": "fs", "type": "stdio", "command": "node", "args": ["s.js"], "cwd": "/w",
		          "env": {"K": "V"}}],
		"skills": [{"dir": "/skills/a", "mode": "auto_list", "enabled_skills": ["x"]}],
		"tools": [{"name": "t"}],
		"rails": [{"type": "core.ask_user"}],
		"subagents": [{"agent_card": {"name": "sub"}}],
		"metadata": {"owner": "team-a"}
	}`
	var spec AgentTemplateSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))

	assert.Equal(t, "a1", spec.AgentCard.ID)
	assert.Equal(t, "expert", spec.AgentCard.Name)
	require.NotNil(t, spec.Model)
	require.NotNil(t, spec.Model.ModelClientConfig)
	assert.Equal(t, "https://llm/v1", spec.Model.ModelClientConfig.APIBase)
	assert.Equal(t, "openai", spec.Model.ModelClientConfig.ClientProvider)
	require.NotNil(t, spec.Model.ModelRequestConfig)
	assert.Equal(t, "gpt-x", spec.Model.ModelRequestConfig.ModelName)

	require.Len(t, spec.PromptSections, 1)
	assert.Equal(t, 10, spec.PromptSections[0].Priority)
	assert.Equal(t, "你是专家。", spec.PromptSections[0].Content["cn"])
	assert.Equal(t, "You are an expert.", spec.PromptSections[0].Content["en"])

	require.Len(t, spec.Mcps, 1)
	assert.Equal(t, "node", spec.Mcps[0].Command)
	require.Len(t, spec.Mcps[0].Args, 1)
	assert.Equal(t, map[string]string{"K": "V"}, spec.Mcps[0].Env)

	require.Len(t, spec.Skills, 1)
	assert.Equal(t, "auto_list", spec.Skills[0].Mode)
	assert.Equal(t, []string{"x"}, spec.Skills[0].EnabledSkills)

	assert.Len(t, spec.Tools, 1)
	assert.Len(t, spec.Rails, 1)
	require.Len(t, spec.Subagents, 1)
	assert.Equal(t, "sub", spec.Subagents[0].AgentCard.Name)
	assert.Equal(t, "team-a", spec.Metadata["owner"])
}

// TestAgentTemplateSpec_ModelDump 转 map 供 native 系 provider 的 agent_template。
// Python: factory.py:306 manifest.model_dump(mode="json")
func TestAgentTemplateSpec_ModelDump(t *testing.T) {
	spec := AgentTemplateSpec{
		AgentCard: mustAgentCard(t, "a1", "expert"),
		Mcps:      []McpServerSpec{{ServerName: "fs", Type: "stdio", Command: "node"}},
	}
	dumped := spec.ModelDump()
	card, ok := dumped["agent_card"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "a1", card["id"])
	mcps, ok := dumped["mcps"].([]any)
	require.True(t, ok)
	require.Len(t, mcps, 1)
}

// TestMcpServerSpec_三种传输类型 上游 type 取值为
// stdio / sse / streamable_http（不是 http）。
func TestMcpServerSpec_三种传输类型(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantTyp string
	}{
		{"stdio", `{"type":"stdio","server_name":"a","command":"node"}`, "stdio"},
		{"sse", `{"type": "sse", "server_name": "b", "url": "https://x/sse"}`, "sse"},
		{"streamable_http", `{"type": "streamable_http", "server_name": "c", "url": "https://x/mcp"}`, "streamable_http"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var spec McpServerSpec
			require.NoError(t, json.Unmarshal([]byte(testCase.raw), &spec))
			assert.Equal(t, testCase.wantTyp, spec.Type)
		})
	}
}

// TestMcpServerSpec_全部字段 11 个字段（对齐上游 extension_spec.py:52-62）。
func TestMcpServerSpec_全部字段(t *testing.T) {
	raw := `{
		"type": "streamable_http", "server_name": "n", "server_id": "i",
		"url": "https://x", "command": "cmd", "args": ["a"], "env": {"K": "V"},
		"cwd": "/w", "params": {"p": 1}, "auth_headers": {"H": "v"},
		"auth_query_params": {"Q": "v"}
	}`
	var spec McpServerSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	assert.Equal(t, "streamable_http", spec.Type)
	assert.Equal(t, "n", spec.ServerName)
	assert.Equal(t, "i", spec.ServerID)
	assert.Equal(t, "https://x", spec.URL)
	assert.Equal(t, "cmd", spec.Command)
	assert.Equal(t, []string{"a"}, spec.Args)
	assert.Equal(t, map[string]string{"K": "V"}, spec.Env)
	assert.Equal(t, "/w", spec.Cwd)
	assert.Equal(t, map[string]any{"p": float64(1)}, spec.Params)
	assert.Equal(t, map[string]string{"H": "v"}, spec.AuthHeaders)
	assert.Equal(t, map[string]string{"Q": "v"}, spec.AuthQueryParams)
}

// TestSkillSpec_ModelDump exclude_none 语义：零值字段不出现。
// Python: factory.py:311 skill.model_dump(exclude_none=True)
func TestSkillSpec_ModelDump(t *testing.T) {
	assert.Equal(t, map[string]any{"dir": "/s"}, SkillSpec{Dir: "/s"}.ModelDump())

	withAll := SkillSpec{Dir: "/s", Mode: "auto_list", EnabledSkills: []string{"a"}}
	dumped := withAll.ModelDump()
	assert.Equal(t, "/s", dumped["dir"])
	assert.Equal(t, "auto_list", dumped["mode"])
	assert.Equal(t, []string{"a"}, dumped["enabled_skills"])
}

// TestPromptSectionSpec_默认priority 未填时为零值 0（上游默认 100，
// 由 15.15a 的 schema 校验补齐；15.5 只做结构）。
func TestPromptSectionSpec_结构(t *testing.T) {
	raw := `{"name":"identity","content":{"cn":"中文","en":"English"}}`
	var spec PromptSectionSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	assert.Equal(t, "identity", spec.Name)
	assert.Equal(t, "中文", spec.Content["cn"])
	assert.Nil(t, spec.RenderParams)
}

// mustAgentCard 构造一个带 id 与 name 的 AgentCard 值。
func mustAgentCard(t *testing.T, id, name string) saschema.AgentCard {
	t.Helper()
	card := saschema.NewAgentCard(saschema.WithAgentID(id), saschema.WithAgentName(name))
	require.NotNil(t, card)
	return *card
}

// TestAgentTemplateSpec_ModelDump含零值字段 JSON 编码不含 omitempty 的字段
// 即使为零值也会出现（agent_card 无 omitempty）。
func TestAgentTemplateSpec_ModelDump零值(t *testing.T) {
	dumped := AgentTemplateSpec{}.ModelDump()
	// agent_card 是必填字段（无 omitempty），零值也序列化成空对象
	assert.Contains(t, dumped, "agent_card")
}

// ──────────────────────────── 15.15a 全量 schema ────────────────────────────

// TestPluginSpec_JSON往返 对齐 Python extension_spec.py:174-198 PluginSpec。
func TestPluginSpec_JSON往返(t *testing.T) {
	raw := `{
		"id": "p1", "name": "插件", "description": "功能包",
		"prompt_sections": [{"name": "sops", "content": {"cn": "守则"}}],
		"tools": [{"type": "harness.tool.file", "params": {"file_path": "/abs/tool.py"}}],
		"mcps": [{"server_name": "fs", "type": "stdio", "command": "node"}],
		"rails": [{"type": "core.ask_user", "params": {}}],
		"skills": [{"dir": "/abs/skills/a", "mode": "all"}],
		"metadata": {"publisher": "ops"}
	}`
	var spec PluginSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	assert.Equal(t, "p1", spec.ID)
	assert.Equal(t, "插件", spec.Name)
	assert.Equal(t, "功能包", spec.Description)
	require.Len(t, spec.PromptSections, 1)
	require.Len(t, spec.Tools, 1)
	assert.Equal(t, "harness.tool.file", spec.Tools[0].Type)
	assert.Equal(t, "/abs/tool.py", spec.Tools[0].Params["file_path"])
	require.Len(t, spec.Mcps, 1)
	require.Len(t, spec.Rails, 1)
	require.Len(t, spec.Skills, 1)
	assert.Equal(t, "ops", spec.Metadata["publisher"])
}

// TestPluginSpec_ID必填 对齐 pydantic 必填字段：id 缺失时报错。
// Python 侧由 BaseModel 校验；Go 侧 JSON 解码无必填概念，由 ValidatePluginSpec 补。
func TestPluginSpec_ID必填(t *testing.T) {
	err := ValidatePluginSpec(PluginSpec{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

// TestMcpDirSpec_JSON 对齐 Python extension_spec.py:70-74。
func TestMcpDirSpec_JSON(t *testing.T) {
	var spec McpDirSpec
	require.NoError(t, json.Unmarshal([]byte(`{"dir": "mcp/"}`), &spec))
	assert.Equal(t, "mcp/", spec.Dir)
}

// TestRubricSpec_未知字段保留 extra="allow" 语义：任意字段原样保留。
// Python: extension_spec.py:123-126 RubricSpec
func TestRubricSpec_未知字段保留(t *testing.T) {
	var spec RubricSpec
	require.NoError(t, json.Unmarshal([]byte(`{"name":"质量","score":5,"nested":{"k":"v"}}`), &spec))
	assert.Equal(t, "质量", spec["name"])
	assert.Equal(t, float64(5), spec["score"])
	assert.Equal(t, map[string]any{"k": "v"}, spec["nested"])
}

// TestMemorySpec_未知字段保留 extra="allow" 语义。
// Python: extension_spec.py:129-132 MemorySpec
func TestMemorySpec_未知字段保留(t *testing.T) {
	var spec MemorySpec
	require.NoError(t, json.Unmarshal([]byte(`{"name":"约定","content":"永不删除测试"}`), &spec))
	assert.Equal(t, "约定", spec["name"])
}

// TestValidatePluginPaths_相对路径报错 对齐 Python :208-236 validate_plugin_paths。
func TestValidatePluginPaths_相对路径报错(t *testing.T) {
	cases := []struct {
		name  string
		spec  PluginSpec
		field string
	}{
		{"tools.params.file_path", PluginSpec{Tools: []BuiltinToolSpec{
			{Type: "harness.tool.file", Params: map[string]any{"file_path": "rel/tool.py"}}}}, "tools.params.file_path"},
		{"rails.params.file_path", PluginSpec{Rails: []RailSpec{
			{Type: "harness.rail.file", Params: map[string]any{"file_path": "rel/rail.py"}}}}, "rails.params.file_path"},
		{"mcps.cwd", PluginSpec{Mcps: []McpServerSpec{{Cwd: "rel/dir"}}}, "mcps.cwd"},
		{"skills.dir", PluginSpec{Skills: []SkillSpec{{Dir: "rel/skill"}}}, "skills.dir"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidatePluginPaths(testCase.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.field)
			assert.Contains(t, err.Error(), "absolute")
		})
	}
}

// TestValidatePluginPaths_绝对路径与空值通过 空字段跳过、绝对路径放行、~ 展开判绝对。
func TestValidatePluginPaths_绝对路径与空值通过(t *testing.T) {
	spec := PluginSpec{
		Tools:  []BuiltinToolSpec{{Type: "harness.tool.file", Params: map[string]any{"file_path": "/abs/t.py"}}},
		Rails:  []RailSpec{{Type: "harness.rail.file", Params: map[string]any{"file_path": "/abs/r.py"}}},
		Mcps:   []McpServerSpec{{Cwd: "/abs/dir"}},
		Skills: []SkillSpec{{Dir: "/abs/skill"}},
	}
	require.NoError(t, ValidatePluginPaths(spec))

	// 空字符串跳过（Python: if file_path / if mcp.cwd）
	spec2 := PluginSpec{Mcps: []McpServerSpec{{Cwd: ""}}}
	assert.NoError(t, ValidatePluginPaths(spec2))
}

// TestValidatePluginPaths_runtimeSdkPaths 对齐 Python :231-234。
func TestValidatePluginPaths_runtimeSdkPaths(t *testing.T) {
	spec := PluginSpec{} // PluginSpec 无 runtime；AgentTemplateSpec 才有
	tpl := AgentTemplateSpec{Runtime: &AgentRuntimeSpec{ProviderName: "claude-code", SdkPaths: []string{"rel/sdk"}}}
	err := ValidatePluginPaths(tpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime.sdk_paths")

	assert.NoError(t, ValidatePluginPaths(spec))
}

// TestValidatePluginPaths_subagent递归 对齐 Python :235-236 直接子模板递归。
func TestValidatePluginPaths_subagent递归(t *testing.T) {
	tpl := AgentTemplateSpec{Subagents: []AgentTemplateSpec{{
		Skills: []SkillSpec{{Dir: "rel/sub-skill"}},
	}}}
	err := ValidatePluginPaths(tpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.dir")
}

// TestValidatePlainMapping_拒绝非字符串键 对齐 Python :17-30 _plain_data。
func TestValidatePlainMapping_拒绝非字符串键(t *testing.T) {
	bad := map[string]any{"k": map[any]any{1: "x"}}
	err := ValidatePlainMapping(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strings")
}

// TestValidatePlainMapping_标量与列表通过 JSON/YAML 可序列化数据放行。
func TestValidatePlainMapping_标量与列表通过(t *testing.T) {
	assert.NoError(t, ValidatePlainMapping(map[string]any{
		"a": 1, "b": []any{"x", 2.5, true, nil}, "c": map[string]any{"d": "e"},
	}))
}
