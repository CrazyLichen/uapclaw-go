// Python 源：jiuwenswarm/server/runtime/team_binding_store.py（394 行 1:1）
//
// 持久化团队实体绑定。有意保持小而基于本地文件：记录稳定的用户可见团队实体名
// 与构建 runtime spec 的模板；运行时状态在别处派生，不在此持久化。
//
// import 即自注册到 common 注入点（Python :384-392）——Go 侧经包级 init 把
// store 模块对象注册到 common.Provider("team_binding_store")，消费方取
// Get/GetTeamSelection 等方法集。

package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	sessionpaths "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 常量 ────────────────────────────

// teamBindingLogComponent 组件日志。
const teamBindingLogComponent = logger.ComponentAgentServer

// teamNameRE 合法团队名：1-64 字符，无路径分隔符与控制字符。
// Python: TEAM_NAME_RE (team_binding_store.py:24)
var teamNameRE = regexp.MustCompile(`^[^/\\\x00-\x1f\x7f]{1,64}$`)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamBindingStoreError 绑定校验或持久化错误。
// Python: TeamBindingStoreError (team_binding_store.py:27-32)
type TeamBindingStoreError struct {
	// Msg 错误消息
	Msg string
	// Code 错误码（BAD_REQUEST / CONFLICT / NOT_FOUND / INTERNAL_ERROR）
	Code string
}

// Error 实现 error。
func (e *TeamBindingStoreError) Error() string { return e.Msg }

// TeamBinding 一条团队绑定。
// Python: TeamBinding dataclass (team_binding_store.py:35-89)
type TeamBinding struct {
	TeamName         string
	TemplateID       string
	CreatedAt        float64
	UpdatedAt        float64
	SessionIDs       []string
	LastSessionID    string
	Legacy           bool
	TemplateSnapshot map[string]any
	// ModelSelection 稳定的业务级模型选择引用。运行时模型池仍在 Team
	// spec/checkpoint；该引用在每次冷构建时重新解析。
	ModelSelection map[string]any
}

// teamBindingFromDict dict → TeamBinding。Python: from_dict (:50-75)
func teamBindingFromDict(data map[string]any) TeamBinding {
	var sessionIDs []string
	if raw, ok := data["session_ids"].([]any); ok {
		for _, item := range raw {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				sessionIDs = append(sessionIDs, s)
			}
		}
	}
	var snapshot, selection map[string]any
	if m, ok := data["template_snapshot"].(map[string]any); ok {
		snapshot = m
	}
	if m, ok := data["model_selection"].(map[string]any); ok {
		selection = m
	}
	return TeamBinding{
		TeamName:         strings.TrimSpace(stringOf(data["team_name"])),
		TemplateID:       strings.TrimSpace(stringOf(data["template_id"])),
		CreatedAt:        floatOf(data["created_at"]),
		UpdatedAt:        floatOf(data["updated_at"]),
		SessionIDs:       sessionIDs,
		LastSessionID:    strings.TrimSpace(stringOf(data["last_session_id"])),
		Legacy:           data["legacy"] == true,
		TemplateSnapshot: snapshot,
		ModelSelection:   selection,
	}
}

// ToDict TeamBinding → dict（不含 template_snapshot，Python :77-89 同款）。
func (b TeamBinding) ToDict() map[string]any {
	payload := map[string]any{
		"team_name":       b.TeamName,
		"template_id":     b.TemplateID,
		"created_at":      b.CreatedAt,
		"updated_at":      b.UpdatedAt,
		"session_ids":     b.SessionIDs,
		"last_session_id": b.LastSessionID,
		"legacy":          b.Legacy,
	}
	if b.ModelSelection != nil {
		payload["model_selection"] = b.ModelSelection
	}
	return payload
}

// replaceBoundSessions 换会话绑定的不可变重建。Python: _replace_bound_sessions (:92-109)
func replaceBoundSessions(binding TeamBinding, sessionIDs []string, lastSessionID string, updatedAt float64) TeamBinding {
	return TeamBinding{
		TeamName:         binding.TeamName,
		TemplateID:       binding.TemplateID,
		CreatedAt:        binding.CreatedAt,
		UpdatedAt:        updatedAt,
		SessionIDs:       sessionIDs,
		LastSessionID:    lastSessionID,
		Legacy:           binding.Legacy,
		TemplateSnapshot: binding.TemplateSnapshot,
		ModelSelection:   binding.ModelSelection,
	}
}

// ValidateTeamName 校验并归一团队名。
// Python: validate_team_name (team_binding_store.py:112-121)
func ValidateTeamName(teamName string) (string, error) {
	normalized := strings.TrimSpace(teamName)
	if normalized == "" {
		return "", &TeamBindingStoreError{Msg: "team_name is required", Code: "BAD_REQUEST"}
	}
	if normalized == "." || normalized == ".." || !teamNameRE.MatchString(normalized) {
		return "", &TeamBindingStoreError{
			Msg:  "team_name must be 1-64 characters without path separators or control characters",
			Code: "BAD_REQUEST",
		}
	}
	return normalized, nil
}

// TeamBindingStore 文件支撑的团队绑定目录。
// Python: TeamBindingStore (team_binding_store.py:124-364)
type TeamBindingStore struct {
	mu   sync.Mutex
	path string
}

// NewTeamBindingStore 构造；path 为空走默认布局 + legacy 迁移。
// Python: __init__ (:127-134)
func NewTeamBindingStore(path string) *TeamBindingStore {
	store := &TeamBindingStore{}
	if path == "" {
		store.path = filepath.Join(agent_teams.GetAgentTeamsHome(), "bindings.json")
		store.migrateLegacyFile(filepath.Join(sessionpaths.GetAgentRootDir(), "teams", "bindings.json"))
	} else {
		store.path = path
	}
	return store
}

// migrateLegacyFile 新目录缺失时搬移旧 agent/teams 目录。
// Python: _migrate_legacy_file (:136-146)
func (s *TeamBindingStore) migrateLegacyFile(legacyPath string) {
	if _, err := os.Stat(s.path); err == nil {
		return
	}
	if _, err := os.Stat(legacyPath); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	if err := os.Rename(legacyPath, s.path); err != nil {
		// 另一进程可能已完成迁移。
		if _, err := os.Stat(s.path); err != nil {
			logger.Warn(teamBindingLogComponent).Err(err).
				Msg("[team-binding] legacy migration failed")
		}
	}
}

// Path 目录文件路径。Python: path property (:148-150)
func (s *TeamBindingStore) Path() string { return s.path }

// List 全部绑定（updated_at 降序）。Python: list (:152-155)
func (s *TeamBindingStore) List() []TeamBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	bindings := make([]TeamBinding, 0, len(data))
	for _, binding := range data {
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].UpdatedAt > bindings[j].UpdatedAt })
	return bindings
}

// Get 按名取绑定；缺失得 nil。Python: get (:157-162)
func (s *TeamBindingStore) Get(teamName string) *TeamBinding {
	normalized := strings.TrimSpace(teamName)
	if normalized == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if binding, ok := s.readUnlocked()[normalized]; ok {
		b := binding
		return &b
	}
	return nil
}

// Create 创建绑定；重名 CONFLICT。
// Python: create (:164-192)
func (s *TeamBindingStore) Create(teamName, templateID string, templateSnapshot map[string]any, legacy bool) (TeamBinding, error) {
	normalizedName, err := ValidateTeamName(teamName)
	if err != nil {
		return TeamBinding{}, err
	}
	normalizedTemplate := strings.TrimSpace(templateID)
	if normalizedTemplate == "" {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "template_id is required", Code: "BAD_REQUEST"}
	}
	now := float64(time.Now().UnixMilli()) / 1000
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	if _, exists := data[normalizedName]; exists {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "team_name already exists", Code: "CONFLICT"}
	}
	binding := TeamBinding{
		TeamName:         normalizedName,
		TemplateID:       normalizedTemplate,
		CreatedAt:        now,
		UpdatedAt:        now,
		Legacy:           legacy,
		TemplateSnapshot: templateSnapshot,
	}
	data[normalizedName] = binding
	if wErr := s.writeUnlocked(data); wErr != nil {
		return TeamBinding{}, wErr
	}
	return binding, nil
}

// BindSession 绑定会话（同会话从其它团解除）。
// Python: bind_session (:194-240)
func (s *TeamBindingStore) BindSession(teamName, sessionID string) (TeamBinding, error) {
	normalizedName := strings.TrimSpace(teamName)
	normalizedSession := strings.TrimSpace(sessionID)
	if normalizedName == "" {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "team_name is required", Code: "BAD_REQUEST"}
	}
	if normalizedSession == "" {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "session_id is required", Code: "BAD_REQUEST"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	binding, ok := data[normalizedName]
	if !ok {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "team binding not found", Code: "NOT_FOUND"}
	}
	now := float64(time.Now().UnixMilli()) / 1000
	changed := false
	for candidateName, candidate := range data {
		if candidateName == normalizedName || !containsStr(candidate.SessionIDs, normalizedSession) {
			continue
		}
		remaining := []string{}
		for _, item := range candidate.SessionIDs {
			if item != normalizedSession {
				remaining = append(remaining, item)
			}
		}
		lastSessionID := candidate.LastSessionID
		if lastSessionID == normalizedSession {
			if len(remaining) > 0 {
				lastSessionID = remaining[len(remaining)-1]
			} else {
				lastSessionID = ""
			}
		}
		data[candidateName] = replaceBoundSessions(candidate, remaining, lastSessionID, now)
		changed = true
	}
	sessionIDs := append([]string{}, binding.SessionIDs...)
	if !containsStr(sessionIDs, normalizedSession) {
		sessionIDs = append(sessionIDs, normalizedSession)
		changed = true
	}
	if binding.LastSessionID != normalizedSession {
		changed = true
	}
	if changed {
		data[normalizedName] = replaceBoundSessions(binding, sessionIDs, normalizedSession, now)
		if wErr := s.writeUnlocked(data); wErr != nil {
			return TeamBinding{}, wErr
		}
	}
	updated := data[normalizedName]
	return updated, nil
}

// GetTeamSelection 返回团队持久化的稳定模型选择。Python: get_team_selection (:242-245)
func (s *TeamBindingStore) GetTeamSelection(teamName string) map[string]any {
	binding := s.Get(teamName)
	if binding != nil && binding.ModelSelection != nil {
		return binding.ModelSelection
	}
	return nil
}

// SetTeamSelection 持久化校验过的稳定模型选择。Python: set_team_selection (:247-279)
func (s *TeamBindingStore) SetTeamSelection(teamName string, selection map[string]any) (TeamBinding, error) {
	normalizedName, err := ValidateTeamName(teamName)
	if err != nil {
		return TeamBinding{}, err
	}
	// Python: ModelSelection.model_validate（model_selection.py:10-38）——
	// 23.4d 已落 swarmcommon.ModelSelectionFromMap（map 还原 + 三约束校验），
	// e18 撤销：结构化校验直连，normalized 以 model_dump(exclude_none=True)
	// 等价形态回存。
	if selection == nil {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "invalid model selection: empty", Code: "BAD_REQUEST"}
	}
	ms, msErr := swarmcommon.ModelSelectionFromMap(selection)
	if msErr != nil {
		return TeamBinding{}, &TeamBindingStoreError{
			Msg:  fmt.Sprintf("invalid model selection: %v", msErr),
			Code: "BAD_REQUEST",
		}
	}
	selection = ms.ToMap() // Python model_dump() 等价
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	binding, ok := data[normalizedName]
	if !ok {
		return TeamBinding{}, &TeamBindingStoreError{Msg: "team binding not found", Code: "NOT_FOUND"}
	}
	if sameMap(binding.ModelSelection, selection) {
		return binding, nil
	}
	updated := TeamBinding{
		TeamName:         binding.TeamName,
		TemplateID:       binding.TemplateID,
		CreatedAt:        binding.CreatedAt,
		UpdatedAt:        float64(time.Now().UnixMilli()) / 1000,
		SessionIDs:       binding.SessionIDs,
		LastSessionID:    binding.LastSessionID,
		Legacy:           binding.Legacy,
		TemplateSnapshot: binding.TemplateSnapshot,
		ModelSelection:   selection,
	}
	data[normalizedName] = updated
	if wErr := s.writeUnlocked(data); wErr != nil {
		return TeamBinding{}, wErr
	}
	return updated, nil
}

// UnbindSession 解绑会话（可跨全部团）。Python: unbind_session (:281-312)
func (s *TeamBindingStore) UnbindSession(sessionID, teamName string) (*TeamBinding, error) {
	normalizedSession := strings.TrimSpace(sessionID)
	if normalizedSession == "" {
		return nil, &TeamBindingStoreError{Msg: "session_id is required", Code: "BAD_REQUEST"}
	}
	normalizedName := strings.TrimSpace(teamName)
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	candidates := []string{}
	if normalizedName != "" {
		candidates = append(candidates, normalizedName)
	} else {
		for name := range data {
			candidates = append(candidates, name)
		}
		sort.Strings(candidates)
	}
	var updatedBinding *TeamBinding
	now := float64(time.Now().UnixMilli()) / 1000
	for _, candidate := range candidates {
		binding, ok := data[candidate]
		if !ok || !containsStr(binding.SessionIDs, normalizedSession) {
			continue
		}
		sessionIDs := []string{}
		for _, item := range binding.SessionIDs {
			if item != normalizedSession {
				sessionIDs = append(sessionIDs, item)
			}
		}
		lastSessionID := binding.LastSessionID
		if lastSessionID == normalizedSession {
			if len(sessionIDs) > 0 {
				lastSessionID = sessionIDs[len(sessionIDs)-1]
			} else {
				lastSessionID = ""
			}
		}
		updated := replaceBoundSessions(binding, sessionIDs, lastSessionID, now)
		data[binding.TeamName] = updated
		if updatedBinding == nil {
			u := updated
			updatedBinding = &u
		}
	}
	if updatedBinding != nil {
		if wErr := s.writeUnlocked(data); wErr != nil {
			return nil, wErr
		}
	}
	return updatedBinding, nil
}

// Delete 删除绑定。Python: delete (:314-324)
func (s *TeamBindingStore) Delete(teamName string) bool {
	normalized := strings.TrimSpace(teamName)
	if normalized == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readUnlocked()
	_, existed := data[normalized]
	if existed {
		delete(data, normalized)
		if wErr := s.writeUnlocked(data); wErr != nil {
			return false
		}
	}
	return existed
}

// readUnlocked 读目录。Python: _read_unlocked (:326-348)
func (s *TeamBindingStore) readUnlocked() map[string]TeamBinding {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]TeamBinding{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		// Python: 读失败 → TeamBindingStoreError INTERNAL_ERROR。Go 侧沿用。
		logger.Warn(teamBindingLogComponent).Err(err).
			Msg("[team-binding] failed to read team bindings")
		return map[string]TeamBinding{}
	}
	items, _ := raw["teams"].(map[string]any)
	if items == nil {
		items = raw
	}
	result := map[string]TeamBinding{}
	for key, value := range items {
		m, ok := value.(map[string]any)
		if !ok {
			continue
		}
		binding := teamBindingFromDict(m)
		if binding.TeamName == "" {
			binding = teamBindingFromDict(mergeKey(m, key))
		}
		if binding.TeamName != "" {
			result[binding.TeamName] = binding
		}
	}
	return result
}

// writeUnlocked 原子写目录（tmp + rename）。Python: _write_unlocked (:350-364)
func (s *TeamBindingStore) writeUnlocked(data map[string]TeamBinding) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	teams := map[string]any{}
	for _, name := range names {
		teams[name] = data[name].ToDict()
	}
	payload, err := json.MarshalIndent(map[string]any{"version": 1, "teams": teams}, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// ──────────────────────────── 进程级单例（:367-377） ────────────────────────────

var (
	defaultStoreOnce sync.Once
	defaultStore     *TeamBindingStore
)

// GetTeamBindingStore 进程级默认 store。
// Python: get_team_binding_store (:371-377)
func GetTeamBindingStore() *TeamBindingStore {
	defaultStoreOnce.Do(func() {
		defaultStore = NewTeamBindingStore("")
		// Python: _register_common_provider (:384-392)——import 即注册到
		// common 注入点。Go 侧注册 store 本体，消费方断言方法集。
		common.Register("team_binding_store", defaultStore)
	})
	return defaultStore
}

// ──────────────────────────── 小助手 ────────────────────────────

// floatOf any → float64（缺省 0）。
func floatOf(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// containsStr 列表包含判断。
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// sameMap 浅相等判断（Python == 对 dict）。
func sameMap(a, b map[string]any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}

// mergeKey 团队名键回填（Python {**value, "team_name": key}）。
func mergeKey(m map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	out["team_name"] = key
	return out
}
