// Package reliability 组件装配（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/factory.py
package reliability

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/detectors"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/remediation"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReliabilityComponents 成员可靠性轨的有状态核心。
//
// Python: factory.py ReliabilityComponents(dataclass)。这些对象累积跨周期状态
// （detector 滑动窗口、重启强度限流器、leader 的本地 sink），必须在 native 重建间
// 存活——构建一次并传入每个运行周期新构造的 ReliabilityRail，rail 实例本身保持
// 无状态、由 provider 构建、绝不缓存。
type ReliabilityComponents struct {
	// Monitor detector 聚合器
	Monitor *ReliabilityMonitor
	// MemberName 成员名
	MemberName string
	// AutoRemediator 本地自动纠偏器（无本地纠偏为 nil）
	AutoRemediator *remediation.LocalAutoRemediator
	// LocalReporter leader 进程内 sink（仅 leader 非 nil）
	LocalReporter *LocalAnomalyReporter
}

// BuildComponentsDeps build_reliability_components 的依赖注入面。
//
// Python factory 以位置/关键字参数收 deps；Go 聚合为结构体避免 8 参构造。
// messager nil 时（单进程 fallback / 17.4a standalone）leader 走 LocalReporter。
type BuildComponentsDeps struct {
	// Config 可靠性配置
	Config *core.ReliabilityConfig
	// MemberName 成员名
	MemberName string
	// Messager 跨进程传输（可 nil）
	Messager messager.Messager
	// TeamName 团队名
	TeamName string
	// SenderId 本成员 messager 节点 id
	SenderId string
	// IsLeader 是否 leader
	IsLeader bool
	// Now 时间戳缝（nil 用 time.Now 秒值）
	Now func() float64
	// Text i18n 渲染缝（nil 用 detectors 默认兜底）
	Text detectors.TextRenderer
	// SteerText 纠偏文案渲染缝（nil 用 remediation 默认兜底）
	SteerText func(kind core.AnomalyKind, summary string) string
	// TopicBuilder TEAM topic 构造缝（nil 用 schema/events 默认；ctx 取 session id）
	TopicBuilder func(ctx context.Context, teamName string) string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildMemberDetectors 构建成员内 detectors（团队级 pingpong 不在内）。
//
// Python: factory.py build_member_detectors。按 enabled 逐个构造，顺序对齐
// 上游：tool_error → repeat_tool → model_error → output_length → compaction。
func BuildMemberDetectors(config core.DetectorsConfig, now func() float64, text detectors.TextRenderer) []detectors.Detector {
	if now == nil {
		now = defaultNow
	}
	var dets []detectors.Detector
	if config.ToolError.Enabled {
		dets = append(dets, detectors.NewToolErrorRateDetector(
			config.ToolError.WindowSeconds,
			config.ToolError.RateThreshold,
			config.ToolError.ConsecutiveThreshold, now))
	}
	if config.RepeatTool.Enabled {
		dets = append(dets, detectors.NewRepeatToolCallDetector(
			config.RepeatTool.HistorySize,
			config.RepeatTool.RepeatWarn,
			config.RepeatTool.PingpongWarn,
			config.RepeatTool.LoopBlock,
			config.RepeatTool.GlobalStop))
	}
	if config.ModelError.Enabled {
		md := detectors.NewModelStreamErrorDetector(
			config.ModelError.WindowSeconds,
			config.ModelError.RateThreshold,
			config.ModelError.ConsecutiveThreshold, now)
		if text != nil {
			md.SetTextRenderer(text)
		}
		dets = append(dets, md)
	}
	if config.OutputLength.Enabled {
		dets = append(dets, detectors.NewOutputLengthDetector(
			config.OutputLength.TextThreshold,
			config.OutputLength.ThinkingThreshold))
	}
	if config.Compaction.Enabled {
		dets = append(dets, detectors.NewFrequentCompactionDetector(
			config.Compaction.WindowSeconds,
			config.Compaction.FrequencyThreshold,
			config.Compaction.DropRatio, now))
	}
	return dets
}

// BuildRemediationPolicy 从配置构建策略。
//
// Python: factory.py build_remediation_policy。
func BuildRemediationPolicy(config *core.ReliabilityConfig) *remediation.RemediationPolicy {
	if config == nil {
		return remediation.NewRemediationPolicy(nil)
	}
	return remediation.BuildRemediationPolicy(config.Policy.SeverityActions)
}

// BuildReliabilityComponents 装配有状态可靠性核心（detectors + reporter + remediator）。
//
// Python: factory.py build_reliability_components。leader 得到 LocalAnomalyReporter
// ——自身异常进程内路由、绕过 messager self-filter；其余角色得到
// EventAnomalyReporter 跨进程发布给 leader。
func BuildReliabilityComponents(deps BuildComponentsDeps) *ReliabilityComponents {
	// i18n 未注入时默认接 schema.T（reliability.* 键组）——必须在
	// BuildMemberDetectors 之前接线：model_error detector 的 notice/unavailable
	// 渲染依赖 renderer，nil 会 panic 被 monitor.observeSafe 吞掉。
	if deps.Text == nil {
		deps.Text = schemaTextRenderer
	}
	if deps.SteerText == nil {
		deps.SteerText = schemaSteerText
	}
	policy := BuildRemediationPolicy(deps.Config)
	now := deps.Now
	if now == nil {
		now = defaultNow
	}
	dets := BuildMemberDetectors(deps.Config.Detectors, now, deps.Text)
	var localReporter *LocalAnomalyReporter
	var reporter AnomalyReporter
	if deps.IsLeader {
		localReporter = NewLocalAnomalyReporter()
		reporter = localReporter
	} else if deps.Messager != nil {
		r := NewEventAnomalyReporter(deps.Messager, deps.TeamName, deps.SenderId)
		if deps.TopicBuilder != nil {
			r.SetTopicBuilder(deps.TopicBuilder)
		}
		reporter = r
	}
	// Python: reporter 为 None 时 monitor.feed 上报路径 reporter.report 会
	// AttributeError——Go 侧以 no-op reporter 显式兜底（事件线路缺失=无上报），
	// 行为等价且更安全（偏差注释，无独立 Python 符号）。
	if reporter == nil {
		reporter = nopReporter{}
	}
	monitor := NewReliabilityMonitor(dets, reporter, policy)
	auto := remediation.NewLocalAutoRemediator(policy,
		deps.Config.RestartIntensity.Intensity,
		deps.Config.RestartIntensity.PeriodSeconds, now)
	if deps.SteerText != nil {
		auto.SetSteerText(deps.SteerText)
	}
	return &ReliabilityComponents{
		Monitor:        monitor,
		MemberName:     deps.MemberName,
		AutoRemediator: auto,
		LocalReporter:  localReporter,
	}
}

// ReliabilityRailFromComponents 用复用的（有状态）组件包装一条新 rail。
//
// Python: factory.py reliability_rail_from_components。
func ReliabilityRailFromComponents(components *ReliabilityComponents) *ReliabilityRail {
	return &ReliabilityRail{
		monitor:       components.Monitor,
		member:        components.MemberName,
		auto:          components.AutoRemediator,
		localReporter: components.LocalReporter,
	}
}

// BuildReliabilityRail 一步构建组件并包装成 rail（无跨周期复用）。
//
// Python: factory.py build_reliability_rail。单生命周期内的便捷入口
// （测试、跨进程 fallback）；warm 进程内重建改为组件构建一次并经
// ReliabilityRailFromComponents 复用。
func BuildReliabilityRail(deps BuildComponentsDeps) *ReliabilityRail {
	components := BuildReliabilityComponents(deps)
	return ReliabilityRailFromComponents(components)
}

// BuildPingPongDetector 为 leader handler 构建团队级 pingpong detector。
//
// Python: factory.py build_pingpong_detector。
func BuildPingPongDetector(config *core.ReliabilityConfig) *detectors.PingPongDetector {
	return detectors.NewPingPongDetector(config.Detectors.PingPong.MinVolleys)
}

// ──────────────────────────── 结构体（包级） ────────────────────────────

// nopReporter 无上报缝（messager 缺失时兜底）。
type nopReporter struct{}

// Report no-op。
func (nopReporter) Report(context.Context, *core.Anomaly) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultNow 默认时钟（Unix 秒浮点；对齐 Python time.time 默认参数）。
func defaultNow() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// schemaTextRenderer i18n 默认渲染缝（dotted key + kwargs → schema.T）。
func schemaTextRenderer(key string, kwargs map[string]any) string {
	s, err := schema.T(key, kwargs)
	if err != nil {
		return key
	}
	return s
}

// schemaSteerText 纠偏文案默认渲染（reliability.steer_self_correct）。
func schemaSteerText(kind core.AnomalyKind, summary string) string {
	s, err := schema.T("reliability.steer_self_correct", map[string]any{
		"kind": string(kind), "summary": summary,
	})
	if err != nil {
		return s
	}
	return s
}
