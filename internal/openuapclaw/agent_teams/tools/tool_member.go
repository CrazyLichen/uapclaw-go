// Python 源：openjiuwen/agent_teams/tools/tool_member.py（scoped 段 1:1）
//
// 本批落（tool_expert 依赖链 + spawn_teammate 新形态）：
//   - 校验/描述助手（:27-160 区段、:456-530）
//   - ExternalCliModels / ExternalCliModelRules（:222-455）
//   - register_teammate / register_external_cli_member（:626-667/:486-540）
//   - SpawnTeammateTool 新形态（:669-914，含 fork/cli_agent/external_cli 面）
//
// scoped 说明（D-15.16-e15）：tool_member.py 其余成员工具（CheckpointTool /
// ListCheckpointsTool / SpawnHumanAgentTool / SpawnPassiveHumanTool /
// SpawnBridgeAgentTool / ShutdownMemberTool / SetMemberModelTool /
// ApprovePlanTool / ApproveToolCallTool / ListMembersTool，:917-1529）在 Go
// 既有 team_tools.go 中已有同族实现（旧 team_tools.py 时代对齐物：role_type
// 分支并入旧 SpawnMemberTool 的 human_agent 路径、shutdown/approve/list 系列
// 已独立落地）——归属漂移按偏差登记，非本批重定位（等价实现已存在且被消费，
// 规则 8 的"删除"对象是本批新增结构替代的部分，见 SpawnMemberTool 删除记录）。

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/locales"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// toolMemberLogComponent 成员工具组件日志。
const toolMemberLogComponent = logger.ComponentAgentServer

// maxServedModels 已在 database/member_options.go（MAX_SERVED_MODELS）。

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalCliModels 一个外部 CLI 成员的模型选择。
// Python: ExternalCliModels (tool_member.py:222-229)
type ExternalCliModels struct {
	// Allocation 池模型分配（可空）
	Allocation *models.Allocation
	// FallbackAllocation 认证兜底分配（可空）
	FallbackAllocation *models.Allocation
	// BuiltinModel CLI 自带登录模型（可空）
	BuiltinModel *database.MemberBuiltinModel
}

// ExternalCliModelRules 外部 CLI 成员的池模型 / 认证兜底 / 内置模型规则。
// Python: ExternalCliModelRules (tool_member.py:231-455)
type ExternalCliModelRules struct {
	team                *TeamBackend
	allocateModelConfig func(modelName string, opts ...models.AllocateOption) *models.Allocation
}

// NewExternalCliModelRules 构造规则求解器。
// Python: __init__ (tool_member.py:234-240)
func NewExternalCliModelRules(team *TeamBackend, allocateModelConfig func(modelName string, opts ...models.AllocateOption) *models.Allocation) *ExternalCliModelRules {
	return &ExternalCliModelRules{team: team, allocateModelConfig: allocateModelConfig}
}

// Resolve 解析成员的模型选择；输入被拒时 (nil, reason)。
// Python: resolve (tool_member.py:242-348)
func (r *ExternalCliModelRules) Resolve(cliAgent, modelName, fallbackModelName, builtinModel, effort, toolName string) (*ExternalCliModels, string) {
	// Python: raw_fallback_model_name 非 str 时拒绝（:262-264）
	if fallbackModelName != "" && !isPlainString(fallbackModelName) {
		return nil, toolName + " requires 'fallback_model_name' to be a model name when given"
	}
	// 缺省、null、空白都表示「无兜底模型」。Python: :266
	fallbackName := strings.TrimSpace(fallbackModelName)
	hasAuthFallback := seamTakesAuthFallback(cliAgent)
	if fallbackName != "" && !hasAuthFallback {
		return nil, fmt.Sprintf("cli_agent '%s' has no authentication fallback; omit 'fallback_model_name'", cliAgent)
	}
	var allocation, fallbackAllocation *models.Allocation
	providerFilter := seamProviderFilterForCLI(cliAgent)
	compatibleEntries := r.CompatiblePoolEntries(cliAgent)
	compatibleModelNames := r.compatibleModelNames(cliAgent)
	preferredCurrentModel := r.preferredCurrentModel(cliAgent, compatibleEntries)
	builtin, builtinError := r.resolveBuiltinModel(builtinModel, effort, cliAgent, modelName)
	if builtinError != "" {
		return nil, builtinError
	}
	if modelName == "*" || fallbackName == "*" {
		return nil, "model_name '*' is a Team logical entry and cannot be passed to an external CLI"
	}
	if modelName != "" {
		if r.team.IsModelGroupPool() {
			candidates := r.team.ResolveCliModels(modelName, providerFilter)
			var logical *models.Allocation
			if r.allocateModelConfig != nil {
				logical = r.allocateModelConfig("")
			}
			if len(candidates) > 0 && logical != nil {
				// Python: Allocation(logical.entry, logical.group_index,
				// persistence_model_name=model_name)（:288-290）
				pinned := *logical
				pinned.PersistenceModelName = modelName
				allocation = &pinned
			}
		} else if r.allocateModelConfig == nil {
			return nil, r.modelNameFailure(cliAgent, modelName, compatibleModelNames, "cannot be resolved without a team model pool")
		} else if providerFilter != nil {
			allocation = r.allocateWithFilter(modelName, providerFilter)
		} else if allocation == nil {
			allocation = r.allocateModelConfig(modelName)
		}
		if allocation == nil {
			return nil, r.modelNameFailure(cliAgent, modelName, compatibleModelNames, "is unavailable or incompatible")
		}
	}
	if fallbackName == "" {
		if len(compatibleModelNames) > 0 && hasAuthFallback {
			return nil, ("fallback_model_name is required while compatible models are available for " +
				fmt.Sprintf("cli_agent '%s': %s", cliAgent, strings.Join(compatibleModelNames, ", ")))
		}
	} else if preferredCurrentModel != "" && fallbackName != preferredCurrentModel {
		return nil, (fmt.Sprintf("fallback_model_name must use current model '%s' because it is present "+
			"in the team model pool and compatible with cli_agent '%s'", preferredCurrentModel, cliAgent))
	} else if r.allocateModelConfig == nil {
		return nil, toolName + " requires a team model pool when 'fallback_model_name' is specified"
	} else {
		if r.team.IsModelGroupPool() {
			candidates := r.team.ResolveCliModels(fallbackName, providerFilter)
			logical := r.allocateModelConfig("")
			if len(candidates) > 0 && logical != nil {
				pinned := *logical
				pinned.PersistenceModelName = fallbackName
				fallbackAllocation = &pinned
			}
		} else if providerFilter != nil {
			fallbackAllocation = r.allocateWithFilter(fallbackName, providerFilter)
		} else {
			fallbackAllocation = r.allocateModelConfig(fallbackName)
		}
		if fallbackAllocation == nil {
			compatibleHint := "none"
			if len(compatibleModelNames) > 0 {
				compatibleHint = strings.Join(compatibleModelNames, ", ")
			}
			return nil, (fmt.Sprintf("fallback_model_name '%s' is unavailable or incompatible with "+
				"cli_agent '%s' (compatible models: %s); omit 'fallback_model_name' "+
				"only when no compatible model is available", fallbackName, cliAgent, compatibleHint))
		}
	}
	return &ExternalCliModels{Allocation: allocation, FallbackAllocation: fallbackAllocation, BuiltinModel: builtin}, ""
}

// CompatiblePoolEntries 一个 CLI kind 的协议兼容池条目。
// Python: compatible_pool_entries (tool_member.py:350-357)
func (r *ExternalCliModelRules) CompatiblePoolEntries(cliAgent string) []models.ModelPoolEntry {
	providerFilter := seamProviderFilterForCLI(cliAgent)
	entries := r.team.GetModelPool()
	if providerFilter == nil {
		return entries
	}
	out := make([]models.ModelPoolEntry, 0, len(entries))
	for _, entry := range entries {
		if providerFilter(entry.APIProvider) {
			out = append(out, entry)
		}
	}
	return out
}

// compatibleModelNames 一个 CLI kind 可见的 safe 模型名。
// IntelliRouter/模型组池只含一个逻辑 "*" 条目，真实部署名在 allocator 目录里
// ——把它投影出来，兜底校验不会把模型组误判为空池。
// Python: _compatible_model_names (tool_member.py:359-376)
func (r *ExternalCliModelRules) compatibleModelNames(cliAgent string) []string {
	providerFilter := seamProviderFilterForCLI(cliAgent)
	if r.team.IsModelGroupPool() {
		seen := map[string]bool{}
		names := []string{}
		for _, item := range r.team.ListCliModels(providerFilter) {
			if item.Model == "" || seen[item.Model] {
				continue
			}
			seen[item.Model] = true
			names = append(names, item.Model)
		}
		sort.Strings(names)
		return names
	}
	seen := map[string]bool{}
	names := []string{}
	for _, entry := range r.CompatiblePoolEntries(cliAgent) {
		if entry.ModelName == "" || seen[entry.ModelName] {
			continue
		}
		seen[entry.ModelName] = true
		names = append(names, entry.ModelName)
	}
	sort.Strings(names)
	return names
}

// preferredCurrentModel 池能兼容分配当前模型时返回当前模型名。
// 模型组下当前模型是逻辑 "*" 条目，不是外部 CLI 能跑的具体模型；兜底必须是
// 真实部署之一。
// Python: _preferred_current_model (tool_member.py:378-395)
func (r *ExternalCliModelRules) preferredCurrentModel(cliAgent string, entries []models.ModelPoolEntry) string {
	if r.team.IsModelGroupPool() {
		return ""
	}
	currentName := r.team.CurrentModelName()
	currentProvider := r.team.CurrentModelProvider()
	if currentName == "" || currentProvider == "" {
		return ""
	}
	currentProtocol := seamModelApiProtocol(currentProvider)
	for _, entry := range entries {
		if entry.ModelName != currentName {
			continue
		}
		if seamModelApiProtocol(entry.APIProvider) == currentProtocol {
			return currentName
		}
	}
	return ""
}

// modelNameFailure 解释被拒的 model_name 并指向内置目录。
// Python: _model_name_failure (tool_member.py:397-425)
func (r *ExternalCliModelRules) modelNameFailure(cliAgent, modelName string, compatible []string, cause string) string {
	config := r.team.ExternalCLIConfig(cliAgent)
	var builtinNames []string
	if config != nil {
		for i := range config.BuiltinModels {
			builtinNames = append(builtinNames, config.BuiltinModels[i].Name)
		}
	}
	for _, name := range builtinNames {
		if name == modelName {
			return fmt.Sprintf("model_name '%s' is a built-in model of cli_agent '%s', not a team model pool "+
				"endpoint; pass it as 'builtin_model' instead (optionally with 'effort')", modelName, cliAgent)
		}
	}
	message := fmt.Sprintf("model_name '%s' %s for cli_agent '%s'", modelName, cause, cliAgent)
	if len(compatible) > 0 {
		message += "; the team model pool offers: " + strings.Join(compatible, ", ")
	} else {
		message += "; the team model pool has no compatible model"
	}
	if len(builtinNames) > 0 {
		message += ". Built-in models of this cli_agent, via 'builtin_model': " + strings.Join(builtinNames, ", ")
	}
	return message
}

// resolveBuiltinModel 校验可选的内置模型选择。MCP 客户端不按 schema 校验，
// 门被绕过的参数在这里拒绝而不是静默忽略。
// Python: _resolve_builtin_model (tool_member.py:427-453)
func (r *ExternalCliModelRules) resolveBuiltinModel(builtinModel, effort, cliAgent, modelName string) (*database.MemberBuiltinModel, string) {
	builtinName := OptionalText(builtinModel)
	effortName := OptionalText(effort)
	if builtinName == "" && effortName == "" {
		return nil, ""
	}
	if !r.team.BuiltinModelsEnabled() {
		return nil, "builtin_model / effort are unavailable: no CLI kind declares builtin_models"
	}
	if builtinName == "" {
		return nil, "'effort' requires 'builtin_model'"
	}
	if modelName != "" {
		return nil, ("'builtin_model' and 'model_name' are mutually exclusive: a built-in model runs on the CLI's " +
			"own login, 'model_name' on a team model pool endpoint")
	}
	return r.team.ResolveBuiltinModel(cliAgent, builtinName, effortName)
}

// allocateWithFilter 带 provider 过滤的分配——e16 修正后 allocator 接口
// 承载 WithProviderFilter option（对齐 Python allocate(provider_filter=)
// 分配前过滤语义），原分配后校验兜底删除。
func (r *ExternalCliModelRules) allocateWithFilter(modelName string, providerFilter func(string) bool) *models.Allocation {
	return r.allocateModelConfig(modelName, models.WithProviderFilter(providerFilter))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemberName 工具边界的 member_name 校验；非法返回错误文案，合法返回空。
// Python: validate_member_name (tool_member.py:27-42)
func ValidateMemberName(memberName string) string {
	if memberName != "" && memberNamePattern.MatchString(memberName) {
		return ""
	}
	return (fmt.Sprintf("Invalid member_name %q: must start with a "+
		"lowercase ASCII letter (a-z), followed by lowercase letters, "+
		"digits (0-9) or hyphen (-); no uppercase, underscore, "+
		"whitespace, or non-ASCII characters (including CJK) — "+
		"member_name is reused as a routing token and a filesystem "+
		"path segment", memberName))
}

// ValidateDisplayName 工具边界的 display_name 校验。
// Schema required 只约束宿主 LLM；MCP 客户端直接 invoke，这里是唯一强制点。
// Python: validate_display_name (tool_member.py:44-62)
func ValidateDisplayName(displayName string) string {
	if displayName != "" && strings.TrimSpace(displayName) != "" {
		return ""
	}
	return fmt.Sprintf("Invalid display_name %q: must be a non-empty, non-blank string", displayName)
}

// OptionalText 返回去空白非空字符串；缺失/空白得空串。
// Python: _optional_text (tool_member.py:117-122)
func OptionalText(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ModelNameDescription 描述注册成员工具的 model_name 参数。
// 模型组内该字段收 route tag 而不是池模型，措辞切换并列出可选 tag。
// Python: model_name_description (tool_member.py:125-136)
func ModelNameDescription(team *TeamBackend, t locales.Translator) string {
	if !team.IsModelGroupPool() {
		return t("spawn_teammate", "model_name")
	}
	tags := team.ListRouteTags()
	selectable := make([]string, 0, len(tags))
	for _, tag := range tags {
		selectable = append(selectable, tag)
	}
	catalog, _ := json.Marshal(map[string]any{"selectable_models": selectable})
	return fmt.Sprintf("%s\n\n<selectable_model_catalog>%s</selectable_model_catalog>",
		t("spawn_teammate", "model_name_group"), catalog)
}

// CheckModelSelectable 拒绝当前模型组无法满足的显式 model_name。
// 模型组内显式模型是硬性要求：让成员跑整组会静默无视用户。
// Python: check_model_selectable (tool_member.py:138-160)
func CheckModelSelectable(team *TeamBackend, modelName any, allocation *models.Allocation) string {
	name := OptionalText(modelName)
	if allocation != nil || name == "" || !team.IsModelGroupPool() {
		return ""
	}
	tags := team.ListRouteTags()
	selectable := strings.Join(tags, ", ")
	if selectable == "" {
		selectable = "<none>"
	}
	return fmt.Sprintf("model_name '%s' is not selectable in the current model group "+
		"(selectable: %s). Do not pick a substitute yourself: ask the user "+
		"which model to use, or omit model_name to let the model group choose.", name, selectable)
}

// BuiltinModelCatalogText 渲染各 CLI kind 声明的内置模型，供模型挑选。
// Python: _builtin_model_catalog (tool_member.py:98-115)
func BuiltinModelCatalogText(team *TeamBackend) string {
	catalog := map[string][]map[string]any{}
	kinds := team.DeclaredCLIKinds()
	keys := make([]string, 0, len(kinds))
	for kind := range kinds {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	for _, cliAgent := range keys {
		config := team.ExternalCLIConfig(cliAgent)
		if config == nil || len(config.BuiltinModels) == 0 {
			continue
		}
		items := make([]map[string]any, 0, len(config.BuiltinModels))
		for i := range config.BuiltinModels {
			item := map[string]any{"name": config.BuiltinModels[i].Name}
			if config.BuiltinModels[i].Description != "" {
				item["description"] = config.BuiltinModels[i].Description
			}
			if len(config.BuiltinModels[i].Efforts) > 0 {
				item["efforts"] = config.BuiltinModels[i].Efforts
			}
			if config.BuiltinModels[i].DefaultEffort != "" {
				item["default_effort"] = config.BuiltinModels[i].DefaultEffort
			}
			items = append(items, item)
		}
		catalog[cliAgent] = items
	}
	data, _ := json.Marshal(catalog)
	return fmt.Sprintf("<builtin_model_catalog>%s</builtin_model_catalog>", data)
}

// ExternalCliModelCatalogContext 渲染对模型安全的当前模型与池协议数据。
// Python: external_cli_model_catalog_context (tool_member.py:182-219)
func ExternalCliModelCatalogContext(team *TeamBackend) string {
	rules := NewExternalCliModelRules(team, nil)
	var currentModel any
	if !team.IsModelGroupPool() && team.CurrentModelName() != "" && team.CurrentModelProvider() != "" {
		currentModel = map[string]any{
			"model_name": team.CurrentModelName(),
			"protocol":   seamModelApiProtocol(team.CurrentModelProvider()),
		}
	}
	compatibleByCLI := map[string][]map[string]string{}
	keys := make([]string, 0, len(team.DeclaredCLIKinds()))
	for kind := range team.DeclaredCLIKinds() {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	for _, cliAgent := range keys {
		if !seamTakesAuthFallback(cliAgent) {
			continue
		}
		providerFilter := seamProviderFilterForCLI(cliAgent)
		options := map[string]bool{}
		if team.IsModelGroupPool() {
			for _, item := range team.ListCliModels(providerFilter) {
				if item.Model != "" {
					options[item.Model+"|"+seamModelApiProtocol(item.Provider)] = true
				}
			}
		} else {
			for _, entry := range rules.CompatiblePoolEntries(cliAgent) {
				if entry.ModelName != "" {
					options[entry.ModelName+"|"+seamModelApiProtocol(entry.APIProvider)] = true
				}
			}
		}
		list := []map[string]string{}
		keys := make([]string, 0, len(options))
		for key := range options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts := strings.SplitN(key, "|", 2)
			list = append(list, map[string]string{"model_name": parts[0], "protocol": parts[1]})
		}
		compatibleByCLI[cliAgent] = list
	}
	data, _ := json.Marshal(map[string]any{
		"current_model":              currentModel,
		"compatible_fallback_models": compatibleByCLI,
	})
	return fmt.Sprintf("<fallback_model_catalog>%s</fallback_model_catalog>", data)
}

// CheckExternalCliDeclared 拒绝团队未声明的 CLI kind（能力天花板）。
// Python: check_external_cli_declared (tool_member.py:167-180)
func CheckExternalCliDeclared(team *TeamBackend, cliAgent string) string {
	if team.ExternalCLIConfig(cliAgent) != nil {
		return ""
	}
	kinds := team.DeclaredCLIKinds()
	shown := make([]string, 0, len(kinds))
	for kind := range kinds {
		shown = append(shown, kind)
	}
	sort.Strings(shown)
	if len(shown) == 0 {
		shown = append(shown, "<none>")
	}
	return fmt.Sprintf("cli_agent '%s' is not declared in TeamAgentSpec.external_cli_agents (declared: %s); "+
		"add a static config entry for it first", cliAgent, strings.Join(shown, ", "))
}

// ExternalCliOptionProperties external_cli 对象的 schema properties
// （spawn_teammate / spawn_expert / scheduled create_task 的内联 member）。
// builtin_model/effort 仅在某个声明 kind 提供内置目录时出现。
// Python: external_cli_option_properties (tool_member.py:456-484)
func ExternalCliOptionProperties(team *TeamBackend, t locales.Translator, builtinModelDescription string) map[string]any {
	properties := map[string]any{
		"fallback_model_name": map[string]any{
			"anyOf": []any{map[string]any{"type": "string", "minLength": 1}, map[string]any{"type": "null"}},
			"description": fmt.Sprintf("%s\n\n%s",
				t("spawn_teammate", "external_cli.fallback_model_name"),
				ExternalCliModelCatalogContext(team)),
		},
	}
	if team.BuiltinModelsEnabled() {
		properties["builtin_model"] = map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("%s\n\n%s", builtinModelDescription, BuiltinModelCatalogText(team)),
		}
		properties["effort"] = map[string]any{"type": "string", "description": t("spawn_teammate", "external_cli.effort")}
	}
	return properties
}

// RegisterTeammate 注册一条普通 LLM teammate 行（UNSTARTED）——唯一知道
// teammate 卡与 mode 如何派生的地方。spawn_teammate、create_task 的内联
// member（F_118）、spawn_expert（带 agent_template）共用，卡 id/mode/options
// 永不漂移。
// Python: register_teammate (tool_member.py:626-667)
func RegisterTeammate(ctx context.Context, team *TeamBackend, in RegisterTeammateInput) atschema.MemberOpResult {
	if team == nil {
		return atschema.NewMemberOpResultFail("team backend is nil")
	}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(team.TeamName()+"_"+in.MemberName),
		agentschema.WithAgentName(in.DisplayName),
		agentschema.WithAgentDescription(in.Desc),
	)
	opts := []SpawnMemberOption{WithMode(team.TeammateMode())}
	if in.Allocation != nil {
		opts = append(opts, WithAllocation(in.Allocation))
	}
	if in.Isolation != "" {
		opts = append(opts, WithIsolation(in.Isolation))
	}
	if in.PermissionsOverride != nil {
		opts = append(opts, WithPermissionsOverride(in.PermissionsOverride))
	}
	if in.Lifetime != nil {
		opts = append(opts, WithLifetime(in.Lifetime))
	}
	if in.AgentTemplate != nil {
		opts = append(opts, WithAgentTemplate(in.AgentTemplate))
	}
	return team.SpawnMember(ctx, in.MemberName, in.DisplayName, card,
		string(atschema.TeamRoleTeammate), in.Desc, in.Prompt, "", opts...)
}

// RegisterTeammateInput RegisterTeammate 的具名入参（Python keyword-only 形参）。
type RegisterTeammateInput struct {
	MemberName          string
	DisplayName         string
	Desc                string
	Prompt              string
	Allocation          *models.Allocation
	Isolation           string
	PermissionsOverride map[string]string
	Lifetime            *database.MemberLifetime
	AgentTemplate       *database.MemberAgentTemplate
}

// RegisterExternalCLIMember 注册一个外部 CLI 成员行（UNSTARTED）：
// 声明 kind 校验、模型规则、注册。spawn_teammate 的 cli_agent 路径、
// spawn_expert 的外部路径、scheduled create_task 的内联成员共用——相同输入
// 恒到相同的行或相同的错误（仅 tool_name 不同）。
// Python: register_external_cli_member (tool_member.py:486-540)
func RegisterExternalCLIMember(ctx context.Context, team *TeamBackend, in RegisterExternalCLIMemberInput) atschema.MemberOpResult {
	if err := CheckExternalCliDeclared(team, in.CLIAgent); err != "" {
		return atschema.NewMemberOpResultFail(err)
	}
	rules := NewExternalCliModelRules(team, in.AllocateModelConfig)
	resolved, reason := rules.Resolve(in.CLIAgent, in.ModelName, in.FallbackModelName, in.BuiltinModel, in.Effort, in.ToolName)
	if resolved == nil {
		return atschema.NewMemberOpResultFail(reason)
	}
	return team.SpawnExternalCLI(ctx, ExternalCLISpawnInput{
		MemberName:         in.MemberName,
		DisplayName:        in.DisplayName,
		CLIAgent:           in.CLIAgent,
		Desc:               in.Desc,
		Prompt:             in.Prompt,
		ModelName:          in.ModelName,
		Allocation:         resolved.Allocation,
		FallbackAllocation: resolved.FallbackAllocation,
		BuiltinModel:       resolved.BuiltinModel,
		Lifetime:           in.Lifetime,
		AgentTemplate:      in.AgentTemplate,
	})
}

// RegisterExternalCLIMemberInput RegisterExternalCLIMember 的具名入参。
type RegisterExternalCLIMemberInput struct {
	ToolName            string
	AllocateModelConfig func(modelName string, opts ...models.AllocateOption) *models.Allocation
	CLIAgent            string
	MemberName          string
	DisplayName         string
	Desc                string
	Prompt              string
	ModelName           string
	FallbackModelName   string
	BuiltinModel        string
	Effort              string
	Lifetime            *database.MemberLifetime
	AgentTemplate       *database.MemberAgentTemplate
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isPlainString 值是否为字符串形态（Python isinstance(x, str) 守卫；Go 入参
// 已收敛为 string，恒真——保留以对齐 resolve 的分支注释位置）。
func isPlainString(string) bool { return true }

// spawnTeammateTool spawn_teammate 新形态。
// Python: SpawnTeammateTool (tool_member.py:669-914)
type spawnTeammateTool struct {
	TeamTool
	team                *TeamBackend
	t                   locales.Translator
	allocateModelConfig func(modelName string, opts ...models.AllocateOption) *models.Allocation
	forkEnabled         bool
	externalCLIEnabled  bool
}

// forkParams / forkSlots / localOnlyParams / externalCliSlots / builtinModelSlots
// Python: _FORK_PARAMS/_FORK_SLOTS/_LOCAL_ONLY_PARAMS/_EXTERNAL_CLI_SLOTS/_BUILTIN_MODEL_SLOTS (:690-695)
var (
	forkParams        = []string{"fork", "fork_source", "fork_mode"}
	forkSlots         = []string{"fork_usage", "fork_param_rows"}
	externalCliSlots  = []string{"external_cli_usage"}
	builtinModelSlots = []string{"builtin_model_param_rows", "builtin_model_usage"}
)

func newSpawnTeammateTool(team *TeamBackend, t locales.Translator, language atschema.Language, modelConfigAllocator func(modelName string, opts ...models.AllocateOption) *models.Allocation, forkEnabled bool) *spawnTeammateTool {
	externalCLIEnabled := len(team.DeclaredCLIKinds()) > 0
	builtinModelsEnabled := externalCLIEnabled && team.BuiltinModelsEnabled()
	st := &spawnTeammateTool{
		team:                team,
		t:                   t,
		allocateModelConfig: modelConfigAllocator,
		forkEnabled:         forkEnabled,
		externalCLIEnabled:  externalCLIEnabled,
	}
	// 卡片：omit_slots 三门控（fork/external_cli/builtin_model）。
	// Python: __init__ :716-723
	omit := map[string]bool{}
	if !forkEnabled {
		for _, name := range forkSlots {
			omit[name] = true
		}
	}
	if !externalCLIEnabled {
		for _, name := range externalCliSlots {
			omit[name] = true
		}
	}
	if !builtinModelsEnabled {
		for _, name := range builtinModelSlots {
			omit[name] = true
		}
	}
	var omitList []string
	for name := range omit {
		omitList = append(omitList, name)
	}
	sort.Strings(omitList)
	// omit 门控经 RenderDesc（Python make_translator 的 omit= 形参；
	// e15 审查发现 Go 原Translator 调用把 omit 列表当 STRINGS key——门控失效）。
	omitSet := map[string]bool{}
	for _, name := range omitList {
		omitSet[name] = true
	}
	description := locales.RenderDesc("spawn_teammate", language, omitSet)
	// 卡片：id=team.spawn_teammate，描述经 omit_slots 门控（Python :574-580）。
	card := tool.ToolCard{BaseCard: schema.BaseCard{ID: "team.spawn_teammate", Name: "spawn_teammate", Description: description}}
	st.TeamTool = NewTeamTool(&card)

	// model_name 描述 + external_cli 附加段。Python: :727-729
	modelDescription := ModelNameDescription(team, t)
	if externalCLIEnabled {
		modelDescription += "\n\n" + t("spawn_teammate", "model_name_external_cli")
	}
	properties := map[string]any{
		"member_name":  map[string]any{"type": "string", "description": t("spawn_teammate", "member_name")},
		"display_name": map[string]any{"type": "string", "description": t("spawn_teammate", "display_name")},
		"desc":         map[string]any{"type": "string", "description": t("spawn_teammate", "desc")},
		"prompt":       map[string]any{"type": "string", "description": t("spawn_teammate", "prompt")},
		"model_name":   map[string]any{"type": "string", "description": modelDescription},
		"isolation": map[string]any{
			"type": "string", "enum": []string{"worktree"},
			"description": ("Optional isolation mode. Set 'worktree' only when the " +
				"user explicitly requests worktree isolation, or when " +
				"the teammate must modify repository files in an " +
				"isolated checkout. Omit this field for read-only, " +
				"game, discussion, research, or standby tasks."),
		},
		"permissions": map[string]any{"type": "object", "description": t("spawn_teammate", "permissions")},
	}
	if forkEnabled {
		properties["fork"] = map[string]any{
			"anyOf":       []any{map[string]any{"type": "boolean"}, map[string]any{"type": "string"}},
			"description": t("spawn_teammate", "fork"),
		}
		properties["fork_source"] = map[string]any{"type": "string", "description": t("spawn_teammate", "fork_source")}
		properties["fork_mode"] = map[string]any{
			"type": "string", "enum": []string{"full", "before", "after", "keep_before_compact_after", "keep_after_compact_before"},
			"description": t("spawn_teammate", "fork_mode"),
		}
	}
	if externalCLIEnabled {
		properties["cli_agent"] = map[string]any{"type": "string", "description": t("spawn_teammate", "cli_agent")}
		properties["external_cli"] = map[string]any{
			"type":        "object",
			"description": t("spawn_teammate", "external_cli"),
			"properties": ExternalCliOptionProperties(team, t,
				t("spawn_teammate", "external_cli.builtin_model")),
		}
	}
	card.InputParams = []*schema.Param{}
	card.Properties = properties
	return st
}

// Invoke 执行 spawn_teammate。
// Python: invoke (tool_member.py:800-862)
func (s *spawnTeammateTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	if err := ValidateMemberName(memberName); err != "" {
		return toolError(err)
	}
	displayName, _ := inputs["display_name"].(string)
	if err := ValidateDisplayName(displayName); err != "" {
		return toolError(err)
	}
	// Schema 省略约束宿主 LLM；这里拒绝 MCP 客户端直传的同款参数——在成员行
	// 写入前检查，被拒调用不产生任何 spawn。Python: :809-821
	if !s.forkEnabled {
		passed := []string{}
		for _, key := range forkParams {
			if v, ok := inputs[key]; ok && v != nil {
				passed = append(passed, key)
			}
		}
		if len(passed) > 0 {
			return toolError(fmt.Sprintf("Cannot use %s: context inheritance (fork) is "+
				"disabled (enable_fork=False on TeamAgentSpec). Spawn the "+
				"teammate without these arguments, or enable fork in the team spec.",
				strings.Join(passed, ", ")))
		}
	}
	if err := s.checkExternalCLIArgs(inputs); err != "" {
		return toolError(err)
	}
	cliAgent := OptionalText(inputs["cli_agent"])
	if cliAgent != "" {
		return s.spawnExternalCLI(ctx, inputs, cliAgent)
	}

	desc, _ := inputs["desc"].(string)
	prompt, _ := inputs["prompt"].(string)
	var allocation *models.Allocation
	if s.allocateModelConfig != nil {
		allocation = s.allocateModelConfig(OptionalText(inputs["model_name"]))
	}
	if err := CheckModelSelectable(s.team, inputs["model_name"], allocation); err != "" {
		return toolError(err)
	}
	isolation, _ := inputs["isolation"].(string)
	permissionsOverride, _ := inputs["permissions"].(map[string]string)
	result := RegisterTeammate(ctx, s.team, RegisterTeammateInput{
		MemberName:          memberName,
		DisplayName:         displayName,
		Desc:                desc,
		Prompt:              prompt,
		Allocation:          allocation,
		Isolation:           isolation,
		PermissionsOverride: permissionsOverride,
	})
	if !result.OK {
		return toolError(result.Reason)
	}
	// Python: fork 记账（:848-855）
	// Python: team.mark_fork_on_spawn（:848-855）——fork 快照/继承链属
	// 20.4 域（CheckpointTool 同源），Go TeamBackend 未落该方法。
	// 偏差 D-15.16-e17：spawn 期 fork 记账 ⤴️fork 章节，步骤位置保留。
	if forkValue, ok := inputs["fork"]; ok && forkValue != nil && forkValue != "false" && forkValue != false {
		logger.Warn(toolMemberLogComponent).
			Str("member_name", memberName).
			Msg("fork-on-spawn accounting not ported (D-15.16-e17, pending fork chapter)")
	}
	data := map[string]any{
		"member_name": memberName, "display_name": displayName, "role_type": "teammate",
	}
	if isolation != "" {
		data["isolation"] = isolation
	}
	return toolSuccess(data)
}

// checkExternalCLIArgs 在任何写入前拒绝本次调用无法携带的 cli_agent/external_cli。
// Python: _check_external_cli_args (tool_member.py:864-884)
func (s *spawnTeammateTool) checkExternalCLIArgs(inputs map[string]any) string {
	cliAgent := OptionalText(inputs["cli_agent"])
	externalArgs, hasExternal := inputs["external_cli"]
	if (cliAgent != "" || hasExternal) && !s.externalCLIEnabled {
		return "cli_agent / external_cli are unavailable: this team declares no external CLI kinds"
	}
	if hasExternal && externalArgs != nil {
		if _, ok := externalArgs.(map[string]any); !ok {
			return "'external_cli' must be an object"
		}
	}
	if extMap, ok := externalArgs.(map[string]any); ok && len(extMap) > 0 && cliAgent == "" {
		return "'external_cli' applies only to an external CLI member: set 'cli_agent', or omit it"
	}
	if cliAgent != "" {
		localOnly := []string{}
		for _, key := range append([]string{"isolation", "permissions"}, forkParams...) {
			if v, ok := inputs[key]; ok && v != nil {
				localOnly = append(localOnly, key)
			}
		}
		if len(localOnly) > 0 {
			return fmt.Sprintf("%s cannot be set with cli_agent '%s': they apply only to a "+
				"local teammate; omit them, or omit 'cli_agent'", strings.Join(localOnly, ", "), cliAgent)
		}
	}
	return ""
}

// spawnExternalCLI 注册成第三方 CLI 成员（role_type='external_cli'）。
// Python: _spawn_external_cli (tool_member.py:886-914)
func (s *spawnTeammateTool) spawnExternalCLI(ctx context.Context, inputs map[string]any, cliAgent string) (map[string]any, error) {
	externalArgs, _ := inputs["external_cli"].(map[string]any)
	if externalArgs == nil {
		externalArgs = map[string]any{}
	}
	memberName, _ := inputs["member_name"].(string)
	displayName, _ := inputs["display_name"].(string)
	if err := ValidateDisplayName(displayName); err != "" {
		return toolError(err)
	}
	desc, _ := inputs["desc"].(string)
	prompt, _ := inputs["prompt"].(string)
	fallbackModelName, _ := externalArgs["fallback_model_name"].(string)
	builtinModel, _ := externalArgs["builtin_model"].(string)
	effort, _ := externalArgs["effort"].(string)
	result := RegisterExternalCLIMember(ctx, s.team, RegisterExternalCLIMemberInput{
		ToolName:            "spawn_teammate",
		AllocateModelConfig: s.allocateModelConfig,
		CLIAgent:            cliAgent,
		MemberName:          memberName,
		DisplayName:         displayName,
		Desc:                desc,
		Prompt:              prompt,
		ModelName:           OptionalText(inputs["model_name"]),
		FallbackModelName:   fallbackModelName,
		BuiltinModel:        builtinModel,
		Effort:              effort,
	})
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{
		"member_name": memberName, "display_name": displayName, "role_type": "external_cli", "cli_agent": cliAgent,
	})
}

// RenderForLLM 模型面渲染。
// Python: _SpawnToolBase.render_for_llm (tool_member.py:613-623)
func (s *spawnTeammateTool) RenderForLLM(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to spawn member"
	}
	role := "teammate"
	if r, ok := output["role_type"].(string); ok && r != "" {
		role = r
	}
	cliAgent, _ := output["cli_agent"].(string)
	suffix := ""
	if cliAgent != "" {
		suffix = " cli_agent=" + cliAgent
	}
	memberName, _ := output["member_name"].(string)
	displayName, _ := output["display_name"].(string)
	return fmt.Sprintf("Member spawned: member_name=%s display_name=%s role=%s%s", memberName, displayName, role, suffix)
}

// MapResult spawn 结果的模型面映射。
// Python: SpawnTeammateTool.map_result（经 _SpawnToolBase.render_for_llm 等价）。
func (s *spawnTeammateTool) MapResult(output map[string]any) string {
	return s.RenderForLLM(output)
}

// spawnHumanAgentTool spawn_human_agent 独立工具（role_type='human_agent'）。
// schema 刻意省 model_name/prompt——human 成员跑框架模板，运行期无字段可拒；
// HITT 检查是防御性兜底（工具在 HITT 关闭时不接线）。
// Python: SpawnHumanAgentTool (tool_member.py:1041-1096)
type spawnHumanAgentTool struct {
	TeamTool
	team *TeamBackend
	t    locales.Translator
}

// newSpawnHumanAgentTool 构造 spawn_human_agent 工具。
func newSpawnHumanAgentTool(team *TeamBackend, t locales.Translator) *spawnHumanAgentTool {
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID:          "team.spawn_human_agent",
		Name:        "spawn_human_agent",
		Description: t("spawn_human_agent"),
	}}
	card.InputParams = []*schema.Param{}
	card.Properties = map[string]any{
		"member_name":  map[string]any{"type": "string", "description": t("spawn_human_agent", "member_name")},
		"display_name": map[string]any{"type": "string", "description": t("spawn_human_agent", "display_name")},
		"desc":         map[string]any{"type": "string", "description": t("spawn_human_agent", "desc")},
	}
	return &spawnHumanAgentTool{TeamTool: NewTeamTool(&card), team: team, t: t}
}

// Invoke 执行 spawn_human_agent。
// Python: invoke (tool_member.py:1068-1096)
func (s *spawnHumanAgentTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	if err := ValidateMemberName(memberName); err != "" {
		return toolError(err)
	}
	displayName, _ := inputs["display_name"].(string)
	if err := ValidateDisplayName(displayName); err != "" {
		return toolError(err)
	}
	if !s.team.HITTEnabled() {
		return toolError("Cannot spawn human agent: HITT capability is disabled " +
			"(enable_hitt=False on TeamAgentSpec or build_team). " +
			"Enable HITT in the team spec or use spawn_teammate instead.")
	}
	desc, _ := inputs["desc"].(string)
	result := s.team.SpawnHumanAgent(ctx, memberName, displayName, desc, "")
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{
		"member_name": memberName, "display_name": displayName, "role_type": "human_agent",
	})
}

// MapResult spawn 结果的模型面映射（对齐 render_for_llm）。
func (s *spawnHumanAgentTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to spawn member"
	}
	d, _ := output["data"].(map[string]any)
	return fmt.Sprintf("Member spawned: member_name=%v display_name=%v role=%v", d["member_name"], d["display_name"], d["role_type"])
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnTeammateTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnHumanAgentTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── fork 半区工具（tool_member.py:917-1038） ────────────────────────────

// checkpointTool checkpoint：保存当前会话上下文的命名快照（fork 能力的半区）。
// 门控 TeamAgentSpec.enable_fork——关闭时工具不装配（create_team_tools），
// invoke 内的检查是对直调 MCP 客户端的兜底。
// Python: CheckpointTool (tool_member.py:917-990)
type checkpointTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
	// t 翻译器闭包（duplicate 错误文案）
	t locales.Translator
}

// newCheckpointTool 构造 checkpoint。
// Python: __init__ (:925-941)
func newCheckpointTool(team *TeamBackend, t locales.Translator, language atschema.Language) *checkpointTool {
	description := locales.RenderDesc("checkpoint", language, nil)
	return &checkpointTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.checkpoint", "checkpoint", description,
			[]*schema.Param{
				schema.NewStringParam("name", t("checkpoint", "name"), true),
				schema.NewStringParam("description", t("checkpoint", "description"), false),
			}, nil)),
		team: team,
		t:    t,
	}
}

// Invoke 执行 checkpoint。
// Python: invoke (:943-968)
func (c *checkpointTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	// fork 关闭的兜底（enable_fork=False）：快照存在的唯一意义就是被 fork。
	if !c.team.ForkEnabled() {
		return toolError("Cannot save a checkpoint: context inheritance (fork) is " +
			"disabled (enable_fork=False on TeamAgentSpec). Checkpoints " +
			"exist only to be forked from.")
	}
	name, _ := inputs["name"].(string)
	count := c.team.SnapshotContextLength()
	description, _ := inputs["description"].(string)
	conflict := c.team.StoreCheckpoint(name, count, description, c.team.memberName)
	if conflict != nil {
		createdBy, _ := conflict["created_by"].(string)
		if createdBy == "" {
			createdBy = "?"
		}
		desc, _ := conflict["description"].(string)
		// Python: t("checkpoint", "duplicate", name=..., created_by=..., description=...)——
		// STRINGS 插值形态；Go STRINGS 表无 kwargs 插值签名，直格式化同源文案。
		return toolError(fmt.Sprintf("快照名 %s 已存在（创建者 %s，描述 %s）；换一个名字，或用 list_checkpoints 查看既有快照", name, createdBy, desc))
	}
	// leader 侧框架事件通知（非成员消息）：快照名以 announcement-only 注记
	// 进入 leader 上下文，不提示回复。leader 自身快照无需自通知。
	if !c.team.isLeader {
		c.team.PublishCheckpointCreated(ctx, name, count, description)
	}
	return toolSuccess(map[string]any{"name": name, "message_count": count})
}

// MapResult 模型面渲染。Python: render_for_llm (:970-975)
func (c *checkpointTool) MapResult(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to save checkpoint"
	}
	data := output["data"].(map[string]any)
	return fmt.Sprintf("Checkpoint '%v' saved at message %v", data["name"], data["message_count"])
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (c *checkpointTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// listCheckpointsTool list_checkpoints：列出全部可 fork 继承的命名快照。
// Python: ListCheckpointsTool (tool_member.py:992-1038)
type listCheckpointsTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// newListCheckpointsTool 构造 list_checkpoints。Python: __init__ (:997-1005)
func newListCheckpointsTool(team *TeamBackend, t locales.Translator, language atschema.Language) *listCheckpointsTool {
	return &listCheckpointsTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.list_checkpoints", "list_checkpoints",
			locales.RenderDesc("list_checkpoints", language, nil), []*schema.Param{}, map[string]any{})),
		team: team,
	}
}

// Invoke 执行 list_checkpoints。Python: invoke (:1008-1019)
func (l *listCheckpointsTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	checkpoints := l.team.ListCheckpoints()
	names := make([]string, 0, len(checkpoints))
	for name := range checkpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	items := []map[string]any{}
	for _, name := range names {
		record := checkpoints[name]
		items = append(items, map[string]any{
			"name":          name,
			"message_count": record["count"],
			"description":   record["description"],
			"created_by":    record["created_by"],
		})
	}
	return toolSuccess(map[string]any{"checkpoints": items, "count": len(items)})
}

// MapResult 模型面渲染。Python: render_for_llm (:1021-1038)
func (l *listCheckpointsTool) MapResult(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to list checkpoints"
	}
	data := output["data"].(map[string]any)
	items, _ := data["checkpoints"].([]map[string]any)
	if len(items) == 0 {
		return "No checkpoints"
	}
	lines := []string{}
	for _, item := range items {
		line := fmt.Sprintf("name=%v message_count=%v created_by=%v", item["name"], item["message_count"], item["created_by"])
		if desc, _ := item["description"].(string); desc != "" {
			line += fmt.Sprintf(" description=%q", desc)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (l *listCheckpointsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── spawn_passive_human / spawn_bridge_agent / set_member_model ────────────────────────────

// spawnPassiveHumanTool spawn_passive_human：注册无头像人类成员（出生 READY）。
// schema 省模型名称/提示词（同 spawn_human_agent：无 harness 无字段可拒）；
// HITT 检查为防御性兜底（工具在 HITT 关闭时不接线）。
// Python: SpawnPassiveHumanTool (tool_member.py:1099-1160)
type spawnPassiveHumanTool struct {
	TeamTool
	team *TeamBackend
	t    locales.Translator
}

// newSpawnPassiveHumanTool 构造。Python: __init__ (:1112-1127)
func newSpawnPassiveHumanTool(team *TeamBackend, t locales.Translator) *spawnPassiveHumanTool {
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID:          "team.spawn_passive_human",
		Name:        "spawn_passive_human",
		Description: t("spawn_passive_human"),
	}}
	card.InputParams = []*schema.Param{
		schema.NewStringParam("member_name", t("spawn_passive_human", "member_name"), true),
		schema.NewStringParam("display_name", t("spawn_passive_human", "display_name"), true),
		schema.NewStringParam("desc", t("spawn_passive_human", "desc"), true),
	}
	return &spawnPassiveHumanTool{TeamTool: NewTeamTool(&card), team: team, t: t}
}

// Invoke 执行。group_chat 运行时前置 + HITT 兜底 + 校验 + 委托。
// Python: invoke (:1129-1157)
func (s *spawnPassiveHumanTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	if s.team.ConversationMode() != "group_chat" {
		return toolError("spawn_passive_human requires a group_chat runtime")
	}
	if err := ValidateMemberName(stringOfInput(inputs["member_name"])); err != "" {
		return toolError(err)
	}
	if err := ValidateDisplayName(stringOfInput(inputs["display_name"])); err != "" {
		return toolError(err)
	}
	if !s.team.HITTEnabled() {
		return toolError("Cannot spawn passive human: HITT capability is disabled " +
			"(enable_hitt=False on TeamAgentSpec or build_team). " +
			"Enable HITT in the team spec or use spawn_teammate instead.")
	}
	result := s.team.SpawnPassiveHuman(ctx,
		stringOfInput(inputs["member_name"]),
		stringOfInput(inputs["display_name"]),
		stringOfInput(inputs["desc"]))
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{"member_name": inputs["member_name"]})
}

// MapResult 模型面渲染。Python: render_for_llm (:1159-1160)
func (s *spawnPassiveHumanTool) MapResult(output map[string]any) string {
	if err, ok := output["error"].(string); ok && err != "" {
		return err
	}
	data := output["data"].(map[string]any)
	return fmt.Sprintf("Passive human member %s registered as READY", data["member_name"])
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnPassiveHumanTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// stringOfInput any → string（nil 安全）。
func stringOfInput(v any) string {
	s, _ := v.(string)
	return s
}

// spawnBridgeAgentTool spawn_bridge_agent：桥接头像成员（本地 LLM 调度 +
// 远端 agent 产出）。prompt 必填（远端经 adapter.connect 采用的私有系统
// 提示词）。Bridge 能力检查为兜底（工具在 Bridge 关闭时不接线）。
// Python: SpawnBridgeAgentTool (tool_member.py:1163-1269)
type spawnBridgeAgentTool struct {
	TeamTool
	team *TeamBackend
	t    locales.Translator
}

// newSpawnBridgeAgentTool 构造。Python: __init__ (:1176-1199)
func newSpawnBridgeAgentTool(team *TeamBackend, t locales.Translator) *spawnBridgeAgentTool {
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID:          "team.spawn_bridge_agent",
		Name:        "spawn_bridge_agent",
		Description: t("spawn_bridge_agent"),
	}}
	card.InputParams = []*schema.Param{
		schema.NewStringParam("member_name", t("spawn_bridge_agent", "member_name"), true),
		schema.NewStringParam("display_name", t("spawn_bridge_agent", "display_name"), false),
		schema.NewStringParam("desc", t("spawn_bridge_agent", "desc"), false),
		schema.NewStringParam("prompt", t("spawn_bridge_agent", "prompt"), true),
		schema.NewStringParam("mailbox_inject_mode", t("spawn_bridge_agent", "mailbox_inject_mode"), false),
		schema.NewStringParam("protocol", t("spawn_bridge_agent", "protocol"), false),

		schema.NewStringParam("model_name", t("spawn_bridge_agent", "model_name"), false),
	}
	return &spawnBridgeAgentTool{TeamTool: NewTeamTool(&card), team: team, t: t}
}

// Invoke 执行。Bridge 兜底 + prompt 必填 + inject_mode 枚举 + adapter_config 形状。
// Python: invoke (:1218-1258)
func (s *spawnBridgeAgentTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	if !s.team.bridgeEnabled() {
		return toolError("Cannot spawn bridge agent: Bridge capability is disabled " +
			"(enable_bridge=False on TeamAgentSpec or build_team). " +
			"Enable Bridge in the team spec or use spawn_teammate instead.")
	}
	desc := stringOfInput(inputs["desc"])
	prompt := stringOfInput(inputs["prompt"])
	if prompt == "" {
		return toolError("spawn_bridge_agent requires a non-empty 'prompt' — it is the " +
			"briefing the remote agent adopts via adapter.connect")
	}
	modeRaw := strings.ToLower(stringOfInput(inputs["mailbox_inject_mode"]))
	if modeRaw == "" {
		modeRaw = "passthrough"
	}
	if modeRaw != "passthrough" && modeRaw != "rephrase" {
		return toolError(fmt.Sprintf("Invalid mailbox_inject_mode '%s'; expected 'passthrough' or 'rephrase'", modeRaw))
	}
	if _, ok := inputs["adapter_config"].(map[string]any); inputs["adapter_config"] != nil && !ok {
		return toolError("adapter_config must be an object/dict")
	}
	adapterConfig, _ := inputs["adapter_config"].(map[string]any)
	result := s.team.SpawnBridgeAgent(ctx,
		stringOfInput(inputs["member_name"]),
		stringOfInput(inputs["display_name"]),
		desc, prompt,
		stringOfInput(inputs["model_name"]),
		modeRaw,
		stringOfInput(inputs["protocol"]),
		adapterConfig)
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{"member_name": inputs["member_name"]})
}

// MapResult 模型面渲染。Python: render_for_llm (:1261-1269)
func (s *spawnBridgeAgentTool) MapResult(output map[string]any) string {
	if err, ok := output["error"].(string); ok && err != "" {
		return err
	}
	data := output["data"].(map[string]any)
	return fmt.Sprintf("Bridge agent %s registered as READY", data["member_name"])
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnBridgeAgentTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// spawnSetMemberModelTool set_member_model：切换运行中外部 CLI 成员的内置模型。
// 实际切换经 TeamBackend 的 set_member_model_fn 回调（外部 CLI 运行时注入，
// Python team.py:591 set_member_model_fn）——工具面只做入参校验与行持久化。
// Python: SetMemberModelTool (tool_member.py:1400-1529 区段)
type spawnSetMemberModelTool struct {
	TeamTool
	team *TeamBackend
	t    locales.Translator
}

// newSetMemberModelTool 构造。
func newSetMemberModelTool(team *TeamBackend, t locales.Translator) *spawnSetMemberModelTool {
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID:          "team.set_member_model",
		Name:        "set_member_model",
		Description: t("set_member_model"),
	}}
	card.Properties = map[string]any{
		"member_name":    map[string]any{"type": "string", "description": t("set_member_model", "member_name")},
		"builtin_model":  map[string]any{"type": "object", "description": t("set_member_model", "builtin_model")},
		"model_name":     map[string]any{"type": "string", "description": t("set_member_model", "model_name")},
		"restart_member": map[string]any{"type": "boolean", "description": t("set_member_model", "restart_member")},
	}
	return &spawnSetMemberModelTool{TeamTool: NewTeamTool(&card), team: team, t: t}
}

// Invoke 执行。委托 set_member_model_fn（成员行持久化 + applied_live 切换）。
// Python: invoke（member 行 builtin_model/model_ref 更新 + fn 回调）。
func (s *spawnSetMemberModelTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName := stringOfInput(inputs["member_name"])
	if memberName == "" {
		return toolError("set_member_model requires 'member_name'")
	}
	if s.team.setMemberModelFn == nil {
		return toolError("set_member_model is not available: no runtime hook wired for this member")
	}
	builtinModel := map[string]any(nil)
	if m, ok := inputs["builtin_model"].(map[string]any); ok {
		builtinModel = m
	}
	ok, err := s.team.setMemberModelFn(ctx, memberName, builtinModel, stringOfInput(inputs["model_name"]))
	if err != nil {
		return toolError(err.Error())
	}
	if !ok {
		return toolError("set_member_model rejected the requested model")
	}
	return toolSuccess(map[string]any{"member_name": memberName})
}

// MapResult 模型面渲染。
func (s *spawnSetMemberModelTool) MapResult(output map[string]any) string {
	if err, ok := output["error"].(string); ok && err != "" {
		return err
	}
	data := output["data"].(map[string]any)
	return fmt.Sprintf("Model updated for member %s", data["member_name"])
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnSetMemberModelTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
