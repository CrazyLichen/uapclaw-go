package schema

import (
	"github.com/google/uuid"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptRequester 中断请求接口，InterruptRequest 及其子类均满足。
// Python: isinstance(x, InterruptRequest) — Python 中 interrupt_requests 的值类型
// 实际可存 InterruptRequest 或其子类（如 AskUserRequest, ToolCallInterruptRequest）。
// Go 中通过接口实现多态：handler 存接口值，JSON 序列化按运行时具体类型输出字段。
// Python: model_config = {"extra": "allow"}：子类扩展字段（如 questions）
// 通过接口多态自然保留，序列化时由具体类型决定输出内容。
type InterruptRequester interface {
	// GetMessage 返回向用户展示的确认消息
	GetMessage() string
	// GetAutoConfirmKey 返回自动确认配置键
	GetAutoConfirmKey() string
	// SetAutoConfirmKey 覆盖自动确认配置键
	//
	// Python 侧是直接属性赋值（request.auto_confirm_key = ""），
	// Go 用接口方法表达同一能力，避免调用方做类型断言。
	SetAutoConfirmKey(key string)
	// GetMetadata 返回中断元数据
	//
	// rail 通过它把 host 侧信息（如 tool_invocation_key）挂到中断上，
	// 队列在恢复时凭它把答案与卡片对应起来。
	GetMetadata() map[string]any
	// SetMetadata 覆盖中断元数据
	SetMetadata(metadata map[string]any)
	// GetInterruptOwner 返回抛出该中断的 rail 键
	//
	// 恢复时答案按此键路由回原 rail；同一次调用上多个 rail 的中断靠它区分。
	GetInterruptOwner() string
	// GetInterruptKind 返回中断类型
	GetInterruptKind() string
	// GetInterruptID 返回本次中断的唯一标识（处理器发号后有值）。
	//
	// Python 侧 interrupt_id 是基类属性，子类天然继承；Go 的类型断言式读取
	// （*InterruptRequest）对嵌入基类的子类（ClientToolRequest 等）失效，
	// 经接口方法表达同一多态语义（2026-11-14 client tool 恢复链实锤）。
	GetInterruptID() string
	// SetInterruptID 处理器发号时就地写入（Python: issue_interrupt_id 直接
	// 属性赋值 request.interrupt_id = ...）。
	SetInterruptID(id string)
	// SetInterruptKind 就地写入中断类型（Python: stamp_interrupt 的
	// request.interrupt_kind = ... 属性赋值）。
	//
	// 走接口方法而非 *InterruptRequest 类型断言：嵌入基类的子类
	// （AskUserRequest 等）在具体类型断言下失效——盖不上 kind/owner，
	// 答案按 owner 路由整个失效（2026-11-14 ask_user 断链实锤，与
	// GetInterruptID/SetInterruptID 同款修正）。
	SetInterruptKind(kind string)
	// SetInterruptOwner 就地写入中断 owner（同上，stampInterrupt 用）。
	SetInterruptOwner(owner string)
}

// InterruptRequest 工具中断请求，携带需要用户确认的信息。
// Python: InterruptRequest(message, payload_schema, auto_confirm_key, ui_options,
// metadata, interrupt_id, interrupt_kind, interrupt_owner)
//
// 身份字段说明（对齐 Python docstring）：
//   - InterruptID：本次中断的唯一标识，由中断处理器在收集暂停调用时填入；
//     宿主通过 InteractiveInput.answer(interrupt_id, value) 回答它。
//   - InterruptKind：这是什么类型的问题（见 InterruptKind），供宿主渲染并决定
//     答案形状。取值是普通字符串，以便第三方 rail 声明自己的类型；空串表示
//     抛出方未声明。
//   - InterruptOwner：抛出该中断的 rail 的稳定键；答案只会投递给那个 rail。
//     答案路由**始终按 owner**，与 kind 无关。
type InterruptRequest struct {
	// Message 向用户展示的确认消息
	Message string
	// PayloadSchema 用户输入的数据结构定义 (JSON Schema)
	PayloadSchema map[string]any
	// AutoConfirmKey 自动确认的配置键
	AutoConfirmKey string
	// UIOptions UI 选项（对齐 Python ui_options: list[dict] | None）
	UIOptions []map[string]any
	// Metadata 中断元数据，承载宿主与 rail 之间传递的附加信息
	Metadata map[string]any
	// InterruptID 本次中断的唯一标识，由中断处理器填入
	InterruptID string
	// InterruptKind 中断类型（见 InterruptKind），未声明时为空串
	InterruptKind string
	// InterruptOwner 抛出该中断的 rail 的稳定键，答案按此路由
	InterruptOwner string
}

// ToolCallInterruptRequest 工具调用级中断请求，扩展 InterruptRequest。
// 用于序列化中断信息给用户输出。
// Python: ToolCallInterruptRequest(InterruptRequest)
// + model_config = {"extra": "allow"} 保留子类额外字段
//
// Request 字段使用 InterruptRequester 接口，支持存储 InterruptRequest 子类
// （如 AskUserRequest），JSON 序列化按运行时具体类型输出所有字段，
// Python: model_dump() + extra="allow" 的子类字段透传行为。
type ToolCallInterruptRequest struct {
	// Request 中断请求接口，可存 InterruptRequest 或其子类（如 AskUserRequest）
	Request InterruptRequester `json:"request"`
	// ToolName 工具名称
	ToolName string `json:"tool_name"`
	// ToolCallID 工具调用 ID
	ToolCallID string `json:"tool_call_id"`
	// ToolArgs 工具参数 JSON 字符串（和 ToolCall.Arguments 一致）
	ToolArgs string `json:"tool_args"`
	// Index 工具调用索引（0 表示未设置，和 ToolCall.Index 语义一致）
	Index int `json:"index"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// InterruptKind 中断类型的已知取值。
// kind 告诉宿主如何呈现一个中断、答案应是什么形状。取值是普通字符串
// （而非封闭枚举），以便第三方 rail 声明自己的类型；空串表示抛出方未声明。
// Python: class InterruptKind
const (
	// InterruptKindUndeclared 抛出方未声明类型
	InterruptKindUndeclared = ""
	// InterruptKindPermission 权限审批
	InterruptKindPermission = "permission"
	// InterruptKindConfirm 确认
	InterruptKindConfirm = "confirm"
	// InterruptKindAskUser 向用户反问
	InterruptKindAskUser = "ask_user"
	// InterruptKindSkillEvolutionApproval 技能演化审批
	InterruptKindSkillEvolutionApproval = "skill_evolution_approval"
	// InterruptKindTeamToolApproval 团队工具审批
	InterruptKindTeamToolApproval = "team_tool_approval"
	// InterruptKindClientTool 客户端工具调用
	InterruptKindClientTool = "client_tool"
)

// ──────────────────────────── 常量 ────────────────────────────

// ClientToolInterruptOwner 能力管理器为客户端工具抛出的中断的 owner 键。
// Python: CLIENT_TOOL_INTERRUPT_OWNER = "openjiuwen.client_tool"
const ClientToolInterruptOwner = "openjiuwen.client_tool"

// ──────────────────────────── 导出函数 ────────────────────────────

// InterruptKindOf 返回中断类型，未声明时返回空串。
// 字段存在之前持久化的技能演化审批数据把 kind 只存在 metadata["interrupt_kind"]
// 里，此处兼容该历史形态。
// Python: interrupt_kind_of(request)
func InterruptKindOf(request *InterruptRequest) string {
	if request == nil {
		return InterruptKindUndeclared
	}
	if request.InterruptKind != "" {
		return request.InterruptKind
	}
	if request.Metadata != nil {
		if kind, ok := request.Metadata["interrupt_kind"].(string); ok {
			return kind
		}
	}
	return InterruptKindUndeclared
}

// NewToolCallInterruptRequest 从 InterruptRequester 和 ToolCall 创建 ToolCallInterruptRequest。
// Python: ToolCallInterruptRequest.from_tool_call(request, tool_call)
// request 参数接受 InterruptRequester 接口，支持传入子类（如 AskUserRequest），
// 子类扩展字段通过接口多态保留在 Request 字段中。
func NewToolCallInterruptRequest(request InterruptRequester, toolCall *llmschema.ToolCall) *ToolCallInterruptRequest {
	return &ToolCallInterruptRequest{
		Request:    request,
		ToolName:   toolCall.Name,
		ToolCallID: toolCall.ID,
		ToolArgs:   toolCall.Arguments,
		Index:      toolCall.Index,
	}
}

// NewInterruptID 生成一个全局唯一的中断 ID。
// Python: new_interrupt_id() -> f"intr_{uuid.uuid4().hex}"
func NewInterruptID() string {
	return "intr_" + uuid.NewString()
}

// IssueInterruptID 给挂在 toolCallID 上的中断就地分配 ID。
// 中断处理器对每个从暂停调用收集到的中断调用它。unanswered 把每个调用映射到
// 「本次恢复在该调用上留下的未答中断」的 (owner, interrupt_id)：
// 若同一 owner 的上一轮中断仍未答，则新中断沿用那个 id——这样宿主分批回答
// 并行调用时手里的 id 一直有效，即使 rail 这次问的是不同的问题；
// 其余中断一律分配新 id。
// Python: issue_interrupt_id(request, tool_call_id, unanswered)
func IssueInterruptID(
	request *InterruptRequest,
	toolCallID string,
	unanswered map[string]AnswerOwnerAndID,
) {
	if request == nil {
		return
	}
	previous, ok := unanswered[toolCallID]
	if ok && previous.Owner == request.InterruptOwner {
		request.InterruptID = previous.InterruptID
		return
	}
	request.InterruptID = NewInterruptID()
}

// IssueInterruptRequesterID 对任意 InterruptRequester 实现就地发号。
// Python: issue_interrupt_id(request, tool_call_id, unanswered)（handler.py）——
// 任意子类经基类属性多态发号；Go 经接口方法等价实现。
func IssueInterruptRequesterID(
	request InterruptRequester,
	toolCallID string,
	unanswered map[string]AnswerOwnerAndID,
) {
	if request == nil {
		return
	}
	previous, ok := unanswered[toolCallID]
	if ok && previous.Owner == request.GetInterruptOwner() {
		request.SetInterruptID(previous.InterruptID)
		return
	}
	request.SetInterruptID(NewInterruptID())
}

// AnswerOwnerAndID 上一轮未答中断的归属信息。
// Python: _Unanswered 的 value 形态 Tuple[str, str]（owner, interrupt_id）
type AnswerOwnerAndID struct {
	// Owner 中断的 owner
	Owner string
	// InterruptID 中断标识
	InterruptID string
}

// GetMessage 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetMessage() string { return r.Message }

// GetAutoConfirmKey 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetAutoConfirmKey() string { return r.AutoConfirmKey }

// SetAutoConfirmKey 实现 InterruptRequester 接口。
func (r *InterruptRequest) SetAutoConfirmKey(key string) { r.AutoConfirmKey = key }

// RequestInterruptKind 返回中断类型。
// 供 permissions 包的 InteractionRequestProvider 接口消费：root 权限队列需要
// 读 interrupt_kind 区分客户端工具中断与权限卡，但不希望依赖 schema 具体类型。
func (r *InterruptRequest) RequestInterruptKind() string { return r.InterruptKind }

// RequestMetadata 返回中断元数据。
// 供 permissions 包的 InteractionRequestProvider 接口消费（读 tool_invocation_key）。
func (r *InterruptRequest) RequestMetadata() map[string]any { return r.Metadata }

// GetMetadata 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetMetadata() map[string]any { return r.Metadata }

// SetMetadata 实现 InterruptRequester 接口。
func (r *InterruptRequest) SetMetadata(metadata map[string]any) { r.Metadata = metadata }

// GetInterruptOwner 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetInterruptOwner() string { return r.InterruptOwner }

// GetInterruptID 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetInterruptID() string { return r.InterruptID }

// SetInterruptID 实现 InterruptRequester 接口（处理器发号用）。
func (r *InterruptRequest) SetInterruptID(id string) { r.InterruptID = id }

// GetInterruptKind 实现 InterruptRequester 接口。
// 走 InterruptKindOf 以兼容 kind 只存在 metadata 里的历史数据。
func (r *InterruptRequest) GetInterruptKind() string { return InterruptKindOf(r) }

// SetInterruptKind 实现 InterruptRequester 接口（stampInterrupt 写入用）。
func (r *InterruptRequest) SetInterruptKind(kind string) { r.InterruptKind = kind }

// SetInterruptOwner 实现 InterruptRequester 接口（stampInterrupt 写入用）。
func (r *InterruptRequest) SetInterruptOwner(owner string) { r.InterruptOwner = owner }

// ModelDump 实现 harness_providers.ToJsonSafe 的 model_dump 缝（spec D3）。
// Python: InterruptRequest(BaseModel).model_dump()——键为 snake_case 字段名，
// jsonsafe.py:36-38 据此把 pydantic 模型转成 dict；没有本方法时 Go 的
// ToJsonSafe 对 struct 走 fmt.Sprint 兜底，_interrupt_prompt 等消费方拿不到字段。
func (r *InterruptRequest) ModelDump(_ string, _ bool) (any, error) {
	return map[string]any{
		"message":          r.Message,
		"payload_schema":   r.PayloadSchema,
		"auto_confirm_key": r.AutoConfirmKey,
		"ui_options":       r.UIOptions,
		"metadata":         r.Metadata,
		"interrupt_id":     r.InterruptID,
		"interrupt_kind":   r.InterruptKind,
		"interrupt_owner":  r.InterruptOwner,
	}, nil
}

// ModelDump 实现 model_dump 缝：内嵌请求字段 + 工具上下文字段平铺到顶层。
// Python: ToolCallInterruptRequest（response.py:92-127，extra="allow"，
// from_tool_call 用 request.model_dump() 平铺子类字段）。
func (r *ToolCallInterruptRequest) ModelDump(_ string, _ bool) (any, error) {
	out := map[string]any{
		"tool_name":    r.ToolName,
		"tool_call_id": r.ToolCallID,
		"tool_args":    r.ToolArgs,
		"index":        r.Index,
	}
	if r.Request != nil {
		if dumped, err := r.Request.(interface {
			ModelDump(mode string, byAlias bool) (any, error)
		}).ModelDump("", false); err == nil {
			if base, ok := dumped.(map[string]any); ok {
				for key, value := range base {
					out[key] = value
				}
			}
		}
	}
	return out, nil
}

// GetMessage 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetMessage() string {
	if r.Request != nil {
		return r.Request.GetMessage()
	}
	return ""
}

// GetAutoConfirmKey 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetAutoConfirmKey() string {
	if r.Request != nil {
		return r.Request.GetAutoConfirmKey()
	}
	return ""
}

// SetAutoConfirmKey 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) SetAutoConfirmKey(key string) {
	if r.Request != nil {
		r.Request.SetAutoConfirmKey(key)
	}
}

// GetMetadata 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetMetadata() map[string]any {
	if r.Request != nil {
		return r.Request.GetMetadata()
	}
	return nil
}

// SetMetadata 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) SetMetadata(metadata map[string]any) {
	if r.Request != nil {
		r.Request.SetMetadata(metadata)
	}
}

// GetInterruptOwner 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetInterruptOwner() string {
	if r.Request != nil {
		return r.Request.GetInterruptOwner()
	}
	return ""
}

// GetInterruptKind 实现 InterruptRequester 接口，委托给 Request 字段。
// GetInterruptID 实现 InterruptRequester 接口，委托内嵌请求。
func (r *ToolCallInterruptRequest) GetInterruptID() string {
	if r.Request != nil {
		return r.Request.GetInterruptID()
	}
	return ""
}

// SetInterruptID 实现 InterruptRequester 接口，委托内嵌请求。
func (r *ToolCallInterruptRequest) SetInterruptID(id string) {
	if r.Request != nil {
		r.Request.SetInterruptID(id)
	}
}

func (r *ToolCallInterruptRequest) GetInterruptKind() string {
	if r.Request != nil {
		return r.Request.GetInterruptKind()
	}
	return ""
}

// SetInterruptKind 实现 InterruptRequester 接口，委托内嵌请求。
func (r *ToolCallInterruptRequest) SetInterruptKind(kind string) {
	if r.Request != nil {
		r.Request.SetInterruptKind(kind)
	}
}

// SetInterruptOwner 实现 InterruptRequester 接口，委托内嵌请求。
func (r *ToolCallInterruptRequest) SetInterruptOwner(owner string) {
	if r.Request != nil {
		r.Request.SetInterruptOwner(owner)
	}
}
