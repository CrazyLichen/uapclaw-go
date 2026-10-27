package tools

import (
	"testing"
)

// TestGetMcpTools_四key组合装配 验证聚合器的条件装配逻辑
// Python: get_mcp_tools (mcp_toolkits.py L39-47)
func TestGetMcpTools_四key组合装配(t *testing.T) {
	cases := []struct {
		name        string
		paidKey     string
		ddgEnabled  string
		bingEnabled string
		wantNames   []string
	}{
		{"无key无flag", "", "", "", []string{"mcp_fetch_webpage", "mcp_exec_command"}},
		{"仅paidKey", "bk", "", "", []string{"mcp_paid_search", "mcp_fetch_webpage", "mcp_exec_command"}},
		{"仅ddg", "", "1", "", []string{"mcp_free_search", "mcp_fetch_webpage", "mcp_exec_command"}},
		{"仅bing", "", "", "yes", []string{"mcp_free_search", "mcp_fetch_webpage", "mcp_exec_command"}},
		{"全量", "bk", "true", "on", []string{"mcp_paid_search", "mcp_free_search", "mcp_fetch_webpage", "mcp_exec_command"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("BOCHA_API_KEY", c.paidKey)
			t.Setenv("PERPLEXITY_API_KEY", "")
			t.Setenv("SERPER_API_KEY", "")
			t.Setenv("JINA_API_KEY", "")
			t.Setenv("FREE_SEARCH_DDG_ENABLED", c.ddgEnabled)
			t.Setenv("FREE_SEARCH_BING_ENABLED", c.bingEnabled)
			got := GetMcpTools()
			if len(got) != len(c.wantNames) {
				t.Fatalf("期望 %d 个，实际 %d", len(c.wantNames), len(got))
			}
			for i, want := range c.wantNames {
				if got[i].Card().Name != want {
					t.Errorf("[%d] 期望 %s 实际 %s", i, want, got[i].Card().Name)
				}
			}
		})
	}
}

// TestHasPaidSearchAPIKey_四env任一非空 验证四个 env 任一非空即命中
// Python: _has_paid_search_api_key (mcp_toolkits.py L15-22)
func TestHasPaidSearchAPIKey_四env任一非空(t *testing.T) {
	keys := []string{"BOCHA_API_KEY", "PERPLEXITY_API_KEY", "SERPER_API_KEY", "JINA_API_KEY"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			for _, k := range keys {
				t.Setenv(k, "")
			}
			if hasPaidSearchAPIKey() {
				t.Fatalf("%s 全空时应返回 false", key)
			}
			t.Setenv(key, "some-key")
			if !hasPaidSearchAPIKey() {
				t.Fatalf("%s 非空时应返回 true", key)
			}
		})
	}
}

// TestIsFreeSearchEnabled_两flag任一开启 验证 DDG/BING 任一 flag 开启即启用
// Python: _is_free_search_enabled (mcp_toolkits.py L32-36)
func TestIsFreeSearchEnabled_两flag任一开启(t *testing.T) {
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "")
	t.Setenv("FREE_SEARCH_BING_ENABLED", "")
	if isFreeSearchEnabled() {
		t.Fatal("两 flag 全空时应返回 false")
	}
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "1")
	if !isFreeSearchEnabled() {
		t.Fatal("DDG flag=1 时应返回 true")
	}
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "")
	t.Setenv("FREE_SEARCH_BING_ENABLED", "enabled")
	if !isFreeSearchEnabled() {
		t.Fatal("BING flag=enabled 时应返回 true")
	}
}

// TestEnvFlag_解析规则 验证 env 布尔解析的取值集合与默认值
// Python: _env_flag (mcp_toolkits.py L25-29)
func TestEnvFlag_解析规则(t *testing.T) {
	truthy := []string{"1", "true", "yes", "on", "enabled", "TRUE ", " true"}
	for _, v := range truthy {
		t.Setenv("MCP_TOOLKITS_TEST_FLAG", v)
		if !envFlag("MCP_TOOLKITS_TEST_FLAG", false) {
			t.Errorf("值 %q 应解析为 true（strip 后 lower）", v)
		}
	}
	falsy := []string{"0", "false", "no", "off", "disabled", " TrUeX"}
	for _, v := range falsy {
		t.Setenv("MCP_TOOLKITS_TEST_FLAG", v)
		if envFlag("MCP_TOOLKITS_TEST_FLAG", false) {
			t.Errorf("值 %q 应解析为 false", v)
		}
	}
	// 空值回退默认
	t.Setenv("MCP_TOOLKITS_TEST_FLAG", "")
	if !envFlag("MCP_TOOLKITS_TEST_FLAG", true) {
		t.Error("空值时应返回默认 true")
	}
	if envFlag("MCP_TOOLKITS_TEST_FLAG", false) {
		t.Error("空值时应返回默认 false")
	}
	// 未设置回退默认
	if !envFlag("MCP_TOOLKITS_UNSET_FLAG", true) {
		t.Error("未设置时应返回默认 true")
	}
	if envFlag("MCP_TOOLKITS_UNSET_FLAG", false) {
		t.Error("未设置时应返回默认 false")
	}
}
