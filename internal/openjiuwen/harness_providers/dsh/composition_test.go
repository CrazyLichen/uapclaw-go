package dsh

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
)

// context 构造测试上下文。
func dshContext(mcpServers []harnessprotocol.McpServerConfig, cwd, systemPrompt string) *harnessprotocol.HarnessContext {
	var cwdPtr *string
	if cwd != "" {
		cwdPtr = &cwd
	}
	return &harnessprotocol.HarnessContext{
		McpServers:   mcpServers,
		Cwd:          cwdPtr,
		SystemPrompt: systemPrompt,
	}
}

// stdio 保留 argv 与 env：上游 test_stdio_mcp_configuration_preserves_argv_and_env。
func TestMCPConfigs_Stdio保留Argv与Env(t *testing.T) {
	server, err := harnessprotocol.NewMcpServerConfig(
		"srv", harnessprotocol.McpTransportStdio,
		[]string{"npx", "-y", "pkg"}, nil, nil,
		map[string]string{"K": "V"}, nil)
	require.NoError(t, err)
	configs, err := MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{server}, "/tmp/ws", ""))
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "srv", configs[0]["serverName"])
	assert.Equal(t, true, configs[0]["failOnStartupError"])
	assert.Equal(t, "stdio", configs[0]["transport"])
	assert.Equal(t, "npx", configs[0]["command"])
	assert.Equal(t, []string{"-y", "pkg"}, configs[0]["args"])
	assert.Equal(t, map[string]string{"K": "V"}, configs[0]["env"])
	assert.Equal(t, "/tmp/ws", configs[0]["cwd"])
}

// HTTP 与 in-process：composition.py:34-37。
func TestMCPConfigs_HTTP与InProcess(t *testing.T) {
	url := "https://mcp.example/sse"
	server, err := harnessprotocol.NewMcpServerConfig(
		"http1", harnessprotocol.McpTransportHTTP, nil, &url, nil,
		nil, map[string]string{"Authorization": "Bearer x"})
	require.NoError(t, err)
	configs, err := MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{server}, "", ""))
	require.NoError(t, err)
	assert.Equal(t, "streamable-http", configs[0]["transport"])
	assert.Equal(t, url, configs[0]["url"])
	assert.Equal(t, map[string]string{"Authorization": "Bearer x"}, configs[0]["headers"])

	instanceServer, err := harnessprotocol.NewMcpServerConfig(
		"inproc", harnessprotocol.McpTransportInProcess, nil, nil, struct{}{}, nil, nil)
	require.NoError(t, err)
	_, err = MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{instanceServer}, "", ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH cannot mount in-process MCP servers")
}

// 名字正则与去重：composition.py:21-22。
func TestMCPConfigs_名字校验(t *testing.T) {
	badName, err := harnessprotocol.NewMcpServerConfig(
		"bad name!", harnessprotocol.McpTransportHTTP, nil, strPtr("https://x"), nil, nil, nil)
	require.NoError(t, err)
	_, err = MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{badName}, "", ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}")

	first, err := harnessprotocol.NewMcpServerConfig(
		"dup", harnessprotocol.McpTransportHTTP, nil, strPtr("https://x"), nil, nil, nil)
	require.NoError(t, err)
	second, err := harnessprotocol.NewMcpServerConfig(
		"dup", harnessprotocol.McpTransportHTTP, nil, strPtr("https://y"), nil, nil, nil)
	require.NoError(t, err)
	_, err = MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{first, second}, "", ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}")

	longName, err := harnessprotocol.NewMcpServerConfig(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", harnessprotocol.McpTransportHTTP, nil, strPtr("https://x"), nil, nil, nil)
	require.NoError(t, err)
	_, err = MCPConfigs(dshContext([]harnessprotocol.McpServerConfig{longName}, "", ""))
	require.Error(t, err)
}

// 无内容 → nil：composition.py:55-56。
func TestWriteOverlay_无内容返回nil(t *testing.T) {
	overlay, err := WriteOverlay(dshContext(nil, "", ""), false, "replace", false)
	require.NoError(t, err)
	assert.Nil(t, overlay)
}

// replace 提示词插件：composition.py:64-90。
func TestWriteOverlay_Replace提示词(t *testing.T) {
	overlay, err := WriteOverlay(dshContext(nil, "", "host instructions"), true, "replace", false)
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer overlay.Cleanup()

	// patch 文件存在且含插件注册
	patchBytes, err := os.ReadFile(overlay.path)
	require.NoError(t, err)
	patch := string(patchBytes)
	assert.Contains(t, patch, "openjiuwen-host-prompt")
	assert.Contains(t, patch, "insert:")

	// env 变量承载提示词，patch 文件无明文
	assert.Contains(t, overlay.envValue, "host instructions")
	assert.NotContains(t, patch, "host instructions")

	// 插件文件存在且 0600
	pluginStat, err := os.Stat(filepath.Join(overlay.dir, "host-prompt.mjs"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), pluginStat.Mode().Perm())
	assert.Contains(t, overlay.envValue, "personaPrefix")
}

// append 独立 section：composition.py:76-83。
func TestWriteOverlay_Append独立section(t *testing.T) {
	overlay, err := WriteOverlay(dshContext(nil, "", "extra"), true, "append", false)
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer overlay.Cleanup()
	assert.Contains(t, overlay.envValue, "append")
	assert.Contains(t, overlay.envValue, "extra")
	// append 不写 personaPrefix
	assert.NotContains(t, overlay.envValue, "personaPrefix")
}

// mcp 与 skill 插件注入：composition.py:91-104。
func TestWriteOverlay_mcp与skill插件注入(t *testing.T) {
	server, err := harnessprotocol.NewMcpServerConfig(
		"srv", harnessprotocol.McpTransportHTTP, nil, strPtr("https://x"), nil, nil, nil)
	require.NoError(t, err)
	overlay, err := WriteOverlay(
		dshContext([]harnessprotocol.McpServerConfig{server}, "", ""), false, "replace", true)
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer overlay.Cleanup()

	patchBytes, err := os.ReadFile(overlay.path)
	require.NoError(t, err)
	patch := string(patchBytes)
	assert.Contains(t, patch, "@deepseek-ai/dsh-mcp-client")
	assert.Contains(t, patch, `JSON.parse(process.env.`)
	for _, plugin := range []string{"skill", "skill-filesystem", "tool-skill"} {
		assert.Contains(t, patch, "@deepseek-ai/dsh-"+plugin)
	}
	assert.Contains(t, patch, "openjiuwen-mcp-0")
}

// patch 与插件文件 0600 + 失败清理：composition.py:107-112。
func TestWriteOverlay_权限0600(t *testing.T) {
	overlay, err := WriteOverlay(dshContext(nil, "", "p"), true, "replace", false)
	require.NoError(t, err)
	require.NotNil(t, overlay)
	patchStat, err := os.Stat(overlay.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), patchStat.Mode().Perm())
	overlay.Cleanup()
	_, err = os.Stat(overlay.dir)
	assert.True(t, os.IsNotExist(err))
}
