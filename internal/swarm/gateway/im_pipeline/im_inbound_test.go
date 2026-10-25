package im_pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助（fakeAdapter，后续任务扩展） ────────────────────────────

// fakeAdapter 实现 IMPlatformAdapter 主接口（不含 ResolveUserIDByName 可选能力）。
type fakeAdapter struct {
	principalUserID    string
	principalName      string
	displayNameByID    map[string]string
	botMentions        []string
	history            []IMHistoryMessage
	relevancePatch     map[string]any
	platformName       string
	replyUserIDKey     string
	useKeywordOverride bool
	candidateUserID    string
}

func (f *fakeAdapter) GetPrincipalUserID() string { return f.principalUserID }
func (f *fakeAdapter) GetPrincipalDisplayName() string {
	return f.principalName
}
func (f *fakeAdapter) ResolveUserDisplayName(userID string) string {
	if n, ok := f.displayNameByID[userID]; ok {
		return n
	}
	return ""
}
func (f *fakeAdapter) GetBotMentionTokens() []string { return f.botMentions }
func (f *fakeAdapter) LoadRecentMessages(threadID string, limit int) []IMHistoryMessage {
	return f.history
}
func (f *fakeAdapter) BuildRelevanceMetadata(metadata map[string]any, senderUserID string, relevant bool) map[string]any {
	patch := map[string]any{"relevant": relevant, "sender": senderUserID}
	for k, v := range f.relevancePatch {
		patch[k] = v
	}
	return patch
}
func (f *fakeAdapter) PlatformName() string   { return f.platformName }
func (f *fakeAdapter) ReplyUserIDKey() string { return f.replyUserIDKey }
func (f *fakeAdapter) UseKeywordOverride() bool {
	return f.useKeywordOverride
}
func (f *fakeAdapter) GetCandidateUserID(metadata map[string]any) string {
	return f.candidateUserID
}

// fakeAdapterWithNameResolver 嵌入 fakeAdapter 并实现 imUserIDNameResolver 可选能力。
type fakeAdapterWithNameResolver struct {
	fakeAdapter
	nameToID map[string]string
}

func (f *fakeAdapterWithNameResolver) ResolveUserIDByName(name string) string {
	return f.nameToID[name]
}

// promptCaptureAdapter 嵌入 fakeAdapter 并捕获 LoadRecentMessages 的 threadID
// （审查偏差①回归：threadID strip 归一验证）。
type promptCaptureAdapter struct {
	fakeAdapter
	gotThreadIDs *[]string
}

func (f *promptCaptureAdapter) LoadRecentMessages(threadID string, limit int) []IMHistoryMessage {
	*f.gotThreadIDs = append(*f.gotThreadIDs, threadID)
	return f.fakeAdapter.LoadRecentMessages(threadID, limit)
}

// TestIMHistoryMessage字段 四字段对齐 Python dataclass。
func TestIMHistoryMessage字段(t *testing.T) {
	m := IMHistoryMessage{UserID: "ou-1", UserName: "张三", Content: "hi", TimestampMs: 1712345678000}
	assert.Equal(t, "ou-1", m.UserID)
	assert.Equal(t, "张三", m.UserName)
	assert.Equal(t, "hi", m.Content)
	assert.Equal(t, int64(1712345678000), m.TimestampMs)
}

// TestIMPlatformAdapter接口完整性 fake 实现 10 方法主接口即满足（编译期保证）。
func TestIMPlatformAdapter接口完整性(t *testing.T) {
	var _ IMPlatformAdapter = &fakeAdapter{}
	var _ IMPlatformAdapter = &fakeAdapterWithNameResolver{}
	// UseKeywordOverride 死字段保留（偏差①）：接口第 10 方法可调用
	a := &fakeAdapter{useKeywordOverride: true}
	assert.True(t, a.UseKeywordOverride())
}

// TestTruthy Python bool() 真值语义（JSON 域值）。
func TestTruthy(t *testing.T) {
	assert.False(t, truthy(nil))
	assert.False(t, truthy(false))
	assert.True(t, truthy(true))
	assert.False(t, truthy(""))
	assert.True(t, truthy("x"))
	assert.False(t, truthy(0.0))
	assert.True(t, truthy(0.1))
	assert.False(t, truthy(map[string]any{}))
	assert.True(t, truthy(map[string]any{"a": 1}))
	assert.False(t, truthy([]any{}))
	assert.True(t, truthy([]any{1}))
}

// TestPythonStr Python str() 显示语义（pythonOrStr 先例同款）。
func TestPythonStr(t *testing.T) {
	assert.Equal(t, "", pythonStr(nil))
	assert.Equal(t, "abc", pythonStr("abc"))
	assert.Equal(t, "True", pythonStr(true))
	assert.Equal(t, "False", pythonStr(false))
	assert.Equal(t, "123", pythonStr(123.0)) // 系数整除无小数
	assert.Equal(t, "123", pythonStr(123))
	assert.Equal(t, "-45", pythonStr(int64(-45)))
}

// TestOrStr Python str(a or b or ...) 链式语义。
func TestOrStr(t *testing.T) {
	assert.Equal(t, "", orStr())
	assert.Equal(t, "", orStr(nil, "", 0.0))
	assert.Equal(t, "first", orStr("first", "second"))
	assert.Equal(t, "second", orStr("", "second"))
	assert.Equal(t, "env", orStr(nil, "env", "OpenAI"))
	assert.Equal(t, "OpenAI", orStr(nil, "", "OpenAI"))
	assert.Equal(t, "123", orStr(123.0, "x"))
}

// TestStripOrStr 审查偏差①⑤修复：str(a or b or ...).strip() 链式语义。
func TestStripOrStr(t *testing.T) {
	assert.Equal(t, "", stripOrStr())
	assert.Equal(t, "", stripOrStr(nil, "", 0.0))
	assert.Equal(t, "", stripOrStr("   ", "\t\n"))
	assert.Equal(t, "ou-123", stripOrStr(" ou-123 "))
	assert.Equal(t, "12345", stripOrStr(12345))                    // 数字 ID truthy 转字符串（偏差⑤）
	assert.Equal(t, "second", stripOrStr("", " second ", "third")) // 取值后整体 strip
}

// TestPendingRepr 审查偏差⑧修复：对齐 Python 日志 %s 输出 dataclass repr。
func TestPendingRepr(t *testing.T) {
	assert.Equal(t, "None", pendingRepr(nil))
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_s_u", Mode: routing.ModeGroup,
		OriginSessionID: "s", Question: "问一下", TargetUserID: "u",
		Status: routing.StatusPending,
	}
	got := pendingRepr(pi)
	assert.Contains(t, got, `"interaction_id":"gpq_s_u"`)
	assert.Contains(t, got, `"question":"问一下"`)
	assert.Contains(t, got, `"status":"pending"`)
}

// TestIsDigits 审查偏差⑦修复：全 ASCII 数字判定。
func TestIsDigits(t *testing.T) {
	assert.True(t, isDigits("123"))
	assert.True(t, isDigits("0"))
	assert.False(t, isDigits(""))
	assert.False(t, isDigits("+123"))
	assert.False(t, isDigits("-123"))
	assert.False(t, isDigits("12.5"))
	assert.False(t, isDigits(" 123"))
	assert.False(t, isDigits("abc"))
}

// TestTruncateStr Python [:n] 切片近似（字节域，resolveInboundReferences 8000 截断先例）。
func TestTruncateStr(t *testing.T) {
	assert.Equal(t, "abc", truncateStr("abc", 10))
	assert.Equal(t, "abcde", truncateStr("abcdefgh", 5))
	assert.Equal(t, "", truncateStr("", 5))
}

// ──────────────────────────── 测试辅助：环境隔离 ────────────────────────────

// setupInteractionsEnv 隔离 interactions 目录 + USER.md 目录（11.8 测试先例复制）。
func setupInteractionsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return tmp
}

// newChatSendMsg 构造 chat.send 请求消息。
func newChatSendMsg(params string, metadata map[string]any) *schema.Message {
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend, json.RawMessage(params))
	msg.Metadata = metadata
	return msg
}

// writeTestConfig 写临时 config.yaml 并注册全局（hookscfg 先例）。
func writeTestConfig(t *testing.T, yamlContent string) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgFile, []byte(yamlContent), 0o644)
	cfg, err := config.New(cfgFile)
	require.NoError(t, err)
	hookscfg.ResetGlobalConfigForTest()
	hookscfg.RegisterConfig(cfg)
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
}

// TestSystemPromptTemplate 逐字对齐 Python SYSTEM_PROMPT_TEMPLATE（含两占位符）。
func TestSystemPromptTemplate(t *testing.T) {
	// Python im_inbound.py L21-56 原文 .strip() 后形态
	want := "你是{principal_name}的数字分身，活跃在即时通讯群聊中。当群里有其他用户发送与{principal_name}相关的消息时，你的任务是改写这条消息，使其更清晰、更完整，以便后续帮助{principal_name}生成恰当的回复。\n\n## 消息格式说明\n\n收到的消息格式为：`[时间戳] [发送者]: 消息内容`\n例如：`[2026-03-20 14:44:34] [某人]: {bot_mention_hint} @{principal_name} 111`\n\n## 判断是否需要处理（按优先级顺序）\n\n### 必须回复的情况（不能输出[无需处理]）：\n\n1. **消息中@了机器人**：如果消息内容中出现机器人 mention（例如 {bot_mention_hint}），无论发送者是谁，都必须回复\n2. **其他人@了用户本人**：如果发送者不是{principal_name}本人，且消息中@了{principal_name}，必须回复\n\n### 由模型判断的情况：\n\n如果以上条件都不满足，则根据以下标准判断：\n- 当前消息中提到了{principal_name}的名字\n- 当前消息是对{principal_name}之前发言的回复或延续\n- 当前消息是群聊中需要{principal_name}参与讨论的话题\n- 当前消息是否与群里其他人所发历史消息有关\n\n如果判断为无关，输出：[无需处理]\n\n## 改写原则\n\n1. 明确发送者意图：对方想表达什么？是提问、请求、讨论还是闲聊？\n2. 补充上下文：如果消息涉及历史对话（如\"那个文件\"、\"刚才说的\"），补充具体信息\n3. 明确回复期望：对方期望什么样的回应？是解答问题、确认信息、还是参与讨论？\n4. 保留原意：不要改变消息的核心意图\n\n## 输出要求\n\n直接输出改写后的消息，不要添加任何解释或额外内容。"
	if SYSTEM_PROMPT_TEMPLATE != want {
		t.Fatalf("SYSTEM_PROMPT_TEMPLATE 不逐字对齐:\n got=%q\nwant=%q", SYSTEM_PROMPT_TEMPLATE, want)
	}
}

// TestNewIMConversationProcessor_默认构造 无参路径：USER.md 路径取 DeepAgentUserMDPath、modelName 三级回退到 gpt-4o。
func TestNewIMConversationProcessor_默认构造(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	t.Setenv("MODEL_NAME", "")
	proc := NewIMConversationProcessor("", "")
	assert.Equal(t, workspace.DeepAgentUserMDPath(), proc.userProfilePath)
	assert.Equal(t, "gpt-4o", proc.modelName)
	assert.NotNil(t, proc.modelClientRaw)
	assert.Nil(t, proc.llm)
}

// TestNewIMConversationProcessor_显式注入 显式路径优先 + config 层回退。
func TestNewIMConversationProcessor_显式注入(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_name: cfg-model\n  model_client_config:\n    api_key: sk-1\n")
	proc := NewIMConversationProcessor("/tmp/x/USER.md", "explicit-model")
	assert.Equal(t, "/tmp/x/USER.md", proc.userProfilePath)
	assert.Equal(t, "explicit-model", proc.modelName)
	assert.Equal(t, "sk-1", proc.modelClientRaw["api_key"])

	proc2 := NewIMConversationProcessor("", "")
	assert.Equal(t, "cfg-model", proc2.modelName)
}

// TestExtractText params 三态（query 优先/content 回退/非 map 空）。
func TestExtractText(t *testing.T) {
	assert.Equal(t, "q1", extractText(newChatSendMsg(`{"query":"q1","content":"c1"}`, nil)))
	assert.Equal(t, "c1", extractText(newChatSendMsg(`{"content":"c1"}`, nil)))
	assert.Equal(t, "c1", extractText(newChatSendMsg(`{"query":"  ","content":"c1"}`, nil))) // query 空 strip 回退
	assert.Equal(t, "c1", extractText(newChatSendMsg(`{"query":123,"content":"c1"}`, nil)))  // query 非 str 跳过 → content 回退
	assert.Equal(t, "", extractText(newChatSendMsg(`{"content":123}`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`[1,2]`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`null`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`{"content":""}`, nil))) // content 空串（isinstance 通过返回 ""）
}

// TestResolveTimestampMs 三态：metadata int 直用 / 字符串全数字转换 / 回退 timestamp*1000。
// 审查偏差⑦修复后语义：±号/小数点字符串拒绝回退（对齐 Python isdigit）；
// 浮点仅整数值采用（JSON 整数），小数浮点回退（Python isinstance int False）。
func TestResolveTimestampMs(t *testing.T) {
	msg := newChatSendMsg(`{}`, nil)
	msg.Timestamp = 1712345678.5
	assert.Equal(t, int64(123), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": 123}))
	assert.Equal(t, int64(456), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "456"}))
	assert.Equal(t, int64(1712345678901), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": 1712345678901})) // JSON 整数 float64 直用
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, nil))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "abc"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "12.5"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "+123"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "-123"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": ""}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": 12.5})) // 小数浮点回退
	assert.Equal(t, int64(1), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": true}))
}

// TestHasImageContext msg_type 与 merged_msg_types 两路。
func TestHasImageContext(t *testing.T) {
	assert.True(t, hasImageContext(map[string]any{"msg_type": "image"}))
	assert.True(t, hasImageContext(map[string]any{"msg_type": " image "}))
	assert.True(t, hasImageContext(map[string]any{"merged_msg_types": []any{"text", "image"}}))
	assert.True(t, hasImageContext(map[string]any{"merged_msg_types": []any{" image "}}))
	assert.False(t, hasImageContext(map[string]any{"msg_type": "text"}))
	assert.False(t, hasImageContext(map[string]any{"merged_msg_types": []any{"text"}}))
	assert.False(t, hasImageContext(map[string]any{"merged_msg_types": "not-list"}))
	assert.False(t, hasImageContext(nil))
}

// TestShouldAlwaysReply_提及列表 条件1。
func TestShouldAlwaysReply_提及列表(t *testing.T) {
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	meta := map[string]any{"im_mentioned_user_ids": []any{"ou-other", " ou-me "}}
	assert.True(t, shouldAlwaysReply("hello", meta, "sender", "ou-me", "张三", nil))
	meta2 := map[string]any{"mentioned_open_ids": []any{"ou-me"}}
	assert.True(t, shouldAlwaysReply("hello", meta2, "sender", "ou-me", "张三", nil))
	assert.False(t, shouldAlwaysReply("hello", map[string]any{}, "sender", "ou-me", "张三", nil))
	// 审查偏差⑥修复：空数组是 Python or 链 falsy → 回退第二键
	meta3 := map[string]any{"im_mentioned_user_ids": []any{}, "mentioned_open_ids": []any{"ou-me"}}
	assert.True(t, shouldAlwaysReply("hello", meta3, "sender", "ou-me", "张三", nil))
}

// TestShouldAlwaysReply_botMentionToken 条件2a。
func TestShouldAlwaysReply_botMentionToken(t *testing.T) {
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	assert.True(t, shouldAlwaysReply("@bot 什么事", nil, "s", "p", "n", []string{"@bot"}))
	assert.False(t, shouldAlwaysReply("hello", nil, "s", "p", "n", []string{"@bot"}))
	assert.False(t, shouldAlwaysReply("hello", nil, "s", "p", "n", []string{""}))
}

// TestShouldAlwaysReply_configBotName 条件2b：config bot_name 两级读取。
// ⚠ 修正记录：bot_name 检查在 Python for 循环内（L305-320），bot_mentions 为空时
// 循环体不执行、bot_name 不检查——True 断言必须传非空且不匹配的 token 列表。
func TestShouldAlwaysReply_configBotName(t *testing.T) {
	writeTestConfig(t, "bot_name: 小助\n")
	assert.True(t, shouldAlwaysReply("小助 帮我订会议室", nil, "s", "p", "n", []string{"@feishu-bot"}))
	assert.False(t, shouldAlwaysReply("hello", nil, "s", "p", "n", []string{"@feishu-bot"}))

	writeTestConfig(t, "channels:\n  wecom:\n    bot_name: 企微小助\n")
	assert.True(t, shouldAlwaysReply("企微小助 在吗", nil, "s", "p", "n", []string{"@feishu-bot"}))
}

// TestShouldAlwaysReply_botName在循环内 条件2b 结构性：bot_mentions 空列表时 bot_name 不检查。
func TestShouldAlwaysReply_botName在循环内(t *testing.T) {
	writeTestConfig(t, "bot_name: 小助\n")
	assert.False(t, shouldAlwaysReply("小助 在吗", nil, "s", "p", "n", []string{}))
}

// TestShouldAlwaysReply_atPrincipalName 条件3。
func TestShouldAlwaysReply_atPrincipalName(t *testing.T) {
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	assert.True(t, shouldAlwaysReply("请问 @张三 什么时候到", nil, "sender-1", "principal-1", "张三", nil))
	assert.False(t, shouldAlwaysReply("@张三 自己说", nil, "principal-1", "principal-1", "张三", nil))
	assert.False(t, shouldAlwaysReply("@张三", nil, "", "principal-1", "张三", nil))
	assert.False(t, shouldAlwaysReply("张三好", nil, "s", "p", "张三", nil))
}

// TestBuildPrompt_四段拼接 历史非空 + 画像存在 + pending 有 + 当前消息。
func TestBuildPrompt_四段拼接(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)

	_ = os.MkdirAll(filepath.Dir(workspace.DeepAgentUserMDPath()), 0o755)
	_ = os.WriteFile(workspace.DeepAgentUserMDPath(), []byte("用户画像内容"), 0o644)

	adapter := &fakeAdapter{
		history: []IMHistoryMessage{
			{UserID: "u1", UserName: "张三", Content: "历史消息1", TimestampMs: 1712345678000},
			{UserID: "u2", UserName: "", Content: "历史消息2", TimestampMs: 1712345680000},
		},
		displayNameByID: map[string]string{"sender-1": "李四"},
	}
	proc := NewIMConversationProcessor("", "m")
	got := proc.buildPrompt("thread-1", "sender-1", "当前消息", 1712345700000, "张三", adapter, "追问上下文内容")

	ts1 := time.UnixMilli(1712345678000).Format("2006-01-02 15:04:05")
	ts2 := time.UnixMilli(1712345680000).Format("2006-01-02 15:04:05")
	tsCur := time.UnixMilli(1712345700000).Format("2006-01-02 15:04:05")
	want := strings.Join([]string{
		"=== 群聊历史消息 ===",
		"最近 2 条消息：\n",
		fmt.Sprintf("[%s] [张三]: 历史消息1", ts1),
		fmt.Sprintf("[%s] [未知用户]: 历史消息2", ts2),
		"",
		"=== 用户画像 ===",
		"用户画像内容",
		"",
		"=== 待回答的追问 ===",
		"追问上下文内容",
		"",
		"=== 当前消息 ===",
		fmt.Sprintf("[%s] [李四]: 当前消息", tsCur),
		"",
	}, "\n")
	assert.Equal(t, want, got)
}

// TestBuildPrompt_空历史无画像无pending 三回退形态。
func TestBuildPrompt_空历史无画像无pending(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	adapter := &fakeAdapter{}
	proc := NewIMConversationProcessor("", "m")
	got := proc.buildPrompt("", "sender-x", "hi", 1000, "n", adapter, "")
	assert.Contains(t, got, "暂无历史消息\n")
	assert.Contains(t, got, "暂无用户画像信息")
	assert.NotContains(t, got, "=== 待回答的追问 ===")
	assert.Contains(t, got, "[未知用户]: hi")
}

// TestLoadUserProfile 不存在空 + 存在返回内容。
func TestLoadUserProfile(t *testing.T) {
	setupInteractionsEnv(t)
	proc := NewIMConversationProcessor(filepath.Join(t.TempDir(), "USER.md"), "m")
	assert.Equal(t, "", proc.loadUserProfile())

	fp := filepath.Join(t.TempDir(), "USER.md")
	_ = os.WriteFile(fp, []byte("画像"), 0o644)
	proc2 := NewIMConversationProcessor(fp, "m")
	assert.Equal(t, "画像", proc2.loadUserProfile())

	proc3 := NewIMConversationProcessor(filepath.Join(t.TempDir(), "nonexist", "USER.md"), "m")
	assert.Equal(t, "", proc3.loadUserProfile())
}

// ──────────────────────────── 测试辅助：LLM fake 注入 ────────────────────────────

// fakeLLMClient 实现 model_clients.BaseModelClient（react_agent_test 先例），Invoke 可编程。
type fakeLLMClient struct {
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (f *fakeLLMClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return f.invokeFn(ctx, messages, opts...)
}
func (f *fakeLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (f *fakeLLMClient) SupportsKVCacheRelease() bool { return false }

// newFakeModel 构造注入 fake client 的 llm.Model（WithClient 先例）。
// WithVerifySSL(false)：默认 true 时 NewModelClientConfig 校验 ssl_cert 必填。
func newFakeModel(t *testing.T, invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "http://localhost:1",
		llmschema.WithVerifySSL(false))
	require.NoError(t, err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&fakeLLMClient{invokeFn: invokeFn}))
	require.NoError(t, err)
	return model
}

// newTestProcessor 常规测试 Processor（无 config → modelClientRaw 空 map）。
func newTestProcessor(t *testing.T) *IMConversationProcessor {
	t.Helper()
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	return NewIMConversationProcessor(filepath.Join(t.TempDir(), "USER.md"), "test-model")
}

// newTestAdapter 标准测试适配器。
func newTestAdapter() *fakeAdapter {
	return &fakeAdapter{
		principalUserID: "ou-principal",
		principalName:   "张三",
		platformName:    "飞书",
		replyUserIDKey:  "reply_feishu_open_id",
	}
}

// TestEnsureLLM_成功 config 注入 + /chat/completions 剥离 + 懒加载幂等。
func TestEnsureLLM_成功(t *testing.T) {
	setupInteractionsEnv(t)
	writeTestConfig(t, "react:\n  model_client_config:\n    api_key: sk-cfg\n    api_base: http://x/chat/completions\n    client_provider: OpenAI\n")
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")
	proc := NewIMConversationProcessor("", "m1")
	model := proc.ensureLLM()
	require.NotNil(t, model)
	assert.Same(t, model, proc.ensureLLM())
}

// TestEnsureLLM_环境变量回退 config 无值时 env 兜底。
func TestEnsureLLM_环境变量回退(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	t.Setenv("API_KEY", "sk-env")
	t.Setenv("API_BASE", "http://env/")
	proc := NewIMConversationProcessor("", "m1")
	assert.NotNil(t, proc.ensureLLM())
}

// TestEnsureLLM_失败保持nil 空 key/base 构造失败 → Warn → llm 保持 nil。
func TestEnsureLLM_失败保持nil(t *testing.T) {
	setupInteractionsEnv(t)
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")
	proc := NewIMConversationProcessor("", "m1")
	assert.Nil(t, proc.ensureLLM())
	assert.Nil(t, proc.llm)
}

// TestRewriteQuery_llmNil 返回空串。
func TestRewriteQuery_llmNil(t *testing.T) {
	setupInteractionsEnv(t)
	proc := newTestProcessor(t)
	assert.Equal(t, "", proc.rewriteQuery(context.Background(), "prompt", "张三", newTestAdapter()))
}

// TestRewriteQuery_成功 mock 返回 + system prompt 双占位符渲染断言。
func TestRewriteQuery_成功(t *testing.T) {
	setupInteractionsEnv(t)
	proc := newTestProcessor(t)
	var gotDicts []map[string]any
	proc.llm = newFakeModel(t, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		require.True(t, messages.IsDicts())
		gotDicts = messages.Dicts()
		return llmschema.NewAssistantMessage("  改写后的消息  "), nil
	})
	adapter := newTestAdapter()
	adapter.botMentions = []string{"@bot1", "@bot2"}
	got := proc.rewriteQuery(context.Background(), "user-prompt", "张三", adapter)
	assert.Equal(t, "改写后的消息", got)

	require.Len(t, gotDicts, 2)
	assert.Equal(t, "system", gotDicts[0]["role"])
	sysPrompt := gotDicts[0]["content"].(string)
	assert.Contains(t, sysPrompt, "你是张三的数字分身")
	assert.Contains(t, sysPrompt, "例如：`[2026-03-20 14:44:34] [某人]: @bot1 / @bot2 @张三 111`")
	assert.Equal(t, "user", gotDicts[1]["role"])
	assert.Equal(t, "user-prompt", gotDicts[1]["content"])
}

// TestRewriteQuery_空hint回退 bot_mentions 空列表 → "@机器人"。
func TestRewriteQuery_空hint回退(t *testing.T) {
	setupInteractionsEnv(t)
	proc := newTestProcessor(t)
	var sysPrompt string
	proc.llm = newFakeModel(t, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		sysPrompt = messages.Dicts()[0]["content"].(string)
		return nil, fmt.Errorf("invoke err")
	})
	proc.rewriteQuery(context.Background(), "p", "n", newTestAdapter())
	assert.Contains(t, sysPrompt, "@机器人")
}

// TestRewriteQuery_invokeErr Invoke 失败 → 空串。
func TestRewriteQuery_invokeErr(t *testing.T) {
	setupInteractionsEnv(t)
	proc := newTestProcessor(t)
	proc.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("boom")
	})
	assert.Equal(t, "", proc.rewriteQuery(context.Background(), "p", "n", newTestAdapter()))
}

// TestRewriteQuery_空响应空串 strip 后空 → 空串（偏差⑤ None 语义）。
func TestRewriteQuery_空响应空串(t *testing.T) {
	setupInteractionsEnv(t)
	proc := newTestProcessor(t)
	proc.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("   "), nil
	})
	assert.Equal(t, "", proc.rewriteQuery(context.Background(), "p", "n", newTestAdapter()))
}

// TestProcess_层1非chatsend reason=non-chat-send。
func TestProcess_层1非chatsend(t *testing.T) {
	proc := newTestProcessor(t)
	msg := schema.NewReqMessage("web", "s", schema.ReqMethodChatAnswer, json.RawMessage(`{}`))
	r := proc.Process(context.Background(), msg, newTestAdapter(), "")
	assert.True(t, r.ShouldForward)
	assert.Equal(t, "non-chat-send", r.Reason)
}

// TestProcess_层2空内容 reason=empty-content。
func TestProcess_层2空内容(t *testing.T) {
	proc := newTestProcessor(t)
	r := proc.Process(context.Background(), newChatSendMsg(`{}`, nil), newTestAdapter(), "")
	assert.Equal(t, "empty-content", r.Reason)
}

// TestProcess_层3控制消息 reason=control-message。
func TestProcess_层3控制消息(t *testing.T) {
	proc := newTestProcessor(t)
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"/new_session"}`, nil), newTestAdapter(), "")
	assert.Equal(t, "control-message", r.Reason)
}

// TestProcess_层4非群聊 reason=non-group-chat。
func TestProcess_层4非群聊(t *testing.T) {
	proc := newTestProcessor(t)
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, map[string]any{"im_chat_type": "p2p"}), newTestAdapter(), "")
	assert.Equal(t, "non-group-chat", r.Reason)
	r2 := proc.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, map[string]any{"chat_type": " Group "}), newTestAdapter(), "")
	assert.NotEqual(t, "non-group-chat", r2.Reason)
}

// TestProcess_层5必答 reason=always-reply + relevance patch。
func TestProcess_层5必答(t *testing.T) {
	proc := newTestProcessor(t)
	adapter := newTestAdapter()
	adapter.relevancePatch = map[string]any{"mentioned": true}
	meta := map[string]any{
		"chat_type":             "group",
		"im_mentioned_user_ids": []any{"ou-principal"},
	}
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"@张三 你好"}`, meta), adapter, "")
	assert.Equal(t, "always-reply", r.Reason)
	assert.Equal(t, true, r.MetadataPatch["relevant"])
	assert.Equal(t, true, r.MetadataPatch["mentioned"])
	assert.True(t, r.ShouldForward)
}

// TestProcess_层6改写失败 三态：llm nil / Invoke err / 空响应。
func TestProcess_层6改写失败(t *testing.T) {
	meta := map[string]any{"chat_type": "group"}
	proc := newTestProcessor(t)
	assert.Equal(t, "rewrite-failed",
		proc.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, meta), newTestAdapter(), "").Reason)

	proc2 := newTestProcessor(t)
	proc2.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("boom")
	})
	assert.Equal(t, "rewrite-failed",
		proc2.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, meta), newTestAdapter(), "").Reason)

	proc3 := newTestProcessor(t)
	proc3.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("  "), nil
	})
	assert.Equal(t, "rewrite-failed",
		proc3.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, meta), newTestAdapter(), "").Reason)
}

// TestProcess_层7b无关 should_forward=false + reason=irrelevant。
func TestProcess_层7b无关(t *testing.T) {
	proc := newTestProcessor(t)
	proc.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("[无需处理]"), nil
	})
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, map[string]any{"chat_type": "group"}), newTestAdapter(), "")
	assert.False(t, r.ShouldForward)
	assert.Equal(t, "irrelevant", r.Reason)
}

// TestProcess_层7a图片回退 [无需处理] + 图片上下文 → image-fallback。
func TestProcess_层7a图片回退(t *testing.T) {
	proc := newTestProcessor(t)
	proc.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("[无需处理]"), nil
	})
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"[图片]"}`, map[string]any{"chat_type": "group", "msg_type": "image"}), newTestAdapter(), "")
	assert.Equal(t, "image-fallback", r.Reason)
	assert.True(t, r.ShouldForward)
	r2 := proc.Process(context.Background(), newChatSendMsg(`{"query":"[图片]"}`,
		map[string]any{"chat_type": "group", "merged_msg_types": []any{"text", "image"}}), newTestAdapter(), "")
	assert.Equal(t, "image-fallback", r2.Reason)
}

// TestProcess_层7c改写成功 reason=rewritten。
func TestProcess_层7c改写成功(t *testing.T) {
	proc := newTestProcessor(t)
	proc.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("改写后的完整请求"), nil
	})
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"那个文件"}`, map[string]any{"chat_type": "group"}), newTestAdapter(), "pending-ctx")
	assert.Equal(t, "rewritten", r.Reason)
	assert.Equal(t, "改写后的完整请求", r.RewrittenContent)
	assert.Equal(t, true, r.MetadataPatch["relevant"])
	assert.True(t, r.ShouldForward)
}

// TestProcess_principal缺失 reason=missing-principal-user（Python L209-210）。
func TestProcess_principal缺失(t *testing.T) {
	proc := newTestProcessor(t)
	adapter := newTestAdapter()
	adapter.principalUserID = ""
	r := proc.Process(context.Background(), newChatSendMsg(`{"query":"hi"}`, map[string]any{"chat_type": "group"}), adapter, "")
	assert.Equal(t, "missing-principal-user", r.Reason)
	assert.True(t, r.ShouldForward)
}

// ──────────────────────────── Task 4: IMInboundPipeline ────────────────────────────

// newInboundWithAdapter 构造带 fakeAdapter 的入站管线。
func newInboundWithAdapter(t *testing.T, adapter IMPlatformAdapter) *IMInboundPipeline {
	t.Helper()
	hookscfg.ResetGlobalConfigForTest()
	t.Cleanup(hookscfg.ResetGlobalConfigForTest)
	p := NewIMInboundPipeline(nil, nil)
	if adapter != nil {
		p.RegisterAdapter("feishu_test", adapter)
	}
	return p
}

// newAvatarChatMsg 数字分身 chat.send 群聊消息。
func newAvatarChatMsg(params string, metadata map[string]any) *schema.Message {
	msg := newChatSendMsg(params, metadata)
	msg.GroupDigitalAvatar = true
	return msg
}

// TestIMInboundPipeline_RegisterUnregister 注册/注销/未知渠道容忍 + processor 默认构造。
func TestIMInboundPipeline_RegisterUnregister(t *testing.T) {
	p := NewIMInboundPipeline(nil, nil)
	p.RegisterAdapter("ch-1", newTestAdapter())
	p.UnregisterAdapter("ch-1")
	p.UnregisterAdapter("nonexist")
	assert.NotNil(t, p.processor)
}

// TestApply_非数字分身放行 步骤1。
func TestApply_非数字分身放行(t *testing.T) {
	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newChatSendMsg(`{"query":"hi"}`, nil)
	assert.True(t, p.Apply(context.Background(), msg))
}

// TestApply_无适配器放行 步骤2。
func TestApply_无适配器放行(t *testing.T) {
	p := newInboundWithAdapter(t, nil)
	msg := newAvatarChatMsg(`{"query":"hi"}`, nil)
	assert.True(t, p.Apply(context.Background(), msg))
}

// TestApply_resume分支 步骤3全链。
func TestApply_resume分支(t *testing.T) {
	setupInteractionsEnv(t)
	p := newInboundWithAdapter(t, newTestAdapter())
	pi := &routing.PendingInteraction{
		InteractionID:    "iact_feishu_test_abcd1234",
		Mode:             routing.ModeDM,
		OriginChannelID:  "feishu_test",
		OriginSessionID:  "sess-1",
		OriginContent:    "原始请求",
		OriginSenderName: "李四",
		OriginSenderID:   "ou-sender",
		Question:         "追问内容",
		TargetUserID:     "ou-principal",
		Status:           routing.StatusPending,
	}
	pi.Save()
	require.NotNil(t, routing.LoadPendingInteraction("iact_feishu_test_abcd1234"))

	msg := newAvatarChatMsg(`{"query":"principal 的回答","content":"principal 的回答"}`, map[string]any{
		"is_resume_message":         true,
		"dm_pending_interaction_id": "iact_feishu_test_abcd1234",
	})
	assert.True(t, p.Apply(context.Background(), msg))

	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	resumeContent := pi.BuildResumeContent("principal 的回答")
	assert.Equal(t, resumeContent, paramsMap["query"])
	assert.Equal(t, resumeContent, paramsMap["content"])
	assert.Equal(t, resumeContent, msg.Metadata["interaction_context"])
	assert.Nil(t, routing.LoadPendingInteraction("iact_feishu_test_abcd1234")) // Save 后 Remove
}

// TestApply_resume无pendingID is_resume_message 但无 id → 仅放行不改写。
func TestApply_resume无pendingID(t *testing.T) {
	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newAvatarChatMsg(`{"query":"hi"}`, map[string]any{"is_resume_message": true})
	assert.True(t, p.Apply(context.Background(), msg))
	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Equal(t, "hi", paramsMap["query"])
}

// TestApply_resume_params非map params JSON 数组 → answer 空 + params 重建 map。
func TestApply_resume_params非map(t *testing.T) {
	setupInteractionsEnv(t)
	p := newInboundWithAdapter(t, newTestAdapter())
	pi := &routing.PendingInteraction{
		InteractionID: "iact_x_1", Mode: routing.ModeDM, OriginChannelID: "c",
		OriginSessionID: "s", OriginContent: "oc", OriginSenderName: "n",
		OriginSenderID: "o", Question: "q", TargetUserID: "t", Status: routing.StatusPending,
	}
	pi.Save()
	msg := newAvatarChatMsg(`[1,2]`, map[string]any{
		"is_resume_message": true, "dm_pending_interaction_id": "iact_x_1",
	})
	assert.True(t, p.Apply(context.Background(), msg))
	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Contains(t, paramsMap["query"], "[任务恢复]")
}

// TestPeekPending 四段模板逐字断言（含名空回退双处）。
func TestPeekPending(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-sender", Mode: routing.ModeGroup,
		OriginChannelID: "c", OriginSessionID: "sess-1",
		OriginContent: "原始请求内容", OriginSenderName: "王五", OriginSenderID: "ou-sender",
		Question: "你的追问内容", TargetUserID: "ou-sender", TargetUserName: "李四",
		Status: routing.StatusPending,
	}
	pi.Save()
	msg := newAvatarChatMsg(`{}`, map[string]any{"im_sender_user_id": "ou-sender"})
	msg.SessionID = "sess-1"

	got := peekPending(msg)
	want := "【追问上下文】你之前在处理以下任务时向 李四 追问了信息：\n" +
		"- 原始请求：原始请求内容\n" +
		"- 你的追问：你的追问内容\n" +
		"现在 李四 已回复，请综合「原始请求」和「用户回复」中的所有信息继续完成任务，" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问。" +
		"不要与群聊历史中的其他任务混淆。"
	assert.Equal(t, want, got)

	pi2 := &routing.PendingInteraction{
		InteractionID: "gpq_sess-2_ou-s2", Mode: routing.ModeGroup,
		OriginChannelID: "c", OriginSessionID: "sess-2",
		OriginContent: "oc", OriginSenderName: "n", OriginSenderID: "o",
		Question: "q", TargetUserID: "ou-s2", Status: routing.StatusPending,
	}
	pi2.Save()
	msg2 := newAvatarChatMsg(`{}`, map[string]any{"open_id": "ou-s2"})
	msg2.SessionID = "sess-2"
	got2 := peekPending(msg2)
	assert.Contains(t, got2, "向 用户 追问了信息")
	assert.Contains(t, got2, "现在 用户 已回复")

	msg3 := newAvatarChatMsg(`{}`, nil)
	msg3.SessionID = ""
	assert.Equal(t, "", peekPending(msg3))
}

// TestApply_完整改写链 步骤4/7/8/9/11 全链断言。
func TestApply_完整改写链(t *testing.T) {
	setupInteractionsEnv(t)
	adapter := newTestAdapter()
	adapter.relevancePatch = map[string]any{"relevance": "high"}
	p := newInboundWithAdapter(t, adapter)
	p.processor.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("改写后的请求"), nil
	})

	msg := newAvatarChatMsg(`{"query":"原始问题","content":"原始问题"}`, map[string]any{
		"chat_type":         "group",
		"im_sender_user_id": "ou-sender",
	})
	assert.True(t, p.Apply(context.Background(), msg))

	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Equal(t, "改写后的请求", paramsMap["query"])
	assert.Equal(t, "改写后的请求", paramsMap["content"])
	assert.Equal(t, "原始问题", msg.Metadata["avatar_original_query"])
	assert.Equal(t, "high", msg.Metadata["relevance"])
	assert.Equal(t, true, msg.Metadata["avatar_mode"])
	assert.Equal(t, "飞书", msg.Metadata["avatar_channel_type"])
	assert.Equal(t, "张三", msg.Metadata["avatar_principal_name"])
	assert.Equal(t, "ou-principal", msg.Metadata["principal_user_id"])
}

// TestApply_irrelevant丢弃 should_forward=false。
func TestApply_irrelevant丢弃(t *testing.T) {
	setupInteractionsEnv(t)
	p := newInboundWithAdapter(t, newTestAdapter())
	p.processor.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("[无需处理]"), nil
	})
	msg := newAvatarChatMsg(`{"query":"无关消息"}`, map[string]any{"chat_type": "group"})
	assert.False(t, p.Apply(context.Background(), msg))
}

// TestApply_pending命中步骤10 二次 FindGroupPending → resume 拼接双写。
func TestApply_pending命中步骤10(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-sender", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "原始请求", OriginSenderName: "王五", OriginSenderID: "ou-sender",
		Question: "需要哪个楼层？", TargetUserID: "ou-sender", TargetUserName: "李四",
		Status: routing.StatusPending,
	}
	pi.Save()

	p := newInboundWithAdapter(t, newTestAdapter())
	p.processor.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("改写后的回答上下文"), nil
	})
	msg := newAvatarChatMsg(`{"query":"3楼"}`, map[string]any{
		"chat_type":         "group",
		"im_sender_user_id": "ou-sender",
	})
	msg.SessionID = "sess-1"
	assert.True(t, p.Apply(context.Background(), msg))

	assert.Contains(t, msg.Metadata["interaction_context"], "【追问上下文】")
	assert.Equal(t, "ou-sender", msg.Metadata["interaction_answered_user_id"])
	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Equal(t, pi.BuildResumeContent("改写后的回答上下文"), paramsMap["query"])
}

// TestApply_avatarDetail键省略 principal 名/ID 空时条件键省略。
func TestApply_avatarDetail键省略(t *testing.T) {
	setupInteractionsEnv(t)
	adapter := newTestAdapter()
	adapter.principalName = ""
	adapter.principalUserID = ""
	p := newInboundWithAdapter(t, adapter)
	msg := newAvatarChatMsg(`{"query":"hi"}`, map[string]any{"chat_type": "group"})
	assert.True(t, p.Apply(context.Background(), msg))
	assert.Equal(t, true, msg.Metadata["avatar_mode"])
	assert.NotContains(t, msg.Metadata, "avatar_principal_name")
	assert.NotContains(t, msg.Metadata, "principal_user_id")
}

// TestApply_非群聊不进步骤11 reason=non-group-chat → avatar_detail 不写。
func TestApply_非群聊不进步骤11(t *testing.T) {
	setupInteractionsEnv(t)
	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newAvatarChatMsg(`{"query":"hi"}`, map[string]any{"chat_type": "p2p"})
	assert.True(t, p.Apply(context.Background(), msg))
	assert.NotContains(t, msg.Metadata, "avatar_mode")
}

// TestApply_尾部日志不panic 尾部九字段（空 MetadataPatch 场景）。
func TestApply_尾部日志不panic(t *testing.T) {
	setupInteractionsEnv(t)
	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newAvatarChatMsg(`{}`, map[string]any{"chat_type": "group"})
	assert.NotPanics(t, func() {
		p.Apply(context.Background(), msg)
	})
}

// ──────────────────────────── 审查偏差修复回归（2026-11-04） ────────────────────────────

// TestProcess_元数据带空白_审查偏差① 层4 threadID 取值 strip：feishu_chat_id
// 带空白填充时 LoadRecentMessages 收到归一后的 threadID（" chat-1 " → "chat-1"）。
func TestProcess_元数据带空白_审查偏差修复(t *testing.T) {
	setupInteractionsEnv(t)
	adapter := newTestAdapter()
	var gotThreadIDs []string
	adapter.history = nil
	p := NewIMInboundPipeline(nil, nil)
	p.RegisterAdapter("feishu_test", &promptCaptureAdapter{fakeAdapter: *adapter, gotThreadIDs: &gotThreadIDs})
	p.processor.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("改写内容"), nil
	})
	msg := newAvatarChatMsg(`{"query":"hi"}`, map[string]any{
		"chat_type":      "group",
		"feishu_chat_id": " chat-1 ",
	})
	assert.True(t, p.Apply(context.Background(), msg))
	assert.Equal(t, []string{"chat-1"}, gotThreadIDs)
}

// TestApply_pending命中带空白元数据 偏差①端到端：sender 三键带空白 → pending 命中
// （TrimSpace 后查 FindGroupPending）+ interaction_answered_user_id 落盘干净值。
func TestApply_pending命中带空白元数据(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "gpq_sess-1_ou-sender", Mode: routing.ModeGroup,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "原始请求", OriginSenderName: "王五", OriginSenderID: "ou-sender",
		Question: "需要哪个楼层？", TargetUserID: "ou-sender", TargetUserName: "李四",
		Status: routing.StatusPending,
	}
	pi.Save()

	p := newInboundWithAdapter(t, newTestAdapter())
	p.processor.llm = newFakeModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("改写后的回答上下文"), nil
	})
	msg := newAvatarChatMsg(`{"query":"3楼"}`, map[string]any{
		"chat_type":         "group",
		"im_sender_user_id": " ou-sender ",
	})
	msg.SessionID = "sess-1"
	assert.True(t, p.Apply(context.Background(), msg))

	// 带空白填充的 sender 经 stripOrStr 归一后命中 pending → interaction_context 落盘
	assert.Contains(t, msg.Metadata["interaction_context"], "【追问上下文】")
	assert.Equal(t, "ou-sender", msg.Metadata["interaction_answered_user_id"])
	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Equal(t, pi.BuildResumeContent("改写后的回答上下文"), paramsMap["query"])
}

// TestApply_resume_带空白pendingID_审查偏差① resume 分支 dm_pending_interaction_id
// 带空白填充的字符串 ID：stripOrStr 归一后照常加载并 resume。
func TestApply_resume_带空白pendingID_审查偏差修复(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "iact_feishu_test_12345678", Mode: routing.ModeDM,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "原始请求", OriginSenderName: "n", OriginSenderID: "o",
		Question: "补充材料？", TargetUserID: "u",
		Status: routing.StatusPending,
	}
	pi.Save()
	require.NotNil(t, routing.LoadPendingInteraction("iact_feishu_test_12345678"))

	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newAvatarChatMsg(`{"query":"材料在这"}`, map[string]any{
		"is_resume_message":         true,
		"dm_pending_interaction_id": " iact_feishu_test_12345678 ", // 带空白填充
		"im_sender_user_id":         " ou-sender ",
	})
	assert.True(t, p.Apply(context.Background(), msg))

	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	resumeContent := pi.BuildResumeContent("材料在这")
	assert.Equal(t, resumeContent, paramsMap["query"])
	assert.Equal(t, resumeContent, msg.Metadata["interaction_context"])
	assert.Nil(t, routing.LoadPendingInteraction("iact_feishu_test_12345678")) // Save 后 Remove
}

// TestApply_resume_数字pendingID不匹配_审查偏差⑤ 数字 ID（truthy 非字符串）经
// pythonStr 转 "12345678" 后查 Load → nil（ID 实际带 iact_ 前缀）→ 仅放行不改写，
// 对齐 Python str(12345678) 加载失败路径。
func TestApply_resume_数字pendingID不匹配_审查偏差修复(t *testing.T) {
	setupInteractionsEnv(t)
	pi := &routing.PendingInteraction{
		InteractionID: "iact_ch_12345678", Mode: routing.ModeDM,
		OriginChannelID: "feishu_test", OriginSessionID: "sess-1",
		OriginContent: "原始请求", OriginSenderName: "n", OriginSenderID: "o",
		Question: "补充材料？", TargetUserID: "u",
		Status: routing.StatusPending,
	}
	pi.Save()

	p := newInboundWithAdapter(t, newTestAdapter())
	msg := newAvatarChatMsg(`{"query":"材料在这"}`, map[string]any{
		"is_resume_message":         true,
		"dm_pending_interaction_id": 12345678, // 数字：str → "12345678" ≠ 保存 ID → Load nil
	})
	assert.True(t, p.Apply(context.Background(), msg))

	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &paramsMap))
	assert.Equal(t, "材料在这", paramsMap["query"])                           // 未被 resume 改写
	assert.Nil(t, msg.Metadata["interaction_context"])                    // 未落 interaction_context
	require.NotNil(t, routing.LoadPendingInteraction("iact_ch_12345678")) // pending 未被消费
}
