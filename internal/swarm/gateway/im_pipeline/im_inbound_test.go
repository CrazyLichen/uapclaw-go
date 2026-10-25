package im_pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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
	assert.Equal(t, "c1", extractText(newChatSendMsg(`{"query":123,"content":"c1"}`, nil))) // query 非 str 跳过 → content 回退
	assert.Equal(t, "", extractText(newChatSendMsg(`{"content":123}`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`[1,2]`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`null`, nil)))
	assert.Equal(t, "", extractText(newChatSendMsg(`{"content":""}`, nil))) // content 空串（isinstance 通过返回 ""）
}

// TestResolveTimestampMs 三态：metadata int 直用 / 字符串全数字转换 / 回退 timestamp*1000。
func TestResolveTimestampMs(t *testing.T) {
	msg := newChatSendMsg(`{}`, nil)
	msg.Timestamp = 1712345678.5
	assert.Equal(t, int64(123), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": 123}))
	assert.Equal(t, int64(456), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "456"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, nil))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "abc"}))
	assert.Equal(t, int64(1712345678500), resolveTimestampMs(msg.Timestamp, map[string]any{"timestamp_ms": "12.5"}))
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
