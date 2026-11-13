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
