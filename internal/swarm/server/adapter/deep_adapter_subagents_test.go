package adapter

import (
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// fakeConfigLister 测试用自定义 agent 列表。
type fakeConfigLister struct {
	defs []*types.AgentDefinition
}

// ListCustomAgents 返回预置的自定义 agent 定义。
func (f *fakeConfigLister) ListCustomAgents() []*types.AgentDefinition {
	return f.defs
}

// newAdapterWithModel 构造带可用模型的 adapter，供子代理构建路径使用。
func newAdapterWithModel(t *testing.T) *DeepAdapter {
	t.Helper()
	d := NewDeepAdapter()
	if model := d.createModel(map[string]any{
		"models": map[string]any{
			"defaults": []any{modelEntry("m1", 8000, true, "")},
		},
	}); model == nil {
		t.Fatal("测试模型构建失败")
	}
	return d
}

// TestBuildConfiguredSubagents_ResearchEnabled 覆盖 research_agent 构建分支。
func TestBuildConfiguredSubagents_ResearchEnabled(t *testing.T) {
	d := newAdapterWithModel(t)
	config := map[string]any{
		"subagents": map[string]any{
			"research_agent": map[string]any{"enabled": true},
		},
	}
	specs, _ := d.buildConfiguredSubagents(config, map[string]any{})
	if len(specs) != 1 {
		t.Fatalf("research_agent 启用后应有 1 个子代理，实际 %d", len(specs))
	}
}

// TestLoadCustomSubagents_自定义Agent 覆盖自定义 agent 列表读取与转换。
func TestLoadCustomSubagents_自定义Agent(t *testing.T) {
	d := newAdapterWithModel(t)
	d.SetConfigLister(&fakeConfigLister{defs: []*types.AgentDefinition{
		{Name: "custom1", Description: "自定义", Source: "user", Prompt: "你是自定义代理"},
	}})

	config := map[string]any{
		"subagents": map[string]any{
			"custom1": map[string]any{"enabled": true},
		},
	}
	specs, _ := d.buildConfiguredSubagents(config, map[string]any{})
	if len(specs) != 1 {
		t.Fatalf("自定义 agent 启用后应有 1 个子代理，实际 %d", len(specs))
	}
}

// TestCodeAdapterBuildPermissionRail_默认模型回退 覆盖 llmModel 为空时取 adapter 快照。
func TestCodeAdapterBuildPermissionRail_默认模型回退(t *testing.T) {
	c := NewCodeAdapter()
	if model := c.deep.createModel(map[string]any{
		"models": map[string]any{
			"defaults": []any{modelEntry("m1", 8000, true, "")},
		},
	}); model == nil {
		t.Fatal("测试模型构建失败")
	}

	configBase := map[string]any{
		"permissions": map[string]any{"enabled": true},
	}
	// 只验证 llmModel==nil 分支被执行且不 panic；具体 rail 构造依赖权限 schema
	_ = c.buildPermissionRail(configBase, nil, "")
}

// TestDeepAdapterBuildConfiguredSubagents_BrowserAgent 锁定 Deep 模式 browser 装配行为，
// 作为 buildBrowserSubagentSpec 抽取前后的特征测试。
func TestDeepAdapterBuildConfiguredSubagents_BrowserAgent(t *testing.T) {
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_DRIVER", "")
	t.Setenv("BROWSER_MANAGED_BINARY", "")

	d := newAdapterWithModel(t)
	specs, _ := d.buildConfiguredSubagents(map[string]any{
		"max_iterations": 20,
		"subagents": map[string]any{
			"browser_agent": map[string]any{"max_iterations": 7},
		},
	}, map[string]any{
		"browser": map[string]any{"chrome_path": "/opt/chrome"},
	})

	browser := findSubagentSpecByName(specs, subagents.BrowserAgentFactoryName)
	if browser == nil {
		t.Fatal("Deep 模式 browser runtime 开启时应装配 browser_agent")
	}
	if browser.MaxIterations != 7 {
		t.Errorf("browser_agent MaxIterations = %d, want 7", browser.MaxIterations)
	}
	if got := os.Getenv("BROWSER_DRIVER"); got != "managed" {
		t.Errorf("BROWSER_DRIVER = %q, want %q", got, "managed")
	}
	if got := os.Getenv("BROWSER_MANAGED_BINARY"); got != "/opt/chrome" {
		t.Errorf("BROWSER_MANAGED_BINARY = %q, want %q", got, "/opt/chrome")
	}
}
