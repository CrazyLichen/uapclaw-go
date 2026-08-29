package experience

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingGovernance 暂存治理操作条目。
//
// 对应 Python: ExperienceManager._pending_governance 内层的 dict
type PendingGovernance struct {
	// Kind 操作类型（当前仅 "simplify"）
	Kind string
	// SkillName 技能名称
	SkillName string
	// Actions 整理操作列表（来自 LLM 输出，保持 []map[string]any）
	Actions []map[string]any
}

// ExperienceManager 编排技能/团队技能的在线演进生命周期。
//
// 对应 Python: openjiuwen/agent_evolving/experience/skill_experience_manager.py ExperienceManager
type ExperienceManager struct {
	// store 所属的 EvolutionStore
	store *checkpointing.EvolutionStore
	// scorer 经验评分器
	scorer *ExperienceScorer
	// kind 类型标识（"skill" 或 "team-skill"）
	kind string
	// language 语言（"cn" 或 "en"）
	language string
	// skillOps 技能经验操作器映射
	skillOps map[string]*skill_call.SkillExperienceOperator
	// pendingApprovalSnapshots 暂存审批快照映射（调用方拥有）
	pendingApprovalSnapshots map[string]*PendingChange
	// pendingGovernance 暂存治理操作映射
	pendingGovernance map[string]*PendingGovernance
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// supportedKindSkill 技能类型标识
	supportedKindSkill = "skill"
	// supportedKindTeamSkill 团队技能类型标识
	supportedKindTeamSkill = "team-skill"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// supportedKinds 支持的类型集合
var supportedKinds = map[string]bool{
	supportedKindSkill:     true,
	supportedKindTeamSkill: true,
}

// rebuildPromptTemplates 重建提示词模板（双语，一比一复刻 Python 原文）。
//
// Python 中的 {evolution_records}、{user_intent}、{min_score} 占位符
// 在 Go 中通过 fmt.Sprintf 替换（使用 %s/%s/%g）。
//
// 对应 Python: ExperienceManager._REBUILD_PROMPT_TEMPLATES
var rebuildPromptTemplates = map[string]map[string]string{
	supportedKindSkill: {
		"cn": "你收到了一个技能的重建请求。旧版本已归档，请执行以下步骤：\n\n" +
			"## 已筛选的历史演进经验（score >= %g）\n\n" +
			"%s\n\n" +
			"## 用户意图\n\n" +
			"%s\n\n" +
			"## 执行要求\n\n" +
			"请调用 skill-creator 技能：\n" +
			"1. 基于以上历史经验和用户意图，生成新的 SKILL.md\n" +
			"2. 重置 evolutions.json 为空列表\n\n" +
			"旧版本已归档至 archive/ 目录，可直接创建新版本。",
		"en": "You received a skill rebuild request. Old version has been archived. Please follow these steps:\n\n" +
			"## Filtered Historical Evolution Records (score >= %g)\n\n" +
			"%s\n\n" +
			"## User Intent\n\n" +
			"%s\n\n" +
			"## Execution Requirements\n\n" +
			"Please invoke the skill-creator skill:\n" +
			"1. Generate new SKILL.md based on the historical records and user intent above\n" +
			"2. Reset evolutions.json to empty list\n\n" +
			"Old version has been archived to archive/ directory, you can directly create the new version.",
	},
	supportedKindTeamSkill: {
		"cn": "你收到了一个团队技能的重建请求。旧版本已归档，请执行以下步骤：\n\n" +
			"## 已筛选的历史演进经验（score >= %g）\n\n" +
			"%s\n\n" +
			"## 用户意图\n\n" +
			"%s\n\n" +
			"## 执行要求\n\n" +
			"请调用 teamskill-creator 技能：\n" +
			"1. 基于以上历史经验和用户意图，生成新的 SKILL.md\n" +
			"2. 重置 evolutions.json 为空列表\n\n" +
			"旧版本已归档至 archive/ 目录，可直接创建新版本。",
		"en": "You received a team skill rebuild request. Old version has been archived. " +
			"Please follow these steps:\n\n" +
			"## Filtered Historical Evolution Records (score >= %g)\n\n" +
			"%s\n\n" +
			"## User Intent\n\n" +
			"%s\n\n" +
			"## Execution Requirements\n\n" +
			"Please invoke the teamskill-creator skill:\n" +
			"1. Generate new SKILL.md based on the historical records and user intent above\n" +
			"2. Reset evolutions.json to empty list\n\n" +
			"Old version has been archived to archive/ directory, you can directly create the new version.",
	},
}

// defaultRebuildIntents 默认重建意图（双语，一比一复刻 Python 原文）。
//
// 对应 Python: ExperienceManager._DEFAULT_REBUILD_INTENTS
var defaultRebuildIntents = map[string]map[string]string{
	supportedKindSkill: {
		"cn": "根据以上演进经验，对技能进行全面优化和重建。",
		"en": "Based on the evolution records above, perform a comprehensive rebuild of the skill.",
	},
	supportedKindTeamSkill: {
		"cn": "根据以上演进经验，对团队技能进行全面优化和重建。",
		"en": "Based on the evolution records above, perform a comprehensive rebuild of the team skill.",
	},
}

// applyUpdatesFn 包级注入函数，由 orchestrator 设置以桥接 evolving.ExecuteUpdates
var applyUpdatesFn func(map[string]operator.Operator, map[schema.UpdateKey]any) []schema.ApplyResult

// ──────────────────────────── 导出函数 ────────────────────────────

// SetApplyUpdatesFn 设置包级注入函数。
// 由 orchestrator 调用，桥接 evolving 包的 ExecuteUpdates，
// 避免 experience ↔ evolving 循环依赖。
func SetApplyUpdatesFn(fn func(map[string]operator.Operator, map[schema.UpdateKey]any) []schema.ApplyResult) {
	applyUpdatesFn = fn
}

// NewExperienceManager 创建 ExperienceManager 实例。
//
// 对应 Python: ExperienceManager.__init__(store, scorer, kind, language, skill_ops, pending_approval_snapshots, pending_governance)
func NewExperienceManager(
	store *checkpointing.EvolutionStore,
	scorer *ExperienceScorer,
	kind string,
	language string,
	skillOps map[string]*skill_call.SkillExperienceOperator,
	pendingApprovalSnapshots map[string]*PendingChange,
	pendingGovernance map[string]*PendingGovernance,
) (*ExperienceManager, error) {
	if !supportedKinds[kind] {
		return nil, fmt.Errorf("不支持的体验管理器类型: %s", kind)
	}

	em := &ExperienceManager{
		store:                    store,
		scorer:                   scorer,
		kind:                     kind,
		language:                 language,
		skillOps:                 skillOps,
		pendingApprovalSnapshots: map[string]*PendingChange{},
		pendingGovernance:        pendingGovernance,
	}
	if em.skillOps == nil {
		em.skillOps = map[string]*skill_call.SkillExperienceOperator{}
	}
	if em.pendingGovernance == nil {
		em.pendingGovernance = map[string]*PendingGovernance{}
	}

	em.BindPendingApprovalSnapshots(pendingApprovalSnapshots)
	return em, nil
}

// PendingApprovalSnapshots 返回暂存审批快照映射。
// 对应 Python: ExperienceManager.pending_approval_snapshots (property)
func (m *ExperienceManager) PendingApprovalSnapshots() map[string]*PendingChange {
	return m.pendingApprovalSnapshots
}

// PendingGovernance 返回暂存治理操作映射。
// 对应 Python: ExperienceManager.pending_governance (property)
func (m *ExperienceManager) PendingGovernance() map[string]*PendingGovernance {
	return m.pendingGovernance
}

// SkillOps 返回技能经验操作器映射。
// 对应 Python: ExperienceManager.skill_ops (property)
func (m *ExperienceManager) SkillOps() map[string]*skill_call.SkillExperienceOperator {
	return m.skillOps
}

// BindPendingApprovalSnapshots 绑定调用方拥有的暂存快照存储。
//
// 对应 Python: ExperienceManager.bind_pending_approval_snapshots()
func (m *ExperienceManager) BindPendingApprovalSnapshots(pendingApprovalSnapshots map[string]*PendingChange) {
	if pendingApprovalSnapshots != nil {
		m.pendingApprovalSnapshots = pendingApprovalSnapshots
	} else {
		m.pendingApprovalSnapshots = map[string]*PendingChange{}
	}
}

// StageRecords 将一批演进记录暂存到审批状态。
//
// 对应 Python: ExperienceManager.stage_records()
func (m *ExperienceManager) StageRecords(
	ctx context.Context,
	skillName string,
	records []checkpointing.EvolutionRecord,
	requiresApproval bool,
	source string,
	userQuery string,
	signalType *string,
	signalSource *string,
	changeType string,
	requestIDPrefix string,
	trajectory *trajectory.Trajectory,
	messages []map[string]any,
	isSharedRecords bool,
) (*ExperienceApprovalRequest, error) {
	proposal := ExperienceProposal{
		SkillName:        skillName,
		Records:          records,
		RequiresApproval: requiresApproval,
		Source:           source,
		UserQuery:        userQuery,
		SignalType:       signalType,
		SignalSource:     signalSource,
	}
	return m.stageRecordsInternal(
		ctx,
		proposal,
		records,
		changeType,
		requestIDPrefix,
		trajectory,
		messages,
		isSharedRecords,
	)
}

// StageApplyResults 将已生成的在线应用结果通过共享生命周期暂存。
//
// 对应 Python: ExperienceManager.stage_apply_results()
func (m *ExperienceManager) StageApplyResults(
	ctx context.Context,
	skillName string,
	applyResults []schema.ApplyResult,
	requiresApproval bool,
	source string,
	requestIDPrefix string,
	userQuery string,
	signalType *string,
	signalSource *string,
	messages []map[string]any,
) (*ExperienceApprovalRequest, error) {
	preview, err := BuildLocalApplyPreview(skillName, applyResults)
	if err != nil {
		return nil, fmt.Errorf("构建本地应用预览失败: %w", err)
	}

	proposal := ExperienceProposal{
		SkillName:        skillName,
		Records:          preview.Records,
		RequiresApproval: requiresApproval,
		Source:           source,
		UserQuery:        userQuery,
		SignalType:       signalType,
		SignalSource:     signalSource,
	}
	return m.stagePendingRequest(
		proposal,
		preview,
		requestIDPrefix,
		nil,
		messages,
		false,
	)
}

// ApproveRequest 应用暂存审批批次到持久化存储。
//
// 对应 Python: ExperienceManager.approve_request()
func (m *ExperienceManager) ApproveRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) {
	return m.applyRequest(ctx, requestID, schema.ApproveAction)
}

// RejectRequest 拒绝并丢弃暂存审批批次。
//
// 对应 Python: ExperienceManager.reject_request()
func (m *ExperienceManager) RejectRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) {
	return m.applyRequest(ctx, requestID, schema.RejectAction)
}

// RetryRequest 重试部分应用的暂存审批批次。
//
// 对应 Python: ExperienceManager.retry_request()
func (m *ExperienceManager) RetryRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) {
	return m.applyRequest(ctx, requestID, schema.RetryAction)
}

// CommitProposal 通过共享暂存生命周期持久化一个已生成的提案。
//
// 对应 Python: ExperienceManager.commit_proposal()
func (m *ExperienceManager) CommitProposal(
	ctx context.Context,
	proposal ExperienceProposal,
) (ExperienceApplyResult, error) {
	request, err := m.StageRecords(
		ctx,
		proposal.SkillName,
		proposal.Records,
		proposal.RequiresApproval,
		proposal.Source,
		proposal.UserQuery,
		proposal.SignalType,
		proposal.SignalSource,
		schema.SkillExperienceEntry,
		"",
		nil,
		nil,
		false,
	)
	if err != nil {
		return ExperienceApplyResult{}, err
	}
	return m.commitStagedRequest(ctx, *request)
}

// RequestSimplify 为技能暂存整理治理操作。
//
// 对应 Python: ExperienceManager.request_simplify()
func (m *ExperienceManager) RequestSimplify(
	ctx context.Context,
	skillName string,
	userIntent *string,
) (string, error) {
	startedAt := time.Now()

	if !m.store.SkillExists(ctx, skillName) {
		logger.Info(logComponent).
			Str("kind", m.kind).
			Str("skill", skillName).
			Str("reason", "skill_not_found").
			Msg("[ExperienceManager] request_simplify skipped")
		return "", nil
	}

	evoLog, err := m.store.LoadFullEvolutionLog(ctx, skillName)
	if err != nil {
		return "", fmt.Errorf("load evolution log for request_simplify: %w", err)
	}
	records := evoLog.Entries
	if len(records) == 0 {
		logger.Info(logComponent).
			Str("kind", m.kind).
			Str("skill", skillName).
			Str("reason", "no_records").
			Msg("[ExperienceManager] request_simplify skipped")
		return "", nil
	}

	content, err := m.store.ReadSkillContent(ctx, skillName)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill", skillName).
			Err(err).
			Msg("[ExperienceManager] request_simplify: read_skill_content 失败")
		return "", nil
	}

	summary := checkpointing.ExtractDescriptionFromSkillMD(content)

	logger.Info(logComponent).
		Str("kind", m.kind).
		Str("skill", skillName).
		Int("records", len(records)).
		Msg("[ExperienceManager] request_simplify loaded records")

	actions, err := m.scorer.Simplify(ctx, skillName, summary, records, userIntent)
	if err != nil || len(actions) == 0 {
		elapsed := time.Since(startedAt).Seconds()
		logger.Info(logComponent).
			Str("kind", m.kind).
			Str("skill", skillName).
			Float64("elapsed", elapsed).
			Msg("[ExperienceManager] request_simplify finished without actions")
		return "", nil
	}

	requestID := fmt.Sprintf("evolve_simplify_%08x", time.Now().UTC().UnixNano()&0xFFFFFFFF)
	m.pendingGovernance[requestID] = &PendingGovernance{
		Kind:      "simplify",
		SkillName: skillName,
		Actions:   actions,
	}

	elapsed := time.Since(startedAt).Seconds()
	logger.Info(logComponent).
		Str("kind", m.kind).
		Str("skill", skillName).
		Str("request", requestID).
		Int("actions", len(actions)).
		Float64("elapsed", elapsed).
		Msg("[ExperienceManager] request_simplify staged")

	return requestID, nil
}

// ApproveSimplify 执行暂存的整理治理操作。
//
// 对应 Python: ExperienceManager.approve_simplify()
func (m *ExperienceManager) ApproveSimplify(ctx context.Context, requestID string) (map[string]int, error) {
	gov := m.pendingGovernance[requestID]
	if gov == nil {
		return map[string]int{}, nil
	}
	delete(m.pendingGovernance, requestID)

	return ExecuteSimplifyActions(ctx, m.store, gov.SkillName, gov.Actions), nil
}

// RejectSimplify 丢弃暂存的整理治理操作。
//
// 对应 Python: ExperienceManager.reject_simplify()
func (m *ExperienceManager) RejectSimplify(requestID string) {
	delete(m.pendingGovernance, requestID)
}

// RequestRebuild 为技能准备重建提示词。
//
// 对应 Python: ExperienceManager.request_rebuild()
func (m *ExperienceManager) RequestRebuild(
	ctx context.Context,
	skillName string,
	userIntent *string,
	minScore float64,
) (string, error) {
	template := m.getRebuildTemplate()
	defaultIntent := m.getDefaultRebuildIntent()

	contextResult, err := RequestRebuildContext(
		ctx,
		m.store,
		RebuildRequest{
			SkillName:  skillName,
			UserIntent: userIntent,
			MinScore:   minScore,
		},
		func(records []checkpointing.EvolutionRecord) string {
			return FormatEvolutionRecords(records, m.language)
		},
		defaultIntent,
		template,
		true,
	)
	if err != nil {
		return "", err
	}
	if contextResult == nil {
		return "", nil
	}
	prompt, _ := contextResult["prompt"].(string)
	return prompt, nil
}

// BuildLocalApplyPreview 从应用结果构建稳定的本地应用预览合约。
//
// 静态导出方法，对应 Python: ExperienceManager.build_local_apply_preview()
func BuildLocalApplyPreview(
	skillName string,
	applyResults []schema.ApplyResult,
) (LocalApplyPreview, error) {
	records := []checkpointing.EvolutionRecord{}
	changeType := schema.SkillExperienceEntry

	for _, result := range applyResults {
		if !result.Applied {
			continue
		}
		if result.LifecycleStage != nil && *result.LifecycleStage != schema.LocalApplyCompleted {
			return LocalApplyPreview{}, fmt.Errorf("%s 不支持的 apply 生命周期阶段: %s", skillName, *result.LifecycleStage)
		}
		// 对齐 Python: records.extend(result.records) — 需将 []any 转为 []EvolutionRecord
		for _, item := range result.Records {
			if record, ok := item.(checkpointing.EvolutionRecord); ok {
				records = append(records, record)
			}
		}
		if result.ChangeType != nil && *result.ChangeType != "" {
			changeType = *result.ChangeType
		}
	}

	return LocalApplyPreview{
		SkillName:    skillName,
		Records:      records,
		ApplyResults: applyResults,
		ChangeType:   changeType,
	}, nil
}

// FormatEvolutionRecords 格式化演进记录列表为文本。
//
// 静态导出方法，对应 Python: ExperienceManager.format_evolution_records()
func FormatEvolutionRecords(records []checkpointing.EvolutionRecord, language string) string {
	var header, contentLabel, empty string
	if language == "en" {
		header = "Experience"
		contentLabel = "Content"
		empty = "(no evolution records)"
	} else {
		header = "经验"
		contentLabel = "内容"
		empty = "（无演进经验）"
	}

	lines := []string{}
	for idx, record := range records {
		section := record.Change.Section
		if section == "" {
			section = "?"
		}
		content := record.Change.Content
		source := record.Source
		if source == "" {
			source = "unknown"
		}
		timestamp := record.Timestamp
		score := record.Score
		lines = append(lines, fmt.Sprintf(
			"### %s #%d [%s] - source: %s, score: %.2f\n- Section: %s\n- %s: %s",
			header, idx+1, timestamp, source, score, section, contentLabel, content,
		))
	}

	if len(lines) == 0 {
		return empty
	}

	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n\n"
		}
		result += line
	}
	return result
}

// ApplyUpdatesFromManager 兼容钩子，执行在线预览更新。
//
// 对应 Python: ExperienceManager.apply_updates() (staticmethod)
func ApplyUpdatesFromManager(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]schema.UpdateValue,
) []schema.ApplyResult {
	return evolvingExecuteUpdates(operators, updates)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updatesToAnyMap 将 UpdateValue map 转换为桥接函数所需的 map[schema.UpdateKey]any 格式。
func updatesToAnyMap(updates map[schema.UpdateKey]schema.UpdateValue) map[schema.UpdateKey]any {
	result := make(map[schema.UpdateKey]any, len(updates))
	for key, value := range updates {
		result[key] = value
	}
	return result
}

// stageRecordsInternal 共享暂存流程（技能/团队技能审批批次）。
//
// 对应 Python: ExperienceManager._stage_records()
func (m *ExperienceManager) stageRecordsInternal(
	ctx context.Context,
	proposal ExperienceProposal,
	records []checkpointing.EvolutionRecord,
	changeType string,
	requestIDPrefix string,
	trajectory *trajectory.Trajectory,
	messages []map[string]any,
	isSharedRecords bool,
) (*ExperienceApprovalRequest, error) {
	operator, ok := m.skillOps[proposal.SkillName]
	if !ok {
		operator = skill_call.NewSkillExperienceOperator(proposal.SkillName)
	}

	updateValue := schema.UpdateValue{
		Payload:    records,
		Mode:       schema.UpdateModeAppend,
		Effect:     schema.UpdateEffectPendingChange,
		ChangeType: &changeType,
	}

	applyResults := m.previewApplyResults(ctx, proposal.SkillName, operator, updateValue)
	preview, err := BuildLocalApplyPreview(proposal.SkillName, applyResults)
	if err != nil {
		return nil, fmt.Errorf("构建本地应用预览失败: %w", err)
	}

	return m.stagePendingRequest(
		proposal,
		preview,
		requestIDPrefix,
		trajectory,
		messages,
		isSharedRecords,
	)
}

// stagePendingRequest 从预览应用结果暂存一个待审批请求。
//
// 对应 Python: ExperienceManager._stage_pending_request()
func (m *ExperienceManager) stagePendingRequest(
	proposal ExperienceProposal,
	preview LocalApplyPreview,
	requestIDPrefix string,
	trajectory *trajectory.Trajectory,
	messages []map[string]any,
	isSharedRecords bool,
) (*ExperienceApprovalRequest, error) {
	pending := makePendingChangeFromPreview(
		preview,
		requestIDPrefix,
		trajectory,
		messages,
		isSharedRecords,
	)
	stagedPending := m.stagePendingChange(pending)

	logger.Info(logComponent).
		Str("change_id", stagedPending.ChangeID).
		Int("records", len(stagedPending.Payload)).
		Str("skill", proposal.SkillName).
		Msg("[ExperienceManager] staged approval request")

	requestID := stagedPending.ChangeID
	return &ExperienceApprovalRequest{
		SkillName:     proposal.SkillName,
		Proposal:      proposal,
		PendingChange: stagedPending,
		RequestID:     requestID,
		ApplyResults:  preview.ApplyResults,
	}, nil
}

// applyRequest 共享请求生命周期（approve/reject/retry）。
//
// 对应 Python: ExperienceManager._apply_request()
func (m *ExperienceManager) applyRequest(
	ctx context.Context,
	requestID string,
	action string,
) (ExperienceApplyResult, error) {
	pending := m.pendingApprovalSnapshots[requestID]
	if pending == nil {
		logger.Warn(logComponent).
			Str("action", action).
			Str("request_id", requestID).
			Msg("[ExperienceManager] unknown request_id")
		return ExperienceApplyResult{
			SkillName: "",
			Errors:    []string{fmt.Sprintf("unknown request_id: %s", requestID)},
		}, nil
	}

	if action == schema.RejectAction {
		m.rejectPendingChange(requestID)
		return RejectPendingChange(pending), nil
	}

	commitResult, err := m.commitPendingChange(ctx, requestID)
	if err != nil {
		return ExperienceApplyResult{}, err
	}
	return toApplyResult(pending.SkillName, commitResult), nil
}

// commitStagedRequest 通过共享暂存生命周期提交一个暂存请求。
//
// 对应 Python: ExperienceManager._commit_staged_request()
func (m *ExperienceManager) commitStagedRequest(
	ctx context.Context,
	request ExperienceApprovalRequest,
) (ExperienceApplyResult, error) {
	if request.RequestID == "" {
		return ExperienceApplyResult{}, fmt.Errorf("暂存请求缺少 request_id")
	}

	result, err := m.ApproveRequest(ctx, request.RequestID)
	if err != nil {
		return result, err
	}
	if !result.Ok() {
		return result, fmt.Errorf(
			"自动提交失败: skill=%s, request_id=%s, applied=%d, pending=%d, errors=%v",
			request.SkillName, request.RequestID,
			result.AppliedCount, result.PendingCount, result.Errors,
		)
	}
	return result, nil
}

// previewApplyResults 预览在线应用结果，不进入暂存或持久化。
//
// 对应 Python: ExperienceManager._preview_apply_results()
func (m *ExperienceManager) previewApplyResults(
	ctx context.Context,
	skillName string,
	op *skill_call.SkillExperienceOperator,
	update schema.UpdateValue,
) []schema.ApplyResult {
	return ApplyUpdatesFromManager(
		map[string]operator.Operator{op.OperatorID(): op},
		map[schema.UpdateKey]schema.UpdateValue{
			schema.UpdateKey{op.OperatorID(), schema.ExperiencesTarget}: update,
		},
	)
}

// evolvingExecuteUpdates 引用 evolving 包的 ExecuteUpdates。
// 由于 experience 包是 evolving 包的子包，不能直接导入 evolving 包（循环依赖），
// 此函数将在 orchestrator.go 中通过包级变量注入实现。
// 当前实现直接调用，由 orchestrator 负责桥接。
func evolvingExecuteUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]schema.UpdateValue,
) []schema.ApplyResult {
	// 对齐 Python: execute_updates(operators, updates)
	// 实际实现由外层 evolving 包的 ExecuteUpdates 提供，
	// 此处通过包级变量注入避免循环依赖
	if applyUpdatesFn != nil {
		return applyUpdatesFn(operators, updatesToAnyMap(updates))
	}
	// 无注入时的默认行为：逐个应用
	return defaultExecuteUpdates(operators, updates)
}

// makePendingChangeFromPreview 从预览结果构建暂存变更。
//
// 静态导出方法，对应 Python: ExperienceManager._make_pending_change_from_preview()
func makePendingChangeFromPreview(
	preview LocalApplyPreview,
	requestIDPrefix string,
	trajectory *trajectory.Trajectory,
	messages []map[string]any,
	isSharedRecords bool,
) *PendingChange {
	// 对齐 Python: make_pending_change 返回的 pending 对象需要转换 Records 类型
	pending := MakePendingChange(
		preview.SkillName,
		preview.Records,
		requestIDPrefix,
		trajectory,
		messages,
		isSharedRecords,
	)
	pending.ChangeType = preview.ChangeType
	return pending
}

// stagePendingChange 在调用方拥有的快照存储中注册一个暂存变更。
//
// 对应 Python: ExperienceManager._stage_pending_change()
func (m *ExperienceManager) stagePendingChange(pending *PendingChange) *PendingChange {
	m.pendingApprovalSnapshots[pending.ChangeID] = pending
	return pending
}

// rejectPendingChange 从快照存储中移除一个暂存变更，不持久化。
//
// 对应 Python: ExperienceManager._reject_pending_change()
func (m *ExperienceManager) rejectPendingChange(changeID string) *PendingChange {
	pending := m.pendingApprovalSnapshots[changeID]
	delete(m.pendingApprovalSnapshots, changeID)
	if pending == nil {
		panic(fmt.Sprintf("key error: %s", changeID))
	}
	return pending
}

// commitPendingChange 持久化暂存变更，部分失败时保留未写入尾部。
//
// 对应 Python: ExperienceManager._commit_pending_change()
func (m *ExperienceManager) commitPendingChange(
	ctx context.Context,
	changeID string,
) (PendingCommitResult, error) {
	return CommitPendingChange(ctx, m.pendingApprovalSnapshots, changeID, m.store)
}

// getRebuildTemplate 获取重建提示词模板。
//
// 对应 Python: ExperienceManager._get_rebuild_template()
func (m *ExperienceManager) getRebuildTemplate() string {
	templates, ok := rebuildPromptTemplates[m.kind]
	if !ok {
		templates = rebuildPromptTemplates[supportedKindSkill]
	}
	template, ok := templates[m.language]
	if !ok {
		template = templates["en"]
	}
	return template
}

// getDefaultRebuildIntent 获取默认重建意图。
//
// 对应 Python: ExperienceManager._get_default_rebuild_intent()
func (m *ExperienceManager) getDefaultRebuildIntent() string {
	intents, ok := defaultRebuildIntents[m.kind]
	if !ok {
		intents = defaultRebuildIntents[supportedKindSkill]
	}
	intent, ok := intents[m.language]
	if !ok {
		intent = intents["en"]
	}
	return intent
}

// toApplyResult 将 PendingCommitResult 转为 ExperienceApplyResult。
//
// 静态方法，对应 Python: ExperienceManager._to_apply_result()
func toApplyResult(skillName string, result PendingCommitResult) ExperienceApplyResult {
	return ExperienceApplyResult{
		SkillName:    skillName,
		AppliedCount: result.AppliedCount,
		PendingCount: result.PendingCount,
	}
}

// defaultExecuteUpdates 无注入时的默认更新执行逻辑。
// 对齐 evolving.ExecuteUpdates 的核心流程：归一化后逐一应用到 operator。
func defaultExecuteUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]schema.UpdateValue,
) []schema.ApplyResult {
	var results []schema.ApplyResult

	// 归一化后逐一应用
	normalized := schema.NormalizeUpdates(updatesToAnyMap(updates))
	for key, update := range normalized {
		op, ok := operators[key.OperatorID()]
		if !ok {
			results = append(results, schema.ApplyResult{
				OperatorID: key.OperatorID(),
				Target:     key.Target(),
				Applied:    false,
				Mode:       update.Mode,
				Effect:     update.Effect,
				Value:      update.Payload,
				ChangeType: update.ChangeType,
				Records:    []any{},
				Errors:     []string{fmt.Sprintf("operator not found: %s", key.OperatorID())},
				Metadata:   schema.MetadataClone(update.Metadata),
			})
			continue
		}
		results = append(results, op.ApplyUpdate(key.Target(), update))
	}

	return results
}
