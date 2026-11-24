// toolkit.go 对齐 Python discovery/toolkit.py（217 行）：增量通知基线表与 DCI 结果。
// noticeStates 为包级 map+mutex 等价实现（偏差 D-16-4-3）。
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// 通知域与上限（对齐 toolkit.py:15-17, 24）
const (
	// toolIDDomain 工具 ID 域前缀（品牌内部串，对齐 _TOOL_ID_DOMAIN）
	toolIDDomain = "openjiuwen.skill-index.v1\x00"
	// maxDescriptionChars 卡描述截断
	maxDescriptionChars = 300
	// maxSessionStates 会话基线表上限
	maxSessionStates = 1024
)

// SkillDCICommandResult 命令工具结果（模型可见文本 + 诊断明细）。
// 对齐 Python SkillDCICommandResult(str) 子类（toolkit.py:20-43）：
// model_content 进 Data["content"]，detailed_output 侧带诊断。
type SkillDCICommandResult struct {
	// ModelContent 模型可见文本
	ModelContent string
	// DetailedOutput 结构化诊断
	DetailedOutput map[string]any
}

// noticeCard 归一化卡（name, description）
type noticeCard struct{ name, description string }

// noticeState 单会话基线（对齐 _NoticeState：快照 + 锁 + prepare 上下文语义）
type noticeState struct {
	mu       sync.Mutex
	snapshot map[string]noticeCard
}

// preparedNotice 一次比对产出（对齐 _PreparedNotice：added 为 (name, description) 对；
// addedIDs 供基线扣除用）
type preparedNotice struct {
	addedIDs     []string
	added        [][2]string // (name, description)
	acknowledged bool
}

// acknowledge 投递成功标记
func (p *preparedNotice) acknowledge() { p.acknowledged = true }

// prepare 比对当前与基线；finally 语义由调用方 defer finish() 保证。
// 对齐 toolkit.py:60-67：acknowledged 或无 added → 基线全量推进；
// 否则基线扣掉 added（待下轮重试）。
func (s *noticeState) prepare(current map[string]noticeCard) (*preparedNotice, func()) {
	// 读锁只用于取基线快照（Go 非重入锁，finally 更新由 finish 二次加锁）
	s.mu.Lock()
	previous := s.snapshot
	s.mu.Unlock()
	p := &preparedNotice{}
	for wid, card := range current {
		if _, existed := previous[wid]; !existed {
			p.addedIDs = append(p.addedIDs, wid)
			p.added = append(p.added, [2]string{card.name, card.description})
		}
	}
	finish := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current != nil {
			if p.acknowledged || len(p.addedIDs) == 0 {
				s.snapshot = current // 基线全量推进
			} else {
				addedSet := map[string]bool{}
				for _, a := range p.addedIDs {
					addedSet[a] = true
				}
				next := map[string]noticeCard{}
				for wid, card := range current {
					if !addedSet[wid] {
						next[wid] = card
					}
				}
				s.snapshot = next // added 留待重试
			}
		}
	}
	return p, finish
}

// 包级基线表（D-16-4-3：Python 模块级 OrderedDict LRU 1024）
var (
	noticeStatesMu sync.Mutex
	noticeStates   = map[string]*noticeState{}
	noticeOrder    []string
)

// toolID 工具标识（scope × tool_name 双维哈希）。
// 对齐 Python _tool_id（toolkit.py:125-131）。
func toolID(sessionScope, toolName string) (string, error) {
	scope := strings.TrimSpace(sessionScope)
	name := strings.TrimSpace(toolName)
	if scope == "" || name == "" {
		return "", fmt.Errorf("session_scope and discovery_tool_name must be non-empty")
	}
	sum := sha256.Sum256([]byte(toolIDDomain + name + "\x00" + scope))
	return fmt.Sprintf("%s__%s", name, hex.EncodeToString(sum[:])), nil
}

// noticeStateOf 取/建基线（LRU 淘汰最旧）。
// 对齐 toolkit.py:134-145。
func noticeStateOf(id string, snapshot map[string]noticeCard) *noticeState {
	noticeStatesMu.Lock()
	defer noticeStatesMu.Unlock()
	state, ok := noticeStates[id]
	if !ok {
		state = &noticeState{snapshot: snapshot}
		noticeStates[id] = state
		noticeOrder = append(noticeOrder, id)
	}
	// 触达前移（order 尾部为最新）
	for i, k := range noticeOrder {
		if k == id {
			noticeOrder = append(noticeOrder[:i], noticeOrder[i+1:]...)
			break
		}
	}
	noticeOrder = append(noticeOrder, id)
	for len(noticeOrder) > maxSessionStates {
		oldest := noticeOrder[0]
		noticeOrder = noticeOrder[1:]
		delete(noticeStates, oldest)
	}
	return state
}

// normalizeCards 卡表归一化（name 回落 worker_id、描述截断 300、HTML 转义）。
// 对齐 toolkit.py:148-161。
func normalizeCards(rawCards map[string]map[string]string) map[string]noticeCard {
	cards := map[string]noticeCard{}
	for rawID, rawCard := range rawCards {
		workerID := strings.TrimSpace(rawID)
		if workerID == "" || rawCard == nil {
			continue
		}
		name := safeText(rawCard["name"], workerID)
		description := shortenWithPlaceholder(safeText(rawCard["description"], name), maxDescriptionChars)
		cards[workerID] = noticeCard{name, description}
	}
	return cards
}

// safeText 空白折叠 + HTML 转义（对齐 _safe_text）
func safeText(value, def string) string {
	v := value
	if v == "" {
		v = def
	}
	v = strings.Join(strings.Fields(v), " ")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	return v
}

// shortenWithPlaceholder textwrap.shorten 等价（超长截断 + "..." 占位）
func shortenWithPlaceholder(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	return string(runes[:width-3]) + "..."
}

// InitializeIncrementalSkillNoticeState 建基线不产出增量。
// 对齐 toolkit.py:86-88。
func InitializeIncrementalSkillNoticeState(sessionScope string, rawCards map[string]map[string]string) error {
	id, err := toolID(sessionScope, SkillIndexToolName)
	if err != nil {
		return err
	}
	noticeStateOf(id, normalizeCards(rawCards))
	return nil
}

// ConsumeIncrementalSkillReminder 消费一次新增通知（渲染成功才推进基线）。
// 对齐 toolkit.py:91-107。
func ConsumeIncrementalSkillReminder(sessionScope string, rawCards map[string]map[string]string, maxChars int) (string, error) {
	if maxChars <= 0 {
		return "", fmt.Errorf("max_chars must be positive")
	}
	id, err := toolID(sessionScope, SkillIndexToolName)
	if err != nil {
		return "", err
	}
	current := normalizeCards(rawCards)
	state := noticeStateOf(id, current)
	prepared, finish := state.prepare(current)
	reminder := renderReminder(prepared.added, maxChars, SkillIndexToolName)
	if reminder != "" {
		prepared.acknowledge()
	}
	finish()
	return reminder, nil
}

// ClearIncrementalSkillNoticeStates 清场（测试专用）。
// 对齐 toolkit.py:110-113。
func ClearIncrementalSkillNoticeStates() {
	noticeStatesMu.Lock()
	defer noticeStatesMu.Unlock()
	noticeStates = map[string]*noticeState{}
	noticeOrder = nil
}

// IncrementalNoticeSession 会话内通知会话（拼进结果、fit 才交付）。
// 对齐 toolkit.py:117-142。
type IncrementalNoticeSession struct {
	maxChars int
	state    *noticeState
}

// NewIncrementalNoticeSession 构造（对齐 :120-125）。
func NewIncrementalNoticeSession(sessionScope string, rawCards map[string]map[string]string, maxChars int) (*IncrementalNoticeSession, error) {
	if maxChars <= 0 {
		return nil, fmt.Errorf("incremental_notice_max_chars must be positive")
	}
	id, err := toolID(sessionScope, SkillIndexToolName)
	if err != nil {
		return nil, err
	}
	return &IncrementalNoticeSession{maxChars: maxChars, state: noticeStateOf(id, normalizeCards(rawCards))}, nil
}

// Append 通知拼进输出（超预算放弃本次投递）。
// 对齐 toolkit.py:126-142：无 added 返回原输出；拼完超 output_budget 放弃
// （acknowledge 不置位 → 基线保留 added 待重试）。
func (s *IncrementalNoticeSession) Append(output string, rawCards map[string]map[string]string, outputBudget int) (string, string) {
	current := normalizeCards(rawCards)
	prepared, finish := s.state.prepare(current)
	reminder := renderReminder(prepared.added, s.maxChars, SkillIndexToolName)
	if reminder == "" {
		finish()
		return output, ""
	}
	combined := output
	if combined == "" {
		combined = reminder
	} else {
		combined = output + "\n\n" + reminder
	}
	if outputBudget > 0 && len([]rune(combined)) > outputBudget {
		finish() // acknowledged 未置位 → added 保留
		return output, ""
	}
	prepared.acknowledge()
	finish()
	return combined, reminder
}

// renderReminder 通知渲染（全量行 / 超长回落计数摘要）。
// 对齐 toolkit.py:170-194。
func renderReminder(added [][2]string, maxChars int, toolName string) string {
	if len(added) == 0 {
		return ""
	}
	rows := make([]string, 0, len(added))
	sorted := append([][2]string{}, added...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i][0]) < strings.ToLower(sorted[j][0])
	})
	for _, row := range sorted {
		rows = append(rows, fmt.Sprintf("- %s: %s", row[0], row[1]))
	}
	full := "<system-reminder>\n" +
		"The following skills are newly available for use with the Skill tool:\n\n" +
		strings.Join(rows, "\n") + "\n" +
		"</system-reminder>"
	if len([]rune(full)) <= maxChars {
		return full
	}
	return "<system-reminder>\n" +
		fmt.Sprintf("%d new skills are available. Use `%s` to search the current catalog.\n", len(added), toolName) +
		"</system-reminder>"
}
