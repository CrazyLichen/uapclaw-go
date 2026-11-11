package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSummary 情境报告中单个会话的摘要。
// Python: situation_report.py:35 SessionSummary
type SessionSummary struct {
	// SessionID 会话标识
	SessionID string
	// ChannelID 通道标识，缺省 "default"
	ChannelID string
	// Title 会话标题
	Title string
	// LastMessageAt 最后消息时间戳（秒）
	LastMessageAt float64
	// MessageCount 消息条数
	MessageCount int
	// CompressedHistory 压缩后的对话历史（[User]/[Assistant] 轮次）
	CompressedHistory string
	// DeliveryContext 投递路由上下文（channel_id / route_metadata / session_id）
	DeliveryContext map[string]any
}

// SituationReport 主动引擎单次 tick 的聚合上下文。
// Python: situation_report.py:48 SituationReport
type SituationReport struct {
	// Sessions 会话摘要列表（按最近活跃降序）
	Sessions []SessionSummary
	// RecommendationHistorySummary 历史推荐记录摘要
	RecommendationHistorySummary string
	// SkillsSummary 候选 skill 摘要
	SkillsSummary string
	// CalendarEvents 即将到来的日程事件
	CalendarEvents []map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// localReadTimeoutSec 本地报告读取的整体超时（秒）
	// Python: situation_report.py:24 _LOCAL_READ_TIMEOUT = 5.0
	localReadTimeoutSec = 5
	// sessionScanBudgetSec 会话扫描的时间预算（秒）
	// Python: situation_report.py:25 _SESSION_SCAN_BUDGET = 4.0
	sessionScanBudgetSec = 4
	// maxScanSessions 单次扫描最多保留的会话数
	// Python: situation_report.py:392 _scan_sessions(max_sessions=10, ...)
	maxScanSessions = 10
	// defaultScanModePrefix 默认只扫 agent 类会话（含 agent-plan 等）
	// Python: situation_report.py:161 mode_prefix: str | None = "agent"
	defaultScanModePrefix = "agent"
	// userTextMaxRunes 每轮用户文本截断长度
	// Python: situation_report.py:320 user_text[:1000]
	userTextMaxRunes = 1000
	// assistantTextMaxRunes 每轮助手文本截断长度
	// Python: situation_report.py:321 assistant_text[:3000]
	assistantTextMaxRunes = 3000
	// skillDescMaxRunes skill 描述截断长度
	// Python: situation_report.py:337 (s.get("description", "") or "")[:80]
	skillDescMaxRunes = 80
	// recommendationHistoryMaxEntries 喂 LLM 的历史推荐条数
	// Python: situation_report.py:369 history[-10:]
	recommendationHistoryMaxEntries = 10
	// recommendationContentSummaryRunes 历史推荐话术摘要长度
	// Python: situation_report.py:375 content[:60]
	recommendationContentSummaryRunes = 60
)

// ──────────────────────────── 枚举 ────────────────────────────

// 会话扫描超时标记。
// Python: situation_report.py:177 raise TimeoutError("proactive session scan exceeded its time budget")
var errSessionScanTimeout = errors.New("proactive session scan exceeded its time budget")

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// readReportMu 保护共享的在飞读取
	// Python: situation_report.py:27 _read_lock
	readReportMu sync.Mutex
	// readReportState 共享的在飞读取（同一时刻只允许一个本地读取）
	// Python: situation_report.py:28-29 _read_future / _read_key
	readReportState *inFlightReportRead
)

// inFlightReportRead 一次在飞的本地报告读取。
// Python: situation_report.py:28 _read_future + :29 _read_key 的合并结构。
type inFlightReportRead struct {
	// key 读取键（目录 + maxRounds + modePrefix）；键变化时不复用
	key string
	// done 读取完成信号
	done chan struct{}
	// report 读到的报告；failed 为 true 时无意义
	report *SituationReport
	// failed 读取是否失败（超时/异常）；失败结果不投给后续 waiter
	failed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsEmpty 是否没有可分析的对话内容。
// Python: situation_report.py:56-58 is_empty
func (r *SituationReport) IsEmpty() bool {
	for _, s := range r.Sessions {
		if s.CompressedHistory != "" {
			return false
		}
	}
	return true
}

// MostRecentActiveSession 返回最近活跃的会话，用于推送投递。
//
// 推送投递只需要会话 id，不需要历史内容——刚发消息、助手还没回完（因而
// 尚无压缩历史）的会话仍是合法投递目标。历史内容是 LLM 决策素材的关注点
// （RenderForLLM / IsEmpty），不是投递目标选择的条件。
//
// Python: situation_report.py:60-72 most_recent_active_session
func (r *SituationReport) MostRecentActiveSession() *SessionSummary {
	var best *SessionSummary
	for i := range r.Sessions {
		s := &r.Sessions[i]
		if s.LastMessageAt <= 0 {
			continue
		}
		if best == nil || s.LastMessageAt > best.LastMessageAt {
			best = s
		}
	}
	return best
}

// FindSessionForChannel 返回指定通道上最近活跃的会话。
//
// 调用方指定目标通道时（如 cron 传 target_channel="web"），把推荐投递到该通道
// 上的会话；按 channel_id 字符串比较（两侧 trim + 小写）。无匹配返回 nil。
//
// 注意：投递目标选择不要求压缩历史——推送只需要会话 id。
//
// Python: situation_report.py:74-94 find_session_for_channel
func (r *SituationReport) FindSessionForChannel(channel string) *SessionSummary {
	if channel == "" {
		return r.MostRecentActiveSession()
	}
	target := strings.ToLower(strings.TrimSpace(channel))
	var best *SessionSummary
	for i := range r.Sessions {
		s := &r.Sessions[i]
		if strings.ToLower(strings.TrimSpace(s.ChannelID)) != target {
			continue
		}
		if s.LastMessageAt <= 0 {
			continue
		}
		if best == nil || s.LastMessageAt > best.LastMessageAt {
			best = s
		}
	}
	return best
}

// RenderForLLM 把完整报告渲染为 LLM 可读的 markdown。
// Python: situation_report.py:96-133 render_for_llm
func (r *SituationReport) RenderForLLM() string {
	parts := make([]string, 0, 8)

	// sessions 按最近活跃排序（ScanSessions 已排），第一个是当前对话
	var activeSessions []SessionSummary
	for _, s := range r.Sessions {
		if s.CompressedHistory != "" {
			activeSessions = append(activeSessions, s)
		}
	}
	if len(activeSessions) > 0 {
		// 当前对话（最近活跃 1 个）——所有推荐都基于此。
		// 不额外截断——靠 maxRounds + 每轮 user[:1000]/assistant[:3000] 控制长度。
		current := activeSessions[0]
		channelLabel := current.ChannelID
		if channelLabel == "" {
			channelLabel = "default"
		}
		title := current.Title
		if title == "" {
			title = truncateRunes(current.SessionID, 16)
		}
		parts = append(parts, "## 当前对话（所有推荐基于此）")
		parts = append(parts, "### 会话: "+title+" (channel: "+channelLabel+")")
		parts = append(parts, current.CompressedHistory)
		parts = append(parts, "")
	}

	parts = append(parts, "## 历史推荐记录（系统已推过，按推荐类型区分去重，见决策约束）")
	if r.RecommendationHistorySummary != "" {
		parts = append(parts, r.RecommendationHistorySummary)
	} else {
		parts = append(parts, "（无推荐历史）")
	}

	if len(r.CalendarEvents) > 0 {
		parts = append(parts, "")
		parts = append(parts, "## 即将到来的日程")
		for _, ev := range r.CalendarEvents {
			title := stringField(ev, "title")
			start := stringField(ev, "start")
			end := stringField(ev, "end")
			location := stringField(ev, "location")
			line := strings.TrimRight("- "+start+" ~ "+end+" "+title, " ")
			if location != "" {
				line += " @ " + location
			}
			parts = append(parts, line)
		}
	}

	if r.SkillsSummary != "" {
		parts = append(parts, "")
		parts = append(parts, "## 候选 Skill")
		parts = append(parts, r.SkillsSummary)
	}

	return strings.Join(parts, "\n")
}

// ScanSessions 扫描会话目录，为最近活跃的会话构建摘要。
//
// modePrefix 按 metadata.json 的 mode 字段做前缀过滤，默认 "agent" 只扫 agent 类
// 会话（含 agent-plan 等）；传空串表示不过滤（扫全部）。
//
// Python: situation_report.py:158 _scan_sessions
func ScanSessions(maxSessions, maxRounds int, modePrefix string) ([]SessionSummary, error) {
	return scanSessionsIn(workspace.AgentSessionsDir(), maxSessions, maxRounds, modePrefix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// scanSessionsIn 在指定会话目录内执行扫描，便于测试注入隔离目录。
//
// 单列出来是因为 workspace.AgentSessionsDir() 底层 cache 了首个取值，
// 测试无法用环境变量重定向；注入目录让扫描/过滤/压缩逻辑可被独立验证。
func scanSessionsIn(sessionsDir string, maxSessions, maxRounds int, modePrefix string) ([]SessionSummary, error) {
	if maxSessions <= 0 {
		return nil, nil
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		return nil, nil
	}

	summaries := make([]SessionSummary, 0, maxSessions)
	deadline := time.Now().Add(sessionScanBudgetSec * time.Second)

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if time.Now().After(deadline) {
			return nil, errSessionScanTimeout
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "health_check_") ||
			strings.HasPrefix(entry.Name(), "heartbeat") {
			continue
		}

		sessionID := entry.Name()
		metaPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
		if _, statErr := os.Stat(metaPath); statErr != nil {
			continue
		}

		// 历史文件：Go 侧只有 history.json（无 jsonl 双格式），
		// 用 session_history 的统一 reader，不要硬编码文件名。
		if !historyExistsIn(sessionsDir, sessionID) {
			continue
		}

		// 走 ReadSessionMetadata（带 sessionsDir 参数，可注入）而非裸读盘。
		// 生产路径下该目录就是 AgentSessionsDir()，与 GetSessionMetadata 的缓存在
		// 用户刚发消息场景上等价（本函数只读 last_message_at 做排序，不追求缓存新鲜度）；
		// 用可注入版本是为了让扫描逻辑可被测试独立验证。
		meta := swarmsession.ReadSessionMetadata(sessionsDir, sessionID)
		if len(meta) == 0 {
			continue
		}

		// 按会话 mode 过滤（agent / code / team ...）——默认只扫 agent 类
		if !matchSessionMode(meta, modePrefix) {
			continue
		}

		lastTS := floatField(meta, "last_message_at")
		if lastTS <= 0 {
			continue
		}

		var deliveryCtx map[string]any
		if raw, ok := meta["delivery_context"].(map[string]any); ok {
			// 只保留路由相关字段
			deliveryCtx = make(map[string]any, 3)
			for _, k := range []string{"channel_id", "route_metadata", "session_id"} {
				if v, exists := raw[k]; exists {
					deliveryCtx[k] = v
				}
			}
		}

		summaries = append(summaries, SessionSummary{
			SessionID:       sessionID,
			ChannelID:       stringFieldDefault(meta, "channel_id", "default"),
			Title:           stringField(meta, "title"),
			LastMessageAt:   lastTS,
			MessageCount:    int(floatField(meta, "message_count")),
			DeliveryContext: deliveryCtx,
		})
	}

	// 先按元数据排序再读历史：只有选中的会话需要压缩。
	// 空历史仍允许投递给活跃会话。
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].LastMessageAt > summaries[j].LastMessageAt
	})
	if len(summaries) > maxSessions {
		summaries = summaries[:maxSessions]
	}
	for i := range summaries {
		if time.Now().After(deadline) {
			return nil, errSessionScanTimeout
		}
		summaries[i].CompressedHistory = compressHistoryIn(sessionsDir, summaries[i].SessionID, maxRounds)
	}
	return summaries, nil
}

// scanSessionsIn 的配套：在指定目录下判定历史文件存在性。
func historyExistsIn(sessionsDir, sessionID string) bool {
	fpath := filepath.Join(sessionsDir, sessionID, "history.json")
	info, err := os.Stat(fpath)
	return err == nil && info.Mode().IsRegular()
}

// CompressHistoryForProfile 读取会话历史并压缩成 [User]/[Assistant] 轮次。
//
// 包含 proactive_recommendation 事件，让 LLM 能看到之前推过什么。
//
// Python: situation_report.py:262 _compress_history_for_profile
func CompressHistoryForProfile(sessionID string, maxRounds int) string {
	return compressHistoryIn(workspace.AgentSessionsDir(), sessionID, maxRounds)
}

// compressHistoryIn 在指定会话目录下压缩历史，便于测试注入。
func compressHistoryIn(sessionsDir, sessionID string, maxRounds int) string {
	if !historyExistsIn(sessionsDir, sessionID) {
		return ""
	}
	// 直接读文件而不走 swarmsession.ReadHistoryRecords：后者内部 historyFilePath
	// 会 MkdirAll 建目录，读路径不应有写副作用。
	fpath := filepath.Join(sessionsDir, sessionID, "history.json")
	data, err := os.ReadFile(fpath)
	if err != nil {
		return ""
	}
	if len(data) == 0 {
		return ""
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return ""
	}

	type round struct {
		user      string
		assistant string
	}
	rounds := make([]round, 0, maxRounds)

	var currentUser *string
	var assistantBuffer []string

	// 接受的助手事件类型
	acceptedEventTypes := map[string]struct{}{"chat.delta": {}, "chat.final": {}}

	for _, entry := range records {
		role := stringField(entry, "role")
		eventType := stringField(entry, "event_type")
		switch {
		case role == "user":
			if currentUser != nil && len(assistantBuffer) > 0 {
				rounds = append(rounds, round{user: *currentUser, assistant: strings.Join(assistantBuffer, "")})
			}
			text := extractContentText(entry["content"])
			currentUser = &text
			assistantBuffer = nil
		case role == "assistant":
			if _, accepted := acceptedEventTypes[eventType]; !accepted {
				continue
			}
			text := strings.TrimSpace(extractContentText(entry["content"]))
			if text != "" {
				assistantBuffer = append(assistantBuffer, text)
			}
		}
	}
	if currentUser != nil && len(assistantBuffer) > 0 {
		rounds = append(rounds, round{user: *currentUser, assistant: strings.Join(assistantBuffer, "")})
	}

	if maxRounds > 0 && len(rounds) > maxRounds {
		rounds = rounds[len(rounds)-maxRounds:]
	}

	parts := make([]string, 0, len(rounds)*2)
	for _, r := range rounds {
		parts = append(parts, "[User]: "+truncateRunes(r.user, userTextMaxRunes))
		parts = append(parts, "[Assistant]: "+truncateRunes(r.assistant, assistantTextMaxRunes))
	}
	return strings.Join(parts, "\n\n")
}

// BuildSituationReport 从会话与 profile 的当前状态构建情境报告。
//
// calendarEvents 由调用方（第 3 段的 calendar_source）拉取后传入——
// 拉取失败在那边被吞成空列表，故本函数不会因日历不可用而阻塞 tick。
//
// modePrefix 透传给 ScanSessions，按会话 mode 前缀过滤。默认 "agent" 只扫
// agent 类会话，避免 code/team 会话的技术语境污染推荐决策；空串扫全部。
//
// Python: situation_report.py:434 build_situation_report
func BuildSituationReport(
	ctx context.Context,
	maxRounds int,
	skills []map[string]any,
	modePrefix string,
	calendarEvents []map[string]any,
) *SituationReport {
	report := readLocalReportWithDeadline(ctx, maxRounds, modePrefix)
	if report == nil {
		// 可选的推荐能力绝不应拖住活跃的聊天会话
		return &SituationReport{}
	}

	skillsSummary := ""
	if len(skills) > 0 {
		skillsSummary = FormatSkillsForLLM(skills)
	}

	return &SituationReport{
		Sessions:                     report.Sessions,
		RecommendationHistorySummary: report.RecommendationHistorySummary,
		SkillsSummary:                skillsSummary,
		CalendarEvents:               calendarEvents,
	}
}

// FormatSkillsForLLM 把 skill 列表渲染为 LLM 上下文。
//
// 精简渲染——只给 skill 名 + 一句话描述（80 字符）+ 安装状态。
// tags 省略（LLM 不需要标签来选 skill，靠名字 + 描述即可）。
//
// Python: situation_report.py:328 _format_skills_for_llm
func FormatSkillsForLLM(skills []map[string]any) string {
	lines := make([]string, 0, len(skills))
	for _, s := range skills {
		name := stringField(s, "name")
		desc := truncateRunes(stringField(s, "description"), skillDescMaxRunes)
		installed, _ := s["installed"].(bool)
		source := stringField(s, "source")

		var status string
		switch {
		case installed:
			status = "[已安装]"
		case source == "builtin":
			status = "[未安装·内置技能]"
		default:
			status = "[未安装]"
		}
		lines = append(lines, "- "+name+" | "+desc+" "+status)
	}
	if len(lines) == 0 {
		return "（无候选 skill）"
	}
	return strings.Join(lines, "\n")
}

// FormatRecommendationHistory 把推荐历史渲染为 LLM 上下文。
//
// 去重锚点按类型不同：
//   - skill_recommend：target 是 skill 名（会跨场景复用），光看 skill 名看不出
//     「同 skill 换皮推」，故带 content 摘要让决策 LLM 看实际说了啥。
//   - need_exploration：target 是 LLM 自由生成的方向名，字面几乎不重复但会
//     「同主题换皮」，故带 content 摘要。
//   - task_reminder：target 是待办/事件名，未闭环时再提醒合理（不算重复），
//     靠 target + 是否闭环判断，不需要 content。
//
// 取 content 前 60 字摘要，避免 prompt 膨胀。
//
// Python: situation_report.py:353 _format_recommendation_history
func FormatRecommendationHistory(history []map[string]any) string {
	if len(history) == 0 {
		return "（无推荐历史）"
	}
	start := 0
	if len(history) > recommendationHistoryMaxEntries {
		start = len(history) - recommendationHistoryMaxEntries
	}
	lines := make([]string, 0, len(history)-start)
	for _, r := range history[start:] {
		rtype := stringFieldDefault(r, "type", "?")
		target := stringFieldDefault(r, "target", "?")
		if rtype == "skill_recommend" || rtype == "need_exploration" {
			content := strings.TrimSpace(stringField(r, "content"))
			if content != "" {
				lines = append(lines, "- ["+rtype+"] "+target+"（话术："+
					truncateRunes(content, recommendationContentSummaryRunes)+"）")
			} else {
				lines = append(lines, "- ["+rtype+"] "+target)
			}
			continue
		}
		lines = append(lines, "- ["+rtype+"] "+target)
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readLocalReportWithDeadline 在超时预算内读取本地报告，多个 waiter 共享同一次读取。
//
// 超时/取消的 waiter 不能放大 IO；挂住的 IO 也不能留住已过期的 tick。
//
// Python: situation_report.py:400 _read_local_report_with_deadline
func readLocalReportWithDeadline(ctx context.Context, maxRounds int, modePrefix string) *SituationReport {
	key := workspace.AgentSessionsDir() + "|" + strconv.Itoa(maxRounds) + "|" + modePrefix

	readReportMu.Lock()
	// 上一轮的读取可能已超时，绝不投递它的陈旧数据
	if readReportState != nil {
		select {
		case <-readReportState.done:
			readReportState = nil
		default:
		}
	}
	if readReportState != nil && readReportState.key != key {
		readReportMu.Unlock()
		return nil
	}
	current := readReportState
	if current == nil {
		current = &inFlightReportRead{key: key, done: make(chan struct{})}
		readReportState = current
		go func() {
			defer close(current.done)
			report, err := readLocalReport(maxRounds, modePrefix)
			if err != nil {
				current.failed = true
				return
			}
			current.report = report
		}()
	}
	readReportMu.Unlock()

	timer := time.NewTimer(localReadTimeoutSec * time.Second)
	defer timer.Stop()
	select {
	case <-current.done:
		if current.failed {
			logger.Warn(logComponent).Msg("local report read failed; skipping this recommendation tick")
			return nil
		}
		return current.report
	case <-timer.C:
		logger.Warn(logComponent).Msg("local report read timed out; skipping this recommendation tick")
		return nil
	case <-ctx.Done():
		return nil
	}
}

// readLocalReport 在一个隔离 goroutine 内读取本地状态，绝不占用调用方协程。
// Python: situation_report.py:386 _read_local_report
func readLocalReport(maxRounds int, modePrefix string) (*SituationReport, error) {
	sessions, err := ScanSessions(maxScanSessions, maxRounds, modePrefix)
	if err != nil {
		return nil, err
	}
	state := LoadRecommendationState("")
	return &SituationReport{
		Sessions:                     sessions,
		RecommendationHistorySummary: FormatRecommendationHistory(state.RecommendationHistory),
	}, nil
}

// matchSessionMode 按 metadata 的 mode 字段做前缀过滤，镜像 dreaming sweeper 的同名逻辑。
//
// 主动推荐面向 agent 类会话（推 skill/待办/需求探索），code/team 会话的技术语境
// 与推荐语义不匹配，混扫会污染 situation report。故默认只扫 mode 以 "agent"
// 开头的会话。modePrefix 为空串表示不过滤（扫全部）。
//
// 与 sweeper 一致：无 mode 字段的会话直接跳过——无法判断类型，按「不属 agent 类」处理。
//
// Python: situation_report.py:139 _match_session_mode
func matchSessionMode(meta map[string]any, modePrefix string) bool {
	if modePrefix == "" {
		return true
	}
	sessionMode := strings.TrimSpace(stringField(meta, "mode"))
	if sessionMode == "" {
		return false
	}
	return strings.HasPrefix(sessionMode, modePrefix)
}

// extractContentText 从 history 记录的 content 字段提取文本。
// Python: situation_report.py:286-291 _extract_text
func extractContentText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

// stringField 取 map 中的字符串字段，非字符串/缺失返回空串。
func stringField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// stringFieldDefault 取字符串字段，缺失时用默认值。
func stringFieldDefault(m map[string]any, key, defaultValue string) string {
	if s, ok := m[key].(string); ok && s != "" {
		return s
	}
	return defaultValue
}

// floatField 取 map 中的数值字段，兼容 int/float64 等数值类型。
func floatField(m map[string]any, key string) float64 {
	switch typed := m[key].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}
