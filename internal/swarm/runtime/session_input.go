package runtime

import (
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// 本文件对应上游两份字节相同的副本，合并为一份（spec DEC-13，登记为偏差 D-1）：
//   - gateway/runtime/session_input.py
//   - jiuwenswarm/runtime/session_input.py

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// steerImageFields steer 图片引用允许的字段集合（5 项）。
	// Python: session_input.py _STEER_IMAGE_FIELDS
	steerImageFields = map[string]struct{}{
		"type": {}, "path": {}, "filename": {}, "mime_type": {}, "size_bytes": {},
	}
	// steerImageMIMETypes steer 图片允许的 MIME（4 项）。
	// Python: session_input.py _STEER_IMAGE_MIME_TYPES
	steerImageMIMETypes = map[string]struct{}{
		"image/png": {}, "image/jpeg": {}, "image/webp": {}, "image/gif": {},
	}
	// steerForbiddenAttachmentKeys steer 模式下不允许出现的附件字段。
	// Python: validate_steer_image_references 内的循环常量
	steerForbiddenAttachmentKeys = []string{
		"images", "image_files", "files", "attachments", "audio_files", "video_files",
	}
)

// ──────────────────────────── 类型别名 ────────────────────────────

// 三个拒收异常与输入意图枚举定义在 session 包（见 session/session_input_errors.go）。
// 原因：session 包零内部依赖、被 runtime 包依赖；Python 的 session_input.py 虽在
// runtime/ 下，但 Go 的包即编译边界，定义在本包会让 session 包反向依赖成环。
// 此处用类型别名 re-export，保持 runtime 包对外的 API 面与上游调用点一致。

// SessionInputMode 会话补充输入的意图（别名）。
type SessionInputMode = sessionpkg.SessionInputMode

const (
	// SessionInputModeSteer 转向（别名）
	SessionInputModeSteer = sessionpkg.SessionInputModeSteer
	// SessionInputModeFollowUp 追加（别名）
	SessionInputModeFollowUp = sessionpkg.SessionInputModeFollowUp
)

// SessionInputRejectedError 已知拒收：输入未到达 SDK 队列（别名）。
type SessionInputRejectedError = sessionpkg.SessionInputRejectedError

// SessionInputTargetError 补充输入不得变成另一个执行的工作（别名）。
type SessionInputTargetError = sessionpkg.SessionInputTargetError

// SessionInputQueueRequiredError 跨会话输入须等普通任务准入（别名）。
type SessionInputQueueRequiredError = sessionpkg.SessionInputQueueRequiredError

// 错误码常量（别名，供调用方判断）。
const (
	sessionInputRejectedCode      = sessionpkg.SessionInputRejectedCode
	sessionInputTargetChangedCode = sessionpkg.SessionInputTargetChangedCode
	sessionInputQueueRequiredCode = sessionpkg.SessionInputQueueRequiredCode
)

// ResolveSessionInputMode 归一化 input_mode 与其遗留别名 runtime_mode。
// Python: session_input.py resolve_session_input_mode
//
// 未知值保留普通发送行为（返回 nil）；交互应答在此之前的别处单独分类。
// 主字段与别名冲突时上游抛 ValueError，此版本吞掉错误返回 nil；
// 需要错误时用 ResolveSessionInputModeChecked。
func ResolveSessionInputMode(params map[string]any) *SessionInputMode {
	mode, _ := ResolveSessionInputModeChecked(params)
	return mode
}

// ResolveSessionInputModeChecked 同 ResolveSessionInputMode，但冲突时报错。
// Python: session_input.py resolve_session_input_mode 的 raise ValueError 分支
func ResolveSessionInputModeChecked(params map[string]any) (*SessionInputMode, error) {
	if params == nil {
		return nil, nil
	}
	primary := params["input_mode"]
	alias := params["runtime_mode"]
	// 上游用真值判断：primary and alias and normalize(primary) != normalize(alias)
	// 空串为假，因此空的主字段不会触发冲突，而是回落到别名。
	if isTruthySessionInputValue(primary) && isTruthySessionInputValue(alias) &&
		normalizeSessionInputRaw(primary) != normalizeSessionInputRaw(alias) {
		return nil, errors.New("input_mode and runtime_mode must agree")
	}
	rawValue := primary
	if !isTruthySessionInputValue(rawValue) {
		rawValue = alias
	}
	if mode, ok := rawValue.(SessionInputMode); ok {
		return &mode, nil
	}
	switch normalizeSessionInputRaw(rawValue) {
	case string(SessionInputModeSteer):
		mode := SessionInputModeSteer
		return &mode, nil
	case string(SessionInputModeFollowUp):
		mode := SessionInputModeFollowUp
		return &mode, nil
	default:
		return nil, nil
	}
}

// ValidateSteerImageReferences 校验 steer 的"仅引用"线格式，不碰文件系统。
// Python: session_input.py validate_steer_image_references
//
// 六类拒收，顺序与上游一致：
//  1. 六个附件字段任一非空
//  2. media_items 不是 list
//  3. 条目不是 mapping，或含 _STEER_IMAGE_FIELDS 之外的键
//  4. type != "image"
//  5. path / filename / mime_type 任一非非空字符串
//  6. mime_type 不在允许集合内
func ValidateSteerImageReferences(params map[string]any) ([]map[string]any, error) {
	for _, key := range steerForbiddenAttachmentKeys {
		if value, ok := params[key]; ok && isTruthySessionInputValue(value) {
			return nil, errors.New(
				"steering accepts uploaded image references only; send other attachments as a queued task")
		}
	}
	rawItems, ok := params["media_items"]
	if !ok || rawItems == nil {
		return nil, nil
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil, errors.New("steer media_items must be a list of uploaded image references")
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("steer images require uploaded file references without inline data")
		}
		for key := range entry {
			if _, allowed := steerImageFields[key]; !allowed {
				return nil, errors.New("steer images require uploaded file references without inline data")
			}
		}
		if entry["type"] != "image" {
			return nil, errors.New("steering accepts uploaded images only")
		}
		for _, key := range []string{"path", "filename", "mime_type"} {
			text, isString := entry[key].(string)
			if !isString || strings.TrimSpace(text) == "" {
				return nil, errors.New("steer image " + key + " must be a non-empty string")
			}
		}
		mimeType, _ := entry["mime_type"].(string)
		if _, allowed := steerImageMIMETypes[mimeType]; !allowed {
			return nil, errors.New("unsupported steer image MIME type")
		}
		out = append(out, entry)
	}
	return out, nil
}

// ValidateSessionInput 校验会话补充输入的整体形状。
// Python: session_input.py validate_session_input
//
// 步骤与上游一致：
//  1. 解析 mode
//  2. 有 expected_execution_id 时：必须非空字符串，且必须 steer 模式
//  3. steer 模式下校验图片引用，得到 image_references
//  4. 有图片引用且（无 query 或 query 是字符串）→ 通过
//  5. 否则 query 必须是非空字符串
func ValidateSessionInput(params map[string]any) error {
	mode, modeErr := ResolveSessionInputModeChecked(params)
	if modeErr != nil {
		return modeErr
	}
	if target, ok := params["expected_execution_id"]; ok {
		targetText, isString := target.(string)
		if !isString || strings.TrimSpace(targetText) == "" {
			return errors.New("expected_execution_id must be a non-empty string")
		}
		if mode == nil || *mode != SessionInputModeSteer {
			return errors.New("expected_execution_id requires steer mode")
		}
	}
	var imageReferences []map[string]any
	if mode != nil && *mode == SessionInputModeSteer {
		refs, refErr := ValidateSteerImageReferences(params)
		if refErr != nil {
			return refErr
		}
		imageReferences = refs
	}
	query := params["query"]
	if len(imageReferences) > 0 {
		if query == nil {
			return nil
		}
		if _, isString := query.(string); isString {
			return nil
		}
	}
	queryText, isString := query.(string)
	if !isString || strings.TrimSpace(queryText) == "" {
		return errors.New("session input requires non-empty text")
	}
	return nil
}

// ──────────────────────────── 结构体 ────────────────────────────

// QueuedSessionInput 已绑定活跃轮的补充输入队列条目。
// Python: session_input.py QueuedSessionInput —— 本体是 ctx.inputs.parts 的
// 一员，BoundaryGuard 按 isinstance 过滤；steering 通道只收 display_content
// 字符串（见 EnqueueBoundSessionInput 注释）。
type QueuedSessionInput struct {
	// Query 用户输入文本（sdk_request.inputs["query"]）
	Query string
	// RequestID 请求信封 id
	RequestID string
	// DisplayContent 前端展示文本（图片-only 补充时取 content/query，绝不
	// 把内部工具提示词当用户原话发布——Python :150 注释对齐）
	DisplayContent string
	// MediaItems steer 图片引用列表（经准入校验的引用条目）
	MediaItems []map[string]any
	// CrossSession 跨会话投递键内容（nil=本会话投递）
	CrossSession map[string]any
	// MessageRoute 跨会话路由五键（session_id/request_id/user_id/
	// chain_id/parent_message_id/hop_count）
	MessageRoute map[string]any
	// BoundaryReady 边界结果信号（入队收尾关闭；BoundaryGuard 消费）
	BoundaryReady chan struct{}
	// BoundaryError 入队/边界失败原因（nil=成功）
	BoundaryError error

	// boundaryClosed 边界信号是否已关闭（幂等标记，非导出）
	boundaryClosed bool
}

// NewQueuedSessionInput 构造边界信号就绪的条目。
func NewQueuedSessionInput(query, requestID string) *QueuedSessionInput {
	return &QueuedSessionInput{
		Query:         query,
		RequestID:     requestID,
		BoundaryReady: make(chan struct{}),
	}
}

// CompleteBoundary 关闭边界信号（成功路径，BoundaryError 保持 nil）。
func (e *QueuedSessionInput) CompleteBoundary() {
	if e.BoundaryReady != nil && !e.boundaryClosed {
		e.boundaryClosed = true
		close(e.BoundaryReady)
	}
}

// FailBoundary 记录边界失败并关闭信号（幂等：重复调用只保留首个错误；
// 首次后信号保持已关闭状态，后续调用不重复 close）。
func (e *QueuedSessionInput) FailBoundary(err error) {
	if e.BoundaryError == nil {
		e.BoundaryError = err
	}
	if e.BoundaryReady != nil && !e.boundaryClosed {
		e.boundaryClosed = true
		close(e.BoundaryReady)
	}
}

// SteerTarget 补充输入投递目标实例的最小能力面。
// Python 为鸭子类型直取 instance.active_round / has_output_stream /
// loop_controller.enqueue_steer；Go 依赖倒置用窄接口承载（规则 10.2：
// 本接口是"DeepAgent 用别人"方向的反例——它是宿主调 agent 的能力，
// 但 EnqueueBoundSessionInput 属宿主侧函数，注入窄接口避免 runtime 包
// 反向依赖 harness）。
type SteerTarget interface {
	// ActiveRoundIs 报告实例当前活跃轮是否仍为 bound（同一轮记账对象）
	ActiveRoundIs(bound any) bool
	// HasOutputStream 报告输出租约是否有消费者
	HasOutputStream() bool
	// EnqueueSteerEntry 同步入队 steering 显示文本（与校验同临界区调用）
	EnqueueSteerEntry(display string) error
}

// EnqueueBoundOptions EnqueueBoundSessionInput 的调用参数。
type EnqueueBoundOptions struct {
	// Instance 投递目标实例
	Instance SteerTarget
	// BoundRound 准入时捕获的活跃轮（不得为 nil）
	BoundRound any
	// Request 原始请求（params 取 expected_execution_id/content/query/跨会话键）
	Request *schema.AgentRequest
	// Query 已解析的 sdk_request.inputs["query"] 文本
	Query string
	// MediaItems steer 图片引用列表（nil=纯文本）
	MediaItems []map[string]any
	// ExecutionLookup 按 execution_id 查账本（nil 时跳过执行复核）
	ExecutionLookup func(executionID string) *sessionpkg.SessionExecutionHandle
}

// EnqueueBoundSessionInput 同步校验并入队活跃轮的 steering queue。
// Python: session_input.py enqueue_bound_session_input（:102-160）
//
// 铁律：目标校验与入队之间不得插入任何可让渡点（含锁获取），否则目标轮
// 可能在间隙结束并错开新轮——调用方保证 Instance 三个方法在同一临界区
// 完成（DeepAgent 的 interactionControlMu 语义）。
// 拒收即错误上抛：入队尚未发生，前端拿到的失败就是"什么都没发"。
func EnqueueBoundSessionInput(opts EnqueueBoundOptions) (*QueuedSessionInput, error) {
	params := requestParamsOf(opts.Request)
	requestID := requestIDOf(opts.Request)

	// ① execution 复核：expected_execution_id 非空时查账本
	// Python: execution = runtime.get_session_execution(expected_id) if expected_id else None
	expectedID, _ := params["expected_execution_id"].(string)
	if opts.ExecutionLookup != nil && expectedID != "" {
		execution := opts.ExecutionLookup(expectedID)
		if execution == nil {
			return nil, &SessionInputTargetError{
				Message: "the targeted execution has ended or changed; supplemental input was not sent",
			}
		}
		if execution.SessionID != sessionIDOf(opts.Request) ||
			execution.State != sessionpkg.SessionExecutionStateRunning ||
			execution.CancellationRequested {
			return nil, &SessionInputTargetError{
				Message: "the targeted execution has ended or changed; supplemental input was not sent",
			}
		}
	}

	// ② bound_round 复核：目标轮必须仍是准入时捕获的那一轮
	// Python: instance.active_round is not target_round or target_round is None
	//         or not instance.has_output_stream()
	if opts.BoundRound == nil || !opts.Instance.ActiveRoundIs(opts.BoundRound) ||
		!opts.Instance.HasOutputStream() {
		return nil, &SessionInputTargetError{
			Message: "the targeted execution has ended or changed; supplemental input was not sent",
		}
	}

	// ③ 构造条目：display_content 优先 content，回落 query
	// Python: content = params.get("content") or params.get("query") or entry
	display := ""
	if text, ok := params["content"].(string); ok {
		display = text
	} else if text, ok := params["query"].(string); ok {
		display = text
	}
	if display == "" {
		display = opts.Query
	}
	entry := NewQueuedSessionInput(opts.Query, requestID)
	entry.DisplayContent = display
	entry.MediaItems = opts.MediaItems

	// ④ 跨会话键：entry.cross_session + message_route 五键
	// Python: cross_session = request.params.get(SESSION_MESSAGE_INTERNAL_KEY)
	if cross, ok := params[SessionMessageInternalKey].(map[string]any); ok {
		entry.CrossSession = make(map[string]any, len(cross)+1)
		for k, v := range cross {
			entry.CrossSession[k] = v
		}
		entry.CrossSession["content"] = entry.DisplayContent
		route := map[string]any{
			"session_id":        sessionIDOf(opts.Request),
			"request_id":        requestID,
			"user_id":           params["user_id"],
			"chain_id":          cross["chain_id"],
			"parent_message_id": cross["message_id"],
			"hop_count":         cross["hop_count"],
		}
		if route["chain_id"] == nil {
			route["chain_id"] = ""
		}
		if route["parent_message_id"] == nil {
			route["parent_message_id"] = ""
		}
		if route["hop_count"] == nil {
			route["hop_count"] = 0
		}
		entry.MessageRoute = route
	}

	// ⑤ 同步入队 steering（display_content 字符串；结构化条目由调用方
	// 挂进 inputs.parts 供 BoundaryGuard 过滤——Python duck-typing 等价）
	// Python: controller.enqueue_steer(entry)
	if err := opts.Instance.EnqueueSteerEntry(entry.DisplayContent); err != nil {
		return nil, err
	}
	return entry, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requestParamsOf 取请求 params（nil 安全；Params 是延迟解析的 RawMessage）。
func requestParamsOf(req *schema.AgentRequest) map[string]any {
	return parseRequestParams(req)
}

// requestIDOf 取请求信封 id（nil 安全）。
func requestIDOf(req *schema.AgentRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

// sessionIDOf 取请求会话标识（nil 安全）。
func sessionIDOf(req *schema.AgentRequest) string {
	if req == nil || req.SessionID == nil {
		return ""
	}
	return strings.TrimSpace(*req.SessionID)
}

// normalizeSessionInputRaw 归一化输入意图取值。
// Python: session_input.py normalize（内部闭包）
//
//	raw.value if isinstance(raw, SessionInputMode) else str(raw).strip().lower()
func normalizeSessionInputRaw(value any) string {
	if mode, ok := value.(SessionInputMode); ok {
		return string(mode)
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(text))
}

// isTruthySessionInputValue 复刻 Python 真值判断。
// None / False / 0 / "" / [] / {} 均为假；其余为真。
func isTruthySessionInputValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return true
	}
}
