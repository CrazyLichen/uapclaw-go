// Python 源：openjiuwen/agent_teams/tools/tool_expert.py（1110 行 1:1）
//
// 专家浏览与孵化工具（view_experts / view_expert_groups / spawn_expert）。
// 仅在 host 注入 ExpertCatalog（experts.SetProcessExpertCatalog）时注册。
// BM25 检索复用 recall.BM25Index（5.32 落地，与 Python recall/bm25.py 同源）；
// ExpertRef / ExpertCatalog / BoundAgentGroup / AgentGroupCard 由 experts 包
// 提供；MemberAgentTemplate 由 tools/database 包提供。
// async 沿 D-15b 同步化（Go 调用方自行 goroutine 化）。

package tools

import (
	"context"
	"fmt"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/experts"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/context_engine/processor/compressor/recall"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolExpertLogComponent 专家工具组件日志。
	toolExpertLogComponent = logger.ComponentAgentServer

	// expertManifestName 包 manifest 文件名。Python: _MANIFEST (:62)
	expertManifestName = "manifest.json"
	// StandaloneExperts 单专家域的 group 参数值。Python: STANDALONE_EXPERTS (:65)
	StandaloneExperts = "*"
	// boundScopeSlot view_experts 描述槽（scoped 时保留）。Python: _BOUND_SCOPE_SLOT (:68)
	boundScopeSlot = "view_experts_bound_scope"

	// expertNameWeight 名称权重。Python: _NAME_WEIGHT (:71)
	expertNameWeight = 3
	// expertDisplayNameWeight 展示名权重。Python: _DISPLAY_NAME_WEIGHT (:72)
	expertDisplayNameWeight = 3
	// expertTagWeight tags 权重。Python: _TAG_WEIGHT (:73)
	expertTagWeight = 2
	// expertTextWeight 正文权重。Python: _TEXT_WEIGHT (:74)
	expertTextWeight = 1
)

// ──────────────────────────── 非导出函数（检索文档，:77-180） ────────────────────────────

// expertFirstLine 多行文本的首行（模型目录一行一条）。
// Python: _first_line (:77-80)
func expertFirstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

// expertSearchDocument 按权重重复拼接各字段文本——BM25 词频随重复增长，
// 字段权重即重复次数。
// Python: _search_document (:93-105)
func expertSearchDocument(weightedFields []struct {
	value  string
	weight int
}) string {
	parts := []string{}
	for _, field := range weightedFields {
		if field.value != "" {
			for i := 0; i < field.weight; i++ {
				parts = append(parts, field.value)
			}
		}
	}
	return strings.Join(parts, " ")
}

// expertDocument 单专家的加权搜索文档。
// Python: _expert_document (:107-119)
func expertDocument(name string, card resources.AgentTemplateCard) string {
	return expertSearchDocument([]struct {
		value  string
		weight int
	}{
		{name, expertNameWeight},
		{localizedValuesText(card.DisplayName), expertDisplayNameWeight},
		{tagsAsText(card.Tags), expertTagWeight},
		{card.Category, expertTextWeight},
		{card.Description, expertTextWeight},
		{card.DisplayText("en"), expertTextWeight},
	})
}

// groupDocument 一个 AgentGroup 的加权搜索文档（含成员：组经它包含的专家
// 被检索到；leader 不可 spawn，不贡献）。
// Python: _group_document (:121-148)
func groupDocument(name string, card *experts.AgentGroupCard) string {
	fields := []struct {
		value  string
		weight int
	}{
		{name, expertNameWeight},
		{card.DisplayLabel("en"), expertDisplayNameWeight},
		{tagsAsText(card.Tags), expertTagWeight},
		{strings.Join(card.Capabilities, " "), expertTextWeight},
		{card.Category, expertTextWeight},
		{card.Description, expertTextWeight},
		{card.DisplayText("en"), expertTextWeight},
	}
	for i := range card.Members {
		member := &card.Members[i]
		if member.IsLeader() {
			continue
		}
		fields = append(fields,
			struct {
				value  string
				weight int
			}{member.Name, expertTextWeight},
			struct {
				value  string
				weight int
			}{localizedValuesText(member.Card.DisplayName), expertTextWeight},
			struct {
				value  string
				weight int
			}{member.Card.Description, expertTextWeight},
			struct {
				value  string
				weight int
			}{member.Card.DisplayText("en"), expertTextWeight},
		)
	}
	return expertSearchDocument(fields)
}

// rankByQuery 返回与 query 匹配的条目下标（相关度降序）。
// 匹配：name 精确等值（大小写不敏感）、文档与 query 共享一个 BM25 token、或
// query 是文档的子串——BM25 只匹配整 token，子串规则让 "fin" 能命中
// "finance"。精确名最前，其次 BM25 分数降序（纯子串命中分 0）；平局保持
// catalog 顺序。query 空串保留全部条目。
// Python: _rank_by_query (:150-178)
func rankByQuery(query string, names []string, documents []string) []int {
	if query == "" {
		order := make([]int, len(names))
		for i := range order {
			order[i] = i
		}
		return order
	}
	needle := strings.ToLower(query)
	scores := recall.BuildBM25Index(documents, 1.5, 0.75).Scores(needle)
	type ranked struct {
		notExact bool
		negScore float64
		index    int
	}
	list := []ranked{}
	for index, name := range names {
		exact := strings.ToLower(name) == needle
		if exact || scores[index] > 0 || strings.Contains(strings.ToLower(documents[index]), needle) {
			list = append(list, ranked{notExact: !exact, negScore: -scores[index], index: index})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].notExact != list[j].notExact {
			return !list[i].notExact
		}
		if list[i].negScore != list[j].negScore {
			return list[i].negScore < list[j].negScore
		}
		return list[i].index < list[j].index
	})
	order := make([]int, len(list))
	for i, entry := range list {
		order[i] = entry.index
	}
	return order
}

// runtimeLabel 卡片的 "native" 或外部 runtime provider 名。
// Python: _runtime_label (:181-185)
func runtimeLabel(card resources.AgentTemplateCard) string {
	if card.Runtime == "native" {
		return "native"
	}
	if card.RuntimeProvider != nil && *card.RuntimeProvider != "" {
		return *card.RuntimeProvider
	}
	return "external"
}

// readCards 读全部卡片；读不了的包带 warning 跳过。
// Python: _read_cards (:188-204)（async gather 沿 D-15b 同步化）
func readCards[T any](locations []experts.PackageLocation, reader func(experts.PackageLocation) (T, error), kind string) []struct {
	Location experts.PackageLocation
	Card     T
} {
	cards := []struct {
		Location experts.PackageLocation
		Card     T
	}{}
	for _, location := range locations {
		card, err := reader(location)
		if err != nil {
			logger.Warn(toolExpertLogComponent).
				Str("kind", kind).
				Str("name", location.Name).
				Str("path", location.Path).
				Msg("[expert] skip unreadable package")
			continue
		}
		cards = append(cards, struct {
			Location experts.PackageLocation
			Card     T
		}{location, card})
	}
	return cards
}

// expertCardAt 读单专家卡片。Python: _expert_card (:207-209)
func expertCardAt(location experts.PackageLocation) (resources.AgentTemplateCard, error) {
	return resources.LoadAgentTemplateCard(location.Path + "/" + expertManifestName)
}

// groupCardAt 读组卡片。Python: _group_card (:211-213)
func groupCardAt(location experts.PackageLocation) (*experts.AgentGroupCard, error) {
	return experts.ReadAgentGroupCard(location.Path)
}

// expertDetail 一个专家的全部卡片字段（按 language 渲染）。
// Python: _expert_detail (:215-230)
func expertDetail(name string, card resources.AgentTemplateCard, language string, source string) map[string]any {
	detail := map[string]any{
		"name":                name,
		"display_name":        orStr(card.DisplayLabel(language), name),
		"description":         card.Description,
		"display_description": card.DisplayText(language),
		"category":            card.Category,
		"tags":                card.TagLabels(language),
		"version":             card.Version,
		"runtime":             runtimeLabel(card),
		"skills":              card.Skills,
	}
	if source != "" {
		detail["source"] = source
	}
	return detail
}

// renderExpertDetail 明细的文本渲染。
// Python: _render_expert_detail (:233-245)
func renderExpertDetail(detail map[string]any, header string) string {
	lines := []string{header}
	for _, key := range []string{"name", "display_name", "source", "category", "version", "runtime"} {
		if v := detail[key]; v != nil && v != "" {
			lines = append(lines, fmt.Sprintf("%s: %v", key, v))
		}
	}
	if tags, ok := detail["tags"].([]string); ok && len(tags) > 0 {
		lines = append(lines, "tags: "+strings.Join(tags, ", "))
	}
	if skills, ok := detail["skills"].([]string); ok && len(skills) > 0 {
		lines = append(lines, "skills: "+strings.Join(skills, ", "))
	}
	desc, _ := detail["description"].(string)
	lines = append(lines, "description: "+desc)
	if displayDesc, ok := detail["display_description"].(string); ok && displayDesc != "" && displayDesc != desc {
		lines = append(lines, "display_description: "+displayDesc)
	}
	return strings.Join(lines, "\n")
}

// readInstalledGroup 定位并读一个已安装 AgentGroup 卡片。
// 错误消息即模型面原因。Python: _read_installed_group (:248-267)
func readInstalledGroup(catalog experts.ExpertCatalog, name string) (experts.PackageLocation, *experts.AgentGroupCard, error) {
	location, err := catalog.LocateGroup(name)
	if err != nil {
		installed := []string{}
		for _, item := range catalog.ListGroupPackages() {
			installed = append(installed, item.Name)
		}
		shown := strings.Join(installed, ", ")
		if shown == "" {
			shown = "<none>"
		}
		return location, nil, fmt.Errorf("Expert group '%s' is not installed (installed: %s)", name, shown)
	}
	card, cardErr := groupCardAt(location)
	if cardErr != nil {
		logger.Warn(toolExpertLogComponent).
			Str("name", name).
			Str("path", location.Path).
			Msg("[expert] unreadable expert group")
		return location, nil, fmt.Errorf("Expert group '%s' cannot be read: %v", name, cardErr)
	}
	return location, card, nil
}

// isPredefinedMember group/member 是否本团队绑定 AgentGroup 的预定义成员。
// Python: _is_predefined_member (:270-272)
func isPredefinedMember(group, member string, boundGroup *experts.BoundAgentGroup) bool {
	return boundGroup != nil && boundGroup.Name == group && boundGroup.Member(member) != nil
}

// groupMemberDetail 一个组成员的全部卡片字段 + group/spawnable/predefined 标记。
// Python: _group_member_detail (:275-293)
func groupMemberDetail(location experts.PackageLocation, entry *experts.AgentGroupMemberCard, language string, boundGroup *experts.BoundAgentGroup, declaredCLIKinds map[string]bool) map[string]any {
	detail := expertDetail(entry.Name, entry.Card, language, "")
	detail["group"] = location.Name
	detail["display_name"] = entry.DisplayLabel(language)
	detail["spawnable"] = isSpawnable(entry, declaredCLIKinds)
	detail["predefined"] = isPredefinedMember(location.Name, entry.Name, boundGroup)
	return detail
}

// renderGroupMemberDetail 成员明细文本。
// Python: _render_group_member_detail (:296-304)
func renderGroupMemberDetail(member map[string]any) string {
	group, _ := member["group"].(string)
	name, _ := member["name"].(string)
	text := renderExpertDetail(member, fmt.Sprintf("Expert group member %s/%s:", group, name))
	spawnable, _ := member["spawnable"].(bool)
	text += fmt.Sprintf("\nspawnable: %v", spawnable)
	if predefined, _ := member["predefined"].(bool); predefined {
		text += fmt.Sprintf("\nThis team already has the predefined member '%s'; "+
			"spawn_expert creates another instance of the same expert under a new member_name.", name)
	}
	return text
}

// missingMemberError 成员缺失错误。Python: _missing_member_error (:307-309)
func missingMemberError(name string, location experts.PackageLocation, card *experts.AgentGroupCard) string {
	names := []string{}
	for i := range card.Members {
		names = append(names, card.Members[i].Name)
	}
	return fmt.Sprintf("Member '%s' is not in expert group '%s' (members: %s)", name, location.Name, strings.Join(names, ", "))
}

// isSpawnable spawn_expert 能否跑该成员：native，或团队声明过的外部 runtime。
// Python: _is_spawnable (:312-320)
func isSpawnable(member *experts.AgentGroupMemberCard, declaredCLIKinds map[string]bool) bool {
	if member.IsLeader() {
		return false
	}
	if member.Card.Runtime == "native" {
		return true
	}
	provider := ""
	if member.Card.RuntimeProvider != nil {
		provider = *member.Card.RuntimeProvider
	}
	_, declared := declaredCLIKinds[seamCLIAgentForProvider(provider)]
	return declared
}

// ──────────────────────────── ViewExpertsTool（:323-535） ────────────────────────────

// viewExpertsTool view_experts：列出/检视本机安装的单专家（agent_template 包）。
type viewExpertsTool struct {
	TeamTool
	catalog          experts.ExpertCatalog
	language         string
	boundGroup       *experts.BoundAgentGroup
	declaredCLIKinds map[string]bool
	defaultGroup     string
}

// newViewExpertsTool 构造 view_experts。
// Python: __init__ (:326-376)
func newViewExpertsTool(catalog experts.ExpertCatalog, t locales.Translator, language string, boundGroup *experts.BoundAgentGroup, dispatchMode string, declaredCLIKinds map[string]bool) *viewExpertsTool {
	scoped := dispatchMode == "scheduled" && boundGroup != nil
	description := t("view_experts")
	if !scoped {
		// omit 门控：非 scoped 时塌缩 bound_scope 槽（e15 审查修正）。
		description = locales.RenderDesc("view_experts", atschema.Language(language), map[string]bool{boundScopeSlot: true})
	}
	card := tool.ToolCard{BaseCard: schema.BaseCard{ID: "team.view_experts", Name: "view_experts", Description: description}}
	card.Properties = map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"list", "get"}, "default": "list", "description": t("view_experts", "action")},
		"query":  map[string]any{"type": "string", "description": t("view_experts", "query")},
		"group":  map[string]any{"type": "string", "description": t("view_experts", "group")},
		"name":   map[string]any{"type": "string", "description": t("view_experts", "name")},
	}
	vt := &viewExpertsTool{
		catalog:          catalog,
		language:         language,
		boundGroup:       boundGroup,
		declaredCLIKinds: declaredCLIKinds,
	}
	if scoped {
		vt.defaultGroup = boundGroup.Name
	}
	vt.TeamTool = NewTeamTool(&card)
	return vt
}

// scope 本次调用浏览的 AgentGroup；nil 表示单专家。group="*" 恒为单专家；
// 省略 group 时团队 scoped 到绑定组则回落绑定组。
// Python: _scope (:378-386)
func (v *viewExpertsTool) scope(group string) *string {
	if group == StandaloneExperts {
		return nil
	}
	if group != "" {
		return &group
	}
	if v.defaultGroup != "" {
		return &v.defaultGroup
	}
	return nil
}

// Invoke 执行 view_experts。
// Python: invoke (:388-401)
func (v *viewExpertsTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	action, _ := inputs["action"].(string)
	if action == "" {
		action = "list"
	}
	group := v.scope(OptionalText(inputs["group"]))
	if action == "list" {
		query := OptionalText(inputs["query"])
		if group == nil {
			return v.list(query)
		}
		return v.listGroup(*group, query)
	}
	if action == "get" {
		name := OptionalText(inputs["name"])
		if group == nil {
			return v.get(name)
		}
		return v.getGroupMember(*group, name)
	}
	return toolError(fmt.Sprintf("Unknown action '%s'; expected 'list' or 'get'", action))
}

// listGroup 组成员列表。Python: _list_group (:403-432)
func (v *viewExpertsTool) listGroup(group, query string) (map[string]any, error) {
	location, card, err := readInstalledGroup(v.catalog, group)
	if err != nil {
		return toolError(err.Error())
	}
	members := []experts.AgentGroupMemberCard{}
	for i := range card.Members {
		if !card.Members[i].IsLeader() {
			members = append(members, card.Members[i])
		}
	}
	names := make([]string, len(members))
	docs := make([]string, len(members))
	for i := range members {
		names[i] = members[i].Name
		docs[i] = expertDocument(members[i].Name, members[i].Card)
	}
	order := rankByQuery(query, names, docs)
	items := []map[string]any{}
	for _, index := range order {
		entry := members[index]
		items = append(items, map[string]any{
			"name":         entry.Name,
			"display_name": entry.DisplayLabel(v.language),
			"category":     entry.Card.Category,
			"runtime":      runtimeLabel(entry.Card),
			"description":  expertFirstLine(entry.Card.DisplayText(v.language)),
			"spawnable":    isSpawnable(&entry, v.declaredCLIKinds),
			"predefined":   isPredefinedMember(location.Name, entry.Name, v.boundGroup),
		})
	}
	bound := v.boundGroup != nil && v.boundGroup.Name == location.Name
	return toolSuccess(map[string]any{
		"action": "list", "query": query, "group": location.Name, "bound": bound, "experts": items,
	})
}

// getGroupMember 组成员明细。Python: _get_group_member (:434-454)
func (v *viewExpertsTool) getGroupMember(group, name string) (map[string]any, error) {
	if name == "" {
		return toolError("'name' is required for action 'get'")
	}
	location, card, err := readInstalledGroup(v.catalog, group)
	if err != nil {
		return toolError(err.Error())
	}
	entry := card.Member(name)
	if entry == nil {
		errorMsg := missingMemberError(name, location, card)
		if group == v.defaultGroup {
			errorMsg += fmt.Sprintf("; pass group='%s' to view a single expert outside the group", StandaloneExperts)
		}
		return toolError(errorMsg)
	}
	detail := groupMemberDetail(location, entry, v.language, v.boundGroup, v.declaredCLIKinds)
	return toolSuccess(map[string]any{"action": "get", "member": detail})
}

// list 单专家列表。Python: _list (:456-475)
func (v *viewExpertsTool) list(query string) (map[string]any, error) {
	cards := readCards(v.catalog.ListExpertPackages(), expertCardAt, "expert")
	names := make([]string, len(cards))
	docs := make([]string, len(cards))
	for i := range cards {
		names[i] = cards[i].Location.Name
		docs[i] = expertDocument(cards[i].Location.Name, cards[i].Card)
	}
	order := rankByQuery(query, names, docs)
	items := []map[string]any{}
	for _, index := range order {
		location, card := cards[index].Location, cards[index].Card
		items = append(items, map[string]any{
			"name":         location.Name,
			"display_name": orStr(card.DisplayLabel(v.language), location.Name),
			"category":     card.Category,
			"runtime":      runtimeLabel(card),
			"description":  expertFirstLine(card.DisplayText(v.language)),
		})
	}
	return toolSuccess(map[string]any{"action": "list", "query": query, "group": nil, "experts": items})
}

// get 单专家明细。Python: _get (:477-494)
func (v *viewExpertsTool) get(name string) (map[string]any, error) {
	if name == "" {
		return toolError("'name' is required for action 'get'")
	}
	location, err := v.catalog.LocateExpert(name)
	if err != nil {
		installed := []string{}
		for _, item := range v.catalog.ListExpertPackages() {
			installed = append(installed, item.Name)
		}
		shown := strings.Join(installed, ", ")
		if shown == "" {
			shown = "<none>"
		}
		return toolError(fmt.Sprintf("Expert '%s' is not installed (installed: %s)", name, shown))
	}
	card, cardErr := expertCardAt(location)
	if cardErr != nil {
		logger.Warn(toolExpertLogComponent).
			Str("name", name).
			Str("path", location.Path).
			Msg("[expert] unreadable expert")
		return toolError(fmt.Sprintf("Expert '%s' cannot be read: %v", name, cardErr))
	}
	detail := expertDetail(location.Name, card, v.language, location.Source)
	return toolSuccess(map[string]any{"action": "get", "expert": detail})
}

// MapResult 模型面渲染。
// Python: render_for_llm (:496-535)
func (v *viewExpertsTool) MapResult(output map[string]any) string {
	if success, ok := output["success"].(bool); !ok || !success {
		if err, ok := output["error"].(string); ok && err != "" {
			return err
		}
		return "Failed to view experts"
	}
	action, _ := output["action"].(string)
	if action == "get" {
		if member, ok := output["member"].(map[string]any); ok {
			return renderGroupMemberDetail(member)
		}
		if expert, ok := output["expert"].(map[string]any); ok {
			return renderExpertDetail(expert, "Expert:")
		}
	}
	if group, ok := output["group"].(string); ok && group != "" {
		return v.renderGroupList(output)
	}
	expertsList, _ := output["experts"].([]map[string]any)
	query, _ := output["query"].(string)
	if len(expertsList) == 0 {
		if query != "" {
			return fmt.Sprintf("No installed expert matches '%s'", query)
		}
		return "No experts installed"
	}
	lines := []string{}
	for _, item := range expertsList {
		category, _ := item["category"].(string)
		if category == "" {
			category = "-"
		}
		lines = append(lines, fmt.Sprintf("name=%v display_name=%v category=%s runtime=%v — %v",
			item["name"], item["display_name"], category, item["runtime"], item["description"]))
	}
	return strings.Join(lines, "\n")
}

// renderGroupList 组列表渲染（组头 + spawn 提示 + 每成员一行）。
// Python: _render_group_list (:515-535)
func (v *viewExpertsTool) renderGroupList(data map[string]any) string {
	group, _ := data["group"].(string)
	expertsList, _ := data["experts"].([]map[string]any)
	query, _ := data["query"].(string)
	if len(expertsList) == 0 {
		if query != "" {
			return fmt.Sprintf("No expert in expert group '%s' matches '%s'", group, query)
		}
		return fmt.Sprintf("Expert group '%s' has no spawnable members", group)
	}
	bound, _ := data["bound"].(bool)
	boundNote := ""
	if bound {
		boundNote = " (the group this team was assembled from)"
	}
	lines := []string{
		fmt.Sprintf("Experts in expert group '%s'%s; spawn one with spawn_expert(expert=<name>, group='%s'):", group, boundNote, group),
	}
	for _, item := range expertsList {
		note := ""
		if predefined, _ := item["predefined"].(bool); predefined {
			note = " predefined=true"
		}
		category, _ := item["category"].(string)
		if category == "" {
			category = "-"
		}
		spawnable, _ := item["spawnable"].(bool)
		lines = append(lines, fmt.Sprintf("name=%v display_name=%v category=%s runtime=%v spawnable=%v%s — %v",
			item["name"], item["display_name"], category, item["runtime"], spawnable, note, item["description"]))
	}
	return strings.Join(lines, "\n")
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (v *viewExpertsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// orStr 首个非空（Python `a or b`）。
func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// localizedValuesText 卡片字段全部语言的文本拼接（str → [str]；map → 各语言值）。
// Python: _localized_values (tool_expert.py:82-90)
func localizedValuesText(value resources.LocalizedText) string {
	if value.Map != nil {
		texts := []string{}
		for _, text := range value.Map {
			texts = append(texts, text)
		}
		sort.Strings(texts)
		return strings.Join(texts, " ")
	}
	return value.Str
}

// tagsAsText 组卡 tags 的文本拼接。
func tagsAsText(tags []resources.LocalizedText) string {
	texts := []string{}
	for _, tag := range tags {
		texts = append(texts, localizedValuesText(tag))
	}
	return strings.Join(texts, " ")
}
