package harness

import (
	"testing"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAddDefaultRails_默认注册模型异常Rail 未显式传开关时默认注册（nil→true）。
func TestAddDefaultRails_默认注册模型异常Rail(t *testing.T) {
	agent := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("anomaly-test")))
	addDefaultRails(agent, hconfig.CreateDeepAgentParams{}, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if countAnomalyRails(agent) != 1 {
		t.Fatalf("默认应注册 1 个 ModelAnomalyDetectionRail，实际 %d", countAnomalyRails(agent))
	}
}

// TestAddDefaultRails_显式关闭不注册 开关为 false 时不注册。
func TestAddDefaultRails_显式关闭不注册(t *testing.T) {
	disabled := false
	agent := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("anomaly-test")))
	addDefaultRails(agent,
		hconfig.CreateDeepAgentParams{EnableModelAnomalyDetectionRail: &disabled},
		&hschema.DeepAgentConfig{}, nil, nil, "cn")

	if countAnomalyRails(agent) != 0 {
		t.Fatalf("显式关闭时不应注册，实际 %d", countAnomalyRails(agent))
	}
}

// TestAddDefaultRails_调用方已提供不重复 显式提供的同类型 rail 不重复追加。
func TestAddDefaultRails_调用方已提供不重复(t *testing.T) {
	provided, err := rails.NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	agent := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("anomaly-test")))
	params := hconfig.CreateDeepAgentParams{Rails: []agentinterfaces.AgentRail{provided}}
	addDefaultRails(agent, params, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if got := countAnomalyRails(agent); got != 0 {
		t.Fatalf("调用方已提供时不应再补默认，addDefaultRails 追加了 %d 个", got)
	}
	// 模拟工厂后续步骤挂载调用方显式提供的 rails
	for _, rail := range params.Rails {
		agent.AddRail(rail)
	}
	if got := countAnomalyRails(agent); got != 1 {
		t.Fatalf("同类型 rail 只能有一个，实际 %d", got)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countAnomalyRails 统计待挂载列表中的模型异常检测 Rail 数量。
func countAnomalyRails(agent *DeepAgent) int {
	agent.railsMu.Lock()
	defer agent.railsMu.Unlock()
	count := 0
	for _, rail := range agent.pendingRails {
		if _, ok := rail.(*rails.ModelAnomalyDetectionRail); ok {
			count++
		}
	}
	return count
}
