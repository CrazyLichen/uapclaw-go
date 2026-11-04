package rails

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	codetools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// taskReminderState 会话级提醒状态（对齐上游 _TaskReminderState）。
type taskReminderState struct {
	// Content 当前生效的提醒内容
	Content string
	// ItemCount 提醒写入时的待办条数
	ItemCount int
	// DeliverySequence 第几次投递（内容里带标记）
	DeliverySequence int
	// TurnsSinceManagement 距上次 todo 工具管理的轮次
	TurnsSinceManagement int
	// TurnsSinceReminder 距上次提醒的轮次
	TurnsSinceReminder int
}

// CodeTaskPlanningRail code 模式 todo rail。
//
// 与 TaskPlanningRail 的差异（对齐上游 CodeTaskPlanningRail）：
//   - todo 工具卡片改用 CC 对齐文案（code/tools.CodeTodoCardBuilder）
//   - 不注入静态 todo 系统提示词节
//   - 连续 N 轮未管理 todo 时追加一次 task_reminder 附件，内容不变不重复
//
// Python: CodeTaskPlanningRail
type CodeTaskPlanningRail struct {
	// TaskPlanningRail 父类（工具注册、模型切换、进度提醒复用）
	*harnessrails.TaskPlanningRail

	// turnsSinceManagement 触发提醒所需的「未管理 todo」轮次阈值
	turnsSinceManagement int
	// turnsBetweenReminders 两次提醒之间的最小轮次
	turnsBetweenReminders int
	// maxTrackedSessions 状态跟踪上限（LRU 淘汰）
	maxTrackedSessions int

	// mu 保护 states/order/locks
	mu sync.Mutex
	// states 会话 → 提醒状态
	states map[string]*taskReminderState
	// order LRU 顺序（尾部最新）
	order []string
	// locks 会话级互斥锁
	locks map[string]*sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskReminderSection 提醒附件片段名
	taskReminderSection = "task_reminder"
	// taskReminderSource 提醒附件来源
	taskReminderSource = "jiuwenswarm.code_task_planning.task_reminder"
	// taskReminderPriority 提醒附件优先级
	taskReminderPriority = 60
	// defaultTurnsSinceManagement 触发提醒的默认阈值
	defaultTurnsSinceManagement = 10
	// defaultTurnsBetweenReminders 两次提醒默认间隔
	defaultTurnsBetweenReminders = 10
	// defaultMaxTrackedSessions 状态跟踪默认上限
	defaultMaxTrackedSessions = 1000
	// taskReminderBlockSeparator 渲染块分隔符（内容候选切分用）
	taskReminderBlockSeparator = "\n\n---\n\n"
	// taskReminderLegacyPrefix 旧版提醒文案前缀
	taskReminderLegacyPrefix = "The task tools haven't been used recently."
	// taskReminderCurrentPrefix 当前提醒文案前缀
	taskReminderCurrentPrefix = "Use the task tools when the current work would benefit"
	// taskReminderDeliveryMarker 投递序号标记前缀
	taskReminderDeliveryMarker = "<!-- task-reminder-delivery:"
	// countedIterationsKey 已计数的 ReAct 迭代集合在 extra 中的键
	countedIterationsKey = "_jiuwenswarm_task_reminder_counted_react_iterations"
	// reactIterationExtraKey ReAct 迭代号在 extra 中的键
	reactIterationExtraKey = "_react_iteration"
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeTaskPlanningRail 创建 code 模式 todo rail。
//
// language 决定 todo 工具注册语言；opts 透传给父类（已内置 CC 卡片与
// inject_prompt=false，调用方可再覆盖）。
func NewCodeTaskPlanningRail(language string, opts ...harnessrails.TaskPlanningOption) *CodeTaskPlanningRail {
	baseOpts := []harnessrails.TaskPlanningOption{
		harnessrails.WithLanguage(language),
		harnessrails.WithInjectPrompt(false),
		harnessrails.WithTodoCardBuilder(codetools.CodeTodoCardBuilder),
	}
	baseOpts = append(baseOpts, opts...)
	return &CodeTaskPlanningRail{
		TaskPlanningRail:      harnessrails.NewTaskPlanningRail(baseOpts...),
		turnsSinceManagement:  defaultTurnsSinceManagement,
		turnsBetweenReminders: defaultTurnsBetweenReminders,
		maxTrackedSessions:    defaultMaxTrackedSessions,
		states:                map[string]*taskReminderState{},
		locks:                 map[string]*sync.Mutex{},
	}
}

// OnUserMessage 在附件历史同步之前恢复提醒状态。
//
// Python: CodeTaskPlanningRail.on_user_message
func (r *CodeTaskPlanningRail) OnUserMessage(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	_ = ctx
	r.restoreTaskReminderAttachment(cbc)
	return nil
}

// BeforeModelCall 先按最新待办切换模型，再按节奏追加提醒附件。
//
// 不调用父类 BeforeModelCall：code 模式跳过静态 todo 节，模型切换用磁盘重读。
//
// Python: CodeTaskPlanningRail.before_model_call
func (r *CodeTaskPlanningRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Agent() == nil {
		return nil
	}
	// 静态 todo 节在 code 模式一律不注入
	if sb := cbc.Agent().SystemPromptBuilder(); sb != nil {
		sb.RemoveSection("todo")
	}
	r.SwitchModelIfNeeded(ctx, cbc, true)

	sessionID := sessionIDOf(cbc)
	manager := prompts.ManagerFor(cbc.Agent())
	if sessionID == "" || manager == nil || !r.HasTodoTool() {
		return nil
	}

	lock := r.lockFor(sessionID)
	lock.Lock()
	defer lock.Unlock()

	r.restoreLocked(cbc, manager, sessionID)
	if !r.markIterationCounted(cbc) {
		return nil
	}

	state := r.getState(cbc, sessionID)
	state.TurnsSinceManagement++
	state.TurnsSinceReminder++
	r.saveState(sessionID, state)

	if state.TurnsSinceManagement < r.turnsSinceManagement ||
		state.TurnsSinceReminder < r.turnsBetweenReminders {
		return nil
	}

	sequence := state.DeliverySequence + 1
	content := buildTaskReminderContent() + fmt.Sprintf("\n\n"+taskReminderDeliveryMarker+"%d -->", sequence)
	if _, err := manager.AddSection(sessionID, taskReminderSection, content,
		prompts.AttachmentTodoReminder, taskReminderSource,
		prompts.WithAttachmentPriority(taskReminderPriority),
		prompts.WithAttachmentMetadata(map[string]any{
			"item_count":        0,
			"delivery_sequence": sequence,
		}),
		prompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "code_task_planning_add_reminder_failed").
			Str("session_id", sessionID).
			Msg("failed to add task reminder attachment")
		return nil
	}
	state.Content = content
	state.ItemCount = 0
	state.DeliverySequence = sequence
	state.TurnsSinceReminder = 0
	r.saveState(sessionID, state)
	return nil
}

// AfterToolCall 转发父类处理后，重置「未管理 todo」计数。
//
// Python: CodeTaskPlanningRail.after_tool_call
func (r *CodeTaskPlanningRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if err := r.TaskPlanningRail.AfterToolCall(ctx, cbc); err != nil {
		return err
	}
	if cbc == nil || !isTodoToolCall(cbc) {
		return nil
	}
	sessionID := sessionIDOf(cbc)
	if sessionID == "" {
		return nil
	}
	lock := r.lockFor(sessionID)
	lock.Lock()
	defer lock.Unlock()
	state := r.getState(cbc, sessionID)
	state.TurnsSinceManagement = 0
	r.saveState(sessionID, state)
	return nil
}

// GetCallbacks 覆盖父类回调映射（BeforeModelCall/AfterToolCall/OnUserMessage）。
func (r *CodeTaskPlanningRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.TaskPlanningRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackOnUserMessage] = func(ctx context.Context, railCtx any) error {
		return r.OnUserMessage(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// restoreTaskReminderAttachment 恢复提醒附件（外层加会话锁）。
func (r *CodeTaskPlanningRail) restoreTaskReminderAttachment(cbc *agentinterfaces.AgentCallbackContext) {
	if cbc == nil || cbc.Agent() == nil {
		return
	}
	sessionID := sessionIDOf(cbc)
	manager := prompts.ManagerFor(cbc.Agent())
	if sessionID == "" || manager == nil {
		return
	}
	lock := r.lockFor(sessionID)
	lock.Lock()
	defer lock.Unlock()
	r.restoreLocked(cbc, manager, sessionID)
}

// restoreLocked 让管理器状态与会话状态一致（调用方持有会话锁）。
//
// Python: CodeTaskPlanningRail._restore_task_reminder_attachment_locked
func (r *CodeTaskPlanningRail) restoreLocked(
	cbc *agentinterfaces.AgentCallbackContext,
	manager *prompts.Manager,
	sessionID string,
) {
	reminders, err := manager.ListByFilter(prompts.AttachmentFilter{
		SessionID: sessionID,
		Section:   taskReminderSection,
		Source:    taskReminderSource,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "code_task_planning_list_reminder_failed").
			Str("session_id", sessionID).
			Msg("failed to list task reminder attachment")
		return
	}
	if len(reminders) > 0 {
		state := r.getState(cbc, sessionID)
		reminder := reminders[0]
		managerSequence := maxInt(
			nonNegativeInt(reminder.Metadata["delivery_sequence"]),
			deliverySequenceFromContent(reminder.Content),
		)
		if state.Content == "" || managerSequence >= state.DeliverySequence {
			itemCount := nonNegativeInt(reminder.Metadata["item_count"])
			changed := state.Content != reminder.Content ||
				state.DeliverySequence != managerSequence ||
				state.ItemCount != itemCount
			state.Content = reminder.Content
			state.ItemCount = itemCount
			state.DeliverySequence = managerSequence
			if changed {
				r.saveState(sessionID, state)
			}
			return
		}
		r.addTaskReminderAttachment(manager, sessionID, state)
		return
	}

	state := r.getState(cbc, sessionID)
	if state.Content == "" {
		recovered := r.recoverTaskReminderFromHistory(cbc, sessionID)
		if recovered == "" {
			return
		}
		state.Content = recovered
		state.DeliverySequence = deliverySequenceFromContent(recovered)
		state.ItemCount = 0
		r.saveState(sessionID, state)
	}
	r.addTaskReminderAttachment(manager, sessionID, state)
}

// addTaskReminderAttachment 把状态里的提醒内容写回附件。
//
// Python: CodeTaskPlanningRail._add_task_reminder_attachment
func (r *CodeTaskPlanningRail) addTaskReminderAttachment(
	manager *prompts.Manager,
	sessionID string,
	state *taskReminderState,
) {
	if _, err := manager.AddSection(sessionID, taskReminderSection, state.Content,
		prompts.AttachmentTodoReminder, taskReminderSource,
		prompts.WithAttachmentPriority(taskReminderPriority),
		prompts.WithAttachmentMetadata(map[string]any{
			"item_count":        state.ItemCount,
			"delivery_sequence": state.DeliverySequence,
		}),
		prompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "code_task_planning_restore_reminder_failed").
			Str("session_id", sessionID).
			Msg("failed to restore task reminder attachment")
	}
}

// recoverTaskReminderFromHistory 从附件历史里恢复仍生效的提醒内容。
//
// Python: CodeTaskPlanningRail._recover_task_reminder_from_history
func (r *CodeTaskPlanningRail) recoverTaskReminderFromHistory(
	cbc *agentinterfaces.AgentCallbackContext,
	sessionID string,
) string {
	messages := historyMessages(cbc)
	targetHash, _ := latestTaskReminderHistoryState(messages, sessionID)
	if targetHash == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if !isAttachmentHistoryMessage(message, sessionID) {
			continue
		}
		rendered := message.GetContent().Text()
		for _, candidate := range taskReminderContentCandidates(rendered) {
			if taskReminderHash(candidate, sessionID) == targetHash {
				return candidate
			}
		}
	}
	// 旧版提醒可能内嵌整份待办列表并被渲染上限截断，
	// 历史里只剩前缀而 metadata 保存完整内容哈希：用待办存储重建后再比对。
	legacy := buildLegacyTaskReminderContent(r.LoadTodosFresh(context.Background(), sessionID))
	if taskReminderHash(legacy, sessionID) == targetHash {
		return legacy
	}
	return ""
}

// getState 返回会话状态副本（缓存命中直接复制，否则从历史推导并落缓存）。
func (r *CodeTaskPlanningRail) getState(
	cbc *agentinterfaces.AgentCallbackContext,
	sessionID string,
) *taskReminderState {
	r.mu.Lock()
	cached, ok := r.states[sessionID]
	r.mu.Unlock()
	if ok {
		return &taskReminderState{
			Content:              cached.Content,
			ItemCount:            cached.ItemCount,
			DeliverySequence:     cached.DeliverySequence,
			TurnsSinceManagement: cached.TurnsSinceManagement,
			TurnsSinceReminder:   cached.TurnsSinceReminder,
		}
	}
	state := stateFromHistory(cbc, sessionID)
	r.saveState(sessionID, state)
	return state
}

// saveState 写入会话状态并维护 LRU 上限。
func (r *CodeTaskPlanningRail) saveState(sessionID string, state *taskReminderState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states == nil {
		r.states = map[string]*taskReminderState{}
	}
	r.states[sessionID] = &taskReminderState{
		Content:              state.Content,
		ItemCount:            state.ItemCount,
		DeliverySequence:     state.DeliverySequence,
		TurnsSinceManagement: state.TurnsSinceManagement,
		TurnsSinceReminder:   state.TurnsSinceReminder,
	}
	// LRU：已有则先摘除再追加到尾部
	for i, id := range r.order {
		if id == sessionID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.order = append(r.order, sessionID)
	limit := r.maxTrackedSessions
	if limit < 1 {
		limit = 1
	}
	for len(r.order) > limit {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.states, oldest)
	}
}

// lockFor 返回会话级互斥锁。
func (r *CodeTaskPlanningRail) lockFor(sessionID string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	lock, ok := r.locks[sessionID]
	if !ok {
		lock = &sync.Mutex{}
		r.locks[sessionID] = lock
	}
	return lock
}

// markIterationCounted 同一 ReAct 迭代只计数一次（模型重试不重复累加）。
//
// Python: CodeTaskPlanningRail._mark_task_reminder_iteration_counted
func (r *CodeTaskPlanningRail) markIterationCounted(cbc *agentinterfaces.AgentCallbackContext) bool {
	iteration := 0
	if raw, ok := cbc.GetExtra(reactIterationExtraKey); ok {
		iteration = nonNegativeInt(raw)
	}
	counted, _ := cbc.GetExtra(countedIterationsKey)
	seen, ok := counted.(map[int]struct{})
	if !ok {
		seen = map[int]struct{}{}
	}
	if _, dup := seen[iteration]; dup {
		return false
	}
	seen[iteration] = struct{}{}
	cbc.SetExtra(countedIterationsKey, seen)
	return true
}

// sessionIDOf 取会话 ID（无会话返回空串）。
func sessionIDOf(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil || cbc.Session() == nil {
		return ""
	}
	return cbc.Session().GetSessionID()
}

// isTodoToolCall 判断当前回调是否 todo_* 工具调用。
func isTodoToolCall(cbc *agentinterfaces.AgentCallbackContext) bool {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return false
	}
	return strings.HasPrefix(inputs.ToolName, "todo_")
}

// historyMessages 取会话完整消息列表（含历史）。
func historyMessages(cbc *agentinterfaces.AgentCallbackContext) []llmschema.BaseMessage {
	if cbc == nil {
		return nil
	}
	modelCtx := cbc.ModelContext()
	if modelCtx == nil {
		return nil
	}
	messages, err := modelCtx.GetMessages(0, true)
	if err != nil {
		return nil
	}
	return messages
}

// isAttachmentHistoryMessage 判断消息是否为附件历史消息（且属本会话）。
//
// Python: CodeTaskPlanningRail._is_attachment_history_message
func isAttachmentHistoryMessage(message llmschema.BaseMessage, sessionID string) bool {
	if message == nil {
		return false
	}
	meta := message.GetMetadata()
	if meta == nil || meta[prompts.MetadataKeyAttachmentHistory] != true {
		return false
	}
	historySession, ok := meta[prompts.MetadataKeySessionID].(string)
	return !ok || historySession == "" || historySession == sessionID
}

// latestTaskReminderHistoryState 取最后一次提醒交付账本哈希与其变更位置。
//
// Python: CodeTaskPlanningRail._latest_task_reminder_history_state
func latestTaskReminderHistoryState(messages []llmschema.BaseMessage, sessionID string) (string, int) {
	currentHash := ""
	lastChangeIndex := -1
	stateSeen := false
	for index, message := range messages {
		if !isAttachmentHistoryMessage(message, sessionID) {
			continue
		}
		nextHash := ""
		if meta := message.GetMetadata(); meta != nil {
			if state, ok := meta[prompts.MetadataKeyState].(map[string]string); ok {
				nextHash = state[taskReminderSection]
			}
		}
		if nextHash != currentHash && (stateSeen || nextHash != "") {
			lastChangeIndex = index
		}
		currentHash = nextHash
		stateSeen = true
	}
	return currentHash, lastChangeIndex
}

// taskReminderPayloadIndex 定位承载指定哈希的提醒消息下标（找不到回落截断消息）。
//
// Python: CodeTaskPlanningRail._task_reminder_payload_index
func taskReminderPayloadIndex(messages []llmschema.BaseMessage, sessionID, targetHash string) int {
	truncatedIndex := -1
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if !isAttachmentHistoryMessage(message, sessionID) {
			continue
		}
		rendered := message.GetContent().Text()
		if truncatedIndex < 0 && (strings.Contains(rendered, taskReminderCurrentPrefix) ||
			strings.Contains(rendered, taskReminderLegacyPrefix)) {
			truncatedIndex = index
		}
		for _, candidate := range taskReminderContentCandidates(rendered) {
			if taskReminderHash(candidate, sessionID) == targetHash {
				return index
			}
		}
	}
	return truncatedIndex
}

// stateFromHistory 从历史推导提醒计数状态。
//
// Python: CodeTaskPlanningRail._task_reminder_state_from_history
func stateFromHistory(cbc *agentinterfaces.AgentCallbackContext, sessionID string) *taskReminderState {
	messages := historyMessages(cbc)
	assistantIndexes := make([]int, 0, len(messages))
	for index, message := range messages {
		if message != nil && message.GetRole() == llmschema.RoleTypeAssistant {
			assistantIndexes = append(assistantIndexes, index)
		}
	}
	lastTodoIndex := lastTodoResultIndex(messages)
	turnsSinceManagement := 0
	for _, index := range assistantIndexes {
		if index > lastTodoIndex {
			turnsSinceManagement++
		}
	}

	targetHash, latestAttachmentIndex := latestTaskReminderHistoryState(messages, sessionID)
	turnsSinceReminder := 0
	switch {
	case targetHash != "":
		reminderIndex := taskReminderPayloadIndex(messages, sessionID, targetHash)
		count := 0
		for _, index := range assistantIndexes {
			if index > reminderIndex {
				count++
			}
		}
		if count > 0 {
			turnsSinceReminder = count - 1
		}
	case latestAttachmentIndex >= 0:
		for _, index := range assistantIndexes {
			if index > latestAttachmentIndex {
				turnsSinceReminder++
			}
		}
	default:
		turnsSinceReminder = len(assistantIndexes)
	}

	return &taskReminderState{
		TurnsSinceManagement: turnsSinceManagement,
		TurnsSinceReminder:   turnsSinceReminder,
	}
}

// lastTodoResultIndex 取最后一条 todo_* 工具结果消息下标（无则 -1）。
//
// Python: CodeTaskPlanningRail._last_todo_result_index
func lastTodoResultIndex(messages []llmschema.BaseMessage) int {
	toolNameByID := map[string]string{}
	for _, message := range messages {
		assistant, ok := message.(*llmschema.AssistantMessage)
		if !ok || assistant == nil {
			continue
		}
		for _, call := range assistant.ToolCalls {
			if call == nil || call.ID == "" {
				continue
			}
			toolNameByID[call.ID] = call.Name
		}
	}
	lastIndex := -1
	for index, message := range messages {
		toolMessage, ok := message.(*llmschema.ToolMessage)
		if !ok || toolMessage == nil {
			continue
		}
		if strings.HasPrefix(toolNameByID[toolMessage.ToolCallID], "todo_") {
			lastIndex = index
		}
	}
	return lastIndex
}

// taskReminderContentCandidates 从渲染文本里切出可能的提醒内容候选。
//
// Python: CodeTaskPlanningRail._task_reminder_content_candidates
func taskReminderContentCandidates(rendered string) []string {
	candidates := []string{}
	boundaries := []string{
		taskReminderBlockSeparator,
		"\n\nThe following previously supplied dynamic context",
		"\n\n以下先前提供的动态上下文已不再生效",
		"\n</system-reminder>",
	}
	for _, prefix := range []string{taskReminderCurrentPrefix, taskReminderLegacyPrefix} {
		start := strings.Index(rendered, prefix)
		if start < 0 {
			continue
		}
		endpoints := map[int]struct{}{len(rendered): {}}
		for _, boundary := range boundaries {
			searchFrom := start
			for {
				end := strings.Index(rendered[searchFrom:], boundary)
				if end < 0 {
					break
				}
				end += searchFrom
				endpoints[end] = struct{}{}
				searchFrom = end + len(boundary)
			}
		}
		ends := make([]int, 0, len(endpoints))
		for end := range endpoints {
			ends = append(ends, end)
		}
		sort.Ints(ends)
		for _, end := range ends {
			if end > start {
				candidates = append(candidates, strings.TrimRight(rendered[start:end], " \t\n"))
			}
		}
	}
	return candidates
}

// deliverySequenceFromContent 从内容里的投递标记解析序号（无标记返回 0）。
//
// Python: CodeTaskPlanningRail._delivery_sequence_from_content
func deliverySequenceFromContent(content string) int {
	index := strings.LastIndex(content, taskReminderDeliveryMarker)
	if index < 0 {
		return 0
	}
	rest := content[index+len(taskReminderDeliveryMarker):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// taskReminderHash 计算提醒内容的附件哈希（与投递账本同口径）。
//
// Python: CodeTaskPlanningRail._task_reminder_hash
func taskReminderHash(content, sessionID string) string {
	return prompts.HashPromptAttachment(&prompts.Attachment{
		Section:     taskReminderSection,
		Kind:        prompts.AttachmentTodoReminder,
		Content:     content,
		Source:      taskReminderSource,
		SessionID:   sessionID,
		ContentKind: "text/markdown",
	})
}

// buildTaskReminderContent 构造提醒正文（逐字对齐上游）。
//
// Python: CodeTaskPlanningRail._build_task_reminder_content
func buildTaskReminderContent() string {
	return "Use the task tools when the current work would benefit from tracking " +
		"progress. Use todo_list to read the latest task state, todo_create " +
		"to add new tasks, and todo_modify to update task status (set to " +
		"in_progress when starting and completed when done). Consider " +
		"cleaning up the task list if it has become stale. Ignore this " +
		"periodic reminder when task tracking is not relevant, and never " +
		"mention the reminder to the user."
}

// buildLegacyTaskReminderContent 重建旧版提醒内容（内嵌待办列表）。
//
// Python: CodeTaskPlanningRail._build_legacy_task_reminder_content
func buildLegacyTaskReminderContent(todos []hschema.TodoItem) string {
	message := "The task tools haven't been used recently. If you're working on " +
		"tasks that would benefit from tracking progress, consider using " +
		"todo_create to add new tasks and todo_modify to update task status " +
		"(set to in_progress when starting, completed when done). Also " +
		"consider cleaning up the task list if it has become stale. Only " +
		"use these if relevant to the current work. This is just a gentle " +
		"reminder - ignore if not applicable. Make sure that you NEVER " +
		"mention this reminder to the user"
	if len(todos) == 0 {
		return message
	}
	items := make([]string, 0, len(todos))
	for _, todo := range todos {
		items = append(items, fmt.Sprintf("#%s. [%s] %s", todo.ID, todo.Status.String(), todo.Content))
	}
	return message + "\n\nHere are the existing tasks:\n\n" + strings.Join(items, "\n")
}

// nonNegativeInt 把任意值转成非负整数（失败返回 0）。
func nonNegativeInt(value any) int {
	switch v := value.(type) {
	case int:
		if v < 0 {
			return 0
		}
		return v
	case int64:
		if v < 0 {
			return 0
		}
		return int(v)
	case float64:
		if v < 0 {
			return 0
		}
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || parsed < 0 {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// maxInt 返回较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
