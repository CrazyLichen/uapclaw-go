package message_handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestHandleChannelControl_NewSession 测试 /new_session slash 命令
func TestHandleChannelControl_NewSession(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/new_session"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /new_session 应被处理")
}

// TestHandleChannelControl_ModeOK 测试 /mode slash 命令
func TestHandleChannelControl_ModeOK(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/mode code.normal"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /mode code.normal 应被处理")
}

// TestHandleChannelControl_SwitchOK 测试 /switch slash 命令
func TestHandleChannelControl_SwitchOK(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/switch fast"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /switch fast 应被处理")
}

// TestHandleChannelControl_SkillsList 测试 /skills list slash 命令
func TestHandleChannelControl_SkillsList(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /skills list 应被处理")
}

// TestHandleChannelControl_BranchOK 测试 /branch slash 命令
func TestHandleChannelControl_BranchOK(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/branch feature-x"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /branch 应被处理")
}

// TestHandleChannelControl_RewindOK 测试 /rewind slash 命令
func TestHandleChannelControl_RewindOK(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/rewind 5"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "飞书渠道 + /rewind 5 应被处理")
}

// TestHandleChannelControl_ModeBad 测试非法 /mode
func TestHandleChannelControl_ModeBad(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/mode unknown"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "非法 /mode 也应被处理（返回 BAD 通知）")
}

// TestHandleChannelControl_Skills不创建渠道状态 A7：Python 在 SKILLS_OK 处提前 return，
// 不调用 _get_or_create_channel_state；Go 必须在建 state 之前短路。
func TestHandleChannelControl_Skills不创建渠道状态(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "/skills list 应被处理")

	mh.statesMu.RLock()
	count := len(mh.channelStates)
	mh.statesMu.RUnlock()
	assert.Equal(t, 0, count, "skills_ok 路径不得创建 channel state")
}

// TestHandleChannelControl_RewindBad 测试非法 /rewind
func TestHandleChannelControl_RewindBad(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/rewind"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "非法 /rewind 也应被处理")
}

// TestHandleChannelControl_非请求消息 测试非请求消息
func TestHandleChannelControl_非请求消息(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatDelta}
	handled := mh.handleChannelControl(context.Background(), msg)
	assert.False(t, handled)
}

// TestHandleChannelControl_非控制文本 测试非控制文本
func TestHandleChannelControl_非控制文本(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"普通消息"}`))
	handled := mh.handleChannelControl(context.Background(), msg)
	assert.False(t, handled)
}

// TestNewSessionCancelAndNotice 测试 /new_session 处理
func TestNewSessionCancelAndNotice(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/new_session"}`), schema.WithProvider("feishu"))

	// 预建 state（模拟首建），获取 oldSID 后重构
	state := mh.GetOrCreateChannelState(msg)
	oldSID := state.SessionID

	mh.newSessionCancelAndNotice(context.Background(), testUserInfos(msg), msg, oldSID, "new-sid-x")

	// 验证通知已发出（收尾函数不再改 state）。
	// CI -race 修复：原先排空 goroutine 与主 select 竞争消费同一消息，
	// 排空侧先就绪时通知被其消费、主 select 落 default 误报失败；
	// channel 容量 256 足够容纳单条通知，改阻塞接收+超时判定。
	select {
	case <-mh.robotMessages:
	case <-time.After(2 * time.Second):
		t.Fatal("收尾函数应发送变更通知")
	}
}

// TestHandleChannelControl_newSession分配失败回滚
// 0.2.5 起 /new_session 需经 session.create 分配真实会话；分配失败必须回滚
// session_id 并下发「创建新会话失败」，不进入转发。
func TestHandleChannelControl_newSession分配失败回滚(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.sessionMap = newTestSessionMap(t)

	notices := make(chan *schema.Message, 8)
	go func() {
		for msg := range mh.robotMessages {
			notices <- msg
		}
	}()

	msg := schema.NewReqMessage("feishu_enterprise:app-1", "sess-old",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"/new_session"}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	state := mh.GetOrCreateChannelState(msg)
	state.SessionID = "sess-old"

	handled := mh.handleChannelControl(context.Background(), msg)
	assert.True(t, handled, "控制指令应被消费")

	select {
	case notice := <-notices:
		assert.Contains(t, notice.Payload["error"], "创建新会话失败")
	case <-time.After(2 * time.Second):
		t.Fatal("分配失败应下发错误通知")
	}
	// 事务回滚：失败时保留旧 session_id
	assert.Equal(t, "sess-old", state.SessionID, "分配失败应回滚 session_id")
}

// TestModeChangeCancelAndNotice 测试 /mode 处理
func TestModeChangeCancelAndNotice(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/mode code.normal"}`), schema.WithProvider("feishu"))

	mh.handleModeSwitch(context.Background(), "agent.code.normal", modeSwitchParams{
		State:          mh.GetOrCreateChannelState(msg),
		UserInfos:      testUserInfos(msg),
		ChannelID:      msg.ChannelID,
		ReplySessionID: msg.SessionID,
		Msg:            msg,
	})

	state := mh.GetOrCreateChannelState(msg)
	assert.Equal(t, ChannelModeAgentCodeNormal, state.Mode)
}

// TestModeChange_一级映射 验证 /mode 各入参归一结果（对齐 channel_mode_from_str）。
// 0.2.5 起 agent 族 plan/fast 合并：/mode agent 落 agent.work.normal。
func TestModeChange_一级映射(t *testing.T) {
	tests := []struct {
		subcommand string
		wantMode   ChannelMode
	}{
		{subcommand: "agent", wantMode: ChannelModeAgentWorkNormal},
		{subcommand: "code", wantMode: ChannelModeAgentCodeNormal},
		{subcommand: "team", wantMode: ChannelModeTeamWorkNormal},
		{subcommand: "agent.work.plan", wantMode: ChannelModeAgentWorkPlan},
		{subcommand: "agent.work.normal", wantMode: ChannelModeAgentWorkNormal},
		{subcommand: "agent.code.plan", wantMode: ChannelModeAgentCodePlan},
		{subcommand: "agent.code.normal", wantMode: ChannelModeAgentCodeNormal},
		{subcommand: "team.code.normal", wantMode: ChannelModeTeamCodeNormal},
		// 白名单扩展后新增可用的入参
		{subcommand: "team.work.normal", wantMode: ChannelModeTeamWorkNormal},
		{subcommand: "team.design", wantMode: ChannelModeAgent},
		{subcommand: "agent.plan", wantMode: ChannelModeAgentWorkPlan},
	}
	for _, tt := range tests {
		t.Run(tt.subcommand, func(t *testing.T) {
			mh := createTestMessageHandlerWithTransport()
			go func() {
				for range mh.robotMessages {
				}
			}()
			msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
				json.RawMessage(`{}`), schema.WithProvider("feishu"))
			state := mh.GetOrCreateChannelState(msg)
			mh.handleModeSwitch(context.Background(), tt.subcommand, modeSwitchParams{
				State:          state,
				UserInfos:      testUserInfos(msg),
				ChannelID:      msg.ChannelID,
				ReplySessionID: msg.SessionID,
				Msg:            msg,
			})
			assert.Equal(t, tt.wantMode, state.Mode, "/mode %s 应映射到 %v", tt.subcommand, tt.wantMode)
		})
	}
}

// TestModeSwitch_非法指令 验证白名单外入参不下发状态变更并回「非法指令」。
func TestModeSwitch_非法指令(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	notices := make(chan *schema.Message, 4)
	go func() {
		for msg := range mh.robotMessages {
			notices <- msg
		}
	}()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	state := mh.GetOrCreateChannelState(msg)
	before := state.Mode

	mh.handleModeSwitch(context.Background(), "plan", modeSwitchParams{
		State:          state,
		UserInfos:      testUserInfos(msg),
		ChannelID:      msg.ChannelID,
		ReplySessionID: msg.SessionID,
		Msg:            msg,
	})

	assert.Equal(t, before, state.Mode, "非法入参不应改状态")
	select {
	case notice := <-notices:
		assert.Equal(t, "非法指令", notice.Payload["content"])
	case <-time.After(2 * time.Second):
		t.Fatal("应下发非法指令通知")
	}
}

// TestPayloadToInt S-06 回归测试。
// 跨进程 JSON 数值必为 float64，原 .(int) 断言永不命中导致字段恒为 0。
func TestPayloadToInt(t *testing.T) {
	assert.Equal(t, 5, payloadToInt(float64(5)), "float64(5) 应转 5")
	assert.Equal(t, 3, payloadToInt(3), "int 应直接命中")
	assert.Equal(t, 7, payloadToInt(int64(7)), "int64 应转换")
	assert.Equal(t, 9, payloadToInt(json.Number("9")), "json.Number 应转换")
	assert.Equal(t, 0, payloadToInt("5"), "字符串应返回 0")
	assert.Equal(t, 0, payloadToInt(nil), "nil 应返回 0")
}

// TestTruncRunes M-07 回归测试。CJK 按字符截断不产生非法 UTF-8。
func TestTruncRunes(t *testing.T) {
	// ASCII 纯英文字符
	assert.Equal(t, "abcde", truncRunes("abcdefgh", 5))
	// CJK：3 字节/字符，按 rune 切
	assert.Equal(t, "测试数", truncRunes("测试数据", 3))
	// 不超长原样返回
	assert.Equal(t, "测试", truncRunes("测试", 10))
	// 空串/零长度
	assert.Equal(t, "", truncRunes("abc", 0))
	assert.Equal(t, "", truncRunes("", 5))
}

// TestRewindSlashConfirmPrompt_四行模板 M-03 回归测试。
// Python L1046-1050 四行模板：不可逆警告/删除范围/confirm 格式/bash 文件影响。
func TestRewindSlashConfirmPrompt_四行模板(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	mh.rewindSlashConfirmPrompt(testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindOK,
		RewindTurn: 3,
	})

	select {
	case notice := <-mh.robotMessages:
		content, _ := notice.Payload["content"].(string)
		assert.Contains(t, content, "[收到 /rewind 3 指令] 确认要回退到第 3 轮吗？")
		assert.Contains(t, content, "此操作不可逆，将删除第 3 轮及之后的所有对话。")
		assert.Contains(t, content, "请回复 /rewind confirm 3 确认，或 /rewind cancel 取消。")
		assert.Contains(t, content, "注意：回退不影响手动编辑的文件或通过 bash 执行的命令。")
	case <-time.After(2 * time.Second):
		t.Fatal("应发送确认提示通知")
	}
}

// TestSendChannelNotice 测试发送渠道通知
func TestSendChannelNotice(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	mh.sendChannelNotice(testUserInfos(msg), msg.ChannelID, msg.SessionID, "测试通知")

	select {
	case notice := <-mh.robotMessages:
		assert.Equal(t, schema.MessageTypeEvent, notice.Type)
	default:
		t.Fatal("未收到通知")
	}
}

// TestSendChannelNotice_Params为空对象 A8：Python Message(params={})，
// Go 侧必须发空对象而非 null，否则下游按 params 是否为 nil 分支的 Channel 行为不同。
func TestSendChannelNotice_Params为空对象(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	mh.sendChannelNotice(testUserInfos(msg), msg.ChannelID, msg.SessionID, "测试通知")

	select {
	case notice := <-mh.robotMessages:
		assert.JSONEq(t, `{}`, string(notice.Params), "params 应为空对象 {}")
		assert.True(t, notice.EnableStreaming, "enable_streaming 默认 True")
		assert.Equal(t, schema.EventTypeChatFinal, notice.EventType)
	case <-time.After(2 * time.Second):
		t.Fatal("未收到通知")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestSwitchMode_模式家族映射 验证 /switch 按当前模式家族决定目标模式。
// 0.2.5：agent 族 plan/fast 已合并，两者都保持 agent.work.normal。
func TestSwitchMode_模式家族映射(t *testing.T) {
	tests := []struct {
		name        string
		currentMode ChannelMode
		switchCmd   string
		wantMode    ChannelMode
		wantInvalid bool
	}{
		// agent 族：plan / fast 都保持 normal（plan 只能走显式入口）
		{name: "agent_plan_切换plan", currentMode: ChannelModeAgentWorkPlan, switchCmd: "plan", wantMode: ChannelModeAgentWorkNormal},
		{name: "agent_normal_切换plan", currentMode: ChannelModeAgentWorkNormal, switchCmd: "plan", wantMode: ChannelModeAgentWorkNormal},
		{name: "agent_plan_切换fast", currentMode: ChannelModeAgentWorkPlan, switchCmd: "fast", wantMode: ChannelModeAgentWorkNormal},
		{name: "agent_normal_切换fast", currentMode: ChannelModeAgentWorkNormal, switchCmd: "fast", wantMode: ChannelModeAgentWorkNormal},
		// code 族 → plan
		{name: "code_plan_切换plan", currentMode: ChannelModeAgentCodePlan, switchCmd: "plan", wantMode: ChannelModeAgentCodePlan},
		{name: "code_normal_切换plan", currentMode: ChannelModeAgentCodeNormal, switchCmd: "plan", wantMode: ChannelModeAgentCodePlan},
		{name: "code_team_切换plan", currentMode: ChannelModeTeamCodeNormal, switchCmd: "plan", wantMode: ChannelModeAgentCodePlan},
		// code 族 /switch fast → 非法
		{name: "code_normal_切换fast_非法", currentMode: ChannelModeAgentCodeNormal, switchCmd: "fast", wantInvalid: true},
		// code 族 → normal
		{name: "code_plan_切换normal", currentMode: ChannelModeAgentCodePlan, switchCmd: "normal", wantMode: ChannelModeAgentCodeNormal},
		{name: "code_team_切换normal", currentMode: ChannelModeTeamCodeNormal, switchCmd: "normal", wantMode: ChannelModeAgentCodeNormal},
		// agent 族 /switch normal → 非法
		{name: "agent_normal_切换normal_非法", currentMode: ChannelModeAgentWorkNormal, switchCmd: "normal", wantInvalid: true},
		// code 族 → team
		{name: "code_plan_切换team", currentMode: ChannelModeAgentCodePlan, switchCmd: "team", wantMode: ChannelModeTeamCodeNormal},
		{name: "code_normal_切换team", currentMode: ChannelModeAgentCodeNormal, switchCmd: "team", wantMode: ChannelModeTeamCodeNormal},
		// agent 族 /switch team → 非法
		{name: "agent_normal_切换team_非法", currentMode: ChannelModeAgentWorkNormal, switchCmd: "team", wantInvalid: true},
		// 其他族 → 非法
		{name: "team_切换fast_非法", currentMode: ChannelModeTeamWorkNormal, switchCmd: "fast", wantInvalid: true},
		{name: "team_切换normal_非法", currentMode: ChannelModeTeamWorkNormal, switchCmd: "normal", wantInvalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mh := createTestMessageHandlerWithTransport()
			go func() {
				for range mh.robotMessages {
				}
			}()

			msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
				json.RawMessage(`{"content":"/switch `+tt.switchCmd+`"}`), schema.WithProvider("feishu"))

			state := mh.GetOrCreateChannelState(msg)
			state.Mode = tt.currentMode

			handled := mh.handleChannelControl(context.Background(), msg)
			assert.True(t, handled, "控制指令应被消费")

			if tt.wantInvalid {
				assert.Equal(t, tt.currentMode, state.Mode, "非法切换时模式不应变更")
			} else {
				assert.Equal(t, tt.wantMode, state.Mode, "模式应变更为目标模式")
			}
		})
	}
}

// TestSkillsSlashNotice_ReqID随机后缀 T-08 回归：req_id 含 token_hex(3)
// 随机后缀（Python f"skills_slash_{time}_{token_hex(3)}"），两次调用不同。
func TestSkillsSlashNotice_ReqID随机后缀(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))

	// AgentClient 未连接 → skillsSlashNotice 走"获取技能列表失败"分支
	// （不 panic 即覆盖 T-08 的 req_id 格式变更编译路径）
	mh.skillsSlashNotice(context.Background(), testUserInfos(msg), msg)
	// 二次调用确保随机后缀路径稳定
	mh.skillsSlashNotice(context.Background(), testUserInfos(msg), msg)
}

// skillsReplies 构造 /skills list 的假响应。
func skillsReplies(payload map[string]any, ok bool) map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodSkillsList): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test",
				schema.WithResponseOK(ok), schema.WithPayload(payload))
		},
	}
}

// TestSkillsSlashNotice_五种文案形态 A3/A4：锁定与 Python 一致的四种失败/成功形态，
// 防止后续被"统一文案"优化掉。
func TestSkillsSlashNotice_五种文案形态(t *testing.T) {
	newMsg := func() *schema.Message {
		return schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
			json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))
	}

	t.Run("未连接_全角冒号", func(t *testing.T) {
		mh := createTestMessageHandlerWithTransport()
		mh.skillsSlashNotice(context.Background(), testUserInfos(newMsg()), newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败：AgentClient 未连接", notice.Payload["error"])
	})

	t.Run("成功_payload透传", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(map[string]any{
			"skills": []any{map[string]any{"name": "s1"}},
		}, true))
		mh.skillsSlashNotice(context.Background(), testUserInfos(newMsg()), newMsg())
		notice := waitNotice(t, mh)
		assert.Nil(t, notice.Payload["error"])
		assert.NotNil(t, notice.Payload["skills"], "成功响应应逐键透传 payload")
	})

	// 空 payload 经 E2A 线格式往返后是 "{}"（不是 nil），Python isinstance(dict)
	// 命中，notice_payload = {} —— 同样不带 data 键。Go 的 {"data": nil} 分支
	// 只在 wire 完全没有 payload 字段时才可能命中，实际链路不可达，保持现状不改。
	t.Run("成功_payload为空对象_无data键", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(nil, true))
		mh.skillsSlashNotice(context.Background(), testUserInfos(newMsg()), newMsg())
		notice := waitNotice(t, mh)
		_, exists := notice.Payload["data"]
		assert.False(t, exists, "空 payload 不应产生 data 键（Python 透传空 dict）")
		assert.Nil(t, notice.Payload["error"])
	})

	t.Run("失败_带error_ASCII冒号", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(map[string]any{"error": "boom"}, false))
		mh.skillsSlashNotice(context.Background(), testUserInfos(newMsg()), newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败: boom", notice.Payload["error"])
	})

	t.Run("失败_无error", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(nil, false))
		mh.skillsSlashNotice(context.Background(), testUserInfos(newMsg()), newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败", notice.Payload["error"])
	})
}

// branchReplies 构造 /branch 需要的两个假响应。
func branchReplies() map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodChatCancel): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
		},
		string(schema.ReqMethodSessionFork): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{
				"session_id": "fork-1",
				"title":      "分支标题",
			}))
		},
	}
}

// seedChannelSession 预置某个消息对应的渠道状态 session_id。
func seedChannelSession(mh *MessageHandler, msg *schema.Message, sid string) {
	mh.statesMu.Lock()
	defer mh.statesMu.Unlock()
	mh.channelStates[getChannelStateKey(msg.ChannelID, msg.SessionID)] = &ChannelControlState{SessionID: sid}
}

// TestBranchSlashNotice_取消先于通知 A1：Python await 取消后再发通知。
// 给 chat.cancel 加 100ms 延迟：同步实现下返回时必已收到 cancel；异步实现下为 0。
func TestBranchSlashNotice_取消先于通知(t *testing.T) {
	f, mh := newFakeAgentServer(t, branchReplies())
	f.setDelay(string(schema.ReqMethodChatCancel), 100*time.Millisecond)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "my-branch",
	})

	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodChatCancel)),
		"取消必须同步完成（异步实现下此处为 0）")
	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodSessionFork)))

	notice := waitNoticeContaining(t, mh, "已分叉会话")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, "分支标题")
}

// TestBranchSlashNotice_成功回填字段 验证 payload 缺字段时的回落。
func TestBranchSlashNotice_成功回填字段(t *testing.T) {
	replies := branchReplies()
	replies[string(schema.ReqMethodSessionFork)] = func(rid string, _ map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
	}
	_, mh := newFakeAgentServer(t, replies)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "named",
	})

	notice := waitNoticeContaining(t, mh, "已分叉会话")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, "named")
	state := mh.GetOrCreateChannelState(msg)
	assert.NotEmpty(t, state.SessionID, "回落后应写入生成的 newSID")
}

// rewindReplies 构造 /rewind 的假响应。
func rewindReplies(ok bool) map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodChatCancel): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
		},
		string(schema.ReqMethodSessionRewind): func(rid string, _ map[string]any) *schema.AgentResponse {
			if !ok {
				return schema.NewAgentResponse(rid, "feishu_test",
					schema.WithResponseOK(false),
					schema.WithPayload(map[string]any{"error": "boom"}))
			}
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{
				"content_preview":   "hello world",
				"remaining_records": 3,
				"removed_records":   2,
				"rewind_context":    true,
			}))
		},
	}
}

// TestRewindSlashNotice_E2A成功 A5/A6：preview 只取 content_preview（无兜底），
// 且读 rewind_context。
func TestRewindSlashNotice_E2A成功(t *testing.T) {
	_, mh := newFakeAgentServer(t, rewindReplies(true))

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.rewindSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 2,
	})

	notice := waitNoticeContaining(t, mh, "已回退到第 2 轮")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, `"hello world"`)
	assert.Contains(t, content, "删除 2 条记录，剩余 3 条。")
	assert.NotContains(t, content, "上下文未同步截断", "E2A 成功路径不应带兜底提示")
}

// 注：「当前无活跃会话，无法回退」分支不可达——Python
// _get_channel_default_state (L247-270) 与 Go getChannelDefaultState 在 config
// 未指定 default_session_id 时都会生成 sid，state.session_id 永不为空。
// 该分支属 Python 同源的防御性代码，故不为其编写用例。

// TestRewindSlashNotice_兜底失败 覆盖 E2A 拒绝后本地回退失败的路径。
func TestRewindSlashNotice_兜底失败(t *testing.T) {
	_, mh := newFakeAgentServer(t, rewindReplies(false))

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sid-without-history-44a")

	mh.rewindSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 1,
	})

	notice := waitNoticeContaining(t, mh, "回退失败")
	assert.Contains(t, notice.Payload["error"], "回退失败")
}

// TestBranchSlashNotice_未连接 覆盖 AgentClient 未连接的早退分支。
func TestBranchSlashNotice_未连接(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "",
	})
	notice := waitNotice(t, mh)
	assert.Equal(t, "分叉失败：AgentClient 未连接", notice.Payload["error"])
}

// TestHandleChannelControl_前置守卫 覆盖类型/方法/渠道/空文本四类早退。
func TestHandleChannelControl_前置守卫(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	text := json.RawMessage(`{"content":"/new_session"}`)

	// 非 req 类型
	eventMsg := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatSend, text, schema.WithProvider("feishu"))
	eventMsg.Type = schema.MessageTypeEvent
	assert.False(t, mh.handleChannelControl(context.Background(), eventMsg), "非 req 类型应早退")

	// 非 chat.send 方法
	answerMsg := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatAnswer, text, schema.WithProvider("feishu"))
	assert.False(t, mh.handleChannelControl(context.Background(), answerMsg), "非 chat.send 应早退")

	// 非受控渠道（web）
	webMsg := schema.NewReqMessage("web-1", "s", schema.ReqMethodChatSend, text, schema.WithProvider("web"))
	assert.False(t, mh.handleChannelControl(context.Background(), webMsg), "web 渠道不解析 slash")

	// 空文本
	emptyMsg := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	assert.False(t, mh.handleChannelControl(context.Background(), emptyMsg), "空文本应早退")
}

// TestHandleChannelControl_NewSessionBad 覆盖 /new_session 带后缀的非法分支。
func TestHandleChannelControl_NewSessionBad(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/new_session please"}`), schema.WithProvider("feishu"))

	assert.True(t, mh.handleChannelControl(context.Background(), msg))
	notice := waitNotice(t, mh)
	assert.Equal(t, "非法指令", notice.Payload["content"])
}

// TestRewindSlashConfirmPrompt_turn为0回落1 覆盖缺省轮次回落。
func TestRewindSlashConfirmPrompt_turn为0回落1(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))

	mh.rewindSlashConfirmPrompt(testUserInfos(msg), msg, command_parser.ParsedChannelControl{Action: command_parser.ActionRewindOK})

	notice := waitNoticeContaining(t, mh, "确认要回退到第 1 轮")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, "/rewind confirm 1")
}

// TestPayloadToInt_jsonNumber 覆盖 json.Number 分支。
func TestPayloadToInt_jsonNumber(t *testing.T) {
	assert.Equal(t, 7, payloadToInt(json.Number("7")))
	assert.Equal(t, 0, payloadToInt(json.Number("bad")))
	assert.Equal(t, 5, payloadToInt(int64(5)))
	assert.Equal(t, 3, payloadToInt(float64(3.9)))
	assert.Equal(t, 0, payloadToInt("not-a-number"))
}

// TestBranchSlashNotice_发送失败 覆盖 session.fork 发送出错分支（transport 已关闭）。
func TestBranchSlashNotice_发送失败(t *testing.T) {
	f, mh := newFakeAgentServer(t, branchReplies())
	f.closeTransport() // 关闭后 Send 返回 ErrTransportClosed

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "x",
	})
	notice := waitNotice(t, mh)
	assert.Contains(t, notice.Payload["error"], "分叉失败")
}

// TestRewindSlashNotice_发送失败_Discard 覆盖 E2A 发送出错后走本地兜底。
func TestRewindSlashNotice_发送失败_Discard(t *testing.T) {
	f, mh := newFakeAgentServer(t, rewindReplies(true))
	f.closeTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sid-without-history-44b")

	mh.rewindSlashNotice(context.Background(), testUserInfos(msg), msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 1,
	})
	notice := waitNoticeContaining(t, mh, "回退失败")
	assert.Contains(t, notice.Payload["error"], "回退失败")
}
