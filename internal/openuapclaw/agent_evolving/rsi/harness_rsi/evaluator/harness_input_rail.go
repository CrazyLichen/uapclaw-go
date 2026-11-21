// harness_input_rail.go 阻止评估输出写入已绑定的指纹化插件包。
// Python: openjiuwen/rsi/harness_rsi/evaluator/harness_input_rail.py（49 行全量对齐）。

package evaluator

import (
	"context"
	"fmt"
	"path/filepath"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/security/fileguard"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessInputRail 拒绝对已知插件的写入；评估器指纹是最后一道防线。
// Python: harness_input_rail.py:14 HarnessInputRail(BaseInterruptRail)。
type HarnessInputRail struct {
	*interrupt.BaseInterruptRail
	// workspace 评估工作区（拒绝文案引用）
	workspace string
	// checker 文件守卫（harness 路径 write=deny）
	checker *fileguard.FileGuardChecker
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHarnessInputRail 构造输入轨道。
// Python: harness_input_rail.py:17-30 __init__——harness 路径 write=deny、
// 其余缺省 allow，workspace_root 为评估工作区。
func NewHarnessInputRail(harnessPath, workspace string) *HarnessInputRail {
	resolvedHarness, err := filepath.Abs(harnessPath)
	if err != nil {
		resolvedHarness = harnessPath
	}
	resolvedWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		resolvedWorkspace = workspace
	}
	// Python: normalize_path_guard_config({"file_guard": {...}}, workspace_root=Path(workspace))
	checker := fileguard.BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{
			"enabled":  true,
			"defaults": map[string]any{"read": "allow", "write": "allow", "exec": "allow"},
			"paths": []any{map[string]any{
				"path":  resolvedHarness,
				"read":  "allow",
				"write": "deny",
				"exec":  "allow",
			}},
		},
	}, resolvedWorkspace, nil)
	return &HarnessInputRail{
		BaseInterruptRail: interrupt.NewBaseInterruptRail(),
		workspace:         resolvedWorkspace,
		checker:           checker,
	}
}

// BeforeToolCall 拒绝已知插件的写操作（Python: :32-49 before_tool_call）。
func (r *HarnessInputRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	// Python: args 为字符串时尝试 JSON 解析，失败则放行（原生工具报告畸形参数）。
	// Python: isinstance(args, str) → JSON 解析；非 dict 放行。
	// Go ToolArgs 为 map[string]any 具体类型（str 形态在传参层已解析）。
	args := inputs.ToolArgs
	if args == nil {
		return nil
	}
	result := r.checker.Evaluate(inputs.ToolName, args)
	if result != nil && result.IsDenied() {
		// Python: self._skip_tool(ctx, ctx.inputs.tool_call, 文案)——文案逐字。
		message := fmt.Sprintf(
			"[HARNESS_INPUT_READ_ONLY] The loaded plugin is an evaluation input, "+
				"not an output directory. This write was not executed. "+
				"Write the deliverable under `%s` instead. "+
				"Reading skills and using plugin tools remain allowed.",
			r.workspace)
		rejectToolCall(cbc, message)
	}
	return nil
}

// rejectToolCall 通用工具拒绝（Python _skip_tool 等价：skip 标记+预设结果+ToolMsg）。
func rejectToolCall(cbc *agentinterfaces.AgentCallbackContext, message string) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolCallID := ""
	if ok && inputs != nil && inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}
	cbc.SetExtra(extraSkipToolKeyLocal, true)
	if ok && inputs != nil {
		inputs.ToolResult = map[string]any{"error": message}
		inputs.ToolMsg = llmschema.NewToolMessage(toolCallID, message)
	}
}
