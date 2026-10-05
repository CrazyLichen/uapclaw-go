//go:build integration

package dreaming

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	coredreaming "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/dreaming"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SweeperE2ESuite 测试 Sweeper 端到端管线。
//
// 对齐 Python: tests/system_tests/test_dreaming_e2e.py
// 覆盖：
//   - Sweeper Init + 目录创建
//   - ScanNewSessions 增量扫描
//   - ParseHistory + DetectRounds 管线
//   - Promote agent/code 模式
//   - Checkpoint 持久化与恢复
//   - LoadExistingSummary 两种模式
//   - DreamingConfig 环境变量加载
//   - StartDreaming/StopDreaming API 生命周期
type SweeperE2ESuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSweeperE2ESuite(t *testing.T) {
	suite.Run(t, new(SweeperE2ESuite))
}

// ---------------------------------------------------------------------------
// Sweeper Init + 目录创建
// ---------------------------------------------------------------------------

// TestSweeper_Init成功 测试 Sweeper.Init 创建目录并加载 checkpoint。
// 对齐 Python: TestDreamingE2EAgent.test_sweep_writes_dreaming_md（Init 部分）
func (s *SweeperE2ESuite) TestSweeper_Init成功() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	err := sweeper.Init()
	s.Require().NoError(err, "Init 不应返回错误")

	// 验证目录已创建
	s.DirExists(filepath.Join(outputDir, ".dreams"), "应创建 .dreams 目录")
	s.DirExists(outputDir, "应创建 output 目录")
}

// TestSweeper_Init无checkpoint 测试 Init 时无 checkpoint 文件不崩溃。
func (s *SweeperE2ESuite) TestSweeper_Init无checkpoint() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	err := sweeper.Init()
	s.Require().NoError(err, "Init 无 checkpoint 时不应返回错误")
}

// ---------------------------------------------------------------------------
// ScanNewSessions 增量扫描
// ---------------------------------------------------------------------------

// TestScanNewSessions_首次扫描 测试首次扫描发现所有新 session。
// 对齐 Python: TestDreamingE2EAgent.test_sweep_writes_dreaming_md
func (s *SweeperE2ESuite) TestScanNewSessions_首次扫描() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	// 创建 3 个 session 目录，每个有足够的轮次（≥4）
	sessionIDs := []string{"sess-001", "sess-002", "sess-003"}
	for _, sid := range sessionIDs {
		s.createSessionDir(filepath.Join(sessionsDir, sid), 5)
	}

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	sessions := sweeper.ScanNewSessions()
	s.Len(sessions, 3, "首次扫描应发现 3 个 session")
}

// TestScanNewSessions_无session 测试空目录扫描返回空列表。
func (s *SweeperE2ESuite) TestScanNewSessions_无session() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	sessions := sweeper.ScanNewSessions()
	s.Empty(sessions, "空目录扫描应返回空列表")
}

// TestScanNewSessions_轮次不足跳过 测试轮次 < 4 的 session 被跳过。
func (s *SweeperE2ESuite) TestScanNewSessions_轮次不足跳过() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	// 创建轮次不足的 session（2 轮）
	s.createSessionDir(filepath.Join(sessionsDir, "short-session"), 2)

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	sessions := sweeper.ScanNewSessions()
	s.Empty(sessions, "轮次不足的 session 应被跳过")
}

// TestScanNewSessions_heartbeat目录跳过 测试 heartbeat 前缀的目录被跳过。
func (s *SweeperE2ESuite) TestScanNewSessions_heartbeat目录跳过() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	// 创建 heartbeat session（应被跳过）
	s.createSessionDir(filepath.Join(sessionsDir, "heartbeat-001"), 5)
	// 创建 3 个正常 session（满足 minNewSessions=3 最低要求）
	for _, sid := range []string{"normal-001", "normal-002", "normal-003"} {
		s.createSessionDir(filepath.Join(sessionsDir, sid), 5)
	}

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	sessions := sweeper.ScanNewSessions()
	// 只应有 3 个正常 session，heartbeat 被跳过
	s.Len(sessions, 3, "heartbeat session 应被跳过，正常 session 不受影响")
	for _, sess := range sessions {
		s.NotContains(sess.SessionID, "heartbeat", "结果中不应包含 heartbeat session")
	}
}

// ---------------------------------------------------------------------------
// ParseHistory + DetectRounds 管线
// ---------------------------------------------------------------------------

// TestParseHistory_真实JSON 测试 ParseHistory 解析真实 history.json。
func (s *SweeperE2ESuite) TestParseHistory_真实JSON() {
	dir := s.T().TempDir()
	s.createHistoryFile(dir, 5) // 5 轮

	events := swarmdreaming.ParseHistory(dir)
	s.NotEmpty(events, "ParseHistory 应返回非空事件列表")

	// 验证至少包含 user 和 assistant 事件
	hasUser := false
	hasAssistant := false
	for _, e := range events {
		if e.Role == "user" {
			hasUser = true
		}
		if e.Role == "assistant" {
			hasAssistant = true
		}
	}
	s.True(hasUser, "应包含 user 事件")
	s.True(hasAssistant, "应包含 assistant 事件")
}

// TestDetectRounds_真实对话 测试 DetectRounds 检测对话轮次。
func (s *SweeperE2ESuite) TestDetectRounds_真实对话() {
	dir := s.T().TempDir()
	s.createHistoryFile(dir, 5)

	events := swarmdreaming.ParseHistory(dir)
	rounds := swarmdreaming.DetectRounds(events)
	s.Len(rounds, 5, "5 轮对话应检测到 5 个 Round")
}

// ---------------------------------------------------------------------------
// Promote agent 模式
// ---------------------------------------------------------------------------

// TestPromote_Agent写入DREAMINGmd 测试 Promote agent 模式写入 DREAMING.md。
// 对齐 Python: TestDreamingE2EAgent.test_sweep_writes_dreaming_md
func (s *SweeperE2ESuite) TestPromote_Agent写入DREAMINGmd() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	result, err := sweeper.Promote("用户偏好", "用户喜欢深色主题", "sess-001")
	s.Require().NoError(err, "Promote 不应返回错误")
	s.NotEmpty(result, "Promote 应返回非空标识")

	// 验证 DREAMING.md 已创建
	dreamingPath := filepath.Join(outputDir, "DREAMING.md")
	s.FileExists(dreamingPath, "应创建 DREAMING.md")

	data, err := os.ReadFile(dreamingPath)
	s.Require().NoError(err)
	content := string(data)
	s.Contains(content, "用户偏好", "DREAMING.md 应包含标题")
	s.Contains(content, "用户喜欢深色主题", "DREAMING.md 应包含内容")
	s.Contains(content, "sess-001", "DREAMING.md 应包含来源 session")
}

// TestPromote_Agent去重 测试相同标题的 Promote 不重复写入。
// 对齐 Python: TestDreamingE2EAgent.test_dedup_skips_existing_title
func (s *SweeperE2ESuite) TestPromote_Agent去重() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	// 第一次 Promote
	result1, err := sweeper.Promote("项目结构", "项目采用分层架构", "sess-001")
	s.Require().NoError(err)
	s.NotEmpty(result1)

	// 第二次相同标题 Promote（应被跳过）
	result2, err := sweeper.Promote("项目结构", "项目采用微服务架构", "sess-002")
	s.Require().NoError(err)
	s.Empty(result2, "相同标题的 Promote 应返回空")

	// 验证 DREAMING.md 只有一条
	entries := swarmdreaming.ParseDreamingEntries(readFile(filepath.Join(outputDir, "DREAMING.md")))
	s.Len(entries, 1, "去重后应只有 1 条")
	// 内容应为第一次的值
	s.Contains(entries[0].Content, "分层架构", "去重后内容应保持第一次的值")
}

// TestPromote_Agent最大条目数 测试超过 maxEntriesAgent 时驱逐旧条目。
func (s *SweeperE2ESuite) TestPromote_Agent最大条目数() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	// 写入 52 条（超过 maxEntriesAgent=50）
	for i := 0; i < 52; i++ {
		_, err := sweeper.Promote(
			"条目_"+strings.Repeat("0", 3-len(string(rune('0'+i%10))))+string(rune('0'+i%10)),
			"内容_"+string(rune('0'+i%10)),
			"sess-001",
		)
		s.Require().NoError(err)
	}

	entries := swarmdreaming.ParseDreamingEntries(readFile(filepath.Join(outputDir, "DREAMING.md")))
	s.LessOrEqual(len(entries), 50, "条目数不应超过 maxEntriesAgent")
}

// ---------------------------------------------------------------------------
// Promote code 模式
// ---------------------------------------------------------------------------

// TestPromote_Code写入Consolidated 测试 Promote code 模式写入 consolidated 文件。
// 对齐 Python: TestDreamingE2ECode.test_sweep_writes_consolidated_files
func (s *SweeperE2ESuite) TestPromote_Code写入Consolidated() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "code", "zh")
	s.Require().NoError(sweeper.Init())

	result, err := sweeper.Promote("代码规范", "使用 golangci-lint", "sess-001")
	s.Require().NoError(err, "Promote code 不应返回错误")
	s.NotEmpty(result, "Promote code 应返回非空标识")

	// 验证 consolidated 文件已创建
	matches, err := filepath.Glob(filepath.Join(outputDir, "consolidated_*.md"))
	s.Require().NoError(err)
	s.NotEmpty(matches, "应创建至少一个 consolidated_*.md 文件")

	// 验证内容包含 YAML frontmatter 和正文
	data, err := os.ReadFile(matches[0])
	s.Require().NoError(err)
	content := string(data)
	s.Contains(content, "---", "consolidated 文件应包含 YAML frontmatter")
	s.Contains(content, "使用 golangci-lint", "consolidated 文件应包含内容")
}

// ---------------------------------------------------------------------------
// Checkpoint 持久化
// ---------------------------------------------------------------------------

// TestCheckpoint_持久化与恢复 测试 checkpoint 在两次 Init 间正确持久化和恢复。
// 由于 checkpoint 由 RunSweep 内部保存（需 LLM 调用成功后），此处直接写入
// checkpoint 文件模拟已扫描状态，验证恢复时已扫描 session 不再出现。
// 对齐 Python: TestDreamingE2EAgent.test_incremental_scan_reprocesses_updated_session
func (s *SweeperE2ESuite) TestCheckpoint_持久化与恢复() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	dreamsDir := filepath.Join(outputDir, ".dreams")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	// 创建 5 个 session
	for _, sid := range []string{"sess-001", "sess-002", "sess-003", "sess-004", "sess-005"} {
		s.createSessionDir(filepath.Join(sessionsDir, sid), 5)
	}

	// 模拟已有 checkpoint：sess-001 和 sess-002 已被扫描
	s.Require().NoError(os.MkdirAll(dreamsDir, 0o755))
	cp := swarmdreaming.CheckpointData{
		ScannedSessions: map[string]swarmdreaming.ScannedSession{
			"sess-001": {HistoryMtime: float64(time.Now().UnixNano()) / 1e9, RoundCount: 5},
			"sess-002": {HistoryMtime: float64(time.Now().UnixNano()) / 1e9, RoundCount: 5},
		},
		LastScanTs: time.Now().Format(time.RFC3339),
	}
	cpData, _ := json.Marshal(cp)
	s.Require().NoError(os.WriteFile(filepath.Join(dreamsDir, "ingestion-checkpoint.json"), cpData, 0o644))

	// Init 应加载 checkpoint
	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	// 扫描应只发现新 session（sess-003, sess-004, sess-005）
	sessions := sweeper.ScanNewSessions()
	s.Len(sessions, 3, "已扫描的 2 个 session 不应再次出现")
	for _, sess := range sessions {
		s.NotEqual("sess-001", sess.SessionID, "sess-001 已扫描，不应再次出现")
		s.NotEqual("sess-002", sess.SessionID, "sess-002 已扫描，不应再次出现")
	}
}

// ---------------------------------------------------------------------------
// LoadExistingSummary
// ---------------------------------------------------------------------------

// TestLoadExistingSummary_Agent模式 测试 agent 模式读取 DREAMING.md。
func (s *SweeperE2ESuite) TestLoadExistingSummary_Agent模式() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	// 先写入 DREAMING.md
	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())
	_, _ = sweeper.Promote("测试知识", "测试内容", "sess-001")

	// 新 sweeper 应能加载现有摘要
	sweeper2 := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper2.Init())

	summary := sweeper2.LoadExistingSummary()
	s.NotEmpty(summary, "应能加载现有 DREAMING.md")
	s.Contains(summary, "测试知识", "摘要应包含已有标题")
}

// TestLoadExistingSummary_空目录 测试空目录返回 UI 占位符。
func (s *SweeperE2ESuite) TestLoadExistingSummary_空目录() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	summary := sweeper.LoadExistingSummary()
	// 空目录时 LoadExistingSummary 返回 uiText.EmptySummary 占位符（如 "(空)"）
	// 对齐 Python: 返回 None 或占位符
	s.NotEmpty(summary, "LoadExistingSummary 在空目录应返回占位符")
}

// ---------------------------------------------------------------------------
// DreamingConfig 环境变量
// ---------------------------------------------------------------------------

// TestDreamingConfig_默认值 测试 LoadDreamingConfig 默认值。
func (s *SweeperE2ESuite) TestDreamingConfig_默认值() {
	cfg := swarmdreaming.LoadDreamingConfig("agent")
	s.False(cfg.Enabled, "默认应不启用")
	s.Equal(14400.0, cfg.IntervalSeconds, "默认间隔应为 14400 秒")
}

// TestDreamingConfig_环境变量覆盖 测试环境变量覆盖配置。
func (s *SweeperE2ESuite) TestDreamingConfig_环境变量覆盖() {
	// 设置环境变量
	s.T().Setenv("DREAMING_AGENT_ENABLED", "true")
	s.T().Setenv("DREAMING_INTERVAL", "7200")

	cfg := swarmdreaming.LoadDreamingConfig("agent")
	s.True(cfg.Enabled, "环境变量应启用 dreaming")
	s.Equal(7200.0, cfg.IntervalSeconds, "环境变量应覆盖间隔")
}

// ---------------------------------------------------------------------------
// StartDreaming / StopDreaming API
// ---------------------------------------------------------------------------

// TestStartDreaming_禁用配置 测试 disabled 配置不启动。
// 对齐 Python: TestDreamingE2ELifecycle.test_start_disabled_config
func (s *SweeperE2ESuite) TestStartDreaming_禁用配置() {
	// 确保默认禁用
	s.T().Setenv("DREAMING_TEST_DISABLED_ENABLED", "false")

	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	orch, err := swarmdreaming.StartDreaming(
		s.Ctx, sessionsDir, outputDir, "test_disabled", "zh",
		func() bool { return false },
	)
	s.Require().NoError(err, "禁用配置不应返回错误")
	s.Nil(orch, "禁用配置应返回 nil orchestrator")

	// 清理
	swarmdreaming.StopDreaming(s.Ctx, "test_disabled")
}

// TestStopDreaming_空模式停止所有 测试空 mode 停止所有 orchestrator。
func (s *SweeperE2ESuite) TestStopDreaming_空模式停止所有() {
	// 清理全局状态
	swarmdreaming.StopDreaming(s.Ctx, "")

	// 验证停止后没有残留
	orch := swarmdreaming.GetDreamingOrchestrator("agent")
	s.Nil(orch, "停止所有后不应有 agent orchestrator")
}

// TestGetDreamingOrchestrator_不存在 测试获取不存在的 orchestrator 返回 nil。
func (s *SweeperE2ESuite) TestGetDreamingOrchestrator_不存在() {
	orch := swarmdreaming.GetDreamingOrchestrator("nonexistent_mode_xyz")
	s.Nil(orch, "不存在的 mode 应返回 nil")
}

// ---------------------------------------------------------------------------
// ParseDreamingEntries
// ---------------------------------------------------------------------------

// TestParseDreamingEntries_标准格式 测试解析标准 DREAMING.md 格式。
func (s *SweeperE2ESuite) TestParseDreamingEntries_标准格式() {
	text := `# Dreaming 记忆

## 知识1
_source: sess-001 | 2025-01-01 00:00:00_
知识1内容

## 知识2
_source: sess-002 | 2025-01-02 00:00:00_
知识2内容
`
	entries := swarmdreaming.ParseDreamingEntries(text)
	s.Len(entries, 2, "应解析 2 条")
	s.Equal("知识1", entries[0].Title)
	s.Contains(entries[0].Content, "知识1内容")
	s.Equal("知识2", entries[1].Title)
}

// TestParseDreamingEntries_空文本 测试解析空文本。
func (s *SweeperE2ESuite) TestParseDreamingEntries_空文本() {
	entries := swarmdreaming.ParseDreamingEntries("")
	s.Empty(entries, "空文本应返回空列表")
}

// ---------------------------------------------------------------------------
// Orchestrator 与 Sweeper 集成
// ---------------------------------------------------------------------------

// TestOrchestrator_SweepFn集成 测试 Orchestrator 调用 SweepFn。
func (s *SweeperE2ESuite) TestOrchestrator_SweepFn集成() {
	var sweepCount int
	sweepFn := func(_ context.Context) error {
		sweepCount++
		return nil
	}

	// 创建 60s 间隔的 orchestrator
	orch := coredreaming.NewDreamingOrchestrator(
		sweepFn,
		60*time.Second,
		coredreaming.WithBusyChecker(func() bool { return false }),
	)
	s.Require().NoError(orch.Start(s.Ctx))
	defer orch.Stop(s.Ctx)

	s.True(orch.IsRunning(), "启动后应处于运行状态")

	// 立即停止（在 120s 初始延迟内），sweep 不应执行
	s.Require().NoError(orch.Stop(s.Ctx))
	s.Equal(0, sweepCount, "初始延迟内停止不应执行 sweep")
}

// TestOrchestrator_Health集成 测试 Orchestrator Health 与 Sweeper 集成。
func (s *SweeperE2ESuite) TestOrchestrator_Health集成() {
	tmpDir := s.T().TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "output")
	s.Require().NoError(os.MkdirAll(sessionsDir, 0o755))

	sweeper := swarmdreaming.NewSweeper(sessionsDir, outputDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())

	orch := coredreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error {
			sweeper.RunSweep(ctx)
			return nil
		},
		3600*time.Second,
	)

	health := orch.Health()
	s.False(health.Running, "未启动时 Health.Running 应为 false")
	s.Equal(3600.0, health.IntervalSeconds, "Health.IntervalSeconds 应为 3600")
}

// ---------------------------------------------------------------------------
// GetPrompt / GetSysMsg
// ---------------------------------------------------------------------------

// TestGetPrompt_所有模式 测试所有模式和语言的 prompt 模板。
func (s *SweeperE2ESuite) TestGetPrompt_所有模式() {
	modes := []string{"agent", "code"}
	langs := []string{"zh", "en"}

	for _, mode := range modes {
		for _, lang := range langs {
			prompt := swarmdreaming.GetPrompt(mode, lang)
			s.NotEmpty(prompt, "GetPrompt(%s,%s) 不应为空", mode, lang)
		}
	}
}

// TestGetSysMsg_所有模式 测试所有模式和语言的 system message。
func (s *SweeperE2ESuite) TestGetSysMsg_所有模式() {
	modes := []string{"agent", "code"}
	langs := []string{"zh", "en"}

	for _, mode := range modes {
		for _, lang := range langs {
			msg := swarmdreaming.GetSysMsg(mode, lang)
			s.NotEmpty(msg, "GetSysMsg(%s,%s) 不应为空", mode, lang)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createSessionDir 创建测试用 session 目录，含 history.json 和 metadata.json。
// rounds 指定对话轮次数（每轮包含 1 个 user + 1 个 assistant）。
func (s *SweeperE2ESuite) createSessionDir(sessionDir string, rounds int) {
	s.Require().NoError(os.MkdirAll(sessionDir, 0o755))
	s.createHistoryFile(sessionDir, rounds)

	// 写入 metadata.json
	metadata := map[string]any{
		"session_id":  filepath.Base(sessionDir),
		"mode":        "agent",
		"created_at":  time.Now().Format(time.RFC3339),
		"round_count": rounds,
	}
	data, _ := json.Marshal(metadata)
	s.Require().NoError(os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644))
}

// createHistoryFile 创建测试用 history.json 文件。
func (s *SweeperE2ESuite) createHistoryFile(sessionDir string, rounds int) {
	var events []map[string]any
	for i := 0; i < rounds; i++ {
		events = append(events, map[string]any{
			"role":    "user",
			"content": "用户问题 " + strings.Repeat("x", 50) + " 第" + string(rune('0'+i)) + "轮",
		})
		events = append(events, map[string]any{
			"role":       "assistant",
			"content":    "助手回答 " + strings.Repeat("y", 100) + " 第" + string(rune('0'+i)) + "轮",
			"event_type": "chat.final",
		})
	}
	data, _ := json.Marshal(events)
	s.Require().NoError(os.WriteFile(filepath.Join(sessionDir, "history.json"), data, 0o644))
}

// readFile 读取文件内容，失败返回空字符串。
func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
