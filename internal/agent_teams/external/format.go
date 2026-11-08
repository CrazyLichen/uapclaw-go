package external

import (
	"fmt"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// 本文件是进程外成员（外部 CLI 驱动）的入站消息渲染。
// 与进程内 MessageHandler 的渲染语义一致：原始消息原文进 <team-inbound>
// （sender / message_id / type / 绝对发送时间作为属性），框架提示进独立的
// <team-note>。human-agent 头像得到 for="controller" 加 hitt-silence note；
// 其余成员得到 reply-hint note。
// Python: openjiuwen/agent_teams/external/format.py (213 行) 的 render_message /
// render_messages。render_task_line / render_task_board 待 25.4 回填。

// ──────────────────────────── 结构体 ────────────────────────────

// MessageLike 渲染用的团队消息行结构视图。
// 用接口而非具体类型，对齐上游的 Protocol 结构化视图：调用方传数据库行或
// 测试假实现都可。
// Python: _MessageLike (format.py L31-38)
type MessageLike interface {
	// MessageID 消息 id
	MessageID() string
	// FromMemberName 发送者成员名
	FromMemberName() string
	// Content 消息正文
	Content() string
	// Broadcast 是否广播
	Broadcast() bool
	// TimestampMs 毫秒 UTC epoch 发送时间
	TimestampMs() int64
}

// RenderMessageOptions RenderMessage 的可选参数。
type RenderMessageOptions struct {
	// IsHumanAgent 接收方是否为 human-agent 头像
	IsHumanAgent bool
	// Body 框架模板消息的投递时正文，由调用方渲染。
	// 传值同时会**去掉** reply-hint：框架指令用工具调用答复，而不是回复。
	// nil 表示普通消息——渲染它自己的 content 并保留提示。
	Body *string
	// Location 渲染发送时间用的时区；nil 表示运行环境的本地时区。
	// 生产不走这个字段（保持上游「取运行时本地时区」的语义），
	// 只给测试固定时区用——否则 golden 会随跑测试的机器时区漂移。
	Location *time.Location
}

// RenderMessagesOptions RenderMessages 的可选参数。
type RenderMessagesOptions struct {
	// IsHumanAgent 接收方是否为 human-agent 头像
	IsHumanAgent bool
	// Bodies 批次中框架模板消息的投递时正文，按 message_id 索引。
	// 普通消息不在映射里，渲染自己的 content。
	Bodies map[string]string
	// Location 渲染发送时间用的时区，语义同 RenderMessageOptions.Location。
	Location *time.Location
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderMessage 把一条入站消息渲染为 <team-inbound> XML。
// 对应进程内 MessageHandler 的渲染：消息正文原文进 <team-inbound>，
// 框架提示进独立的 <team-note>。
// Python: render_message (format.py L52-108)
func RenderMessage(message MessageLike, opts RenderMessageOptions) (string, error) {
	msgType := agentteams.InboundTypeDirect
	if message.Broadcast() {
		msgType = agentteams.InboundTypeBroadcast
	}
	timeInfo := formatAbsoluteTimeIn(opts.Location, message.TimestampMs())

	if opts.IsHumanAgent {
		silence, err := atschema.T("hitt.silence_note")
		if err != nil {
			return "", fmt.Errorf("解析 hitt.silence_note 失败: %w", err)
		}
		content := message.Content()
		if opts.Body != nil {
			content = *opts.Body
		}
		return agentteams.RenderInbound(content, agentteams.InboundOptions{
			Sender:        message.FromMemberName(),
			MessageID:     message.MessageID(),
			MsgType:       msgType,
			TimeInfo:      timeInfo,
			ForController: true,
			NoteKind:      "hitt-silence",
			NoteText:      silence,
		}), nil
	}

	if opts.Body != nil {
		return agentteams.RenderInbound(*opts.Body, agentteams.InboundOptions{
			Sender:    message.FromMemberName(),
			MessageID: message.MessageID(),
			MsgType:   msgType,
			TimeInfo:  timeInfo,
		}), nil
	}

	hint, err := atschema.ReplyHintFor(message.FromMemberName())
	if err != nil {
		return "", fmt.Errorf("解析 reply_hint 失败: %w", err)
	}
	return agentteams.RenderInbound(message.Content(), agentteams.InboundOptions{
		Sender:    message.FromMemberName(),
		MessageID: message.MessageID(),
		MsgType:   msgType,
		TimeInfo:  timeInfo,
		NoteKind:  "reply-hint",
		NoteText:  hint,
	}), nil
}

// RenderMessages 按给定顺序（最旧在前）渲染一批入站消息。
// bodies 是批次中框架模板消息的投递时正文，按 message_id 索引。
// 普通消息不在映射里，渲染自己的 content。
// Python: render_messages (format.py L111-128)
func RenderMessages(messages []MessageLike, opts RenderMessagesOptions) (string, error) {
	rendered := make([]string, 0, len(messages))
	for _, m := range messages {
		one := RenderMessageOptions{IsHumanAgent: opts.IsHumanAgent, Location: opts.Location}
		if opts.Bodies != nil {
			if body, ok := opts.Bodies[m.MessageID()]; ok {
				b := body
				one.Body = &b
			}
		}
		text, err := RenderMessage(m, one)
		if err != nil {
			return "", err
		}
		rendered = append(rendered, text)
	}
	return stringsJoin(rendered, "\n\n"), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatAbsoluteTimeIn 按指定时区渲染绝对发送时间；location 为 nil 时用运行环境本地时区。
//
// 时区可注入只为测试确定性：golden 由上游按 Asia/Shanghai 生成，
// 若直接取 time.Local，在 UTC 机器（CI runner）上会渲染成 +00:00 而比对失败。
// 生产调用不传 location，行为与上游 astimezone() 一致。
//
// Python: _format_absolute (timefmt.py)
func formatAbsoluteTimeIn(location *time.Location, timestampMs int64) string {
	if location == nil {
		return agentteams.FormatAbsoluteTime(timestampMs)
	}
	return agentteams.FormatAbsoluteTimeIn(location, timestampMs)
}

// stringsJoin 避免为一次 Join 引入 strings 导入面（本包其余部分不需要 strings）。
func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
