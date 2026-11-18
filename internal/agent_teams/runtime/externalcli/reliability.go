// Package externalcli —— 外部运行时可靠性投递半区（RuntimeReliabilityContext）。
//
// 本文件是 external-runtime reliability loop 的「投递」半区：每次启动/每轮一个
// RuntimeReliabilityContext 持有一个待定失败与一个已定 failure id，保证每次启动
// 或每轮至多发出一条 ExternalRuntimeFailure、至多一条终态 failed 消息。
// 「分类」半区在各 SDK runtime 旁（claudecode/codex failure_classifier），Go 侧
// ⤴️15.7/15.8（D-15.13-x）。成员状态经注入回调更新，本层不直接触碰 DB 层。
//
// 已知偏差：
//   - D-rel-1：Python send_message(protocol="json") 在 Go TeamMessageManager.SendMessage
//     无 protocol 形参——content 本身即 JSON 串，投递语义等价。
//   - D-rel-2：Python 方法为 async；Go 改同步方法（锁内小步操作），轮终态调用点
//     已在 D-15.16-a2 异步派发（asyncGo），时序不变。
//   - D-rel-3：ExternalRuntimeRetryingEvent.Reason 沿 25.1g D-g3 以 map 形状承载
//     （reasonToMap 转换），typed reason 仍是本上下文的入参形状。
//
// 对应 Python 代码：openjiuwen/agent_teams/external/reliability.py（430 行）
package externalcli

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	atevents "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UpdateStatusCallback 由 TeamAgent 壳注入的状态更新回调；runtime 不感知 DB 层。
// Python: UpdateStatusCallback = Callable[[MemberStatus], Awaitable[None]] (reliability.py:51)
type UpdateStatusCallback func(ctx context.Context, status atschema.MemberStatus) error

// RuntimeReliabilityContext Claude/Codex SDK runtime 的每次启动/每轮可靠性状态。
//
// 每次 BeginAttempt 开新尝试：清空待定失败与已定 failure id。一次尝试内：
//   - RecordPending 存一个候选失败（Claude AssistantMessage.error / Codex
//     ErrorNotification(will_retry=False)），不写 failed 消息；
//   - PublishRetrying 发非持久进度事件；
//   - FinalizeFailure 是唯一出口：首次调用铸 failure_id、把 failed 消息持久化到
//     leader 信箱、记当前轮轨迹 span；后续调用复用既有 failure_id 不再写——
//     每次尝试 exactly-once；
//   - MarkMemberError 把成员翻 ERROR（仅启动失败/任务异常退出两条 runtime
//     不可用路径；轮内失败由成员按正常轮语义回落 READY）。
//
// 投递 best-effort：send_message/publish 失败只记日志不抛——未读的信箱消息会被
// 后续 sweep 捡起。
//
// Python: RuntimeReliabilityContext (reliability.py:54)
type RuntimeReliabilityContext struct {
	mu sync.Mutex

	// memberName/teamName/sessionID/agentKind 身份与可靠性词汇
	memberName string
	teamName   string
	sessionID  string
	agentKind  atschema.ExternalRuntimeAgentKind
	// messageMgr/leaderName 失败消息投递面（leader 信箱）
	messageMgr *tools.TeamMessageManager
	leaderName string
	// msgr 重试进度事件发布面；nil 降级只记日志
	msgr messager.Messager
	// statusCB 成员状态更新回调（ERROR 翻转）
	statusCB UpdateStatusCallback
	// trajectory 轨迹记录器（失败 span 追加；nil 跳过）
	trajectory *harnessproviders.HarnessTrajectoryRecorder
	// cliPath 失败报告携带的 CLI 可执行路径（空报告 "<default>"）
	cliPath string
	// model CLI/SDK 确认的实际服务模型
	model string
	// 一次尝试内的状态；见 BeginAttempt（Python :104-108）
	phase           atschema.ExternalRuntimePhase
	roundID         *int
	pendingCategory atschema.ExternalRuntimeFailureCategory
	pendingReason   *atschema.ExternalRuntimeFailureReason
	failureID       string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeReliabilityContext 绑定一个成员 runtime 的投递与状态面。
// Python: __init__ (reliability.py:76)
func NewRuntimeReliabilityContext(
	memberName, teamName, sessionID string,
	agentKind atschema.ExternalRuntimeAgentKind,
	messageMgr *tools.TeamMessageManager,
	msgr messager.Messager,
	leaderName string,
	statusCB UpdateStatusCallback,
	trajectory *harnessproviders.HarnessTrajectoryRecorder,
	cliPath string,
) *RuntimeReliabilityContext {
	return &RuntimeReliabilityContext{
		memberName: memberName,
		teamName:   teamName,
		sessionID:  sessionID,
		agentKind:  agentKind,
		messageMgr: messageMgr,
		msgr:       msgr,
		leaderName: leaderName,
		statusCB:   statusCB,
		trajectory: trajectory,
		cliPath:    cliPath,
	}
}

// BeginAttempt 开新启动/轮次尝试，清空先前失败状态。
// Python: begin_attempt (reliability.py:114)
func (c *RuntimeReliabilityContext) BeginAttempt(phase atschema.ExternalRuntimePhase, roundID *int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.phase = phase
	c.roundID = roundID
	c.pendingCategory = ""
	c.pendingReason = nil
	c.failureID = ""
}

// Phase 当前尝试阶段（startup/turn）。
// Python: phase property (reliability.py:122)
func (c *RuntimeReliabilityContext) Phase() atschema.ExternalRuntimePhase {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

// RoundID 当前尝试轮次 id（startup 为 nil）。
// Python: round_id property (reliability.py:127)
func (c *RuntimeReliabilityContext) RoundID() *int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roundID
}

// HasPending 本次尝试是否已记录候选失败。
// Python: has_pending property (reliability.py:133)
func (c *RuntimeReliabilityContext) HasPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingCategory != ""
}

// HasFinalized 本次尝试是否已 finalize 失败。
// Python: has_finalized property (reliability.py:138)
func (c *RuntimeReliabilityContext) HasFinalized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failureID != ""
}

// FailureID 已定 failure id（未定为空串）。
// Python: failure_id property (reliability.py:143)
func (c *RuntimeReliabilityContext) FailureID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failureID
}

// PendingCategory 待定失败类别（未定为空串）。
// Python: pending_category property (reliability.py:148)
func (c *RuntimeReliabilityContext) PendingCategory() atschema.ExternalRuntimeFailureCategory {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingCategory
}

// PendingReason 待定失败原因（未定为 nil）。
// Python: pending_reason property (reliability.py:153)
func (c *RuntimeReliabilityContext) PendingReason() *atschema.ExternalRuntimeFailureReason {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingReason
}

// Model CLI/SDK 确认的实际服务模型。
// Python: model property (reliability.py:158)
func (c *RuntimeReliabilityContext) Model() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.model
}

// UpdateModel 记录 CLI/SDK 确认的实际服务模型；nil/空白忽略。
// Python: update_model (reliability.py:162)
func (c *RuntimeReliabilityContext) UpdateModel(model *string) {
	if model == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if normalized := strings.TrimSpace(*model); normalized != "" {
		c.model = normalized
	}
}

// ClearModel 切换另一 CLI 配置前清空实际模型。
// Python: clear_model (reliability.py:170)
func (c *RuntimeReliabilityContext) ClearModel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = ""
}

// RecordPending 记录候选失败；轮终态负责 finalize。
// 已 finalize 后迟到的候选不得重开本次尝试；同一尝试内更晚的候选仅在携带更强
// 结构化信号（非空 http_status/sdk_error_code）时覆盖 category，否则首个结构化
// 信号胜出；reason 按 mergeReason 保留细节。
// Python: record_pending (reliability.py:178)
func (c *RuntimeReliabilityContext) RecordPending(
	category atschema.ExternalRuntimeFailureCategory,
	reason *atschema.ExternalRuntimeFailureReason,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failureID != "" {
		// 已 finalize；迟到的候选不得重开本次尝试（reliability.py:191-193）。
		return
	}
	if c.pendingCategory == "" {
		c.pendingCategory = category
		c.pendingReason = reason
		return
	}
	// 保留更具体的 reason；category 跟随结构化信号（reliability.py:198-201）。
	if moreSpecific(reason, c.pendingReason) {
		c.pendingCategory = category
	}
	c.pendingReason = mergeReason(reason, c.pendingReason)
}

// PublishRetrying 向团队 topic 发布非持久重试进度事件。
// 不结束轮、不改成员状态。messager 为 nil 降级只记警告；publish 失败只记日志
// （best-effort，进度信号不持久化）。
// Python: publish_retrying (reliability.py:203)
func (c *RuntimeReliabilityContext) PublishRetrying(
	category atschema.ExternalRuntimeFailureCategory,
	reason *atschema.ExternalRuntimeFailureReason,
	summary string,
) error {
	c.mu.Lock()
	phase := c.phase
	roundID := c.roundID
	model := c.model
	c.mu.Unlock()
	if c.msgr == nil {
		// Python: "[{}] external runtime retrying event dropped (no messager): {}" (reliability.py:218-222)
		logger.Warn(logger.ComponentAgentCore).
			Str("member", c.memberName).
			Str("summary", summary).
			Msg("[external-runtime] external runtime retrying event dropped (no messager)")
		return nil
	}
	event := atevents.ExternalRuntimeRetryingEvent{
		BaseEventMessage: atevents.BaseEventMessage{TeamName: c.teamName, MemberName: c.memberName},
		AgentKind:        string(c.agentKind),
		Model:            model,
		Phase:            string(phaseOrTurn(phase)),
		Category:         string(category),
		Summary:          summary,
		Reason:           reasonToMap(reason),
		RoundID:          roundID,
	}
	topicID := atevents.TeamTopicTeam.Build(c.sessionID, c.teamName)
	// 进度事件不得打断轮次（Python except Exception: team_logger.exception，reliability.py:242-246）
	if err := c.msgr.Publish(context.Background(), topicID, atevents.EventMessageFromEvent(event)); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("member", c.memberName).
			Msg("failed to publish external runtime retrying event")
	}
	// Python: "[external-runtime] member {} {} retrying category={} summary={} round_id={}" (reliability.py:247-254)
	logger.Info(logger.ComponentAgentCore).
		Str("member", c.memberName).
		Str("agent_kind", string(c.agentKind)).
		Str("category", string(category)).
		Str("summary", summary).
		Int("round_id", intPtrVal(roundID)).
		Msg("[external-runtime] retrying")
	return nil
}

// FinalizeFailure 为本次尝试 finalize 并持久化唯一一条 failed 消息。
// 首次调用铸 failure_id、写 leader 信箱、记轨迹 span；重复调用返回 nil 不写
// 第二条——每次尝试 exactly-once。
// Python: finalize_failure (reliability.py:260)
func (c *RuntimeReliabilityContext) FinalizeFailure(
	category atschema.ExternalRuntimeFailureCategory,
	reason *atschema.ExternalRuntimeFailureReason,
	summary string,
) *atschema.ExternalRuntimeFailure {
	c.mu.Lock()
	if c.failureID != "" {
		c.mu.Unlock()
		// Python: if self._failure_id is not None: return None (reliability.py:277-278)
		return nil
	}
	failureID := newFailureID()
	c.failureID = failureID
	phase := c.phase
	model := c.model
	c.mu.Unlock()

	// Python: if not suggested_action: suggested_action = _default_suggested_action(category)
	failure := atschema.NewExternalRuntimeFailure(
		failureID, c.teamName, c.memberName, string(c.agentKind), model,
		phaseOrTurn(phase), category, summary,
		defaultSuggestedAction(category),
		cliPathPtr(c.cliPath), reason,
	)
	c.persistFailure(failure)
	c.recordSpan(failure)
	return failure
}

// persistFailure 经 send_message 把 failed 消息写入 leader 信箱。
// message_manager 缺失时失败消息未投递只记错误；投递异常只记日志不抛。
// Python: _persist_failure (reliability.py:300)
func (c *RuntimeReliabilityContext) persistFailure(failure *atschema.ExternalRuntimeFailure) {
	if c.messageMgr == nil {
		// Python: "[{}] external runtime failure has no message manager; failure {} undelivered: {}" (reliability.py:303-308)
		logger.Error(logger.ComponentAgentCore).
			Str("member", c.memberName).
			Str("failure_id", failure.FailureID).
			Str("summary", failure.Summary).
			Msg("external runtime failure has no message manager; failure undelivered")
		return
	}
	content, err := json.Marshal(failure)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("member", c.memberName).
			Str("failure_id", failure.FailureID).
			Msg("failed to marshal external runtime failure")
		return
	}
	// D-rel-1：Python protocol="json" —— content 本身即 JSON 串。
	if _, err := c.messageMgr.SendMessage(context.Background(), string(content), c.leaderName, c.memberName); err != nil {
		// 投递不得打断轮次（Python except Exception: team_logger.exception，reliability.py:319-324）
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("member", c.memberName).
			Str("failure_id", failure.FailureID).
			Msg("failed to persist external runtime failure to mailbox")
	}
	// Python: "[external-runtime] member {} {} failed model={} cli_path={} phase={} category={} ..." (reliability.py:325-339)
	logger.Error(logger.ComponentAgentCore).
		Str("member", c.memberName).
		Str("agent_kind", string(c.agentKind)).
		Str("model", firstNonEmpty(failure.Model, "<unknown>")).
		Str("cli_path", cliPathOr(failure.CLIPath)).
		Str("phase", string(failure.Phase)).
		Str("category", string(failure.Category)).
		Str("failure_id", failure.FailureID).
		Int("round_id", intPtrVal(failure.RoundID)).
		Str("summary", failure.Summary).
		Bool("user_action_required", failure.UserActionRequired).
		Msg("[external-runtime] member failed")
}

// recordSpan 把已定失败记到成员轨迹上（best-effort）。
// Python: _record_span (reliability.py:341)
func (c *RuntimeReliabilityContext) recordSpan(failure *atschema.ExternalRuntimeFailure) {
	if c.trajectory == nil {
		return
	}
	attributes := map[string]attribute.Value{
		"external_runtime.failure_id":  attribute.StringValue(failure.FailureID),
		"external_runtime.phase":       attribute.StringValue(string(failure.Phase)),
		"external_runtime.category":    attribute.StringValue(string(failure.Category)),
		"external_runtime.summary":     attribute.StringValue(failure.Summary),
		"external_runtime.member_name": attribute.StringValue(c.memberName),
		"external_runtime.team_name":   attribute.StringValue(c.teamName),
		"external_runtime.agent_kind":  attribute.StringValue(string(c.agentKind)),
	}
	if failure.RoundID != nil {
		attributes["external_runtime.round_id"] = attribute.IntValue(*failure.RoundID)
	}
	// Python: recorder.record_failure(...) + except → debug log（reliability.py:357-367）
	c.trajectory.RecordFailure("external_runtime.failed", failure.Summary, attributes)
}

// MarkMemberError 把成员翻 ERROR（仅 runtime 不可用路径）。
// 回调失败照常记错误日志并回传——调用方（finalizeStartupFailure 等收口路径）
// 显式忽略返回值，可观测行为与 Python except Exception 等价且不丢失错误信息。
// Python: mark_member_error (reliability.py:369)
func (c *RuntimeReliabilityContext) MarkMemberError() error {
	if c.statusCB == nil {
		return nil
	}
	if err := c.statusCB(context.Background(), atschema.MemberStatusError); err != nil {
		// Python: team_logger.exception（reliability.py:375-379）
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("member", c.memberName).
			Msg("failed to mark member ERROR after runtime failure")
		return err
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// moreSpecific 返回 new 是否比 old 携带更强结构化信号。
// Python: _more_specific (reliability.py:382)
func moreSpecific(new, old *atschema.ExternalRuntimeFailureReason) bool {
	if old == nil {
		return true
	}
	return reasonScore(new) > reasonScore(old)
}

// reasonScore 按 reason 携带的结构化信号量打分（http_status 2 分、
// sdk_error_code/sdk_error_type 各 1 分）。
// Python: _reason_score (reliability.py:394)
func reasonScore(reason *atschema.ExternalRuntimeFailureReason) int {
	if reason == nil {
		return 0
	}
	score := 0
	if reason.HTTPStatus != nil {
		score += 2
	}
	if reason.SDKErrorCode != "" {
		score++
	}
	if reason.SDKErrorType != "" {
		score++
	}
	return score
}

// mergeReason 把新候选 reason 并入旧的，保留细节：新结构化字段优先，
// 新 message 空时回落旧文案——原始 SDK 上下文不丢。
// Python: _merge_reason (reliability.py:406)
func mergeReason(new, old *atschema.ExternalRuntimeFailureReason) *atschema.ExternalRuntimeFailureReason {
	if old == nil {
		return new
	}
	if new == nil {
		return old
	}
	merged := *new
	if merged.Message == "" {
		merged.Message = old.Message
	}
	if merged.SDKErrorType == "" {
		merged.SDKErrorType = old.SDKErrorType
	}
	if merged.SDKErrorCode == "" {
		merged.SDKErrorCode = old.SDKErrorCode
	}
	if merged.HTTPStatus == nil {
		merged.HTTPStatus = old.HTTPStatus
	}
	return &merged
}

// defaultSuggestedAction 返回类别对应的用户建议动作（i18n 一比一文案）。
// Python: _default_suggested_action (reliability.py:425)
func defaultSuggestedAction(category atschema.ExternalRuntimeFailureCategory) string {
	action, _ := atschema.T("reliability.suggested_action." + string(category))
	return action
}

// phaseOrTurn 阶段为空回落 "turn"（Python self._phase or "turn"）。
func phaseOrTurn(phase atschema.ExternalRuntimePhase) atschema.ExternalRuntimePhase {
	if phase == "" {
		return atschema.ExternalRuntimePhaseTurn
	}
	return phase
}

// newFailureID 铸失败关联 id（Python uuid.uuid4().hex；D-15.13-e）。
func newFailureID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// cliPathPtr 空路径转 nil（Python excluded_fields：cli_path None 时排除字段）。
func cliPathPtr(cliPath string) *string {
	if cliPath == "" {
		return nil
	}
	return &cliPath
}

// cliPathOr 报告日志用：nil → "<default>"（Python cli_path or "<default>"）。
func cliPathOr(p *string) string {
	if p == nil {
		return "<default>"
	}
	return *p
}

// intPtrVal nil 指针回落 0（日志字段用）。
func intPtrVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// reasonToMap typed reason → 25.1g D-g3 的事件 map 形状（键 message/
// sdk_error_type/sdk_error_code/http_status）；nil → 空 map（Python
// default_factory ExternalRuntimeFailureReason）。
func reasonToMap(reason *atschema.ExternalRuntimeFailureReason) map[string]any {
	m := map[string]any{
		"message":        "",
		"sdk_error_type": "",
		"sdk_error_code": "",
		"http_status":    nil,
	}
	if reason == nil {
		return m
	}
	m["message"] = reason.Message
	m["sdk_error_type"] = reason.SDKErrorType
	m["sdk_error_code"] = reason.SDKErrorCode
	if reason.HTTPStatus != nil {
		m["http_status"] = *reason.HTTPStatus
	}
	return m
}
