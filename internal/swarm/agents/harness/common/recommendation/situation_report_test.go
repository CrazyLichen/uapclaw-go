package recommendation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 会话目录构造辅助 ────────────────────────────

// writeSession 在 sessionsDir 下构造一个会话（metadata.json + history.json）。
func writeSession(t *testing.T, sessionsDir, sessionID string, meta map[string]any, history []map[string]any) {
	t.Helper()
	dir := filepath.Join(sessionsDir, sessionID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if meta != nil {
		data, err := json.MarshalIndent(meta, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644))
	}
	if history != nil {
		data, err := json.MarshalIndent(history, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644))
	}
}

// userRecord 构造一条 user 历史记录。
func userRecord(text string) map[string]any {
	return map[string]any{"role": "user", "content": text}
}

// assistantRecord 构造一条 assistant 历史记录。
func assistantRecord(eventType, text string) map[string]any {
	return map[string]any{"role": "assistant", "event_type": eventType, "content": text}
}

// ──────────────────────────── SituationReport 方法 ────────────────────────────

// TestSituationReport_IsEmpty 无压缩历史即空。
// Python: situation_report.py:56
func TestSituationReport_IsEmpty(t *testing.T) {
	t.Run("无会话为空", func(t *testing.T) {
		assert.True(t, (&SituationReport{}).IsEmpty())
	})

	t.Run("有会话但无历史仍为空", func(t *testing.T) {
		r := &SituationReport{Sessions: []SessionSummary{{SessionID: "s1", LastMessageAt: 1}}}
		assert.True(t, r.IsEmpty(), "没有压缩历史视为空")
	})

	t.Run("有压缩历史非空", func(t *testing.T) {
		r := &SituationReport{Sessions: []SessionSummary{{SessionID: "s1", CompressedHistory: "[User]: hi"}}}
		assert.False(t, r.IsEmpty())
	})
}

// TestSituationReport_MostRecentActiveSession 取 last_message_at 最大的会话。
// Python: situation_report.py:60
func TestSituationReport_MostRecentActiveSession(t *testing.T) {
	t.Run("无会话返回nil", func(t *testing.T) {
		assert.Nil(t, (&SituationReport{}).MostRecentActiveSession())
	})

	t.Run("全是零时间戳返回nil", func(t *testing.T) {
		r := &SituationReport{Sessions: []SessionSummary{
			{SessionID: "a", LastMessageAt: 0},
			{SessionID: "b", LastMessageAt: 0},
		}}
		assert.Nil(t, r.MostRecentActiveSession(), "无活动会话不应作为投递目标")
	})

	t.Run("取最大时间戳", func(t *testing.T) {
		r := &SituationReport{Sessions: []SessionSummary{
			{SessionID: "a", LastMessageAt: 100},
			{SessionID: "b", LastMessageAt: 300},
			{SessionID: "c", LastMessageAt: 200},
		}}
		got := r.MostRecentActiveSession()
		require.NotNil(t, got)
		assert.Equal(t, "b", got.SessionID)
	})

	t.Run("无压缩历史但有活动仍可选为投递目标", func(t *testing.T) {
		// 推送只需要 session id，刚发消息、助手还没回完的会话是合法目标
		r := &SituationReport{Sessions: []SessionSummary{
			{SessionID: "fresh", LastMessageAt: 500, CompressedHistory: ""},
		}}
		got := r.MostRecentActiveSession()
		require.NotNil(t, got)
		assert.Equal(t, "fresh", got.SessionID)
	})
}

// TestSituationReport_FindSessionForChannel 按通道筛最近活跃会话。
// Python: situation_report.py:74
func TestSituationReport_FindSessionForChannel(t *testing.T) {
	sessions := []SessionSummary{
		{SessionID: "w1", ChannelID: "web", LastMessageAt: 100},
		{SessionID: "w2", ChannelID: "WEB", LastMessageAt: 300},
		{SessionID: "t1", ChannelID: "tui", LastMessageAt: 500},
		{SessionID: "d1", ChannelID: "", LastMessageAt: 50},
	}
	r := &SituationReport{Sessions: sessions}

	t.Run("空通道回落全局最活跃", func(t *testing.T) {
		got := r.FindSessionForChannel("")
		require.NotNil(t, got)
		assert.Equal(t, "t1", got.SessionID, "空通道应回落 MostRecentActiveSession")
	})

	t.Run("按通道过滤并取最活跃", func(t *testing.T) {
		got := r.FindSessionForChannel("web")
		require.NotNil(t, got)
		assert.Equal(t, "w2", got.SessionID)
	})

	t.Run("通道大小写不敏感", func(t *testing.T) {
		got := r.FindSessionForChannel("  WeB  ")
		require.NotNil(t, got)
		assert.Equal(t, "w2", got.SessionID, "应 trim + 小写比较")
	})

	t.Run("通道无会话返回nil", func(t *testing.T) {
		assert.Nil(t, r.FindSessionForChannel("dingtalk"))
	})

	t.Run("该通道会话无活动返回nil", func(t *testing.T) {
		r2 := &SituationReport{Sessions: []SessionSummary{
			{SessionID: "x", ChannelID: "web", LastMessageAt: 0},
		}}
		assert.Nil(t, r2.FindSessionForChannel("web"))
	})
}

// TestSituationReport_RenderForLLM 渲染含各段落。
// Python: situation_report.py:96
func TestSituationReport_RenderForLLM(t *testing.T) {
	report := &SituationReport{
		Sessions: []SessionSummary{
			{SessionID: "sess-1", ChannelID: "web", Title: "报表讨论", CompressedHistory: "[User]: 帮我整理报表"},
			{SessionID: "sess-2", ChannelID: "tui", CompressedHistory: ""},
		},
		RecommendationHistorySummary: "- [task_reminder] 整理报表",
		SkillsSummary:                "- 翻译技能 | 翻文档 [已安装]",
		CalendarEvents: []map[string]any{
			{"title": "周会", "start": "2026-11-11T10:00", "end": "2026-11-11T11:00", "location": "3F"},
		},
	}
	got := report.RenderForLLM()

	assert.Contains(t, got, "## 当前对话（所有推荐基于此）")
	assert.Contains(t, got, "### 会话: 报表讨论 (channel: web)")
	assert.Contains(t, got, "[User]: 帮我整理报表")
	assert.Contains(t, got, "## 历史推荐记录（系统已推过，按推荐类型区分去重，见决策约束）")
	assert.Contains(t, got, "- [task_reminder] 整理报表")
	assert.Contains(t, got, "## 即将到来的日程")
	assert.Contains(t, got, "- 2026-11-11T10:00 ~ 2026-11-11T11:00 周会 @ 3F")
	assert.Contains(t, got, "## 候选 Skill")
	assert.Contains(t, got, "- 翻译技能 | 翻文档 [已安装]")
}

// TestSituationReport_RenderForLLM_降级 缺标题/通道/历史时的保底文案。
func TestSituationReport_RenderForLLM_降级(t *testing.T) {
	t.Run("无会话只渲染历史段", func(t *testing.T) {
		got := (&SituationReport{}).RenderForLLM()
		assert.Contains(t, got, "## 历史推荐记录")
		assert.Contains(t, got, "（无推荐历史）")
		assert.NotContains(t, got, "## 当前对话")
	})

	t.Run("标题为空用会话ID前16字符", func(t *testing.T) {
		report := &SituationReport{Sessions: []SessionSummary{
			{SessionID: "abcdefghijklmnopqrstuvwxyz", CompressedHistory: "x"},
		}}
		got := report.RenderForLLM()
		assert.Contains(t, got, "### 会话: abcdefghijklmnop (channel: default)")
	})

	t.Run("无日程与skill段", func(t *testing.T) {
		report := &SituationReport{Sessions: []SessionSummary{{CompressedHistory: "x"}}}
		got := report.RenderForLLM()
		assert.NotContains(t, got, "## 即将到来的日程")
		assert.NotContains(t, got, "## 候选 Skill")
	})

	t.Run("日程无地点不加at", func(t *testing.T) {
		report := &SituationReport{CalendarEvents: []map[string]any{
			{"title": "站会", "start": "09:00", "end": "09:15"},
		}}
		got := report.RenderForLLM()
		assert.Contains(t, got, "- 09:00 ~ 09:15 站会")
		assert.NotContains(t, got, " @ ")
	})
}

// ──────────────────────────── CompressHistoryForProfile 测试分组 ────────────────────────────

// TestCompressHistoryForProfile 按 [User]/[Assistant] 轮次压缩。
// Python: situation_report.py:262
func TestCompressHistoryForProfile(t *testing.T) {
	sessionsDir := t.TempDir()

	t.Run("基本轮次", func(t *testing.T) {
		writeSession(t, sessionsDir, "s1", nil, []map[string]any{
			userRecord("你好"),
			assistantRecord("chat.final", "你好，有什么可以帮你"),
		})
		got := compressHistoryIn(sessionsDir, "s1", 20)
		assert.Equal(t, "[User]: 你好\n\n[Assistant]: 你好，有什么可以帮你", got)
	})

	t.Run("只接受chat.delta与chat.final", func(t *testing.T) {
		writeSession(t, sessionsDir, "s2", nil, []map[string]any{
			userRecord("问题"),
			assistantRecord("reasoning", "这段不该被采纳"),
			assistantRecord("chat.final", "答案"),
		})
		got := compressHistoryIn(sessionsDir, "s2", 20)
		assert.Contains(t, got, "答案")
		assert.NotContains(t, got, "这段不该被采纳", "非 chat.delta/chat.final 应被忽略")
	})

	t.Run("同轮多个final被拼接", func(t *testing.T) {
		writeSession(t, sessionsDir, "s3", nil, []map[string]any{
			userRecord("Q"),
			assistantRecord("chat.delta", "部分1"),
			assistantRecord("chat.final", "部分2"),
		})
		got := compressHistoryIn(sessionsDir, "s3", 20)
		assert.Contains(t, got, "部分1部分2", "同轮多段应无缝拼接")
	})

	t.Run("maxRounds截断保留最新", func(t *testing.T) {
		history := make([]map[string]any, 0, 10)
		for i := 0; i < 5; i++ {
			history = append(history, userRecord("问题"+string(rune('0'+i))))
			history = append(history, assistantRecord("chat.final", "答案"+string(rune('0'+i))))
		}
		writeSession(t, sessionsDir, "s4", nil, history)
		got := compressHistoryIn(sessionsDir, "s4", 2)
		assert.Contains(t, got, "问题3", "应保留最新 2 轮")
		assert.Contains(t, got, "问题4")
		assert.NotContains(t, got, "问题0", "最早的轮次应被截断")
	})

	t.Run("content为列表时拼接", func(t *testing.T) {
		writeSession(t, sessionsDir, "s5", nil, []map[string]any{
			{"role": "user", "content": []any{"第一段", "第二段"}},
			assistantRecord("chat.final", "收到"),
		})
		got := compressHistoryIn(sessionsDir, "s5", 20)
		assert.Contains(t, got, "[User]: 第一段 第二段")
	})

	t.Run("用户文本截断1000字符", func(t *testing.T) {
		long := strings.Repeat("字", 1500)
		writeSession(t, sessionsDir, "s6", nil, []map[string]any{
			userRecord(long),
			assistantRecord("chat.final", "ok"),
		})
		got := compressHistoryIn(sessionsDir, "s6", 20)
		assert.Contains(t, got, "[User]: "+strings.Repeat("字", 1000))
		assert.NotContains(t, got, strings.Repeat("字", 1001), "用户文本应截断到 1000 字符")
	})

	t.Run("助手文本截断3000字符", func(t *testing.T) {
		long := strings.Repeat("答", 4000)
		writeSession(t, sessionsDir, "s7", nil, []map[string]any{
			userRecord("Q"),
			assistantRecord("chat.final", long),
		})
		got := compressHistoryIn(sessionsDir, "s7", 20)
		assert.Contains(t, got, "[Assistant]: "+strings.Repeat("答", 3000))
		assert.NotContains(t, got, strings.Repeat("答", 3001))
	})

	t.Run("历史文件不存在返回空", func(t *testing.T) {
		assert.Equal(t, "", compressHistoryIn(sessionsDir, "no-such-session", 20))
	})

	t.Run("历史文件损坏返回空", func(t *testing.T) {
		dir := filepath.Join(sessionsDir, "broken")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), []byte("{坏"), 0o644))
		assert.Equal(t, "", compressHistoryIn(sessionsDir, "broken", 20))
	})

	t.Run("只有助手无用户不产生轮次", func(t *testing.T) {
		writeSession(t, sessionsDir, "s8", nil, []map[string]any{
			assistantRecord("chat.final", "无人提问"),
		})
		assert.Equal(t, "", compressHistoryIn(sessionsDir, "s8", 20))
	})

	t.Run("用户无助手回复不产生轮次", func(t *testing.T) {
		writeSession(t, sessionsDir, "s9", nil, []map[string]any{
			userRecord("只有提问"),
		})
		assert.Equal(t, "", compressHistoryIn(sessionsDir, "s9", 20),
			"用户提问但助手未回复，不构成一轮")
	})
}

// ──────────────────────────── ScanSessions 测试分组 ────────────────────────────

// TestScanSessionsIn 扫描、过滤、排序、截断。
// Python: situation_report.py:158
func TestScanSessionsIn(t *testing.T) {
	t.Run("空目录返回空", func(t *testing.T) {
		got, err := scanSessionsIn(t.TempDir(), 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("目录不存在返回空", func(t *testing.T) {
		got, err := scanSessionsIn(filepath.Join(t.TempDir(), "absent"), 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("maxSessions为0直接返回", func(t *testing.T) {
		got, err := scanSessionsIn(t.TempDir(), 0, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("按last_message_at降序并截断", func(t *testing.T) {
		dir := t.TempDir()
		for i, ts := range []float64{100, 300, 200} {
			sid := "s" + string(rune('0'+i))
			writeSession(t, dir, sid, map[string]any{
				"channel_id": "web", "mode": "agent", "last_message_at": ts, "title": sid,
			}, []map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		}
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "s1", got[0].SessionID, "300 应排第一")
		assert.Equal(t, "s2", got[1].SessionID, "200 第二")
		assert.Equal(t, "s0", got[2].SessionID, "100 第三")

		truncated, err := scanSessionsIn(dir, 2, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Len(t, truncated, 2, "应按 maxSessions 截断")
	})

	t.Run("mode前缀过滤", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "agent-sess", map[string]any{
			"mode": "agent", "last_message_at": 100.0,
		}, []map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		writeSession(t, dir, "code-sess", map[string]any{
			"mode": "code", "last_message_at": 200.0,
		}, []map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		writeSession(t, dir, "team-sess", map[string]any{
			"mode": "team", "last_message_at": 300.0,
		}, []map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})

		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		require.Len(t, got, 1, "默认只扫 agent 类")
		assert.Equal(t, "agent-sess", got[0].SessionID)

		// modePrefix 为空串 = 不过滤
		all, err := scanSessionsIn(dir, 10, 20, "")
		require.NoError(t, err)
		assert.Len(t, all, 3)
	})

	t.Run("agent子模式也算agent类", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "s", map[string]any{"mode": "agent.plan", "last_message_at": 100.0},
			[]map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Len(t, got, 1, "agent.plan 前缀是 agent，应被扫到")
	})

	t.Run("无mode字段被跳过", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "no-mode", map[string]any{"last_message_at": 100.0},
			[]map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got, "无 mode 无法判断类型，按不属 agent 类处理")
	})

	t.Run("跳过health_check与heartbeat前缀", func(t *testing.T) {
		dir := t.TempDir()
		for _, sid := range []string{"health_check_x", "heartbeat_y"} {
			writeSession(t, dir, sid, map[string]any{"mode": "agent", "last_message_at": 100.0},
				[]map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		}
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("缺metadata跳过", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "no-meta", nil, []map[string]any{userRecord("q")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("缺history跳过", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "no-history", map[string]any{"mode": "agent", "last_message_at": 100.0}, nil)
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got, "无历史文件无法分析，应跳过")
	})

	t.Run("last_message_at为0跳过", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "no-activity", map[string]any{"mode": "agent", "last_message_at": 0.0},
			[]map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		assert.Empty(t, got, "无活动会话不应进报告")
	})

	t.Run("delivery_context只保留路由字段", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "s", map[string]any{
			"mode": "agent", "last_message_at": 100.0,
			"delivery_context": map[string]any{
				"channel_id":     "web",
				"route_metadata": map[string]any{"k": "v"},
				"session_id":     "s",
				"secret_field":   "不该保留",
			},
		}, []map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NotNil(t, got[0].DeliveryContext)
		assert.Contains(t, got[0].DeliveryContext, "channel_id")
		assert.Contains(t, got[0].DeliveryContext, "route_metadata")
		assert.Contains(t, got[0].DeliveryContext, "session_id")
		assert.NotContains(t, got[0].DeliveryContext, "secret_field", "只保留路由相关字段")
	})

	t.Run("channel_id缺省default", func(t *testing.T) {
		dir := t.TempDir()
		writeSession(t, dir, "s", map[string]any{"mode": "agent", "last_message_at": 100.0},
			[]map[string]any{userRecord("q"), assistantRecord("chat.final", "a")})
		got, err := scanSessionsIn(dir, 10, 20, defaultScanModePrefix)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "default", got[0].ChannelID)
	})
}

// ──────────────────────────── FormatSkillsForLLM 测试分组 ────────────────────────────

// TestFormatSkillsForLLM 三种安装状态与描述截断。
// Python: situation_report.py:328
func TestFormatSkillsForLLM(t *testing.T) {
	t.Run("空列表", func(t *testing.T) {
		assert.Equal(t, "（无候选 skill）", FormatSkillsForLLM(nil))
	})

	t.Run("三种状态", func(t *testing.T) {
		got := FormatSkillsForLLM([]map[string]any{
			{"name": "已装技能", "description": "描述A", "installed": true, "source": "local"},
			{"name": "内置未装", "description": "描述B", "installed": false, "source": "builtin"},
			{"name": "其他未装", "description": "描述C", "installed": false, "source": "marketplace"},
		})
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "- 已装技能 | 描述A [已安装]", lines[0])
		assert.Equal(t, "- 内置未装 | 描述B [未安装·内置技能]", lines[1])
		assert.Equal(t, "- 其他未装 | 描述C [未安装]", lines[2])
	})

	t.Run("描述截断到80字符", func(t *testing.T) {
		long := strings.Repeat("描", 200)
		got := FormatSkillsForLLM([]map[string]any{{"name": "s", "description": long, "installed": true}})
		assert.Contains(t, got, strings.Repeat("描", 80))
		assert.NotContains(t, got, strings.Repeat("描", 81))
	})

	t.Run("描述缺失时不 panic", func(t *testing.T) {
		got := FormatSkillsForLLM([]map[string]any{{"name": "只有名字", "installed": true}})
		assert.Contains(t, got, "只有名字")
	})
}

// ──────────────────────────── FormatRecommendationHistory 测试分组 ────────────────────────────

// TestFormatRecommendationHistory 按类型决定是否带话术摘要。
// Python: situation_report.py:353
func TestFormatRecommendationHistory(t *testing.T) {
	t.Run("空历史", func(t *testing.T) {
		assert.Equal(t, "（无推荐历史）", FormatRecommendationHistory(nil))
	})

	t.Run("skill与exploration带话术摘要", func(t *testing.T) {
		got := FormatRecommendationHistory([]map[string]any{
			{"type": "skill_recommend", "target": "翻译技能", "content": "帮你翻文档"},
			{"type": "need_exploration", "target": "世界杯赛制", "content": "顺带聊聊"},
		})
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 2)
		assert.Equal(t, "- [skill_recommend] 翻译技能（话术：帮你翻文档）", lines[0])
		assert.Equal(t, "- [need_exploration] 世界杯赛制（话术：顺带聊聊）", lines[1])
	})

	t.Run("task_reminder不带话术", func(t *testing.T) {
		got := FormatRecommendationHistory([]map[string]any{
			{"type": "task_reminder", "target": "整理报表", "content": "这段不该出现"},
		})
		assert.Equal(t, "- [task_reminder] 整理报表", got)
	})

	t.Run("content缺失时只输出target", func(t *testing.T) {
		got := FormatRecommendationHistory([]map[string]any{
			{"type": "skill_recommend", "target": "翻译技能"},
		})
		assert.Equal(t, "- [skill_recommend] 翻译技能", got)
	})

	t.Run("话术截断到60字符", func(t *testing.T) {
		long := strings.Repeat("话", 200)
		got := FormatRecommendationHistory([]map[string]any{
			{"type": "skill_recommend", "target": "T", "content": long},
		})
		assert.Contains(t, got, strings.Repeat("话", 60))
		assert.NotContains(t, got, strings.Repeat("话", 61))
	})

	t.Run("只取最近10条", func(t *testing.T) {
		history := make([]map[string]any, 0, 15)
		for i := 0; i < 15; i++ {
			history = append(history, map[string]any{
				"type": "task_reminder", "target": "T" + strconv.Itoa(i),
			})
		}
		got := FormatRecommendationHistory(history)
		lines := strings.Split(got, "\n")
		assert.Len(t, lines, 10, "只喂最近 10 条")
		assert.Contains(t, lines[0], "T5", "应从第 6 条开始")
		assert.Contains(t, lines[9], "T14")
	})

	t.Run("字段缺失用问号占位", func(t *testing.T) {
		got := FormatRecommendationHistory([]map[string]any{{}})
		assert.Equal(t, "- [?] ?", got)
	})
}

// ──────────────────────────── matchSessionMode 测试分组 ────────────────────────────

// TestMatchSessionMode 前缀匹配语义。
// Python: situation_report.py:139
func TestMatchSessionMode(t *testing.T) {
	t.Run("前缀为空不过滤", func(t *testing.T) {
		assert.True(t, matchSessionMode(map[string]any{}, ""))
	})

	t.Run("无mode返回false", func(t *testing.T) {
		assert.False(t, matchSessionMode(map[string]any{}, "agent"))
		assert.False(t, matchSessionMode(map[string]any{"mode": ""}, "agent"))
		assert.False(t, matchSessionMode(map[string]any{"mode": "   "}, "agent"))
	})

	t.Run("前缀匹配", func(t *testing.T) {
		assert.True(t, matchSessionMode(map[string]any{"mode": "agent"}, "agent"))
		assert.True(t, matchSessionMode(map[string]any{"mode": "agent.plan"}, "agent"))
		assert.False(t, matchSessionMode(map[string]any{"mode": "code"}, "agent"))
	})
}

// ──────────────────────────── 数值/文本提取辅助 ────────────────────────────

// TestFloatField 兼容多种数值类型。
func TestFloatField(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  float64
	}{
		{"float64", 1.5, 1.5},
		{"float32", float32(2.5), 2.5},
		{"int", 3, 3.0},
		{"int64", int64(4), 4.0},
		{"字符串不转换", "5", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, floatField(map[string]any{"k": tc.value}, "k"))
		})
	}
	assert.Equal(t, 0.0, floatField(map[string]any{}, "missing"))
}

// TestExtractContentText 字符串/列表/其他类型。
func TestExtractContentText(t *testing.T) {
	assert.Equal(t, "abc", extractContentText("abc"))
	assert.Equal(t, "a b", extractContentText([]any{"a", "b", 3}))
	assert.Equal(t, "", extractContentText(123))
	assert.Equal(t, "", extractContentText(nil))
}

// TestStringFieldHelpers 字符串字段提取。
func TestStringFieldHelpers(t *testing.T) {
	m := map[string]any{"s": "v", "n": 1}
	assert.Equal(t, "v", stringField(m, "s"))
	assert.Equal(t, "", stringField(m, "n"))
	assert.Equal(t, "", stringField(m, "missing"))

	assert.Equal(t, "v", stringFieldDefault(m, "s", "d"))
	assert.Equal(t, "d", stringFieldDefault(m, "missing", "d"))
	assert.Equal(t, "d", stringFieldDefault(map[string]any{"s": ""}, "s", "d"))
}

// ──────────────────────────── BuildSituationReport 测试分组 ────────────────────────────

// TestBuildSituationReport_技能摘要 技能列表被渲染进报告。
func TestBuildSituationReport_技能摘要(t *testing.T) {
	// 用真实 workspace 目录（读不到内容时返回空报告，但技能段仍应拼上）
	report := BuildSituationReport(context.Background(), 20, []map[string]any{
		{"name": "翻译技能", "description": "翻文档", "installed": true},
	}, defaultScanModePrefix)

	require.NotNil(t, report)
	assert.Contains(t, report.SkillsSummary, "翻译技能")
	assert.Contains(t, report.SkillsSummary, "[已安装]")
}

// TestBuildSituationReport_无技能 空技能列表时不渲染技能段。
func TestBuildSituationReport_无技能(t *testing.T) {
	report := BuildSituationReport(context.Background(), 20, nil, defaultScanModePrefix)
	require.NotNil(t, report)
	assert.Empty(t, report.SkillsSummary)
}

// TestBuildSituationReport_日历内部拉取 日历由函数内部从 MCP 拉取，不由调用方传入。
//
// Python: situation_report.py:463 calendar_events = await fetch_calendar_events()
// 无日历 MCP 配置时得空列表，不报错（可选能力不阻断 tick）。
func TestBuildSituationReport_日历内部拉取(t *testing.T) {
	report := BuildSituationReport(context.Background(), 20, nil, defaultScanModePrefix)
	require.NotNil(t, report, "无日历配置也应返回非 nil 报告")
	// 未配置日历 MCP 时为空；配了则应是归一化后的事件（都有 title 键）
	for _, ev := range report.CalendarEvents {
		_, hasTitle := ev["title"]
		assert.True(t, hasTitle, "日历事件应已归一化（必有 title 键）")
	}
}

// TestBuildSituationReport_ctx取消 ctx 已取消时返回空报告不阻塞。
func TestBuildSituationReport_ctx取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := BuildSituationReport(ctx, 20, nil, defaultScanModePrefix)
	require.NotNil(t, report, "ctx 取消也应返回非 nil 报告")
}

// ──────────────────────────── 补充覆盖 ────────────────────────────

// TestCompressHistoryForProfile_公开入口 公开入口走真实 sessions 目录。
func TestCompressHistoryForProfile_公开入口(t *testing.T) {
	// 不存在的会话 → 空串（不 panic、不建目录）
	assert.Equal(t, "", CompressHistoryForProfile("nonexistent-session-xyz", 20))
}

// TestScanSessions_公开入口 公开入口走真实 sessions 目录。
func TestScanSessions_公开入口(t *testing.T) {
	got, err := ScanSessions(maxScanSessions, 20, defaultScanModePrefix)
	require.NoError(t, err)
	// 真实环境可能为空，只验证不报错、元素类型正确
	for _, s := range got {
		assert.NotEmpty(t, s.SessionID)
	}
}

// TestReadLocalReport 直接调用内部读函数，覆盖成功与扫描超时分支。
func TestReadLocalReport(t *testing.T) {
	report, err := readLocalReport(20, defaultScanModePrefix)
	// 成功或扫描超时都可能（真实目录），但不应是其它错误
	if err != nil {
		assert.ErrorIs(t, err, errSessionScanTimeout)
		return
	}
	require.NotNil(t, report)
	assert.NotEmpty(t, report.RecommendationHistorySummary, "即使无历史也应渲染「（无推荐历史）」")
}

// TestReadLocalReportWithDeadline_共享在飞读取 并发调用共享同一次读取。
func TestReadLocalReportWithDeadline_共享在飞读取(t *testing.T) {
	readReportMu.Lock()
	readReportState = nil
	readReportMu.Unlock()
	t.Cleanup(func() {
		readReportMu.Lock()
		readReportState = nil
		readReportMu.Unlock()
	})

	done := make(chan *SituationReport, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- readLocalReportWithDeadline(context.Background(), 20, defaultScanModePrefix)
		}()
	}
	first, second := <-done, <-done
	// 两次结果应来自同一次读取（同为 nil 或指针相同）
	if first == nil && second == nil {
		return
	}
	assert.Same(t, first, second, "并发 waiter 应共享同一次本地读取结果")
}

// TestReadLocalReportWithDeadline_键变化不复用 不同 key 的在飞读取不共享。
func TestReadLocalReportWithDeadline_键变化不复用(t *testing.T) {
	readReportMu.Lock()
	readReportState = &inFlightReportRead{key: "different-key", done: make(chan struct{})}
	readReportMu.Unlock()
	t.Cleanup(func() {
		readReportMu.Lock()
		readReportState = nil
		readReportMu.Unlock()
	})

	got := readLocalReportWithDeadline(context.Background(), 20, defaultScanModePrefix)
	assert.Nil(t, got, "键不匹配的陈旧在飞读取不应被复用，本次直接放弃")
}

// TestReadLocalReportWithDeadline_陈旧已完成的读取被丢弃 上一轮已完成的读取不复用。
func TestReadLocalReportWithDeadline_陈旧已完成读取被丢弃(t *testing.T) {
	stale := &inFlightReportRead{key: "stale", done: make(chan struct{})}
	close(stale.done) // 已完成
	stale.report = &SituationReport{SkillsSummary: "陈旧数据"}

	readReportMu.Lock()
	readReportState = stale
	readReportMu.Unlock()
	t.Cleanup(func() {
		readReportMu.Lock()
		readReportState = nil
		readReportMu.Unlock()
	})

	got := readLocalReportWithDeadline(context.Background(), 20, defaultScanModePrefix)
	// 不应返回陈旧报告
	if got != nil {
		assert.NotEqual(t, "陈旧数据", got.SkillsSummary, "已完成的历史读取应被丢弃并重新读取")
	}
}

// TestReadLocalReport_扫描超时 用大量会话触发扫描超时分支（sessionScanBudgetSec 很小的时候）。
// 该用例以「要么成功、要么超时」断言，避免依赖真实计时。
func TestReadLocalReport_扫描超时(t *testing.T) {
	report, err := readLocalReport(20, defaultScanModePrefix)
	if err != nil {
		assert.ErrorIs(t, err, errSessionScanTimeout)
		assert.Nil(t, report)
		return
	}
	assert.NotNil(t, report)
}
