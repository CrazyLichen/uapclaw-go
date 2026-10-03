package dreaming

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ---------------------------------------------------------------------------
// DreamingConfig
// ---------------------------------------------------------------------------

// TestDreamingConfig_默认值 测试默认值。
func TestDreamingConfig_默认值(t *testing.T) {
	cfg := DreamingConfig{}
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 0.0, cfg.IntervalSeconds)
}

// TestDreamingConfig_Load_异常回退 测试 config 加载失败时返回默认值。
// Python: DreamingConfig.load() 异常时返回 cls()（enabled=false, interval=14400）
func TestDreamingConfig_Load_异常回退(t *testing.T) {
	// 无 config 文件时也应返回默认值，不 panic
	cfg := LoadDreamingConfig("nonexistent_mode")
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 14400.0, cfg.IntervalSeconds)
}

// ---------------------------------------------------------------------------
// ParseHistory
// ---------------------------------------------------------------------------

// TestParseHistory_事件过滤 测试只保留 user/chat.final/assistant 事件。
// Python: test__should_keep
func TestParseHistory_事件过滤(t *testing.T) {
	dir := t.TempDir()
	history := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi", "event_type": "tool_call"},
		{"role": "assistant", "content": "final answer", "event_type": "chat.final"},
		{"role": "assistant", "content": "direct"},
		{"role": "tool", "content": "result"},
	}
	data, _ := json.Marshal(history)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644))

	events := ParseHistory(dir)
	assert.Len(t, events, 3)
	assert.Equal(t, "user", events[0].Role)
	assert.Equal(t, "chat.final", events[1].EventType)
	assert.Equal(t, "assistant", events[2].Role)
	assert.Equal(t, "", events[2].EventType) // 无 event_type
}

// TestParseHistory_空文件 测试空 history.json 返回空。
func TestParseHistory_空文件(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), []byte("[]"), 0o644))
	events := ParseHistory(dir)
	assert.Len(t, events, 0)
}

// TestParseHistory_文件不存在 测试 history.json 不存在返回空。
func TestParseHistory_文件不存在(t *testing.T) {
	events := ParseHistory(t.TempDir())
	assert.Len(t, events, 0)
}

// ---------------------------------------------------------------------------
// DetectRounds
// ---------------------------------------------------------------------------

// TestDetectRounds 测试 user+assistant 配对。
// Python: _detect_rounds
func TestDetectRounds(t *testing.T) {
	events := []HistoryEvent{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2"},
	}
	rounds := DetectRounds(events)
	assert.Len(t, rounds, 2)
	assert.Equal(t, 0, rounds[0].UserIdx)
	assert.Equal(t, 1, rounds[0].AssistantIdx)
	assert.Equal(t, 2, rounds[1].UserIdx)
	assert.Equal(t, 3, rounds[1].AssistantIdx)
}

// TestDetectRounds_无配对 测试无 assistant 跟随时无轮次。
func TestDetectRounds_无配对(t *testing.T) {
	events := []HistoryEvent{
		{Role: "user", Content: "q1"},
		{Role: "user", Content: "q2"},
	}
	rounds := DetectRounds(events)
	assert.Len(t, rounds, 0)
}

// ---------------------------------------------------------------------------
// ParseDreamingEntries
// ---------------------------------------------------------------------------

// TestParseDreamingEntries_标题来源内容 测试 Markdown 解析。
// Python: _parse_dreaming_entries
func TestParseDreamingEntries_标题来源内容(t *testing.T) {
	text := "# Dreaming 记忆\n\n## 标题A\n_source: sess_1 | 2026-04-15_\n正文内容"
	entries := ParseDreamingEntries(text)
	require.Len(t, entries, 1)
	assert.Equal(t, "标题A", entries[0].Title)
	assert.Equal(t, "_source: sess_1 | 2026-04-15_", entries[0].Source)
	assert.Equal(t, "正文内容", entries[0].Content)
}

// TestParseDreamingEntries_空内容 测试空 DREAMING.md 返回空。
func TestParseDreamingEntries_空内容(t *testing.T) {
	entries := ParseDreamingEntries("")
	assert.Len(t, entries, 0)
}

// TestParseDreamingEntries_多条目 测试多条目解析。
func TestParseDreamingEntries_多条目(t *testing.T) {
	text := "# Dreaming\n\n## A\n_source: s1_\nbody a\n\n## B\n_source: s2_\nbody b"
	entries := ParseDreamingEntries(text)
	require.Len(t, entries, 2)
	assert.Equal(t, "A", entries[0].Title)
	assert.Equal(t, "B", entries[1].Title)
}

// ---------------------------------------------------------------------------
// Compress
// ---------------------------------------------------------------------------

// TestCompress_基本压缩 测试 user/assistant 内容截取。
func TestCompress_基本压缩(t *testing.T) {
	s := NewSweeper("", "", "agent", "zh")
	events := []HistoryEvent{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi there"},
	}
	result := s.compress(events, 0)
	assert.Contains(t, result, "[User]: hello world")
	assert.Contains(t, result, "[Assistant]: hi there")
}

// TestCompress_code模式不含assistant 测试 code 模式不含 assistant 内容。
func TestCompress_code模式不含assistant(t *testing.T) {
	s := NewSweeper("", "", "code", "zh")
	events := []HistoryEvent{
		{Role: "user", Content: "fix bug"},
		{Role: "assistant", Content: "done"},
	}
	result := s.compress(events, 0)
	assert.Contains(t, result, "[User]: fix bug")
	assert.NotContains(t, result, "[Assistant]")
}

// ---------------------------------------------------------------------------
// Checkpoint
// ---------------------------------------------------------------------------

// TestCheckpoint_加载保存 测试 round-trip。
func TestCheckpoint_加载保存(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "agent", "zh")
	s.dreamsDir = filepath.Join(dir, ".dreams")
	require.NoError(t, os.MkdirAll(s.dreamsDir, 0o755))

	s.scannedSessions = map[string]ScannedSession{
		"sess_1": {HistoryMtime: 123456.0, RoundCount: 5},
	}
	s.saveCheckpoint()

	s2 := NewSweeper("", dir, "agent", "zh")
	s2.dreamsDir = s.dreamsDir
	cp := s2.loadCheckpoint()
	assert.Equal(t, 123456.0, cp.ScannedSessions["sess_1"].HistoryMtime)
	assert.Equal(t, 5, cp.ScannedSessions["sess_1"].RoundCount)
}

// TestCheckpoint_文件不存在 测试 checkpoint 文件不存在时返回空。
func TestCheckpoint_文件不存在(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "agent", "zh")
	s.dreamsDir = filepath.Join(dir, ".dreams")
	cp := s.loadCheckpoint()
	assert.Nil(t, cp.ScannedSessions)
}

// ---------------------------------------------------------------------------
// PromoteAgent
// ---------------------------------------------------------------------------

// TestPromoteAgent_去重 测试标题重复时返回空。
// Python: if any(e["title"] == title for e in entries): return None
func TestPromoteAgent_去重(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "agent", "zh")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// 首次写入
	result1 := s.promoteAgent("标题A", "内容A", "sess_1")
	assert.NotEmpty(t, result1)

	// 重复标题
	result2 := s.promoteAgent("标题A", "内容B", "sess_2")
	assert.Empty(t, result2)
}

// TestPromoteAgent_淘汰 测试超 maxEntriesAgent 时淘汰旧条目。
func TestPromoteAgent_淘汰(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "agent", "zh")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// 写入超量条目
	for i := 0; i < maxEntriesAgent+5; i++ {
		s.promoteAgent(fmt.Sprintf("标题%d", i), fmt.Sprintf("内容%d", i), "sess_1")
	}

	// 验证 DREAMING.md 中条目数不超过 maxEntriesAgent
	data, err := os.ReadFile(filepath.Join(dir, "DREAMING.md"))
	require.NoError(t, err)
	entries := ParseDreamingEntries(string(data))
	assert.LessOrEqual(t, len(entries), maxEntriesAgent)
}

// ---------------------------------------------------------------------------
// PromoteCode
// ---------------------------------------------------------------------------

// TestPromoteCode_hash去重 测试相同内容 hash 去重。
// Python: if output_path.exists(): return None
func TestPromoteCode_hash去重(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "code", "zh")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	result1 := s.promoteCode("标题A", "相同内容", "sess_1")
	assert.NotEmpty(t, result1)

	// 相同内容 → hash 去重
	result2 := s.promoteCode("标题B", "相同内容", "sess_2")
	assert.Empty(t, result2)
}

// TestPromoteCode_YAMLfrontmatter 测试 YAML frontmatter 格式。
func TestPromoteCode_YAMLfrontmatter(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "code", "zh")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	result := s.promoteCode("标题A", "内容A", "sess_1")
	require.NotEmpty(t, result)

	data, err := os.ReadFile(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), "---")
	assert.Contains(t, string(data), "name: 标题A")
	assert.Contains(t, string(data), "source_session: sess_1")
}

// ---------------------------------------------------------------------------
// LoadExistingSummary
// ---------------------------------------------------------------------------

// TestLoadExistingSummary_agent模式 测试读 DREAMING.md 标题列表。
func TestLoadExistingSummary_agent模式(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	dreamingContent := "# Dreaming 记忆\n\n## 标题A\n内容A\n\n## 标题B\n内容B"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "DREAMING.md"), []byte(dreamingContent), 0o644))

	s := NewSweeper("", dir, "agent", "zh")
	summary := s.LoadExistingSummary()
	assert.Contains(t, summary, "- 标题A")
	assert.Contains(t, summary, "- 标题B")
}

// TestLoadExistingSummary_code模式 测试读 consolidated_*.md name 字段。
func TestLoadExistingSummary_code模式(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	content1 := "---\nname: 知识1\nsource_session: s1\ncreated_at: 2026-01-01\n---\n\n内容1"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "consolidated_abc123.md"), []byte(content1), 0o644))

	s := NewSweeper("", dir, "code", "zh")
	summary := s.LoadExistingSummary()
	assert.Contains(t, summary, "- 知识1")
}

// TestLoadExistingSummary_空目录 测试空目录返回占位符。
func TestLoadExistingSummary_空目录(t *testing.T) {
	dir := t.TempDir()
	s := NewSweeper("", dir, "agent", "zh")
	summary := s.LoadExistingSummary()
	assert.Equal(t, "(空)", summary)
}

// ---------------------------------------------------------------------------
// matchSessionMode
// ---------------------------------------------------------------------------

// TestMatchSessionMode 测试 metadata.json 中的 mode 匹配。
func TestMatchSessionMode(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sess_1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	meta := map[string]any{"mode": "agent.plan"}
	data, _ := json.Marshal(meta)
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644))

	s := NewSweeper(dir, "", "agent", "zh")
	assert.True(t, s.matchSessionMode("sess_1"))

	s2 := NewSweeper(dir, "", "code", "zh")
	assert.False(t, s2.matchSessionMode("sess_1"))
}

// ---------------------------------------------------------------------------
// Sweeper Init + NewSweeper
// ---------------------------------------------------------------------------

// TestNewSweeper 测试构造函数。
func TestNewSweeper(t *testing.T) {
	s := NewSweeper("/tmp/sessions", "/tmp/output", "agent", "en")
	assert.Equal(t, "/tmp/sessions", s.sessionsDir)
	assert.Equal(t, "/tmp/output", s.outputDir)
	assert.Equal(t, "agent", s.mode)
	assert.Equal(t, "en", s.language)
	assert.Equal(t, filepath.Join("/tmp/output", ".dreams"), s.dreamsDir)
}

// TestSweeper_Init 测试 Init 创建目录。
func TestSweeper_Init(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	s := NewSweeper("", outputDir, "agent", "zh")
	require.NoError(t, s.Init())

	_, err := os.Stat(filepath.Join(outputDir, ".dreams"))
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// getUIText
// ---------------------------------------------------------------------------

// TestGetUIText_回退 测试未知语言回退到 zh。
func TestGetUIText_回退(t *testing.T) {
	ui := getUIText("fr")
	assert.Equal(t, "(空)", ui.EmptySummary) // 回退到 zh
}

// TestGetUIText_英文 测试英文。
func TestGetUIText_英文(t *testing.T) {
	ui := getUIText("en")
	assert.Equal(t, "(None)", ui.EmptySummary)
}

// ---------------------------------------------------------------------------
// extractContentStr
// ---------------------------------------------------------------------------

// TestExtractContentStr_纯文本 测试纯文本提取。
func TestExtractContentStr_纯文本(t *testing.T) {
	content := llmschema.NewUserMessage("hello").Content
	result := extractContentStr(content)
	assert.Equal(t, "hello", result)
}
