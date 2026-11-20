// Python 源：openjiuwen/agent_teams/tools/tool_expert.py（续段 1:1）
//
// ViewExpertGroupsTool（:538-700）/ ExpertTemplate（:703-713）/
// _ExpertMemberIdentity（:716-723）/ ExpertUnavailable（:726-727）/
// ExpertTemplateResolver（:730-863）/ expert_member_texts（:867-872）/
// expert_member_record（:875-879）/ expert_member_cli_config（:882-892）/
// SpawnExpertTool（:895-1097）。

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/experts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── ViewExpertGroupsTool（:538-700） ────────────────────────────

// viewExpertGroupsTool view_expert_groups：列出/检视本机安装的 AgentGroup 及成员花名册。
type viewExpertGroupsTool struct {
	TeamTool
	catalog          experts.ExpertCatalog
	language         string
	boundGroup       *experts.BoundAgentGroup
	declaredCLIKinds map[string]bool
}

// newViewExpertGroupsTool 构造 view_expert_groups。
// Python: __init__ (:541-576)
func newViewExpertGroupsTool(catalog experts.ExpertCatalog, t locales.Translator, language string, boundGroup *experts.BoundAgentGroup, declaredCLIKinds map[string]bool) *viewExpertGroupsTool {
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID: "team.view_expert_groups", Name: "view_expert_groups", Description: t("view_expert_groups"),
	}}
	card.Properties = map[string]any{
		"action":       map[string]any{"type": "string", "enum": []string{"list", "get"}, "default": "list", "description": t("view_expert_groups", "action")},
		"query":        map[string]any{"type": "string", "description": t("view_expert_groups", "query")},
		"capabilities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": t("view_expert_groups", "capabilities")},
		"name":         map[string]any{"type": "string", "description": t("view_expert_groups", "name")},
		"member":       map[string]any{"type": "string", "description": t("view_expert_groups", "member")},
	}
	return &viewExpertGroupsTool{
		TeamTool: NewTeamTool(&card), catalog: catalog, language: language,
		boundGroup: boundGroup, declaredCLIKinds: declaredCLIKinds,
	}
}

// Invoke 执行 view_expert_groups。
// Python: invoke (:578-591)
func (v *viewExpertGroupsTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	action, _ := inputs["action"].(string)
	if action == "" {
		action = "list"
	}
	if action == "list" {
		capabilities := []string{}
		if raw, ok := inputs["capabilities"].([]any); ok {
			for _, item := range raw {
				if s, ok := item.(string); ok {
					capabilities = append(capabilities, s)
				}
			}
		} else if _, present := inputs["capabilities"]; present && inputs["capabilities"] != nil {
			return toolError("'capabilities' must be a list of capability names")
		}
		return v.list(OptionalText(inputs["query"]), capabilities)
	}
	if action == "get" {
		return v.get(OptionalText(inputs["name"]), OptionalText(inputs["member"]))
	}
	return toolError(fmt.Sprintf("Unknown action '%s'; expected 'list' or 'get'", action))
}

// boundName 绑定组名（可空）。Python: _bound_name (:593-594)
func (v *viewExpertGroupsTool) boundName() string {
	if v.boundGroup != nil {
		return v.boundGroup.Name
	}
	return ""
}

// list 组列表（capabilities 超集过滤 + BM25 排序）。
// Python: _list (:596-621)
func (v *viewExpertGroupsTool) list(query string, capabilities []string) (map[string]any, error) {
	cards := readCards(v.catalog.ListGroupPackages(), groupCardAt, "expert group")
	required := map[string]bool{}
	for _, c := range capabilities {
		required[c] = true
	}
	candidates := []struct {
		Location experts.PackageLocation
		Card     *experts.AgentGroupCard
	}{}
	for _, entry := range cards {
		match := true
		for capName := range required {
			found := false
			for _, c := range entry.Card.Capabilities {
				if c == capName {
					found = true
					break
				}
			}
			if !found {
				match = false
				break
			}
		}
		if match {
			candidates = append(candidates, entry)
		}
	}
	names := make([]string, len(candidates))
	docs := make([]string, len(candidates))
	for i := range candidates {
		names[i] = candidates[i].Location.Name
		docs[i] = groupDocument(candidates[i].Location.Name, candidates[i].Card)
	}
	order := rankByQuery(query, names, docs)
	items := []map[string]any{}
	for _, index := range order {
		location, card := candidates[index].Location, candidates[index].Card
		memberCount := 0
		for i := range card.Members {
			if !card.Members[i].IsLeader() {
				memberCount++
			}
		}
		items = append(items, map[string]any{
			"name":         location.Name,
			"display_name": orStr(card.DisplayLabel(v.language), location.Name),
			"members":      memberCount,
			"capabilities": card.Capabilities,
			"description":  expertFirstLine(card.DisplayText(v.language)),
			"bound":        location.Name == v.boundName(),
		})
	}
	return toolSuccess(map[string]any{"action": "list", "query": query, "capabilities": capabilities, "groups": items})
}

// get 组/成员明细。Python: _get (:623-664)
func (v *viewExpertGroupsTool) get(name, member string) (map[string]any, error) {
	if name == "" {
		return toolError("'name' is required for action 'get'")
	}
	location, card, err := readInstalledGroup(v.catalog, name)
	if err != nil {
		return toolError(err.Error())
	}
	bound := location.Name == v.boundName()
	if member != "" {
		entry := card.Member(member)
		if entry == nil {
			return toolError(missingMemberError(member, location, card))
		}
		detail := groupMemberDetail(location, entry, v.language, v.boundGroup, v.declaredCLIKinds)
		return toolSuccess(map[string]any{"action": "get", "member": detail})
	}
	members := []map[string]any{}
	for i := range card.Members {
		entry := &card.Members[i]
		members = append(members, map[string]any{
			"name":         entry.Name,
			"display_name": entry.DisplayLabel(v.language),
			"description":  expertFirstLine(entry.Card.DisplayText(v.language)),
			"runtime":      runtimeLabel(entry.Card),
			"spawnable":    isSpawnable(entry, v.declaredCLIKinds),
			"predefined":   isPredefinedMember(location.Name, entry.Name, v.boundGroup),
		})
	}
	group := map[string]any{
		"name":         location.Name,
		"display_name": orStr(card.DisplayLabel(v.language), location.Name),
		"description":  card.DisplayText(v.language),
		"capabilities": card.Capabilities,
		"version":      card.Version,
		"source":       location.Source,
		"bound":        bound,
		"members":      members,
	}
	return toolSuccess(map[string]any{"action": "get", "group": group})
}

// MapResult 模型面渲染。
// Python: render_for_llm (:666-700)
func (v *viewExpertGroupsTool) MapResult(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to view expert groups"
	}
	action, _ := output["action"].(string)
	if action == "get" {
		if member, ok := output["member"].(map[string]any); ok {
			return renderGroupMemberDetail(member)
		}
		if group, ok := output["group"].(map[string]any); ok {
			lines := []string{}
			name, _ := group["name"].(string)
			displayName, _ := group["display_name"].(string)
			lines = append(lines, fmt.Sprintf("Expert group: %s (%s)", name, displayName))
			if bound, _ := group["bound"].(bool); bound {
				lines = append(lines, "bound: true — this team was assembled from this group")
			}
			if capabilities, ok := group["capabilities"].([]string); ok && len(capabilities) > 0 {
				lines = append(lines, "capabilities: "+strings.Join(capabilities, ", "))
			}
			if version, _ := group["version"].(string); version != "" {
				lines = append(lines, "version: "+version)
			}
			description, _ := group["description"].(string)
			lines = append(lines, "description: "+description)
			lines = append(lines, "members:")
			if members, ok := group["members"].([]map[string]any); ok {
				for _, member := range members {
					note := ""
					if predefined, _ := member["predefined"].(bool); predefined {
						note = " predefined=true"
					}
					spawnable, _ := member["spawnable"].(bool)
					lines = append(lines, fmt.Sprintf("- name=%v display_name=%v runtime=%v spawnable=%v%s — %v",
						member["name"], member["display_name"], member["runtime"], spawnable, note, member["description"]))
				}
			}
			return strings.Join(lines, "\n")
		}
	}
	groups, _ := output["groups"].([]map[string]any)
	query, _ := output["query"].(string)
	capabilities, _ := output["capabilities"].([]string)
	if len(groups) == 0 {
		if query != "" || len(capabilities) > 0 {
			return "No installed expert group matches"
		}
		return "No expert groups installed"
	}
	lines := []string{}
	for _, item := range groups {
		capabilitiesJoined := "-"
		if caps, ok := item["capabilities"].([]string); ok && len(caps) > 0 {
			capabilitiesJoined = strings.Join(caps, ",")
		}
		boundNote := ""
		if bound, _ := item["bound"].(bool); bound {
			boundNote = " bound=true"
		}
		lines = append(lines, fmt.Sprintf("name=%v display_name=%v members=%v capabilities=%s%s — %v",
			item["name"], item["display_name"], item["members"], capabilitiesJoined, boundNote, item["description"]))
	}
	return strings.Join(lines, "\n")
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (v *viewExpertGroupsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── ExpertTemplate / Resolver（:703-863） ────────────────────────────

// ExpertTemplate 一个 spawn 从专家模板需要的东西（无论来源）。
// Python: ExpertTemplate (tool_expert.py:703-713)
type ExpertTemplate struct {
	// Ref 专家引用
	Ref experts.ExpertRef
	// Version spawn 时的模板版本；未知为空
	Version string
	// DisplayName 展示名
	DisplayName string
	// Desc 描述
	Desc string
	// Persona 渲染后的 persona
	Persona string
	// Snapshot native 成员的能力快照
	Snapshot *hschema.AgentTemplateSpec
	// ExternalCLI 外部 CLI 成员的启动配置
	ExternalCLI *atschema.ExternalCliAgentSpec
}

// expertMemberIdentity 一次 spawn_expert 注册的成员的名字与文本。
// Python: _ExpertMemberIdentity (:716-723)
type expertMemberIdentity struct {
	memberName  string
	displayName string
	desc        string
	prompt      string
}

// expertUnavailable 请求的专家模板无法解析；消息即模型面原因。
// Python: ExpertUnavailable (:726-727)
type expertUnavailable struct{ msg string }

func (e *expertUnavailable) Error() string { return e.msg }

// ExpertTemplateResolver 为新成员解析专家模板：spawn_expert 与 scheduled
// create_task 的内联成员共用。host 已装配进团队 spec 的绑定组成员直接取自
// spec（零磁盘访问，快照与 persona 同预定义成员）；其余模板——包括被 spec
// 排除的绑定组成员（如空名册起步的 scheduled 团队）——经 catalog + core
// 加载器按需加载。persona 以 persona_language（基础 spec 的）渲染，展示名以
// leader 的 language。
// Python: ExpertTemplateResolver (:730-756)
type ExpertTemplateResolver struct {
	catalog          experts.ExpertCatalog
	language         string
	boundGroup       *experts.BoundAgentGroup
	personaWorkspace string
	personaLanguage  string
}

// NewExpertTemplateResolver 构造解析器。
// Python: __init__ (:743-756)
func NewExpertTemplateResolver(catalog experts.ExpertCatalog, language string, boundGroup *experts.BoundAgentGroup, personaWorkspace, personaLanguage string) *ExpertTemplateResolver {
	return &ExpertTemplateResolver{
		catalog: catalog, language: language, boundGroup: boundGroup,
		personaWorkspace: personaWorkspace, personaLanguage: personaLanguage,
	}
}

// Resolve 解析一个专家模板；无法解析时返回 *expertUnavailable（模型面消息）。
// Python: resolve (:758-780)
func (r *ExpertTemplateResolver) Resolve(expert string, group string) (*ExpertTemplate, error) {
	if group != "" && expert == experts.LeaderMember {
		return nil, &expertUnavailable{msg: fmt.Sprintf("The leader of expert group '%s' cannot be spawned as a teammate", group)}
	}
	bound := r.boundGroup
	// host 已装配进团队 spec 的绑定组成员从 spec 孵化，与其预定义实例同源。
	// 成员被 spec 排除的绑定组（scheduled 团队空名册起步）按普通组按需加载。
	if group != "" && bound != nil && group == bound.Name {
		if member := bound.Member(expert); member != nil {
			return &ExpertTemplate{
				Ref:         experts.ExpertRef{Name: expert, Group: group},
				Version:     "",
				DisplayName: member.DisplayName,
				Desc:        member.Desc,
				Persona:     member.Persona,
				Snapshot:    member.Snapshot,
				ExternalCLI: member.ExternalCLI,
			}, nil
		}
	}
	if group != "" {
		return r.groupMemberTemplate(expert, group)
	}
	return r.singleExpertTemplate(expert)
}

// singleExpertTemplate 单专家模板加载。
// Python: _single_expert_template (:782-798)
func (r *ExpertTemplateResolver) singleExpertTemplate(expert string) (*ExpertTemplate, error) {
	location, err := r.catalog.LocateExpert(expert)
	if err != nil {
		installed := []string{}
		for _, item := range r.catalog.ListExpertPackages() {
			installed = append(installed, item.Name)
		}
		shown := strings.Join(installed, ", ")
		if shown == "" {
			shown = "<none>"
		}
		logger.Warn(toolExpertLogComponent).
			Str("expert", expert).
			Msg("[expert] spawn_expert cannot locate expert")
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert '%s' is not installed (installed: %s)", expert, shown)}
	}
	manifest := location.Path + "/" + expertManifestName
	template, loadErr := resources.LoadAgentTemplatePackage(manifest)
	if loadErr != nil {
		logger.Warn(toolExpertLogComponent).
			Str("expert", expert).
			Str("path", location.Path).
			Msg("[expert] spawn_expert cannot load expert")
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert '%s' cannot be loaded: %v", expert, loadErr)}
	}
	card, cardErr := resources.LoadAgentTemplateCard(manifest)
	if cardErr != nil {
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert '%s' cannot be loaded: %v", expert, cardErr)}
	}
	return r.fromPackage(experts.ExpertRef{Name: expert}, template, card)
}

// groupMemberTemplate 组成员模板加载。
// Python: _group_member_template (:800-826)
func (r *ExpertTemplateResolver) groupMemberTemplate(expert, group string) (*ExpertTemplate, error) {
	location, err := r.catalog.LocateGroup(group)
	if err != nil {
		installed := []string{}
		for _, item := range r.catalog.ListGroupPackages() {
			installed = append(installed, item.Name)
		}
		shown := strings.Join(installed, ", ")
		if shown == "" {
			shown = "<none>"
		}
		logger.Warn(toolExpertLogComponent).
			Str("group", group).
			Msg("[expert] spawn_expert cannot locate expert group")
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert group '%s' is not installed (installed: %s)", group, shown)}
	}
	packageBundle, loadErr := experts.LoadAgentGroupPackageBundle(location.Path)
	if loadErr != nil {
		logger.Warn(toolExpertLogComponent).
			Str("group", group).
			Str("path", location.Path).
			Msg("[expert] spawn_expert cannot load expert group")
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert group '%s' cannot be loaded: %v", group, loadErr)}
	}
	template, ok := packageBundle.Templates[expert]
	if !ok || expert == experts.LeaderMember {
		members := []string{}
		for name := range packageBundle.Templates {
			if name != experts.LeaderMember {
				members = append(members, name)
			}
		}
		sort.Strings(members)
		return nil, &expertUnavailable{msg: fmt.Sprintf("'%s' is not a member of expert group '%s' (members: %s)", expert, group, strings.Join(members, ", "))}
	}
	card, cardErr := resources.LoadAgentTemplateCard(packageBundle.PackageDir + "/agents/" + expert + "/" + expertManifestName)
	if cardErr != nil {
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert '%s/%s' cannot be loaded: %v", group, expert, cardErr)}
	}
	return r.fromPackage(experts.ExpertRef{Name: expert, Group: group}, template, card)
}

// fromPackage 从包组装 ExpertTemplate（external 翻译 + persona 渲染 + 快照）。
// Python: _from_package (:828-845)
func (r *ExpertTemplateResolver) fromPackage(ref experts.ExpertRef, template hschema.AgentTemplateSpec, card resources.AgentTemplateCard) (*ExpertTemplate, error) {
	externalCLI, extErr := seamExternalCliAgentSpecFromTemplate(&template)
	if extErr != nil {
		return nil, &expertUnavailable{msg: fmt.Sprintf("Expert '%s' has an unsupported runtime: %v", ref.Label(), extErr)}
	}
	displayName := orStr(card.DisplayLabel(r.language), orStr(template.AgentCard.BaseCard.Name, ref.Name))
	desc := orStr(card.Description, template.AgentCard.BaseCard.Description)
	var snapshot *hschema.AgentTemplateSpec
	if externalCLI == nil {
		snapshot = experts.TemplateSnapshot(template)
	}
	return &ExpertTemplate{
		Ref:         ref,
		Version:     card.Version,
		DisplayName: displayName,
		Desc:        desc,
		Persona:     experts.RenderTemplatePersona(template, r.personaLanguage, r.personaWorkspace),
		Snapshot:    snapshot,
		ExternalCLI: externalCLI,
	}, nil
}

// UniqueDisplayName 模板展示名；teammate 已占用时缀成员名。
// Python: unique_display_name (:847-863)
func (r *ExpertTemplateResolver) UniqueDisplayName(team *TeamBackend, displayName, memberName string, alsoTaken map[string]bool) string {
	taken := map[string]bool{}
	members, listErr := team.ListMembers(context.Background())
	if listErr == nil {
		for _, entry := range members {
			taken[entry.DisplayName] = true
		}
	}
	for name := range alsoTaken {
		taken[name] = true
	}
	if !taken[displayName] {
		return displayName
	}
	suffix := fmt.Sprintf(" (%s)", memberName)
	if r.language == "cn" {
		suffix = fmt.Sprintf("（%s）", memberName)
	}
	return displayName + suffix
}

// ──────────────────────────── 记录/文本助手（:867-892） ────────────────────────────

// ExpertMemberTexts 成员 desc/prompt：模板自身 + leader 追加（永不替换）。
// Python: expert_member_texts (:867-872)
func ExpertMemberTexts(template *ExpertTemplate, desc, prompt string) (string, string) {
	join := func(parts ...string) string {
		out := []string{}
		for _, part := range parts {
			if part != "" {
				out = append(out, part)
			}
		}
		return strings.Join(out, "\n\n")
	}
	return join(template.Desc, desc), join(template.Persona, prompt)
}

// ExpertMemberRecord 模板孵出成员的 options.agent_template 记录。
// Python: expert_member_record (:875-879)
func ExpertMemberRecord(template *ExpertTemplate) *database.MemberAgentTemplate {
	ref := map[string]any{}
	for k, v := range template.Ref.ToDict() {
		ref[k] = v
	}
	record := &database.MemberAgentTemplate{Ref: ref, Version: template.Version}
	if template.Snapshot != nil {
		record.Snapshot = structToMap(template.Snapshot)
	}
	if template.ExternalCLI != nil {
		record.ExternalCLI = structToMap(template.ExternalCLI)
	}
	return record
}

// structToMap 结构体 → JSON map（pydantic model_dump(mode="json") 语义）。
func structToMap(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// ExpertMemberCLIRuntime 外部 CLI 成员的启动配置：模板的叠加在声明 kind 的之上。
// kind 未声明时 ok=false；注册随后会拒绝。
// Python: expert_member_cli_config (:882-892)
func ExpertMemberCLIRuntime(team *TeamBackend, template *ExpertTemplate) (*atschema.ExternalCliAgentSpec, bool) {
	if template.ExternalCLI == nil {
		return nil, false
	}
	declared := team.ExternalCLIConfig(template.ExternalCLI.CLIAgent)
	if declared == nil {
		return nil, false
	}
	merged, err := seamMergeExternalCliConfig(declared, template.ExternalCLI)
	if err != nil {
		return nil, false
	}
	return merged, true
}

// ──────────────────────────── SpawnExpertTool（:895-1097） ────────────────────────────

// spawnExpertTool spawn_expert：注册以已安装专家模板运行的成员
// （role_type teammate / external_cli）。模板来自团队绑定 AgentGroup（group
// 命中时零磁盘访问，快照与 persona 同预定义成员）或 catalog + core 加载器。
// 成员行把 persona 存 prompt、能力快照（或外部 CLI 启动配置）存
// options.agent_template，仅凭行即可重建。desc/prompt 只追加不替换。
type spawnExpertTool struct {
	TeamTool
	team                *TeamBackend
	t                   locales.Translator
	resolver            *ExpertTemplateResolver
	allocateModelConfig func(modelName string, opts ...models.AllocateOption) *models.Allocation
}

// newSpawnExpertTool 构造 spawn_expert。
// Python: __init__ (:906-958)
func newSpawnExpertTool(team *TeamBackend, t locales.Translator, catalog experts.ExpertCatalog, language string, boundGroup *experts.BoundAgentGroup, personaWorkspace, personaLanguage string, modelConfigAllocator func(modelName string, opts ...models.AllocateOption) *models.Allocation, resolver *ExpertTemplateResolver) *spawnExpertTool {
	if resolver == nil {
		resolver = NewExpertTemplateResolver(catalog, language, boundGroup, personaWorkspace, personaLanguage)
	}
	externalCLIProperties := ExternalCliOptionProperties(team, t, t("spawn_expert", "external_cli.builtin_model"))
	modelDescription := t("spawn_expert", "model_name")
	if team.IsModelGroupPool() {
		// 模型组内 native 专家收 route tag；列出可选集。
		modelDescription += "\n\n" + ModelNameDescription(team, t)
	}
	card := tool.ToolCard{BaseCard: schema.BaseCard{
		ID: "team.spawn_expert", Name: "spawn_expert", Description: t("spawn_expert"),
	}}
	card.Properties = map[string]any{
		"expert":       map[string]any{"type": "string", "description": t("spawn_expert", "expert")},
		"group":        map[string]any{"type": "string", "description": t("spawn_expert", "group")},
		"member_name":  map[string]any{"type": "string", "description": t("spawn_expert", "member_name")},
		"display_name": map[string]any{"type": "string", "description": t("spawn_expert", "display_name")},
		"desc":         map[string]any{"type": "string", "description": t("spawn_expert", "desc")},
		"prompt":       map[string]any{"type": "string", "description": t("spawn_expert", "prompt")},
		"model_name":   map[string]any{"type": "string", "description": modelDescription},
		"external_cli": map[string]any{
			"type": "object", "description": t("spawn_expert", "external_cli"), "properties": externalCLIProperties,
		},
		"isolation":   map[string]any{"type": "string", "enum": []string{"worktree"}, "description": t("spawn_expert", "isolation")},
		"permissions": map[string]any{"type": "object", "description": t("spawn_expert", "permissions")},
	}
	return &spawnExpertTool{
		TeamTool:            NewTeamTool(&card),
		team:                team,
		t:                   t,
		resolver:            resolver,
		allocateModelConfig: modelConfigAllocator,
	}
}

// Invoke 执行 spawn_expert。
// Python: invoke (:960-996)
func (s *spawnExpertTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	if err := ValidateMemberName(memberName); err != "" {
		return toolError(err)
	}
	expert := OptionalText(inputs["expert"])
	if expert == "" {
		return toolError("'expert' is required: an installed expert package name, or a member name with 'group'")
	}
	group := OptionalText(inputs["group"])
	externalArgs, hasExternalArgs := inputs["external_cli"].(map[string]any)
	if _, present := inputs["external_cli"]; present && inputs["external_cli"] != nil && !hasExternalArgs {
		return toolError("'external_cli' must be an object")
	}
	template, resolveErr := s.resolver.Resolve(expert, group)
	var unavailable *expertUnavailable
	if resolveErr != nil {
		if asUnavailable(resolveErr, &unavailable) {
			return toolError(unavailable.msg)
		}
		logger.Error(toolExpertLogComponent).Err(resolveErr).Msg("spawn_expert failed")
		return toolError(fmt.Sprintf("Internal error: %v", resolveErr))
	}
	if template.ExternalCLI == nil && hasExternalArgs && len(externalArgs) > 0 {
		return toolError(fmt.Sprintf("Expert '%s' runs natively; 'external_cli' applies only to experts with an "+
			"external runtime — omit it", template.Ref.Label()))
	}

	displayName := OptionalText(inputs["display_name"])
	if displayName == "" {
		displayName = s.resolver.UniqueDisplayName(s.team, template.DisplayName, memberName, nil)
	}
	descInput, _ := inputs["desc"].(string)
	promptInput, _ := inputs["prompt"].(string)
	desc, prompt := ExpertMemberTexts(template, descInput, promptInput)
	identity := expertMemberIdentity{memberName: memberName, displayName: displayName, desc: desc, prompt: prompt}
	if template.ExternalCLI != nil {
		return s.spawnExternal(ctx, template, inputs, externalArgs, identity)
	}
	return s.spawnNative(ctx, template, inputs, identity)
}

// spawnNative native 专家孵化。
// Python: _spawn_native (:998-1020)
func (s *spawnExpertTool) spawnNative(ctx context.Context, template *ExpertTemplate, inputs map[string]any, identity expertMemberIdentity) (map[string]any, error) {
	modelName := OptionalText(inputs["model_name"])
	var allocation *models.Allocation
	if s.allocateModelConfig != nil {
		allocation = s.allocateModelConfig(modelName)
	}
	if err := CheckModelSelectable(s.team, modelName, allocation); err != "" {
		return toolError(err)
	}
	isolation, _ := inputs["isolation"].(string)
	permissionsOverride, _ := inputs["permissions"].(map[string]string)
	result := RegisterTeammate(ctx, s.team, RegisterTeammateInput{
		MemberName:          identity.memberName,
		DisplayName:         identity.displayName,
		Desc:                identity.desc,
		Prompt:              identity.prompt,
		Allocation:          allocation,
		Isolation:           isolation,
		PermissionsOverride: permissionsOverride,
		AgentTemplate:       ExpertMemberRecord(template),
	})
	return s.registered(result, template, identity.memberName, identity.displayName, "teammate", "")
}

// spawnExternal 外部 CLI 专家孵化。
// Python: _spawn_external (:1022-1059)
func (s *spawnExpertTool) spawnExternal(ctx context.Context, template *ExpertTemplate, inputs map[string]any, externalArgs map[string]any, identity expertMemberIdentity) (map[string]any, error) {
	unsupported := []string{}
	for _, key := range []string{"isolation", "permissions"} {
		if v, ok := inputs[key]; ok && v != nil {
			unsupported = append(unsupported, key)
		}
	}
	if len(unsupported) > 0 {
		return toolError(fmt.Sprintf("%s cannot be set for expert '%s': it runs on an "+
			"external CLI runtime", strings.Join(unsupported, ", "), template.Ref.Label()))
	}
	cliAgent := template.ExternalCLI.CLIAgent
	fallbackModelName, _ := externalArgs["fallback_model_name"].(string)
	builtinModel, _ := externalArgs["builtin_model"].(string)
	effort, _ := externalArgs["effort"].(string)
	modelName := OptionalText(inputs["model_name"])
	memberConfig, _ := ExpertMemberCLIRuntime(s.team, template)
	result := RegisterExternalCLIMember(ctx, s.team, RegisterExternalCLIMemberInput{
		ToolName:            "spawn_expert",
		AllocateModelConfig: s.allocateModelConfig,
		CLIAgent:            cliAgent,
		MemberName:          identity.memberName,
		DisplayName:         identity.displayName,
		Desc:                identity.desc,
		Prompt:              identity.prompt,
		ModelName:           modelName,
		FallbackModelName:   fallbackModelName,
		BuiltinModel:        builtinModel,
		Effort:              effort,
		AgentTemplate:       withMemberConfig(ExpertMemberRecord(template), memberConfig),
	})
	return s.registered(result, template, identity.memberName, identity.displayName, "external_cli", cliAgent)
}

// withMemberConfig 把成员实际启动配置写进 agent_template.external_cli
// （Python team.py:3255 区段 _with_member_config）。
func withMemberConfig(record *database.MemberAgentTemplate, memberConfig *atschema.ExternalCliAgentSpec) *database.MemberAgentTemplate {
	if memberConfig == nil || record == nil {
		return record
	}
	record.ExternalCLI = structToMap(memberConfig)
	return record
}

// registered 注册结果包装（含模型面日志）。Python: _registered (:1061-1086)
func (s *spawnExpertTool) registered(result atschema.MemberOpResult, template *ExpertTemplate, memberName, displayName, roleType, cliAgent string) (map[string]any, error) {
	if result.OK {
		logger.Info(toolExpertLogComponent).
			Str("member_name", memberName).
			Str("ref", template.Ref.Label()).
			Str("version", orStr(template.Version, "-")).
			Msg("[expert] spawn_expert registered member")
	} else {
		logger.Warn(toolExpertLogComponent).
			Str("member_name", memberName).
			Str("ref", template.Ref.Label()).
			Msg("[expert] spawn_expert rejected member")
	}
	data := map[string]any{
		"member_name":  memberName,
		"display_name": displayName,
		"role_type":    roleType,
		"expert":       template.Ref.Label(),
	}
	if cliAgent != "" {
		data["cli_agent"] = cliAgent
	}
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(data)
}

// MapResult 模型面渲染。Python: render_for_llm (:1088-1097)
func (s *spawnExpertTool) MapResult(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to spawn expert"
	}
	cliAgent, _ := output["cli_agent"].(string)
	suffix := ""
	if cliAgent != "" {
		suffix = " cli_agent=" + cliAgent
	}
	return fmt.Sprintf("Expert member registered: member_name=%v display_name=%v expert=%v role=%v%s",
		output["member_name"], output["display_name"], output["expert"], output["role_type"], suffix)
}

// asUnavailable 错误是否为专家不可用（模型面消息直传）。
func asUnavailable(err error, target **expertUnavailable) bool {
	if e, ok := err.(*expertUnavailable); ok {
		*target = e
		return true
	}
	return false
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (s *spawnExpertTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
