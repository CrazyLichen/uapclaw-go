// 工具实例的装配期自动登记。
//
// ## 为什么需要这一步
//
// 偏差 V-18：Go 的工具是**函数式实现**（`NewXxxTool(...) tool.Tool`），没有
// 导出具体类型可供 `reflect.TypeOf` 做常量比对。三个绑定校验
// （readonly / code_control / subagent_control）因此改用「装配期登记类型表 +
// `ResolveToolBinding` 精确类型比对」。
//
// ## 为什么用观察者而不是直接调用
//
// 登记表需要「工具实例的真实类型」，而实例在
// `ProgressiveToolRail.Init` 等 agentcore 层的位置才构造出来。
// 若 `agentcore/harness/rails` 直接 import 本包会形成循环依赖
// （本包已依赖 `agentcore/harness/rails` 取 `ProgressiveToolRail` 类型）。
//
// 故由 agentcore 层暴露 `SetToolInstanceObserver` 回调，本包在 `init()` 里
// 注册观察者——依赖方向仍是 swarm → agentcore，单向外。

package auto_permission

import (
	"reflect"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// observedToolTypes 观察到的工具具体类型（工具名 → 类型），只增不减
	observedToolTypesMu sync.RWMutex
	observedToolTypes   = map[string]reflect.Type{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ObservedToolTypes 返回已观察到的工具类型表（测试与诊断用，拷贝）。
func ObservedToolTypes() map[string]reflect.Type {
	observedToolTypesMu.RLock()
	defer observedToolTypesMu.RUnlock()
	out := make(map[string]reflect.Type, len(observedToolTypes))
	for name, toolType := range observedToolTypes {
		out[name] = toolType
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册工具实例观察者。
//
// 只登记本批三个绑定校验关心的工具名，避免无界增长：
//   - tool_search      → readonly 快速路径
//   - enter_plan_mode / exit_plan_mode / switch_mode → code 控制路由
//   - sessions_* / subagent_* → 会话控制
func init() {
	rails.SetToolInstanceObserver(observeToolInstance)
}

// observeToolInstance 工具装配时的观察逻辑（具名以便单测直接驱动）。
func observeToolInstance(toolName string, instance tool.Tool) {
	{
		if _, watched := watchedToolNames[toolName]; !watched {
			return
		}
		if instance == nil {
			return
		}
		toolType := reflect.TypeOf(instance)
		if toolType == nil {
			return
		}
		observedToolTypesMu.Lock()
		observedToolTypes[toolName] = toolType
		observedToolTypesMu.Unlock()

		// 原生路径工具需要的是**实例**（路径冻结要校验指针同一性）
		if _, isNativePath := nativePathToolNames[toolName]; isNativePath {
			nativePathExecutorsMu.Lock()
			nativePathExecutors[toolName] = instance
			nativePathExecutorsMu.Unlock()
		}

		// 同步到三个绑定校验的类型表
		switch {
		case toolName == toolSearchName:
			SetReadonlyToolTypes(map[string]reflect.Type{toolName: toolType})
		case toolName == enterPlanModeTool || toolName == exitPlanModeTool ||
			toolName == switchModeTool:
			codeControlToolTypesMu.Lock()
			codeControlToolTypes[toolName] = toolType
			codeControlToolTypesMu.Unlock()
		case isSubagentRuntimeControlTool(toolName):
			subagentControlToolTypesMu.Lock()
			subagentControlToolTypes[toolName] = toolType
			subagentControlToolTypesMu.Unlock()
		}
	}
}

// watchedToolNames 需要观察的工具名闭集。
// 放在 init 之后定义，Go 允许（包级变量初始化早于 init）。
var watchedToolNames = map[string]struct{}{
	// 原生路径工具（需实例做路径冻结）
	"read_file": {}, "write_file": {}, "edit_file": {},
	"glob": {}, "grep": {}, "list_files": {}, "read_pdf": {},
	// 只读快速路径
	toolSearchName: {},
	// Code 控制路由
	enterPlanModeTool: {}, exitPlanModeTool: {}, switchModeTool: {},
	// 会话控制（Go 侧三个 + 上游六个名字全收）
	"sessions_spawn": {}, "sessions_list": {}, "sessions_cancel": {},
	"subagent_spawn": {}, "subagent_close": {}, "subagent_list": {},
	"subagent_resume": {}, "subagent_send_input": {}, "subagent_wait": {},
}
