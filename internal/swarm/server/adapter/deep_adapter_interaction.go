package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StartInteraction 绑定产品会话并启动本适配器的 DeepAgent 交互循环。
// Python: interface_deep.py:15044-15072 start_interaction
//
// 这是 GoalManager 的唯一构造点（deep_agent.py:3972，注释明确
// "start() is the only place that constructs GoalManager"），因此
// command.goal 的一切能力都以本方法先跑过为前提。
func (d *DeepAdapter) StartInteraction(ctx context.Context, sessionID string) error {
	if d.instance == nil {
		return fmt.Errorf("DeepAgent 实例未初始化，无法启动交互")
	}
	if sessionID == "" {
		return fmt.Errorf("start_interaction 需要显式 session_id")
	}
	sess := session.CreateAgentSession(sessionID, d.instance.Card(), nil)
	if err := sess.PreRun(ctx, map[string]any{}); err != nil {
		return fmt.Errorf("会话预运行失败: %w", err)
	}
	if err := d.instance.Start(ctx, sess); err != nil {
		return fmt.Errorf("启动交互循环失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startInteractionIfNeeded 幂等启动（已绑定同会话则直接返回）。
// Python: interface_deep.py:15044（上游 start 内部对同会话重复调用返回 None）
func (d *DeepAdapter) startInteractionIfNeeded(ctx context.Context, sessionID string) error {
	if d.instance == nil {
		return nil
	}
	if d.instance.InteractionStarted() {
		bound := d.instance.InteractionSession()
		if bound == nil || bound.GetSessionID() == sessionID {
			return nil
		}
		return fmt.Errorf("交互循环已绑定会话 %s，无法改绑 %s",
			bound.GetSessionID(), sessionID)
	}
	return d.StartInteraction(ctx, sessionID)
}

// interactionStarted 交互循环是否已启动。
// Python: interface_deep.py:15418-15423 _instance_interaction_started
func (d *DeepAdapter) interactionStarted() bool {
	if d.instance == nil {
		return false
	}
	return d.instance.InteractionStarted()
}

// interactionSession 返回 start_interaction 绑定的长生命周期会话。
// Python: interface_deep.py:16230-16248 _active_loop_session
func (d *DeepAdapter) interactionSession() *session.Session {
	if d.instance == nil {
		return nil
	}
	return d.instance.InteractionSession()
}

// ──────────────────────────── 结构体 ────────────────────────────

// outputHandoff 宿主自持的输出排空屏障。
// Python: output_handoff.py OutputHandoff（整文件）
//
// Core 可能在宿主消费完交互卡之前就排队 EOF。向这种 finishing 租约投递答案
// 会丢掉后续输出——所以下一次中断派发必须等这个读者释放所有权，而不是重发
// 输入、抢租约或中止上一轮。
type outputHandoff struct {
	// owner 持有租约的 DeepAgent
	owner *harness.DeepAgent
	// stream 被包裹的输出流
	stream *hschema.InteractionOutputStream
	// drained 老读者是否已排空
	drained chan struct{}
	// once 保证 drained 只关一次
	once sync.Once
	// mu 保护 closeErr
	mu sync.Mutex
	// closeErr 关闭时的错误（供 Wait 上抛）
	closeErr error
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newOutputHandoff 构造排空屏障。
func newOutputHandoff(owner *harness.DeepAgent, stream *hschema.InteractionOutputStream) *outputHandoff {
	return &outputHandoff{owner: owner, stream: stream, drained: make(chan struct{})}
}

// Drained 老读者是否已排空。
func (h *outputHandoff) Drained() bool {
	select {
	case <-h.drained:
		return true
	default:
		return false
	}
}

// Wait 等待老读者释放所有权。
// Python: output_handoff.py wait（关闭失败时上抛）
func (h *outputHandoff) Wait(ctx context.Context) error {
	select {
	case <-h.drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closeErr != nil {
		return fmt.Errorf("上一个输出所有者关闭失败: %w", h.closeErr)
	}
	return nil
}

// markDrained 标记排空并记录关闭错误。
func (h *outputHandoff) markDrained(closeErr error) {
	h.mu.Lock()
	h.closeErr = closeErr
	h.mu.Unlock()
	h.once.Do(func() { close(h.drained) })
}

// attachAndSendInputs 抢输出租约并投递输入，返回流句柄与权限是否已派发。
// Python: interface_deep.py:16008-16085 _attach_and_send_inputs
//
// sendWithoutOutput 为真（steer / follow_up / 中断恢复）时即使拿不到租约
// 也必须投递：HITL 答案若不注入，工具会一直阻塞在等待上。
func (d *DeepAdapter) attachAndSendInputs(
	ctx context.Context, req *schema.AgentRequest, inputs map[string]any,
	sendWithoutOutput bool,
) (*hschema.InteractionOutputStream, bool, error) {
	// 普通中断轮先等前一个读者释放：ACK-only 注入到已排队 EOF 的租约上
	// 会丢掉全部恢复输出。
	if prev := d.interactionOutputHandoff; prev != nil {
		if d.isInterruptResumeDispatch(req.Params) && !d.goalRecordIsActive() {
			if err := prev.Wait(ctx); err != nil {
				return nil, false, err
			}
		}
	}

	stream, err := d.instance.AttachOutput(ctx)
	if err != nil {
		return nil, false, err
	}
	if stream == nil && !sendWithoutOutput {
		return nil, false, nil
	}
	if stream != nil {
		d.interactionOutputHandoff = newOutputHandoff(d.instance, stream)
	}

	sendReq := hschema.SendInputRequest{
		RequestID: req.RequestID,
		Inputs:    inputs,
		Mode:      resolveInputDispatchMode(req.Params),
	}
	if sendErr := d.instance.SendInput(ctx, sendReq); sendErr != nil {
		if stream != nil {
			stream.Close(ctx, false)
			d.interactionOutputHandoff = nil
		}
		return nil, false, sendErr
	}
	return stream, false, nil
}

// openInteractionStream 启动交互、抢租约、投递输入，返回租约帧通道。
// Python: interface_deep.py:19953-20010 attach → send_input → for chunk in stream
//
// accepted 为真表示拿不到租约（另一读者持有）：调用方应只回受理确认帧。
func (d *DeepAdapter) openInteractionStream(
	ctx context.Context, req *schema.AgentRequest, inputs map[string]any, sessionID string,
) (rawCh <-chan stream.Schema, accepted bool, err error) {
	if startErr := d.startInteractionIfNeeded(ctx, sessionID); startErr != nil {
		return nil, false, startErr
	}
	inject := d.shouldInjectIntoExistingInteraction(req.Params)
	interactionStream, _, attachErr := d.attachAndSendInputs(ctx, req, inputs, inject)
	if attachErr != nil {
		return nil, false, attachErr
	}
	if interactionStream == nil {
		return nil, true, nil
	}
	out := make(chan stream.Schema, 64)
	go func() {
		defer close(out)
		defer func() {
			// 结束时释放租约；不连带取消在飞轮（abort=false）。
			interactionStream.Close(context.Background(), false)
			d.interactionOutputHandoff = nil
		}()
		for frame := range interactionStream.All(ctx) {
			select {
			case out <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, false, nil
}

// resolveInputDispatchMode 解析输入分发模式。
// Python: interface_deep.py:15940-15960 _resolve_input_dispatch_mode
func resolveInputDispatchMode(params json.RawMessage) hschema.InputDispatchMode {
	parsed := parseParams(params)
	if strings.ToLower(strings.TrimSpace(paramsString(parsed, "input_mode", ""))) == "steer" {
		return hschema.InputDispatchModeSteer
	}
	return hschema.InputDispatchModeFollowUp
}

// shouldInjectIntoExistingInteraction 拿不到租约时是否仍必须投递。
// Python: interface_deep.py:15802-15806
func (d *DeepAdapter) shouldInjectIntoExistingInteraction(params json.RawMessage) bool {
	parsed := parseParams(params)
	mode := strings.ToLower(strings.TrimSpace(paramsString(parsed, "input_mode", "")))
	if mode == "steer" || mode == "follow_up" {
		return true
	}
	return d.isInterruptResumeDispatch(params)
}

// isInterruptResumeDispatch HITL 答案必须注入既有交互。
// Python: interface_deep.py:15786-15800 _is_interrupt_resume_dispatch
func (d *DeepAdapter) isInterruptResumeDispatch(params json.RawMessage) bool {
	parsed := parseParams(params)
	answers, ok := parsed["answers"].([]any)
	return ok && len(answers) > 0
}

// interactionFrameOutput 从租约帧里取出 SDK 输出块（非输出帧返回 nil）。
func interactionFrameOutput(frame stream.Schema) *stream.OutputSchema {
	switch v := frame.(type) {
	case *stream.OutputSchema:
		return v
	case stream.OutputSchema:
		return &v
	default:
		return nil
	}
}

// aggregateInteractionStream 把租约帧聚合成一次非流式响应载荷。
// Python: interface_deep.py:18480-18560 的非流式聚合
func (d *DeepAdapter) aggregateInteractionStream(
	rawCh <-chan stream.Schema,
) map[string]any {
	usage := &utils.UsageAccumulator{}
	emittedAskUserIDs := make(map[string]bool)
	content := ""
	for frame := range rawCh {
		output := interactionFrameOutput(frame)
		if output == nil {
			continue
		}
		parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
			d.interactionConverter, content != "")
		if parsed == nil {
			continue
		}
		eventType, _ := parsed["event_type"].(string)
		if eventType != "chat.delta" && eventType != "chat.final" {
			continue
		}
		if text, ok := parsed["content"].(string); ok {
			content += text
		}
	}
	return map[string]any{"content": content}
}

// abortSharedAgentIfSafe 对无关会话安全时才中止共享 DeepAgent/调度器。
// Python: interface_deep.py:16277-16310 _abort_shared_agent_if_safe
//
// 绝不在 goal 轮在飞时中止：goal 监督器拥有自己的生命周期，这里 abort
// 共享 DeepAgent（例如前端"发送前先 cancel"触发的 stream_cancel）会杀掉
// goal 生产者，让该会话的后端永久冻结。goal 只能由 /goal pause|clear 停止。
//
// 返回 true 表示已执行中止；false 表示跳过。
func (d *DeepAdapter) abortSharedAgentIfSafe(ctx context.Context, normalizedSID, intent string) bool {
	if d.instance == nil {
		return true
	}
	if d.hasActiveGoalRound() {
		logger.Info(logComponent).
			Str("event_type", "GOAL_ABORT_SKIPPED").
			Str("intent", intent).
			Str("session_id", normalizedSID).
			Msg("interrupt: active goal round -> skip instance.abort (goal controlled via /goal pause|clear)")
		return false
	}
	otherCount := d.otherActiveSessions(normalizedSID)
	if otherCount > 0 {
		// instance.abort() 是共享 DeepAgent 上的全局操作，会中止所有会话；
		// 有其它会话活跃时不得调用。按会话收尾（rail abort / 取消该会话任务 /
		// 杀 shell 进程）足以停掉目标会话的工作。
		logger.Info(logComponent).
			Str("event_type", "ABORT_SKIPPED_OTHER_SESSIONS").
			Str("intent", intent).
			Str("session_id", normalizedSID).
			Int("other_active_count", otherCount).
			Msg("interrupt: skip instance.abort/scheduler cancel, other sessions still active")
		return false
	}
	d.instance.Abort(ctx)
	// 协作式 abort 只能在 checkpoint 之间生效，无法打断 in-flight LLM HTTP 调用。
	// 直接取消 TaskScheduler 中运行中的任务，注入取消到 await 点。
	d.cancelSchedulerRunningTasks()
	return true
}

// ──────────────────────────── 常量 ────────────────────────────

// roundVisibleTextMaxChars 降级去重文本上限（够放完整答案，又有界）。
// Python: interface_deep.py:208 _ROUND_VISIBLE_TEXT_MAX_CHARS
const roundVisibleTextMaxChars = 256 * 1024

// ──────────────────────────── 非导出函数 ────────────────────────────

// trackRoundOutputBoundary 给「当前消费的 chunk 属于哪一轮」盖章。
// Python: interface_deep.py:15542-15573
//
// 输出从队列读，active_round 可能已经指向下一个（goal）轮，而本轮的尾巴
// 还在队列里。逐 chunk 现查会把尾巴误标成 goal，进而把 user 轮的终止 final
// 降级、把两个答案糊进同一个气泡。所以每轮只在第一个 chunk 采样一次。
func (d *DeepAdapter) trackRoundOutputBoundary(output *stream.OutputSchema) {
	if d.streamRoundOutputEnded {
		d.resetRoundKindLatch()
	}
	if d.streamRoundKindLatch == "" {
		d.streamRoundKindLatch = d.currentInteractionRunKind()
	}
	if isRoundTerminalChunkType(output.Type) {
		d.streamRoundOutputEnded = true
	}
}

// isRoundTerminalChunkType 该类型是否为一轮输出的最后一块。
// Python: interface_deep.py:197-203 _ROUND_TERMINAL_CHUNK_TYPES
// （answer / chat.ask_user_question / __interaction__）
func isRoundTerminalChunkType(chunkType string) bool {
	switch chunkType {
	case "answer", "chat.ask_user_question", "__interaction__":
		return true
	default:
		return false
	}
}

// resetRoundKindLatch 忘记上一轮的归属，下一块重新采样。
// Python: interface_deep.py:15512-15516
func (d *DeepAdapter) resetRoundKindLatch() {
	d.streamRoundKindLatch = ""
	d.streamRoundOutputEnded = false
	d.streamRoundVisibleText = ""
}

// noteRoundVisibleText 记住本轮已流出的可见文本（尾部截断到上限）。
// Python: interface_deep.py:15518-15525
func (d *DeepAdapter) noteRoundVisibleText(content string) {
	if content == "" {
		return
	}
	text := d.streamRoundVisibleText + content
	if len(text) > roundVisibleTextMaxChars {
		text = text[len(text)-roundVisibleTextMaxChars:]
	}
	d.streamRoundVisibleText = text
}

// currentInteractionRunKind 当前活跃轮的 run kind（未启动/无活跃轮返回空串）。
// Python: interface_deep.py:15497-15508
func (d *DeepAdapter) currentInteractionRunKind() string {
	if d.instance == nil || !d.instance.InteractionStarted() {
		return ""
	}
	active := d.instance.ActiveRound()
	if active == nil || active.Work == nil {
		return ""
	}
	return string(active.Work.Kind)
}

// adaptGoalIntermediateFinal 把 goal attempt 边界的 chat.final 降级。
// Python: interface_deep.py:15619-15637
//
// 返回 (payload, keep)：keep=false 表示应当丢弃该帧。
func (d *DeepAdapter) adaptGoalIntermediateFinal(chunk map[string]any) (map[string]any, bool) {
	if eventType, _ := chunk["event_type"].(string); eventType != "chat.final" {
		return chunk, true
	}
	if !d.shouldDemoteGoalIntermediateFinal() {
		return chunk, true
	}
	content, _ := chunk["content"].(string)
	if d.goalIntermediateFinalRepeatsStreamedText(content) {
		// 气泡（以及历史）已经有这段文本。降级成 delta 会追加第二份，
		// 因为只有真正的 chat.final 才替换气泡内容。
		return nil, false
	}
	adapted := make(map[string]any, len(chunk)+1)
	for key, value := range chunk {
		adapted[key] = value
	}
	adapted["event_type"] = "chat.delta"
	adapted[goalIntermediateField] = true
	return adapted, true
}

// shouldDemoteGoalIntermediateFinal attempt 边界的 chat.final 是否必须降级。
// Python: interface_deep.py:15470-15500
//
// 只在「GoalRecord 仍 ACTIVE」且「正在关闭的文本属于 goal 轮」时降级。
// 归属优先取轮次闩（chunk 的来源证明），再取 streamContentRunKind，
// 最后才回落 live active_round。
func (d *DeepAdapter) shouldDemoteGoalIntermediateFinal() bool {
	if !d.goalRecordIsActive() {
		return false
	}
	if kind := d.streamRoundKindLatch; kind != "" {
		return kind == "goal"
	}
	if kind := d.streamContentRunKind; kind != "" {
		return kind == "goal"
	}
	return d.hasActiveGoalRound()
}

// goalIntermediateFinalRepeatsStreamedText 降级后的 final 是否只是重复已流文本。
// Python: interface_deep.py:15527-15540
func (d *DeepAdapter) goalIntermediateFinalRepeatsStreamedText(content string) bool {
	answer := strings.TrimSpace(content)
	if answer == "" {
		return true
	}
	streamed := strings.TrimSpace(d.streamRoundVisibleText)
	return streamed != "" && strings.Contains(streamed, answer)
}
