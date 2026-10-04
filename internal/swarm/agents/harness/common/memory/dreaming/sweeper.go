package dreaming

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingConfig Dreaming 配置。
// Python: jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py (DreamingConfig)
type DreamingConfig struct {
	// Enabled 是否启用
	Enabled bool
	// IntervalSeconds 调度间隔秒数，默认 14400.0（4 小时）
	IntervalSeconds float64
}

// ScannedSession 已扫描 session 的快照信息。
type ScannedSession struct {
	// HistoryMtime history.json 的修改时间戳
	HistoryMtime float64 `json:"history_mtime"`
	// RoundCount 对话轮次计数
	RoundCount int `json:"round_count"`
}

// SweeperSession 扫描结果条目。
type SweeperSession struct {
	// SessionID 会话标识
	SessionID string
	// CompressedText 压缩后的对话文本
	CompressedText string
}

// HistoryEvent history.json 中的一条事件。
type HistoryEvent struct {
	// Role 消息角色
	Role string `json:"role"`
	// Content 消息内容
	Content string `json:"content"`
	// EventType 事件类型（如 "chat.final"）
	EventType string `json:"event_type"`
}

// Round 对话轮次（user 消息起始索引, assistant 消息索引）。
type Round struct {
	// UserIdx user 消息在 events 中的索引
	UserIdx int
	// AssistantIdx assistant 消息在 events 中的索引
	AssistantIdx int
}

// KnowledgeItem LLM 提取的知识条目。
type KnowledgeItem struct {
	// Title 标题
	Title string `json:"title"`
	// Content 内容
	Content string `json:"content"`
	// SourceSessionID 来源 session（由 RunSweep 填充）
	SourceSessionID string `json:"source_session_id,omitempty"`
}

// DreamingEntry DREAMING.md 中的条目。
type DreamingEntry struct {
	// Title 标题
	Title string
	// Source 来源信息（格式：_source: session_id | timestamp_）
	Source string
	// Content 内容
	Content string
}

// CheckpointData checkpoint 数据。
type CheckpointData struct {
	// ScannedSessions 已扫描 session 映射
	ScannedSessions map[string]ScannedSession `json:"scanned_sessions"`
	// LastScanTs 上次扫描时间
	LastScanTs string `json:"last_scan_ts"`
}

// UIText i18n 文本。
// Python: _UI_TEXT
type UIText struct {
	// EmptySummary 空摘要占位符
	EmptySummary string
	// DreamingHeader DREAMING.md 文件头
	DreamingHeader string
	// TruncateRounds 轮次截断提示
	TruncateRounds string
	// TruncateHard 硬截断提示
	TruncateHard string
}

// Sweeper 统一记忆整理管线：Scan + Compression + LLM Extraction + Promotion。
// Python: jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py (Sweeper)
type Sweeper struct {
	// sessionsDir 会话目录路径
	sessionsDir string
	// outputDir 输出目录路径
	outputDir string
	// mode 模式（"agent" / "code"）
	mode string
	// language 语言（"zh" / "en"）
	language string
	// dreamsDir checkpoint 目录（outputDir + "/.dreams"）
	dreamsDir string
	// scannedSessions 已扫描 session 缓存
	scannedSessions map[string]ScannedSession
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minSessionRounds 最少对话轮次（对齐 Python: _MIN_SESSION_ROUNDS = 4）
	minSessionRounds = 4
	// maxSessionsPerSweep 每轮最多扫描 session 数（对齐 Python: _MAX_SESSIONS_PER_SWEEP = 10）
	maxSessionsPerSweep = 10
	// maxSessionAgeDays session 最大保留天数（对齐 Python: _MAX_SESSION_AGE_DAYS = 30）
	maxSessionAgeDays = 30
	// maxCompressTokens 压缩后最大 token 估算（对齐 Python: _MAX_COMPRESS_TOKENS = 30000）
	maxCompressTokens = 30000
	// maxEntriesAgent agent 模式 DREAMING.md 最大条目数（对齐 Python: _MAX_ENTRIES_AGENT = 50）
	maxEntriesAgent = 50
	// maxPromotionsPerSession 每个 session 最多提取条目数（对齐 Python: _MAX_PROMOTIONS_PER_SESSION = 5）
	maxPromotionsPerSession = 5
	// minNewSessions 最少新 session 数才触发 sweep（对齐 Python: _MIN_NEW_SESSIONS = 3）
	minNewSessions = 3
	// minSessionAgeBypassDays 绕过最少新 session 限制的 session 天数（对齐 Python: _MIN_SESSION_AGE_BYPASS_DAYS = 7）
	minSessionAgeBypassDays = 7
	// maxCompressRecurse 压缩递归最大深度
	maxCompressRecurse = 5

	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentServer
)

// ──────────────────────────── 全局变量 ────────────────────────────

// uiText i18n 文本映射（对齐 Python: _UI_TEXT）
var uiText = map[string]UIText{
	"zh": {
		EmptySummary:   "(空)",
		DreamingHeader: "# Dreaming 记忆\n",
		TruncateRounds: "[... 前 %d 轮对话已省略 ...]\n\n",
		TruncateHard:   "[... 内容过长，已截断 ...]\n\n",
	},
	"en": {
		EmptySummary:   "(None)",
		DreamingHeader: "# Dreaming Memories\n",
		TruncateRounds: "[... First %d rounds omitted ...]\n\n",
		TruncateHard:   "[... Content too long, truncated ...]\n\n",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadDreamingConfig 从 config.yaml 和环境变量加载 Dreaming 配置。
// Python: DreamingConfig.load(mode)
func LoadDreamingConfig(mode string) DreamingConfig {
	defaultCfg := DreamingConfig{
		Enabled:         false,
		IntervalSeconds: 14400.0,
	}

	// 对齐 Python: from jiuwenswarm.common.config import get_config
	cfg, err := config.New("")
	if err != nil {
		logger.Warn(logComponent).Str("mode", mode).Err(err).Msg("[dreaming] configuration load failed, using default values")
		return defaultCfg
	}
	cfgData, err := cfg.Load()
	if err != nil {
		logger.Warn(logComponent).Str("mode", mode).Err(err).Msg("[dreaming] configuration load failed, using default values")
		return defaultCfg
	}

	// 从 config.yaml 读取 memory.dreaming.{mode} 段
	// Python: raw = get_config().get("memory", {}).get("dreaming", {}).get(mode, {})
	memorySection, _ := cfgData["memory"].(map[string]any)
	dreamingSection, _ := memorySection["dreaming"].(map[string]any)
	raw, _ := dreamingSection[mode].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}

	// 环境变量优先级高于配置文件
	// Python: env_key = f"DREAMING_{mode.upper()}_ENABLED"
	envKey := fmt.Sprintf("DREAMING_%s_ENABLED", strings.ToUpper(mode))
	if envVal := os.Getenv(envKey); envVal != "" {
		// Python: enabled = env_val.lower() in ("true", "1", "yes")
		lower := strings.ToLower(envVal)
		defaultCfg.Enabled = lower == "true" || envVal == "1" || lower == "yes"
	} else {
		// Python: enabled = bool(raw.get("enabled", False))
		if v, ok := raw["enabled"]; ok {
			if b, ok := v.(bool); ok {
				defaultCfg.Enabled = b
			}
		}
	}

	// Python: interval = float(os.getenv("DREAMING_INTERVAL", str(raw.get("interval_seconds", 14400.0))))
	if envInterval := os.Getenv("DREAMING_INTERVAL"); envInterval != "" {
		if f, perr := strconv.ParseFloat(envInterval, 64); perr == nil {
			defaultCfg.IntervalSeconds = f
		}
	} else {
		if v, ok := raw["interval_seconds"]; ok {
			if f, ok := v.(float64); ok {
				defaultCfg.IntervalSeconds = f
			}
		}
	}

	return defaultCfg
}

// NewSweeper 创建 Sweeper 实例。
// Python: Sweeper.__init__(sessions_dir, output_dir, mode, language)
func NewSweeper(sessionsDir, outputDir, mode, language string) *Sweeper {
	return &Sweeper{
		sessionsDir:     sessionsDir,
		outputDir:       outputDir,
		mode:            mode,
		language:        language,
		dreamsDir:       filepath.Join(outputDir, ".dreams"),
		scannedSessions: make(map[string]ScannedSession),
	}
}

// Init 创建目录并加载 checkpoint。
// Python: Sweeper.init()
func (s *Sweeper) Init() error {
	// Python: self._dreams_dir.mkdir(parents=True, exist_ok=True)
	if err := os.MkdirAll(s.dreamsDir, 0o755); err != nil {
		return fmt.Errorf("创建 dreams 目录失败: %w", err)
	}
	// Python: Path(self._output_dir).mkdir(parents=True, exist_ok=True)
	if err := os.MkdirAll(s.outputDir, 0o755); err != nil {
		return fmt.Errorf("创建 output 目录失败: %w", err)
	}
	// Python: cp = self._load_checkpoint()
	cp := s.loadCheckpoint()
	// Python: raw = cp.get("scanned_sessions", [])
	raw := cp.ScannedSessions
	if len(raw) > 0 {
		s.scannedSessions = raw
	} else {
		s.scannedSessions = make(map[string]ScannedSession)
	}
	return nil
}

// RunSweep 执行完整管线。由 Orchestrator 调用。
// Python: Sweeper.run_sweep()
// T-01: 移除 error 返回值，对齐 Python void 语义（内部已通过 recover + 日志处理所有异常）
func (s *Sweeper) RunSweep(ctx context.Context) {
	// 对齐 Python: try/except 包裹 scan_new_sessions，异常时 log + 安全返回
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("[Sweeper] Scan stage failed")
		}
	}()

	sweepStart := time.Now()

	// ── 扫描 + 预过滤 + 压缩 ──
	// Python: sessions = await asyncio.get_running_loop().run_in_executor(None, self.scan_new_sessions)
	sessions := s.ScanNewSessions()
	if len(sessions) == 0 {
		logger.Debug(logComponent).Msg("[Sweeper] No eligible session found")
		return
	}

	// ── LLM 提取 ──
	// Python: existing_summary = self.load_existing_summary()
	existingSummary := s.LoadExistingSummary()
	var allKnowledge []KnowledgeItem
	var succeededIDs []string
	var failedIDs []string

	// Python: for s in sessions: items = await self._extract_via_llm(...)
	for _, sess := range sessions {
		items, err := s.extractViaLLM(ctx, sess.CompressedText, existingSummary)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sess.SessionID).Msg("[Sweeper] LLM Extraction for session failed")
			failedIDs = append(failedIDs, sess.SessionID)
			continue
		}
		// Python: for item in items: item["source_session_id"] = s["session_id"]
		for i := range items {
			items[i].SourceSessionID = sess.SessionID
		}
		allKnowledge = append(allKnowledge, items...)
		succeededIDs = append(succeededIDs, sess.SessionID)
	}

	// ── 更新成功会话的检查点 ──
	// Python: if succeeded_ids: ... self.save_checkpoint()
	if len(succeededIDs) > 0 {
		sessionsRoot := filepath.Clean(s.sessionsDir)
		for _, sid := range succeededIDs {
			// Python: history_mtime = (sessions_root / sid / "history.json").stat().st_mtime
			historyPath := filepath.Join(sessionsRoot, sid, "history.json")
			var historyMtime float64
			if info, err := os.Stat(historyPath); err == nil {
				historyMtime = float64(info.ModTime().UnixNano()) / 1e9
			}
			// Python: events = self._parse_history(sessions_root / sid)
			events := ParseHistory(filepath.Join(sessionsRoot, sid))
			// Python: rounds = self._detect_rounds(events)
			rounds := DetectRounds(events)
			s.scannedSessions[sid] = ScannedSession{
				HistoryMtime: historyMtime,
				RoundCount:   len(rounds),
			}
		}
		s.saveCheckpoint()
	}

	// ── 晋升 ──
	// Python: for k in all_knowledge: result = self._promote(...)
	promoted := 0
	for _, k := range allKnowledge {
		result, err := s.Promote(k.Title, k.Content, k.SourceSessionID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("title", k.Title).Msg("[Sweeper] Promotion failed")
			continue
		}
		if result != "" {
			promoted++
		}
	}

	// Python: logger.info("[Sweeper] sweep completed: duration=%.1fs sessions=%d ...")
	duration := time.Since(sweepStart)
	logger.Info(logComponent).
		Float64("duration_seconds", duration.Seconds()).
		Int("sessions", len(sessions)).
		Int("llm_succeeded", len(succeededIDs)).
		Int("llm_failed", len(failedIDs)).
		Int("extracted", len(allKnowledge)).
		Int("promoted", promoted).
		Msg("[Sweeper] sweep completed")

	return
}

// ScanNewSessions 增量扫描 session 目录，返回新/更新的 session 列表（含压缩文本）。
// Python: Sweeper.scan_new_sessions()
func (s *Sweeper) ScanNewSessions() []SweeperSession {
	sessionsRoot := filepath.Clean(s.sessionsDir)
	rootInfo, err := os.Stat(sessionsRoot)
	if err != nil || !rootInfo.IsDir() {
		return nil
	}

	// Python: all_ids = {d.name for d in sessions_root.iterdir() if d.is_dir() and not d.name.startswith("heartbeat")}
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return nil
	}
	allIDs := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), "heartbeat") {
			allIDs[entry.Name()] = true
		}
	}

	// Python: scanned_ids = set(self.scanned_sessions.keys())
	scannedIDs := make(map[string]bool)
	for k := range s.scannedSessions {
		scannedIDs[k] = true
	}

	// Python: brand_new = all_ids - scanned_ids
	brandNew := make(map[string]bool)
	for id := range allIDs {
		if !scannedIDs[id] {
			brandNew[id] = true
		}
	}

	// Python: new_ids = brand_new.copy()
	newIDs := make(map[string]bool)
	for id := range brandNew {
		newIDs[id] = true
	}

	// Python: 增量检测——已有 session 的 history.json mtime 变更
	for id := range allIDs {
		if !scannedIDs[id] {
			continue
		}
		historyPath := filepath.Join(sessionsRoot, id, "history.json")
		info, err := os.Stat(historyPath)
		if err != nil {
			continue
		}
		currentMtime := float64(info.ModTime().UnixNano()) / 1e9
		snapshot, exists := s.scannedSessions[id]
		if !exists {
			continue
		}
		// Python: if current_mtime <= snapshot.get("history_mtime", 0): continue
		if currentMtime <= snapshot.HistoryMtime {
			continue
		}
		newIDs[id] = true
	}

	// Python: if len(new_ids) < _MIN_NEW_SESSIONS: ...
	if len(newIDs) < minNewSessions {
		ageBypass := float64(minSessionAgeBypassDays) * 86400.0
		hasOld := false
		now := float64(time.Now().Unix())
		for id := range newIDs {
			info, err := os.Stat(filepath.Join(sessionsRoot, id))
			if err != nil {
				continue
			}
			if now-float64(info.ModTime().UnixNano())/1e9 > ageBypass {
				hasOld = true
				break
			}
		}
		if !hasOld {
			logger.Info(logComponent).
				Int("new_sessions", len(newIDs)).
				Int("min_required", minNewSessions).
				Msg("[Sweeper] new sessions < min and no expired sessions, skipping")
			return nil
		}
	}

	// Python: 遍历 new_ids，过滤、压缩
	var results []SweeperSession
	now := float64(time.Now().Unix())
	maxAge := float64(maxSessionAgeDays) * 86400.0

	for _, sessionID := range sortedKeys(newIDs) {
		sessionDir := filepath.Join(sessionsRoot, sessionID)

		// Python: mtime = session_dir.stat().st_mtime
		info, err := os.Stat(sessionDir)
		if err != nil {
			continue
		}
		// Python: if now - mtime > max_age: ... continue
		if now-float64(info.ModTime().UnixNano())/1e9 > maxAge {
			s.scannedSessions[sessionID] = ScannedSession{}
			continue
		}

		// Python: is_incremental = session_id in self.scanned_sessions and self.scanned_sessions.get(session_id)
		_, wasScanned := s.scannedSessions[sessionID]
		isIncremental := wasScanned && s.scannedSessions[sessionID] != (ScannedSession{})

		// Python: if not self._match_session_mode(session_id): continue
		if !s.matchSessionMode(sessionID) {
			continue
		}

		// Python: events = self._parse_history(session_dir)
		events := ParseHistory(sessionDir)
		if len(events) == 0 {
			continue
		}

		// Python: rounds = self._detect_rounds(events)
		rounds := DetectRounds(events)

		if isIncremental {
			// Python: last_count = self.scanned_sessions[session_id].get("round_count", 0)
			lastCount := 0
			if snapshot, ok := s.scannedSessions[sessionID]; ok {
				lastCount = snapshot.RoundCount
			}
			// Python: new_rounds = len(rounds) - last_count
			newRounds := len(rounds) - lastCount
			if newRounds <= 0 {
				continue
			}
			// Python: if new_rounds < _MIN_SESSION_ROUNDS: continue
			if newRounds < minSessionRounds {
				continue
			}
			// Python: incremental_events = events[rounds[last_count][0]:]
			if lastCount < len(rounds) {
				startIdx := rounds[lastCount].UserIdx
				compressed := s.compress(events[startIdx:], 0)
				if strings.TrimSpace(compressed) == "" {
					continue
				}
				results = append(results, SweeperSession{SessionID: sessionID, CompressedText: compressed})
			}
		} else {
			// Python: if len(rounds) < _MIN_SESSION_ROUNDS: continue
			if len(rounds) < minSessionRounds {
				continue
			}
			compressed := s.compress(events, 0)
			if strings.TrimSpace(compressed) == "" {
				continue
			}
			results = append(results, SweeperSession{SessionID: sessionID, CompressedText: compressed})
		}

		// Python: if len(results) >= _MAX_SESSIONS_PER_SWEEP: break
		if len(results) >= maxSessionsPerSweep {
			break
		}
	}

	return results
}

// LoadExistingSummary 加载已有 DREAMING.md 或 consolidated_*.md 摘要。
// Python: Sweeper.load_existing_summary()
func (s *Sweeper) LoadExistingSummary() string {
	ui := getUIText(s.language)
	output := filepath.Clean(s.outputDir)
	info, err := os.Stat(output)
	if err != nil || !info.IsDir() {
		return ui.EmptySummary
	}

	if s.mode == "agent" {
		// Python: dreaming_path = output / "DREAMING.md"
		dreamingPath := filepath.Join(output, "DREAMING.md")
		data, err := os.ReadFile(dreamingPath)
		if err != nil {
			return ui.EmptySummary
		}
		text := string(data)
		// Python: titles = re.findall(r'^## (.+)$', text, re.MULTILINE)
		re := regexp.MustCompile(`(?m)^## (.+)$`)
		matches := re.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			return ui.EmptySummary
		}
		var lines []string
		for _, m := range matches {
			lines = append(lines, "- "+m[1])
		}
		return strings.Join(lines, "\n")
	}

	// code 模式：读 consolidated_*.md 的 name 字段
	// Python: for f in sorted(output.glob("consolidated_*.md")):
	var lines []string
	entries, err := os.ReadDir(output)
	if err != nil {
		return ui.EmptySummary
	}
	// T-02: 对齐 Python sorted(output.glob(...))，按文件名排序保证确定性
	var consolidatedFiles []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "consolidated_") && strings.HasSuffix(entry.Name(), ".md") {
			consolidatedFiles = append(consolidatedFiles, entry)
		}
	}
	sort.Slice(consolidatedFiles, func(i, j int) bool {
		return consolidatedFiles[i].Name() < consolidatedFiles[j].Name()
	})
	for _, entry := range consolidatedFiles {
		data, err := os.ReadFile(filepath.Join(output, entry.Name()))
		if err != nil {
			logger.Warn(logComponent).Str("file", entry.Name()).Err(err).Msg("[dreaming] failed to read consolidated file")
			continue
		}
		text := string(data)
		// Python: m = re.search(r'^name:\s*(.+)$', text, re.MULTILINE)
		re := regexp.MustCompile(`(?m)^name:\s*(.+)$`)
		m := re.FindStringSubmatch(text)
		if len(m) > 1 {
			lines = append(lines, "- "+strings.TrimSpace(m[1]))
		} else {
			lines = append(lines, "- "+strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	if len(lines) == 0 {
		return ui.EmptySummary
	}
	return strings.Join(lines, "\n")
}

// Promote 路由到 agent/code 模式的 promotion。
// Python: Sweeper._promote(title, content, session_id)
func (s *Sweeper) Promote(title, content, sessionID string) (string, error) {
	if title == "" || content == "" {
		return "", nil
	}
	if s.mode == "agent" {
		return s.promoteAgent(title, content, sessionID), nil
	}
	return s.promoteCode(title, content, sessionID), nil
}

// ParseHistory 解析 history.json，只保留 user/chat.final/assistant 事件。
// Python: Sweeper._parse_history(session_dir)
func ParseHistory(sessionDir string) []HistoryEvent {
	historyPath := filepath.Join(sessionDir, "history.json")
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil
	}
	var allEvents []HistoryEvent
	if err := json.Unmarshal(data, &allEvents); err != nil {
		return nil
	}

	// Python: _should_keep(entry)
	var events []HistoryEvent
	for _, e := range allEvents {
		if e.Role == "user" {
			events = append(events, e)
		} else if e.EventType == "chat.final" {
			events = append(events, e)
		} else if e.Role == "assistant" && e.EventType == "" {
			events = append(events, e)
		}
	}
	return events
}

// DetectRounds 检测对话轮次。
// Python: Sweeper._detect_rounds(events)
func DetectRounds(events []HistoryEvent) []Round {
	currentUser := -1
	var pairs []Round
	for i, e := range events {
		if e.Role == "user" {
			currentUser = i
		} else if e.Role == "assistant" && currentUser != -1 {
			pairs = append(pairs, Round{UserIdx: currentUser, AssistantIdx: i})
			currentUser = -1
		}
	}
	return pairs
}

// ParseDreamingEntries 解析 DREAMING.md 中的条目。
// Python: Sweeper._parse_dreaming_entries(text)
func ParseDreamingEntries(text string) []DreamingEntry {
	// Python: re.split(r'^## ', text, flags=re.MULTILINE)[1:]
	re := regexp.MustCompile(`(?m)^## `)
	parts := re.Split(text, -1)
	if len(parts) <= 1 {
		return nil
	}

	var entries []DreamingEntry
	for _, part := range parts[1:] {
		lines := strings.Split(strings.TrimSpace(part), "\n")
		if len(lines) == 0 {
			continue
		}
		title := strings.TrimSpace(lines[0])
		var source string
		var contentLines []string
		for _, line := range lines[1:] {
			if strings.HasPrefix(line, "_source:") && strings.HasSuffix(line, "_") {
				source = line
			} else {
				contentLines = append(contentLines, line)
			}
		}
		entries = append(entries, DreamingEntry{
			Title:   title,
			Source:  source,
			Content: strings.TrimSpace(strings.Join(contentLines, "\n")),
		})
	}
	return entries
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchSessionMode 按 metadata.json 过滤 mode。
// Python: Sweeper._match_session_mode(session_id)
func (s *Sweeper) matchSessionMode(sessionID string) bool {
	metaPath := filepath.Join(s.sessionsDir, sessionID, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	sessionMode, _ := raw["mode"].(string)
	sessionMode = strings.TrimSpace(sessionMode)
	if sessionMode == "" {
		return false
	}
	// Python: return session_mode.startswith(self._mode)
	return strings.HasPrefix(sessionMode, s.mode)
}

// compress 压缩对话文本。
// Python: Sweeper._compress(events, _depth=0)
func (s *Sweeper) compress(events []HistoryEvent, depth int) string {
	isCode := strings.HasPrefix(s.mode, "code")

	var parts []string
	for _, e := range events {
		if e.Role == "user" {
			// Python: parts.append(f"[User]: {content[:2000]}")
			content := e.Content
			if len(content) > 2000 {
				content = content[:2000]
			}
			parts = append(parts, "[User]: "+content)
		} else if !isCode && e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			// Python: parts.append(f"[Assistant]: {content[:3000]}")
			content := e.Content
			if len(content) > 3000 {
				content = content[:3000]
			}
			parts = append(parts, "[Assistant]: "+content)
		}
	}

	result := strings.Join(parts, "\n\n")

	// Python: est_tokens = len(result) // 2
	estTokens := len(result) / 2
	if estTokens > maxCompressTokens {
		if depth < maxCompressRecurse {
			rounds := DetectRounds(events)
			if len(rounds) > 2 {
				// Python: keep_from = len(rounds) // 3
				keepFrom := len(rounds) / 3
				trimmed := events[rounds[keepFrom].UserIdx:]
				ui := getUIText(s.language)
				prefix := fmt.Sprintf(ui.TruncateRounds, keepFrom)
				return prefix + s.compress(trimmed, depth+1)
			}
		}
		// Python: result = _UI_TEXT[self._language]["truncate_hard"] + result[-max_chars:]
		ui := getUIText(s.language)
		maxChars := maxCompressTokens * 2
		result = ui.TruncateHard + result[len(result)-maxChars:]
	}

	return result
}

// extractViaLLM 调用 LLM 提取知识条目。
// Python: Sweeper._extract_via_llm(compressed_text, existing_summary)
func (s *Sweeper) extractViaLLM(ctx context.Context, compressedText, existingSummary string) ([]KnowledgeItem, error) {
	// Python: from jiuwenswarm.common.config import get_default_models
	entries := getDefaultModels()
	if len(entries) == 0 {
		logger.Warn(logComponent).Msg("[Sweeper] No default models configured")
		return nil, nil
	}

	entry := entries[0]
	// Python: mcc = entry.get("model_client_config", {})
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	// Python: model_name = mcc.get("model_name", "")
	modelName, _ := mcc["model_name"].(string)

	// 将 mcc 的 map[string]any 转成 ModelClientConfig（消除后续类型断言）
	// ModelClientConfig 自定义了 UnmarshalJSON，支持 known/extra 字段拆分
	var clientConfig llmschema.ModelClientConfig
	if mccData, err := json.Marshal(mcc); err == nil {
		if err := json.Unmarshal(mccData, &clientConfig); err != nil {
			logger.Warn(logComponent).Err(err).Msg("[Sweeper] ModelClientConfig 反序列化失败，使用默认值")
		}
	}
	// 回退默认值
	if clientConfig.ClientProvider == "" {
		clientConfig.ClientProvider = "OpenAI"
	}
	if clientConfig.Timeout <= 0 {
		clientConfig.Timeout = 60.0
	}
	if clientConfig.MaxRetries <= 0 {
		clientConfig.MaxRetries = 3
	}
	// 补充自动生成的 ClientID
	if clientConfig.ClientID == "" {
		clientConfig.ClientID = uuid.New().String()
	}

	// Python: ModelRequestConfig(model=model_name, temperature=0.3)
	modelConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
		llmschema.WithTemperature(0.3),
	)

	model, err := llm.NewModel(&clientConfig, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("构造 Model 失败: %w", err)
	}

	// 选择提示词模板和系统消息
	key := [2]string{s.mode, s.language}
	promptTemplate, ok1 := promptMap[key]
	sysMsg, ok2 := sysMsgMap[key]
	if !ok1 || !ok2 {
		promptTemplate = promptMap[[2]string{"code", "zh"}]
		sysMsg = sysMsgMap[[2]string{"code", "zh"}]
	}

	// Python: prompt = prompt_template.format(existing_knowledge=..., compressed_session=..., max_items=...)
	prompt := fmt.Sprintf(promptTemplate, existingSummary, compressedText, maxPromotionsPerSession)

	messages := model_clients.NewMessagesParam(
		llmschema.NewSystemMessage(sysMsg),
		llmschema.NewUserMessage(prompt),
	)

	// Python: response = await model.invoke([...])
	response, err := model.Invoke(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}

	// Python: content = self._extract_content_str(response.content)
	content := extractContentStr(response.Content)

	// Python: json_match = re.search(r'```json\s*\n?(.*?)\n?```', content, re.DOTALL)
	jsonMatch := regexp.MustCompile("(?s)```json\\s*\\n?(.*?)\\n?```").FindStringSubmatch(content)
	if jsonMatch == nil {
		// Python: json_match = re.search(r'```\s*\n?(.*?)\n?```', content, re.DOTALL)
		jsonMatch = regexp.MustCompile("(?s)```\\s*\\n?(.*?)\\n?```").FindStringSubmatch(content)
	}
	jsonStr := content
	if jsonMatch != nil {
		jsonStr = jsonMatch[1]
	}

	// Python: parsed = json.loads(json_str.strip())
	var items []KnowledgeItem
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonStr)), &items); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[Sweeper] LLM output JSON parse failed")
		return nil, nil
	}
	return items, nil
}

// extractContentStr 从 LLM 响应中提取文本内容。
// Python: Sweeper._extract_content_str(content)
func extractContentStr(content llmschema.MessageContent) string {
	if content.Text() != "" {
		return content.Text()
	}
	parts := content.Parts()
	if len(parts) == 0 {
		return ""
	}
	var textParts []string
	for _, p := range parts {
		if p.Type == "text" {
			textParts = append(textParts, p.Text)
		}
	}
	return strings.Join(textParts, "\n")
}

// promoteAgent 写入 DREAMING.md。
// Python: Sweeper.promote_agent(title, content, session_id)
func (s *Sweeper) promoteAgent(title, content, sessionID string) string {
	dreamingPath := filepath.Join(s.outputDir, "DREAMING.md")
	ui := getUIText(s.language)

	// Python: if dreaming_path.exists(): existing = dreaming_path.read_text(...)
	existing := ui.DreamingHeader
	if data, err := os.ReadFile(dreamingPath); err == nil {
		existing = string(data)
	}

	// Python: entries = self._parse_dreaming_entries(existing)
	entries := ParseDreamingEntries(existing)

	// Python: if any(e["title"] == title for e in entries): return None
	for _, e := range entries {
		if e.Title == title {
			return ""
		}
	}

	// Python: today = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
	today := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Python: entries.append({title, source, content})
	entries = append(entries, DreamingEntry{
		Title:   title,
		Source:  fmt.Sprintf("_source: %s | %s_", sessionID, today),
		Content: content,
	})

	// Python: if len(entries) > _MAX_ENTRIES_AGENT: entries = entries[len(entries) - _MAX_ENTRIES_AGENT:]
	if len(entries) > maxEntriesAgent {
		entries = entries[len(entries)-maxEntriesAgent:]
	}

	// Python: parts = [header]; for e in entries: ...
	var parts []string
	parts = append(parts, ui.DreamingHeader)
	for _, e := range entries {
		parts = append(parts, "\n## "+e.Title)
		if e.Source != "" {
			parts = append(parts, e.Source)
		}
		parts = append(parts, "\n"+e.Content+"\n")
	}

	// Python: dreaming_path.write_text("\n".join(parts), encoding="utf-8")
	result := strings.Join(parts, "\n")
	if err := os.WriteFile(dreamingPath, []byte(result), 0o644); err != nil {
		logger.Error(logComponent).Err(err).Msg("[dreaming] failed to write DREAMING.md")
		return ""
	}
	return dreamingPath
}

// promoteCode 写入 consolidated_*.md。
// Python: Sweeper.promote_code(title, content, session_id)
func (s *Sweeper) promoteCode(title, content, sessionID string) string {
	// Python: content_hash = hashlib.sha256(content.encode("utf-8")).hexdigest()[:12]
	hash := sha256.Sum256([]byte(content))
	contentHash := fmt.Sprintf("%x", hash[:])[:12]
	filename := fmt.Sprintf("consolidated_%s.md", contentHash)
	outputPath := filepath.Join(s.outputDir, filename)

	// Python: if output_path.exists(): return None
	if _, err := os.Stat(outputPath); err == nil {
		return ""
	}

	// Python: today = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
	today := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Python: body = f"---\nname: {title}\nsource_session: {session_id}\ncreated_at: {today}\n---\n\n{content}\n"
	body := fmt.Sprintf("---\nname: %s\nsource_session: %s\ncreated_at: %s\n---\n\n%s\n", title, sessionID, today, content)

	if err := os.WriteFile(outputPath, []byte(body), 0o644); err != nil {
		logger.Error(logComponent).Err(err).Msg("[dreaming] failed to write consolidated file")
		return ""
	}
	return outputPath
}

// loadCheckpoint 加载 checkpoint 文件。
// Python: Sweeper._load_checkpoint()
func (s *Sweeper) loadCheckpoint() CheckpointData {
	path := filepath.Join(s.dreamsDir, "ingestion-checkpoint.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckpointData{}
	}
	if strings.TrimSpace(string(data)) == "" {
		return CheckpointData{}
	}
	var cp CheckpointData
	if err := json.Unmarshal(data, &cp); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).Msg("[dreaming] failed to load checkpoint")
		return CheckpointData{}
	}
	return cp
}

// saveCheckpoint 保存 checkpoint 文件。
// Python: Sweeper.save_checkpoint()
func (s *Sweeper) saveCheckpoint() {
	path := filepath.Join(s.dreamsDir, "ingestion-checkpoint.json")
	_ = os.MkdirAll(s.dreamsDir, 0o755)

	// Python: data = {"scanned_sessions": self.scanned_sessions, "last_scan_ts": ...}
	data := CheckpointData{
		ScannedSessions: s.scannedSessions,
		LastScanTs:      time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[Sweeper] checkpoint save failed")
		return
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[Sweeper] checkpoint save failed")
	}
}

// getDefaultModels 内部获取默认模型列表。
// 对齐 Python: from jiuwenswarm.common.config import get_default_models
func getDefaultModels() []map[string]any {
	cfg, err := config.New("")
	if err != nil {
		return nil
	}
	cfgData, err := cfg.Load()
	if err != nil {
		return nil
	}
	modelsSection, _ := cfgData["models"].(map[string]any)
	if modelsSection == nil {
		return nil
	}

	// 优先级：models.defaults（列表） > models.default（单对象）
	if defaults, ok := modelsSection["defaults"].([]any); ok && len(defaults) > 0 {
		result := make([]map[string]any, 0, len(defaults))
		for _, entry := range defaults {
			if m, ok := entry.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	if defaultEntry, ok := modelsSection["default"].(map[string]any); ok {
		return []map[string]any{defaultEntry}
	}
	return nil
}

// getUIText 获取指定语言的 i18n 文本，回退到 zh。
func getUIText(language string) UIText {
	if t, ok := uiText[language]; ok {
		return t
	}
	return uiText["zh"]
}

// sortedKeys 返回 map 的已排序键（对齐 Python 的 sorted(new_ids)）。
// T-03: 用 sort.Strings 替换冒泡排序，复杂度 O(n log n)。
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
