package scheduling

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// verdict_reviewer_test.go verdict.py 全矩阵逐分支 + reviewer_spec.py 逐字段收窄。

// TestSettleReviewTally_全矩阵 verdict.py 全分支。
// Python: verdict.py L17-46。
func TestSettleReviewTally_全矩阵(t *testing.T) {
	cases := []struct {
		name      string
		tally     map[string]any
		threshold float64
		want      string
	}{
		{"二元池未投满", map[string]any{"verdict_total": 2, "verdict_voted": 1}, 0.85, VerdictUndecided},
		{"二元池有fail", map[string]any{"verdict_total": 2, "verdict_voted": 2, "verdict_fail_count": 1}, 0.85, VerdictFail},
		{"二元池全pass_inspector未投满", map[string]any{"verdict_total": 2, "verdict_voted": 2, "verdict_fail_count": 0, "inspector_count": 1, "inspector_voted": 0}, 0.85, VerdictUndecided},
		{"inspector平均分低于阈值", map[string]any{"verdict_total": 1, "verdict_voted": 1, "verdict_fail_count": 0, "inspector_count": 1, "inspector_voted": 1, "inspector_avg": 0.5}, 0.85, VerdictFail},
		{"inspector_avg缺失", map[string]any{"verdict_total": 1, "verdict_voted": 1, "verdict_fail_count": 0, "inspector_count": 1, "inspector_voted": 1}, 0.85, VerdictFail},
		{"两池全过", map[string]any{"verdict_total": 2, "verdict_voted": 2, "verdict_fail_count": 0, "inspector_count": 2, "inspector_voted": 2, "inspector_avg": 0.9}, 0.85, VerdictPass},
		{"仅二元池全过", map[string]any{"verdict_total": 1, "verdict_voted": 1, "verdict_fail_count": 0}, 0.85, VerdictPass},
		{"仅inspector池达标", map[string]any{"inspector_count": 1, "inspector_voted": 1, "inspector_avg": 0.85}, 0.85, VerdictPass},
		{"仅inspector池未达", map[string]any{"inspector_count": 1, "inspector_voted": 1, "inspector_avg": 0.84}, 0.85, VerdictFail},
		{"空计票", map[string]any{}, 0.85, VerdictPass},
		{"自定义阈值", map[string]any{"inspector_count": 1, "inspector_voted": 1, "inspector_avg": 0.9}, 0.95, VerdictFail},
	}
	for _, c := range cases {
		if got := SettleReviewTally(c.tally, c.threshold); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBuildReviewerAgentSpec_逐字段收窄 reviewer_spec.py 收窄断言。
// Python: reviewer_spec.py L36-70。
func TestBuildReviewerAgentSpec_逐字段收窄(t *testing.T) {
	base := &schema.DeepAgentSpec{
		Subagents:              []schema.SubAgentSpec{{}},
		AddGeneralPurposeAgent: true,
		EnableAsyncSubagent:    true,
		EnableTaskPlanning:     true,
		CompletionTimeout:      99999,
		Rails: []schema.RailSpec{
			{Type: SysOperationType, Params: map[string]any{"workspace": "/tmp/x"}},
			{Type: "skill_use", Params: nil},
		},
		BuiltinTools: []schema.BuiltinToolSpec{
			{Type: "sys_operation"},
			{Type: "todo"},
		},
	}

	spec := BuildReviewerAgentSpec(base, nil, nil)

	// 子代理清空；通用代理/异步子代理/任务计划关闭。
	if spec.Subagents != nil {
		t.Error("subagents must be nil")
	}
	if spec.AddGeneralPurposeAgent || spec.EnableAsyncSubagent || spec.EnableTaskPlanning {
		t.Error("capability switches must be false")
	}
	// 轮次封顶 1200。
	if spec.CompletionTimeout != 1200.0 {
		t.Errorf("completion_timeout = %v, want 1200", spec.CompletionTimeout)
	}
	// 文件 rail 只读；其他 rail 原样。
	if spec.Rails[0].Params["read_only"] != true {
		t.Error("sys_operation rail must be read-only")
	}
	if spec.Rails[0].Params["workspace"] != "/tmp/x" {
		t.Error("other params must be preserved")
	}
	if spec.Rails[1].Type != "skill_use" {
		t.Error("non-sys-operation rail must pass through")
	}
	// base 不被修改。
	if base.Rails[0].Params["read_only"] != nil {
		t.Error("base spec must be unmodified")
	}
	if base.Subagents == nil || base.CompletionTimeout != 99999 {
		t.Error("base spec fields must be unmodified")
	}
	// allow-list 收窄。
	narrowed := BuildReviewerAgentSpec(base, []string{"skill_use"}, []string{"todo"})
	if len(narrowed.Rails) != 1 || narrowed.Rails[0].Type != "skill_use" {
		t.Errorf("rail allow-list failed: %v", narrowed.Rails)
	}
	if len(narrowed.BuiltinTools) != 1 || narrowed.BuiltinTools[0].Type != "todo" {
		t.Errorf("tool allow-list failed: %v", narrowed.BuiltinTools)
	}
	// MCP 与原始工具卡片原样继承。
	if len(narrowed.ToolCards) != len(base.ToolCards) {
		t.Error("tool cards must be inherited")
	}
}

// TestReviewerCompletionTimeoutSeconds_值 常量对齐。
func TestReviewerCompletionTimeoutSeconds_值(t *testing.T) {
	if ReviewerCompletionTimeoutSeconds != 1200.0 {
		t.Fatalf("timeout = %v, want 1200", ReviewerCompletionTimeoutSeconds)
	}
}
