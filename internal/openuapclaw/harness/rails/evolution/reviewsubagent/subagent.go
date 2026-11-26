package reviewsubagent

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// review_subagent.go 稳定的 Skill 演进审查子 agent 配置装配。
// Python: openjiuwen/harness/rails/evolution/review/subagent.py（:87-220 装配半区；
// 常量段 subagent_prompt.go 已随 15.24b 提前）。

// ──────────────────────────── 常量 ────────────────────────────

// EvolutionReviewAgentName 审查子 agent 稳定名称。
// Python: EVOLUTION_REVIEW_AGENT_NAME（subagent.py:20）。
const EvolutionReviewAgentName = "evolution_reviewer"

// reviewAgentMaxIterationsDefault 审查子 agent 默认最大迭代。
// Python: build_evolution_review_agent_config 的 max_iterations 默认 25（:129）。
const reviewAgentMaxIterationsDefault = 25

// ──────────────────────────── 结构体 ────────────────────────────

// ReviewAgentBinding 审查 agent 配置的私有绑定元数据。
// Python: _ReviewAgentBinding（subagent.py:87-92，frozen dataclass）。
type ReviewAgentBinding struct {
	// Runtime 审查运行时
	Runtime *review.EvolutionReviewRuntime
	// QueryService 只读查询服务
	QueryService *experience.ExperienceQueryService
	// Store 演进存储
	Store *checkpointing.EvolutionStore
}

// ──────────────────────────── 全局变量 ────────────────────────────

// reviewAgentBindingMu 保护 reviewAgentBindings 的并发读写。
// 偏差（语言层）：Python 经 setattr(config, "_evolution_review_binding", ...) 动态属性
// 挂载（subagent.py:96-112）；Go 结构体无动态属性，改用包级注册表以配置指针为键。
var (
	reviewAgentBindingMu sync.Mutex
	reviewAgentBindings  = map[*harnessschema.SubAgentConfig]*ReviewAgentBinding{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetReviewAgentBinding 向审查 agent 配置附加绑定元数据。
// Python: _set_review_agent_binding（subagent.py:96-112）。
func SetReviewAgentBinding(config *harnessschema.SubAgentConfig, runtime *review.EvolutionReviewRuntime, queryService *experience.ExperienceQueryService, store *checkpointing.EvolutionStore) {
	reviewAgentBindingMu.Lock()
	defer reviewAgentBindingMu.Unlock()
	reviewAgentBindings[config] = &ReviewAgentBinding{
		Runtime:      runtime,
		QueryService: queryService,
		Store:        store,
	}
}

// GetReviewAgentBinding 读取审查 agent 配置的绑定元数据（无则 nil）。
// Python: _get_review_agent_binding（subagent.py:115-117）。
func GetReviewAgentBinding(config *harnessschema.SubAgentConfig) *ReviewAgentBinding {
	reviewAgentBindingMu.Lock()
	defer reviewAgentBindingMu.Unlock()
	return reviewAgentBindings[config]
}

// BuildEvolutionReviewAgentConfig 构建稳定的受限审查子 agent 配置。
// Python: build_evolution_review_agent_config（subagent.py:125-171）。
func BuildEvolutionReviewAgentConfig(
	runtime *review.EvolutionReviewRuntime,
	model *llm.Model,
	queryService *experience.ExperienceQueryService,
	store *checkpointing.EvolutionStore,
	language string,
	maxIterations int,
	agentID string,
) (*harnessschema.SubAgentConfig, error) {
	if maxIterations <= 0 {
		maxIterations = reviewAgentMaxIterationsDefault
	}
	// Python: subagent_model = model if isinstance(model, Model) else None
	subagentModel := model
	reviewAgentID := ReviewToolAgentID(agentID, runtime)
	reviewTools, err := tools.CreateEvolutionReviewTools(tools.EvolutionReviewToolsDeps{
		Runtime:      runtime,
		QueryService: queryService,
		Store:        store,
		Language:     language,
		AgentID:      reviewAgentID,
	})
	if err != nil {
		return nil, err
	}
	description := "Skill 演进审查 agent，仅使用只读工具。"
	if language == "en" {
		description = "Skill evolution review agent with read-only tools."
	}
	// Python: mcps=[] / rails=[] / skills=None / enable_task_loop=False /
	// restrict_to_work_dir=True / enable_read_image_multimodal=False
	config := &harnessschema.SubAgentConfig{
		AgentCard: schema.NewAgentCard(
			schema.WithAgentName(EvolutionReviewAgentName),
			schema.WithAgentDescription(description),
		),
		SystemPrompt:              review.BuildEvolutionReviewAgentPrompt(language),
		ToolInstances:             reviewTools,
		Mcps:                      nil,
		Model:                     subagentModel,
		Rails:                     nil,
		Skills:                    nil,
		Language:                  language,
		EnableTaskLoop:            false,
		MaxIterations:             maxIterations,
		RestrictToWorkDir:         true,
		EnableReadImageMultimodal: false,
	}
	SetReviewAgentBinding(config, runtime, queryService, store)
	return config, nil
}

// EnsureEvolutionReviewAgentConfig 返回恰好含一个稳定审查子 agent 的 subagents 列表。
// 存在且绑定相等 → 原样返回；缺失或绑定不等 → replace。
// Python: ensure_evolution_review_agent_config（subagent.py:174-187）。
func EnsureEvolutionReviewAgentConfig(subagents []*harnessschema.SubAgentConfig, config *harnessschema.SubAgentConfig) []*harnessschema.SubAgentConfig {
	existing := findReviewAgentConfig(subagents)
	if existing == nil {
		return append(subagents, config)
	}
	existingBinding := GetReviewAgentBinding(existing)
	incomingBinding := GetReviewAgentBinding(config)
	if existingBinding == nil || !reviewBindingEqual(existingBinding, incomingBinding) {
		return ReplaceEvolutionReviewAgentConfig(subagents, config)
	}
	return subagents
}

// RemoveEvolutionReviewAgentConfig 返回移除稳定审查子 agent 后的列表。
// Python: remove_evolution_review_agent_config（subagent.py:190-192）。
func RemoveEvolutionReviewAgentConfig(subagents []*harnessschema.SubAgentConfig) []*harnessschema.SubAgentConfig {
	out := make([]*harnessschema.SubAgentConfig, 0, len(subagents))
	for _, item := range subagents {
		if agentNameOf(item) != EvolutionReviewAgentName {
			out = append(out, item)
		}
	}
	return out
}

// ReplaceEvolutionReviewAgentConfig 用给定配置替换既有稳定审查子 agent 条目。
// Python: replace_evolution_review_agent_config（subagent.py:195-197）。
func ReplaceEvolutionReviewAgentConfig(subagents []*harnessschema.SubAgentConfig, config *harnessschema.SubAgentConfig) []*harnessschema.SubAgentConfig {
	return append(RemoveEvolutionReviewAgentConfig(subagents), config)
}

// ReviewToolAgentID 返回稳定且 resource-manager 安全的工具 id 后缀。
// Python: _review_tool_agent_id（subagent.py:204-209）：
// 种子 = "{parent}_evolution_review_{id(runtime):x}"，非法字符归一为 _。
// 偏差（语言层）：Python 用 id(runtime) 地址；Go 用 runtime 指针地址派生。
func ReviewToolAgentID(agentID string, runtime *review.EvolutionReviewRuntime) string {
	parentID := strings.TrimSpace(agentID)
	if parentID == "" {
		parentID = "agent"
	}
	seed := fmt.Sprintf("%s_evolution_review_%s", parentID, runtimeAddressHex(runtime))
	scopedID := strings.Trim(reviewAgentIDSanitize.ReplaceAllString(seed, "_"), "_")
	if scopedID == "" {
		return fmt.Sprintf("evolution_review_%s", runtimeAddressHex(runtime))
	}
	return scopedID
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reviewAgentIDSanitize 工具 id 清洗正则。
// Python: re.sub(r"[^0-9A-Za-z_]+", "_", seed)（subagent.py:208）。
var reviewAgentIDSanitize = regexp.MustCompile(`[^0-9A-Za-z_]+`)

// runtimeAddressHex 返回 runtime 指针地址的十六进制串（无 0x 前缀）。
// Python: f"{id(runtime):x}"。
func runtimeAddressHex(runtime *review.EvolutionReviewRuntime) string {
	return strings.TrimPrefix(fmt.Sprintf("%p", runtime), "0x")
}

// agentNameOf 读取配置的 agent_card.name。
// Python: _agent_name（subagent.py:200-201）。
func agentNameOf(config *harnessschema.SubAgentConfig) string {
	if config == nil || config.AgentCard == nil {
		return ""
	}
	return config.AgentCard.Name
}

// findReviewAgentConfig 按稳定名称查找既有审查子 agent 条目（无则 nil）。
func findReviewAgentConfig(subagents []*harnessschema.SubAgentConfig) *harnessschema.SubAgentConfig {
	for _, item := range subagents {
		if agentNameOf(item) == EvolutionReviewAgentName {
			return item
		}
	}
	return nil
}

// reviewBindingEqual 比较 bindings 字段级相等（Python frozen dataclass __eq__ 语义）。
func reviewBindingEqual(a, b *ReviewAgentBinding) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Runtime == b.Runtime && a.QueryService == b.QueryService && a.Store == b.Store
}
