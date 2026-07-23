package interaction

import (
	"context"
	"fmt"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserInbox 用户侧收件箱，将外部输入路由到团队运行时。
// 对齐 Python: UserInbox (openjiuwen/agent_teams/interaction/user_inbox.py)
//
// 三种入口：
//   - DeliverToLeader — 纯文本直达 Leader DeepAgent（保留历史 invoke 语义）
//   - Direct — @member_name body 点对点消息
//   - Broadcast — 团队范围广播
//
// 所有路径通过 TeamMessageManager，消息最终存储在队友间流量的同一数据源。
type UserInbox struct {
	// messageManager 消息管理器
	// ⤵️ 待 9.55 回填: TeamMessageManager — 提供 SendMessage/BroadcastMessage 方法
	messageManager any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// inboxLogComponent 日志组件
	inboxLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserInbox 创建用户收件箱。
// 对齐 Python: UserInbox.__init__(message_manager)
func NewUserInbox(messageManager any) *UserInbox {
	return &UserInbox{messageManager: messageManager}
}

// Direct 发送 @target body 点对点消息。
// 对齐 Python: UserInbox.direct(target, body)
//
// Python 执行步骤：
//  1. msg_id = await self._mm.send_message(content=body, to_member_name=target, from_member_name=USER_PSEUDO_MEMBER_NAME)
//  2. if msg_id is None: return DeliverResult.failure(f"send_failed:{target}")
//  3. return DeliverResult.success(msg_id)
//
// ⤵️ 待 9.55 回填: 调用 messageManager.SendMessage(content=body, to=target, from="user")
func (u *UserInbox) Direct(target string, body string) (*DeliverResult, error) {
	logger.Debug(inboxLogComponent).Str("target", target).
		Str("from", agentteams.UserPseudoMemberName).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("UserInbox.Direct (stub)")
	// 对齐 Python 步骤 1-3（当前 stub 实现）
	// ⤵️ 待 9.55 回填: msgID := u.messageManager.SendMessage(body, target, agentteams.UserPseudoMemberName)
	msgID := "stub-direct-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// Broadcast 广播用户侧公告。
// 对齐 Python: UserInbox.broadcast(body)
//
// Python 执行步骤：
//  1. msg_id = await self._mm.broadcast_message(content=body, from_member_name=USER_PSEUDO_MEMBER_NAME)
//  2. if msg_id is None: return DeliverResult.failure("broadcast_failed")
//  3. return DeliverResult.success(msg_id)
//
// ⤵️ 待 9.55 回填: 调用 messageManager.BroadcastMessage(content=body, from="user")
func (u *UserInbox) Broadcast(body string) (*DeliverResult, error) {
	logger.Debug(inboxLogComponent).Str("from", agentteams.UserPseudoMemberName).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("UserInbox.Broadcast (stub)")
	// 对齐 Python 步骤 1-3（当前 stub 实现）
	// ⤵️ 待 9.55 回填: msgID := u.messageManager.BroadcastMessage(body, agentteams.UserPseudoMemberName)
	msgID := "stub-broadcast-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// DeliverToLeader 将输入投递到 Leader DeepAgent。
// 对齐 Python: UserInbox.deliver_to_leader(deliver_input, body) (staticmethod)
//
// Python 执行步骤：
//  1. team_logger.debug("UserInbox: delivering input to leader DeepAgent")
//  2. try: await deliver_input(body)
//  3. except Exception as e: return DeliverResult.failure(f"deliver_to_leader_failed:{e}")
//  4. return DeliverResult.success(message_id=None)
//
// 此通道不产生 bus message ID，成功时 MessageID 为 nil。
func DeliverToLeader(deliverInput func(ctx context.Context, content string) error, body string) *DeliverResult {
	// 对齐 Python 步骤 1
	logger.Debug(inboxLogComponent).Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("DeliverToLeader")

	if deliverInput == nil {
		reason := "deliver_to_leader_failed:no_deliver_fn"
		return NewDeliverResultFailure(reason)
	}

	// 对齐 Python 步骤 2-4
	ctx := context.Background()
	if err := deliverInput(ctx, body); err != nil {
		// 对齐 Python 步骤 3: return DeliverResult.failure(f"deliver_to_leader_failed:{e}")
		reason := "deliver_to_leader_failed:" + err.Error()
		return NewDeliverResultFailure(reason)
	}
	// 对齐 Python 步骤 4: return DeliverResult.success(message_id=None)
	return NewDeliverResultSuccess(nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
