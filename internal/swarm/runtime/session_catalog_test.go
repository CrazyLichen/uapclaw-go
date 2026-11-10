package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 输入构造 ────────────────────────────

// TestNewSessionListInput_默认值 默认 limit=20、offset=0、search 为空。
// Python: session_catalog.py:43-45 limit=_DEFAULT_LIMIT, offset=0, search=""
func TestNewSessionListInput_默认值(t *testing.T) {
	input := NewSessionListInput("web")
	require.Equal(t, "web", input.ChannelID)
	require.Equal(t, DefaultSessionLimit, input.Limit)
	require.Equal(t, 0, input.Offset)
	require.Equal(t, "", input.Search)
}

// TestSessionCatalog常量_与上游一致 默认 20、上限 200。
// Python: session_catalog.py:17-18 _DEFAULT_LIMIT / _MAX_LIMIT
func TestSessionCatalog常量_与上游一致(t *testing.T) {
	require.Equal(t, 20, DefaultSessionLimit)
	require.Equal(t, 200, MaxSessionLimit)
}

// ──────────────────────────── 渠道与 ID 归一化 ────────────────────────────

// TestNormalizeChannelID_归一化 去空白小写；空则报错。
// Python: session_catalog.py:98-102 _normalize_channel_id
func TestNormalizeChannelID_归一化(t *testing.T) {
	got, err := normalizeChannelID("  WEB  ")
	require.NoError(t, err)
	require.Equal(t, "web", got)

	for _, value := range []string{"", "   "} {
		_, err := normalizeChannelID(value)
		require.ErrorIs(t, err, ErrSessionChannelRequired, "value=%q", value)
	}
}

// TestNormalizeSessionID_归一化 去空白；空报 required；非法报 invalid。
// Python: session_catalog.py:105-114 _normalize_session_id
func TestNormalizeSessionID_归一化(t *testing.T) {
	got, err := normalizeSessionID("  sess-1  ")
	require.NoError(t, err)
	require.Equal(t, "sess-1", got)

	_, err = normalizeSessionID("   ")
	require.ErrorIs(t, err, ErrSessionIDRequired)

	// 路径穿越与非法字符必须被拒
	for _, invalid := range []string{"../etc/passwd", "a/b", "a\\b", ".."} {
		_, err := normalizeSessionID(invalid)
		require.ErrorIs(t, err, ErrSessionIDInvalid, "value=%q", invalid)
	}
}

// ──────────────────────────── 分页校验 ────────────────────────────

// TestValidatePage_合法范围 1..200 合法，offset>=0 合法。
// Python: session_catalog.py:123-135 _validate_page
func TestValidatePage_合法范围(t *testing.T) {
	for _, limit := range []int{1, 20, 200} {
		gotLimit, gotOffset, err := validatePage(limit, 0)
		require.NoError(t, err, "limit=%d", limit)
		require.Equal(t, limit, gotLimit)
		require.Equal(t, 0, gotOffset)
	}
}

// TestValidatePage_越界报错 limit 为 0、201、负数均报错。
// Python: session_catalog.py:124-129
func TestValidatePage_越界报错(t *testing.T) {
	for _, limit := range []int{0, -1, 201, 1000} {
		_, _, err := validatePage(limit, 0)
		require.ErrorIs(t, err, ErrSessionLimitInvalid, "limit=%d", limit)
	}
}

// TestValidatePage_负偏移报错 offset 为负报错。
// Python: session_catalog.py:130-134
func TestValidatePage_负偏移报错(t *testing.T) {
	_, _, err := validatePage(20, -1)
	require.ErrorIs(t, err, ErrSessionOffsetInvalid)
}

// ──────────────────────────── 安全转换 ────────────────────────────

// TestSafeFloat_各类输入 bool 与非数值归零，数值正常。
// Python: session_catalog.py:138-144 _safe_float
func TestSafeFloat_各类输入(t *testing.T) {
	cases := []struct {
		value any
		want  float64
	}{
		{true, 0}, {false, 0},
		{1.5, 1.5}, {2, 2.0}, {int64(3), 3.0},
		{"4.5", 4.5}, {"not-a-number", 0},
		{nil, 0}, {[]string{"x"}, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.want, safeFloat(c.value), "value=%v", c.value)
	}
}

// TestSafeInt_各类输入 bool 与非数值归零，数值正常。
// Python: session_catalog.py:147-153 _safe_int
func TestSafeInt_各类输入(t *testing.T) {
	cases := []struct {
		value any
		want  int
	}{
		{true, 0}, {false, 0},
		{5, 5}, {int64(6), 6}, {7.9, 7},
		{"8", 8}, {"bad", 0},
		{nil, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.want, safeInt(c.value), "value=%v", c.value)
	}
}

// ──────────────────────────── 模式投影 ────────────────────────────

// TestSingleAgentMode_单Agent模式 四个权威模式均可，别名折叠后也可。
// Python: session_catalog.py:160-163 _single_agent_mode
func TestSingleAgentMode_单Agent模式(t *testing.T) {
	for _, mode := range []string{
		"agent.work.normal", "agent.work.plan", "agent.code.normal", "agent.code.plan",
	} {
		require.Equal(t, mode, singleAgentMode(mode))
	}
	// 别名折叠
	require.Equal(t, "agent.work.normal", singleAgentMode("agent"))
	require.Equal(t, "agent.code.plan", singleAgentMode("code.plan"))
}

// TestSingleAgentMode_团队模式返回空 团队模式不是单 Agent 模式。
// Python: session_catalog.py:163 is_single_agent_mode(mode) 判定
func TestSingleAgentMode_团队模式返回空(t *testing.T) {
	for _, mode := range []string{"team", "team.work.normal", "team.code.plan"} {
		require.Equal(t, "", singleAgentMode(mode), "mode=%q", mode)
	}
}

// TestSingleAgentMode_非法输入返回空 非字符串、空串、未知模式。
func TestSingleAgentMode_非法输入返回空(t *testing.T) {
	require.Equal(t, "", singleAgentMode(nil))
	require.Equal(t, "", singleAgentMode(42))
	require.Equal(t, "", singleAgentMode(""))
	require.Equal(t, "", singleAgentMode("bogus.mode"))
}

// ──────────────────────────── 会话投影 ────────────────────────────

// TestProjectSession_渠道不匹配返回nil 元数据渠道与请求渠道不同则过滤。
// Python: session_catalog.py:172-174
func TestProjectSession_渠道不匹配返回nil(t *testing.T) {
	metadata := map[string]any{
		"session_id": "s1", "channel_id": "cli", "mode": "agent.work.normal",
	}
	require.Nil(t, projectSession(metadata, "web", false))
}

// TestProjectSession_单Agent会话正常投影 字段逐一映射。
// Python: session_catalog.py:185-197
func TestProjectSession_单Agent会话正常投影(t *testing.T) {
	metadata := map[string]any{
		"session_id": "s1", "channel_id": "WEB", "title": "标题",
		"mode": "agent.code.plan", "work_mode": "CODE",
		"project_id": "p1", "project_dir": "/tmp/p1", "model": "m1",
		"created_at": 100.5, "last_message_at": 200.5, "message_count": 7,
	}
	got := projectSession(metadata, "web", false)
	require.NotNil(t, got)
	require.Equal(t, "s1", got.SessionID)
	require.Equal(t, "web", got.ChannelID, "渠道小写化")
	require.Equal(t, "标题", got.Title)
	require.Equal(t, "agent.code.plan", got.Mode)
	require.Equal(t, "code", got.WorkMode, "work_mode 小写化")
	require.Equal(t, "p1", got.ProjectID)
	require.Equal(t, "/tmp/p1", got.ProjectDir)
	require.Equal(t, "m1", got.Model)
	require.Equal(t, 100.5, got.CreatedAt)
	require.Equal(t, 200.5, got.LastMessageAt)
	require.Equal(t, 7, got.MessageCount)
}

// TestProjectSession_团队模式默认过滤 include_team=false 时团队会话被过滤。
// Python: session_catalog.py:176-181（单 Agent 范围是默认）
func TestProjectSession_团队模式默认过滤(t *testing.T) {
	metadata := map[string]any{
		"session_id": "s1", "channel_id": "web", "mode": "team.work.normal",
	}
	require.Nil(t, projectSession(metadata, "web", false))
}

// TestProjectSession_团队模式按需包含 include_team=true 时团队会话保留。
// Python: session_catalog.py:176-179
func TestProjectSession_团队模式按需包含(t *testing.T) {
	metadata := map[string]any{
		"session_id": "s1", "channel_id": "web", "mode": "team.work.normal",
	}
	got := projectSession(metadata, "web", true)
	require.NotNil(t, got)
	require.Equal(t, "team.work.normal", got.Mode)
}

// TestProjectSession_非法会话ID返回nil ID 不合法则过滤。
// Python: session_catalog.py:182-184
func TestProjectSession_非法会话ID返回nil(t *testing.T) {
	metadata := map[string]any{
		"session_id": "../escape", "channel_id": "web", "mode": "agent.work.normal",
	}
	require.Nil(t, projectSession(metadata, "web", false))
}

// TestProjectSession_缺字段用零值 标题/时间/计数缺省为零值。
// Python: session_catalog.py:188-196 _safe_text/_safe_float/_safe_int 缺省
func TestProjectSession_缺字段用零值(t *testing.T) {
	metadata := map[string]any{
		"session_id": "s1", "channel_id": "web", "mode": "agent.work.normal",
	}
	got := projectSession(metadata, "web", false)
	require.NotNil(t, got)
	require.Equal(t, "", got.Title)
	require.Equal(t, float64(0), got.CreatedAt)
	require.Equal(t, 0, got.MessageCount)
}

// ──────────────────────────── 序列化 ────────────────────────────

// TestSessionSummaryToDict_十一字段 投影含上游全部字段。
// Python: session_catalog.py:64-77 to_dict
func TestSessionSummaryToDict_十一字段(t *testing.T) {
	got := SessionSummaryToDict(SessionSummary{
		SessionID: "s1", ChannelID: "web", Title: "t", Mode: "agent.work.normal",
		WorkMode: "work", ProjectID: "p", ProjectDir: "d", Model: "m",
		CreatedAt: 1.0, LastMessageAt: 2.0, MessageCount: 3,
	})
	require.Len(t, got, 11)
	for _, key := range []string{
		"session_id", "channel_id", "title", "mode", "work_mode",
		"project_id", "project_dir", "model",
		"created_at", "last_message_at", "message_count",
	} {
		require.Contains(t, got, key)
	}
}

// TestSessionListResultToDict_四字段 含 sessions/total/limit/offset。
// Python: session_catalog.py:89-95 to_dict
func TestSessionListResultToDict_四字段(t *testing.T) {
	result := SessionListResult{
		Sessions: []SessionSummary{{SessionID: "s1"}},
		Total:    1, Limit: 20, Offset: 0,
	}
	got := SessionListResultToDict(result)
	require.Len(t, got, 4)
	require.Equal(t, 1, got["total"])
	require.Equal(t, 20, got["limit"])
	require.Equal(t, 0, got["offset"])
	sessions, ok := got["sessions"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, sessions, 1)
}

// ──────────────────────────── 读路径 ────────────────────────────

// TestGetSession_空会话ID报错 归一化阶段拒绝。
// Python: session_catalog.py:229
func TestGetSession_空会话ID报错(t *testing.T) {
	_, err := GetSession(SessionGetInput{ChannelID: "web", SessionID: ""})
	require.ErrorIs(t, err, ErrSessionIDRequired)
}

// TestGetSession_空渠道报错 归一化阶段拒绝。
// Python: session_catalog.py:228
func TestGetSession_空渠道报错(t *testing.T) {
	_, err := GetSession(SessionGetInput{ChannelID: "", SessionID: "s1"})
	require.ErrorIs(t, err, ErrSessionChannelRequired)
}

// TestGetSession_不存在返回nil 读不到元数据时返回 nil 而非错误。
// Python: session_catalog.py:238-239
func TestGetSession_不存在返回nil(t *testing.T) {
	got, err := GetSession(SessionGetInput{
		ChannelID: "web", SessionID: "definitely-not-exist-session-20-1",
	})
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestListSessions_分页参数报错 limit 越界直接报错。
// Python: session_catalog.py:252
func TestListSessions_分页参数报错(t *testing.T) {
	input := SessionListInput{ChannelID: "web", Limit: 0, Offset: 0}
	_, err := ListSessions(input, false, true)
	require.ErrorIs(t, err, ErrSessionLimitInvalid)
}

// TestListSessions_搜索串过长报错 超过 200 字符报错。
// Python: session_catalog.py:253-254
func TestListSessions_搜索串过长报错(t *testing.T) {
	long := make([]rune, maxSessionSearchLength+1)
	for index := range long {
		long[index] = 'a'
	}
	input := SessionListInput{
		ChannelID: "web", Limit: 20, Offset: 0, Search: string(long),
	}
	_, err := ListSessions(input, false, true)
	require.ErrorIs(t, err, ErrSessionSearchInvalid)
}

// TestListSessions_空渠道报错 渠道缺失报错。
func TestListSessions_空渠道报错(t *testing.T) {
	input := SessionListInput{ChannelID: "", Limit: 20}
	_, err := ListSessions(input, false, true)
	require.ErrorIs(t, err, ErrSessionChannelRequired)
}

// TestListSessions_返回结构一致 即使无匹配也返回 limit/offset 与 total。
// Python: session_catalog.py:299-304
func TestListSessions_返回结构一致(t *testing.T) {
	input := SessionListInput{ChannelID: "no-such-channel-20-1", Limit: 20, Offset: 0}
	got, err := ListSessions(input, false, true)
	require.NoError(t, err)
	require.Equal(t, 0, got.Total)
	require.Equal(t, 20, got.Limit)
	require.Equal(t, 0, got.Offset)
	require.Empty(t, got.Sessions)
}
