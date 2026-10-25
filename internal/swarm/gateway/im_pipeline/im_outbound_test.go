package im_pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 注意：setupInteractionsEnv/writeTestConfig/newFakeModel/fakeAdapter 等已在
// im_inbound_test.go 定义（同包共享）。

// TestSkipEventTypes 四跳过事件逐字。
func TestSkipEventTypes(t *testing.T) {
	for _, et := range []string{"chat.delta", "chat.tool_call", "chat.tool_result", "todo.updated"} {
		assert.True(t, skipEventTypes[et], et)
	}
	assert.False(t, skipEventTypes["chat.final"])
	assert.Len(t, skipEventTypes, 4)
}

// TestGroupAckKeywords 九关键词逐字。
func TestGroupAckKeywords(t *testing.T) {
	assert.Equal(t, []string{"待办", "提醒", "定时", "日程", "会议", "行程", "安排", "记下了", "记住了"}, groupAckKeywords)
}

// TestFollowupRegex 群聊/私聊追问前缀正则（RE2 非贪婪捕获组）。
func TestFollowupRegex(t *testing.T) {
	m := reGroupFollowup.FindStringSubmatch("[群聊追问@张三] 具体问题")
	require.NotNil(t, m)
	assert.Equal(t, "张三", m[1])
	m2 := reGroupFollowup.FindStringSubmatch("[群聊追问] 具体问题")
	require.NotNil(t, m2)
	assert.Equal(t, "", m2[1])
	assert.Nil(t, reGroupFollowup.FindStringSubmatch("普通消息"))
	assert.NotNil(t, reDMFollowup.FindStringSubmatch("[私聊追问] 问题"))
	assert.Nil(t, reDMFollowup.FindStringSubmatch("[群聊追问] 问题"))
}

// TestExtractContent 三态：字符串 strip / dict 三键 / 其他 fmt.Sprint。
func TestExtractContent(t *testing.T) {
	msg := func(payload map[string]any) *schema.Message {
		m := schema.NewResMessage("ch", "s", true, payload)
		return m
	}
	assert.Equal(t, "hello", extractContent(msg(map[string]any{"content": " hello "})))
	assert.Equal(t, "out", extractContent(msg(map[string]any{"content": map[string]any{"output": "out"}})))
	assert.Equal(t, "text", extractContent(msg(map[string]any{"content": map[string]any{"text": "text"}})))
	assert.Equal(t, "msg", extractContent(msg(map[string]any{"content": map[string]any{"message": "msg"}})))
	assert.Equal(t, "", extractContent(msg(map[string]any{"content": map[string]any{}})))
	assert.Equal(t, "", extractContent(msg(nil)))
	assert.Equal(t, "123", extractContent(msg(map[string]any{"content": 123})))
	assert.Equal(t, "", extractContent(msg(map[string]any{"content": 0})))
}

// TestIsPersonalActionReply 空白移除 + 关键词命中。
func TestIsPersonalActionReply(t *testing.T) {
	assert.True(t, isPersonalActionReply("已为您创建 待办"))
	assert.True(t, isPersonalActionReply("提醒\n你\n明天开会"))
	assert.True(t, isPersonalActionReply("记住了"))
	assert.False(t, isPersonalActionReply("这是公开回复"))
	assert.False(t, isPersonalActionReply("   "))
	assert.False(t, isPersonalActionReply(""))
}

// TestNewHexID 8 位 hex（对齐 secrets.token_hex(4)）。
func TestNewHexID(t *testing.T) {
	id := newHexID()
	assert.Len(t, id, 8)
	for _, c := range id {
		assert.Regexp(t, `[0-9a-f]`, string(c))
	}
	assert.NotEqual(t, newHexID(), id)
}

// TestOutboundEnsureLLM_成功 完整配置初始化 + model_name 回退 + 幂等。
func TestOutboundEnsureLLM_成功(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_name: out-model\n  model_client_config:\n    api_key: sk-o\n    api_base: http://x/chat/completions\n    client_provider: OpenAI\n")
	p := NewIMOutboundPipeline()
	assert.True(t, p.ensureLLM())
	assert.Equal(t, "out-model", p.llmModelName)
	assert.NotNil(t, p.llm)
	assert.True(t, p.ensureLLM())
}

// TestOutboundEnsureLLM_缺key守卫 api_key/api_base 缺失 → 跳过（仅出站有守卫）。
func TestOutboundEnsureLLM_缺key守卫(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")
	p := NewIMOutboundPipeline()
	assert.False(t, p.ensureLLM())
	assert.Nil(t, p.llm)
}

// TestOutboundEnsureLLM_provider缺失默认 client_provider key 缺失 → "OpenAI"（get 默认）。
func TestOutboundEnsureLLM_provider缺失默认(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	assert.True(t, p.ensureLLM())
}

// TestOutboundRegister 注册/注销。
func TestOutboundRegister(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("ch-1", newTestAdapter())
	p.UnregisterAdapter("ch-1")
	p.UnregisterAdapter("nonexist")
}

// ──────────────────────────── Task 6: 出站 Apply + classifyPersonalAction ────────────────────────────

// newOutboundMsg 构造出站消息（res 形态 + payload）。
func newOutboundMsg(payload map[string]any, metadata map[string]any) *schema.Message {
	msg := schema.NewResMessage("feishu_test", "sess-1", true, payload)
	msg.Metadata = metadata
	return msg
}

// mergeMeta 合并两个 metadata（nil 容忍）。
func mergeMeta(base, overlay map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range overlay {
		m[k] = v
	}
	return m
}

// newDigitalAvatarOut 构造数字分身群聊出站消息。
// ⚠ 修正记录：必须置 GroupDigitalAvatar=true，否则 Apply 步骤1 短路。
func newDigitalAvatarOut(content string, metadata map[string]any) *schema.Message {
	msg := newOutboundMsg(map[string]any{"content": content, "event_type": "chat.final"},
		mergeMeta(map[string]any{"chat_type": "group"}, metadata))
	msg.GroupDigitalAvatar = true
	return msg
}

// TestOutboundApply_层1非数字分身 短路且 metadata 不变。
func TestOutboundApply_层1非数字分身(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "hi", "event_type": "chat.final"}, map[string]any{"chat_type": "group"})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.NotContains(t, msg.Metadata, "reply_scope")
}

// TestOutboundApply_avatarMode元数据路径 GroupDigitalAvatar=false 但 avatar_mode=true。
func TestOutboundApply_avatarMode元数据路径(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "hi"}, map[string]any{"chat_type": "group", "avatar_mode": true})
	assert.NoError(t, p.Apply(context.Background(), msg))
}

// TestOutboundApply_层2跳过事件 四跳过事件逐个。
func TestOutboundApply_层2跳过事件(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	for _, et := range []string{"chat.delta", "chat.tool_call", "chat.tool_result", "todo.updated"} {
		msg := newOutboundMsg(map[string]any{"content": "hi", "event_type": et}, map[string]any{"chat_type": "group"})
		msg.GroupDigitalAvatar = true
		assert.NoError(t, p.Apply(context.Background(), msg))
		assert.NotContains(t, msg.Metadata, "reply_scope")
	}
}

// TestOutboundApply_层3非群聊与层4无适配器。
func TestOutboundApply_层3非群聊与层4无适配器(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "hi"}, map[string]any{"chat_type": "p2p"})
	msg.GroupDigitalAvatar = true
	assert.NoError(t, p.Apply(context.Background(), msg))

	p2 := NewIMOutboundPipeline()
	msg2 := newDigitalAvatarOut("hi", nil)
	assert.NoError(t, p2.Apply(context.Background(), msg2))
}

// TestOutboundApply_层5空内容。
func TestOutboundApply_层5空内容(t *testing.T) {
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "  ", "event_type": "chat.final"}, map[string]any{"chat_type": "group"})
	msg.GroupDigitalAvatar = true
	assert.NoError(t, p.Apply(context.Background(), msg))
}

// TestOutboundApply_层6清除pending interaction_answered_user_id → Remove。
func TestOutboundApply_层6清除pending(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-answered", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "oc", OriginSenderName: "n", OriginSenderID: "o",
		Question: "q", TargetUserID: "ou-answered", Status: routing.StatusPending,
	}
	pi.Save()
	require.NotNil(t, routing.FindGroupPending("sess-1", "ou-answered"))

	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "群聊回复", "event_type": "chat.final"},
		map[string]any{"chat_type": "group", "interaction_answered_user_id": "ou-answered"})
	msg.GroupDigitalAvatar = true
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Nil(t, routing.FindGroupPending("sess-1", "ou-answered"))
}

// TestOutboundApply_层8replyScope已设与无候选。
func TestOutboundApply_层8replyScope已设与无候选(t *testing.T) {
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	msg := newDigitalAvatarOut("hi", map[string]any{"reply_scope": "dm"})
	assert.NoError(t, p.Apply(context.Background(), msg))

	adapter2 := newTestAdapter()
	p2 := NewIMOutboundPipeline()
	p2.RegisterAdapter("feishu_test", adapter2)
	msg2 := newDigitalAvatarOut("hi", nil)
	assert.NoError(t, p2.Apply(context.Background(), msg2))
}

// TestOutboundApply_LLM判DM全链 meta 四键升级。
func TestOutboundApply_LLM判DM全链(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("DM"), nil
	})

	msg := newDigitalAvatarOut("已为你记录待办", map[string]any{"reply_target_name": "李四"})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "dm", msg.Metadata["reply_scope"])
	assert.Equal(t, "ou-target", msg.Metadata["reply_feishu_open_id"])
	assert.Equal(t, "processor_target_user", msg.Metadata["reply_reason"])
	assert.Equal(t, true, msg.Metadata["reply_personal_action"])
}

// TestOutboundApply_replyReason回退 reply_candidate_reason 优先。
func TestOutboundApply_replyReason回退(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("dm"), nil
	})
	msg := newDigitalAvatarOut("回复", map[string]any{"reply_candidate_reason": "custom_reason"})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "custom_reason", msg.Metadata["reply_reason"])
}

// TestOutboundApply_LLM判CHAT 不动 reply_scope。
func TestOutboundApply_LLM判CHAT(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("CHAT"), nil
	})
	msg := newDigitalAvatarOut("公开回复", nil)
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.NotContains(t, msg.Metadata, "reply_scope")
}

// TestOutboundApply_LLM无结果关键词兜底 两态。
func TestOutboundApply_LLM无结果关键词兜底(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("无法判断"), nil
	})
	msg := newDigitalAvatarOut("已记录待办", nil)
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "dm", msg.Metadata["reply_scope"])

	p2 := NewIMOutboundPipeline()
	adapter2 := newTestAdapter()
	adapter2.candidateUserID = "ou-target"
	p2.RegisterAdapter("feishu_test", adapter2)
	require.True(t, p2.ensureLLM())
	p2.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("无法判断"), nil
	})
	msg2 := newDigitalAvatarOut("普通群聊回复", nil)
	assert.NoError(t, p2.Apply(context.Background(), msg2))
	assert.NotContains(t, msg2.Metadata, "reply_scope")
}

// TestOutboundApply_LLM失败兜底 ensureLLM false（缺 key）→ 关键词兜底。
func TestOutboundApply_LLM失败兜底(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)
	msg := newDigitalAvatarOut("已设置提醒", nil)
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "dm", msg.Metadata["reply_scope"])
}

// TestClassifyPersonalAction_prompt拼装 platform/target 回退/300/500 截断/原始请求段。
func TestClassifyPersonalAction_prompt拼装(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	require.True(t, p.ensureLLM())
	var gotPrompt string
	p.llm = newFakeModel(t, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// NewMessagesParam 路径：UserMessage 首 message
		gotPrompt = messages.Messages()[0].GetContent().Text()
		return llmschema.NewAssistantMessage("DM"), nil
	})
	longQuery := strings.Repeat("问", 400)
	longContent := strings.Repeat("答", 600)
	meta := map[string]any{
		"avatar_original_query": longQuery,
		"reply_target_name":     "",
	}
	isPersonal, raw := p.classifyPersonalAction(context.Background(), "飞书", meta, longContent)
	assert.True(t, *isPersonal)
	assert.Equal(t, "DM", raw)
	assert.Contains(t, gotPrompt, "请判断下面这条飞书机器人回复")
	assert.Contains(t, gotPrompt, "目标用户：目标用户")
	assert.Contains(t, gotPrompt, strings.Repeat("问", 300))
	assert.NotContains(t, gotPrompt, strings.Repeat("问", 301))
	assert.Contains(t, gotPrompt, strings.Repeat("答", 500))
}

// TestClassifyPersonalAction_invokeErr Invoke 失败 → (nil, "")。
func TestClassifyPersonalAction_invokeErr(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("boom")
	})
	isPersonal, raw := p.classifyPersonalAction(context.Background(), "飞书", map[string]any{}, "c")
	assert.Nil(t, isPersonal)
	assert.Equal(t, "", raw)
}

// TestClassifyPersonalAction_DMCHAT判定顺序 文本同时含 DM/CHAT 时 DM 先命中。
func TestClassifyPersonalAction_DMCHAT判定顺序(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n")
	p := NewIMOutboundPipeline()
	require.True(t, p.ensureLLM())
	p.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("CHAT then DM"), nil
	})
	isPersonal, _ := p.classifyPersonalAction(context.Background(), "飞书", map[string]any{}, "c")
	assert.True(t, *isPersonal)
}

// ──────────────────────────── Task 7: 追问 handlers ────────────────────────────

// TestGroupFollowup_全链 名字接口解析成功 → 四键 + payload 恢复 + 落盘断言。
func TestGroupFollowup_全链(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	adapter := &fakeAdapterWithNameResolver{
		fakeAdapter: fakeAdapter{
			principalUserID: "ou-principal", principalName: "张三",
			platformName: "飞书", replyUserIDKey: "reply_feishu_open_id",
			displayNameByID: map[string]string{"ou-sender": "王五"},
		},
		nameToID: map[string]string{"李四": "ou-lisi"},
	}
	p.RegisterAdapter("feishu_test", adapter)

	msg := newDigitalAvatarOut("[群聊追问@李四] 需要哪个楼层？", map[string]any{
		"avatar_original_query": "帮我订会议室",
		"im_sender_user_id":     "ou-sender",
	})
	assert.NoError(t, p.Apply(context.Background(), msg))

	assert.Equal(t, "李四", msg.Metadata["interaction_mention_user"])
	assert.Equal(t, "ou-lisi", msg.Metadata["interaction_mention_user_id"])
	assert.Equal(t, true, msg.Metadata["interaction_question"])
	assert.Equal(t, "[群聊追问@李四] 需要哪个楼层？", msg.Payload["content"])

	pi := routing.LoadPendingInteraction("gpq_sess-1_ou-lisi")
	require.NotNil(t, pi)
	assert.Equal(t, routing.ModeGroup, pi.Mode)
	assert.Equal(t, "feishu_test", pi.OriginChannelID)
	assert.Equal(t, "sess-1", pi.OriginSessionID)
	assert.Equal(t, "帮我订会议室", pi.OriginContent)
	assert.Equal(t, "李四", pi.OriginSenderName)
	assert.Equal(t, "ou-sender", pi.OriginSenderID)
	assert.Equal(t, "需要哪个楼层？", pi.Question)
	assert.Equal(t, "ou-lisi", pi.TargetUserID)
	assert.Equal(t, "李四", pi.TargetUserName)
	assert.Equal(t, routing.StatusPending, pi.Status)
	// OriginMetadata 拷贝隔离
	msg.Metadata["new_key"] = "x"
	pi2 := routing.LoadPendingInteraction("gpq_sess-1_ou-lisi")
	assert.NotContains(t, pi2.OriginMetadata, "new_key")
}

// TestGroupFollowup_sender回退 名字接口未实现 + target_name 空 → sender 回退 + 显示名补齐。
func TestGroupFollowup_sender回退(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.displayNameByID = map[string]string{"ou-sender": "王五"}
	p.RegisterAdapter("feishu_test", adapter)

	msg := newDigitalAvatarOut("[群聊追问] 需要哪个楼层？", map[string]any{
		"open_id": "ou-sender",
	})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "王五", msg.Metadata["interaction_mention_user"])
	assert.Equal(t, "ou-sender", msg.Metadata["interaction_mention_user_id"])
	pi := routing.LoadPendingInteraction("gpq_sess-1_ou-sender")
	require.NotNil(t, pi)
	assert.Equal(t, "王五", pi.TargetUserName)
}

// TestGroupFollowup_candidate回退 sender 也空 → candidate + reply_target_name 补名。
func TestGroupFollowup_candidate回退(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-cand"
	p.RegisterAdapter("feishu_test", adapter)

	msg := newDigitalAvatarOut("[群聊追问] 问题", map[string]any{
		"reply_target_name": "赵六",
	})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "赵六", msg.Metadata["interaction_mention_user"])
	assert.Equal(t, "ou-cand", msg.Metadata["interaction_mention_user_id"])
	pi := routing.LoadPendingInteraction("gpq_sess-1_ou-cand")
	require.NotNil(t, pi)
	assert.Equal(t, "赵六", pi.TargetUserName)
}

// TestGroupFollowup_名字接口空解析 ResolveUserIDByName 空串 → sender 回退。
func TestGroupFollowup_名字接口空解析(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	adapter := &fakeAdapterWithNameResolver{
		fakeAdapter: fakeAdapter{principalUserID: "p", principalName: "n", platformName: "飞书"},
		nameToID:    map[string]string{},
	}
	p.RegisterAdapter("feishu_test", adapter)
	msg := newDigitalAvatarOut("[群聊追问@李四] q", map[string]any{"im_sender_user_id": "ou-sender"})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "ou-sender", msg.Metadata["interaction_mention_user_id"])
}

// TestGroupFollowup_全空 无 sender 无 candidate → ID 尾空串仍落盘。
func TestGroupFollowup_全空(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newDigitalAvatarOut("[群聊追问@某人] q", nil)
	assert.NoError(t, p.Apply(context.Background(), msg))
	pi := routing.LoadPendingInteraction("gpq_sess-1_")
	require.NotNil(t, pi)
}

// TestDMFollowup_全链 前缀剥离 + reply_scope/candidate 键 + payload 恢复 + 落盘。
func TestDMFollowup_全链(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	adapter := newTestAdapter()
	adapter.candidateUserID = "ou-target"
	p.RegisterAdapter("feishu_test", adapter)

	msg := newDigitalAvatarOut("[私聊追问] 需要哪个楼层？", map[string]any{
		"avatar_original_query": "原始请求",
		"reply_target_name":     "李四",
		"im_sender_user_id":     "ou-sender",
	})
	assert.NoError(t, p.Apply(context.Background(), msg))

	assert.Equal(t, "dm", msg.Metadata["reply_scope"])
	assert.Equal(t, true, msg.Metadata["interaction_question"])
	assert.Equal(t, "ou-target", msg.Metadata["reply_feishu_open_id"])
	assert.Equal(t, "[私聊追问] 需要哪个楼层？", msg.Payload["content"])

	dir := pathutil.AgentInteractionsDir()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	name := entries[0].Name()
	assert.Regexp(t, `^iact_feishu_test_[0-9a-f]{8}\.json$`, name)
	pi := routing.LoadPendingInteraction(strings.TrimSuffix(name, ".json"))
	require.NotNil(t, pi)
	assert.Equal(t, routing.ModeDM, pi.Mode)
	assert.Equal(t, "需要哪个楼层？", pi.Question)
	assert.Equal(t, "ou-target", pi.TargetUserID)
	assert.Equal(t, "", pi.TargetUserName)
	assert.Equal(t, "李四", pi.OriginSenderName)
	assert.Equal(t, "原始请求", pi.OriginContent)
}

// TestDMFollowup_无候选 candidate 空 → reply 键不写、TargetUserID 空。
func TestDMFollowup_无候选(t *testing.T) {
	setupInteractionsEnv(t)
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newDigitalAvatarOut("[私聊追问] q", nil)
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Equal(t, "dm", msg.Metadata["reply_scope"])
	assert.NotContains(t, msg.Metadata, "reply_feishu_open_id")
}

// TestApply_清除先于前缀解析 两者并存场景断言清除发生。
func TestApply_清除先于前缀解析(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-answered", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "oc", OriginSenderName: "n", OriginSenderID: "o",
		Question: "q", TargetUserID: "ou-answered", Status: routing.StatusPending,
	}
	pi.Save()

	p := NewIMOutboundPipeline()
	adapter := &fakeAdapterWithNameResolver{
		fakeAdapter: fakeAdapter{principalUserID: "p", principalName: "n", platformName: "飞书"},
		nameToID:    map[string]string{"李四": "ou-lisi"},
	}
	p.RegisterAdapter("feishu_test", adapter)

	msg := newDigitalAvatarOut("[群聊追问@李四] 新问题", map[string]any{
		"interaction_answered_user_id": "ou-answered",
	})
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Nil(t, routing.FindGroupPending("sess-1", "ou-answered"))
	assert.NotNil(t, routing.LoadPendingInteraction("gpq_sess-1_ou-lisi"))
}

// ──────────────────────────── 审查偏差修复回归（2026-11-04） ────────────────────────────

// TestIsPersonalActionReply_Unicode空白 审查偏差②修复：Python re \s 为 Unicode 语义，
// 关键词中间夹全角空格/NBSP/行分隔符时移除后仍命中关键词。
func TestIsPersonalActionReply_Unicode空白(t *testing.T) {
	assert.True(t, isPersonalActionReply("记\u3000下了"))        // 全角空格
	assert.True(t, isPersonalActionReply("记\u00a0下了"))        // NBSP
	assert.True(t, isPersonalActionReply("提\u2028醒"))         // 行分隔符
	assert.True(t, isPersonalActionReply("待\u3000办\u3000已建")) // 多处全角空白
	assert.False(t, isPersonalActionReply("记\u4e00下了"))       // 非空白汉字不移除，不命中
}

// TestExtractContent_第三态pythonStr 审查偏差③修复：truthy 非字符串经 pythonStr
// 转换——true → "True"（对齐 Python str()；原 fmt.Sprint 产 "true"）；数字仍 "123"。
// false 走 Python str(False or "") → "" 路径（or 链先吞 False），非差异场景。
func TestExtractContent_第三态pythonStr(t *testing.T) {
	msg := func(payload map[string]any) *schema.Message {
		return schema.NewResMessage("ch", "s", true, payload)
	}
	assert.Equal(t, "True", extractContent(msg(map[string]any{"content": true})))
	assert.Equal(t, "123", extractContent(msg(map[string]any{"content": 123})))
	assert.Equal(t, "123", extractContent(msg(map[string]any{"content": 123.0})))
	assert.Equal(t, "", extractContent(msg(map[string]any{"content": false})))
}

// TestOutboundEnsureLLM_provider显式null 审查偏差④修复：key 存在但 null →
// pythonStr → "" → 构造校验失败 → false（对齐 Python pydantic 拒绝 None）。
func TestOutboundEnsureLLM_provider显式null(t *testing.T) {
	setupInteractionsEnv(t)
	// YAML 显式 null key：client_provider:（空值）
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n    client_provider:\n")
	p := NewIMOutboundPipeline()
	assert.False(t, p.ensureLLM())
	assert.Nil(t, p.llm)
}

// TestOutboundEnsureLLM_provider空串 审查偏差④修复：key 存在但空串 → 校验失败 → false。
func TestOutboundEnsureLLM_provider空串(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n    client_provider: \"\"\n")
	p := NewIMOutboundPipeline()
	assert.False(t, p.ensureLLM())
	assert.Nil(t, p.llm)
}

// TestOutboundEnsureLLM_provider小写归一 审查偏差④伴随验证："openai" 小写 →
// 两边均归一化 "OpenAI" → 构造成功。
func TestOutboundEnsureLLM_provider小写归一(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: k\n    api_base: http://x\n    client_provider: openai\n")
	p := NewIMOutboundPipeline()
	assert.True(t, p.ensureLLM())
}

// TestOutboundApply_层6清除pending_数字ID_审查偏差⑤ 审查偏差⑤修复：数字类型
// interaction_answered_user_id（Python str(12345) truthy）转字符串后照常清除。
func TestOutboundApply_层6清除pending_数字ID_审查偏差(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_12345", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "oc", OriginSenderName: "n", OriginSenderID: "o",
		Question: "q", TargetUserID: "12345", Status: routing.StatusPending,
	}
	pi.Save()
	require.NotNil(t, routing.FindGroupPending("sess-1", "12345"))

	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "群聊回复", "event_type": "chat.final"},
		map[string]any{"chat_type": "group", "interaction_answered_user_id": 12345})
	msg.GroupDigitalAvatar = true
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Nil(t, routing.FindGroupPending("sess-1", "12345"))
}

// TestOutboundApply_层6清除pending_带空白ID_审查偏差① 带空白填充的字符串 ID
// stripOrStr 归一后照常清除。
func TestOutboundApply_层6清除pending_带空白ID_审查偏差(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-answered", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "oc", OriginSenderName: "n", OriginSenderID: "o",
		Question: "q", TargetUserID: "ou-answered", Status: routing.StatusPending,
	}
	pi.Save()

	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", newTestAdapter())
	msg := newOutboundMsg(map[string]any{"content": "群聊回复", "event_type": "chat.final"},
		map[string]any{"chat_type": "group", "interaction_answered_user_id": " ou-answered "})
	msg.GroupDigitalAvatar = true
	assert.NoError(t, p.Apply(context.Background(), msg))
	assert.Nil(t, routing.FindGroupPending("sess-1", "ou-answered"))
}

// TestGroupFollowup_元数据带空白 审查偏差①端到端：sender 三键带空白 →
// PendingInteraction.OriginSenderID 落盘干净值。
func TestGroupFollowup_元数据带空白(t *testing.T) {
	setupInteractionsEnv(t)
	adapter := newTestAdapter()
	adapter.displayNameByID = map[string]string{"ou-sender": "王五"}
	p := NewIMOutboundPipeline()
	p.RegisterAdapter("feishu_test", adapter)
	msg := newDigitalAvatarOut("[群聊追问]啥时候交付？",
		map[string]any{"im_sender_user_id": " ou-sender "})
	assert.NoError(t, p.Apply(context.Background(), msg))

	pi := routing.FindGroupPending("sess-1", "ou-sender")
	require.NotNil(t, pi)
	assert.Equal(t, "ou-sender", pi.OriginSenderID)
	assert.Equal(t, "王五", pi.TargetUserName)
}
