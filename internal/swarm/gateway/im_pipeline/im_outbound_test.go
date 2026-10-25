package im_pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
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
