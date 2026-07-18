package signal

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionSignal 演化信号，标识 Agent 执行过程中的问题和诊断信息。
//
// 信号由评估结果（离线）或对话监控（在线）产生，
// 驱动优化器决定优化方向和内容。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionSignal
type EvolutionSignal struct {
	// SignalType 信号类型（如 "low_score"、"execution_failure"、"user_correction"）
	SignalType string
	// Section 建议修改的 SKILL.md 区域（如 "Troubleshooting"、"Examples"）
	Section string
	// Excerpt 问题摘要或关键片段
	Excerpt string
	// SkillName 关联的技能名称（可选）
	SkillName *string
	// Context 诊断上下文（如 question/label/answer/reason/score/source/tool_name）
	Context map[string]any
}

// SignalOption 演化信号构造选项函数。
type SignalOption func(*evolutionSignalConfig)

// evolutionSignalConfig MakeEvolutionSignal 的内部配置。
type evolutionSignalConfig struct {
	source    *string
	toolName  *string
	skillName *string
	context   map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// EvolutionCategory 演化类别枚举，保留向后兼容。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionCategory(str, Enum)
type EvolutionCategory string

const (
	// EvolutionCategorySkillExperience 技能经验
	EvolutionCategorySkillExperience EvolutionCategory = "skill_experience"
	// EvolutionCategoryNewSkill 新技能
	EvolutionCategoryNewSkill EvolutionCategory = "new_skill"
)

// EvolutionTarget 演化目标层枚举，标识技能经验作用的目标层。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionTarget(str, Enum)
type EvolutionTarget string

const (
	// EvolutionTargetDescription 描述层
	EvolutionTargetDescription EvolutionTarget = "description"
	// EvolutionTargetBody 主体层
	EvolutionTargetBody EvolutionTarget = "body"
	// EvolutionTargetScript 脚本层
	EvolutionTargetScript EvolutionTarget = "script"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithSource 设置信号来源。
func WithSource(source string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.source = &source }
}

// WithToolName 设置工具名称。
func WithToolName(toolName string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.toolName = &toolName }
}

// WithSkillName 设置技能名称。
func WithSkillName(skillName string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.skillName = &skillName }
}

// WithContext 设置诊断上下文。
func WithContext(context map[string]any) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.context = context }
}

// MakeEvolutionSignal 创建演化信号，合并 source/tool_name 到 context。
//
// 对应 Python: make_evolution_signal(signal_type, section, excerpt, tool_name, skill_name, source, context)
func MakeEvolutionSignal(signalType, section, excerpt string, opts ...SignalOption) *EvolutionSignal {
	cfg := &evolutionSignalConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	mergedContext := map[string]any{}
	for k, v := range cfg.context {
		mergedContext[k] = v
	}
	if cfg.source != nil {
		if _, exists := mergedContext["source"]; !exists {
			mergedContext["source"] = *cfg.source
		}
	}
	if cfg.toolName != nil {
		if _, exists := mergedContext["tool_name"]; !exists {
			mergedContext["tool_name"] = *cfg.toolName
		}
	}

	var skillName *string
	if cfg.skillName != nil {
		skillName = cfg.skillName
	}

	var context map[string]any
	if len(mergedContext) > 0 {
		context = mergedContext
	}

	return &EvolutionSignal{
		SignalType: signalType,
		Section:    section,
		Excerpt:    excerpt,
		SkillName:  skillName,
		Context:    context,
	}
}

// GetSignalSource 从信号 context 中读取 source 元数据，向后兼容。
//
// 对应 Python: get_signal_source(signal)
func GetSignalSource(sig *EvolutionSignal) *string {
	if sig.Context == nil {
		return nil
	}
	source, ok := sig.Context["source"]
	if !ok || source == nil {
		return nil
	}
	s := fmt.Sprintf("%v", source)
	return &s
}

// MakeSignalFingerprint 构建信号去重指纹。
//
// 对应 Python: make_signal_fingerprint(signal)
// 返回 [4]string{signal_type, context.tool_name, skill_name, excerpt[:200]}
func MakeSignalFingerprint(sig *EvolutionSignal) [4]string {
	context := sig.Context
	if context == nil {
		context = map[string]any{}
	}
	toolName := ""
	if v, ok := context["tool_name"]; ok && v != nil {
		toolName = fmt.Sprintf("%v", v)
	}
	skillName := ""
	if sig.SkillName != nil {
		skillName = *sig.SkillName
	}
	excerpt := sig.Excerpt
	if len(excerpt) > 200 {
		excerpt = excerpt[:200]
	}
	return [4]string{sig.SignalType, toolName, skillName, excerpt}
}

// ToDict 将信号转换为字典形式。
//
// 对应 Python: EvolutionSignal.to_dict()
func (s *EvolutionSignal) ToDict() map[string]any {
	d := map[string]any{
		"type":       s.SignalType,
		"section":    s.Section,
		"excerpt":    s.Excerpt,
		"skill_name": s.SkillName,
	}
	if s.Context != nil {
		d["context"] = s.Context
	}
	return d
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateString 截断字符串到指定最大长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// stringPtrValue 安全获取 *string 的值，nil 返回空字符串。
func stringPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
