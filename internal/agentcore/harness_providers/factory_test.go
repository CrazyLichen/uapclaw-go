package harness_providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// 本文件覆盖 factory.go 中不依赖内置 provider（native / native_v2 /
// claudecode / codex / dsh）的部分：注册表语义、manifest 转换、
// BuildHarnessContext、ManifestProviderConfig。
//
// ⏵ 15.6-15.9 / 25.1f：以下 23 条上游用例依赖五个内置 provider 真实实现，
// 本节全部挂起（见 spec §7.5）：
//   test_factory.py 9 条
//   test_provider_discovery.py 8 条
//   test_model_config.py 6 条
// factory.go 整文件登记 .coverage-exempt。

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeProvider 一个最小的 HarnessProvider 实现。
type fakeProvider struct {
	card    harnessprotocol.HarnessCard
	configs []harnessprotocol.JsonObject
}

// Card 返回 provider 元数据。
func (f *fakeProvider) Card() harnessprotocol.HarnessCard { return f.card }

// Create 记录配置并返回错误（真实 harness 由 15.6-15.9 提供）。
func (f *fakeProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	f.configs = append(f.configs, config)
	return nil, assert.AnError
}

// newFakeProvider 构造一个测试 provider。
func newFakeProvider(name string, capabilities ...harnessprotocol.HarnessCapability) *fakeProvider {
	declared := make(map[harnessprotocol.HarnessCapability]bool, len(capabilities))
	for _, capability := range capabilities {
		declared[capability] = true
	}
	return &fakeProvider{card: harnessprotocol.HarnessCard{
		Name:                       name,
		ImplementationVersion:      "1.0",
		ProtocolVersion:            harnessprotocol.ProtocolVersion,
		CompatibleProtocolVersions: map[string]bool{harnessprotocol.ProtocolVersion: true},
		Capabilities:               declared,
	}}
}

// registerFake 注册测试 provider 并在测试结束时清理。
func registerFake(t *testing.T, name string, provider *fakeProvider) {
	t.Helper()
	require.NoError(t, RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return provider, nil
	}))
	t.Cleanup(func() { UnregisterHarnessProvider(name) })
}

// ──────────────────────────── 注册表 ────────────────────────────

// TestRegisterHarnessProvider_空名报错 对齐 factory.py:93-94。
func TestRegisterHarnessProvider_空名报错(t *testing.T) {
	err := RegisterHarnessProvider("", func() (harnessprotocol.HarnessProvider, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty or built-in name")
}

// TestRegisterHarnessProvider_内置名报错 五个内置名都不可注册。
func TestRegisterHarnessProvider_内置名报错(t *testing.T) {
	for _, name := range ProviderNames {
		err := RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
			return nil, nil
		})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "empty or built-in name")
	}
}

// TestUnregisterHarnessProvider_未知名忽略 对齐 factory.py:98-100。
func TestUnregisterHarnessProvider_未知名忽略(t *testing.T) {
	UnregisterHarnessProvider("never-registered") // 不 panic 即通过
}

// TestAvailableProviders_含五个内置名 对齐 factory.py:103-109。
func TestAvailableProviders_含五个内置名(t *testing.T) {
	available := AvailableProviders()
	for _, name := range ProviderNames {
		assert.Contains(t, available, name)
	}
}

// TestAvailableProviders_含注册名 注册后出现在列表里。
func TestAvailableProviders_含注册名(t *testing.T) {
	registerFake(t, "custom-available", newFakeProvider("custom-available"))
	assert.Contains(t, AvailableProviders(), "custom-available")
}

// TestAvailableProviders_注册名不重复 重复注册不产生重复条目。
func TestAvailableProviders_注册名不重复(t *testing.T) {
	registerFake(t, "custom-dup", newFakeProvider("custom-dup"))
	count := 0
	for _, name := range AvailableProviders() {
		if name == "custom-dup" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// TestResolveProvider_内置名未链接报错 五个内置名在本节都未注册。
func TestResolveProvider_内置名未链接报错(t *testing.T) {
	for _, name := range ProviderNames {
		_, err := ResolveProvider(name)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "is declared but not yet linked")
	}
}

// TestResolveProvider_未知名报错 对齐 factory.py:134。
func TestResolveProvider_未知名报错(t *testing.T) {
	_, err := ResolveProvider("no-such-provider")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown harness provider")
	// 错误消息里列出可选名（对齐上游 join(available_providers())）
	assert.Contains(t, err.Error(), "native")
}

// TestResolveProvider_注册名成功 注册后能解析出 provider。
func TestResolveProvider_注册名成功(t *testing.T) {
	provider := newFakeProvider("custom-resolve")
	registerFake(t, "custom-resolve", provider)

	got, err := ResolveProvider("custom-resolve")
	require.NoError(t, err)
	assert.Equal(t, "custom-resolve", got.Card().Name)
}

// TestResolveProvider_工厂返回错误 对齐 factory.py:194-196。
func TestResolveProvider_工厂返回错误(t *testing.T) {
	require.NoError(t, RegisterHarnessProvider("broken-factory",
		func() (harnessprotocol.HarnessProvider, error) {
			return nil, assert.AnError
		}))
	t.Cleanup(func() { UnregisterHarnessProvider("broken-factory") })

	_, err := ResolveProvider("broken-factory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be instantiated")
}

// TestResolveProvider_工厂返回nil 对齐 factory.py:197-198 的 SPI 检查。
func TestResolveProvider_工厂返回nil(t *testing.T) {
	require.NoError(t, RegisterHarnessProvider("nil-factory",
		func() (harnessprotocol.HarnessProvider, error) { return nil, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("nil-factory") })

	_, err := ResolveProvider("nil-factory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement HarnessProvider")
}

// ──────────────────────────── manifest 转换 ────────────────────────────

// TestManifestSourceFromPath_路径加载 对齐 factory.py:234 的路径分支
// （union 已在 API 层拆开：spec 由调用方直传，本函数只管路径）。
func TestManifestSourceFromPath_路径加载(t *testing.T) {
	dir := t.TempDir()
	writeValidManifest(t, dir)

	byDir, err := ManifestSourceFromPath(dir)
	require.NoError(t, err)
	assert.Equal(t, "expert", byDir.AgentCard.Name)

	byFile, err := ManifestSourceFromPath(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	assert.Equal(t, "expert", byFile.AgentCard.Name)

	// 不存在的路径
	_, err = ManifestSourceFromPath(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}

// TestManifestMcpServers_stdio 对齐 factory.py:247-255。
func TestManifestMcpServers_stdio(t *testing.T) {
	spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{
		ServerName: "fs",
		Type:       "stdio",
		Command:    "node",
		Args:       []string{"server.js"},
		Env:        map[string]string{"K": "V"},
	}}}
	servers, err := ManifestMcpServers(spec)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	assert.Equal(t, "fs", servers[0].Name)
	assert.Equal(t, harnessprotocol.McpTransportStdio, servers[0].Transport)
	assert.Equal(t, []string{"node", "server.js"}, servers[0].Command)
	assert.Equal(t, map[string]string{"K": "V"}, servers[0].Env)
	assert.Nil(t, servers[0].URL)
}

// TestManifestMcpServers_http 三种非 stdio 类型都映射到 HTTP 传输。
// 对齐 factory.py:256-263（上游 sse 与 streamable_http 都走 HTTP 分支）。
func TestManifestMcpServers_http(t *testing.T) {
	for _, manifestType := range []string{"sse", "streamable_http"} {
		t.Run(manifestType, func(t *testing.T) {
			spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{
				ServerName:  "api",
				Type:        manifestType,
				URL:         "https://x/mcp",
				AuthHeaders: map[string]string{"A": "B"},
			}}}
			servers, err := ManifestMcpServers(spec)
			require.NoError(t, err)
			require.Len(t, servers, 1)
			assert.Equal(t, harnessprotocol.McpTransportHTTP, servers[0].Transport)
			require.NotNil(t, servers[0].URL)
			assert.Equal(t, "https://x/mcp", *servers[0].URL)
			assert.Equal(t, map[string]string{"A": "B"}, servers[0].Headers)
			assert.Empty(t, servers[0].Command)
		})
	}
}

// TestManifestMcpServers_缺serverName报错 对齐 factory.py:245-246。
func TestManifestMcpServers_缺serverName报错(t *testing.T) {
	spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{Type: "stdio", Command: "x"}}}
	_, err := ManifestMcpServers(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires server_name")
}

// TestManifestMcpServers_serverID回退 对齐 factory.py:244 `server_name or server_id`。
func TestManifestMcpServers_serverID回退(t *testing.T) {
	spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{
		ServerID: "by-id", Type: "stdio", Command: "x",
	}}}
	servers, err := ManifestMcpServers(spec)
	require.NoError(t, err)
	assert.Equal(t, "by-id", servers[0].Name)
}

// TestManifestMcpServers_stdio缺command报错 对齐 factory.py:248-249。
func TestManifestMcpServers_stdio缺command报错(t *testing.T) {
	spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{ServerName: "fs", Type: "stdio"}}}
	_, err := ManifestMcpServers(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires command")
}

// TestManifestMcpServers_http缺url报错 对齐 factory.py:256-257。
func TestManifestMcpServers_http缺url报错(t *testing.T) {
	spec := hschema.AgentTemplateSpec{Mcps: []hschema.McpServerSpec{{
		ServerName: "api", Type: "sse",
	}}}
	_, err := ManifestMcpServers(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires url")
}

// TestManifestMcpServers_空列表 返回空切片。
func TestManifestMcpServers_空列表(t *testing.T) {
	servers, err := ManifestMcpServers(hschema.AgentTemplateSpec{})
	require.NoError(t, err)
	assert.Empty(t, servers)
}

// TestStdioCommand_相对脚本按cwd解析 对齐 factory.py:266-278。
func TestStdioCommand_相对脚本按cwd解析(t *testing.T) {
	cwd := t.TempDir()
	script := filepath.Join(cwd, "run.js")
	require.NoError(t, os.WriteFile(script, []byte("//"), 0o600))

	spec := hschema.McpServerSpec{
		Type: "stdio", Command: "node", Args: []string{"run.js", "--flag"}, Cwd: cwd,
	}
	command, err := StdioCommand(spec)
	require.NoError(t, err)
	require.Len(t, command, 3)
	assert.Equal(t, "node", command[0])
	assert.Equal(t, script, command[1], "相对脚本解析成绝对路径")
	assert.Equal(t, "--flag", command[2], "非文件参数原样保留")
}

// TestStdioCommand_绝对路径原样 对齐 factory.py:272-273。
func TestStdioCommand_绝对路径原样(t *testing.T) {
	spec := hschema.McpServerSpec{Type: "stdio", Command: "node", Args: []string{"/abs/x.js"}}
	command, err := StdioCommand(spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"node", "/abs/x.js"}, command)
}

// TestStdioCommand_相对路径但文件不存在 原样保留（对齐 is_file() 判定）。
func TestStdioCommand_相对路径但文件不存在(t *testing.T) {
	cwd := t.TempDir()
	spec := hschema.McpServerSpec{
		Type: "stdio", Command: "node", Args: []string{"missing.js"}, Cwd: cwd,
	}
	command, err := StdioCommand(spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"node", "missing.js"}, command)
}

// TestStdioCommand_无cwd 参数原样保留。
func TestStdioCommand_无cwd(t *testing.T) {
	spec := hschema.McpServerSpec{Type: "stdio", Command: "node", Args: []string{"x.js"}}
	command, err := StdioCommand(spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"node", "x.js"}, command)
}

// ──────────────────────────── BuildHarnessContext 测试分组 ────────────────────────────

// TestBuildHarnessContext_native只带extraSystemPrompt 对齐 factory.py:429-435。
//
// native 系：模板由 harness 自己加载，只有 extra_system_prompt 进 context。
func TestBuildHarnessContext_native只带extraSystemPrompt(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a1", "expert", "描述"),
		PromptSections: []hschema.PromptSectionSpec{{
			Content: map[string]string{"cn": "我是专家。"}, Priority: 1,
		}},
		Mcps: []hschema.McpServerSpec{{ServerName: "fs", Type: "stdio", Command: "node"}},
	}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider:          "native",
		HostSessionID:     "host-1",
		ExtraSystemPrompt: "额外",
	})
	require.NoError(t, err)
	assert.Equal(t, "额外", harnessContext.SystemPrompt, "native 系不渲染 persona")
	assert.Empty(t, harnessContext.McpServers, "native 系不传 MCP 服务器")
	assert.Equal(t, "a1", harnessContext.AgentID)
	assert.Equal(t, "expert", harnessContext.AgentName)
	assert.Equal(t, "host-1", harnessContext.HostSessionID)
}

// TestBuildHarnessContext_第三方渲染提示词与MCP 对齐 factory.py:429-433。
func TestBuildHarnessContext_第三方渲染提示词与MCP(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a1", "expert", "描述"),
		PromptSections: []hschema.PromptSectionSpec{{
			Content: map[string]string{"cn": "我是专家。"}, Priority: 1,
		}},
		Mcps: []hschema.McpServerSpec{{
			ServerName: "fs", Type: "stdio", Command: "node",
		}},
	}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider:      "claudecode",
		HostSessionID: "host-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "我是专家。", harnessContext.SystemPrompt)
	require.Len(t, harnessContext.McpServers, 1)
	assert.Equal(t, "fs", harnessContext.McpServers[0].Name)
	// MCP 服务器存在时必须声明 MCP_SERVERS 能力（factory.py:439-440）
	assert.True(t, harnessContext.HostCapabilities[harnessprotocol.HostCapabilityMcpServers])
}

// TestBuildHarnessContext_段落拼接 多段落空行连接，extra 追加在后。
func TestBuildHarnessContext_段落拼接(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		PromptSections: []hschema.PromptSectionSpec{
			{Content: map[string]string{"cn": "第一段"}, Priority: 1},
			{Content: map[string]string{"cn": "第二段"}, Priority: 2},
		},
	}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider: "codex", HostSessionID: "h", ExtraSystemPrompt: "额外",
	})
	require.NoError(t, err)
	assert.Equal(t, "第一段\n\n第二段\n\n额外", harnessContext.SystemPrompt)
}

// TestBuildHarnessContext_checkpointSink加能力 对齐 factory.py:437-438。
func TestBuildHarnessContext_checkpointSink加能力(t *testing.T) {
	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
	sink := &fakeSink{}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider:       "claudecode",
		HostSessionID:  "h",
		CheckpointSink: sink,
	})
	require.NoError(t, err)
	assert.True(t, harnessContext.HostCapabilities[harnessprotocol.HostCapabilityCheckpointSink])
	assert.NotNil(t, harnessContext.CheckpointSink)
}

// TestBuildHarnessContext_无MCP不声明能力 对齐 factory.py:439-440 的 if mcp_servers。
func TestBuildHarnessContext_无MCP不声明能力(t *testing.T) {
	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider: "claudecode", HostSessionID: "h",
	})
	require.NoError(t, err)
	assert.False(t, harnessContext.HostCapabilities[harnessprotocol.HostCapabilityMcpServers])
}

// TestBuildHarnessContext_agentID回退 对齐 factory.py:443 `agent_id or card.id or card.name`。
func TestBuildHarnessContext_agentID回退(t *testing.T) {
	t.Run("用 card.id", func(t *testing.T) {
		spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "id-1", "name-1", "d")}
		harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
			Provider: "native", HostSessionID: "h"})
		require.NoError(t, err)
		assert.Equal(t, "id-1", harnessContext.AgentID)
	})
	t.Run("id 空时用 name", func(t *testing.T) {
		spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "", "name-2", "d")}
		harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
			Provider: "native", HostSessionID: "h"})
		require.NoError(t, err)
		assert.Equal(t, "name-2", harnessContext.AgentID)
	})
	t.Run("显式覆盖优先", func(t *testing.T) {
		spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "id-1", "name-1", "d")}
		harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
			Provider: "native", HostSessionID: "h", AgentID: "explicit"})
		require.NoError(t, err)
		assert.Equal(t, "explicit", harnessContext.AgentID)
	})
}

// TestBuildHarnessContext_cwd与env cwd 转指针，env 非 nil。
func TestBuildHarnessContext_cwd与env(t *testing.T) {
	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}

	withCwd, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider: "native", HostSessionID: "h", Cwd: "/tmp/w",
		Env: map[string]string{"K": "V"}})
	require.NoError(t, err)
	require.NotNil(t, withCwd.Cwd)
	assert.Equal(t, "/tmp/w", *withCwd.Cwd)
	assert.Equal(t, map[string]string{"K": "V"}, withCwd.Env)

	noCwd, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider: "native", HostSessionID: "h"})
	require.NoError(t, err)
	assert.Nil(t, noCwd.Cwd)
	assert.NotNil(t, noCwd.Env, "env 必须非 nil（上游 dict(env or {})）")
}

// TestBuildHarnessContext_语言默认cn 未传 language 时用 cn。
func TestBuildHarnessContext_语言默认cn(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard:      mustCard(t, "a", "n", "d"),
		PromptSections: []hschema.PromptSectionSpec{{Content: map[string]string{"cn": "中文"}, Priority: 1}},
	}
	harnessContext, err := BuildHarnessContext(spec, BuildHarnessContextOptions{
		Provider: "claudecode", HostSessionID: "h",
	})
	require.NoError(t, err)
	assert.Equal(t, "中文", harnessContext.SystemPrompt)
}

// ──────────────────────────── ManifestProviderConfig 测试分组 ────────────────────────────

// TestManifestProviderConfig_native带模板 对齐 factory.py:297-301。
func TestManifestProviderConfig_native带模板(t *testing.T) {
	for _, provider := range []string{"native", "native_v2"} {
		t.Run(provider, func(t *testing.T) {
			spec := hschema.AgentTemplateSpec{
				AgentCard: mustCard(t, "a", "expert", "d"),
				Tools:     []hschema.BuiltinToolSpec{{Type: "t"}},
			}
			config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{
				Provider: provider, Language: "en",
			})
			require.NoError(t, err)
			assert.Equal(t, "en", config["language"])
			template, ok := config["agent_template"].(map[string]any)
			require.True(t, ok)
			card, ok := template["agent_card"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "expert", card["name"])
		})
	}
}

// TestManifestProviderConfig_第三方拒绝DeepAgent专属段 对齐 factory.py:302-307。
func TestManifestProviderConfig_第三方拒绝DeepAgent专属段(t *testing.T) {
	cases := []struct {
		section string
		build   func(*hschema.AgentTemplateSpec)
	}{
		{"tools", func(spec *hschema.AgentTemplateSpec) { spec.Tools = []hschema.BuiltinToolSpec{{Type: "t"}} }},
		{"rails", func(spec *hschema.AgentTemplateSpec) { spec.Rails = []hschema.RailSpec{{Type: "r"}} }},
		{"subagents", func(spec *hschema.AgentTemplateSpec) {
			spec.Subagents = []hschema.AgentTemplateSpec{{}}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.section, func(t *testing.T) {
			spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
			testCase.build(&spec)
			_, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{Provider: "claudecode"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.section)
			assert.Contains(t, err.Error(), "depends on the DeepAgent framework")
		})
	}
}

// TestManifestProviderConfig_skills进config 对齐 factory.py:308-309。
func TestManifestProviderConfig_skills进config(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Skills:    []hschema.SkillSpec{{Dir: "/s", Mode: "auto_list"}},
	}
	config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{Provider: "claudecode"})
	require.NoError(t, err)
	skills, ok := config["skills"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, skills, 1)
	assert.Equal(t, "/s", skills[0]["dir"])
	assert.Equal(t, "auto_list", skills[0]["mode"])
}

// TestManifestProviderConfig_显式config优先 对齐 factory.py:296 的 dict(config)。
func TestManifestProviderConfig_显式config优先(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Skills:    []hschema.SkillSpec{{Dir: "/s"}},
	}
	config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{
		Provider: "claudecode",
		Config:   map[string]any{"skills": []any{"显式覆盖"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []any{"显式覆盖"}, config["skills"])
}

// TestManifestProviderConfig_native显式模板优先 setddefault 语义。
func TestManifestProviderConfig_native显式模板优先(t *testing.T) {
	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
	config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{
		Provider: "native",
		Config:   map[string]any{"agent_template": map[string]any{"custom": true}},
	})
	require.NoError(t, err)
	template, ok := config["agent_template"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, template["custom"])
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// mustCard 构造一个 AgentCard 值。
func mustCard(t *testing.T, id, name, description string) saschema.AgentCard {
	t.Helper()
	card := saschema.NewAgentCard(
		saschema.WithAgentID(id),
		saschema.WithAgentName(name),
		saschema.WithAgentDescription(description),
	)
	require.NotNil(t, card)
	return *card
}

// writeValidManifest 在 dir 下写一个通过校验的 manifest.json。
func writeValidManifest(t *testing.T, dir string) {
	t.Helper()
	content := `{
		"package_type": "agent_template",
		"name": "expert",
		"description": "领域专家",
		"agent_card": {"id": "a1", "name": "expert", "description": "领域专家"}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0o600))
}

// ──────────────────────────── ProviderModelConfig 测试分组 ────────────────────────────

// modelConfigProvider 实现 ModelConfigProvider 的测试 provider。
type modelConfigProvider struct {
	fakeProvider
	authFallback   bool
	endpointModels bool
	lastChoice     *harnessprotocol.ModelChoice
	lastFallback   *harnessprotocol.ModelChoice
}

// ModelConfig 记录选择并返回固定片段。
func (m *modelConfigProvider) ModelConfig(choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice) (harnessprotocol.JsonObject, error) {
	copied := choice
	m.lastChoice = &copied
	m.lastFallback = fallback
	return harnessprotocol.JsonObject{"model": "mapped"}, nil
}

// newModelConfigProvider 构造一个支持模型配置的 provider。
func newModelConfigProvider(name string, authFallback, endpointModels bool) *modelConfigProvider {
	provider := &modelConfigProvider{authFallback: authFallback, endpointModels: endpointModels}
	declared := map[harnessprotocol.HarnessCapability]bool{}
	if authFallback {
		declared[harnessprotocol.HarnessCapabilityAuthFallback] = true
	}
	if endpointModels {
		declared[harnessprotocol.HarnessCapabilityEndpointModels] = true
	}
	provider.card = harnessprotocol.HarnessCard{
		Name:                       name,
		ImplementationVersion:      "1.0",
		ProtocolVersion:            harnessprotocol.ProtocolVersion,
		CompatibleProtocolVersions: map[string]bool{harnessprotocol.ProtocolVersion: true},
		Capabilities:               declared,
	}
	return provider
}

// TestProviderModelConfig_正常映射 对齐 factory.py:358-368。
func TestProviderModelConfig_正常映射(t *testing.T) {
	provider := newModelConfigProvider("mc-ok", true, true)
	require.NoError(t, RegisterHarnessProvider("mc-ok",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-ok") })

	model := "m1"
	fragment, err := ProviderModelConfig("mc-ok", harnessprotocol.ModelChoice{Model: &model}, nil)
	require.NoError(t, err)
	assert.Equal(t, "mapped", fragment["model"])
	require.NotNil(t, provider.lastChoice)
	assert.Equal(t, "m1", *provider.lastChoice.Model)
}

// TestProviderModelConfig_不实现钩子报错 对齐 factory.py:359-360。
func TestProviderModelConfig_不实现钩子报错(t *testing.T) {
	registerFake(t, "mc-none", newFakeProvider("mc-none"))

	_, err := ProviderModelConfig("mc-none", harnessprotocol.ModelChoice{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not configure models")
}

// TestProviderModelConfig_无回退能力报错 对齐 factory.py:361-362。
func TestProviderModelConfig_无回退能力报错(t *testing.T) {
	provider := newModelConfigProvider("mc-no-auth", false, true)
	require.NoError(t, RegisterHarnessProvider("mc-no-auth",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-no-auth") })

	fallbackModel := "fb"
	_, err := ProviderModelConfig("mc-no-auth", harnessprotocol.ModelChoice{},
		&harnessprotocol.ModelChoice{Model: &fallbackModel})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no authentication fallback")
}

// TestProviderModelConfig_无端点能力报错 对齐 factory.py:363-365。
func TestProviderModelConfig_无端点能力报错(t *testing.T) {
	provider := newModelConfigProvider("mc-no-endpoint", true, false)
	require.NoError(t, RegisterHarnessProvider("mc-no-endpoint",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-no-endpoint") })

	apiBase := "https://x"
	_, err := ProviderModelConfig("mc-no-endpoint", harnessprotocol.ModelChoice{
		Endpoint: &harnessprotocol.ModelEndpoint{APIBase: &apiBase},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not take endpoint models")
}

// ──────────────────────────── CreateHarness 测试分组 ────────────────────────────

// TestCreateHarness_转发配置 对齐 factory.py:393-396。
func TestCreateHarness_转发配置(t *testing.T) {
	provider := newFakeProvider("ch-basic")
	registerFake(t, "ch-basic", provider)

	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Skills:    []hschema.SkillSpec{{Dir: "/s"}},
	}
	// fakeProvider.Create 返回 assert.AnError，验证错误透传
	_, err := CreateHarness(spec, CreateHarnessOptions{Provider: "ch-basic"})
	require.Error(t, err)
	require.Len(t, provider.configs, 1)
	_, hasSkills := provider.configs[0]["skills"]
	assert.True(t, hasSkills, "skills 应进 provider 配置")
}

// TestCreateHarness_未知名 provider 报错。
func TestCreateHarness_未知名provider报错(t *testing.T) {
	_, err := CreateHarness(hschema.AgentTemplateSpec{},
		CreateHarnessOptions{Provider: "no-such"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown harness provider")
}

// TestCreateHarness_DeepAgent段被拒 第三方 provider 收到 tools 段。
func TestCreateHarness_DeepAgent段被拒(t *testing.T) {
	registerFake(t, "ch-third", newFakeProvider("ch-third"))

	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Tools:     []hschema.BuiltinToolSpec{{Type: "t"}},
	}
	_, err := CreateHarness(spec, CreateHarnessOptions{Provider: "ch-third"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depends on the DeepAgent framework")
}

// ──────────────────────────── manifestModelChoice 测试分组 ────────────────────────────

// TestManifestProviderConfig_模型无端点 只有 model_name 时不带端点。
func TestManifestProviderConfig_模型无端点(t *testing.T) {
	provider := newModelConfigProvider("mc-model-only", true, true)
	require.NoError(t, RegisterHarnessProvider("mc-model-only",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-model-only") })

	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Model: &hschema.ModelSpec{
			ModelRequestConfig: &llmschema.ModelRequestConfig{ModelName: "gpt-x"},
		},
	}
	config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{Provider: "mc-model-only"})
	require.NoError(t, err)
	assert.Equal(t, "mapped", config["model"])
	require.NotNil(t, provider.lastChoice)
	require.NotNil(t, provider.lastChoice.Model)
	assert.Equal(t, "gpt-x", *provider.lastChoice.Model)
	assert.Nil(t, provider.lastChoice.Endpoint, "无 api_base 时不带端点")
}

// TestManifestProviderConfig_模型带端点 api_base 存在时构造端点。
func TestManifestProviderConfig_模型带端点(t *testing.T) {
	provider := newModelConfigProvider("mc-endpoint", true, true)
	require.NoError(t, RegisterHarnessProvider("mc-endpoint",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-endpoint") })

	spec := hschema.AgentTemplateSpec{
		AgentCard: mustCard(t, "a", "n", "d"),
		Model: &hschema.ModelSpec{
			ModelClientConfig: llmschema.ModelClientConfig{
				APIBase:        "https://llm/v1",
				APIKey:         "secret",
				ClientProvider: "openai",
			},
			ModelRequestConfig: &llmschema.ModelRequestConfig{ModelName: "gpt-x"},
		},
	}
	_, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{Provider: "mc-endpoint"})
	require.NoError(t, err)
	require.NotNil(t, provider.lastChoice)
	require.NotNil(t, provider.lastChoice.Endpoint)
	require.NotNil(t, provider.lastChoice.Endpoint.APIBase)
	assert.Equal(t, "https://llm/v1", *provider.lastChoice.Endpoint.APIBase)
	require.NotNil(t, provider.lastChoice.Endpoint.APIKey)
	assert.Equal(t, "secret", *provider.lastChoice.Endpoint.APIKey)
	require.NotNil(t, provider.lastChoice.Endpoint.Provider)
	assert.Equal(t, "openai", *provider.lastChoice.Endpoint.Provider)
}

// TestManifestProviderConfig_无模型不调用 provider 清单无 model 时跳过。
func TestManifestProviderConfig_无模型不调用provider(t *testing.T) {
	provider := newModelConfigProvider("mc-nomodel", true, true)
	require.NoError(t, RegisterHarnessProvider("mc-nomodel",
		func() (harnessprotocol.HarnessProvider, error) { return provider, nil }))
	t.Cleanup(func() { UnregisterHarnessProvider("mc-nomodel") })

	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
	config, err := ManifestProviderConfig(spec, ManifestProviderConfigOptions{Provider: "mc-nomodel"})
	require.NoError(t, err)
	assert.NotContains(t, config, "model")
	assert.Nil(t, provider.lastChoice)
}

// TestExpandHome_展开 factory 的 cwd 展开（覆盖 expandHome 各分支）。
func TestExpandHome_展开(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := expandHome("~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	got, err = expandHome("~/sub")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "sub"), got)

	got, err = expandHome("/abs")
	require.NoError(t, err)
	assert.Equal(t, "/abs", got)

	got, err = expandHome("~user/x")
	require.NoError(t, err)
	assert.Equal(t, "~user/x", got)
}

// TestStdioCommand_cwd带波浪号 cwd 的 ~ 会被展开。
func TestStdioCommand_cwd带波浪号(t *testing.T) {
	spec := hschema.McpServerSpec{
		Type: "stdio", Command: "node", Args: []string{"x.js"}, Cwd: "~/nonexistent-dir-xyz",
	}
	// 文件不存在 → 参数原样保留，但 cwd 展开路径不报错
	command, err := StdioCommand(spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"node", "x.js"}, command)
}

// TestProviderLabel_各形态 client_provider 的三种形态（string / Stringer / 其他）。
func TestProviderLabel_各形态(t *testing.T) {
	assert.Equal(t, "", providerLabel(nil))
	assert.Equal(t, "openai", providerLabel("openai"))
	assert.Equal(t, "typed", providerLabel(stringerValue{}))
	assert.Equal(t, "42", providerLabel(42))
}

// stringerValue 实现 String() 的类型（模拟 Python 的枚举）。
type stringerValue struct{}

// String 实现 fmt.Stringer。
func (s stringerValue) String() string { return "typed" }

// TestHasDeepAgentSection_未知段 白名单外的段名恒 false。
func TestHasDeepAgentSection_未知段(t *testing.T) {
	manifest := hschema.AgentTemplateSpec{
		Tools: []hschema.BuiltinToolSpec{{Type: "t"}},
	}
	assert.True(t, hasDeepAgentSection(manifest, "tools"))
	assert.False(t, hasDeepAgentSection(manifest, "rails"))
	assert.False(t, hasDeepAgentSection(manifest, "subagents"))
	assert.False(t, hasDeepAgentSection(manifest, "unknown-section"))
}

// TestCreateHarness_native系带模板 native provider 收到 agent_template。
func TestCreateHarness_native系带模板(t *testing.T) {
	provider := newFakeProvider("ch-native-like")
	// 用注册名绕过内置名限制，但配置走 native 分支需内置名
	// 故此处直接验 ManifestProviderConfig 的 native 分支已覆盖，
	// 本用例只确认注册 provider 走第三方分支
	registerFake(t, "ch-native-like", provider)

	spec := hschema.AgentTemplateSpec{AgentCard: mustCard(t, "a", "n", "d")}
	_, err := CreateHarness(spec, CreateHarnessOptions{Provider: "ch-native-like", Language: "en"})
	require.Error(t, err)
	require.Len(t, provider.configs, 1)
	// 第三方分支不写 language
	_, hasLanguage := provider.configs[0]["language"]
	assert.False(t, hasLanguage)
}
