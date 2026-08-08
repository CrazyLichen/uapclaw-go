package skill_call

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillExperienceOptimizerBase 技能经验优化器共享基类。
//
// 两个子类（SkillExperienceOptimizer / TeamSkillExperienceOptimizer）
// 共享 Domain/DefaultTargets/RequiresForwardData/Bind 等行为，
// 通过嵌入 BaseOptimizerMixin 避免重复实现。
//
// 对应 Python: SkillExperienceOptimizer 和 TeamSkillExperienceOptimizer 的共享字段
type SkillExperienceOptimizerBase struct {
	optimizer.BaseOptimizerMixin
	// llm LLM 模型实例
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
	// onlineContexts 在线演进上下文映射（skill_name → EvolutionContext）
	onlineContexts map[string]*experience.EvolutionContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent skill_call 包日志组件常量
const logComponent = logger.ComponentAgentCore

const (
	// SkillContentMaxChars 个体优化器技能内容截断上限
	// 对齐 Python: _SKILL_CONTENT_MAX_CHARS = 6000
	SkillContentMaxChars = 6000
	// SectionPreviewChars 章节预览字符数
	// 对齐 Python: _SECTION_PREVIEW_CHARS = 200
	SectionPreviewChars = 200
	// ContextMaxChars 上下文拼接上限
	// 对齐 Python: _CONTEXT_MAX_CHARS = 500
	ContextMaxChars = 500
	// RetryParseTimeoutSecs 重试解析超时（秒）
	// 对齐 Python: _RETRY_PARSE_TIMEOUT_SECS = 20
	RetryParseTimeoutSecs = 20
	// TeamSkillContentMaxChars 团队优化器技能内容截断上限
	// 对齐 Python: TEAM_SKILL_CONTENT_MAX_CHARS = 6000
	TeamSkillContentMaxChars = 6000
	// PatchRetrySkillContentChars 团队 patch 重试时技能内容截断上限
	// 对齐 Python: PATCH_RETRY_SKILL_CONTENT_CHARS = 3000
	PatchRetrySkillContentChars = 3000
	// PatchRetryTrajectoryChars 团队 patch 重试时轨迹截断上限
	// 对齐 Python: PATCH_RETRY_TRAJECTORY_CHARS = 6000
	PatchRetryTrajectoryChars = 6000
	// TrajectoryIssuesRetryChars 团队 patch 重试时轨迹问题截断上限
	// 对齐 Python: TRAJECTORY_ISSUES_RETRY_CHARS = 2000
	TrajectoryIssuesRetryChars = 2000
	// UserIntentRetryChars 团队 patch 重试时用户意图截断上限
	// 对齐 Python: USER_INTENT_RETRY_CHARS = 500
	UserIntentRetryChars = 500
	// SummaryRetryChars 团队 patch 重试时摘要截断上限
	// 对齐 Python: SUMMARY_RETRY_CHARS = 200
	SummaryRetryChars = 200
	// TeamEvolutionPreviewChars 团队已有演进预览截断上限
	// 对齐 Python: TEAM_EVOLUTION_PREVIEW_CHARS = 200
	TeamEvolutionPreviewChars = 200
	// TeamEvolutionMaxRecords 团队已有演进最大展示条数
	// 对齐 Python: TEAM_EVOLUTION_MAX_RECORDS = 6
	TeamEvolutionMaxRecords = 6
	// TeamRetryParseTimeoutSecs 团队重试解析超时（秒）
	// 对齐 Python: TEAM_RETRY_PARSE_TIMEOUT_SECS = 20
	TeamRetryParseTimeoutSecs = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

// InitialScoreBySignal 个体优化器信号类型→初始评分映射
// 对齐 Python: INITIAL_SCORE_BY_SIGNAL
var InitialScoreBySignal = map[string]float64{
	"execution_failure":   0.65,
	"user_correction":     0.70,
	"script_artifact":     0.60,
	"conversation_review": 0.50,
}

// GenerateRecordsLLMPolicy 默认的个体记录生成 LLM 调用策略
// 对齐 Python: GENERATE_RECORDS_LLM_POLICY
var GenerateRecordsLLMPolicy = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 150,
	TotalBudgetSecs:    300,
	MaxAttempts:        2,
}

// TeamSkillRecordLLMPolicy 默认的团队记录生成 LLM 调用策略
// 对齐 Python: TEAM_SKILL_RECORD_LLM_POLICY
var TeamSkillRecordLLMPolicy = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 120,
	TotalBudgetSecs:    420,
	MaxAttempts:        3,
}

// TeamInitialScoreBySignal 团队优化器信号类型→初始评分映射
// 对齐 Python: TEAM_INITIAL_SCORE_BY_SIGNAL
var TeamInitialScoreBySignal = map[string]float64{
	"trajectory_issue": 0.65,
	"user_intent":      0.70,
	"team_skill_mixed": 0.68,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Domain 返回优化器域 "skill_experience"。
//
// 对齐 Python: SkillExperienceOptimizer.domain = "skill_experience"
func (b *SkillExperienceOptimizerBase) Domain() string {
	return "skill_experience"
}

// DefaultTargets 返回默认优化目标列表 ["experiences"]。
//
// 对齐 Python:
//
//	@staticmethod
//	def default_targets() -> List[str]:
//	    return [EXPERIENCES_TARGET]
func (b *SkillExperienceOptimizerBase) DefaultTargets() []string {
	return []string{schema.ExperiencesTarget}
}

// RequiresForwardData 返回 true，技能经验优化需要前向数据。
func (b *SkillExperienceOptimizerBase) RequiresForwardData() bool {
	return true
}

// Bind 过滤并绑定可优化的 Operator，从 config 提取 online_contexts。
//
// 对齐 Python:
//
//	self._online_contexts = dict(config.get("online_contexts") or {})
//	return super().bind(operators=operators, targets=targets, **config)
func (b *SkillExperienceOptimizerBase) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	if len(targets) == 0 {
		targets = b.DefaultTargets()
	}
	// 对齐 Python: self._online_contexts = dict(config.get("online_contexts") or {})
	if config != nil {
		if oc, ok := config["online_contexts"]; ok && oc != nil {
			b.onlineContexts = make(map[string]*experience.EvolutionContext)
			switch v := oc.(type) {
			case map[string]*experience.EvolutionContext:
				for k, val := range v {
					b.onlineContexts[k] = val
				}
			case map[string]any:
				for k, val := range v {
					if ectx, ok := val.(*experience.EvolutionContext); ok {
						b.onlineContexts[k] = ectx
					}
				}
			default:
				b.onlineContexts = map[string]*experience.EvolutionContext{}
			}
		} else {
			b.onlineContexts = map[string]*experience.EvolutionContext{}
		}
	} else {
		b.onlineContexts = map[string]*experience.EvolutionContext{}
	}
	return b.BaseOptimizerMixin.Bind(operators, targets, config)
}

// UpdateLLM 更新运行时 llm/model（热重载）。
// 对齐 Python: SkillExperienceOptimizer.update_llm(llm, model)
func (b *SkillExperienceOptimizerBase) UpdateLLM(newLLM *llm.Model, newModel string) {
	if newLLM == nil {
		logger.Warn(logger.ComponentAgentCore).Msg("[SkillExperienceOptimizer] UpdateLLM: llm 为 nil，拒绝更新")
		return
	}
	b.llm = newLLM
	b.model = newModel
}

// LLM 返回配置的 LLM 客户端。
func (b *SkillExperienceOptimizerBase) LLM() *llm.Model {
	return b.llm
}

// ModelName 返回配置的模型名称。
func (b *SkillExperienceOptimizerBase) ModelName() string {
	return b.model
}

// Language 返回配置的语言。
func (b *SkillExperienceOptimizerBase) Language() string {
	return b.language
}

// OnlineContexts 返回在线演进上下文映射。
func (b *SkillExperienceOptimizerBase) OnlineContexts() map[string]*experience.EvolutionContext {
	return b.onlineContexts
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// removeSkillPrefix 从 operator ID 中移除 "skill_experience_" 前缀。
// 对齐 Python: op_id.removeprefix("skill_experience_")
func removeSkillPrefix(opID string) string {
	return strings.TrimPrefix(opID, "skill_experience_")
}
