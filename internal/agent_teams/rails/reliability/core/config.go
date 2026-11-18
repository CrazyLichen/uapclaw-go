// Package core 团队可靠性数据层：配置。
//
// 对应 Python：openjiuwen/agent_teams/reliability/config.py。
// 挂 TeamAgentSpec.reliability，enabled 默认 False（既有团队零影响）；
// 每团队一份配置，成员级阈值覆写为后续扩展。
package core

// ──────────────────────────── 结构体 ────────────────────────────

// ToolErrorConfig 工具调用错误率检测阈值。
//
// Python: config.py ToolErrorConfig。
type ToolErrorConfig struct {
	// Enabled 是否启用（pydantic 默认 True）
	Enabled bool `json:"enabled"`
	// WindowSeconds 错误率滑动窗口宽度（秒）
	WindowSeconds float64 `json:"window_seconds"`
	// RateThreshold 窗口内错误数阈值
	RateThreshold int `json:"rate_threshold"`
	// ConsecutiveThreshold 连续失败数阈值
	ConsecutiveThreshold int `json:"consecutive_threshold"`
}

// RepeatToolConfig 重复/循环工具调用检测阈值。
//
// Python: config.py RepeatToolConfig。
type RepeatToolConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// HistorySize 滑动历史长度（最近 N 次已完成调用）
	HistorySize int `json:"history_size"`
	// RepeatWarn 泛重复告警阈值
	RepeatWarn int `json:"repeat_warn"`
	// PingpongWarn A-B 交替（乒乓）告警阈值
	PingpongWarn int `json:"pingpong_warn"`
	// LoopBlock 无进展循环（HIGH）阈值
	LoopBlock int `json:"loop_block"`
	// GlobalStop 全局熔断（CRITICAL）阈值
	GlobalStop int `json:"global_stop"`
}

// ModelErrorConfig 模型调用错误检测阈值。
//
// Python: config.py ModelErrorConfig。
type ModelErrorConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// WindowSeconds 错误率滑动窗口宽度（秒）
	WindowSeconds float64 `json:"window_seconds"`
	// RateThreshold 窗口内错误数阈值
	RateThreshold int `json:"rate_threshold"`
	// ConsecutiveThreshold 连续失败数阈值
	ConsecutiveThreshold int `json:"consecutive_threshold"`
}

// OutputLengthConfig 超长输出/思考检测阈值。
//
// Python: config.py OutputLengthConfig。
type OutputLengthConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// TextThreshold 输出文本长度阈值
	TextThreshold int `json:"text_threshold"`
	// ThinkingThreshold 思考内容长度阈值
	ThinkingThreshold int `json:"thinking_threshold"`
}

// CompactionConfig 频繁压缩推断阈值。
//
// Python: config.py CompactionConfig。
type CompactionConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// WindowSeconds 压缩频次滑动窗口宽度（秒）
	WindowSeconds float64 `json:"window_seconds"`
	// FrequencyThreshold 窗口内压缩次数阈值
	FrequencyThreshold int `json:"frequency_threshold"`
	// DropRatio 判定一次压缩所需的消息数降幅（0-1）
	DropRatio float64 `json:"drop_ratio"`
}

// PingPongConfig 团队级 ping-pong 检测阈值。
//
// Python: config.py PingPongConfig。
type PingPongConfig struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// MinVolleys 触发告警的连击数（一次互发计一击）
	MinVolleys int `json:"min_volleys"`
}

// DetectorsConfig 全部 detector 的开关与阈值。
//
// Python: config.py DetectorsConfig。
type DetectorsConfig struct {
	// ToolError 工具错误率
	ToolError ToolErrorConfig `json:"tool_error"`
	// RepeatTool 重复调用
	RepeatTool RepeatToolConfig `json:"repeat_tool"`
	// ModelError 模型错误
	ModelError ModelErrorConfig `json:"model_error"`
	// OutputLength 超长输出
	OutputLength OutputLengthConfig `json:"output_length"`
	// Compaction 频繁压缩
	Compaction CompactionConfig `json:"compaction"`
	// PingPong 团队级乒乓
	PingPong PingPongConfig `json:"pingpong"`
}

// RestartIntensityConfig 本地自动纠偏预算（周期内次数上限）。
//
// Python: config.py RestartIntensityConfig。
type RestartIntensityConfig struct {
	// Intensity 周期内允许的纠偏动作数
	Intensity int `json:"intensity"`
	// PeriodSeconds 预算周期（秒）
	PeriodSeconds float64 `json:"period_seconds"`
}

// RemediationPolicyConfig 严重度→动作映射配置。
//
// Python: config.py RemediationPolicyConfig（dict[Severity, list[RemediationAction]]）。
// Go 侧键/值用 string（severity/action 小写串）以支持 yaml/json 往返，
// 读取时经 ParseSeverity / ParseRemediationAction 转换（语言层等价，spec D-g3 同族）。
type RemediationPolicyConfig struct {
	// SeverityActions 严重度到有序动作列表的映射
	SeverityActions map[string][]string `json:"severity_actions"`
}

// ReliabilityConfig 每团队可靠性框架配置。
//
// Python: config.py ReliabilityConfig。opt-in：enabled 默认 False；
// monitor_roles 选择挂 ReliabilityRail 的角色（默认 leader + teammate）。
type ReliabilityConfig struct {
	// Enabled 总开关（pydantic 默认 False）
	Enabled bool `json:"enabled"`
	// MonitorRoles 挂可靠性轨的角色列表
	MonitorRoles []string `json:"monitor_roles"`
	// Detectors 各 detector 开关与阈值
	Detectors DetectorsConfig `json:"detectors"`
	// Policy 严重度→动作映射
	Policy RemediationPolicyConfig `json:"policy"`
	// RestartIntensity 本地纠偏预算
	RestartIntensity RestartIntensityConfig `json:"restart_intensity"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultReliabilityConfig 返回填好 pydantic 默认值的配置。
//
// Python: pydantic 字段 default / default_factory（enabled=False、
// monitor_roles=["leader","teammate"]、各 detector 默认启用、阈值逐字段
// 对齐 config.py:26-113）。Go 无 pydantic 默认值机制，以此构造函数等价。
func DefaultReliabilityConfig() *ReliabilityConfig {
	return &ReliabilityConfig{
		Enabled:      false,
		MonitorRoles: []string{"leader", "teammate"},
		Detectors: DetectorsConfig{
			ToolError:    ToolErrorConfig{Enabled: true, WindowSeconds: 60.0, RateThreshold: 5, ConsecutiveThreshold: 3},
			RepeatTool:   RepeatToolConfig{Enabled: true, HistorySize: 30, RepeatWarn: 10, PingpongWarn: 10, LoopBlock: 20, GlobalStop: 30},
			ModelError:   ModelErrorConfig{Enabled: true, WindowSeconds: 120.0, RateThreshold: 3, ConsecutiveThreshold: 2},
			OutputLength: OutputLengthConfig{Enabled: true, TextThreshold: 32000, ThinkingThreshold: 16000},
			Compaction:   CompactionConfig{Enabled: true, WindowSeconds: 300.0, FrequencyThreshold: 3, DropRatio: 0.3},
			PingPong:     PingPongConfig{Enabled: true, MinVolleys: 6},
		},
		Policy:           RemediationPolicyConfig{SeverityActions: DefaultSeverityActionStrings()},
		RestartIntensity: RestartIntensityConfig{Intensity: 5, PeriodSeconds: 60.0},
	}
}

// DefaultSeverityActionStrings 返回默认严重度→动作表的字符串形状。
//
// Python: remediation/policy.py DEFAULT_SEVERITY_ACTIONS（Tiered leader-first：
// LOW 仅观测、MEDIUM 上报、HIGH 先本地纠偏再上报、CRITICAL 纠偏后升级用户）。
func DefaultSeverityActionStrings() map[string][]string {
	return map[string][]string{
		"low":      {"observe_only"},
		"medium":   {"report_leader"},
		"high":     {"local_steer", "report_leader"},
		"critical": {"local_steer", "escalate_user"},
	}
}
