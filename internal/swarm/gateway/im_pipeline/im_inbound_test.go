package im_pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
