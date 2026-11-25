package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	crand "crypto/rand"
	"encoding/hex"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	promptsTool "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// trajectoryListDefaultLimit trajectory 索引默认页大小。
	// Python: _TRAJECTORY_LIST_DEFAULT_LIMIT (review.py:41)。
	trajectoryListDefaultLimit = 25
	// trajectoryListMaxLimit trajectory 索引最大页大小。
	// Python: _TRAJECTORY_LIST_MAX_LIMIT (review.py:42)。
	trajectoryListMaxLimit = 50
	// trajectoryReadMaxRefs 单次读取 trajectory ref 上限。
	// Python: _TRAJECTORY_READ_MAX_REFS (review.py:43)。
	trajectoryReadMaxRefs = 8
	// reviewTextPreviewChars 预览截断。
	// Python: _REVIEW_TEXT_PREVIEW_CHARS (review.py:44)。
	reviewTextPreviewChars = 240
	// reviewDetailTextChars 详情截断。
	// Python: _REVIEW_DETAIL_TEXT_CHARS (review.py:45)。
	reviewDetailTextChars = 1200
	// reviewArgTextChars 参数截断。
	// Python: _REVIEW_ARG_TEXT_CHARS (review.py:46)。
	reviewArgTextChars = 800
	// reviewLLMMessageTextChars LLM 消息截断。
	// Python: _REVIEW_LLM_MESSAGE_TEXT_CHARS (review.py:47)。
	reviewLLMMessageTextChars = 500
	// reviewLLMMessageLimit LLM 消息保留条数。
	// Python: _REVIEW_LLM_MESSAGE_LIMIT (review.py:48)。
	reviewLLMMessageLimit = 3

	// atPlanApproved 计划审批属性键（Go semconv 缺失，字面量对齐 Python 值）。
	// Python: semconv.AT_PLAN_APPROVED。
	atPlanApproved = "agentteam.plan.approved"
	// atPlanSubmittedBy 计划提交者属性键。
	// Python: semconv.AT_PLAN_SUBMITTED_BY。
	atPlanSubmittedBy = "agentteam.plan.submitted_by"
)

// contextAttributeKeys 语境属性键白名单（27 键，顺序对齐 Python）。
// Python: _CONTEXT_ATTRIBUTE_KEYS (review.py:50-77)。
var contextAttributeKeys = []string{
	obs.GenAIConversationID,
	trajectory.TeamID,
	obs.ATTeamName,
	obs.ATTeamDisplayName,
	obs.ATTeamLeader,
	obs.ATAgentID,
	obs.GenAIAgentName,
	obs.ATAgentRole,
	obs.OJSpanInput,
	obs.OJSpanOutput,
	obs.ATMemberName,
	obs.ATMemberStatusOld,
	obs.ATMemberStatusNew,
	obs.ATMemberRestartReason,
	obs.ATMemberRestartCount,
	obs.ATMemberShutdownForce,
	obs.ATMessageID,
	obs.ATMessageFrom,
	obs.ATMessageTo,
	obs.ATMessageBroadcast,
	obs.ATTaskID,
	obs.ATTaskStatus,
	obs.ATTaskAssignee,
	atPlanApproved,
	atPlanSubmittedBy,
	obs.ATEventType,
}

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionReviewToolsDeps CreateEvolutionReviewTools 组装依赖。
// Python: create_evolution_review_tools (review.py:103-143) 的关键字参数。
type EvolutionReviewToolsDeps struct {
	// Runtime 审查运行时。
	Runtime *review.EvolutionReviewRuntime
	// QueryService 只读查询服务（可空，回落 store 构造）。
	QueryService *experience.ExperienceQueryService
	// Store 演进存储（可空）。
	Store *checkpointing.EvolutionStore
	// Language 语言。
	Language string
	// AgentID 代理 id（工具 id 后缀）。
	AgentID string
}

// evolutionReviewTool 受限审查工具基座。
// Python: _EvolutionReviewTool (review.py:146-219)。
type evolutionReviewTool struct {
	BaseEvolutionTool
	language     string
	runtime      *review.EvolutionReviewRuntime
	queryService *experience.ExperienceQueryService
	store        *checkpointing.EvolutionStore
}

// submitEvolutionReviewTool 提交审查结论工具。
// Python: SubmitEvolutionReviewResultTool (review.py:222-379)。
type submitEvolutionReviewTool struct {
	evolutionReviewTool
}

// reviewListSkillExperiencesTool 审查侧经验索引工具。
// Python: EvolutionReviewListSkillExperiencesTool (review.py:382-422)。
type reviewListSkillExperiencesTool struct {
	evolutionReviewTool
}

// reviewReadSkillExperiencesTool 审查侧经验读取工具。
// Python: EvolutionReviewReadSkillExperiencesTool (review.py:425-494)。
type reviewReadSkillExperiencesTool struct {
	evolutionReviewTool
}

// reviewListTrajectorySpansTool trajectory 索引工具。
// Python: EvolutionReviewListTrajectorySpansTool (review.py:674-780)。
type reviewListTrajectorySpansTool struct {
	evolutionReviewTool
}

// reviewReadTrajectorySpansTool trajectory 详情工具。
// Python: EvolutionReviewReadTrajectorySpansTool (review.py:783-826)。
type reviewReadTrajectorySpansTool struct {
	evolutionReviewTool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateEvolutionReviewTools 创建审查子代理的五只读工具。
// Python: create_evolution_review_tools (review.py:103-143)。
func CreateEvolutionReviewTools(deps EvolutionReviewToolsDeps) ([]tool.Tool, error) {
	resolvedQuery := deps.QueryService
	if resolvedQuery == nil {
		var err error
		resolvedQuery, err = queryServiceFromStore(deps.Store)
		if err != nil {
			return nil, err
		}
	}
	mk := func(name, toolID string, inputParams map[string]any) (*evolutionReviewTool, error) {
		card, err := buildReviewToolCard(name, toolID, deps.Language, deps.AgentID, inputParams)
		if err != nil {
			return nil, err
		}
		return &evolutionReviewTool{
			BaseEvolutionTool: BaseEvolutionTool{
				ToolName: name, ToolID: toolID, FailureMessage: "evolution tool failed", card: card,
			},
			language: deps.Language, runtime: deps.Runtime,
			queryService: resolvedQuery, store: deps.Store,
		}, nil
	}

	submitBase, err := mk("submit_evolution_review", "SubmitEvolutionReviewResultTool", submitInputParams(deps.Language))
	if err != nil {
		return nil, err
	}
	submit := &submitEvolutionReviewTool{evolutionReviewTool: *submitBase}

	listExpBase, err := mk("list_skill_experiences", "EvolutionReviewListSkillExperiencesTool", listReviewInputParams(deps.Language))
	if err != nil {
		return nil, err
	}
	listExp := &reviewListSkillExperiencesTool{evolutionReviewTool: *listExpBase}

	readExpBase, err := mk("read_skill_experiences", "EvolutionReviewReadSkillExperiencesTool", readReviewInputParams(deps.Language))
	if err != nil {
		return nil, err
	}
	readExp := &reviewReadSkillExperiencesTool{evolutionReviewTool: *readExpBase}

	listTrajBase, err := mk("list_trajectory_spans", "EvolutionReviewListTrajectorySpansTool", listTrajectoryInputParams(deps.Language))
	if err != nil {
		return nil, err
	}
	listTraj := &reviewListTrajectorySpansTool{evolutionReviewTool: *listTrajBase}

	readTrajBase, err := mk("read_trajectory_spans", "EvolutionReviewReadTrajectorySpansTool", readTrajectoryInputParams(deps.Language))
	if err != nil {
		return nil, err
	}
	readTraj := &reviewReadTrajectorySpansTool{evolutionReviewTool: *readTrajBase}

	return []tool.Tool{submit, listExp, readExp, listTraj, readTraj}, nil
}

// Stream 不支持流式。
func (t *evolutionReviewTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// GetCallTimeout 沿用卡片级超时。
func (t *evolutionReviewTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// Invoke 提交审查结论并登记到 scope。
// Python: SubmitEvolutionReviewResultTool.invoke (review.py:351-379)。
func (t *submitEvolutionReviewTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	refRaw, ok := args["evolution_review_ref"]
	if !ok {
		return Failure(fmt.Errorf("evolution_review_ref is required")).ToMap(), nil
	}
	ref := fmt.Sprintf("%v", refRaw)
	sessionID := reviewSessionID(args, opts)
	runtime, err := t.runtimeForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	// Python: args.pop("evolution_review_ref") 后整体作为 result。
	result := map[string]any{}
	for k, v := range args {
		if k != "evolution_review_ref" {
			result[k] = v
		}
	}
	scope, err := runtime.RecordReviewResult(ref, sessionID, result)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	reviewResult := map[string]any{}
	for k, v := range scope.Result {
		reviewResult[k] = v
	}
	reviewResult["evolution_review_ref"] = ref
	reviewResult["status"] = string(scope.Status)

	proposalIDs := make([]string, 0, len(scope.ProposalIDs))
	for id := range scope.ProposalIDs {
		proposalIDs = append(proposalIDs, id)
	}
	sort.Strings(proposalIDs)

	data := map[string]any{
		"status":               string(scope.Status),
		"evolution_review_ref": ref,
		"proposal_ids":         proposalIDs,
		"review_result":        reviewResult,
		"proposal_selection_for_submission": map[string]any{
			"evolution_review_ref":  ref,
			"subject":               scope.Subject,
			"selected_proposal_ids": proposalIDs,
		},
	}
	result2 := map[string]any{"success": true, "data": data}
	return result2, nil
}

// Invoke 审查侧经验索引。
// Python: EvolutionReviewListSkillExperiencesTool.invoke (review.py:403-422)。
func (t *reviewListSkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	ref := fmt.Sprintf("%v", args["evolution_review_ref"])
	runtime, err := t.runtimeForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	scope, err := runtime.ResolveScope(ref, reviewSessionID(args, opts))
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	qs, err := t.queryServiceForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	result, err := qs.ListExperiences(
		ctx, scope.Subject,
		floatPtrArg(args, "min_score"),
		intArgOr(args, "limit", 50),
		stringArg(args, "cursor"),
		stringArg(args, "target"),
		stringArg(args, "section"),
		stringArg(args, "query"),
		stringArgOr(args, "sort", "score_desc"),
	)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	return map[string]any{"success": true, "data": result}, nil
}

// Invoke 审查侧经验读取（读取后记账 evidence refs）。
// Python: EvolutionReviewReadSkillExperiencesTool.invoke (review.py:474-494)。
func (t *reviewReadSkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	ref := fmt.Sprintf("%v", args["evolution_review_ref"])
	sessionID := reviewSessionID(args, opts)
	runtime, err := t.runtimeForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	scope, err := runtime.ResolveScope(ref, sessionID)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	qs, err := t.queryServiceForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	result, err := qs.ReadExperiences(
		ctx, scope.Subject,
		stringSliceArg(args, "record_ids"),
		intArgOr(args, "max_content_chars", 2000),
	)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	// Python L488-491: 记账已读 record ids。
	readIDs := make([]string, 0)
	if items, ok := result["items"].([]map[string]any); ok {
		for _, item := range items {
			if id, ok := item["record_id"].(string); ok && id != "" {
				readIDs = append(readIDs, id)
			}
		}
	}
	if err := runtime.RecordEvidenceRead(ref, sessionID, readIDs); err != nil {
		return Failure(err).ToMap(), nil
	}
	return map[string]any{"success": true, "data": result}, nil
}

// Invoke trajectory 索引（四过滤 + 游标分页）。
// Python: EvolutionReviewListTrajectorySpansTool.invoke (review.py:738-780)。
func (t *reviewListTrajectorySpansTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	ref := fmt.Sprintf("%v", args["evolution_review_ref"])
	runtime, err := t.runtimeForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	scope, err := runtime.ResolveScope(ref, reviewSessionID(args, opts))
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	indexed := trajectorySpanIndex(scope.Trajectory)
	items := make([]map[string]any, 0, len(indexed))
	for _, entry := range indexed {
		items = append(items, spanIndexItem(entry.span))
	}

	if kind := stringArg(args, "kind"); kind != "" {
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if fmt.Sprintf("%v", item["kind"]) == kind {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if nameContains := strings.ToLower(stringArg(args, "name_contains")); nameContains != "" {
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if strings.Contains(strings.ToLower(fmt.Sprintf("%v", item["name"])), nameContains) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if toolName := stringArg(args, "tool_name"); toolName != "" {
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if fmt.Sprintf("%v", item["tool_name"]) == toolName {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if rawHasError, present := args["has_error"]; present {
		expected, _ := rawHasError.(bool)
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			hasError, _ := item["has_error"].(bool)
			if hasError == expected {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	total := len(items)
	start := intArgOr(args, "cursor", 0)
	if start < 0 {
		start = 0
	}
	limit := intArgOr(args, "limit", trajectoryListDefaultLimit)
	if limit <= 0 {
		limit = trajectoryListDefaultLimit
	}
	if limit > trajectoryListMaxLimit {
		limit = trajectoryListMaxLimit
	}
	pageEnd := start + limit
	if pageEnd > total {
		pageEnd = total
	}
	var page []map[string]any
	if start < total {
		page = items[start:pageEnd]
	} else {
		page = []map[string]any{}
	}
	nextIndex := start + len(page)
	var nextCursor any
	if nextIndex < total {
		nextCursor = fmt.Sprintf("%d", nextIndex)
	}
	return map[string]any{"success": true, "data": map[string]any{
		"items": page, "next_cursor": nextCursor, "total": total,
	}}, nil
}

// Invoke trajectory 详情（≤8 refs，读取后记账）。
// Python: EvolutionReviewReadTrajectorySpansTool.invoke (review.py:806-826)。
func (t *reviewReadTrajectorySpansTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	ref := fmt.Sprintf("%v", args["evolution_review_ref"])
	refs := stringSliceArg(args, "refs")
	if len(refs) > trajectoryReadMaxRefs {
		return Failure(fmt.Errorf("read at most %d trajectory refs per call", trajectoryReadMaxRefs)).ToMap(), nil
	}
	sessionID := reviewSessionID(args, opts)
	runtime, err := t.runtimeForRef(ref)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	scope, err := runtime.ResolveScope(ref, sessionID)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	byRef := map[string]map[string]any{}
	for _, entry := range trajectorySpanIndex(scope.Trajectory) {
		byRef[entry.ref] = entry.span
	}
	missing := make([]string, 0)
	for _, r := range refs {
		if _, ok := byRef[r]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return Failure(fmt.Errorf("unknown trajectory refs: %v", missing)).ToMap(), nil
	}
	if err := runtime.RecordTrajectoryRead(ref, sessionID, refs); err != nil {
		return Failure(err).ToMap(), nil
	}
	items := make([]map[string]any, 0, len(refs))
	for _, r := range refs {
		items = append(items, spanDetail(byRef[r]))
	}
	return map[string]any{"success": true, "data": map[string]any{"items": items}}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildReviewToolCard 构建受限审查工具卡（id 带 agent_id 后缀）。
// Python: _EvolutionReviewTool.__init__/_tool_id (review.py:158-175)。
func buildReviewToolCard(name, toolID, language, agentID string, inputParams map[string]any) (*tool.ToolCard, error) {
	description := EvolutionTextOf(language,
		fmt.Sprintf("受限 Skill 演进审查工具：%s。", name),
		fmt.Sprintf("Restricted Skill evolution review tool: %s.", name))
	inputParamsParsed, err := cschema.ParseJSONSchemaMap(inputParams)
	if err != nil {
		return nil, fmt.Errorf("工具 %q: 解析输入参数失败: %w", name, err)
	}
	finalID := fmt.Sprintf("%s_%s", toolID, agentID)
	if agentID == "" {
		finalID = fmt.Sprintf("%s_%s", toolID, newShortID())
	}
	return tool.NewToolCardWithID(finalID, name, description, inputParamsParsed, nil), nil
}

// newShortID 8 位随机 hex（对齐 BuildToolCard 兜底形态）。
func newShortID() string {
	b := make([]byte, 4)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

// EvolutionTextOf 双语文案选择（prompts_tool 的别名，review.py:80-82 _text）。
func EvolutionTextOf(language, cn, en string) string {
	return promptsTool.EvolutionText(language, cn, en)
}

// submitInputParams 提交结论工具入参 schema。
// Python: SubmitEvolutionReviewResultTool._input_params (review.py:227-349)。
func submitInputParams(language string) map[string]any {
	experienceSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "一句话说明经验适用场景和建议行为。", "One sentence describing applicability and recommended behavior."),
			},
			"content": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "可直接提交的经验正文。", "Experience content ready for submission."),
			},
			"target": map[string]any{
				"type": "string",
				"enum": schema.EvolutionTargetValues,
				"description": EvolutionTextOf(language,
					fmt.Sprintf("写入目标；可选值：%s。", joinValues(schema.EvolutionTargetValues)),
					fmt.Sprintf("Write target. Allowed values: %s.", joinValues(schema.EvolutionTargetValues))),
			},
			"section": map[string]any{
				"type": "string",
				"enum": sortedSections(),
				"description": EvolutionTextOf(language,
					fmt.Sprintf("目标 section；可选值：%s。普通正文经验优先使用 Troubleshooting。", joinValues(sortedSections())),
					fmt.Sprintf("Target section. Allowed values: %s. Prefer Troubleshooting for normal body experiences.", joinValues(sortedSections()))),
			},
			"reason": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "推荐沉淀该经验的原因。", "Reason for recommending this experience."),
			},
		},
		"required": []string{"summary", "content"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "当前 follow-up prompt 中提供的 evolution_review_ref。", "Evolution review ref from the current follow-up prompt."),
			},
			"subject": promptsTool.BuildEvolutionSubjectSchema(language),
			"outcome": map[string]any{
				"type":        "string",
				"enum":        []string{"recommend_evolve", "no_evolution"},
				"description": EvolutionTextOf(language, "当前演进目标的审查结论。", "Review conclusion for the current evolution target."),
			},
			"evidence_refs": map[string]any{
				"type":        "array",
				"description": EvolutionTextOf(language, "产出结果前已读取的证据 ref。", "Evidence refs that were read before producing the result."),
				"items":       map[string]any{"type": "string"},
			},
			"proposals": map[string]any{
				"type":        "array",
				"maxItems":    evoschema.MaxEvolutionReviewProposals,
				"description": EvolutionTextOf(language, "recommend_evolve 结论下已审查通过的 experience proposal。", "Reviewed experience proposals for recommend_evolve outcomes."),
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"proposal_id": map[string]any{
							"type":        "string",
							"description": EvolutionTextOf(language, "本次 result 内唯一的 proposal id。", "Proposal id unique within this result."),
						},
						"experience": experienceSchema,
						"reason": map[string]any{
							"type":        "string",
							"description": EvolutionTextOf(language, "该 proposal 的审查理由。", "Review rationale for this proposal."),
						},
						"evidence_refs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []string{"proposal_id", "experience"},
				},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "简短审查摘要。", "Short review summary."),
			},
		},
		"required": []string{"evolution_review_ref", "subject", "outcome", "evidence_refs", "proposals"},
	}
}

// listReviewInputParams 审查侧经验索引入参。
// Python: _EvolutionReviewTool._input_params（ref 必填）。
func listReviewInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": reviewRefParam(language),
			"subject":              promptsTool.BuildEvolutionSubjectSchema(language),
			"min_score":            map[string]any{"type": "number"},
			"limit":                map[string]any{"type": "integer", "default": 50},
			"cursor":               map[string]any{"type": "string"},
			"target":               map[string]any{"type": "string"},
			"section":              map[string]any{"type": "string"},
			"query":                map[string]any{"type": "string"},
			"sort":                 map[string]any{"type": "string", "default": "score_desc"},
		},
		"required": []string{"evolution_review_ref"},
	}
}

// readReviewInputParams 审查侧经验读取入参。
// Python: EvolutionReviewReadSkillExperiencesTool._input_params (review.py:447-472)。
func readReviewInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": reviewRefParam(language),
			"record_ids": map[string]any{
				"type":        "array",
				"description": EvolutionTextOf(language, "要读取的经验记录 ID。", "Experience record IDs to read."),
				"items":       map[string]any{"type": "string"},
			},
			"max_content_chars": map[string]any{
				"type":        "integer",
				"default":     2000,
				"description": EvolutionTextOf(language, "每条记录最多返回的内容字符数。", "Maximum content characters per record."),
			},
		},
		"required": []string{"evolution_review_ref", "record_ids"},
	}
}

// listTrajectoryInputParams trajectory 索引入参。
// Python: EvolutionReviewListTrajectorySpansTool._input_params (review.py:679-736)。
func listTrajectoryInputParams(language string) map[string]any {
	categories := make([]string, 0, len(trajectory.TeamCategories))
	for k := range trajectory.TeamCategories {
		categories = append(categories, k)
	}
	sort.Strings(categories)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": reviewRefParam(language),
			"cursor": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "上一轮 list 调用返回的零基游标。", "Opaque zero-based cursor returned by the previous list call."),
			},
			"limit": map[string]any{
				"type":        "integer",
				"default":     trajectoryListDefaultLimit,
				"description": EvolutionTextOf(language, "最多返回的 trajectory span 索引条目数。", "Maximum trajectory span index items to return."),
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        categories,
				"description": EvolutionTextOf(language, "可选 trajectory span 类型过滤。", "Optional trajectory span kind filter."),
			},
			"name_contains": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "可选 span 名称大小写不敏感子串过滤。", "Optional case-insensitive span-name substring filter."),
			},
			"tool_name": map[string]any{
				"type":        "string",
				"description": EvolutionTextOf(language, "tool span 的可选工具名过滤。", "Optional tool-name filter for tool spans."),
			},
			"has_error": map[string]any{
				"type":        "boolean",
				"description": EvolutionTextOf(language, "可选错误状态过滤。", "Optional error-state filter."),
			},
		},
		"required": []string{"evolution_review_ref"},
	}
}

// readTrajectoryInputParams trajectory 详情入参。
// Python: EvolutionReviewReadTrajectorySpansTool._input_params (review.py:788-804)。
func readTrajectoryInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": reviewRefParam(language),
			"refs": map[string]any{
				"type":        "array",
				"description": EvolutionTextOf(language, "当前审查通过 list_trajectory_spans 获得的 span ref。", "Span refs returned by list_trajectory_spans for the current review."),
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"evolution_review_ref", "refs"},
	}
}

// reviewRefParam evolution_review_ref 参数定义。
// Python: _evolution_review_ref_param (review.py:88-96)。
func reviewRefParam(language string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": EvolutionTextOf(language, "当前 follow-up prompt 中提供的 evolution_review_ref。", "Evolution review ref from the current follow-up prompt."),
	}
}

// sortedSections 排序后的 section 全集。
// Python: _SECTION_VALUES = sorted(VALID_SECTIONS) (review.py:39)。
func sortedSections() []string {
	keys := make([]string, 0, len(schema.ValidSections))
	for k := range schema.ValidSections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// queryServiceFromStore 由 store 构造查询服务。
// Python: _query_service_from_store (review.py:497-500)。
func queryServiceFromStore(store *checkpointing.EvolutionStore) (*experience.ExperienceQueryService, error) {
	if store == nil {
		return nil, fmt.Errorf("query_service or store is required")
	}
	return experience.NewExperienceQueryService(store), nil
}

// runtimeForRef 解析运行时（缺失报 KeyError 语义）。
// Python: _runtime_for_ref (review.py:204-207)。
func (t *evolutionReviewTool) runtimeForRef(ref string) (*review.EvolutionReviewRuntime, error) {
	if t.runtime == nil {
		return nil, fmt.Errorf("unknown evolution review ref: %s", ref)
	}
	return t.runtime, nil
}

// queryServiceForRef 解析查询服务。
// Python: _query_service_for_ref (review.py:215-219)。
func (t *evolutionReviewTool) queryServiceForRef(ref string) (*experience.ExperienceQueryService, error) {
	if _, err := t.runtimeForRef(ref); err != nil {
		return nil, err
	}
	if t.queryService != nil {
		return t.queryService, nil
	}
	return queryServiceFromStore(t.store)
}

// reviewSessionID 提取会话 id。
// Python: _session_id (review.py:196-202)。
func reviewSessionID(args map[string]any, opts []tool.ToolOption) string {
	return sessionIDFromOpts(args, opts)
}

// spanRefEntry 索引条目（ref → span）。
type spanRefEntry struct {
	ref  string
	span map[string]any
}

// spanRef 生成 span 引用。
// Python: _span_ref (review.py:503-507)。
func spanRef(span map[string]any) string {
	identity := trajectory.SpanIdentityOf(span)
	if identity == nil {
		return ""
	}
	return fmt.Sprintf("span:%s:%s", identity.TraceID, identity.SpanID)
}

// parentSpanRef 生成父 span 引用。
// Python: _parent_span_ref (review.py:510-515)。
func parentSpanRef(span map[string]any) string {
	traceID := strings.TrimSpace(strFromAny(span["traceId"]))
	parentSpanID := strings.TrimSpace(strFromAny(span["parentSpanId"]))
	if traceID == "" || parentSpanID == "" {
		return ""
	}
	return fmt.Sprintf("span:%s:%s", traceID, parentSpanID)
}

// reviewSafe 审查安全脱敏（图片/二进制内容剔除）。
// Python: _review_safe (review.py:518-543)。
func reviewSafe(value any) any {
	if text, ok := value.(string); ok {
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(text)), "data:image/") {
			return "<omitted:image_content>"
		}
		return text
	}
	switch v := value.(type) {
	case nil, bool, int, int64, float64, string:
		return value
	case map[string]any:
		contentType := strings.ToLower(strFromAny(v["type"]))
		if contentType == "image" || contentType == "image_url" || contentType == "input_image" {
			return map[string]any{"type": contentType, "omitted": "image_content"}
		}
		result := map[string]any{}
		for key, item := range v {
			keyLower := strings.ToLower(key)
			if strings.Contains(keyLower, "base64") || keyLower == "image_url" || keyLower == "bytes" {
				continue
			}
			result[key] = reviewSafe(item)
		}
		return result
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, reviewSafe(item))
		}
		return out
	default:
		return fmt.Sprintf("%v", value)
	}
}

// boundedValue 截断值包装。
// Python: _bounded_value (review.py:546-558)。
func boundedValue(value any, limit int) map[string]any {
	safeValue := reviewSafe(value)
	var text string
	if s, ok := safeValue.(string); ok {
		text = s
	} else {
		encoded, err := json.Marshal(safeValue)
		if err != nil {
			text = fmt.Sprintf("%v", safeValue)
		} else {
			text = string(encoded)
		}
	}
	truncated := len(text) > limit
	shown := any(safeValue)
	if truncated {
		shown = text[:limit]
	}
	result := map[string]any{"value": shown, "truncated": truncated}
	if truncated {
		result["original_chars"] = len(text)
	}
	return result
}

// boundedMessages 截断消息列表（保留末尾 N 条）。
// Python: _bounded_messages (review.py:561-581)。
func boundedMessages(messages []map[string]any) map[string]any {
	selected := messages
	truncatedCount := 0
	if len(messages) > reviewLLMMessageLimit {
		selected = messages[len(messages)-reviewLLMMessageLimit:]
		truncatedCount = len(messages) - reviewLLMMessageLimit
	}
	items := make([]map[string]any, 0, len(selected))
	for _, message := range selected {
		item := map[string]any{"role": strFromAny(message["role"])}
		if content, ok := message["content"]; ok {
			item["content"] = boundedValue(content, reviewLLMMessageTextChars)
		}
		if toolCalls, ok := message["tool_calls"]; ok {
			item["tool_calls"] = boundedValue(toolCalls, reviewArgTextChars)
		}
		items = append(items, item)
	}
	return map[string]any{
		"items":          items,
		"truncated":      truncatedCount > 0,
		"original_count": len(messages),
	}
}

// spanCommon span 公共字段。
// Python: _span_common (review.py:584-598)。
func spanCommon(span map[string]any, includeError bool) map[string]any {
	item := map[string]any{
		"ref":                  spanRef(span),
		"parent_ref":           parentSpanRef(span),
		"name":                 strFromAny(span["name"]),
		"kind":                 trajectory.SpanCategory(span),
		"start_time_unix_nano": strFromAny(span["startTimeUnixNano"]),
		"end_time_unix_nano":   strFromAny(span["endTimeUnixNano"]),
		"status":               trajectory.SpanStatus(span),
	}
	if includeError {
		if spanErr := trajectory.ReadSpanError(span); spanErr != nil {
			item["error"] = boundedValue(map[string]any{
				"status": spanErr.Status, "message": spanErr.Message, "exception": spanErr.Exception,
			}, reviewDetailTextChars)
		}
	}
	return item
}

// trajectorySpanIndex 构建排序后的 span 索引。
// Python: _trajectory_span_index (review.py:601-609)。
func trajectorySpanIndex(traj *trajectory.Trajectory) []spanRefEntry {
	if traj == nil {
		return []spanRefEntry{}
	}
	spans := trajectory.IterSpans(traj)
	// Python: sorted(iter_spans(trajectory), key=span_sort_key)——
	// Go 侧 SpanSortKeyOf 全字段可比，本地稳定排序（compareSpanSortKey 未导出）。
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := trajectory.SpanSortKeyOf(spans[i]), trajectory.SpanSortKeyOf(spans[j])
		if a.StartTime != b.StartTime {
			return a.StartTime < b.StartTime
		}
		if a.EndTime != b.EndTime {
			return a.EndTime < b.EndTime
		}
		if a.TraceID != b.TraceID {
			return a.TraceID < b.TraceID
		}
		return a.SpanID < b.SpanID
	})
	indexed := make([]spanRefEntry, 0, len(spans))
	for _, span := range spans {
		ref := spanRef(span)
		if ref != "" {
			indexed = append(indexed, spanRefEntry{ref: ref, span: span})
		}
	}
	return indexed
}

// spanIndexItem 索引条目。
// Python: _span_index_item (review.py:612-625)。
func spanIndexItem(span map[string]any) map[string]any {
	item := spanCommon(span, false)
	item["has_error"] = trajectory.ReadSpanError(span) != nil
	delete(item, "status")
	attrs := trajectory.SpanAttributes(span)
	if fmt.Sprintf("%v", item["kind"]) == "llm" {
		model := strFromAny(attrs[obs.GenAIResponseModel])
		if model == "" {
			model = strFromAny(attrs[obs.GenAIRequestModel])
		}
		if model != "" {
			item["model"] = model
		}
	} else if fmt.Sprintf("%v", item["kind"]) == "tool" {
		toolCall := trajectory.ReadToolCall(span)
		if name := strFromAny(toolCall["name"]); name != "" {
			item["tool_name"] = name
		}
	}
	return item
}

// contextAttributes 语境属性白名单投影。
// Python: _context_attributes (review.py:628-641)。
func contextAttributes(span map[string]any) map[string]any {
	attrs := trajectory.SpanAttributes(span)
	result := map[string]any{}
	for _, key := range contextAttributeKeys {
		value, ok := attrs[key]
		if !ok {
			continue
		}
		if key == obs.OJSpanInput || key == obs.OJSpanOutput {
			result[key] = boundedValue(value, reviewDetailTextChars)
			continue
		}
		if text, ok := value.(string); ok && len(text) > reviewTextPreviewChars {
			result[key] = boundedValue(value, reviewTextPreviewChars)
			continue
		}
		result[key] = reviewSafe(value)
	}
	return result
}

// spanDetail span 详情（llm/tool/context 三分支）。
// Python: _span_detail (review.py:644-671)。
func spanDetail(span map[string]any) map[string]any {
	item := spanCommon(span, true)
	kind := fmt.Sprintf("%v", item["kind"])
	attrs := trajectory.SpanAttributes(span)
	if kind == "llm" {
		promptsMsgs, completionsMsgs := trajectory.ReadLLMExchange(span)
		model := strFromAny(attrs[obs.GenAIResponseModel])
		if model == "" {
			model = strFromAny(attrs[obs.GenAIRequestModel])
		}
		item["llm"] = map[string]any{
			"model":           model,
			"input_messages":  boundedMessages(promptsMsgs),
			"output_messages": boundedMessages(completionsMsgs),
		}
	} else if kind == "tool" {
		toolCall := trajectory.ReadToolCall(span)
		toolDetail := map[string]any{
			"name": strFromAny(toolCall["name"]),
			"id":   strFromAny(toolCall["id"]),
		}
		if input, ok := toolCall["input"]; ok {
			toolDetail["input"] = boundedValue(input, reviewArgTextChars)
		}
		if output, ok := toolCall["output"]; ok {
			toolDetail["output"] = boundedValue(output, reviewDetailTextChars)
		}
		item["tool"] = toolDetail
	} else {
		context := contextAttributes(span)
		if len(context) > 0 {
			item["context"] = context
		}
	}
	return item
}

// joinValues 逗号连接取值列表（与 skill.go 无关，本包自持）。
// Python: _values (review.py:84-85)。
func joinValues(values []string) string {
	return strings.Join(values, ", ")
}

// strFromAny any → string（nil → 空串）。
func strFromAny(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}
