package runtime

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	serversession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionGetInput 选择一个由请求方 Runtime 渠道拥有的会话。
// Python: session_catalog.py:30-35 SessionGetInput
type SessionGetInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
}

// SessionListInput 选择某 Runtime 渠道拥有的一页会话。
// Python: session_catalog.py:38-45 SessionListInput
type SessionListInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// Limit 每页条数（默认 20，范围 1..200）
	Limit int
	// Offset 偏移量（非负）
	Offset int
	// Search 搜索串（去空白后按词与匹配，最长 200）
	Search string
}

// SessionSummary 不含投递信息与渠道私有元数据的稳定会话事实。
// Python: session_catalog.py:48-77 SessionSummary
type SessionSummary struct {
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识（小写）
	ChannelID string
	// Title 标题
	Title string
	// Mode 模式（单 Agent 权威模式；include_team 时可为团队模式）
	Mode string
	// WorkMode work 或 code（小写）
	WorkMode string
	// ProjectID 工程标识
	ProjectID string
	// ProjectDir 工程目录
	ProjectDir string
	// Model 模型
	Model string
	// CreatedAt 创建时间戳
	CreatedAt float64
	// LastMessageAt 最后消息时间戳
	LastMessageAt float64
	// MessageCount 消息条数
	MessageCount int
}

// SessionListResult 经所有权与单 Agent 过滤后的一页不可变结果。
// Python: session_catalog.py:80-95 SessionListResult
type SessionListResult struct {
	// Sessions 当前页会话
	Sessions []SessionSummary
	// Total 过滤后总数
	Total int
	// Limit 生效的每页条数
	Limit int
	// Offset 生效的偏移量
	Offset int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultSessionLimit 默认每页条数
	// Python: session_catalog.py:17 _DEFAULT_LIMIT
	DefaultSessionLimit = 20

	// MaxSessionLimit 每页条数上限
	// Python: session_catalog.py:18 _MAX_LIMIT
	MaxSessionLimit = 200

	// maxSessionSearchLength 搜索串长度上限
	// Python: session_catalog.py:253 len(request.search) > 200
	maxSessionSearchLength = 200

	// unrestrictedSessionLimit 取全量时使用的上限
	//
	// 上游 collect_all_sessions_metadata() 不分页，本包复用的
	// GetAllSessionsMetadata(limit, offset) 自带分页，故以一个明确充分大的
	// 上限取全量；超出该值属于异常规模，需要另行设计分页查询。
	unrestrictedSessionLimit = 1 << 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrSessionChannelRequired 渠道标识缺失
	// Python: session_catalog.py:100-101 code=BAD_REQUEST
	ErrSessionChannelRequired = errors.New("channel_id is required")
	// ErrSessionIDRequired 会话标识缺失
	// Python: session_catalog.py:107-108 code=BAD_REQUEST
	ErrSessionIDRequired = errors.New("session_id is required")
	// ErrSessionIDInvalid 会话标识不合法
	// Python: session_catalog.py:112-113 code=BAD_REQUEST
	ErrSessionIDInvalid = errors.New("invalid session_id")
	// ErrSessionLimitInvalid 每页条数越界
	// Python: session_catalog.py:124-129 code=BAD_REQUEST
	ErrSessionLimitInvalid = errors.New("limit must be between 1 and 200")
	// ErrSessionOffsetInvalid 偏移量非法
	// Python: session_catalog.py:130-134 code=BAD_REQUEST
	ErrSessionOffsetInvalid = errors.New("offset must be a non-negative integer")
	// ErrSessionSearchInvalid 搜索串非法
	// Python: session_catalog.py:253-254 code=BAD_REQUEST
	ErrSessionSearchInvalid = errors.New("invalid session search")
	// ErrSessionGetInputRequired 查询输入类型不对
	// Python: session_catalog.py:226-227 code=BAD_REQUEST
	ErrSessionGetInputRequired = errors.New("session get input is required")
	// ErrSessionListInputRequired 列表输入类型不对
	// Python: session_catalog.py:247-248 code=BAD_REQUEST
	ErrSessionListInputRequired = errors.New("session list input is required")
	// ErrSessionReadFailed 读取会话失败
	// Python: session_catalog.py:235-237 / :288-290 code=READ_FAILED
	ErrSessionReadFailed = errors.New("failed to read session")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionListInput 构造列表输入并补默认值。
// Python: session_catalog.py:38-45 limit=_DEFAULT_LIMIT, offset=0
func NewSessionListInput(channelID string) SessionListInput {
	return SessionListInput{ChannelID: channelID, Limit: DefaultSessionLimit, Offset: 0}
}

// GetSession 读取一个拥有的单 Agent 会话，不写回元数据。
// Python: session_catalog.py:224-240 get_session
func GetSession(request SessionGetInput) (*SessionSummary, error) {
	// Python:228-229 归一化
	channelID, err := normalizeChannelID(request.ChannelID)
	if err != nil {
		return nil, err
	}
	sessionID, err := normalizeSessionID(request.SessionID)
	if err != nil {
		return nil, err
	}

	// Python:230-237 读取元数据；异常统一为 READ_FAILED
	metadata, err := readSessionMetadata(sessionID)
	if err != nil {
		return nil, err
	}
	// Python:238-239 空元数据返回 nil
	if len(metadata) == 0 {
		return nil, nil
	}
	// Python:240 投影（不含团队）
	return projectSession(metadata, channelID, false), nil
}

// ListSessions 列出拥有的会话；单 Agent 范围是 SDK 调用的默认。
// Python: session_catalog.py:243-304 list_sessions
func ListSessions(request SessionListInput, includeTeam bool, readOnly bool) (SessionListResult, error) {
	// Python:249 归一化渠道
	channelID, err := normalizeChannelID(request.ChannelID)
	if err != nil {
		return SessionListResult{}, err
	}
	// Python:252 分页参数校验
	limit, offset, err := validatePage(request.Limit, request.Offset)
	if err != nil {
		return SessionListResult{}, err
	}
	// Python:253-255 搜索串校验与词切分
	if len([]rune(request.Search)) > maxSessionSearchLength {
		return SessionListResult{}, ErrSessionSearchInvalid
	}
	searchTerms := strings.Fields(strings.ToLower(request.Search))

	// Python:256-284 收集并过滤
	rawSessions, err := collectSessionMetadata()
	if err != nil {
		return SessionListResult{}, err
	}
	sessions := make([]SessionSummary, 0, len(rawSessions))
	for _, metadata := range rawSessions {
		projected := projectSession(metadata, channelID, includeTeam)
		if projected == nil {
			continue
		}
		if len(searchTerms) > 0 {
			searchable := strings.ToLower(strings.Join([]string{
				projected.SessionID, projected.Title, projected.ProjectID,
				projected.ProjectDir, projected.Mode,
			}, " "))
			matchedAll := true
			for _, term := range searchTerms {
				if !strings.Contains(searchable, term) {
					matchedAll = false
					break
				}
			}
			if !matchedAll {
				continue
			}
		}
		sessions = append(sessions, *projected)
	}

	// Python:292-297 排序：last_message_at 降序，同值按 session_id 升序
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].LastMessageAt != sessions[j].LastMessageAt {
			return sessions[i].LastMessageAt > sessions[j].LastMessageAt
		}
		return sessions[i].SessionID < sessions[j].SessionID
	})

	// Python:298-304 切片分页
	total := len(sessions)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]SessionSummary, 0, end-offset)
	page = append(page, sessions[offset:end]...)

	return SessionListResult{Sessions: page, Total: total, Limit: limit, Offset: offset}, nil
}

// SessionSummaryToDict 返回会话事实的 JSON 兼容投影。
// Python: session_catalog.py:64-77 to_dict
func SessionSummaryToDict(summary SessionSummary) map[string]any {
	return map[string]any{
		"session_id":      summary.SessionID,
		"channel_id":      summary.ChannelID,
		"title":           summary.Title,
		"mode":            summary.Mode,
		"work_mode":       summary.WorkMode,
		"project_id":      summary.ProjectID,
		"project_dir":     summary.ProjectDir,
		"model":           summary.Model,
		"created_at":      summary.CreatedAt,
		"last_message_at": summary.LastMessageAt,
		"message_count":   summary.MessageCount,
	}
}

// SessionListResultToDict 返回结果页的 JSON 兼容投影。
// Python: session_catalog.py:89-95 to_dict
func SessionListResultToDict(result SessionListResult) map[string]any {
	sessions := make([]map[string]any, 0, len(result.Sessions))
	for _, item := range result.Sessions {
		sessions = append(sessions, SessionSummaryToDict(item))
	}
	return map[string]any{
		"sessions": sessions,
		"total":    result.Total,
		"limit":    result.Limit,
		"offset":   result.Offset,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeChannelID 归一化渠道标识。
// Python: session_catalog.py:98-102 _normalize_channel_id
func normalizeChannelID(value string) (string, error) {
	channelID := strings.ToLower(strings.TrimSpace(value))
	if channelID == "" {
		return "", ErrSessionChannelRequired
	}
	return channelID, nil
}

// normalizeSessionID 归一化并校验会话标识。
// Python: session_catalog.py:105-114 _normalize_session_id
func normalizeSessionID(value string) (string, error) {
	sessionID := strings.TrimSpace(value)
	if sessionID == "" {
		return "", ErrSessionIDRequired
	}
	if !isSafeSessionID(sessionID) {
		return "", ErrSessionIDInvalid
	}
	return sessionID, nil
}

// isSafeSessionID 会话标识是否合法。
// Python: session_catalog.py:117-120 _is_safe_session_id
func isSafeSessionID(value string) bool { return common.IsValidSessionID(value) }

// validatePage 校验分页参数。
// Python: session_catalog.py:123-135 _validate_page
//
// 上游拒绝 bool（Python 中 bool 是 int 子类）；Go 侧 Limit/Offset 为 int 类型，
// 该风险由类型系统承担。
func validatePage(limit, offset int) (int, int, error) {
	if limit < 1 || limit > MaxSessionLimit {
		return 0, 0, ErrSessionLimitInvalid
	}
	if offset < 0 {
		return 0, 0, ErrSessionOffsetInvalid
	}
	return limit, offset, nil
}

// safeFloat 安全浮点转换，bool 与非数值归零。
// Python: session_catalog.py:138-144 _safe_float
func safeFloat(value any) float64 {
	switch typed := value.(type) {
	case bool:
		return 0
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

// safeInt 安全整数转换，bool 与非数值归零。
// Python: session_catalog.py:147-153 _safe_int
func safeInt(value any) int {
	switch typed := value.(type) {
	case bool:
		return 0
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return int(parsed)
	}
	return 0
}

// singleAgentMode 归一化后若为单 Agent 模式则返回，否则空串。
// Python: session_catalog.py:160-163 _single_agent_mode
func singleAgentMode(value any) string {
	raw, ok := value.(string)
	if !ok {
		return ""
	}
	normalized := schema.CanonicalizeMode(raw)
	mode := strings.TrimSpace(normalized)
	if mode == "" {
		return ""
	}
	if !schema.IsSingleAgentMode(mode) {
		return ""
	}
	return mode
}

// projectSession 把元数据投影为会话摘要；不合规返回 nil。
// Python: session_catalog.py:166-197 _project_session
func projectSession(metadata map[string]any, channelID string, includeTeam bool) *SessionSummary {
	// Python:172-174 渠道必须匹配
	itemChannel := strings.ToLower(safeTextFromAny(metadata["channel_id"]))
	if itemChannel != channelID {
		return nil
	}

	// Python:175-179 单 Agent 模式；include_team 时容许团队模式
	mode := singleAgentMode(metadata["mode"])
	if mode == "" && includeTeam {
		if raw, ok := metadata["mode"].(string); ok {
			normalized := schema.CanonicalizeMode(raw)
			if schema.IsTeamMode(normalized) {
				mode = strings.TrimSpace(normalized)
			}
		}
	}
	// Python:180-181 无模式则跳过（团队模式默认被过滤掉）
	if mode == "" {
		return nil
	}

	// Python:182-184 会话 ID 必须存在且合法
	sessionID := safeTextFromAny(metadata["session_id"])
	if sessionID == "" || !isSafeSessionID(sessionID) {
		return nil
	}

	// Python:185-197 逐字段投影
	return &SessionSummary{
		SessionID:     sessionID,
		ChannelID:     itemChannel,
		Title:         safeTextFromAny(metadata["title"]),
		Mode:          mode,
		WorkMode:      strings.ToLower(safeTextFromAny(metadata["work_mode"])),
		ProjectID:     safeTextFromAny(metadata["project_id"]),
		ProjectDir:    safeTextFromAny(metadata["project_dir"]),
		Model:         safeTextFromAny(metadata["model"]),
		CreatedAt:     safeFloat(metadata["created_at"]),
		LastMessageAt: safeFloat(metadata["last_message_at"]),
		MessageCount:  safeInt(metadata["message_count"]),
	}
}

// readSessionMetadata 读取单个会话元数据（cache_bust=true、不写回）。
// Python: session_catalog.py:200-209 _read_session_metadata
//
// Go 侧 ReadSessionMetadataWithCache 无 cache_bust/enable_writeback 开关：
// 该函数是纯读路径，不存在写回，等价于 enable_writeback=False。
func readSessionMetadata(sessionID string) (map[string]any, error) {
	metadata := serversession.GetSessionMetadata(sessionID)
	if metadata == nil {
		return nil, nil
	}
	return metadata, nil
}

// collectSessionMetadata 收集全部会话元数据。
// Python: session_catalog.py:212-221 _collect_session_metadata
//
// 上游 read_only 只影响 enable_writeback；Go 侧 GetAllSessionsMetadata 本身
// 不写回，故两个分支合并。分页由调用方完成（上游同样先全量再排序分页）。
func collectSessionMetadata() ([]map[string]any, error) {
	all, _ := serversession.GetAllSessionsMetadata(unrestrictedSessionLimit, 0)
	return all, nil
}
