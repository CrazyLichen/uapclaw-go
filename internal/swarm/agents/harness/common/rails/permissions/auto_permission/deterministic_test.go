package auto_permission

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// deterministicFacts 构造确定性放行用例的 facts。
func deterministicFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName: "web_fetch",
			Category: "network",
			RiskTier: "low",
		},
		WorkspaceRoot: "/ws",
	}
}

// TestMaybeAllowDeterministic_非确定性路由 验证返回 Handled=false。
func TestMaybeAllowDeterministic_非确定性路由(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{Level: "ask", Source: permissions.SemanticReviewSource}
	got := rail.maybeAllowDeterministicSandboxScope(deterministicFacts(), deterministicAllowOptions{
		Route: route,
	})
	if got.Handled {
		t.Error("non-deterministic route should not be handled")
	}
}

// TestMaybeAllowDeterministic_nil路由 验证 nil 安全。
func TestMaybeAllowDeterministic_nil路由(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.maybeAllowDeterministicSandboxScope(deterministicFacts(), deterministicAllowOptions{})
	if got.Handled {
		t.Error("nil route should not be handled")
	}
}

// TestMaybeAllowDeterministic_有领域路由 验证不抢领域路由的判定。
func TestMaybeAllowDeterministic_有领域路由(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{Level: "allow", Source: permissions.RecentFetchSource}
	got := rail.maybeAllowDeterministicSandboxScope(deterministicFacts(), deterministicAllowOptions{
		Route:       route,
		DomainRoute: &permissions.DecisionRoute{Level: "ask", Source: permissions.SemanticReviewSource},
	})
	if got.Handled {
		t.Error("domain route present should not be handled")
	}
}

// TestMaybeAllowDeterministic_放行 验证确定性放行。
func TestMaybeAllowDeterministic_放行(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{Level: "allow", Source: permissions.RecentFetchSource}
	got := rail.maybeAllowDeterministicSandboxScope(deterministicFacts(), deterministicAllowOptions{
		Route:       route,
		PolicyLevel: permissions.AutoDecisionAllowLevel,
		ChannelKind: "web",
	})
	if !got.Handled {
		t.Fatal("deterministic route should be handled")
	}
	if got.DecisionSource != DeterministicBoundedScopeDecisionSource {
		t.Errorf("decision_source = %q", got.DecisionSource)
	}
	if got.Result != nil {
		t.Errorf("result should be nil, got %v", got.Result)
	}
}

// TestReviewerRoute_包装 验证路由包装调用。
func TestReviewerRoute_包装(t *testing.T) {
	rail := newBridgeRail(t)
	rail.workspaceRoot = "/ws"
	route := rail.reviewerRoute(
		deterministicFacts(),
		permissions.AutoDecisionDenyLevel,
		"",
		nil, nil, nil,
	)
	if route.Source != "hard_guard" {
		t.Errorf("deny policy should route to hard_guard, got %q", route.Source)
	}
}

// TestBoundedWriteMaxFiles 验证类型转换与默认值。
func TestBoundedWriteMaxFiles(t *testing.T) {
	cases := []struct {
		name string
		opts map[string]any
		want int
	}{
		{"缺失用默认", map[string]any{}, defaultBoundedWriteMaxFiles},
		{"int", map[string]any{"bounded_write_max_files": 7}, 7},
		{"float64", map[string]any{"bounded_write_max_files": 5.0}, 5},
		{"非法类型用默认", map[string]any{"bounded_write_max_files": "x"}, defaultBoundedWriteMaxFiles},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := boundedWriteMaxFiles(c.opts); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestBoundedWriteExcludedPaths 验证路径列表转换。
func TestBoundedWriteExcludedPaths(t *testing.T) {
	// 缺失
	if got := boundedWriteExcludedPaths(map[string]any{}); got != nil {
		t.Errorf("missing -> %v", got)
	}
	// []string 直返
	if got := boundedWriteExcludedPaths(map[string]any{
		"bounded_write_excluded_paths": []string{"/a"},
	}); len(got) != 1 || got[0] != "/a" {
		t.Errorf("[]string -> %v", got)
	}
	// []any 转换
	if got := boundedWriteExcludedPaths(map[string]any{
		"bounded_write_excluded_paths": []any{"/a", 42, "/b"},
	}); len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Errorf("[]any -> %v（非字符串应被跳过）", got)
	}
	// 非法类型
	if got := boundedWriteExcludedPaths(map[string]any{
		"bounded_write_excluded_paths": "x",
	}); got != nil {
		t.Errorf("invalid -> %v", got)
	}
}

// TestBoolPtr 验证指针辅助。
func TestBoolPtr(t *testing.T) {
	ptr := boolPtr(true)
	if ptr == nil || *ptr != true {
		t.Error("boolPtr broken")
	}
}
