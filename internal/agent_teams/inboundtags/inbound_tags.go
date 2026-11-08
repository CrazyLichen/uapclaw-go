package inboundtags

import (
	"fmt"
	"html"
	"strings"
)

// 本文件提供成员入站消息与框架事件的 XML 渲染。
// 团队成员的 harness 输入混了两类东西：另一个成员或用户发来的**原始消息**，
// 以及运行时包裹在它周围的**框架元数据与指令**（message id、时间、回复提示、
// 事件通知）。早期的 i18n 模板把两者拼成一整条扁平字符串，模型分不清「谁说的」
// 与「框架加的」。本文件改用语义化 XML，给模型一条清晰边界：
//
//   - <team-inbound> 原样包裹**原始消息**，sender / message_id / type / time 作为属性。
//   - <team-clock> 每次准入输入附带一个当前时间锚点。
//   - <team-event> 包裹**框架事件**，kind 属性标明类型。
//   - <team-context> 包裹**团队既成事实**（自己的身份、团队元数据）。
//   - <team-policy> 包裹外部 CLI 成员 system prompt 里的**团队准则**。
//   - <team-note> 携带框架附加的提示或约束，**嵌套在所修饰块的内部**作为最后一个
//     子元素：某条提示在说哪件事，是树上的事实，不需要模型按前后位置去猜。
// 这些都是纯结构函数：调用方传入动态数据与已本地化的文本片段（由 schema.T 解析），
// 拿回一段 XML 字符串。文案不进这一层，双语契约留在 i18n（唯一真相），
// 标签结构留在这里。type / kind / for 的属性值是稳定的英文契约 token，
// **永不本地化**，因为 inbound-tags 系统提示词段按名字引用它们。
// Python: openjiuwen/agent_teams/inbound_render.py (417 行)

// ──────────────────────────── 结构体 ────────────────────────────

// InboundOptions RenderInbound 的可选参数。
// Python 侧全用 keyword-only 参数，Go 无此能力，故聚成结构体。
type InboundOptions struct {
	// Sender 发送者的 member_name
	Sender string
	// MessageID 消息 id，让模型能引用 / 标记已读
	MessageID string
	// MsgType 稳定契约 token：InboundTypeDirect 或 InboundTypeBroadcast（不本地化）
	MsgType string
	// TimeInfo 绝对发送时间（由 FormatAbsoluteTime 渲染）。不带相对差：
	// 本块写进历史后会被再次读取。
	TimeInfo string
	// ForController 为真时添加 for="controller"（HITT 头像视为给其控制者的通知）
	ForController bool
	// NoteKind 可选 <team-note> 的 kind（如 "reply-hint" / "hitt-silence"）
	NoteKind string
	// NoteText 可选 <team-note> 正文；NoteKind 与 NoteText 都非空才渲染
	NoteText string
}

// EventOptions RenderEvent 的可选参数。
type EventOptions struct {
	// TaskID 可选任务 id。用指针区分「未设置」与「显式空串」——
	// 上游是 `if task_id is not None`，空串仍会渲染 task_id=""。
	TaskID *string
	// ForController 为真时添加 for="controller"（HITT）
	ForController bool
	// NoteKind 可选 <team-note> kind，嵌套在事件内
	NoteKind string
	// NoteText 可选 <team-note> 正文
	NoteText string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InboundTypeDirect <team-inbound> type 属性的直接消息契约 token
	InboundTypeDirect = "direct"
	// InboundTypeBroadcast <team-inbound> type 属性的广播消息契约 token
	InboundTypeBroadcast = "broadcast"
	// ControllerSender 人类成员自己控制者的 from 取值。
	// 不是成员名：控制者没有名册条目、没有 agent 进程，头像永远无法通过
	// send_message 找到它。
	ControllerSender = "controller"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// snapshotEventKinds 每次出现都是某块状态的**完整快照**的事件 kind 集合，
	// 因此最新一次就足以表达此前所有的信息。
	// 只有具备该性质的事件才能进这里。增量（roster-change）或带主体的
	// 一次性事件（stale-claim，携带 task_id）一旦丢掉较早的一条就会丢信息。
	// 偏差 D-3：上游是 frozenset，Go 用 map 无迭代顺序；当前只有 1 个元素
	// 所以无歧义。若增到 ≥2 个元素，须改为有序结构以保证与上游一致。
	snapshotEventKinds = map[string]struct{}{
		"task-board": {},
	}

	// eventOpenTagFormat 某 kind 的 <team-event> 开标签。RenderEvent 把 kind
	// 放在最前，因此可用它从首字符识别一个已渲染的块。
	eventOpenTagFormat = `<team-event kind="%s"`
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderInbound 把一条成员/用户入站消息渲染为 <team-inbound> XML。
// content 原样渲染在元素内（XML 转义，绝不改写）。
// 有 note 时 <team-note> 作为最后一个子元素嵌套在内部。
// Python: render_inbound (inbound_render.py L115-157)
func RenderInbound(content string, opts InboundOptions) string {
	attrs := []string{
		`from="` + escAttr(opts.Sender) + `"`,
		`message_id="` + escAttr(opts.MessageID) + `"`,
		`type="` + escAttr(opts.MsgType) + `"`,
		`time="` + escAttr(opts.TimeInfo) + `"`,
	}
	if opts.ForController {
		attrs = append(attrs, `for="controller"`)
	}
	return renderBlock("team-inbound", attrs, content, renderNote(opts.NoteKind, opts.NoteText))
}

// RenderControllerInput 把一条控制者指令渲染为 <team-inbound from="controller">。
// 人类成员的 harness 输入带两样东西，一旦变成纯文本就长得一样：它的控制者在
// 指示它，与团队转来别人的消息。没有标记时，头像会把控制者的话当作普通 user
// 轮次，然后像对待队友那样去「回复 user」——而 user 是 leader 那边的另一个真人。
// 标签把来源变成属性，而不是一次猜测。
// 没有 message_id 与 time：控制者指令不是总线消息（无可引用、无需标记已读），
// 且到达即消费。
// Python: render_controller_input (inbound_render.py L160-185)
func RenderControllerInput(content string) string {
	attrs := []string{
		`from="` + ControllerSender + `"`,
		`type="` + InboundTypeDirect + `"`,
	}
	return renderBlock("team-inbound", attrs, content, "")
}

// RenderClock 把当前时间锚点渲染为自闭合的 <team-clock>。
// 写进成员历史的每个 time 属性都是绝对发送时间。相对差（「3 分钟前」）只在
// 渲染时为真、历史被重读时立刻失效，所以成员拿到的是当前时间——每次准入输入
// 一次——并自行对最新的锚点计算经过时长。更早的锚点记录的是「当时的现在」，
// 永不刷新。
// Python: render_clock (inbound_render.py L188-205)
func RenderClock(now string) string {
	return `<team-clock now="` + escAttr(now) + `"/>`
}

// RenderEvent 把一个框架事件渲染为 <team-event> XML 块。
// 用于任务指派、计划审批、催促、完成通知、任务看板等。body 是框架自己的指令
// 文本（由调用方用 schema.T 解析）——没有「原始消息」需要分离，所以整段 body
// 就在一个元素里。
// Python: render_event (inbound_render.py L208-245)
func RenderEvent(kind, body string, opts EventOptions) string {
	attrs := []string{`kind="` + escAttr(kind) + `"`}
	if opts.TaskID != nil {
		attrs = append(attrs, `task_id="`+escAttr(*opts.TaskID)+`"`)
	}
	if opts.ForController {
		attrs = append(attrs, `for="controller"`)
	}
	return renderBlock("team-event", attrs, body, renderNote(opts.NoteKind, opts.NoteText))
}

// RenderTeamContext 把团队既成事实渲染为 <team-context> XML 块。
// 用于成员自己的身份与团队元数据：关于团队的事实而非事件。与它所替代的
// 每轮提示词附件不同，本块在状态首次存在时写进对话一次，之后留在那里——
// 所以它是普通历史，不是会被刷新或撤回的快照。
// Python: render_team_context (inbound_render.py L248-263)
func RenderTeamContext(body string) string {
	return renderBlock("team-context", nil, body, "")
}

// RenderTeamContextWithIdentity 渲染带嵌套 <identity> 子块的 <team-context>。
// RenderTeamContext 的 fork 感知变体：成员自己的身份被包进 <identity> 元素
// （可选 <identity-conversion> 子元素标注 fork 来源），让模型能把**当前**身份块
// 与通过 fork 继承来的更早身份块区分开。仅在 fork 开启时使用——非 fork 团队的
// 纯 RenderTeamContext 保持字节一致。
// identityConversion / infoBody 为 nil 表示不存在（上游用 truthiness 判定，
// 空串等价于 None）。
// Python: render_team_context_with_identity (inbound_render.py L266-300)
func RenderTeamContextWithIdentity(identityBody string, identityConversion, infoBody *string) string {
	identityParts := []string{escText(identityBody)}
	if identityConversion != nil && *identityConversion != "" {
		identityParts = append(identityParts, renderBlock("identity-conversion", nil, *identityConversion, ""))
	}
	identity := "<identity>\n" + strings.Join(identityParts, "\n") + "\n</identity>"
	inner := []string{identity}
	if infoBody != nil && *infoBody != "" {
		inner = append(inner, escText(*infoBody))
	}
	return "<team-context>\n" + strings.Join(inner, "\n") + "\n</team-context>"
}

// RenderTeamPolicy 把团队准则渲染为 <team-policy> XML 块。
// tools 属性命名提供团队协作工具的 MCP server 名，note 说明那对准则里使用的裸工具名
// 意味着什么。工具实际如何被寻址不在这里说明：模型调用它用的名字是 harness
// provider 的事，两者仅在 server 名上汇合。
// body 是团队自己的提示词文本而非某个发送者写的，所以与本文件其他块不同，
// 它**不转义**：它是 Markdown，转义会破坏回引号、& 与比较运算符。
// note 同样原样插入。
// Python: render_team_policy (inbound_render.py L303-327)
func RenderTeamPolicy(body, tools, note string) string {
	declaration := `<team-note kind="tool-namespace">` + "\n" + note + "\n</team-note>"
	return `<team-policy tools="` + escAttr(tools) + `">` + "\n" + declaration + "\n\n" + body + "\n</team-policy>"
}

// RenderTeamPolicyInline 渲染进程内 leader 的团队准则（F_122）。
// RenderTeamPolicy 的历史内形态：同一元素，但没有 tools 属性、没有工具命名空间
// note，因为进程内成员用裸名调用团队工具。当团队由框架建好、没有 build_team
// 结果承载准则时，经 TeamContextTracker 在 leader 首条输入之前投递一次。
// 与外部形态一样，body 是团队自己的 Markdown、**不转义**。
// Python: render_team_policy_inline (inbound_render.py L330-347)
func RenderTeamPolicyInline(body string) string {
	return "<team-policy>\n" + body + "\n</team-policy>"
}

// DropSupersededSnapshots 返回去掉「被更晚快照取代」的输入后的 parts。
// 成员忙碌时回来后面对期间积压的一切。任务看板可能在里面出现好几次，每次都是
// 相隔数秒的完整survey——但只有最后一次描述成员即将据以行动的看板，更早的都是
// 噪音，离变成永久历史只差一步。
// 条目整条丢弃，绝不编辑：每条是一个输入，且只丢 snapshotEventKinds 里的 kind。
// 其余一切（包括名册增量、单任务催促）都携带别的条目不会重复的信息。
// Python: drop_superseded_snapshots (inbound_render.py L372-399)
func DropSupersededSnapshots(parts []string) []string {
	newest := make(map[string]int, len(parts))
	for i, part := range parts {
		if kind := snapshotKindOf(part); kind != "" {
			newest[kind] = i
		}
	}
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		// 非快照条目 kind 为空，map 查不到 → 取自身下标 → 自然保留，无需单独分支。
		keep, ok := newest[snapshotKindOf(part)]
		if !ok || keep == i {
			out = append(out, part)
		}
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// escText 转义元素正文：只转 & < >，不转引号。
// 与 Python html.escape(text, quote=False) 逐字节一致。
// Python: _esc_text (inbound_render.py L78-80)
func escText(text string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(text)
}

// escAttr 转义属性值（转义引号）。
// Python: _esc_attr (inbound_render.py L83-85) —— html.escape(quote=True)，
// 单引号输出 &#x27;、双引号输出 &quot;。
// 本实现用 html.EscapeString：单引号输出 &#39;、双引号输出 &#34;。
// XML 解析后等值，属已知偏差 D-1（spec §7）。
func escAttr(value string) string {
	return html.EscapeString(value)
}

// renderNote 渲染嵌套的 <team-note> 子元素，缺失时返回空串。
// 自带尾换行，让外层块不需要对「有没有 note」做分支。
// Python: _render_note (inbound_render.py L88-96)
func renderNote(noteKind, noteText string) string {
	if noteKind == "" || noteText == "" {
		return ""
	}
	return `<team-note kind="` + escAttr(noteKind) + `">` + "\n" + escText(noteText) + "\n</team-note>\n"
}

// renderBlock 把一个块元素渲染在其 body 与可选嵌套 note 之外。
// tag 元素名；attrs 已转义的 name="value" 片段（按契约顺序）；body 元素正文
// （在此转义）；note 已渲染的 <team-note> 子元素或空串。
// Python: _render_block (inbound_render.py L99-112)
func renderBlock(tag string, attrs []string, body, note string) string {
	openTag := tag
	if len(attrs) > 0 {
		openTag = tag + " " + strings.Join(attrs, " ")
	}
	return "<" + openTag + ">\n" + escText(body) + "\n" + note + "</" + tag + ">"
}

// snapshotKindOf 返回 text 所属的快照 kind，不是则返回空串。
// 只有「整条输入就是一个快照事件」才符合条件：这些事件每次 deliver_input
// 投递一个，所以整条条目就是这个块，丢掉它不带走走任何别的东西。
// <team-note> 不算「别的东西」——它嵌套在块内部、只修饰那个块，因此随它所挂的
// 快照一起过期、一起被取代。
// Python: snapshot_kind_of (inbound_render.py L350-369)
func snapshotKindOf(text string) string {
	stripped := strings.TrimSpace(text)
	if !strings.HasSuffix(stripped, "</team-event>") {
		return ""
	}
	for kind := range snapshotEventKinds {
		if strings.HasPrefix(stripped, fmt.Sprintf(eventOpenTagFormat, kind)) {
			return kind
		}
	}
	return ""
}
