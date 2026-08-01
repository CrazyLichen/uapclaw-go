package experience

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/evolving/updater/single_dim"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnlineEvolutionOrchestrator 在线演进流水线协调器。
//
// 对应 Python: OnlineEvolutionOrchestrator
//
// Manager 仍然是生命周期状态的所有者；此类仅编排
// 上下文构建、更新生成、本地预览、暂存和可选自动审批。
type OnlineEvolutionOrchestrator struct {
	// store 所属的 EvolutionStore
	store *checkpointing.EvolutionStore
	// updater 单维更新器
	updater *single_dim.SingleDimUpdater
	// manager 经验生命周期管理器
	manager *ExperienceManager
	// skillOps 技能经验 Operator 映射
	skillOps map[string]*skill_call.SkillExperienceOperator
	// requestIDPrefix 请求标识前缀
	requestIDPrefix string
	// stageSource 暂存来源标识
	stageSource string
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOnlineEvolutionOrchestrator 创建在线演进编排器。
//
// 对应 Python: OnlineEvolutionOrchestrator.__init__()
func NewOnlineEvolutionOrchestrator(
	store *checkpointing.EvolutionStore,
	updater *single_dim.SingleDimUpdater,
	manager *ExperienceManager,
	skillOps map[string]*skill_call.SkillExperienceOperator,
	requestIDPrefix string,
	stageSource string,
) *OnlineEvolutionOrchestrator {
	return &OnlineEvolutionOrchestrator{
		store:           store,
		updater:         updater,
		manager:         manager,
		skillOps:        skillOps,
		requestIDPrefix: requestIDPrefix,
		stageSource:     stageSource,
	}
}

// Evolve 执行在线演进并返回结构化结果。
//
// 对应 Python: OnlineEvolutionOrchestrator.evolve()
func (o *OnlineEvolutionOrchestrator) Evolve(
	ctx context.Context,
	skillName string,
	signals []signal.EvolutionSignal,
	messages []map[string]any,
	userQuery string,
	trajectoryArg *trajectory.Trajectory,
	requiresApproval bool,
	metadata map[string]any,
	source *string,
) (*OnlineEvolutionResult, error) {
	// 对齐 Python: 前置检查
	if skillName == "" || len(signals) == 0 {
		return &OnlineEvolutionResult{
			SkillName: skillName,
			Status:    OnlineEvolutionStatusSkippedNoInput,
			Message:   "online evolution skipped because skill_name or signals are empty",
		}, nil
	}
	if !o.store.SkillExists(ctx, skillName) {
		return &OnlineEvolutionResult{
			SkillName: skillName,
			Status:    OnlineEvolutionStatusSkippedSkillNotFound,
			Message:   fmt.Sprintf("online evolution skipped because skill '%s' does not exist", skillName),
		}, nil
	}

	// 对齐 Python: 获取或创建 SkillExperienceOperator
	op := o.skillOps[skillName]
	if op == nil {
		op = skill_call.NewSkillExperienceOperator(skillName)
		o.skillOps[skillName] = op
	}

	// 对齐 Python: 构建上下文
	onlineContext, err := o.buildContext(ctx, skillName, signals, messages, userQuery, trajectoryArg, metadata)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: 生成 LocalApplyPreview
	preview, err := o.generateLocalApplyPreview(ctx, op, onlineContext)
	if err != nil {
		return nil, err
	}

	if len(preview.Records) == 0 {
		message := fmt.Sprintf("no applied updates for skill=%s", skillName)
		logger.Info(logger.ComponentAgentCore).
			Str("skill", skillName).
			Msg(message)
		return &OnlineEvolutionResult{
			SkillName: skillName,
			Status:    OnlineEvolutionStatusNoEvolutionNoRecords,
			Message:   message,
		}, nil
	}

	// 对齐 Python: stage_apply_results
	stageSource := o.stageSource
	if source != nil && *source != "" {
		stageSource = *source
	}
	signalType := getSignalType(onlineContext)
	signalSource := getSignalSource(onlineContext)
	var messagesCopy []map[string]any
	if onlineContext.Messages != nil {
		messagesCopy = make([]map[string]any, len(onlineContext.Messages))
		for i, m := range onlineContext.Messages {
			messagesCopy[i] = m
		}
	}

	request := o.manager.StageApplyResults(
		ctx, skillName, preview.ApplyResults,
		requiresApproval, stageSource, &o.requestIDPrefix,
		onlineContext.UserQuery, &signalType, &signalSource,
		messagesCopy,
	)

	if requiresApproval {
		return &OnlineEvolutionResult{
			SkillName: skillName,
			Status:    OnlineEvolutionStatusStaged,
			Request:   &request,
			Message:   fmt.Sprintf("evolution request staged for skill=%s", skillName),
		}, nil
	}

	// 对齐 Python: auto-approve
	requestID := ""
	if request.RequestID != nil {
		requestID = *request.RequestID
	}
	result, err := o.manager.ApproveRequest(ctx, requestID)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("skill", skillName).
			Str("request_id", requestID).
			Err(err).
			Msg("[OnlineEvolutionOrchestrator] auto-approve 失败")
		// 对齐 Python: auto-approve 失败不返回 error，仍返回 auto_approved 状态
	}
	if result.AppliedCount > 0 || result.PendingCount > 0 {
		logger.Info(logger.ComponentAgentCore).
			Str("skill", skillName).
			Int("applied_count", result.AppliedCount).
			Int("pending_count", result.PendingCount).
			Msg("[OnlineEvolutionOrchestrator] auto-approve 完成")
	}

	return &OnlineEvolutionResult{
		SkillName: skillName,
		Status:    OnlineEvolutionStatusAutoApproved,
		Request:   &request,
		Message:   fmt.Sprintf("evolution request auto-approved for skill=%s", skillName),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildContext 构建 EvolutionContext。
// 对应 Python: OnlineEvolutionOrchestrator._build_context()
func (o *OnlineEvolutionOrchestrator) buildContext(
	ctx context.Context,
	skillName string,
	signals []signal.EvolutionSignal,
	messages []map[string]any,
	userQuery string,
	trajectoryArg *trajectory.Trajectory,
	metadata map[string]any,
) (*EvolutionContext, error) {
	skillContent, err := o.store.ReadSkillContent(ctx, skillName)
	if err != nil {
		return nil, err
	}

	descTarget := signal.EvolutionTargetDescription
	bodyTarget := signal.EvolutionTargetBody
	scriptTarget := signal.EvolutionTargetScript

	existingDescRecords := o.store.GetPendingRecords(ctx, skillName, &descTarget)
	existingBodyRecords := o.store.GetPendingRecords(ctx, skillName, &bodyTarget)
	existingScriptRecords := o.store.GetPendingRecords(ctx, skillName, &scriptTarget)

	var messagesCopy []map[string]any
	if messages != nil {
		messagesCopy = make([]map[string]any, len(messages))
		for i, m := range messages {
			messagesCopy[i] = m
		}
	}

	var metadataCopy map[string]any
	if metadata != nil {
		metadataCopy = make(map[string]any, len(metadata))
		for k, v := range metadata {
			metadataCopy[k] = v
		}
	}

	return &EvolutionContext{
		SkillName:             skillName,
		Signals:               signals,
		Messages:              messagesCopy,
		UserQuery:             userQuery,
		SkillContent:          skillContent,
		ExistingDescRecords:   existingDescRecords,
		ExistingBodyRecords:   existingBodyRecords,
		ExistingScriptRecords: existingScriptRecords,
		Trajectory:            trajectoryArg,
		Metadata:              metadataCopy,
	}, nil
}

// generateLocalApplyPreview 生成 LocalApplyPreview。
// 对应 Python: OnlineEvolutionOrchestrator._generate_local_apply_preview()
func (o *OnlineEvolutionOrchestrator) generateLocalApplyPreview(
	ctx context.Context,
	op *skill_call.SkillExperienceOperator,
	onlineContext *EvolutionContext,
) (*LocalApplyPreview, error) {
	// 对齐 Python: updater.bind()
	operators := map[string]operator.Operator{
		op.OperatorID(): op,
	}
	config := map[string]any{
		"online_contexts": map[string]*EvolutionContext{
			onlineContext.SkillName: onlineContext,
		},
	}
	o.updater.Bind(operators, []string{schema.ExperiencesTarget}, config)

	// 对齐 Python: 构建 trajectories 列表
	var trajectories []*trajectory.Trajectory
	if onlineContext.Trajectory != nil {
		trajectories = []*trajectory.Trajectory{onlineContext.Trajectory}
	}

	// 对齐 Python: 将 EvolutionSignal 转为指针切片
	signalPtrs := make([]*signal.EvolutionSignal, len(onlineContext.Signals))
	for i := range onlineContext.Signals {
		signalPtrs[i] = &onlineContext.Signals[i]
	}

	// 对齐 Python: updater.process()
	updates, err := o.updater.Process(ctx, trajectories, signalPtrs, map[string]any{})
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("skill", onlineContext.SkillName).
			Err(err).
			Msg("[OnlineEvolutionOrchestrator] updater.Process 失败")
		return nil, err
	}

	// 对齐 Python: execute_updates → BuildLocalApplyPreview
	applyResults := ApplyUpdatesFromManager(
		map[string]operator.Operator{op.OperatorID(): op},
		updates,
	)

	preview := BuildLocalApplyPreview(onlineContext.SkillName, applyResults)
	return &preview, nil
}

// getPreferredSignal 获取优先信号。
// 对应 Python: OnlineEvolutionOrchestrator._get_preferred_signal()
func getPreferredSignal(onlineContext *EvolutionContext) *signal.EvolutionSignal {
	for i := range onlineContext.Signals {
		if onlineContext.Signals[i].SignalType == schema.UserIntentSignal {
			return &onlineContext.Signals[i]
		}
	}
	if len(onlineContext.Signals) > 0 {
		return &onlineContext.Signals[0]
	}
	return nil
}

// getSignalType 获取信号类型。
// 对应 Python: OnlineEvolutionOrchestrator._get_signal_type()
func getSignalType(onlineContext *EvolutionContext) string {
	preferred := getPreferredSignal(onlineContext)
	if preferred != nil {
		return preferred.SignalType
	}
	return ""
}

// getSignalSource 获取信号来源。
// 对应 Python: OnlineEvolutionOrchestrator._get_signal_source()
func getSignalSource(onlineContext *EvolutionContext) string {
	preferred := getPreferredSignal(onlineContext)
	if preferred != nil {
		source := signal.GetSignalSource(preferred)
		if source != nil {
			return *source
		}
	}
	return ""
}
