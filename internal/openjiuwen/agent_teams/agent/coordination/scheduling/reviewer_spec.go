package scheduling

// reviewer_spec.go 把成员规格收窄为一次性评审人所需的最小面。
//
// scheduled 派发的 reviewer 只检查交付物并投票，因此不应继承 teammate 的
// 完整能力集（todo 计划、子代理、写权限、调度与消息工具）。框架按名字
// 收窄它认识的开关；装配成员能力的宿主认识其余的，通过 allow-list 收窄。
//
// Python: agent_teams/agent/scheduling/reviewer_spec.py（全文，2026-12-04 快照）。

import (
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ReviewerCompletionTimeoutSeconds 临时 reviewer 的完成超时（秒）。
// 临时 reviewer 经 run_once 运行，completion_timeout 是终止本轮的硬上限
// （对常驻成员只是慢轮日志阈值，宿主可设得远高于此）。钉住 reviewer 自己的
// 上限使卡死的评审在有限时间内结束：调度器在半程 steer 仍无票的 reviewer，
// 并在该上限终止运行后重派。
// Python: reviewer_spec.py L20-23。
const ReviewerCompletionTimeoutSeconds = 1200.0

// SysOperationType 文件系统 rail 类型名。
// Python: SYS_OPERATION (harness/manifest/builtin_elements.py L67)。
const SysOperationType = "core.sys_operation"

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildReviewerAgentSpec 从成员规格派生 reviewer 基础规格。
// 恒定应用：文件工具只读、harness todo 计划关闭、不挂子代理（声明或通用）、
// 本轮以 ReviewerCompletionTimeoutSeconds 封顶。MCP 服务器与原始工具卡片
// 原样继承。
//
// 参数：
//   - base: 派生源成员规格（通常是 teammate 规格）
//   - railTypes: 保留的 rail 类型；nil 保留全部声明 rail
//   - toolTypes: 保留的内置工具类型；nil 保留全部声明内置工具
//
// 返回新的 DeepAgentSpec；base 不被修改。
//
// Python: build_reviewer_agent_spec (reviewer_spec.py L36-70)。
func BuildReviewerAgentSpec(base *schema.DeepAgentSpec, railTypes []string, toolTypes []string) *schema.DeepAgentSpec {
	var rails []schema.RailSpec
	for _, rail := range base.Rails {
		if railTypes != nil && !containsString(railTypes, rail.Type) {
			continue
		}
		rails = append(rails, readOnlyRail(rail))
	}
	var tools []schema.BuiltinToolSpec
	for _, toolSpec := range base.BuiltinTools {
		if toolTypes != nil && !containsString(toolTypes, toolSpec.Type) {
			continue
		}
		tools = append(tools, toolSpec)
	}
	spec := *base
	spec.Rails = rails
	spec.BuiltinTools = tools
	// MCP 服务器与原始工具卡片原样继承（ToolCards/Mcps 保持 base 的切片）。
	spec.Subagents = nil
	spec.AddGeneralPurposeAgent = false
	spec.EnableAsyncSubagent = false
	spec.EnableTaskPlanning = false
	spec.CompletionTimeout = ReviewerCompletionTimeoutSeconds
	return &spec
}

// readOnlyRail 把文件系统 rail 切到只读；其他 rail 原样通过。
// Python: _read_only_rail (reviewer_spec.py L72-78)。
func readOnlyRail(rail schema.RailSpec) schema.RailSpec {
	if rail.Type != SysOperationType {
		return rail
	}
	params := map[string]any{}
	for key, value := range rail.Params {
		params[key] = value
	}
	params["read_only"] = true
	return schema.RailSpec{Type: rail.Type, Params: params}
}

// containsString 字符串包含判定。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
