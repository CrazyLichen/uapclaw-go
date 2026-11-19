package externalcli

import (
	"fmt"
	external "github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// 本文件一比一移植 spawn 模型挑选解析器。
// Python 源：agent-core openjiuwen/agent_teams/workflow/backends/external_model.py（183 行）。
// 纯解析器：只读挑选提示与团队后端模型池，绝不 spawn/分配；池命中产出端点配置，
// builtin 命中产出无端点配置（仅携带 effort）；全空提示把模型留给 CLI 自己的默认。

// ──────────────────────────── 结构体 ────────────────────────────

// PlanError 挑选规则违规（对齐 Python BackendError(retryable=False)）。
// Python: workflow/engine/errors.py BackendError——该错误类型未移植，
// Go 以本类型承载同语义（不可重试、双受众文案原文），登记于 spec 偏差。
type PlanError struct {
	// Message 双受众文案（leader 可读 + 日志可排查）
	Message string
}

func (e *PlanError) Error() string { return e.Message }

// ExternalModelPlan 一次第三方调用的模型解析结果。
// Python: ExternalModelPlan (external_model.py:23-30)
type ExternalModelPlan struct {
	// Primary 池命中（端点配置）或 builtin 命中（无端点仅 effort）；
	// 双 nil 表示该调用把模型留给 CLI 自己登录态的默认模型
	Primary *schema.ExternalCliModelConfig
	// Display 展示名
	Display string
}

// PlanArgs plan_external_model 的关键字参数包（Go 无关键字参数）。
type PlanArgs struct {
	// CLIAgent CLI 种类；空串 ≈ Python None（原生 agent）
	CLIAgent string
	// ModelHint spawn 参数 model（团队模型池名）；"" ≈ None
	ModelHint string
	// BuiltinHint spawn 参数 builtin_model（CLI 自带登录模型）；"" ≈ None
	BuiltinHint string
	// EffortHint spawn 参数 effort；"" ≈ None
	EffortHint string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PlanExternalModel 校验并解析一次第三方调用的模型选项。
// Python: plan_external_model (external_model.py:35-95)
//
// 规则依序：model 与 builtin_model 互斥；model 按该 CLI 的 provider 过滤在团队
// 模型池解析；builtin_model 对拍该 kind 的目录且 effort 限声明集；单独 effort
// 拒绝；原生 agent 拒绝 builtin/effort（其 model 由后端解析，不经此处）；
// 全空提示返回双 nil。
func PlanExternalModel(
	args PlanArgs,
	entry *schema.ExternalCliAgentSpec,
	teamBackend poolSource,
) (*ExternalModelPlan, error) {
	// 互斥（:46-50）
	if args.ModelHint != "" && args.BuiltinHint != "" {
		return nil, &PlanError{Message: "'builtin_model' and 'model' are mutually exclusive: a built-in model " +
			"runs on the CLI's own login, 'model' on a team model pool endpoint"}
	}
	// effort 单独出现拒绝（:52-58）
	if args.EffortHint != "" && args.BuiltinHint == "" {
		return nil, &PlanError{Message: "'effort' requires 'builtin_model': the reasoning effort option only " +
			"applies to a model the CLI offers on its own login"}
	}
	// 原生 agent（:60-69）
	if args.CLIAgent == "" {
		if args.BuiltinHint != "" || args.EffortHint != "" {
			return nil, &PlanError{Message: "builtin_model / effort are unavailable for native agents: only " +
				"external CLI agents offer models on their own login"}
		}
		// 原生 agent 的 model_hint 由后端解析，不经此处
		return &ExternalModelPlan{Primary: nil, Display: ""}, nil
	}
	if args.ModelHint != "" {
		return resolvePoolModel(args.CLIAgent, args.ModelHint, entry, teamBackend)
	}
	if args.BuiltinHint != "" {
		return resolveBuiltinModel(args.CLIAgent, args.BuiltinHint, args.EffortHint, entry)
	}
	return &ExternalModelPlan{Primary: nil, Display: ""}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// poolSource 模型池读取面（*tools.TeamBackend 满足；窄接口避免包依赖）。
// Python: team_backend.get_model_pool()（鸭子式，:96）
type poolSource interface {
	GetModelPool() []models.ModelPoolEntry
}

// resolvePoolModel 按该 CLI 的协议过滤器在团队模型池解析 model_hint。
// Python: _resolve_pool_model (external_model.py:97-131)
func resolvePoolModel(
	cliAgent, modelHint string,
	entry *schema.ExternalCliAgentSpec,
	teamBackend poolSource,
) (*ExternalModelPlan, error) {
	pool := []models.ModelPoolEntry{}
	if teamBackend != nil {
		pool = teamBackend.GetModelPool()
	}
	compatible := external.CompatiblePoolModelNames(cliAgent, pool)
	group := []models.ModelPoolEntry{}
	for _, e := range pool {
		if e.ModelName == modelHint {
			group = append(group, e)
		}
	}
	if len(group) == 0 {
		return nil, &PlanError{Message: poolMissMessage(cliAgent, modelHint, compatible, entry,
			"is not offered by the team model pool")}
	}
	providerFilter := external.ProviderFilterForCLI(cliAgent)
	hit := group[0] // 与 resolve_member_model 相同的"按名分组取首项"规则（:108）
	if providerFilter != nil && !providerFilter(hit.APIProvider) {
		return nil, &PlanError{Message: poolMissMessage(cliAgent, modelHint, compatible, entry,
			"is unavailable or incompatible")}
	}
	apiBase := strings.TrimSpace(hit.APIBaseURL)
	apiKey := strings.TrimSpace(hit.APIKey)
	// 与 team 路径对齐（_external_cli_provider_name）：每个配置端点都暴露在
	// 稳定的 "jiuwen" provider 身份下，codex 风格 runtime 才不会把网关误当
	// 官方端点（:113-116）
	provider := strings.TrimSpace(hit.APIProvider)
	if apiBase != "" {
		provider = external.JiuwenExternalProvider
	}
	config := &schema.ExternalCliModelConfig{
		Provider: external.EmptyToNil(provider),
		Model:    external.EmptyToNil(modelHint),
		APIBase:  external.EmptyToNil(apiBase),
		APIKey:   external.EmptyToNil(apiKey),
	}
	return &ExternalModelPlan{Primary: config, Display: modelHint}, nil
}

// poolMissMessage 解释被拒的 model_hint 并指向内建目录。
// Python: _pool_miss_message (external_model.py:133-176)
// 三档措辞族：目录内名字重定向到 builtin_model；未知名字列池再列目录；
// 无目录条目只列池。类别与根因在前、列表在后（截断后仍保留违规名）。
func poolMissMessage(
	cliAgent, modelHint string,
	compatible []string,
	entry *schema.ExternalCliAgentSpec,
	cause string,
) string {
	var catalog []schema.ExternalCliBuiltinModel
	if entry != nil {
		catalog = entry.BuiltinModels
	}
	builtinNames := make([]string, 0, len(catalog))
	for _, item := range catalog {
		builtinNames = append(builtinNames, item.Name)
	}
	var message string
	if containsStr(builtinNames, modelHint) {
		message = fmt.Sprintf(
			"model '%s' is a built-in model of cli_agent '%s', not a team model pool endpoint; pass it as "+
				"'builtin_model' instead (optionally with 'effort')", modelHint, cliAgent)
	} else {
		message = fmt.Sprintf("model '%s' %s for cli_agent '%s'", modelHint, cause, cliAgent)
	}
	if len(compatible) > 0 {
		message += "; the team model pool offers: " + joinComma(compatible)
	} else {
		message += "; the team model pool has no compatible model"
	}
	if len(builtinNames) > 0 && !containsStr(builtinNames, modelHint) {
		message += ". Built-in models of this cli_agent, via 'builtin_model': " + joinComma(builtinNames)
	}
	return message
}

// resolveBuiltinModel 对拍该 kind 的目录并校验 effort。
// Python: _resolve_builtin_model (external_model.py:178-183+)
func resolveBuiltinModel(
	cliAgent, builtinHint, effortHint string,
	entry *schema.ExternalCliAgentSpec,
) (*ExternalModelPlan, error) {
	var catalog []schema.ExternalCliBuiltinModel
	if entry != nil {
		catalog = entry.BuiltinModels
	}
	builtinNames := make([]string, 0, len(catalog))
	for _, item := range catalog {
		builtinNames = append(builtinNames, item.Name)
	}
	var model *schema.ExternalCliBuiltinModel
	if entry != nil {
		model = entry.FindBuiltinModel(builtinHint)
	}
	if model == nil {
		if len(builtinNames) > 0 {
			return nil, &PlanError{Message: fmt.Sprintf(
				"builtin_model '%s' is not declared by cli_agent '%s'; this cli_agent offers: %s",
				builtinHint, cliAgent, joinComma(builtinNames))}
		}
		return nil, &PlanError{Message: fmt.Sprintf(
			"builtin_model '%s' is not available for cli_agent '%s': this cli_agent declares no builtin_models",
			builtinHint, cliAgent)}
	}
	if effortHint != "" && !containsStr(model.Efforts, effortHint) {
		supported := "none"
		if len(model.Efforts) > 0 {
			supported = joinComma(model.Efforts)
		}
		return nil, &PlanError{Message: fmt.Sprintf(
			"effort '%s' is not supported by builtin_model '%s' of cli_agent '%s'; supported efforts: %s",
			effortHint, builtinHint, cliAgent, supported)}
	}
	// effort 提示缺省取目录默认（Python: effort_hint or model.default_effort）
	effort := effortHint
	if effort == "" {
		effort = model.DefaultEffort
	}
	config := &schema.ExternalCliModelConfig{Model: builtinHint, Effort: effort}
	return &ExternalModelPlan{Primary: config, Display: builtinHint}, nil
}
