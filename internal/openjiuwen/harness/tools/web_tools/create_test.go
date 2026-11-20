package web_tools

import (
	"os"
	"testing"
)

// TestCreateWebTools 工厂函数
func TestCreateWebTools(t *testing.T) {
	// 清理环境
	for _, key := range paidSearchAPIKeyEnvs() {
		os.Unsetenv(key)
	}
	os.Unsetenv(freeSearchDDGEnabledEnv)
	os.Unsetenv(freeSearchBingEnabledEnv)

	// 只创建 fetch_webpage
	tools := CreateWebTools("cn", "test-agent", true, true, true)
	if len(tools) != 1 {
		t.Errorf("无搜索启用时应只有 fetch_webpage, got %d", len(tools))
	}
	if tools[0].Card().Name != "fetch_webpage" {
		t.Errorf("工具名 = %q, want %q", tools[0].Card().Name, "fetch_webpage")
	}

	// 启用免费搜索
	os.Setenv(freeSearchDDGEnabledEnv, "true")
	defer os.Unsetenv(freeSearchDDGEnabledEnv)
	tools = CreateWebTools("cn", "test-agent", true, true, true)
	if len(tools) != 2 {
		t.Errorf("免费搜索启用应有 2 个工具, got %d", len(tools))
	}

	// 启用付费搜索
	os.Setenv("BOCHA_API_KEY", "test-key")
	defer os.Unsetenv("BOCHA_API_KEY")
	tools = CreateWebTools("cn", "test-agent", true, true, true)
	if len(tools) != 3 {
		t.Errorf("全部启用应有 3 个工具, got %d", len(tools))
	}

	// 不包含
	tools = CreateWebTools("cn", "test-agent", false, false, false)
	if len(tools) != 0 {
		t.Errorf("全部不包含应有 0 个工具, got %d", len(tools))
	}
}
