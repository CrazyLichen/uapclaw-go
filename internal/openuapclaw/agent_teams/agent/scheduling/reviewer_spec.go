// Python 源：openjiuwen/agent_teams/agent/scheduling/reviewer_spec.py（80 行 1:1）
//
// 把成员 spec 收窄成一次性 reviewer 所需的基座。scheduled-dispatch 的 reviewer
// 只检查交付物并投票，不继承 teammate 的完整能力面（todo planning、sub-agents、
// 写权限、调度与消息工具）。框架只收它认识名字的能力开关；装配成员能力的 host
// 认识其余，经 allow-list 收窄。

package scheduling

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/manifest"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ReviewerCompletionTimeoutSeconds reviewer 完成超时（秒）。
// 一次性 reviewer 走 run_once，completion_timeout 是硬性上限（会取消该轮）；
// 常驻成员只作慢轮日志阈值，host 可设得远高。钉 reviewer 自己的上限让卡死的
// 评审在有限时间内结束：调度器在中途 steer 仍无投票的 reviewer，超时结束后
// 再重派一次。
// Python: REVIEWER_COMPLETION_TIMEOUT_SECONDS (reviewer_spec.py:24)
const ReviewerCompletionTimeoutSeconds = 1200.0

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildReviewerAgentSpec 从成员 spec 派生 reviewer 基座 spec。
// 恒定应用：文件工具只读化、harness todo planning 关、不挂任何子代理（声明的
// 与 general-purpose 都不挂）、该轮钉 1200s 超时。MCP servers 与原始工具卡片
// 原样继承。
// base 不被修改；返回新 spec（Python model_copy(update=...) 语义）。
//
// Python: build_reviewer_agent_spec (reviewer_spec.py:32-70)
func BuildReviewerAgentSpec(base *hschema.DeepAgentSpec, railTypes []string, toolTypes []string) *hschema.DeepAgentSpec {
	// rails：逐个只读化；railTypes 为 nil 保留全部声明 rail。
	// Python: mcp_config.py:50-54 列表推导（reviewer_spec.py:50-54）
	rails := make([]hschema.RailSpec, 0, len(base.Rails))
	for _, rail := range base.Rails {
		if railTypes != nil && !railTypesContain(railTypes, rail.Type) {
			continue
		}
		rails = append(rails, readOnlyRail(rail))
	}
	// tools：BuiltinToolSpec 按白名单过滤；非 BuiltinToolSpec 形态（原始工具卡片）
	// 恒保留——Go 侧原始工具卡片在 ToolCards 字段，白名单只作用于 BuiltinTools。
	// Python: reviewer_spec.py:55-59
	builtinTools := make([]hschema.BuiltinToolSpec, 0, len(base.BuiltinTools))
	for _, spec := range base.BuiltinTools {
		if toolTypes != nil && !railTypesContain(toolTypes, spec.Type) {
			continue
		}
		builtinTools = append(builtinTools, spec)
	}
	// Python: base.model_copy(update={...})——浅拷贝后覆写七项。
	reviewer := *base
	reviewer.Rails = rails
	reviewer.BuiltinTools = builtinTools
	reviewer.Subagents = nil
	reviewer.AddGeneralPurposeAgent = false
	reviewer.EnableAsyncSubagent = false
	reviewer.EnableTaskPlanning = false
	reviewer.CompletionTimeout = ReviewerCompletionTimeoutSeconds
	return &reviewer
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// railTypesContain 白名单包含判断（Python `x in collection`）。
// Python 无对应独立函数（集合成员判断内联在推导式中）。
func railTypesContain(types []string, target string) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}

// readOnlyRail 把文件系统 rail 切到只读；其他 rail 原样透传。
// params 换新 map（Python model_copy(update={"params": {**old, "read_only": True}})），
// 原 rail.Params 不被改动。
// Python: _read_only_rail (reviewer_spec.py:73-77)
func readOnlyRail(rail hschema.RailSpec) hschema.RailSpec {
	if rail.Type != manifest.SysOperation {
		return rail
	}
	params := make(map[string]any, len(rail.Params)+1)
	for k, v := range rail.Params {
		params[k] = v
	}
	params["read_only"] = true
	return hschema.RailSpec{Type: rail.Type, Params: params}
}
