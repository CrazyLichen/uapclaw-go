package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnPayloadBuilder 跨进程 spawn 载荷构造器。
// Python: SpawnPayloadBuilder (openjiuwen/agent_teams/agent/payload.py)
//
// 集中管理 spawn teammate 时的跨进程 wire 格式。
// 输出键是 TeamAgent.FromSpawnPayload 的公共契约——
// 改这里的字段要同步改子进程入口。
// 状态仅在 memberPortMap 和 teammatePortCounter，
// 共同充当增量端口分配器：每个成员名获得稳定的端口分配。
// 并发纪律：mu 保护上面两个可变字段。
// Python 侧是单线程 asyncio，串行调用天然安全；Go 侧 SpawnManager.OnTeammateUnhealthy
// 为每个不健康成员起一条重启 goroutine，两条同时走到 BuildMemberMessagerConfig 就是
// 并发写同一张 map——那是 runtime 的 fatal error（不可 recover，直接终止进程）。
type SpawnPayloadBuilder struct {
	// spec 团队 Agent 规格
	spec atschema.TeamAgentSpec
	// ctx 运行时上下文
	ctx atschema.TeamRuntimeContext
	// mu 保护 memberPortMap 与 teammatePortCounter
	mu sync.Mutex
	// memberPortMap 成员名到端口的稳定映射
	memberPortMap map[string]int
	// teammatePortCounter 队友端口计数器
	teammatePortCounter int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTeammateBasePort 队友直连端口基准。
	// Python: meta.get("teammate_base_port", 16000)
	defaultTeammateBasePort = 16000
	// defaultTeammatePortOffset 队友端口分配步长。
	// Python: meta.get("teammate_port_offset", 10)
	defaultTeammatePortOffset = 10
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnPayloadBuilder 创建新的 SpawnPayloadBuilder 实例。
// Python: SpawnPayloadBuilder.__init__(spec, ctx)
func NewSpawnPayloadBuilder(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *SpawnPayloadBuilder {
	return &SpawnPayloadBuilder{
		spec:          spec,
		ctx:           ctx,
		memberPortMap: make(map[string]int),
	}
}

// BuildSpawnPayload 构建跨进程 spawn 载荷。
// Python: SpawnPayloadBuilder.build_spawn_payload(ctx, initial_message)
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

	// 成员消息传输配置：
	// Python: member_transport = self.build_member_messager_config(ctx.member_name)
	// 序列化为 map 等价于 Python model_dump(mode="json")——走 JSON 往返，
	// 与本文件其它 dump 路径一致。
	var transport map[string]any
	if mt := b.BuildMemberMessagerConfig(ctx.MemberName); mt != nil {
		if raw, err := json.Marshal(mt); err == nil {
			_ = json.Unmarshal(raw, &transport)
		}
	}

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
// Python: SpawnPayloadBuilder.build_member_context(member_spec)
func (b *SpawnPayloadBuilder) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	return atschema.TeamRuntimeContext{
		Role:           memberSpec.RoleType,
		MemberName:     memberSpec.MemberName,
		Persona:        memberSpec.Persona,
		DisplayName:    memberSpec.DisplayName,
		Desc:           memberSpec.Desc,
		Prompt:         memberSpec.Prompt,
		TeamSpec:       b.ctx.TeamSpec,
		MessagerConfig: b.BuildMemberMessagerConfig(memberSpec.MemberName),
		DBConfig:       b.ctx.DBConfig,
	}
}

// BuildMemberMessagerConfig 为指定成员分配稳定的传输配置。
// Python: SpawnPayloadBuilder.build_member_messager_config(member_name)（payload.py L88-114）
// 语义：
//   - leader 传输配置缺失（ctx.MessagerConfig == nil）时返回 nil：无基准就无从派生
//   - 端口从 spec.metadata["teammate_base_port"]（默认 16000）起，
//     每个新成员按 "teammate_port_offset"（默认 10）递增；同名成员复用同一端口
//   - direct_addr 固定 tcp://127.0.0.1:{port}，pubsub 收发地址沿用 leader
//   - metadata 浅拷贝后剔除 pubsub_bind（成员不占用 leader 的订阅绑定端口）
func (b *SpawnPayloadBuilder) BuildMemberMessagerConfig(memberName string) *atschema.MessagerTransportConfig {
	leaderCfg := b.ctx.MessagerConfig
	if leaderCfg == nil {
		return nil
	}

	basePort := defaultTeammateBasePort
	portOffset := defaultTeammatePortOffset
	if v, ok := messagerIntField(b.spec.Metadata["teammate_base_port"]); ok {
		basePort = v
	}
	if v, ok := messagerIntField(b.spec.Metadata["teammate_port_offset"]); ok {
		portOffset = v
	}

	portBase := b.assignPort(memberName, basePort, portOffset)

	metadata := make(map[string]any, len(leaderCfg.Metadata))
	for k, v := range leaderCfg.Metadata {
		metadata[k] = v
	}
	delete(metadata, "pubsub_bind")

	out := *leaderCfg
	out.NodeID = memberName
	out.DirectAddr = fmt.Sprintf("tcp://127.0.0.1:%d", portBase)
	out.PubsubPublishAddr = leaderCfg.PubsubPublishAddr
	out.PubsubSubscribeAddr = leaderCfg.PubsubSubscribeAddr
	out.Metadata = metadata
	return &out
}

// BuildSpawnConfig 构建 SpawnAgentConfig。
// Python: SpawnPayloadBuilder.build_spawn_config(ctx)
//
// payload 是子进程 TeamAgent.FromSpawnPayload 的公共契约：
// {spec: model_dump, context: model_dump}。
// 注意与 BuildSpawnPayload 的区分——后者产出 {coordination, query}
// 形状，作为 spawn_agent 的 inputs 参数传递（Python spawn_manager L86-91：
// Runner.spawn_agent(build_spawn_config(ctx), build_spawn_payload(...)) 双参数分离）。
// 原实现误用 BuildSpawnPayload 覆盖 payload，导致子进程
// from_spawn_payload 读不到 spec/context。
// M-18: 补 RunnerConfig=serialize_runner_config(Runner.get_config())
// （Python L121）——子进程 child.go 以非 nil 判定应用配置，
// 原实现恒空导致子进程 Runner 永远用默认配置而非父进程快照。
// LoggingConfig 归档 TODO：Python _build_member_logging_config 按 sinks
// 结构改写文件 sink 到 per-member 目录，Go LoggingConfig 仅
// level+output_dir 无 sinks 结构，无法逐 sink 复刻（Runner.SpawnAgent
// 已兜底全局快照，等 Go 日志配置结构对齐 Python 后回填）。
func (b *SpawnPayloadBuilder) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) runnerspawn.SpawnAgentConfig {
	// S-26: spec/context 序列化为 map（Python model_dump(mode="json") 等价）
	specBytes, err := json.Marshal(b.spec)
	if err != nil {
		specBytes = []byte("{}")
	}
	var specMap map[string]any
	if err := json.Unmarshal(specBytes, &specMap); err != nil {
		specMap = make(map[string]any)
	}
	ctxBytes, err := json.Marshal(ctx)
	if err != nil {
		ctxBytes = []byte("{}")
	}
	var ctxMap map[string]any
	if err := json.Unmarshal(ctxBytes, &ctxMap); err != nil {
		ctxMap = make(map[string]any)
	}
	// M-18: Python L121 runner_config=serialize_runner_config(Runner.get_config())
	var runnerConfig map[string]any
	if rc, rcErr := runnerspawn.SerializeRunnerConfig(runner.GetConfig()); rcErr == nil && rc != nil {
		runnerConfig = rc
	}
	return runnerspawn.SpawnAgentConfig{
		AgentKind:    runnerspawn.SpawnAgentKindTeamAgent,
		RunnerConfig: runnerConfig,
		// TODO(LoggingConfig): 归档——Go LoggingConfig 无 sinks 结构，
		// 无法复刻 Python per-member 目录改写；Runner.SpawnAgent 已兜底全局快照
		Payload: map[string]any{
			"spec":    specMap,
			"context": ctxMap,
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assignPort 为成员分配稳定的直连端口（并发安全）。
// 同名成员返回首次分配的端口——重启必须复用同一端口，否则 peers 表里
// 写死的 direct_addr 失效；新成员按首次出现顺序递增一个步长。
// 查表与分配必须在同一把锁内完成，否则两次并发分配会拿到同一个 counter 值。
func (b *SpawnPayloadBuilder) assignPort(memberName string, basePort, portOffset int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if port, ok := b.memberPortMap[memberName]; ok {
		return port
	}
	port := basePort + b.teammatePortCounter*portOffset
	b.teammatePortCounter++
	b.memberPortMap[memberName] = port
	return port
}

// messagerIntField 把 metadata 中的数值字段转成 int。
// YAML/JSON 反序列化后数值可能是 int / int64 / float64 / json.Number / string，
// Python 侧靠动态类型直接参与算术再格式化，Go 需显式归一。
// 无法转换时返回 false，调用方保留默认值。
func messagerIntField(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
		return 0, false
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}
