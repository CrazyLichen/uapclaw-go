// harness_structure_rail.go 向 planner 系统提示词注入目标 harness 结构摘要。
// Python: agents/rails/harness_structure_rail.py HarnessStructureRail(DeepAgentRail)。

package rails

import (
	"context"
	"strings"

	harnessrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 常量 ────────────────────────────

// sectionName 注入的提示词分段名。
const sectionName = "member_harness_structure"

// sectionPriority 分段优先级（Python _SECTION_PRIORITY = 88）。
const sectionPriority = 88

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessStructureRail 把目标 harness 结构摘要注入 planner 系统提示词。
// Python: harness_structure_rail.py HarnessStructureRail(DeepAgentRail)。
type HarnessStructureRail struct {
	harnessrails.DeepAgentRail
	// harnessSummaries 角色 → 结构摘要
	harnessSummaries map[string]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHarnessStructureRail 构造结构摘要护栏。
func NewHarnessStructureRail(harnessSummaries map[string]string) *HarnessStructureRail {
	copied := map[string]string{}
	for k, v := range harnessSummaries {
		copied[k] = v
	}
	rail := &HarnessStructureRail{harnessSummaries: copied}
	// Python: priority = 88
	rail.WithPriority(sectionPriority)
	return rail
}

// UpdateHarnessSummaries 更新结构摘要。
// Python: harness_structure_rail.py update_harness_summaries。
func (r *HarnessStructureRail) UpdateHarnessSummaries(harnessSummaries map[string]string) {
	copied := map[string]string{}
	for k, v := range harnessSummaries {
		copied[k] = v
	}
	r.harnessSummaries = copied
}

// BeforeModelCall 按摘要有无增删提示词分段。
// Python: harness_structure_rail.py before_model_call。
//
// Go 偏差（D-5）：Python 经 agent.system_prompt_builder 增删 PromptSection；
// Go DeepAgent 提示词分段机制不同，本护栏以 extra 摘要键传给提示词装配层，
// 由 harness 侧消费。Python 行为（空摘要→移除分段）在此处等价承载。
func (r *HarnessStructureRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if len(r.harnessSummaries) == 0 {
		cbc.SetExtra("_harness_structure_section", "")
		return nil
	}
	// Python: content_lines = ["## Role Harness Structure", "", ...]
	lines := []string{
		"## Role Harness Structure",
		"",
		"The following summaries describe the REAL existing harness surfaces for each role.",
		"Prefer plans that modify these existing surfaces instead of inventing missing files.",
		"",
	}
	for role, summary := range r.harnessSummaries {
		lines = append(lines, "- "+role+": "+summary)
	}
	cbc.SetExtra("_harness_structure_section", strings.Join(lines, "\n"))
	return nil
}

// GetCallbacks 声明覆盖的钩子。
func (r *HarnessStructureRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}
