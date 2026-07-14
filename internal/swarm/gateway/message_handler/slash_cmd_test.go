package message_handler

import (
	"encoding/json"
	"testing"

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
		json.RawMessage(`{"content":"/new_session"}`))

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
		json.RawMessage(`{"content":"/mode code.normal"}`))

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
		json.RawMessage(`{"content":"/switch fast"}`))

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
		json.RawMessage(`{"content":"/skills list"}`))

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
		json.RawMessage(`{"content":"/branch feature-x"}`))

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
		json.RawMessage(`{"content":"/rewind 5"}`))

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
		json.RawMessage(`{"content":"/mode unknown"}`))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled, "非法 /mode 也应被处理（返回 BAD 通知）")
}

// TestHandleChannelControl_RewindBad 测试非法 /rewind
func TestHandleChannelControl_RewindBad(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/rewind"}`))

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

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/new_session"}`))

	parsed := command_parser.ParseChannelControlText("/new_session")
	mh.newSessionCancelAndNotice(msg, parsed)

	// 验证渠道状态已更新
	state := mh.GetOrCreateChannelState(msg)
	assert.NotEqual(t, "sess-1", state.SessionID, "sessionID 应已更新")
}

// TestModeChangeCancelAndNotice 测试 /mode 处理
func TestModeChangeCancelAndNotice(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/mode code.normal"}`))

	parsed := command_parser.ParsedChannelControl{
		Action:         command_parser.ActionModeOK,
		ModeSubcommand: "code.normal",
	}
	mh.modeChangeCancelAndNotice(msg, parsed)

	state := mh.GetOrCreateChannelState(msg)
	assert.Equal(t, ChannelModeCodeNormal, state.Mode)
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
				Action:            command_parser.ActionSwitchOK,
				SwitchSubcommand:  tt.switchCmd,
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
