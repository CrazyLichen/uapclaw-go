package message_handler

import (
	"encoding/json"
	"strings"
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled, "非法 /mode 也应被处理（返回 BAD 通知）")
}

// TestHandleChannelControl_Skills不创建渠道状态 A7：Python 在 SKILLS_OK 处提前 return，
// 不调用 _get_or_create_channel_state；Go 必须在建 state 之前短路。
func TestHandleChannelControl_Skills不创建渠道状态(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(msg)
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

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled, "非法 /rewind 也应被处理")
}

// TestHandleChannelControl_非请求消息 测试非请求消息
func TestHandleChannelControl_非请求消息(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatDelta}
	handled := mh.handleChannelControl(msg)
	assert.False(t, handled)
}

// TestHandleChannelControl_非控制文本 测试非控制文本
func TestHandleChannelControl_非控制文本(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"普通消息"}`))
	handled := mh.handleChannelControl(msg)
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

	mh.newSessionCancelAndNotice(msg, oldSID, "new-sid-x")

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

// TestHandleChannelControl_newSession企业通道SessionMap轮转
// Python L654-667：企业通道身份齐备走 SessionMap rotate=true
func TestHandleChannelControl_newSession企业通道SessionMap轮转(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.sessionMap = newTestSessionMap(t)

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_enterprise:app-1", "sess-old",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"/new_session"}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled)

	state := mh.GetOrCreateChannelState(msg)
	if !strings.Contains(state.SessionID, "feishu::oc-9::bot-1::") {
		t.Errorf("企业通道 /new_session 应经 SessionMap 轮转，实际 %q", state.SessionID)
	}
	if state.SessionID == "sess-old" {
		t.Error("应轮转出新值")
	}
}

// TestHandleChannelControl_newSession非企业通道走GenerateChannelSessionID
func TestHandleChannelControl_newSession非企业通道(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.sessionMap = newTestSessionMap(t)

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-old",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"/new_session"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled)

	state := mh.GetOrCreateChannelState(msg)
	if !strings.HasPrefix(state.SessionID, "feishu") {
		t.Errorf("非企业通道应走 GenerateChannelSessionID（feishu 前缀），实际 %q", state.SessionID)
	}
	if state.SessionID == "sess-old" {
		t.Error("应生成新值")
	}
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

	parsed := command_parser.ParsedChannelControl{
		Action:         command_parser.ActionModeOK,
		ModeSubcommand: "code.normal",
	}
	mh.modeChangeCancelAndNotice(msg, parsed)

	state := mh.GetOrCreateChannelState(msg)
	assert.Equal(t, ChannelModeCodeNormal, state.Mode)
}

// TestModeChange_一级映射 S-01 回归测试。
// Python L715-730 的 8 路映射：一级值 agent/code/team 必须显式映射，
// 原实现走 ParseChannelMode 会把一级值回落 AgentPlan。
func TestModeChange_一级映射(t *testing.T) {
	tests := []struct {
		subcommand string
		wantMode   ChannelMode
	}{
		{subcommand: "agent", wantMode: ChannelModeAgentPlan},
		{subcommand: "code", wantMode: ChannelModeCodeNormal},
		{subcommand: "team", wantMode: ChannelModeTeam},
		{subcommand: "agent.plan", wantMode: ChannelModeAgentPlan},
		{subcommand: "agent.fast", wantMode: ChannelModeAgentFast},
		{subcommand: "code.plan", wantMode: ChannelModeCodePlan},
		{subcommand: "code.normal", wantMode: ChannelModeCodeNormal},
		{subcommand: "code.team", wantMode: ChannelModeCodeTeam},
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
			mh.modeChangeCancelAndNotice(msg, command_parser.ParsedChannelControl{
				Action:         command_parser.ActionModeOK,
				ModeSubcommand: tt.subcommand,
			})
			state := mh.GetOrCreateChannelState(msg)
			assert.Equal(t, tt.wantMode, state.Mode, "/mode %s 应映射到 %v", tt.subcommand, tt.wantMode)
		})
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
	mh.rewindSlashConfirmPrompt(msg, command_parser.ParsedChannelControl{
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
	mh.sendChannelNotice(msg, map[string]any{"content": "测试通知"})

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
	mh.sendChannelNotice(msg, map[string]any{"content": "测试通知"})

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

// TestSwitchMode_模式家族映射 测试 /switch 命令根据当前模式家族决定目标模式
func TestSwitchMode_模式家族映射(t *testing.T) {
	tests := []struct {
		name        string
		currentMode ChannelMode
		switchCmd   string
		wantMode    ChannelMode
		wantInvalid bool
	}{
		// agent 家族 → /switch plan → agent.plan
		{name: "agent_plan_切换plan", currentMode: ChannelModeAgentPlan, switchCmd: "plan", wantMode: ChannelModeAgentPlan},
		{name: "agent_fast_切换plan", currentMode: ChannelModeAgentFast, switchCmd: "plan", wantMode: ChannelModeAgentPlan},
		// code 家族 → /switch plan → code.plan
		{name: "code_plan_切换plan", currentMode: ChannelModeCodePlan, switchCmd: "plan", wantMode: ChannelModeCodePlan},
		{name: "code_normal_切换plan", currentMode: ChannelModeCodeNormal, switchCmd: "plan", wantMode: ChannelModeCodePlan},
		{name: "code_team_切换plan", currentMode: ChannelModeCodeTeam, switchCmd: "plan", wantMode: ChannelModeCodePlan},
		// agent 家族 → /switch fast → agent.fast
		{name: "agent_plan_切换fast", currentMode: ChannelModeAgentPlan, switchCmd: "fast", wantMode: ChannelModeAgentFast},
		{name: "agent_fast_切换fast", currentMode: ChannelModeAgentFast, switchCmd: "fast", wantMode: ChannelModeAgentFast},
		// code 家族 /switch fast → 非法
		{name: "code_normal_切换fast_非法", currentMode: ChannelModeCodeNormal, switchCmd: "fast", wantInvalid: true},
		// code 家族 → /switch normal → code.normal
		{name: "code_plan_切换normal", currentMode: ChannelModeCodePlan, switchCmd: "normal", wantMode: ChannelModeCodeNormal},
		{name: "code_normal_切换normal", currentMode: ChannelModeCodeNormal, switchCmd: "normal", wantMode: ChannelModeCodeNormal},
		{name: "code_team_切换normal", currentMode: ChannelModeCodeTeam, switchCmd: "normal", wantMode: ChannelModeCodeNormal},
		// agent 家族 /switch normal → 非法
		{name: "agent_plan_切换normal_非法", currentMode: ChannelModeAgentPlan, switchCmd: "normal", wantInvalid: true},
		// code 家族 → /switch team → code.team
		{name: "code_plan_切换team", currentMode: ChannelModeCodePlan, switchCmd: "team", wantMode: ChannelModeCodeTeam},
		{name: "code_normal_切换team", currentMode: ChannelModeCodeNormal, switchCmd: "team", wantMode: ChannelModeCodeTeam},
		{name: "code_team_切换team", currentMode: ChannelModeCodeTeam, switchCmd: "team", wantMode: ChannelModeCodeTeam},
		// agent 家族 /switch team → 非法
		{name: "agent_fast_切换team_非法", currentMode: ChannelModeAgentFast, switchCmd: "team", wantInvalid: true},
		// team 模式 /switch fast → 非法
		{name: "team_切换fast_非法", currentMode: ChannelModeTeam, switchCmd: "fast", wantInvalid: true},
		// team 模式 /switch normal → 非法
		{name: "team_切换normal_非法", currentMode: ChannelModeTeam, switchCmd: "normal", wantInvalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mh := createTestMessageHandlerWithTransport()
			go func() {
				for range mh.robotMessages {
				}
			}()

			msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
				json.RawMessage(`{"content":"/switch `+tt.switchCmd+`"}`))

			// 设置当前模式
			state := mh.GetOrCreateChannelState(msg)
			state.Mode = tt.currentMode

			parsed := command_parser.ParsedChannelControl{
				Action:           command_parser.ActionSwitchOK,
				SwitchSubcommand: tt.switchCmd,
			}
			mh.modeChangeCancelAndNotice(msg, parsed)

			if tt.wantInvalid {
				// 非法指令时模式不应变更
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
	mh.skillsSlashNotice(msg)
	// 二次调用确保随机后缀路径稳定
	mh.skillsSlashNotice(msg)
}
