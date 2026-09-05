package security

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SafetyPromptRail 安全提示词 Rail。
//
// 在每次模型调用前注入安全原则到 system prompt，引导模型自律。
// priority=85，事件集={BeforeModelCall}。
//
// 对齐 Python: SafetyPromptRail(BaseSecurityRail) — prompt_security_rail.py L16-46
type SafetyPromptRail struct {
	BaseSecurityRail
	// systemPromptBuilder 系统提示词构建器（init 时获取）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 枚举 ────────────────────────────

// SecurityRail 类型别名，对齐 Python: SecurityRail = SafetyPromptRail (prompt_security_rail.py L49)
type SecurityRail = SafetyPromptRail

// ──────────────────────────── 常量 ────────────────────────────

const (
	// safetyPromptRailPriority 安全提示词 Rail 优先级
	// 对齐 Python: SafetyPromptRail.priority = 85
	safetyPromptRailPriority = 85
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSafetyPromptRail 创建安全提示词 Rail。
//
// 对齐 Python: SafetyPromptRail.__init__()
func NewSafetyPromptRail() *SafetyPromptRail {
	r := &SafetyPromptRail{
		BaseSecurityRail: *NewBaseSecurityRail(
			WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		),
	}
	r.WithPriority(safetyPromptRailPriority)
	return r
}

// Init 初始化钩子，获取 systemPromptBuilder 引用。
//
// 对齐 Python: SafetyPromptRail.init(agent) (prompt_security_rail.py L30-31)
func (r *SafetyPromptRail) Init(agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 反初始化钩子，移除 safety section。
//
// 对齐 Python: SafetyPromptRail.uninit(agent) (prompt_security_rail.py L33-36)
func (r *SafetyPromptRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionSafety)
		r.systemPromptBuilder = nil
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runSecurityCheck 注入安全提示词 section，始终返回 Allow。
//
// 对齐 Python: SafetyPromptRail.run_security_check(security_ctx) (prompt_security_rail.py L38-46)
func (r *SafetyPromptRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	if r.systemPromptBuilder == nil {
		// 对齐 Python: if self.system_prompt_builder is None: return self.allow()
		return r.Allow(nil), nil
	}

	// 对齐 Python: safety_section = build_safety_section(self.system_prompt_builder.language)
	// Go 侧 BuildSafetySection() 始终返回非 nil（包含 cn/en 双语），无需 nil 检查
	section := sections.BuildSafetySection()
	r.systemPromptBuilder.AddSection(section)

	logger.Debug(securityLogComponent).
		Str("section", sections.SectionSafety).
		Msg("已注入安全提示词 section")

	return r.Allow(nil), nil
}
