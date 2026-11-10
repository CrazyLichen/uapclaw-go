// 权限 rail 组的装配配方与装配自检。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/permission_rail_group.py
// （117 行）
//
// 作用：给出「一次权限 rail 装配」的完整配方，并提供两道校验——
// `validateComposition` 查候选集合成员关系，`verify` 查 SDK 图里实际注册的
// 就是这批实例（图身份，不是字段相等）。
//
// 冷安装与会话替换复用同一配方，避免两处装配漂移。
//
// ## 为什么需要它
//
// 三个 root rail（`RootPermissionQueueRail` / `RootContextRail` /
// `RootPermissionCompletionRail`）共享**同一个** `RootPermissionQueue` 实例。
// 若各装配点各自 new 一个队列，rail 之间就看不到彼此写入的卡——
// `before_tool` 的 resume 分支会永远拿不到数据。

package permissions

import (
	"errors"
	"reflect"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionRailGroup 一次权限 rail 装配的候选集合。
//
// Python: @dataclass(frozen=True) PermissionRailGroup
// 字段全部可为 nil（不参与本次装配）。
type PermissionRailGroup struct {
	// PermissionRail 权限中断轨（PermissionInterruptRail 或 AutoPermissionRail）
	PermissionRail agentinterfaces.AgentRail
	// RootPermissionQueueRail 号码牌与闩锁轨
	RootPermissionQueueRail *RootPermissionQueueRail
	// RootContextRail 决策上下文绑定轨
	RootContextRail *RootContextRail
	// RootPermissionCompletionRail 任务完成清理轨
	RootPermissionCompletionRail *RootPermissionCompletionRail
	// StreamEventRail 流事件轨
	StreamEventRail *JiuClawStreamEventRailBridge
	// AskUserRail 结构化询问轨
	AskUserRail *StructuredAskUserRailBridge
}

// JiuClawStreamEventRailBridge 流事件轨接线桥。
//
// Python 的 `JiuSwarmStreamEventRail` 在 Go 侧名为 `JiuClawStreamEventRail`
// （品牌映射）。这里用桥结构承载「装配后需要校验的 queue 引用」，
// 避免 permissions 子包直接依赖 common/rails 的具体类型。
type JiuClawStreamEventRailBridge struct {
	// Rail 实际 rail 实例
	Rail agentinterfaces.AgentRail
	// RootPermissionQueue 装配时注入的队列（供 verify 校验图身份）
	RootPermissionQueue *RootPermissionQueue
}

// PermissionRailGroupOptions NewPermissionRailGroup 的入参。
//
// Python: build_permission_group 的关键字参数
type PermissionRailGroupOptions struct {
	// PermissionRail 权限中断轨（已由 permission_builder 构造）
	PermissionRail agentinterfaces.AgentRail
	// Queue 三个 root rail 共享的队列实例
	Queue *RootPermissionQueue
	// AnswerClaimed 答案被认领时的回调
	AnswerClaimed func(*ToolInvocationKeyV1)
	// RootContextRail 决策上下文轨（已构造）
	RootContextRail *RootContextRail
	// StreamEventRail 流事件轨桥
	StreamEventRail *JiuClawStreamEventRailBridge
	// AskUserRail 结构化询问轨桥
	AskUserRail *StructuredAskUserRailBridge
}

// StructuredAskUserRailBridge 结构化询问轨接线桥。
type StructuredAskUserRailBridge struct {
	// Rail 实际 rail 实例
	Rail agentinterfaces.AgentRail
	// StrictContinuationContract 装配时注入的严格续接契约标记
	StrictContinuationContract bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionRailGroup 按配方构造 rail 组候选。
//
// Python: build_permission_group(config, *, permission_builder, permission_inputs,
// queue, answer_claimed, sandboxed, language)
//
// 只返回候选，注册与发布由调用方负责（对齐 Python 的
// "Return candidates only; the caller owns registration and publication"）。
func NewPermissionRailGroup(opts PermissionRailGroupOptions) *PermissionRailGroup {
	return &PermissionRailGroup{
		PermissionRail:               opts.PermissionRail,
		RootPermissionQueueRail:      NewRootPermissionQueueRail(opts.Queue, opts.AnswerClaimed),
		RootContextRail:              opts.RootContextRail,
		RootPermissionCompletionRail: NewRootPermissionCompletionRail(opts.Queue),
		StreamEventRail:              opts.StreamEventRail,
		AskUserRail:                  opts.AskUserRail,
	}
}

// Rails 返回非 nil 的 rail 列表。
//
// Python: PermissionRailGroup.rails()
func (g *PermissionRailGroup) Rails() []agentinterfaces.AgentRail {
	candidates := []agentinterfaces.AgentRail{
		g.PermissionRail,
		nilIfNilRail(g.RootPermissionQueueRail),
		nilIfNilRail(g.RootContextRail),
		nilIfNilRail(g.RootPermissionCompletionRail),
		nilIfNilRailOfBridge(g.StreamEventRail),
		nilIfNilRailOfAskBridge(g.AskUserRail),
	}
	out := make([]agentinterfaces.AgentRail, 0, len(candidates))
	for _, rail := range candidates {
		if rail != nil {
			out = append(out, rail)
		}
	}
	return out
}

// ValidateComposition 校验候选集合的成员关系（不要求已注册到 SDK）。
//
// Python: PermissionRailGroup.validate_composition(rails, *, smart, sys_operation)
//
// smart 为 true 时，三个 root rail + stream rail 必须**各恰好一个**，
// 且候选集合里那个必须就是本组持有的实例（图身份，不是类型相符）。
func (g *PermissionRailGroup) ValidateComposition(
	rails []agentinterfaces.AgentRail,
	smart bool,
	sysOperation any,
) error {
	if smart {
		checks := []struct {
			name     string
			railType reflect.Type
			held     agentinterfaces.AgentRail
		}{
			{"root_permission_queue_rail", reflect.TypeOf((*RootPermissionQueueRail)(nil)), nilIfNilRail(g.RootPermissionQueueRail)},
			{"root_context_rail", reflect.TypeOf((*RootContextRail)(nil)), nilIfNilRail(g.RootContextRail)},
			{"root_permission_completion_rail", reflect.TypeOf((*RootPermissionCompletionRail)(nil)), nilIfNilRail(g.RootPermissionCompletionRail)},
			{"stream_event_rail", streamRailType(g.StreamEventRail), nilIfNilRailOfBridge(g.StreamEventRail)},
		}
		for _, check := range checks {
			if countType(rails, check.railType) != 1 {
				return errors.New("required_agent_rail_count_invalid:" + check.name)
			}
			if countIdentity(rails, check.held) != 1 {
				return errors.New("required_agent_rail_graph_identity_mismatch:" + check.name)
			}
		}
	}
	expectedPermissionCount := 0
	if smart || g.PermissionRail != nil {
		expectedPermissionCount = 1
	}
	if countPermissionRails(rails) != expectedPermissionCount {
		return errors.New("required_permission_rail_count_invalid")
	}
	if g.PermissionRail == nil {
		if smart {
			return errors.New("required_permission_rail_identity_mismatch")
		}
		return nil
	}
	if countIdentity(rails, g.PermissionRail) != 1 {
		return errors.New("required_permission_rail_identity_mismatch")
	}
	// Python: isinstance(permission, AutoPermissionInterruptRail) is not smart
	if isAutoPermissionRail(g.PermissionRail) != smart {
		return errors.New("required_permission_rail_type_invalid")
	}
	_ = sysOperation
	return nil
}

// Verify 校验 SDK 图里注册的就是这批实例。
//
// Python: PermissionRailGroup.verify(instance, *, smart, queue, sys_operation)
// 校验的是「实际注册图」，不是适配器字段或配置清单。
func (g *PermissionRailGroup) Verify(
	registered []agentinterfaces.AgentRail,
	smart bool,
	queue *RootPermissionQueue,
	sysOperation any,
) error {
	wanted := g.Rails()
	if len(registered) != len(wanted) {
		return errors.New("permission_registered_graph_mismatch")
	}
	for _, item := range wanted {
		if countIdentity(registered, item) != 1 {
			return errors.New("permission_registered_graph_mismatch")
		}
	}
	if err := g.ValidateComposition(registered, smart, sysOperation); err != nil {
		return err
	}
	// Python: stream._root_permission_queue is (queue if smart else None)
	var expectedQueue *RootPermissionQueue
	if smart {
		expectedQueue = queue
	}
	if g.StreamEventRail == nil || g.StreamEventRail.RootPermissionQueue != expectedQueue {
		return errors.New("permission_stream_rail_unavailable")
	}
	// Python: ask._strict_continuation_contract is smart
	if g.AskUserRail == nil || g.AskUserRail.StrictContinuationContract != smart {
		return errors.New("permission_ask_rail_unavailable")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nilIfNilRail 把 typed-nil 指针转成真正的 nil 接口。
//
// Go 的 typed nil 装进接口后不等于 nil，会破坏 `rail != nil` 判断。
func nilIfNilRail(rail agentinterfaces.AgentRail) agentinterfaces.AgentRail {
	if rail == nil {
		return nil
	}
	value := reflect.ValueOf(rail)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return nil
	}
	return rail
}

// streamRailType 返回流事件轨桥里 rail 的具体类型（无桥返回 nil）。
//
// Python 用 JiuSwarmStreamEventRail 类型做 count；Go 的 bridge 包了一层，
// 按实际实例类型取，保证与 countType 的精确类型比较一致。
func streamRailType(bridge *JiuClawStreamEventRailBridge) reflect.Type {
	if bridge == nil || bridge.Rail == nil {
		return nil
	}
	return reflect.TypeOf(bridge.Rail)
}

// nilIfNilRailOfBridge 取流事件轨桥的 rail（nil 安全）。
func nilIfNilRailOfBridge(bridge *JiuClawStreamEventRailBridge) agentinterfaces.AgentRail {
	if bridge == nil {
		return nil
	}
	return bridge.Rail
}

// nilIfNilRailOfAskBridge 取询问轨桥的 rail（nil 安全）。
func nilIfNilRailOfAskBridge(bridge *StructuredAskUserRailBridge) agentinterfaces.AgentRail {
	if bridge == nil {
		return nil
	}
	return bridge.Rail
}

// countPermissionRails 统计 rails 里权限中断轨的实例数。
//
// Python: sum(isinstance(rail, PERMISSION_RAIL_TYPES) for rail in rails)
// Go 侧 permissions 子包不能引用 auto_permission 子包的类型（循环依赖），
// 改用反射检查是否实现权限轨标识接口。
func countPermissionRails(rails []agentinterfaces.AgentRail) int {
	count := 0
	for _, rail := range rails {
		if rail == nil {
			continue
		}
		if _, ok := rail.(harnesssecurity.PermissionRailMarker); ok {
			count++
		}
	}
	return count
}

// isAutoPermissionRail 判定该轨是否为自动权限轨。
//
// Python: isinstance(permission, AutoPermissionInterruptRail)
func isAutoPermissionRail(rail agentinterfaces.AgentRail) bool {
	if rail == nil {
		return false
	}
	_, ok := rail.(harnesssecurity.AutoPermissionRailMarker)
	return ok
}

// countType 统计 rails 里指定类型的实例数（railType 为 nil 时返回 0）。
func countType(rails []agentinterfaces.AgentRail, railType reflect.Type) int {
	if railType == nil {
		return 0
	}
	count := 0
	for _, rail := range rails {
		if reflect.TypeOf(rail) == railType {
			count++
		}
	}
	return count
}

// countIdentity 统计 rails 里与 target 同一实例的个数（指针同一性）。
func countIdentity(rails []agentinterfaces.AgentRail, target agentinterfaces.AgentRail) int {
	if target == nil {
		return 0
	}
	targetPtr := reflect.ValueOf(target).Pointer()
	count := 0
	for _, rail := range rails {
		if rail == nil {
			continue
		}
		if reflect.ValueOf(rail).Pointer() == targetPtr {
			count++
		}
	}
	return count
}
