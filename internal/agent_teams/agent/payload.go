package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnPayloadBuilder 跨进程 spawn 载荷构造器。
// 对齐 Python: SpawnPayloadBuilder (openjiuwen/agent_teams/agent/payload.py)
//
// 集中管理 spawn teammate 时的跨进程 wire 格式。
// 输出键是 TeamAgent.FromSpawnPayload 的公共契约——
// 改这里的字段要同步改子进程入口。
//
// 状态仅在 memberPortMap 和 teammatePortCounter，
// 共同充当增量端口分配器：每个成员名获得稳定的端口分配。
type SpawnPayloadBuilder struct {
	// spec 团队 Agent 规格
	spec atschema.TeamAgentSpec
	// ctx 运行时上下文
	ctx atschema.TeamRuntimeContext
	// memberPortMap 成员名到端口的稳定映射
	memberPortMap map[string]int
	// teammatePortCounter 队友端口计数器
	teammatePortCounter int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnPayloadBuilder 创建新的 SpawnPayloadBuilder 实例。
// 对齐 Python: SpawnPayloadBuilder.__init__(spec, ctx)
func NewSpawnPayloadBuilder(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *SpawnPayloadBuilder {
	return &SpawnPayloadBuilder{
		spec:          spec,
		ctx:           ctx,
		memberPortMap: make(map[string]int),
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// BuildSpawnPayload 构建跨进程 spawn 载荷。
// 对齐 Python: SpawnPayloadBuilder.build_spawn_payload(ctx, initial_message)
//
// 输出 schema 是公共 wire 契约——必须保留每个键。
func (b *SpawnPayloadBuilder) BuildSpawnPayload(ctx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	teamSpec := ctx.TeamSpec
	teamName := ""
	displayName := ""
	leaderMemberName := ""
	if teamSpec != nil {
		teamName = teamSpec.TeamName
		displayName = teamSpec.DisplayName
		leaderMemberName = teamSpec.LeaderMemberName
	}

	// TODO(#9.65): memberTransport = b.BuildMemberMessagerConfig(ctx.MemberName)
	// 当 MessagerTransportConfig 实现后，序列化为 map
	var transport any = nil

	coordination := map[string]any{
		"team_name":          teamName,
		"display_name":       displayName,
		"leader_member_name": leaderMemberName,
		"member_name":        ctx.MemberName,
		"role":               string(ctx.Role),
		"persona":            ctx.Persona,
		"transport":          transport,
	}

	query := initialMessage
	if query == "" {
		query = "Join the team and wait for your first assignment."
	}

	return map[string]any{
		"coordination": coordination,
		"query":        query,
	}
}

// BuildMemberContext 构造成员运行时上下文。
// 对齐 Python: SpawnPayloadBuilder.build_member_context(member_spec)
func (b *SpawnPayloadBuilder) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	return atschema.TeamRuntimeContext{
		Role:           memberSpec.RoleType,
		MemberName:     memberSpec.MemberName,
		Persona:        memberSpec.Persona,
		TeamSpec:       b.ctx.TeamSpec,
		MessagerConfig: func() *messager.MessagerTransportConfig {
			if v := b.BuildMemberMessagerConfig(memberSpec.MemberName); v != nil {
				if cfg, ok := v.(*messager.MessagerTransportConfig); ok {
					return cfg
				}
			}
			return nil
		}(),
		DBConfig:       b.ctx.DBConfig,
	}
}

// BuildMemberMessagerConfig 为指定成员分配稳定的传输配置。
// 对齐 Python: SpawnPayloadBuilder.build_member_messager_config(member_name)
//
// TODO(#9.65): MessagerTransportConfig 深拷贝和端口分配实现后替换
func (b *SpawnPayloadBuilder) BuildMemberMessagerConfig(memberName string) any {
	return nil
}

// BuildSpawnConfig 构建 SpawnAgentConfig。
// 对齐 Python: SpawnPayloadBuilder.build_spawn_config(ctx)
//
// TODO(#9.58): SpawnAgentConfig 类型定义后实现
func (b *SpawnPayloadBuilder) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) any {
	return nil
}
