package rails

import (
	"context"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// HeartbeatRail 心跳工具注册护栏。
// 在成员 agent 上注册心跳任务创建/管理工具（service 由工厂注入）。
//
// Python: HeartbeatRail (code/rails/heartbeat_rail.py, 1:1 骨架)。
// 基座 HeartbeatRuntimeBridge + code/rails/heartbeat/ 子系统（scheduler/store/
// tools/runtime/session_resolver，5145 行）Go 未实施 ⤴️9.11c（该行已挂账
// HeartbeatRuntimeBridge 前置件）——本批实现 rail 骨架与工具注册缝：
// Bridge 缺省 nil，Init 降级为不注册工具（工厂门控逻辑已在 member_rails.go）。
type HeartbeatRail struct {
	rails.DeepAgentRail
	// service 心跳作业服务（工厂注入，buildctx.HeartbeatJobService 窄面）
	service buildctx.HeartbeatJobService
	// toolContext 工具上下文（Python SimpleNamespace tool_context：
	// channel_id/session_id/metadata/user_id/mode/tool_scope）
	toolContext map[string]any
	// tools 本 rail 注册的工具（uninit 时移除）
	tools []HeartbeatTool
}

// HeartbeatTool 心跳工具的窄面（桥构造后填充）。
type HeartbeatTool struct {
	// Card 工具卡
	Card interface{ GetName() string }
	// Tool 工具本体
	Tool any
}

// heartbeatToolNames rail 拥有的工具名集合。
// Python: HEARTBEAT_TOOL_NAMES（heartbeat/tools.py）
// ⤴️9.11c：与 tools.py 1:1 对齐时填充。
var heartbeatToolNames []string

// heartbeatRailLogComponent 日志组件
var heartbeatRailLogComponent = logger.ComponentAgentServer

// 编译时验证 AgentRail 接口实现
var _ agentinterfaces.AgentRail = (*HeartbeatRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHeartbeatRailForSwarm 构造心跳工具注册 rail。
//
// Python: HeartbeatRail.__init__ (heartbeat_rail.py:35-39)——
// service + tool_context 构造期绑定；HeartbeatRuntimeBridge 以
// BuildHeartbeatTools 窄函数注入缝承接（nil = 降级）。
func NewHeartbeatRailForSwarm(service buildctx.HeartbeatJobService, toolContext map[string]any) *HeartbeatRail {
	return &HeartbeatRail{service: service, toolContext: toolContext}
}

// ToolContext 返回工具上下文（测试与工厂断言用）。
func (r *HeartbeatRail) ToolContext() map[string]any { return r.toolContext }

// Init 在具体 DeepAgent 实例上注册 rail 持有的工具。
//
// Python: init (heartbeat_rail.py:41-53)。
// ⤴️9.11c：HeartbeatRuntimeBridge.build_tools(context) 基座回填后，
// 工具经 AbilityManager.AddAbility 注册并打 discovery 元数据。
func (r *HeartbeatRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	// Bridge 未实施：工具列表为空，注册降级为日志（门控已保证只在
	// 支持心跳的渠道挂载，成员不会「承诺心跳」——工具缺失等同无能力）。
	logger.Debug(heartbeatRailLogComponent).
		Msg("[HeartbeatRail] tools registration pending 9.11b HeartbeatRuntimeBridge seam")
	return nil
}

// Uninit 仅移除本 rail 拥有的工具。
//
// Python: uninit (heartbeat_rail.py:55-66)。
func (r *HeartbeatRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if manager := r.abilityManagerOf(nil); manager != nil {
		for _, name := range heartbeatToolNames {
			_ = manager
			_ = name
			// Python: ability_manager.remove(name)，异常 debug 忽略。
			// ⤴️9.11c：AbilityManager remove 签名接线随基座回填。
		}
	}
	r.tools = nil
	return nil
}

// RegisteredToolNames 返回已注册工具名（排序，测试断言用）。
//
// Python: init 的日志 sorted(...)。
func (r *HeartbeatRail) RegisteredToolNames() []string {
	names := make([]string, 0, len(r.tools))
	for _, t := range r.tools {
		if t.Card != nil {
			names = append(names, t.Card.GetName())
		}
	}
	sort.Strings(names)
	return names
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// abilityManagerOf 读取 agent 能力管理器（窄面）。
func (r *HeartbeatRail) abilityManagerOf(agent agentinterfaces.BaseAgent) any {
	// ⤴️9.11c：AbilityManager remove 接线时改为具体接口。
	return nil
}
