package harness

import (
	"testing"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAddDefaultRails_默认注册模型异常Rail 未显式传开关时默认注册（nil→true）。
func TestAddDefaultRails_默认注册模型异常Rail(t *testing.T) {
	got := addDefaultRails(hconfig.CreateDeepAgentParams{}, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if countAnomalyRails(got) != 1 {
		t.Fatalf("默认应返回 1 个 ModelAnomalyDetectionRail，实际 %d", countAnomalyRails(got))
	}
}

// TestAddDefaultRails_显式关闭不注册 开关为 false 时不注册。
func TestAddDefaultRails_显式关闭不注册(t *testing.T) {
	disabled := false
	got := addDefaultRails(
		hconfig.CreateDeepAgentParams{EnableModelAnomalyDetectionRail: &disabled},
		&hschema.DeepAgentConfig{}, nil, nil, "cn")

	if countAnomalyRails(got) != 0 {
		t.Fatalf("显式关闭时不应注册，实际 %d", countAnomalyRails(got))
	}
}

// TestAddDefaultRails_调用方已提供不重复 显式提供的同类型 rail 不重复追加。
func TestAddDefaultRails_调用方已提供不重复(t *testing.T) {
	provided, err := rails.NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	params := hconfig.CreateDeepAgentParams{Rails: []agentinterfaces.AgentRail{provided}}
	got := addDefaultRails(params, &hschema.DeepAgentConfig{}, nil, nil, "cn")

	if n := countAnomalyRails(got); n != 0 {
		t.Fatalf("调用方已提供时不应再补默认，addDefaultRails 追加了 %d 个", n)
	}
	// 模拟工厂后续步骤挂载调用方显式提供的 rails
	all := append([]agentinterfaces.AgentRail{}, params.Rails...)
	all = append(all, got...)
	if n := countAnomalyRails(all); n != 1 {
		t.Fatalf("同类型 rail 只能有一个，实际 %d", n)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countAnomalyRails 统计 rail 列表中的模型异常检测 Rail 数量。
func countAnomalyRails(list []agentinterfaces.AgentRail) int {
	count := 0
	for _, rail := range list {
		if _, ok := rail.(*rails.ModelAnomalyDetectionRail); ok {
			count++
		}
	}
	return count
}
