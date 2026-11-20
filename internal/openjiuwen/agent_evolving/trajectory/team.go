package trajectory

import (
	"fmt"
	"sort"
	"strings"

	obs "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
)

// team.go 团队轨迹 span 的选择器与森林构建。
// 对齐 Python: agent_evolving/trajectory/team.py（全文，2026-12-04 快照）。

// ──────────────────────────── 常量 ────────────────────────────

// TeamCategories 团队轨迹词汇的 span 类别闭集。
// Python: team.py TEAM_CATEGORIES。
var TeamCategories = map[string]struct{}{
	"llm": {}, "tool": {}, "team": {}, "agent": {}, "task": {},
	"message": {}, "member": {}, "plan": {}, "event": {},
}

// ContextCategories 协作上下文类别（llm/tool 之外的语境 span）。
// Python: team.py CONTEXT_CATEGORIES。
var ContextCategories = map[string]struct{}{
	"team": {}, "agent": {}, "task": {}, "message": {}, "member": {}, "plan": {}, "event": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SpanCategory 按稳定命名/语义属性归类规范 span。
// Python: team.py span_category。
func SpanCategory(span map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(fmt.Sprint(span["name"])))
	attrs := SpanAttributes(span)
	recordKind, _ := attrs[obs.OJTrajectoryRecordKind].(string)
	// reasoning 语义优先于命名：reasoning span 也以 llm. 前缀命名，但它是
	// 父 span 输出的复述，只会消耗窗口的 span 名额。
	if recordKind == "reasoning" || name == "llm.reasoning" || strings.HasPrefix(name, "llm.reasoning.") {
		return ""
	}
	if recordKind == "event" {
		return "event"
	}
	if name == "llm.call" || strings.HasPrefix(name, "llm.") {
		return "llm"
	}
	if strings.HasPrefix(name, "tool.") || strings.HasPrefix(name, "execute_tool") {
		return "tool"
	}
	operation := strings.ToLower(fmt.Sprint(attrs[obs.GenAIOperationName]))
	switch operation {
	case "chat", "text_completion", "generate_content":
		return "llm"
	case "execute_tool":
		return "tool"
	}
	for _, pair := range []struct {
		category string
		prefixes []string
	}{
		{"team", []string{"team."}},
		{"agent", []string{"agent."}},
		{"task", []string{"task."}},
		{"message", []string{"msg.", "message."}},
		{"member", []string{"member."}},
		{"plan", []string{"plan."}},
		{"event", []string{"event."}},
	} {
		for _, prefix := range pair.prefixes {
			if strings.HasPrefix(name, prefix) {
				return pair.category
			}
		}
	}
	if attrs[obs.ATEventType] != nil {
		return "event"
	}
	if attrs[obs.ATTaskID] != nil {
		return "task"
	}
	if attrs[obs.ATMemberName] != nil {
		return "member"
	}
	return ""
}

// IsTeamSpan span 是否以团队轨迹词汇声明。
// Python: team.py is_team_span。
func IsTeamSpan(span map[string]any) bool {
	category := SpanCategory(span)
	if category == "" {
		return false
	}
	_, exists := TeamCategories[category]
	return exists
}

// normalizeCategories 归一化类别集合；"team" 是 processor 对
// team/plan/event 语境 span 的速记，展开之。
// Python: team.py _normalise_categories。
func normalizeCategories(categories []string) map[string]struct{} {
	if categories == nil {
		result := make(map[string]struct{}, len(TeamCategories))
		for category := range TeamCategories {
			result[category] = struct{}{}
		}
		return result
	}
	values := map[string]struct{}{}
	for _, category := range categories {
		values[strings.ToLower(category)] = struct{}{}
	}
	if _, hasTeam := values["team"]; hasTeam {
		values["team"] = struct{}{}
		values["plan"] = struct{}{}
		values["event"] = struct{}{}
	}
	return values
}

// SelectTeamSpans 以脱离字典选择团队词汇 span。
// categories=nil 选择 LLM/tool 加全部协作语境 span；llm.reasoning 恒被
// span_category 排除。团队/成员语境不刻意复制到每个 LLM/tool span 上：
// 团队选择器按匹配语境 span 的原生 trace ID 路由，再对树应用类别过滤。
// Python: team.py select_team_spans。
func SelectTeamSpans(value any, categories []string, teamID string, traceID string) []map[string]any {
	wanted := normalizeCategories(categories)
	allSpans := IterSpans(value)
	var routedTraceIDs map[string]struct{}
	if traceID != "" {
		routedTraceIDs = map[string]struct{}{traceID: {}}
	} else if teamID != "" {
		routedTraceIDs = map[string]struct{}{}
		for _, span := range allSpans {
			if fmt.Sprint(SpanAttributes(span)[TeamID]) == teamID {
				if trace := fmt.Sprint(span["traceId"]); trace != "" && trace != "<nil>" {
					routedTraceIDs[trace] = struct{}{}
				}
			}
		}
	}
	if teamID != "" && len(routedTraceIDs) == 0 {
		return []map[string]any{}
	}
	var result []map[string]any
	for _, span := range allSpans {
		category := SpanCategory(span)
		if _, wantedCategory := wanted[category]; !wantedCategory || category == "" {
			continue
		}
		if routedTraceIDs != nil {
			trace := fmt.Sprint(span["traceId"])
			if _, routed := routedTraceIDs[trace]; !routed {
				continue
			}
		}
		result = append(result, span)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(result[i]), SpanSortKeyOf(result[j])) < 0
	})
	return result
}

// IterTeamSpans select_team_spans 的迭代形态。
// Python: team.py iter_team_spans。
func IterTeamSpans(value any, categories []string, teamID string, traceID string) []map[string]any {
	return SelectTeamSpans(value, categories, teamID, traceID)
}

// ForestNode 团队森林节点：{"span", "children"}。
// Python: team.py build_team_forest 的 node 字典。
type ForestNode struct {
	Span     map[string]any
	Children []*ForestNode
}

// nodeKey 节点键：身份优先，匿名 span 用索引占位。
// Python: team.py _node_key。
func nodeKey(span map[string]any, index int) string {
	identity := SpanIdentityOf(span)
	if identity != nil {
		return identity.TraceID + "/" + identity.SpanID
	}
	return fmt.Sprintf("__anonymous__/%d", index)
}

// parentIdentity 读 span 的父身份。
// Python: team.py _parent_identity。
func parentIdentity(span map[string]any) *SpanIdentity {
	traceText := idText(span["traceId"])
	parentText := idText(span["parentSpanId"])
	if traceText == "" || parentText == "" {
		return nil
	}
	return &SpanIdentity{TraceID: traceText, SpanID: parentText}
}

// BuildTeamForest 用原生父身份构建脱离的团队 span 森林。
// 缺失父 span 的节点为根；不创建合成 Team/root span。
// Python: team.py build_team_forest。
func BuildTeamForest(value any, categories []string, teamID string, traceID string) []*ForestNode {
	spans := SelectTeamSpans(value, categories, teamID, traceID)
	nodes := map[string]*ForestNode{}
	orderedKeys := make([]string, 0, len(spans))
	for index, span := range spans {
		key := nodeKey(span, index)
		// 选择器产物本应脱离。身份重复（非匿名）忽略；匿名 span 保留为独立节点。
		if _, exists := nodes[key]; exists && SpanIdentityOf(span) != nil {
			continue
		}
		nodes[key] = &ForestNode{Span: span}
		orderedKeys = append(orderedKeys, key)
	}
	var roots []*ForestNode
	for _, key := range orderedKeys {
		node := nodes[key]
		parentKey := parentIdentity(node.Span)
		var parent *ForestNode
		if parentKey != nil {
			parent = nodes[parentKey.TraceID+"/"+parentKey.SpanID]
		}
		if parent == nil || parent == node {
			roots = append(roots, node)
		} else {
			parent.Children = append(parent.Children, node)
		}
	}
	sortForestNodes(roots)
	return roots
}

// sortForestNodes 森林节点按稳定键排序（递归）。
// Python: team.py build_team_forest 内部 sort_nodes。
func sortForestNodes(items []*ForestNode) {
	sort.SliceStable(items, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(items[i].Span), SpanSortKeyOf(items[j].Span)) < 0
	})
	for _, item := range items {
		sortForestNodes(item.Children)
	}
}

// TeamForest build_team_forest 别名。
// Python: team.py team_forest。
func TeamForest(value any, categories []string, teamID string, traceID string) []*ForestNode {
	return BuildTeamForest(value, categories, teamID, traceID)
}

// FlattenForest 把森林展平为按时间序的脱离 span 列表。
// Python: team.py flatten_forest。
func FlattenForest(forest []*ForestNode) []map[string]any {
	var result []map[string]any
	var visit func(node *ForestNode)
	visit = func(node *ForestNode) {
		if node.Span != nil {
			result = append(result, node.Span)
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	for _, root := range forest {
		visit(root)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(result[i]), SpanSortKeyOf(result[j])) < 0
	})
	return result
}

// memberValue 读 span 的成员归属。
// Python: team.py _member_value。
func memberValue(span map[string]any) string {
	attrs := SpanAttributes(span)
	for _, key := range []string{obs.ATMemberName, obs.ATAgentID, obs.ATMemberName} {
		value := attrs[key]
		if value != nil && fmt.Sprint(value) != "" && fmt.Sprint(value) != "<nil>" {
			return fmt.Sprint(value)
		}
	}
	return ""
}

// indexSpans 按身份索引 span。
// Python: team.py _index_spans。
func indexSpans(spans []map[string]any) map[SpanIdentity]map[string]any {
	byIdentity := map[SpanIdentity]map[string]any{}
	for _, span := range spans {
		identity := SpanIdentityOf(span)
		if identity != nil {
			byIdentity[*identity] = span
		}
	}
	return byIdentity
}

// descendantKeys 从目标身份沿父子指针收集全部后代身份。
// Python: team.py _descendant_keys。
func descendantKeys(spans []map[string]any, targetKeys map[SpanIdentity]struct{}) map[SpanIdentity]struct{} {
	byIdentity := indexSpans(spans)
	children := map[SpanIdentity][]SpanIdentity{}
	for identity, span := range byIdentity {
		parent := parentIdentity(span)
		if parent != nil {
			children[*parent] = append(children[*parent], identity)
		}
	}
	result := map[SpanIdentity]struct{}{}
	for key := range targetKeys {
		result[key] = struct{}{}
	}
	pending := make([]SpanIdentity, 0, len(targetKeys))
	for key := range targetKeys {
		pending = append(pending, key)
	}
	for len(pending) > 0 {
		parent := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, child := range children[parent] {
			if _, exists := result[child]; !exists {
				result[child] = struct{}{}
				pending = append(pending, child)
			}
		}
	}
	return result
}

// SelectMemberSpans 选择一个成员的 span，默认含其子 LLM/tool span。
// Python: team.py select_member_spans。
func SelectMemberSpans(value any, memberID string, teamID string, categories []string, includeDescendants, includeAncestors bool) []map[string]any {
	spans := SelectTeamSpans(value, categories, teamID, "")
	target := memberID
	targetKeys := map[SpanIdentity]struct{}{}
	for _, span := range spans {
		if memberValue(span) != target {
			continue
		}
		if identity := SpanIdentityOf(span); identity != nil {
			targetKeys[*identity] = struct{}{}
		}
	}
	selectedKeys := targetKeys
	if includeDescendants {
		selectedKeys = descendantKeys(spans, selectedKeys)
	}
	if includeAncestors {
		byIdentity := indexSpans(spans)
		for identity := range selectedKeys {
			current := byIdentity[identity]
			for current != nil {
				parent := parentIdentity(current)
				if parent == nil {
					break
				}
				parentSpan, exists := byIdentity[*parent]
				if !exists {
					break
				}
				selectedKeys[*parent] = struct{}{}
				current = parentSpan
			}
		}
	}
	var result []map[string]any
	for _, span := range spans {
		if identity := SpanIdentityOf(span); identity != nil {
			if _, selected := selectedKeys[*identity]; selected {
				result = append(result, span)
			}
		}
	}
	return result
}

// MemberSpans select_member_spans 别名。
// Python: team.py member_spans。
func MemberSpans(value any, memberID string, teamID string, categories []string, includeDescendants, includeAncestors bool) []map[string]any {
	return SelectMemberSpans(value, memberID, teamID, categories, includeDescendants, includeAncestors)
}

// taskValue 读 span 的任务归属。
// Python: team.py _task_value。
func taskValue(span map[string]any) string {
	value := SpanAttributes(span)[obs.ATTaskID]
	if value == nil {
		return ""
	}
	text := fmt.Sprint(value)
	if text == "<nil>" {
		return ""
	}
	return text
}

// SelectTaskSpans 选择任务 span 及其原生后代。
// Python: team.py select_task_spans。
func SelectTaskSpans(value any, taskID string, teamID string, categories []string, includeDescendants bool) []map[string]any {
	spans := SelectTeamSpans(value, categories, teamID, "")
	target := taskID
	targetKeys := map[SpanIdentity]struct{}{}
	for _, span := range spans {
		if taskValue(span) != target {
			continue
		}
		if identity := SpanIdentityOf(span); identity != nil {
			targetKeys[*identity] = struct{}{}
		}
	}
	selectedKeys := targetKeys
	if includeDescendants {
		selectedKeys = descendantKeys(spans, selectedKeys)
	}
	var result []map[string]any
	for _, span := range spans {
		if identity := SpanIdentityOf(span); identity != nil {
			if _, selected := selectedKeys[*identity]; selected {
				result = append(result, span)
			}
		}
	}
	return result
}

// GroupSpansByMember 按成员标签分组 span，不创建成员轨迹拷贝。
// Python: team.py group_spans_by_member。
func GroupSpansByMember(value any, teamID string, categories []string) map[string][]map[string]any {
	groups := map[string][]map[string]any{}
	for _, span := range SelectTeamSpans(value, categories, teamID, "") {
		member := memberValue(span)
		if member != "" {
			groups[member] = append(groups[member], span)
		}
	}
	result := make(map[string][]map[string]any, len(groups))
	members := make([]string, 0, len(groups))
	for member := range groups {
		members = append(members, member)
	}
	sort.Strings(members)
	for _, member := range members {
		spans := groups[member]
		sort.SliceStable(spans, func(i, j int) bool {
			return compareSpanSortKey(SpanSortKeyOf(spans[i]), SpanSortKeyOf(spans[j])) < 0
		})
		result[member] = spans
	}
	return result
}

// MemberIDs 返回团队轨迹中观测到的稳定成员 ID 列表。
// Python: team.py member_ids。
func MemberIDs(value any, teamID string) []string {
	groups := GroupSpansByMember(value, teamID, nil)
	members := make([]string, 0, len(groups))
	for member := range groups {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

// ReadTeamAttributes 只读 span 的规范 agentteam.* 字段。
// Python: team.py read_team_attributes。
func ReadTeamAttributes(span map[string]any) map[string]any {
	attrs := SpanAttributes(span)
	result := map[string]any{}
	for key, value := range attrs {
		if strings.HasPrefix(key, "agentteam.") {
			result[key] = value
		}
	}
	return result
}

// DescendantSpans 返回指定 span 的选中后代（不含自身）。
// Python: team.py descendant_spans。
func DescendantSpans(value any, parent map[string]any, categories []string, teamID string) []map[string]any {
	identity := SpanIdentityOf(parent)
	if identity == nil {
		return []map[string]any{}
	}
	spans := SelectTeamSpans(value, categories, teamID, "")
	selected := descendantKeys(spans, map[SpanIdentity]struct{}{*identity: {}})
	delete(selected, *identity)
	var result []map[string]any
	for _, span := range spans {
		if spanIdentity := SpanIdentityOf(span); spanIdentity != nil {
			if _, exists := selected[*spanIdentity]; exists {
				result = append(result, span)
			}
		}
	}
	return result
}
