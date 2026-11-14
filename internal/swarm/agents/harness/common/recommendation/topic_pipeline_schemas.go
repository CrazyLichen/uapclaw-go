package recommendation

// topic_pipeline_schemas.go 一比一对齐 topic_pipeline.py:186-1031 的
// schema 树与阶段分发。JSON 常量由 AST 求值机器导出（键排序），
// 运行时 json.Unmarshal 还原为与 Python dict 等价的 map 结构。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// GlobalInterestPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const GlobalInterestPromptVersion = "topic-starter-v6-global-interest-v25"

// ProjectContinuationPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const ProjectContinuationPromptVersion = "topic-starter-v6-project-continuation-v15"

// MemoryIncrementalPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const MemoryIncrementalPromptVersion = "topic-starter-v6-memory-incremental-v7"

// RecentPlannerPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const RecentPlannerPromptVersion = "topic-starter-v6-recent-planner-v5"

// RecentFinalizerPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const RecentFinalizerPromptVersion = "topic-starter-v6-recent-finalizer-v2"

// ProjectDomainPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const ProjectDomainPromptVersion = "topic-starter-v6-project-domain-v3"

// HotNewsMatcherPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const HotNewsMatcherPromptVersion = "topic-starter-v6-hot-news-matcher-v5"

// BehaviorMappingPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const BehaviorMappingPromptVersion = "topic-starter-v6-behavior-mapping-v2"

// MemoryInitPromptVersion 阶段提示词版本（topic_pipeline.py:189-198）
const MemoryInitPromptVersion = "topic-starter-v6-memory-init-v1"

// UtilityConfigVersion 效用 API 配置版本（topic_pipeline.py:198）
const UtilityConfigVersion = "topic-starter-v6-utility-api-v41"

// DiversityPairBatchSize / DiversityMarginalBatchSize 多样性批量（:201-202）
const (
	DiversityPairBatchSize     = 5
	DiversityMarginalBatchSize = 12
)

// correctionClassificationRule 语义优先分类规则（topic_history.py:123-130）
const correctionClassificationRule = `action_type 按语义优先分类：明确修订已有目标、偏好或任务状态（包括完成、暂停、恢复、取消与纠正）优先取 correction，即使同一句还提出新目标或请求简短确认。只改变该表达指向的事项，不把无关事项也判为完成或暂停。例如‘报告已经写完，接下来准备演讲’是 correction；‘我想写一份报告’是 explicit_goal_preference_task；‘请解释一下报告的结构’是 natural_question。一般经历叙述或假设中的完成不等于修订已有任务状态。`

// taxonomyTagsJSON 冻结 taxonomy 表 JSON（topic_history.py:86-121，键排序）
const taxonomyTagsJSON = `null`

// taxonomyVersion 分类法版本（topic_history.py:75）
const taxonomyVersion = "v4-conversation-taxonomy-1"

// pipelineStageSchemasJSON 各阶段响应 schema（topic_pipeline.py:660-936，键排序）
const pipelineStageSchemasJSON = `{"rid_interest_distiller":{"additionalProperties":false,"properties":{"decisions":{"items":{"additionalProperties":false,"properties":{"interest_summary_id":{"minLength":1,"type":"string"},"rank":{"anyOf":[{"minimum":1,"type":"integer"},{"type":"null"}]},"resonance_score_nullable":{"type":"null"},"selected":{"type":"boolean"}},"required":["interest_summary_id","selected","rank","resonance_score_nullable"],"type":"object"},"minItems":0,"type":"array"}},"required":["decisions"],"type":"object"},"v6_diversity_scorer":{"additionalProperties":false,"properties":{"marginal_scores":{"items":{"additionalProperties":false,"properties":{"candidate_id":{"minLength":1,"type":"string"},"marginal_diversity_nullable":{"anyOf":[{"maximum":1,"minimum":0,"type":"number"},{"type":"null"}]}},"required":["candidate_id","marginal_diversity_nullable"],"type":"object"},"maxItems":12,"minItems":0,"type":"array"},"pairwise_scores":{"items":{"additionalProperties":false,"properties":{"compatible":{"type":"boolean"},"diversity_score_nullable":{"anyOf":[{"maximum":1,"minimum":0,"type":"number"},{"type":"null"}]},"left_candidate_id":{"minLength":1,"type":"string"},"reason":{"minLength":1,"type":"string"},"right_candidate_id":{"minLength":1,"type":"string"}},"required":["left_candidate_id","right_candidate_id","reason","compatible","diversity_score_nullable"],"type":"object"},"maxItems":5,"minItems":0,"type":"array"}},"required":["pairwise_scores","marginal_scores"],"type":"object"},"v6_evidence_extractor":{"additionalProperties":false,"properties":{"candidate_id":{"minLength":1,"type":"string"},"spans":{"items":{"additionalProperties":false,"properties":{"catalog_entry_id":{"minLength":1,"type":"string"},"document_revision_id":{"minLength":1,"type":"string"}},"required":["document_revision_id","catalog_entry_id"],"type":"object"},"minItems":0,"type":"array"}},"required":["candidate_id","spans"],"type":"object"},"v6_global_interest_generator":{"additionalProperties":false,"properties":{"candidates":{"items":{"additionalProperties":false,"properties":{"blocked_by":{"minLength":1,"type":"string"},"decision":{"enum":["write","skip"],"type":"string"},"direction":{"type":"string"},"intent":{"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"new_angle":{"minLength":1,"type":"string"},"positions_left":{"minimum":0,"type":"integer"},"slot_id":{"minLength":1,"type":"string"},"title":{"type":"string"}},"required":["slot_id","interest_summary_id","positions_left","direction","blocked_by","new_angle","decision","intent","title"],"type":"object"},"minItems":0,"type":"array"}},"required":["candidates"],"type":"object"},"v6_hot_news_matcher":{"additionalProperties":false,"properties":{"direction":{"anyOf":[{"minLength":1,"type":"string"},{"type":"null"}]},"hot_entry_id":{"minLength":1,"type":"string"},"interest_summary_id":{"anyOf":[{"minLength":1,"type":"string"},{"type":"null"}]},"match":{"enum":["matched","closest"],"type":"string"},"news_query":{"minLength":1,"type":"string"}},"required":["hot_entry_id","interest_summary_id","match","news_query","direction"],"type":"object"},"v6_pa_fixed_retriever":{"additionalProperties":false,"properties":{"hits":{"items":{"additionalProperties":false,"properties":{"interest_summary_id":{"minLength":1,"type":"string"},"starter_id":{"minLength":1,"type":"string"}},"required":["starter_id","interest_summary_id"],"type":"object"},"maxItems":8,"minItems":0,"type":"array"}},"required":["hits"],"type":"object"},"v6_project_continuation_generator":{"additionalProperties":false,"properties":{"candidates":{"items":{"additionalProperties":false,"properties":{"blocked_by":{"minLength":1,"type":"string"},"decision":{"enum":["write","skip"],"type":"string"},"intent":{"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"nearest_covered":{"minLength":1,"type":"string"},"nearest_dismissed":{"minLength":1,"type":"string"},"nearest_offered":{"minLength":1,"type":"string"},"new_angle":{"minLength":1,"type":"string"},"slot_id":{"minLength":1,"type":"string"},"source_event_id":{"type":"string"},"title":{"type":"string"}},"required":["slot_id","interest_summary_id","blocked_by","new_angle","nearest_covered","nearest_offered","nearest_dismissed","decision","intent","title","source_event_id"],"type":"object"},"minItems":0,"type":"array"}},"required":["candidates"],"type":"object"},"v6_project_domain_candidate_generator":{"additionalProperties":false,"properties":{"candidates":{"items":{"additionalProperties":false,"properties":{"freshness_class":{"enum":["current_event","recent_development"],"type":"string"},"identity_anchor":{"minLength":1,"type":"string"},"intent":{"minLength":1,"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"primary_query":{"minLength":1,"type":"string"},"slot_id":{"minLength":1,"type":"string"},"title":{"minLength":1,"type":"string"}},"required":["slot_id","interest_summary_id","identity_anchor","intent","title","primary_query","freshness_class"],"type":"object"},"minItems":0,"type":"array"}},"required":["candidates"],"type":"object"},"v6_recent_candidate_generator":{"additionalProperties":false,"properties":{"candidates":{"items":{"additionalProperties":false,"properties":{"direction":{"minLength":1,"type":"string"},"freshness_class":{"enum":["current_event","recent_development"],"type":"string"},"identity_anchor":{"minLength":1,"type":"string"},"intent":{"minLength":1,"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"primary_query":{"minLength":1,"type":"string"},"slot_id":{"minLength":1,"type":"string"},"title":{"minLength":1,"type":"string"}},"required":["slot_id","interest_summary_id","identity_anchor","intent","title","primary_query","freshness_class","direction"],"type":"object"},"maxItems":4,"minItems":0,"type":"array"}},"required":["candidates"],"type":"object"},"v6_recent_finalizer":{"additionalProperties":false,"properties":{"candidates":{"items":{"additionalProperties":false,"properties":{"candidate_id":{"minLength":1,"type":"string"},"title":{"minLength":1,"type":"string"}},"required":["candidate_id","title"],"type":"object"},"minItems":0,"type":"array"}},"required":["candidates"],"type":"object"},"v6_session_summarizer":{"additionalProperties":false,"properties":{"summaries":{"items":{"additionalProperties":false,"properties":{"session_id":{"minLength":1,"type":"string"},"summary":{"minLength":1,"type":"string"}},"required":["session_id","summary"],"type":"object"},"minItems":1,"type":"array"}},"required":["summaries"],"type":"object"},"v6_utility_scorer":{"additionalProperties":false,"properties":{"scores":{"items":{"additionalProperties":false,"properties":{"admitted":{"type":"boolean"},"assertion_spans":{"items":{"minLength":1,"type":"string"},"type":"array"},"candidate_id":{"minLength":1,"type":"string"},"click_score_nullable":{"anyOf":[{"maximum":1,"minimum":0,"type":"number"},{"type":"null"}]},"combined_utility_nullable":{"anyOf":[{"maximum":1,"minimum":0,"type":"number"},{"type":"null"}]},"depth_score_nullable":{"anyOf":[{"maximum":1,"minimum":0,"type":"number"},{"type":"null"}]},"given_conditions":{"items":{"additionalProperties":false,"properties":{"card_quote":{"minLength":1,"type":"string"},"condition":{"minLength":1,"type":"string"},"source":{"enum":["user","assistant_estimate","source_page","stated_assumption","unsupported"],"type":"string"},"source_quote":{"minLength":1,"type":"string"}},"required":["card_quote","condition","source","source_quote"],"type":"object"},"type":"array"},"question_focus":{"minLength":1,"type":"string"},"reason":{"minLength":1,"type":"string"}},"required":["candidate_id","question_focus","assertion_spans","given_conditions","reason","admitted","click_score_nullable","depth_score_nullable","combined_utility_nullable"],"type":"object"},"minItems":1,"type":"array"}},"required":["scores"],"type":"object"}}`

// slotDecisionItemSchemasJSON 槽位决策条目 schema（:938-942）
const slotDecisionItemSchemasJSON = `{"v6_global_interest_generator":{"additionalProperties":false,"properties":{"blocked_by":{"minLength":1,"type":"string"},"decision":{"enum":["write","skip"],"type":"string"},"direction":{"type":"string"},"intent":{"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"new_angle":{"minLength":1,"type":"string"},"positions_left":{"minimum":0,"type":"integer"},"slot_id":{"minLength":1,"type":"string"},"title":{"type":"string"}},"required":["slot_id","interest_summary_id","positions_left","direction","blocked_by","new_angle","decision","intent","title"],"type":"object"},"v6_project_continuation_generator":{"additionalProperties":false,"properties":{"blocked_by":{"minLength":1,"type":"string"},"decision":{"enum":["write","skip"],"type":"string"},"intent":{"type":"string"},"interest_summary_id":{"minLength":1,"type":"string"},"nearest_covered":{"minLength":1,"type":"string"},"nearest_dismissed":{"minLength":1,"type":"string"},"nearest_offered":{"minLength":1,"type":"string"},"new_angle":{"minLength":1,"type":"string"},"slot_id":{"minLength":1,"type":"string"},"source_event_id":{"type":"string"},"title":{"type":"string"}},"required":["slot_id","interest_summary_id","blocked_by","new_angle","nearest_covered","nearest_offered","nearest_dismissed","decision","intent","title","source_event_id"],"type":"object"}}`

// ──────────────────────────── 全局变量 ────────────────────────────

// V6StageResponseSchemas 各阶段响应 schema（运行时还原）
var V6StageResponseSchemas = func() map[string]map[string]any {
	result := map[string]map[string]any{}
	if err := json.Unmarshal([]byte(pipelineStageSchemasJSON), &result); err != nil {
		panic(err)
	}
	return result
}()

// slotDecisionItemSchemas 槽位决策条目 schema（运行时还原）
var slotDecisionItemSchemas = func() map[string]map[string]any {
	result := map[string]map[string]any{}
	if err := json.Unmarshal([]byte(slotDecisionItemSchemasJSON), &result); err != nil {
		panic(err)
	}
	return result
}()

// pipelineMemoryInitSystemPrompt / pipelineMemoryIncrementalSystemPrompt
// 从提示词常量文件引用。
var (
	pipelineMemoryInitSystemPrompt        = pipelineV6MemoryInitSystemPrompt
	pipelineMemoryIncrementalSystemPrompt = pipelineV6MemoryIncrementalSystemPrompt
)

// V6BehaviorMappingSystemPrompt 行为分类器提示词（:466-478，f-string 插值逐字组装）
var V6BehaviorMappingSystemPrompt = "你是 Topic Starter v6 的行为分类器。逐条独立分类输入行为，并且只输出 JSON " +
	"\"mappings\":[...]。每条输入必须恰好对应一项，包含且仅包含 " +
	"behavior_index、tag、action_type、literal_focus_span。behavior_index 必须逐字" +
	"复制输入；tag 必须取冻结 taxonomy 的 id，无法覆盖的明确目标、偏好或持续任务" +
	"可用 provisional:<slug>；action_type 只能取 natural_question、" +
	"explicit_goal_preference_task、follow_up、correction。" +
	correctionClassificationRule +
	"literal_focus_span must be null for every item。\n" +
	"taxonomy(" + taxonomyVersion + "): " + taxonomyTagsJSON

// ──────────────────────────── 导出函数 ────────────────────────────

// V6ResponseSchema 阶段响应 schema 分发（topic_pipeline.py:944-990）
// _v6_response_schema；slot 决策阶段按槽位数固定 min/max，效用阶段按候选
// 数收紧并枚举 candidate_id。
func V6ResponseSchema(stage, payload string) (map[string]any, error) {
	if stage == "behavior_map_batch" {
		return strictArrayResponseFromSchema("mappings", map[string]any{
			"behavior_index": map[string]any{"type": "integer", "minimum": 1},
			"tag":            V6TagSchema,
			"action_type": map[string]any{"type": "string", "enum": []string{
				"natural_question", "explicit_goal_preference_task", "follow_up", "correction"}},
			"literal_focus_span": map[string]any{"type": "null"},
		}, 1, nil), nil
	}
	if stage == "memory_init" {
		return V6StageResponseSchemas["memory_init"], nil
	}
	if stage == "memory_incremental" {
		return V6StageResponseSchemas["memory_incremental"], nil
	}
	if itemSchema, isSlot := slotDecisionItemSchemas[stage]; isSlot {
		// 每个槽恰好一个决策，Ark 按此数答复
		var parsed struct {
			CandidateSlots []map[string]any `json:"candidate_slots"`
		}
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			return nil, err
		}
		slots := len(parsed.CandidateSlots)
		return strictArrayResponseSchema("candidates", itemSchema, slots, &slots), nil
	}
	if stage == "v6_utility_scorer" {
		schema := deepCopyJSON(V6StageResponseSchemas[stage])
		var parsed struct {
			Candidates []struct {
				CandidateID string `json:"candidate_id"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			return nil, err
		}
		count := len(parsed.Candidates)
		scores := mapAnyOf(schema["properties"])["scores"].(map[string]any)
		scores["minItems"] = count
		scores["maxItems"] = count
		items := mapAnyOf(scores["items"])
		properties := mapAnyOf(items["properties"])
		candidateID := mapAnyOf(properties["candidate_id"])
		enums := []any{}
		for _, candidate := range parsed.Candidates {
			enums = append(enums, candidate.CandidateID)
		}
		candidateID["enum"] = enums
		return schema, nil
	}
	if schema, exists := V6StageResponseSchemas[stage]; exists {
		return schema, nil
	}
	if strings.HasPrefix(stage, "v6_") {
		return nil, fmt.Errorf("unknown v6 Ark stage: %s", stage)
	}
	return nil, nil
}

// V6TagSchema 标签 schema（topic_pipeline.py:537-545；taxonomy 枚举运行时注入）
var V6TagSchema = func() map[string]any {
	var tags map[string]any
	if err := json.Unmarshal([]byte(taxonomyTagsJSON), &tags); err != nil {
		panic(err)
	}
	enums := make([]any, 0, len(tags))
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		enums = append(enums, key)
	}
	return map[string]any{"anyOf": []any{
		map[string]any{"type": "string", "enum": enums},
		map[string]any{"type": "string", "pattern": "^provisional:[a-z0-9][a-z0-9-]{0,63}$"},
	}}
}()

// ──────────────────────────── 非导出函数 ────────────────────────────

// strictArrayResponseSchema 收紧数组响应 schema（:455-464）
func strictArrayResponseSchema(fieldName string, itemProperties map[string]any,
	minItems int, maxItems *int) map[string]any {
	arraySchema := map[string]any{
		"type":     "array",
		"items":    strictObjectSchema(itemProperties),
		"minItems": minItems,
	}
	if maxItems != nil {
		arraySchema["maxItems"] = *maxItems
	}
	return strictObjectSchema(map[string]any{fieldName: arraySchema})
}

// strictArrayResponseFromSchema 收紧数组响应 schema（既有 schema 形态入参）
func strictArrayResponseFromSchema(fieldName string, itemProperties map[string]any,
	minItems int, maxItems *int) map[string]any {
	return strictArrayResponseSchema(fieldName, itemProperties, minItems, maxItems)
}

// strictObjectSchema 收紧对象 schema（:451-454）
func strictObjectSchema(properties map[string]any) map[string]any {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	required := make([]any, 0, len(keys))
	for _, key := range keys {
		required = append(required, key)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

// deepCopyJSON 经 JSON 往返深拷贝（对齐 copy.deepcopy 的数据面）
func deepCopyJSON(value map[string]any) map[string]any {
	raw, _ := json.Marshal(value)
	decoded := map[string]any{}
	_ = json.Unmarshal(raw, &decoded)
	return decoded
}

// ──────────────────────────── ⤴️ 31c A 段收尾：日期/血缘/类型（topic_pipeline.py:331-450/:901-1031） ────────────────────────────

// isoPublishedAtOrNone 只保留证据契约可回放的显式发布元数据
// （topic_pipeline.py:331-347）。
func isoPublishedAtOrNone(value any) string {
	text := strings.TrimSpace(pyStr(value))
	if text == "" {
		return ""
	}
	normalized := strings.Replace(text, "Z", "+00:00", 1)
	parsed, err := parseISODate(normalized)
	if err != nil {
		return ""
	}
	// 无时区的时间点按发布者本地钟表；当 UTC 读会偏移数小时，不可用作发布元数据
	if parsed.locationUTC() == false && (strings.Contains(text, "T") || strings.Contains(text, ":")) {
		return ""
	}
	return text
}

// unambiguousSearchPublishedAtOrNone 只归一化绝对、无歧义的结构化搜索日期
// （topic_pipeline.py:348-386）。
func unambiguousSearchPublishedAtOrNone(value any) string {
	text := strings.TrimSpace(pyStr(value))
	if text == "" {
		return ""
	}
	normalized := strings.Replace(text, "Z", "+00:00", 1)
	parsed, err := parseISODate(normalized)
	if err == nil {
		hasTime := strings.Contains(text, "T") || strings.Contains(text, ":")
		if hasTime && parsed.locationUTC() == false {
			return ""
		}
		// ISO：无时区按 UTC 归位，有时区转 UTC；文本不变地原样返回
		return text
	}
	// 结构化日期格式族
	for _, layout := range []string{"January 02, 2006", "Jan 02, 2006", "2006年01月02日"} {
		if t, err := timeParseLayout(text, layout); err == nil {
			return t
		}
	}
	return ""
}

// parseISODate ISO 解析（Z 归一后）。
func parseISODate(text string) (parsedDate, error) {
	layouts := []string{
		"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02", "2006-01-02T15:04:05", "2006-01-02T15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, text); err == nil {
			return parsedDate{utc: t.UTC(), hasTZ: hasTZOffset(layout) || tzInText(text)}, nil
		}
	}
	return parsedDate{}, fmt.Errorf("not iso: %s", text)
}

// hasTZOffset 布局是否带时区占位。
func hasTZOffset(layout string) bool {
	return strings.Contains(layout, "Z07:00")
}

// tzInText 文本是否显式带时区。
func tzInText(text string) bool {
	return strings.HasSuffix(text, "Z") || strings.Contains(text, "+") ||
		strings.Contains(text, "-10:00") || strings.Contains(text, "+08:00") ||
		(len(text) > 5 && (text[len(text)-6] == '+') && text[len(text)-3] == ':')
}

// parsedDate 解析结果。
type parsedDate struct {
	utc   time.Time
	hasTZ bool
}

// locationUTC 是否带时区信息。
func (p parsedDate) locationUTC() bool { return p.hasTZ }

// timeParseLayout 结构化日期解析（匹配返回 ISO 格式化 UTC 串）。
func timeParseLayout(text, layout string) (string, error) {
	t, err := time.Parse(layout, text)
	if err != nil {
		return "", err
	}
	return t.UTC().Format("2006-01-02T15:04:05Z07:00"), nil
}

// PipelineStageInvoker / PipelineStageInvokerFactory / WebSearchFn /
// WebSearchProviderFactory / PipelineContextLoader / PipelineRecordsLoader
// 类型别名（topic_pipeline.py:406-412；StageInvokerFunc 已在 topic_generation）。
type (
	PipelineStageInvoker        = StageInvokerFunc
	PipelineStageInvokerFactory = func(config *ArkBackgroundConfig) *ArkStageInvoker
	WebSearchFn                 = func(ctx context.Context, query string, maxResults, timeoutSeconds int) (string, []map[string]string, error)
	WebSearchProviderFactory    = func(config *ArkBackgroundConfig) WebSearchFn
	PipelineContextLoader       = func(scope string) *V6ContextSnapshot
	PipelineRecordsLoader       = func() map[string][]map[string]any
)

// UtilityBinding 效用组件绑定（topic_pipeline.py:415-419）。
// Python 返回必非 nil（构造参数恒合法）；Go 构造 error 时回 nil 由调用方 fail-closed。
func UtilityBinding(config *ArkBackgroundConfig) *LearnedComponentBinding {
	providerVersion := config.ExpectedModel
	if providerVersion == "" {
		providerVersion = config.Model
	}
	binding, err := NewLearnedComponentBinding(UTILITY_COMPONENT_ID, "ark", providerVersion, UtilityConfigVersion)
	if err != nil {
		return nil
	}
	return binding
}

// UnconfiguredV6Search 未配置搜索的 fail-loud 占位（topic_pipeline.py:443-449）。
func UnconfiguredV6Search(ctx context.Context, query string, maxResults, timeoutSeconds int) (string, []map[string]string, error) {
	return "", nil, fmt.Errorf("v6 web search provider is config-bound at generation time")
}

// LineageIds 提取非空字段 ID 排序去重（topic_pipeline.py:901-910）。
func LineageIds(items any, fieldName string) []string {
	raw, isList := items.([]any)
	if !isList {
		return nil
	}
	seen := map[string]bool{}
	var result []string
	for _, item := range raw {
		m, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		text := strings.TrimSpace(pyStr(m[fieldName]))
		if text != "" && !seen[text] {
			seen[text] = true
			result = append(result, text)
		}
	}
	sort.Strings(result)
	return result
}

// V6ProviderInvocationLineage 将一次真实 Ark 响应绑定到其请求/结果处理过的 ID
// （topic_pipeline.py:913-1031；分支顺序逐条对应）。
func V6ProviderInvocationLineage(stage, payload string, result any) map[string]any {
	if _, exists := V6StageResponseSchemas[stage]; !exists {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		parsed = map[string]any{}
	}
	output, _ := result.(map[string]any)
	if output == nil {
		output = map[string]any{}
	}
	var candidateIDs, starterIDs []string
	var skippedSlots []map[string]string
	hasSkippedSlots := false
	bindingScope := ""
	switch {
	case stage == "v6_session_summarizer" || stage == "rid_interest_distiller":
		bindingScope = "all_cards"
	case stage == "v6_recent_candidate_generator" ||
		stage == "v6_project_domain_candidate_generator" ||
		stage == "v6_project_continuation_generator" ||
		stage == "v6_global_interest_generator":
		bindingScope = "candidate"
		allowedSlotIDs := map[string]bool{}
		for _, id := range LineageIds(parsed["candidate_slots"], "slot_id") {
			allowedSlotIDs[id] = true
		}
		decisions, _ := output["candidates"].([]any)
		var written []any
		var skipped []any
		for _, item := range decisions {
			m, isMap := item.(map[string]any)
			if !isMap {
				continue
			}
			if pyStr(m["decision"]) == "skip" {
				skipped = append(skipped, m)
			} else {
				written = append(written, m)
			}
		}
		writtenIDs := map[string]bool{}
		for _, id := range LineageIds(written, "slot_id") {
			writtenIDs[id] = true
		}
		for _, id := range LineageIds(written, "slot_id") {
			if allowedSlotIDs[id] {
				candidateIDs = append(candidateIDs, id)
			}
		}
		if _, isSlot := slotDecisionItemSchemas[stage]; isSlot {
			// 跳过的槽不产卡；跳过原因留在小票上
			hasSkippedSlots = true
			for _, item := range skipped {
				m := item.(map[string]any)
				skippedSlots = append(skippedSlots, map[string]string{
					"slot_id":             pyStr(m["slot_id"]),
					"interest_summary_id": pyStr(m["interest_summary_id"]),
					"blocked_by":          pyStr(m["blocked_by"]),
					"nearest_covered":     pyStr(m["nearest_covered"]),
					"new_angle":           pyStr(m["new_angle"]),
				})
			}
		}
	case stage == "v6_recent_finalizer":
		bindingScope = "candidate"
		requestIDs := map[string]bool{}
		for _, id := range LineageIds(parsed["candidates"], "candidate_id") {
			requestIDs[id] = true
		}
		for _, id := range LineageIds(output["candidates"], "candidate_id") {
			if requestIDs[id] {
				candidateIDs = append(candidateIDs, id)
			}
		}
	case stage == "v6_hot_news_matcher":
		// 新闻卡是报道或（无报道时）热榜卡：两者都携带匹配
		bindingScope = "candidate"
		idSet := map[string]bool{}
		for _, value := range anyListOf(parsed["candidate_ids"]) {
			if text := pyStr(value); strings.TrimSpace(text) != "" {
				idSet[text] = true
			}
		}
		for id := range idSet {
			candidateIDs = append(candidateIDs, id)
		}
	case stage == "v6_evidence_extractor":
		bindingScope = "candidate"
		if candidateID := strings.TrimSpace(pyStr(parsed["candidate_id"])); candidateID != "" {
			candidateIDs = []string{candidateID}
		} else {
			candidateIDs = []string{}
		}
	case stage == "v6_utility_scorer":
		bindingScope = "candidate"
		candidateIDs = LineageIds(parsed["candidates"], "candidate_id")
	case stage == "v6_diversity_scorer":
		bindingScope = "candidate"
		pairCandidateIDs := map[string]bool{}
		for _, pairValue := range anyListOf(parsed["requested_pairs"]) {
			pair, isMap := pairValue.(map[string]any)
			if !isMap {
				continue
			}
			for _, pairField := range []string{"left_candidate_id", "right_candidate_id"} {
				if text := strings.TrimSpace(pyStr(pair[pairField])); text != "" {
					pairCandidateIDs[text] = true
				}
			}
		}
		for _, candidateID := range anyListOf(parsed["requested_marginal_candidate_ids"]) {
			if text := strings.TrimSpace(pyStr(candidateID)); text != "" {
				pairCandidateIDs[text] = true
			}
		}
		for id := range pairCandidateIDs {
			candidateIDs = append(candidateIDs, id)
		}
	case stage == "v6_pa_fixed_retriever":
		bindingScope = "starter"
		starterIDs = LineageIds(output["hits"], "starter_id")
	default:
		return nil
	}
	if candidateIDs == nil {
		candidateIDs = []string{}
	}
	if starterIDs == nil {
		starterIDs = []string{}
	}
	requestID := strings.TrimSpace(pyStr(parsed["request_id"]))
	inputSnapshotID := strings.TrimSpace(pyStr(orValue(parsed["input_snapshot_id"], parsed["user_state_snapshot_id"])))
	lineage := map[string]any{
		"binding_scope":        bindingScope,
		"candidate_ids":        stringsToAny(candidateIDs),
		"starter_ids":          stringsToAny(starterIDs),
		"component_request_id": nilIfEmpty(requestID, requestID != ""),
		"input_snapshot_id":    inputSnapshotID,
	}
	if hasSkippedSlots {
		slots := make([]any, 0, len(skippedSlots))
		for _, slot := range skippedSlots {
			slots = append(slots, map[string]any{
				"slot_id": slot["slot_id"], "interest_summary_id": slot["interest_summary_id"],
				"blocked_by": slot["blocked_by"], "nearest_covered": slot["nearest_covered"],
				"new_angle": slot["new_angle"],
			})
		}
		lineage["skipped_slots"] = slots
	}
	return lineage
}

// V6SystemPrompt 阶段系统提示词分发（topic_pipeline.py:862-870）。
func V6SystemPrompt(stage, suppliedPrompt string) string {
	switch stage {
	case "behavior_map_batch":
		return V6BehaviorMappingSystemPrompt
	case "memory_init":
		return pipelineMemoryInitSystemPrompt
	case "memory_incremental":
		return pipelineMemoryIncrementalSystemPrompt
	}
	return suppliedPrompt
}

// V6MaxTokens 阶段答案 token 上限（topic_pipeline.py:872-894）。
func V6MaxTokens(stage string) *int {
	// 只对随输入增长的会话摘要阶段收紧到 16384；其余阶段维持默认
	if stage == "v6_session_summarizer" {
		value := 16384
		return &value
	}
	return nil
}

// V6PromptVersion 阶段提示词版本表（topic_pipeline.py:895-900）。
func V6PromptVersion(stage string) string {
	versions := map[string]string{
		"v6_hot_news_matcher":                   HotNewsMatcherPromptVersion,
		"v6_global_interest_generator":          GlobalInterestPromptVersion,
		"v6_project_continuation_generator":     ProjectContinuationPromptVersion,
		"memory_incremental":                    MemoryIncrementalPromptVersion,
		"v6_recent_candidate_generator":         RecentPlannerPromptVersion,
		"v6_recent_finalizer":                   RecentFinalizerPromptVersion,
		"v6_project_domain_candidate_generator": ProjectDomainPromptVersion,
		"behavior_map_batch":                    BehaviorMappingPromptVersion,
		"memory_init":                           MemoryInitPromptVersion,
	}
	return versions[stage]
}

// ──────────────────────────── ⤴️ 31c B 段前置：V6 数据类型（topic_pipeline.py:2480-2522） ────────────────────────────

// V6SourceMessage 单条来源消息（topic_pipeline.py:2480-2501）。
type V6SourceMessage struct {
	// EventID 事件 ID
	EventID string
	// SessionID 会话 ID
	SessionID string
	// Ordinal 记录序号
	Ordinal int
	// Content 正文
	Content string
	// ObservedAt 观察时间
	ObservedAt float64
	// InputOrigin 输入来源
	InputOrigin string
	// AssistanceOrigin 协助来源
	AssistanceOrigin string
	// IntentCluster 兴趣簇
	IntentCluster string
	// ActionType 行为类型
	ActionType string
	// ProjectID 项目 ID（可空）
	ProjectID any
	// RequestID 请求 ID（可空）
	RequestID string
	// AssistantAnswer 助手回答（可空）
	AssistantAnswer any
}

// Natural 自然行为判定（:2494-2501）。
func (m *V6SourceMessage) Natural() bool {
	return m.InputOrigin == "manual_free_text" && m.AssistanceOrigin == "none"
}

// V6ContextSnapshot 一次生成的冻结上下文（topic_pipeline.py:2502-2522）。
type V6ContextSnapshot struct {
	// State 冻结的 v4 状态
	State map[string]any
	// ContextScope 上下文范围
	ContextScope string
	// ProjectID 项目 ID（可空）
	ProjectID any
	// Messages 来源消息
	Messages []*V6SourceMessage
	// DeltaEventIds 增量事件 ID
	DeltaEventIDs []string
	// ShortIntentState 短意图状态（topic_short_intent.py ShortIntentState）
	ShortIntentState *ShortIntentState
	// InputSnapshotId 输入快照 ID
	InputSnapshotID string
	// ProfileProjection 画像投影
	ProfileProjection map[string]any
	// LongTermProfileInterests 长期画像兴趣
	LongTermProfileInterests []*LongTermProfileInterestInput
	// CompletedSessionPrefixes 完结前缀（⤴️31d 会话域投影）
	CompletedSessionPrefixes []*CompletedSessionProjection
	// StateMaintenanceReceipt 状态维护小票（可空）
	StateMaintenanceReceipt map[string]any
	// PendingShortIntentTriggerLineage 未消费的短意图触发血缘；生产快照在
	// 触发被消费后显式用空表（可空，nil 表示注入快照保留状态血缘）
	PendingShortIntentTriggerLineage []map[string]any
	// OtherScopeDiscussionCoverage 另一 scope 的只读去标识讨论覆盖
	OtherScopeDiscussionCoverage []map[string]any
}
