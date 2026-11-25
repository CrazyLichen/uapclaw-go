package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	promptsTool "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/subagent"
)

// ──────────────────────────── 常量 ────────────────────────────

// MainEvolutionToolNames 主 agent 演进工具名清单。
// Python: MAIN_EVOLUTION_TOOL_NAMES (tools/skill.py:19-26)；
// EVOLUTION_TOOL_NAMES 为兼容别名（L29），Go 以同名导出常量提供。
var (
	// MainEvolutionToolNames 六工具注册名（顺序对齐 Python 元组）。
	MainEvolutionToolNames = []string{
		"prepare_skill_evolution",
		"evolve_review_task",
		"list_skill_experiences",
		"read_skill_experiences",
		"evolve_skill_experiences",
		"simplify_skill_experiences",
	}
	// EvolutionToolNames 兼容别名。
	// Python: EVOLUTION_TOOL_NAMES = MAIN_EVOLUTION_TOOL_NAMES (skill.py:29)。
	EvolutionToolNames = MainEvolutionToolNames
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrepareScopeFunc 准备审查 scope 的协作回调（rail 侧 _prepare_evolution_review_scope）。
// Python: prepare_scope Callable (skill.py:36 注入，调用点 L185-191)。
// 偏差 D-15.24b-1：memberID/teamID 定位暂不可从 SessionFacade 获取（接口无
// GetAgentID/GetTeamID），恒传空串；⤴️15.24c 团队装配批补。
type PrepareScopeFunc func(source string, subject map[string]any, sessionID, memberID, teamID, userIntent string) (review.EvolutionReviewLaunch, error)

// evolutionTool 主 agent 经验工具公共基座。
// Python: _EvolutionTool (tools/skill.py:99-157)。
type evolutionTool struct {
	BaseEvolutionTool
	// queryService 只读查询服务（可为 nil，按需注入）。
	queryService *experience.ExperienceQueryService
	// submissionService 提交服务（可为 nil）。
	submissionService *experience.ExperienceSubmissionService
	// reviewRuntime 审查运行时（可为 nil）。
	reviewRuntime *review.EvolutionReviewRuntime
	// prepareScope 准备 scope 回调（Prepare 工具必注入）。
	prepareScope PrepareScopeFunc
}

// EvolveReviewTaskToolDeps EvolveReviewTaskTool 组装依赖。
// Python: EvolveReviewTaskTool.__init__ (skill.py:210-219)——parent_agent/review_runtime。
type EvolveReviewTaskToolDeps struct {
	// ParentAgent 父代理（TaskTool 拉起子 agent 必需）。
	ParentAgent interfaces.DeepAgentInterface
	// ReviewRuntime 审查运行时。
	ReviewRuntime *review.EvolutionReviewRuntime
	// Language 语言。
	Language string
	// AgentID 代理 id（可空）。
	AgentID string
}

// evolveReviewTaskTool 拉起受限演进审查子代理的工具。
// Python: EvolveReviewTaskTool (tools/skill.py:204-295)。
type evolveReviewTaskTool struct {
	BaseEvolutionTool
	// taskTool 复用既有 TaskTool（Python: self._task_tool = TaskTool(card=self.card, ...)）。
	taskTool tool.Tool
	// reviewRuntime 审查运行时。
	reviewRuntime *review.EvolutionReviewRuntime
	// parentAgent 父代理引用（Python self._task_tool.parent_agent 判空语义）。
	parentAgent interfaces.DeepAgentInterface
}

// prepareSkillEvolutionReviewTool 准备演进审查的工具。
// Python: PrepareSkillEvolutionReviewTool (tools/skill.py:160-201)。
type prepareSkillEvolutionReviewTool struct {
	evolutionTool
}

// listSkillExperiencesTool 经验索引列表工具。
// Python: ListSkillExperiencesTool (tools/skill.py:298-320)。
type listSkillExperiencesTool struct {
	evolutionTool
}

// readSkillExperiencesTool 经验全文读取工具。
// Python: ReadSkillExperiencesTool (tools/skill.py:323-340)。
type readSkillExperiencesTool struct {
	evolutionTool
}

// evolveSkillExperiencesTool 接受审查 proposals 的工具。
// Python: EvolveSkillExperiencesTool (tools/skill.py:343-383)。
type evolveSkillExperiencesTool struct {
	evolutionTool
}

// simplifySkillExperiencesTool 精简动作工具。
// Python: SimplifySkillExperiencesTool (tools/skill.py:386-408)。
type simplifySkillExperiencesTool struct {
	evolutionTool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMainEvolutionTools 创建主 agent 六工具（显式注入服务）。
// Python: create_main_evolution_tools (tools/skill.py:32-78)。
// prepareScope 必注入；queryService/submissionService 必注入（Python L43-44）；
// reviewRuntime 可为 nil（Evolve 两工具按 Python L353-354 报错）。
func CreateMainEvolutionTools(
	queryService *experience.ExperienceQueryService,
	submissionService *experience.ExperienceSubmissionService,
	prepareScope PrepareScopeFunc,
	reviewRuntime *review.EvolutionReviewRuntime,
	language string,
	agentID string,
	parentAgent interfaces.DeepAgentInterface,
) ([]tool.Tool, error) {
	if queryService == nil || submissionService == nil || prepareScope == nil {
		return nil, fmt.Errorf("query_service, submission_service, and prepare_scope are required")
	}
	common := evolutionTool{
		queryService:      queryService,
		submissionService: submissionService,
		reviewRuntime:     reviewRuntime,
		prepareScope:      prepareScope,
	}
	prepareCard, err := promptsTool.BuildEvolutionToolCard("prepare_skill_evolution", "PrepareSkillEvolutionReviewTool", language, agentID)
	if err != nil {
		return nil, err
	}
	prepare := &prepareSkillEvolutionReviewTool{evolutionTool: common}
	prepare.ToolName = "prepare_skill_evolution"
	prepare.ToolID = "PrepareSkillEvolutionReviewTool"
	prepare.FailureMessage = "evolution tool failed"
	prepare.card = prepareCard

	reviewCard, err := promptsTool.BuildEvolutionToolCard("evolve_review_task", "EvolveReviewTaskTool", language, agentID)
	if err != nil {
		return nil, err
	}
	evolveReview := &evolveReviewTaskTool{
		BaseEvolutionTool: BaseEvolutionTool{
			ToolName:       "evolve_review_task",
			ToolID:         "EvolveReviewTaskTool",
			FailureMessage: "evolution tool failed",
			card:           reviewCard,
		},
		reviewRuntime: reviewRuntime,
		parentAgent:   parentAgent,
	}
	// Python: self._task_tool = TaskTool(card=self.card, parent_agent=..., language=...)
	// Go 侧 NewTaskTool 自建 task_tool 卡（仅程序化 Invoke，不注册，卡身份无影响）。
	evolveReview.taskTool = subagent.NewTaskTool(parentAgent, review.EvolutionReviewAgentName, language, agentID)

	listCard, err := promptsTool.BuildEvolutionToolCard("list_skill_experiences", "ListSkillExperiencesTool", language, agentID)
	if err != nil {
		return nil, err
	}
	list := &listSkillExperiencesTool{evolutionTool: common}
	list.ToolName = "list_skill_experiences"
	list.ToolID = "ListSkillExperiencesTool"
	list.FailureMessage = "evolution tool failed"
	list.card = listCard

	readCard, err := promptsTool.BuildEvolutionToolCard("read_skill_experiences", "ReadSkillExperiencesTool", language, agentID)
	if err != nil {
		return nil, err
	}
	read := &readSkillExperiencesTool{evolutionTool: common}
	read.ToolName = "read_skill_experiences"
	read.ToolID = "ReadSkillExperiencesTool"
	read.FailureMessage = "evolution tool failed"
	read.card = readCard

	evolveCard, err := promptsTool.BuildEvolutionToolCard("evolve_skill_experiences", "EvolveSkillExperiencesTool", language, agentID)
	if err != nil {
		return nil, err
	}
	evolve := &evolveSkillExperiencesTool{evolutionTool: common}
	evolve.ToolName = "evolve_skill_experiences"
	evolve.ToolID = "EvolveSkillExperiencesTool"
	evolve.FailureMessage = "evolution tool failed"
	evolve.card = evolveCard

	simplifyCard, err := promptsTool.BuildEvolutionToolCard("simplify_skill_experiences", "SimplifySkillExperiencesTool", language, agentID)
	if err != nil {
		return nil, err
	}
	simplify := &simplifySkillExperiencesTool{evolutionTool: common}
	simplify.ToolName = "simplify_skill_experiences"
	simplify.ToolID = "SimplifySkillExperiencesTool"
	simplify.FailureMessage = "evolution tool failed"
	simplify.card = simplifyCard

	return []tool.Tool{prepare, evolveReview, list, read, evolve, simplify}, nil
}

// CreateEvolveReviewTaskTool 创建专用的受限演进审查工具。
// Python: create_evolve_review_task_tool (tools/skill.py:81-96)。
func CreateEvolveReviewTaskTool(deps EvolveReviewTaskToolDeps) ([]tool.Tool, error) {
	card, err := promptsTool.BuildEvolutionToolCard("evolve_review_task", "EvolveReviewTaskTool", deps.Language, deps.AgentID)
	if err != nil {
		return nil, err
	}
	t := &evolveReviewTaskTool{
		BaseEvolutionTool: BaseEvolutionTool{
			ToolName:       "evolve_review_task",
			ToolID:         "EvolveReviewTaskTool",
			FailureMessage: "evolution tool failed",
			card:           card,
		},
		reviewRuntime: deps.ReviewRuntime,
		parentAgent:   deps.ParentAgent,
	}
	t.taskTool = subagent.NewTaskTool(deps.ParentAgent, review.EvolutionReviewAgentName, deps.Language, deps.AgentID)
	return []tool.Tool{t}, nil
}

// Invoke 准备演进审查。
// Python: PrepareSkillEvolutionReviewTool.invoke (skill.py:179-201)。
func (t *prepareSkillEvolutionReviewTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	if v, ok := args["user_confirmed"].(bool); !ok || !v {
		return Failure(fmt.Errorf("user_confirmed must be true before preparing skill evolution review")).ToMap(), nil
	}
	subject, err := t.subjectFromInputs(args)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	sessionID := sessionIDFromOpts(args, opts)
	// Python: **self._session_scope(kwargs)（member_id/team_id）；偏差 D-15.24b-1 恒空串。
	launch, err := t.prepareScope("agent_detected_signal", subject, sessionID, "", "", stringArg(args, "user_intent"))
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	out := t.ToOutput(map[string]any{
		"success":              true,
		"operation":            "prepare_skill_evolution",
		"scope_id":             launch.ScopeID,
		"evolution_review_ref": launch.EvolutionReviewRef,
		"subject":              launch.Subject,
	})
	result := out.ToMap()
	InjectMappedContent(result, out)
	return result, nil
}

// Invoke 拉起受限演进审查子代理并回收审查结论。
// Python: EvolveReviewTaskTool.invoke (skill.py:230-267)。
func (t *evolveReviewTaskTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	ref := strings.TrimSpace(stringArg(args, "evolution_review_ref"))
	if ref == "" {
		return Failure(fmt.Errorf("evolution_review_ref is required")).ToMap(), nil
	}
	if t.parentAgent == nil {
		return Failure(fmt.Errorf("parent_agent is required to run evolve_review_task")).ToMap(), nil
	}
	taskResult, err := t.taskTool.Invoke(ctx, map[string]any{
		"subagent_type":    review.EvolutionReviewAgentName,
		"task_description": buildTaskDescription(args, ref),
	}, opts...)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	// Python: task_result.success 判断——Go Invoke 失败走 error 分支，
	// map 内 success=false 视为失败透传。
	if success, ok := taskResult["success"].(bool); ok && !success {
		return taskResult, nil
	}

	scope, err := t.reviewRuntime.ResolveScope(ref, sessionIDFromOpts(args, opts))
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	if (scope.Status != review.ScopeStatusReviewCompleted && scope.Status != review.ScopeStatusNoEvolution) || scope.Result == nil {
		return Failure(fmt.Errorf("evolution reviewer did not submit a review result; scope status is %s", scope.Status)).ToMap(), nil
	}

	reviewResult := map[string]any{}
	for k, v := range scope.Result {
		reviewResult[k] = v
	}
	reviewResult["evolution_review_ref"] = ref
	reviewResult["status"] = scope.Status
	outputJSON, err := json.Marshal(reviewResult)
	if err != nil {
		return Failure(err).ToMap(), nil
	}

	data := map[string]any{}
	if v, ok := taskResult["data"].(map[string]any); ok {
		for k, vv := range v {
			data[k] = vv
		}
	} else {
		for k, vv := range taskResult {
			data[k] = vv
		}
	}
	data["output"] = string(outputJSON)
	data["review_result"] = reviewResult

	// Python: render_for_llm 只渲染 data["output"]（skill.py:269-273）。
	result := map[string]any{"success": true, "data": data}
	result[MappedContentKey] = string(outputJSON)
	return result, nil
}

// Invoke 列出经验索引。
// Python: ListSkillExperiencesTool.invoke (skill.py:304-320)。
func (t *listSkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	subject, err := t.subjectFromInputs(args)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	result, err := t.queryService.ListExperiences(
		ctx, subject,
		subjectKindArg(args),
		floatPtrArg(args, "min_score"),
		intArgOr(args, "limit", 20),
		stringArg(args, "cursor"),
		stringArg(args, "target"),
		stringArg(args, "section"),
		stringArg(args, "query"),
		stringArgOr(args, "sort", "score_desc"),
	)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	InjectMappedContent(result, t.ToOutput(result))
	return result, nil
}

// Invoke 读取经验全文。
// Python: ReadSkillExperiencesTool.invoke (skill.py:329-340)。
func (t *readSkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	subject, err := t.subjectFromInputs(args)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	recordIDs := stringSliceArg(args, "record_ids")
	result, err := t.queryService.ReadExperiences(
		ctx, subject,
		subjectKindArg(args),
		recordIDs,
		intArgOr(args, "max_content_chars", 2000),
	)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	InjectMappedContent(result, t.ToOutput(result))
	return result, nil
}

// Invoke 接受审查 proposals 并落盘。
// Python: EvolveSkillExperiencesTool.invoke (skill.py:349-383)。
func (t *evolveSkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	subject, err := t.subjectFromInputs(args)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	if t.reviewRuntime == nil {
		return Failure(fmt.Errorf("review_runtime is required")).ToMap(), nil
	}
	ref := stringArg(args, "evolution_review_ref")
	if strings.TrimSpace(ref) == "" {
		return Failure(fmt.Errorf("evolution_review_ref is required")).ToMap(), nil
	}
	rawSelected, _ := args["selected_proposal_ids"].([]any)
	selected := make([]string, 0, len(rawSelected))
	for _, id := range rawSelected {
		s, ok := id.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return Failure(fmt.Errorf("selected_proposal_ids must contain non-empty strings")).ToMap(), nil
		}
		selected = append(selected, s)
	}
	if len(selected) == 0 {
		return Failure(fmt.Errorf("selected_proposal_ids must be a non-empty list")).ToMap(), nil
	}
	seen := make(map[string]bool, len(selected))
	for _, s := range selected {
		if seen[s] {
			return Failure(fmt.Errorf("selected_proposal_ids must not contain duplicates")).ToMap(), nil
		}
		seen[s] = true
	}
	sessionID := sessionIDFromOpts(args, opts)
	// Python: except KeyError → "unknown evolution_review_ref"（L376-377）。
	prepared, err := t.submissionService.PrepareEvolveSubmission(
		newSubmissionResolver(t.reviewRuntime), ref, subject, selected, sessionID)
	if err != nil {
		return Failure(fmt.Errorf("unknown evolution_review_ref")).ToMap(), nil
	}
	result, err := t.submissionService.ApplyPreparedEvolveSubmission(ctx, prepared, "agent_evolve_tool")
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	if success, ok := result["success"].(bool); ok && success {
		// Python L379-380: 成功才消费 prepared submission。
		selection, err := t.reviewRuntime.ResolveSelectedProposals(ref, subject, selected, sessionID)
		if err == nil {
			_ = t.reviewRuntime.ConsumePreparedSubmission(selection, sessionID)
		}
	}
	InjectMappedContent(result, t.ToOutput(result))
	return result, nil
}

// Invoke 执行精简动作。
// Python: SimplifySkillExperiencesTool.invoke (skill.py:392-408)。
func (t *simplifySkillExperiencesTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	args := InputsDict(inputs)
	subject, err := t.subjectFromInputs(args)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	rawActions, ok := args["actions"].([]any)
	if !ok || len(rawActions) == 0 {
		return Failure(fmt.Errorf("actions must be a non-empty list")).ToMap(), nil
	}
	// Python L399-401: 先 normalize 校验，保证无 Interrupt rail 直连安全。
	normalizedSubject, normErr := t.normalizeSubjectKind(subject)
	if normErr != nil {
		return Failure(normErr).ToMap(), nil
	}
	if _, err := experience.NormalizeSimplifyDraft(normalizedSubject, rawActions); err != nil {
		return Failure(err).ToMap(), nil
	}
	result, err := t.submissionService.ApplySimplifyActions(ctx, subject, subjectKindArg(args), rawActions)
	if err != nil {
		return Failure(err).ToMap(), nil
	}
	InjectMappedContent(result, t.ToOutput(result))
	return result, nil
}

// Stream 不支持流式（Python BaseEvolutionTool.stream 仅 yield 终态占位）。
func (t *evolutionTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Stream 不支持流式（同上）。
func (t *evolveReviewTaskTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// GetCallTimeout 沿用卡片级超时策略。
func (t *evolutionTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// GetCallTimeout 沿用卡片级超时策略。
func (t *evolveReviewTaskTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// Card 返回配置卡。
func (t *evolutionTool) Card() *tool.ToolCard { return t.card }

// Card 返回配置卡。
func (t *evolveReviewTaskTool) Card() *tool.ToolCard { return t.card }

// ──────────────────────────── 非导出函数 ────────────────────────────

// subjectFromInputs 提取并归一 subject。
// Python: _subject_from_inputs (skill.py:120-125)。
func (t *evolutionTool) subjectFromInputs(args map[string]any) (map[string]any, error) {
	raw, ok := args["subject"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("subject is required")
	}
	subjectMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("subject is required")
	}
	normalized, err := t.normalizeSubjectKind(subjectMap)
	if err != nil {
		return nil, err
	}
	return normalized.ToPayload(), nil
}

// normalizeSubjectKind 归一化 subject kind（ValidationError → ValueError 映射）。
// Python: _normalize_subject_kind (skill.py:127-131)。
func (t *evolutionTool) normalizeSubjectKind(subject map[string]any) (experience.EvolutionSubject, error) {
	return experience.NormalizeSubject(subject, nil)
}

// sessionIDFromOpts 提取会话 id。
// Python: _session_id (skill.py:133-140)——conversation_id || session_id ||
// session.get_session_id()。
func sessionIDFromOpts(args map[string]any, opts []tool.ToolOption) string {
	if v := stringArg(args, "conversation_id"); v != "" {
		return v
	}
	if v := stringArg(args, "session_id"); v != "" {
		return v
	}
	if len(opts) > 0 {
		callOpts := tool.NewToolCallOptions(opts...)
		var sess sessioninterfaces.SessionFacade
		if callOpts.Session != nil {
			sess = callOpts.Session
		}
		if sess != nil {
			return sess.GetSessionID()
		}
	}
	return ""
}

// stringArg 取字符串参数（缺失返回空串）。
func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// stringArgOr 取字符串参数（缺失/空回落默认值）。
func stringArgOr(args map[string]any, key, fallback string) string {
	if v := stringArg(args, key); v != "" {
		return v
	}
	return fallback
}

// intArgOr 取整数参数（缺失/非法回落默认值）。
func intArgOr(args map[string]any, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return fallback
}

// floatPtrArg 取可选浮点参数（缺失返回 nil）。
func floatPtrArg(args map[string]any, key string) *float64 {
	if v, ok := args[key].(float64); ok {
		return &v
	}
	return nil
}

// stringSliceArg 取字符串切片参数（缺失返回空切片）。
func stringSliceArg(args map[string]any, key string) []string {
	raw, _ := args[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, fmt.Sprintf("%v", v))
	}
	return out
}

// subjectKindArg 取 subject kind（Python subject payload 内的 kind 优先）。
func subjectKindArg(args map[string]any) string {
	if subject, ok := args["subject"].(map[string]any); ok {
		if kind, ok := subject["kind"].(string); ok {
			return kind
		}
	}
	return ""
}

// buildTaskDescription 构建审查子代理任务描述。
// Python: _build_task_description (skill.py:275-295)。
func buildTaskDescription(args map[string]any, ref string) string {
	lines := []string{
		"Perform the Skill evolution review for this prepared review ref.",
		fmt.Sprintf("evolution_review_ref: %s", ref),
		"If no task evidence is available but user_intent is present, you may propose review-direction-only " +
			"experience with evidence_refs=[].",
		"If no task evidence is available and user_intent is empty, submit outcome=no_evolution.",
		"Do not invent execution evidence.",
		"If task evidence is available, first read relevant task and experience evidence, then cite only " +
			"the refs you actually read.",
		"Use the available read-only review tools and call submit_evolution_review before the final answer.",
		"Return only the exact JSON produced by submit_evolution_review.",
	}
	if subject := stringArg(args, "subject"); subject != "" {
		lines = append(lines[:2], append([]string{fmt.Sprintf("subject: %s", subject)}, lines[2:]...)...)
	}
	if userIntent := strings.TrimSpace(stringArg(args, "user_intent")); userIntent != "" {
		lines = append(lines[:2], append([]string{fmt.Sprintf("user_intent: %s", userIntent)}, lines[2:]...)...)
	}
	return strings.Join(lines, "\n")
}

// newSubmissionResolver 构造 proposal 解析协作面适配器。
// Python 鸭子调用 review_runtime.resolve_selected_proposals 的 Go 适配
// （review.EvolutionProposalSelection → experience.PreparedEvolveSubmission）。
func newSubmissionResolver(rt *review.EvolutionReviewRuntime) experience.ProposalResolver {
	return &reviewRuntimeResolver{runtime: rt}
}

// reviewRuntimeResolver ProposalResolver 的 review 运行时适配器。
type reviewRuntimeResolver struct {
	runtime *review.EvolutionReviewRuntime
}

// ResolveSelectedProposals 解析选定 proposals。
func (r *reviewRuntimeResolver) ResolveSelectedProposals(
	ref string, subject map[string]any, selected []string, sessionID string,
) (*experience.PreparedEvolveSubmission, error) {
	selection, err := r.runtime.ResolveSelectedProposals(ref, subject, selected, sessionID)
	if err != nil {
		return nil, err
	}
	return &experience.PreparedEvolveSubmission{
		EvolutionReviewRef: selection.EvolutionReviewRef,
		Subject:            selection.Subject,
		ExperienceDrafts:   selection.ExperienceDrafts,
		RecordSource:       selection.RecordSource,
	}, nil
}
