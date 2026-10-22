package message_handler

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestChannelModeString 测试 ChannelMode 字符串转换
func TestChannelModeString(t *testing.T) {
	tests := map[ChannelMode]string{
		ChannelModeAgentPlan:  "agent.plan",
		ChannelModeAgentFast:  "agent.fast",
		ChannelModeCodePlan:   "code.plan",
		ChannelModeCodeNormal: "code.normal",
		ChannelModeCodeTeam:   "code.team",
		ChannelModeTeam:       "team",
	}
	for mode, expected := range tests {
		if got := ChannelModeString(mode); got != expected {
			t.Errorf("ChannelModeString(%v) = %q, want %q", mode, got, expected)
		}
	}
}

// TestParseChannelMode 测试 ChannelMode 解析
func TestParseChannelMode(t *testing.T) {
	tests := map[string]ChannelMode{
		"agent.plan":  ChannelModeAgentPlan,
		"agent.fast":  ChannelModeAgentFast,
		"code.plan":   ChannelModeCodePlan,
		"code.normal": ChannelModeCodeNormal,
		"code.team":   ChannelModeCodeTeam,
		"team":        ChannelModeTeam,
		// 大小写无关
		"AGENT.PLAN":  ChannelModeAgentPlan,
		" Code.Plan ": ChannelModeCodePlan,
	}
	for input, expected := range tests {
		if got := ParseChannelMode(input); got != expected {
			t.Errorf("ParseChannelMode(%q) = %v, want %v", input, got, expected)
		}
	}

	// 非法值返回默认
	if got := ParseChannelMode("unknown"); got != ChannelModeAgentPlan {
		t.Errorf("ParseChannelMode(unknown) = %v, want ChannelModeAgentPlan", got)
	}
}

// TestIsValidChannelMode 测试 ChannelMode 有效性判断
func TestIsValidChannelMode(t *testing.T) {
	valid := []string{"agent.plan", "agent.fast", "code.plan", "code.normal", "code.team", "team"}
	for _, v := range valid {
		if !IsValidChannelMode(v) {
			t.Errorf("%q 应为合法 ChannelMode", v)
		}
	}
	invalid := []string{"unknown", "agent", "code", ""}
	for _, v := range invalid {
		if IsValidChannelMode(v) {
			t.Errorf("%q 应为非法 ChannelMode", v)
		}
	}
}

// TestApplyChannelState_受控渠道 测试受控渠道的状态注入
func TestApplyChannelState_受控渠道(t *testing.T) {
	mh := createTestMessageHandler()

	// 创建飞书渠道消息
	msg := schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))

	// 应用渠道状态
	mh.ApplyChannelState(msg)

	// 验证 session_id 已注入
	if msg.SessionID == "" {
		t.Error("受控渠道消息应有 session_id 注入")
	}

	// 验证 mode 已注入到 params
	var params map[string]any
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("解析 params 失败：%v", err)
	}
	if mode, ok := params["mode"]; !ok {
		t.Error("params 中应有 mode 字段")
	} else if mode != "agent.plan" {
		t.Errorf("mode 应为 agent.plan，实际：%v", mode)
	}
}

// TestApplyChannelState_非受控渠道 测试非受控渠道跳过状态注入
func TestApplyChannelState_非受控渠道(t *testing.T) {
	mh := createTestMessageHandler()

	// 创建 Web 渠道消息（Web 不在受控类型中）
	msg := schema.NewReqMessage("web_test", "existing-session", schema.ReqMethodChatSend, json.RawMessage(`{}`))

	// 应用渠道状态
	mh.ApplyChannelState(msg)

	// Web 渠道不在受控类型中，ApplyChannelState 应直接返回
	// session_id 不应被覆盖
	if msg.SessionID != "existing-session" {
		t.Errorf("非受控渠道 session_id 不应被覆盖，实际：%q", msg.SessionID)
	}
}

// TestApplyChannelState_modeSetdefault 测试 mode 的 setdefault 语义
func TestApplyChannelState_modeSetdefault(t *testing.T) {
	mh := createTestMessageHandler()

	// 创建消息，params 中已有 mode
	msg := schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatSend, json.RawMessage(`{"mode":"code.plan"}`))

	mh.ApplyChannelState(msg)

	// mode 已存在，不应被覆盖
	var params map[string]any
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("解析 params 失败：%v", err)
	}
	if mode := params["mode"]; mode != "code.plan" {
		t.Errorf("已有 mode 不应被覆盖，实际：%v", mode)
	}
}

// TestGetOrCreateChannelState 测试渠道状态获取/创建
func TestGetOrCreateChannelState(t *testing.T) {
	mh := createTestMessageHandler()

	msg1 := schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	state1 := mh.GetOrCreateChannelState(msg1)

	// 第二次获取应返回同一个状态
	msg2 := schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	state2 := mh.GetOrCreateChannelState(msg2)

	if state1 != state2 {
		t.Error("相同 channel 应返回同一个 ChannelControlState 实例")
	}

	// 不同 channel 应创建新状态
	msg3 := schema.NewReqMessage("feishu_other", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	state3 := mh.GetOrCreateChannelState(msg3)

	if state1 == state3 {
		t.Error("不同 channel 应返回不同 ChannelControlState 实例")
	}
}

// TestGenerateChannelSessionID 测试 session ID 生成
func TestGenerateChannelSessionID(t *testing.T) {
	id1 := GenerateChannelSessionID("testchannel")
	id2 := GenerateChannelSessionID("testchannel")

	// 应以 channelID 开头
	if !strings.HasPrefix(id1, "testchannel_") {
		t.Errorf("生成的 ID 应以 'testchannel_' 开头，实际：%q", id1)
	}

	// 两次生成的 ID 应不同
	if id1 == id2 {
		t.Error("两次生成的 session ID 应不同")
	}

	// 格式：{channelID}_{hex_ts}_{6_hex}
	// 从末尾提取最后两部分
	partsAll := strings.Split(id1, "_")
	// 最后一个部分是 6 hex 随机后缀
	suffix := partsAll[len(partsAll)-1]
	if len(suffix) != 6 {
		t.Errorf("随机后缀应为 6 个 hex 字符，实际：%d (suffix=%q)", len(suffix), suffix)
	}
}

// TestControlChannelTypes 测试受控渠道类型集合
func TestControlChannelTypes(t *testing.T) {
	// 受控渠道
	controlled := []string{
		string(channel_manager.ChannelTypeFeishu),
		string(channel_manager.ChannelTypeXiaoyi),
		string(channel_manager.ChannelTypeDingTalk),
		string(channel_manager.ChannelTypeWhatsApp),
		string(channel_manager.ChannelTypeWeCom),
		string(channel_manager.ChannelTypeWeChat),
	}
	for _, ct := range controlled {
		if !controlChannelTypes[ct] {
			t.Errorf("%q 应为受控渠道类型", ct)
		}
	}

	// 非受控渠道
	uncontrolled := []string{
		string(channel_manager.ChannelTypeWeb),
		string(channel_manager.ChannelTypeACP),
		string(channel_manager.ChannelTypeCLI),
	}
	for _, ct := range uncontrolled {
		if controlChannelTypes[ct] {
			t.Errorf("%q 不应为受控渠道类型", ct)
		}
	}
}

// TestInferChannelTypeFromID 测试从 channelID 推断渠道类型
func TestInferChannelTypeFromID(t *testing.T) {
	tests := map[string]channel_manager.ChannelType{
		"web_123":    channel_manager.ChannelTypeWeb,
		"feishu_abc": channel_manager.ChannelTypeFeishu,
		"dingtalk_x": channel_manager.ChannelTypeDingTalk,
		"tui_1":      channel_manager.ChannelTypeCLI,
	}
	for id, expected := range tests {
		if got := inferChannelTypeFromID(id); got != expected {
			t.Errorf("inferChannelTypeFromID(%q) = %v, want %v", id, got, expected)
		}
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestGetChannelStateKey 测试渠道状态复合键生成
func TestGetChannelStateKey(t *testing.T) {
	if got := getChannelStateKey("ch1", ""); got != "ch1" {
		t.Errorf("无 sessionID 时 key 应为 channelID，实际：%q", got)
	}
	if got := getChannelStateKey("ch1", "sess1"); got != "ch1:sess1" {
		t.Errorf("有 sessionID 时 key 应为 channelID:sessionID，实际：%q", got)
	}
}

// TestGenerateRandomHex 测试随机 hex 生成
func TestGenerateRandomHex(t *testing.T) {
	h := generateRandomHex(3)
	if len(h) != 6 {
		t.Errorf("3 字节应生成 6 个 hex 字符，实际：%d", len(h))
	}
}

// createTestMessageHandler 创建测试用 MessageHandler
func createTestMessageHandler() *MessageHandler {
	return &MessageHandler{
		userMessages:   make(chan *schema.Message, 256),
		robotMessages:  make(chan *schema.Message, 256),
		streamTasks:    make(map[string]*streamTaskEntry),
		streamSessions: make(map[string]string),
		streamMetadata: make(map[string]map[string]any),
		streamModes:    make(map[string]string),
		channelStates:  make(map[string]*ChannelControlState),
	}
}

// newTestSessionMap 测试用 SessionMap（t.TempDir 隔离，避免写真实 checkpoint 目录）
func newTestSessionMap(t *testing.T) *routing.SessionMap {
	return routing.NewSessionMapAtForTest(filepath.Join(t.TempDir(), "session_map.json"), routing.SessionMapScopePerChatBot)
}

// TestApplyChannelState_sessionMap分支 企业通道身份齐备走 SessionMap
// Python 事实：FeishuChannel.name="feishu"（provider，受控检查通过），
// channel_id="feishu_enterprise:{app_id}"（SessionMap 通道族前缀匹配）
func TestApplyChannelState_sessionMap分支(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise:app-1", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	mh.ApplyChannelState(msg)

	if !strings.Contains(msg.SessionID, "feishu::oc-9::bot-1::") {
		t.Errorf("SessionMap 分支应注入 session-map 风格 sid，实际 %q", msg.SessionID)
	}
	// state 同步刷新（无条件覆盖，spec 陷阱点）
	state := mh.GetOrCreateChannelState(msg)
	if state.SessionID != msg.SessionID {
		t.Errorf("state.SessionID 应与 msg.SessionID 同步: %q vs %q", state.SessionID, msg.SessionID)
	}
	// 二次应用幂等（非轮转返回同值）
	first := msg.SessionID
	mh.ApplyChannelState(msg)
	if msg.SessionID != first {
		t.Errorf("二次 Apply 应幂等: %q vs %q", msg.SessionID, first)
	}
}

// TestApplyChannelState_身份缺失走elif 企业通道但缺 user_id → config/state 路径
// Python L1158-1159：elif state.session_id → msg.session_id = state.session_id（覆盖入站值）
func TestApplyChannelState_身份缺失走elif(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise:app-1", "existing-sid",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1")) // 缺 UserID

	mh.ApplyChannelState(msg)
	// 身份缺失 → 不走 SessionMap → elif 分支用 state 覆盖（state 为首建生成值）
	if msg.SessionID == "existing-sid" {
		t.Errorf("elif 分支应用 state.SessionID 覆盖入站 sid（对齐 Python L1158-1159），实际保留入站值")
	}
	if strings.Contains(msg.SessionID, "::") {
		t.Errorf("身份缺失不应走 SessionMap，实际 sid %q", msg.SessionID)
	}
}

// TestApplyChannelState_非企业通道不触发SessionMap
// provider=feishu 受控，但 channel_id 无 feishu_enterprise 前缀 → 走 elif
func TestApplyChannelState_非企业通道不触发SessionMap(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_test", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	mh.ApplyChannelState(msg)
	if strings.Contains(msg.SessionID, "::") {
		t.Errorf("非企业通道不应走 SessionMap，实际 sid %q", msg.SessionID)
	}
}

// TestGetOrCreateChannelState_首次创建sessionMap覆盖 首建时 SessionMap 覆盖 config 默认值
func TestGetOrCreateChannelState_首次创建sessionMap覆盖(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise:app-1", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	state := mh.GetOrCreateChannelState(msg)
	if !strings.Contains(state.SessionID, "feishu::oc-9::bot-1::") {
		t.Errorf("首次创建应被 SessionMap 覆盖 config 默认，实际 %q", state.SessionID)
	}
}
