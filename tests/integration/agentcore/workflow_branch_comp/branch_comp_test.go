//go:build integration

package workflow_branch_comp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	conditionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
)

// BranchCompSuite 8.19 BranchComp 全链集成。
type BranchCompSuite struct {
	suite.Suite
}

func TestBranchCompSuite(t *testing.T) {
	suite.Run(t, new(BranchCompSuite))
}

// newEnd 构造 End 组件（batch 模板收敛）。
func (s *BranchCompSuite) newEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	s.Require().NoError(err)
	e, err := flow.NewEnd(end)
	s.Require().NoError(err)
	return e
}

// newBranch 构造双出口分支组件：${amount} > 100 → high 出口，AlwaysTrue → low 出口。
// withNoMatch=true 时两条均为不可能命中的表达式（全不命中场景）。
func (s *BranchCompSuite) newBranch(withNoMatch bool) *flow.BranchComponent {
	b := flow.NewBranchComponent()
	if withNoMatch {
		s.Require().NoError(b.AddBranch("${amount} > 999999", "end_a", "high"))
		s.Require().NoError(b.AddBranch("${amount} < -999999", "end_b", "low"))
		return b
	}
	s.Require().NoError(b.AddBranch("${amount} > 100", "end_a", "high"))
	s.Require().NoError(b.AddBranch(conditionpkg.NewAlwaysTrue(), "end_b", "low"))
	return b
}

func (s *BranchCompSuite) Test双出口_表达式命中high出口() {
	w := workflow.NewWorkflow(nil)
	b := s.newBranch(false)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("branch", b, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	// designated end 为后注册者：high 用例命中 end_a，故 end_a 后注册
	w.SetEndComp("end_b", s.newEnd("low:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.SetEndComp("end_a", s.newEnd("high:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "branch")
	w.AddConditionalConnection("branch", b.Router())

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"amount": 150}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok, "invoke 应返回 *WorkflowOutput")
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(s.resultText(wo), "high", "命中 ${amount} > 100 应走 high 出口")
}

func (s *BranchCompSuite) TestAlwaysTrue兜底走low出口() {
	w := workflow.NewWorkflow(nil)
	b := s.newBranch(false)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("branch", b, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end_a", s.newEnd("high:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.SetEndComp("end_b", s.newEnd("low:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "branch")
	w.AddConditionalConnection("branch", b.Router())

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"amount": 1}, sess, nil, nil)
	s.Require().NoError(err)
	wo := out.(*workflow.WorkflowOutput)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(s.resultText(wo), "low", "表达式不命中应走 AlwaysTrue 兜底出口")
}

func (s *BranchCompSuite) Test全不命中_101021上抛() {
	w := workflow.NewWorkflow(nil)
	b := s.newBranch(true)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("branch", b, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end_a", s.newEnd("high:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.SetEndComp("end_b", s.newEnd("low:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "branch")
	w.AddConditionalConnection("branch", b.Router())

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	_, err := w.Invoke(context.Background(),
		map[string]any{"amount": 150}, sess, nil, nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "branch meeting the condition was not found",
		"全不命中应上抛 COMPONENT_BRANCH_EXECUTION_ERROR（101021）")
}

// TestCNF屏障_互斥出口汇合 mid 节点 wait_for_all=true、前驱 end_a/end_b 同属
// 分支组（互斥）：编译期 CNF 把 AND 屏障归并为 OR 组，单条命中即触发——
// 无 CNF 时双前驱屏障会永远等不到第二条消息而挂死。
func (s *BranchCompSuite) TestCNF屏障_互斥出口汇合() {
	w := workflow.NewWorkflow(nil)
	b := s.newBranch(false)
	waitForAll := true
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("branch", b, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddWorkflowComp("end_a", s.newEnd("high:{{amount}}"), nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddWorkflowComp("end_b", s.newEnd("low:{{amount}}"), nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddWorkflowComp("mid", s.newEnd("mid:done"), &waitForAll, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("final", s.newEnd("final:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "branch")
	w.AddConditionalConnection("branch", b.Router())
	w.AddConnection([]string{"end_a", "end_b"}, "mid")
	w.AddConnection([]string{"mid"}, "final")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := w.Invoke(ctx, map[string]any{"amount": 150}, sess, nil, nil)
	s.Require().NoError(err, "CNF 归并后单出口命中即触发 mid（否则屏障挂死/超时）")
	wo := out.(*workflow.WorkflowOutput)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(s.resultText(wo), "final")
}

// resultText 提取 WorkflowOutput 的文本形态（End batch 输出 map/字符串双形态）。
func (s *BranchCompSuite) resultText(wo *workflow.WorkflowOutput) string {
	switch v := wo.Result.(type) {
	case string:
		return v
	case map[string]any:
		s.T().Helper()
		var parts []string
		for _, val := range v {
			parts = append(parts, s.toString(val))
		}
		return stringsJoin(parts)
	case []workflow.WorkflowChunk:
		var parts []string
		for _, c := range v {
			parts = append(parts, s.toString(c))
		}
		return stringsJoin(parts)
	}
	return s.toString(wo.Result)
}

func (s *BranchCompSuite) toString(v any) string {
	if str, ok := v.(string); ok {
		return str
	}
	if m, ok := v.(map[string]any); ok {
		var parts []string
		for _, val := range m {
			parts = append(parts, s.toString(val))
		}
		return stringsJoin(parts)
	}
	if fmts, ok := v.(interface{ String() string }); ok {
		return fmts.String()
	}
	return ""
}

// stringsJoin 简易拼接（避免重复 import strings）
func stringsJoin(parts []string) string {
	out := ""
	for _, p := range parts {
		out += p + ";"
	}
	return out
}
