// Python 源：jiuwenswarm/tests 下 mcp_config 对应用例 + 本批场景化补充（T1）
//
// 场景对应（讨论点 → 用例）：
//   - 占位解析（_resolve_string）→ TestResolveString
//   - 凭据 resolver（build_mcp_credential_resolver）→ TestBuildMcpCredentialResolver
//   - 启用项提取（extract_enabled_mcp_server_entries）→ TestExtractEnabledMCPServerEntries
//   - corepack 路由（resolve_node_package_runner）→ TestResolveNodePackageRunner
//   - 单条目构建（build_mcp_server_config stdio/http 两分支）→ TestBuildMCPServerConfig
//   - 稳定 id（_stable_mcp_server_id/_safe_id_part）→ TestStableMCPServerID
//   - 批量构建（build_enabled_mcp_server_configs）→ TestBuildEnabledMCPServerConfigs

package common

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// fakeCredentialStore 测试用的凭据存储桩（mcp_credential 域实现形态）。
type fakeCredentialStore struct {
	data map[string]map[string]string
}

func (f *fakeCredentialStore) GetAll(name string) map[string]string { return f.data[name] }

// ──────────────────────────── 测试 ────────────────────────────

func TestResolveString(t *testing.T) {
	t.Run("resolver为nil时原样返回", func(t *testing.T) {
		assert.Equal(t, "Bearer ${TOKEN}", resolveString("Bearer ${TOKEN}", nil))
	})
	t.Run("占位被替换", func(t *testing.T) {
		resolver := func(key string) string {
			if key == "TOKEN" {
				return "abc"
			}
			return ""
		}
		assert.Equal(t, "Bearer abc", resolveString("Bearer ${TOKEN}", resolver))
	})
	t.Run("缺失保持字面量", func(t *testing.T) {
		resolver := func(string) string { return "" }
		assert.Equal(t, "Bearer ${MISSING}", resolveString("Bearer ${MISSING}", resolver))
	})
}

func TestBuildMcpCredentialResolver(t *testing.T) {
	t.Run("空名返回nil", func(t *testing.T) {
		assert.Nil(t, BuildMcpCredentialResolver(""))
		assert.Nil(t, BuildMcpCredentialResolver("  "))
	})
	t.Run("域未注册返回nil", func(t *testing.T) {
		Register("mcp_credential", nil) // 清理可能的前置注册
		assert.Nil(t, BuildMcpCredentialResolver("nope"))
	})
	t.Run("存储为空返回nil", func(t *testing.T) {
		Register("mcp_credential", &fakeCredentialStore{data: map[string]map[string]string{}})
		assert.Nil(t, BuildMcpCredentialResolver("m1"))
	})
	t.Run("存储凭据优先env兜底", func(t *testing.T) {
		require.NoError(t, os.Setenv("MCP_TEST_ENV_KEY", "from-env"))
		defer func() { require.NoError(t, os.Unsetenv("MCP_TEST_ENV_KEY")) }()
		Register("mcp_credential", &fakeCredentialStore{data: map[string]map[string]string{
			"m1": {"STORED": "from-store"},
		}})
		resolver := BuildMcpCredentialResolver("m1")
		require.NotNil(t, resolver)
		assert.Equal(t, "from-store", resolver("STORED"))         // 存储凭据命中
		assert.Equal(t, "from-env", resolver("MCP_TEST_ENV_KEY")) // 缺失走 env 兜底
		assert.Equal(t, "", resolver("BOTH_MISSING"))             // 双缺返回空（字面量回退）
	})
}

func TestExtractEnabledMCPServerEntries(t *testing.T) {
	cfg := map[string]any{
		"mcp": map[string]any{
			"servers": []any{
				map[string]any{"name": "a", "enabled": true},
				map[string]any{"name": "b", "enabled": false},
				map[string]any{"name": "c"}, // enabled 缺省 true
				"not-a-dict",
			},
		},
	}
	entries := ExtractEnabledMCPServerEntries(cfg)
	require.Len(t, entries, 2)
	assert.Equal(t, "a", entries[0]["name"])
	assert.Equal(t, "c", entries[1]["name"])
	// Python: servers 读失败时 fallback config_base——D-15.16-e7 下恒走 fallback
	assert.Empty(t, ExtractEnabledMCPServerEntries(nil))
}

func TestResolveNodePackageRunner(t *testing.T) {
	origLookPath := lookPathFn
	defer func() { lookPathFn = origLookPath }()

	t.Run("非node启动器原样", func(t *testing.T) {
		lookPathFn = func(string) (string, error) { return "", assert.AnError }
		cmd, args := ResolveNodePackageRunner("python", []string{"-m", "x"})
		assert.Equal(t, "python", cmd)
		assert.Equal(t, []string{"-m", "x"}, args)
	})
	t.Run("启动器在PATH原样", func(t *testing.T) {
		lookPathFn = func(name string) (string, error) { return "/usr/bin/" + name, nil }
		cmd, args := ResolveNodePackageRunner("npx", []string{"-y", "pkg"})
		assert.Equal(t, "npx", cmd)
		assert.Equal(t, []string{"-y", "pkg"}, args)
	})
	t.Run("缺启动器有corepack走corepack", func(t *testing.T) {
		lookPathFn = func(name string) (string, error) {
			if name == "corepack" {
				return "/usr/bin/corepack", nil
			}
			return "", assert.AnError
		}
		cmd, args := ResolveNodePackageRunner("npx", []string{"-y", "pkg"})
		assert.Equal(t, "corepack", cmd)
		assert.Equal(t, []string{"npx", "-y", "pkg"}, args)
	})
	t.Run("缺启动器缺corepack原样", func(t *testing.T) {
		lookPathFn = func(string) (string, error) { return "", assert.AnError }
		cmd, args := ResolveNodePackageRunner("npm", []string{"i"})
		assert.Equal(t, "npm", cmd)
		assert.Equal(t, []string{"i"}, args)
	})
}

func TestBuildMCPServerConfig(t *testing.T) {
	t.Run("非法transport返回nil", func(t *testing.T) {
		assert.Nil(t, BuildMCPServerConfig(map[string]any{"name": "x", "transport": "grpc"}, MCPServerConfigOptions{}))
	})
	t.Run("缺名返回nil", func(t *testing.T) {
		assert.Nil(t, BuildMCPServerConfig(map[string]any{"transport": "stdio"}, MCPServerConfigOptions{}))
	})
	t.Run("stdio缺command返回nil", func(t *testing.T) {
		assert.Nil(t, BuildMCPServerConfig(map[string]any{"name": "x", "transport": "stdio"}, MCPServerConfigOptions{}))
	})
	t.Run("http缺url返回nil", func(t *testing.T) {
		assert.Nil(t, BuildMCPServerConfig(map[string]any{"name": "x", "transport": "http"}, MCPServerConfigOptions{}))
	})
	t.Run("stdio全字段", func(t *testing.T) {
		resolver := func(key string) string {
			if key == "SSH_HOST" {
				return "1.2.3.4"
			}
			return ""
		}
		cfg := BuildMCPServerConfig(map[string]any{
			"name":      "ssh-mcp",
			"transport": "stdio",
			"command":   "python",
			"args":      []any{"--host", "${SSH_HOST}"},
			"cwd":       "/tmp/ws",
			"env":       map[string]any{"TOKEN": "${SSH_TOKEN}"},
			"timeout_s": 30,
			"server_id": "explicit-id",
		}, MCPServerConfigOptions{CredentialResolver: resolver})
		require.NotNil(t, cfg)
		assert.Equal(t, "ssh-mcp", cfg.ServerName)
		assert.Equal(t, "stdio", cfg.ClientType)
		assert.Equal(t, "explicit-id", cfg.ServerID)
		assert.Equal(t, "stdio://ssh-mcp", cfg.ServerPath)
		params := cfg.Params
		assert.Equal(t, "python", params["command"])
		assert.Equal(t, []string{"--host", "1.2.3.4"}, params["args"])
		assert.Equal(t, "/tmp/ws", params["cwd"])
		assert.Equal(t, map[string]any{"TOKEN": "${SSH_TOKEN}"}, params["env"]) // 缺失保持字面量
		assert.Equal(t, 30, params["timeout_s"])
	})
	t.Run("http全字段含auth_headers", func(t *testing.T) {
		resolver := func(key string) string {
			if key == "GILDATA_TOKEN" {
				return "tok"
			}
			return ""
		}
		cfg := BuildMCPServerConfig(map[string]any{
			"name":      "gildata",
			"transport": "streamable-http",
			"url":       "https://mcp.example.com?token=${GILDATA_TOKEN}",
			"headers":   map[string]any{"Authorization": "Bearer ${GILDATA_TOKEN}"},
			"timeout_s": 15,
		}, MCPServerConfigOptions{CredentialResolver: resolver})
		require.NotNil(t, cfg)
		assert.Equal(t, "https://mcp.example.com?token=tok", cfg.ServerPath)
		assert.Equal(t, map[string]string{"Authorization": "Bearer tok"}, cfg.AuthHeaders)
		assert.Equal(t, 15, cfg.Params["timeout_s"])
	})
	t.Run("scope派生稳定id", func(t *testing.T) {
		cfg := BuildMCPServerConfig(map[string]any{
			"name": "amap", "transport": "stdio", "command": "npx", "args": []any{"-y", "amap"},
		}, MCPServerConfigOptions{ServerIDScope: "team:t1"})
		require.NotNil(t, cfg)
		assert.True(t, strings.HasPrefix(cfg.ServerID, "mcp_team_t1_amap_"))
	})
}

func TestStableMCPServerID(t *testing.T) {
	a := stableMCPServerID("team:t1", "amap", mustCfg(t, "amap"))
	b := stableMCPServerID("team:t1", "amap", mustCfg(t, "amap"))
	assert.Equal(t, a, b) // 同输入稳定
	c := stableMCPServerID("team:t2", "amap", mustCfg(t, "amap"))
	assert.NotEqual(t, a, c) // scope 隔离
}

// mustCfg 构造稳定 id 测试用的最小 McpServerConfig。
func mustCfg(t *testing.T, name string) *mcptypes.McpServerConfig {
	t.Helper()
	return &mcptypes.McpServerConfig{ServerName: name, ClientType: "stdio", ServerPath: "stdio://" + name}
}

func TestSafeIDPart(t *testing.T) {
	assert.Equal(t, "team_t1", safeIDPart("team:t1", "scope"))
	assert.Equal(t, "scope", safeIDPart("", "scope"))
	assert.Equal(t, "scope", safeIDPart("!!!", "scope"))
	assert.Len(t, safeIDPart(strings.Repeat("a", 60), "x"), 48) // 截 48
}

func TestBuildEnabledMCPServerConfigs(t *testing.T) {
	cfg := map[string]any{
		"mcp": map[string]any{
			"servers": []any{
				map[string]any{"name": "ok-stdio", "transport": "stdio", "command": "python"},
				map[string]any{"name": "bad", "transport": "grpc"},
				map[string]any{"name": "off", "transport": "stdio", "command": "x", "enabled": false},
			},
		},
	}
	configs := BuildEnabledMCPServerConfigs(cfg, "team:t1", false)
	require.Len(t, configs, 1)
	assert.Equal(t, "ok-stdio", configs[0].ServerName)
	// resolve_credentials=true 且域未注册 → resolver nil，占位保持字面量
	configs2 := BuildEnabledMCPServerConfigs(cfg, "team:t1", true)
	require.Len(t, configs2, 1)
}
