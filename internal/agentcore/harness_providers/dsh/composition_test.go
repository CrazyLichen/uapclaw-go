package dsh

import (
	"os"
	"path/filepath"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游对齐：test_harness.py:801-811 test_stdio_mcp_configuration_preserves_argv_and_env
func TestMCPConfigs_Stdio保留Argv与Env(t *testing.T) {
	cwd := "/tmp/proj"
	context := &harnessprotocol.HarnessContext{
		McpServers: []harnessprotocol.McpServerConfig{{
			Name:     "fs",
			Transport: harnessprotocol.McpTransportStdio,
			Command:  []string{"npx", "-y", "server-fs", "/data"},
			Env:      map[string]string{"A": "1"},
		}},
		Cwd: &cwd,
	}
	configs, err := MCPConfigs(context)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	config := configs[0]
	assert.Equal(t, "fs", config["serverName"])
	assert.Equal(t, true, config["failOnStartupError"])
	assert.Equal(t, "stdio", config["transport"])
	assert.Equal(t, "npx", config["command"])
	assert.Equal(t, []any{"-y", "server-fs", "/data"}, config["args"])
	assert.Equal(t, map[string]any{"A": "1"}, config["env"])
	assert.Equal(t, "/tmp/proj", config["cwd"])
}

// Python: composition.py:34-37
func TestMCPConfigs_HTTP与InProcess(t *testing.T) {
	url := "http://x"
	context := &harnessprotocol.HarnessContext{
		McpServers: []harnessprotocol.McpServerConfig{{
			Name: "web", Transport: harnessprotocol.McpTransportHTTP,
			URL: &url, Headers: map[string]string{"H": "v"},
		}},
	}
	configs, err := MCPConfigs(context)
	require.NoError(t, err)
	assert.Equal(t, "streamable-http", configs[0]["transport"])
	assert.Equal(t, "http://x", configs[0]["url"])
	assert.Equal(t, map[string]any{"H": "v"}, configs[0]["headers"])

	_, err = MCPConfigs(&harnessprotocol.HarnessContext{
		McpServers: []harnessprotocol.McpServerConfig{{
			Name: "inproc", Transport: harnessprotocol.McpTransportInProcess,
		}},
	})
	require.Error(t, err)
	assert.Equal(t, "DSH cannot mount in-process MCP servers", err.Error())
}

// Python: composition.py:21-22
func TestMCPConfigs_名字校验(t *testing.T) {
	mk := func(name string) *harnessprotocol.HarnessContext {
		return &harnessprotocol.HarnessContext{
			McpServers: []harnessprotocol.McpServerConfig{{
				Name: name, Transport: harnessprotocol.McpTransportHTTP,
			}},
		}
	}
	_, err := MCPConfigs(mk("bad name!"))
	require.Error(t, err)
	assert.Equal(t, "DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}", err.Error())
	_, err = MCPConfigs(&harnessprotocol.HarnessContext{
		McpServers: []harnessprotocol.McpServerConfig{
			{Name: "dup", Transport: harnessprotocol.McpTransportHTTP},
			{Name: "dup", Transport: harnessprotocol.McpTransportHTTP},
		},
	})
	require.Error(t, err)
	assert.Equal(t, "DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}", err.Error())
}

// Python: composition.py:55-56
func TestWriteOverlay_无内容返回nil(t *testing.T) {
	result, err := WriteOverlay(&harnessprotocol.HarnessContext{}, true, "replace", false)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// Python: composition.py:66-75 replace 模式：persona-prefix 原位替换插件
func TestWriteOverlay_Replace提示词(t *testing.T) {
	context := &harnessprotocol.HarnessContext{SystemPrompt: "HOST SECRET PROMPT"}
	result, err := WriteOverlay(context, true, "replace", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	t.Cleanup(result.cleanup)

	plugin, err := os.ReadFile(filepath.Join(result.dir, "host-prompt.mjs"))
	require.NoError(t, err)
	body := string(plugin)
	assert.Contains(t, body, "deployment:persona-prefix")
	assert.Contains(t, body, "system-prompt/assemble")

	patch, err := os.ReadFile(result.path)
	require.NoError(t, err)
	// 用户文本只经 env 变量传递，patch 与插件体不含明文
	assert.NotContains(t, string(patch), "HOST SECRET PROMPT")
	assert.NotContains(t, body, "HOST SECRET PROMPT")
	assert.Contains(t, string(patch), "openjiuwen-host-prompt")
	// env 变量承载明文
	assert.Contains(t, result.envValue, "HOST SECRET PROMPT")

	info, err := os.Stat(result.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Python: composition.py:76-83 append 模式：独立 section + 单插值变量
func TestWriteOverlay_Append独立section(t *testing.T) {
	context := &harnessprotocol.HarnessContext{SystemPrompt: "APPEND TEXT"}
	result, err := WriteOverlay(context, true, "append", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	t.Cleanup(result.cleanup)

	plugin, err := os.ReadFile(filepath.Join(result.dir, "host-prompt.mjs"))
	require.NoError(t, err)
	assert.Contains(t, string(plugin), "openjiuwen:host-instructions")
	assert.Contains(t, string(plugin), "MAX_SAFE_INTEGER")
}

// Python: composition.py:91-104 skill 三插件 + mcp-client 注入
func TestWriteOverlay_mcp与skill插件注入(t *testing.T) {
	context := &harnessprotocol.HarnessContext{
		McpServers: []harnessprotocol.McpServerConfig{{
			Name: "fs", Transport: harnessprotocol.McpTransportStdio,
			Command: []string{"svc"},
		}},
	}
	result, err := WriteOverlay(context, false, "replace", true)
	require.NoError(t, err)
	require.NotNil(t, result)
	t.Cleanup(result.cleanup)

	patch, err := os.ReadFile(result.path)
	require.NoError(t, err)
	body := string(patch)
	for _, id := range []string{"skill", "skill-filesystem", "tool-skill"} {
		assert.Contains(t, body, "- id: "+id+"\n")
	}
	assert.Contains(t, body, "@deepseek-ai/dsh-mcp-client")
	assert.Contains(t, body, "!!js \"JSON.parse(process.env."+result.envName+").mcps[0]\"")
	// env 值可被 JSON 解析出 mcps 数组
	assert.Contains(t, result.envValue, `"serverName":"fs"`)
}
