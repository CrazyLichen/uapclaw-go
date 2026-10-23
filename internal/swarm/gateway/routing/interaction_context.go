package routing

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingInteraction 统一的追问上下文，支持群聊追问和 DM 追问两种模式。
//
// 存储路径: {workspace_dir}/agent/workspace/interactions/{interaction_id}.json
// 文件名前缀: gpq_* 群聊追问, iact_* DM 追问
// TTL: 24 小时
//
// Python: jiuwenswarm/gateway/routing/interaction_context.py PendingInteraction (L39-54)
type PendingInteraction struct {
	// InteractionID 交互标识（gpq_{session}_{user} 或 iact_{channel}_{hex}）
	InteractionID string `json:"interaction_id"`
	// Mode 模式："dm" 私聊追问 / "group" 群聊追问
	Mode string `json:"mode"`
	// OriginChannelID 发起渠道 ID
	OriginChannelID string `json:"origin_channel_id"`
	// OriginSessionID 发起会话 ID
	OriginSessionID string `json:"origin_session_id"`
	// OriginContent 原始请求内容
	OriginContent string `json:"origin_content"`
	// OriginSenderName 原始发送者名称
	OriginSenderName string `json:"origin_sender_name"`
	// OriginSenderID 原始发送者 ID
	OriginSenderID string `json:"origin_sender_id"`
	// Question 追问内容
	Question string `json:"question"`
	// TargetUserID 被追问用户 ID
	TargetUserID string `json:"target_user_id"`
	// TargetUserName 被追问用户名称
	TargetUserName string `json:"target_user_name"`
	// OriginMetadata 原始 metadata（落盘前过滤敏感键）
	OriginMetadata map[string]any `json:"origin_metadata"`
	// PrincipalID 数字分身主体用户 ID
	PrincipalID string `json:"principal_id"`
	// Status 状态："pending" / "completed"
	Status string `json:"status"`
	// CreatedAt 创建时间戳（秒，float 对齐 Python time.time()）
	CreatedAt float64 `json:"created_at"`
	// UpdatedAt 更新时间戳（秒，float 对齐 Python time.time()）
	UpdatedAt float64 `json:"updated_at"`
}

// pendingInteractionAlias UnmarshalJSON 辅助：指针字段检测键存在性
// （对齐 Python cls(**{k: data[k] for k in data if k in fields}) 的必填/可选区分）
type pendingInteractionAlias struct {
	// InteractionID 交互标识
	InteractionID *string `json:"interaction_id"`
	// Mode 模式
	Mode *string `json:"mode"`
	// OriginChannelID 发起渠道 ID
	OriginChannelID *string `json:"origin_channel_id"`
	// OriginSessionID 发起会话 ID
	OriginSessionID *string `json:"origin_session_id"`
	// OriginContent 原始请求内容
	OriginContent *string `json:"origin_content"`
	// OriginSenderName 原始发送者名称
	OriginSenderName *string `json:"origin_sender_name"`
	// OriginSenderID 原始发送者 ID
	OriginSenderID *string `json:"origin_sender_id"`
	// Question 追问内容
	Question *string `json:"question"`
	// TargetUserID 被追问用户 ID
	TargetUserID *string `json:"target_user_id"`
	// TargetUserName 被追问用户名称
	TargetUserName *string `json:"target_user_name"`
	// OriginMetadata 原始 metadata
	OriginMetadata map[string]any `json:"origin_metadata"`
	// PrincipalID 数字分身主体用户 ID
	PrincipalID *string `json:"principal_id"`
	// Status 状态
	Status *string `json:"status"`
	// CreatedAt 创建时间戳
	CreatedAt *float64 `json:"created_at"`
	// UpdatedAt 更新时间戳
	UpdatedAt *float64 `json:"updated_at"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ModeDM 私聊追问模式
	ModeDM = "dm"
	// ModeGroup 群聊追问模式
	ModeGroup = "group"
	// StatusPending 待处理状态
	StatusPending = "pending"
	// StatusCompleted 已完成状态
	StatusCompleted = "completed"
	// DefaultTTL 默认存活时长：24 小时（秒）
	DefaultTTL = 86400.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sensitiveKeys 敏感键集合（小写比较剔除）
// Python: _SENSITIVE_KEYS = frozenset({"api_key", "token", "authorization"}) (L24)
var sensitiveKeys = map[string]bool{
	"api_key":       true,
	"token":         true,
	"authorization": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Save 将交互上下文落盘（防御式，失败仅 Warn）。
//
// Python: save (L59-82)：updated_at 刷新为当前时间 → 过滤敏感键 →
// json.dumps(ensure_ascii=False, indent=2) 写文件；异常仅 warning。
// Go 对齐差异：CreatedAt 零值补当前时间（对齐 dataclass default_factory 语义）。
func (pi *PendingInteraction) Save() {
	// Python: self.updated_at = time.time()（无条件刷新）
	now := float64(time.Now().UnixMilli()) / 1000.0
	pi.UpdatedAt = now
	// dataclass default_factory=time.time 的构造期默认值对齐：
	// 调用方（11.12）逐字段构造 struct 时若未填时间字段，此处补当前时间
	if pi.CreatedAt == 0 {
		pi.CreatedAt = now
	}

	data := map[string]any{
		"interaction_id":     pi.InteractionID,
		"mode":               pi.Mode,
		"origin_channel_id":  pi.OriginChannelID,
		"origin_session_id":  pi.OriginSessionID,
		"origin_content":     pi.OriginContent,
		"origin_sender_name": pi.OriginSenderName,
		"origin_sender_id":   pi.OriginSenderID,
		"question":           pi.Question,
		"target_user_id":     pi.TargetUserID,
		"target_user_name":   pi.TargetUserName,
		"origin_metadata":    filterSensitive(pi.OriginMetadata),
		"principal_id":       pi.PrincipalID,
		"status":             pi.Status,
		"created_at":         pi.CreatedAt,
		"updated_at":         pi.UpdatedAt,
	}

	// Python: json.dumps(data, ensure_ascii=False, indent=2) — 中文不转义
	// Go: json.Marshal 默认不转义非 ASCII，但会转义 HTML 字符（<>&），
	// SetEscapeHTML(false) 对齐 ensure_ascii=False 的完整语义
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction save failed")
		return
	}
	// Encode 自带尾换行；Python write_text 不带 → TrimSpace 对齐
	content := bytes.TrimRight(buf.Bytes(), "\n")
	// 缩进 2 空格（对齐 indent=2）：Encode 无缩进能力，用 Indent 重排
	indented := indentJSON(content)
	if err := os.WriteFile(pi.path(), indented, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction save failed")
		return
	}
	logger.Info(logComponent).
		Str("interaction_id", pi.InteractionID).
		Str("mode", pi.Mode).
		Msg("PendingInteraction save")
}

// Remove 删除交互文件（防御式，失败仅 Warn）。
//
// Python: remove (L84-91)：存在才删；异常仅 warning。
func (pi *PendingInteraction) Remove() {
	fp := pi.path()
	if _, err := os.Stat(fp); err != nil {
		if os.IsNotExist(err) {
			return // Python: p.exists() 为 False 直接跳过
		}
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction remove failed")
		return
	}
	if err := os.Remove(fp); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction remove failed")
		return
	}
	logger.Info(logComponent).
		Str("interaction_id", pi.InteractionID).
		Msg("PendingInteraction remove")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// path 交互文件完整路径：{interactionsDir}/{interaction_id}.json
// Python: _path (L56-57)
func (pi *PendingInteraction) path() string {
	return filepath.Join(interactionsDir(), pi.InteractionID+".json")
}

// interactionsDir 交互目录（每次动态解析 + mkdir，对齐 Python _get_interactions_dir L28-31）
func interactionsDir() string {
	dir := path.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755) // Python: mkdir(parents=True, exist_ok=True) 不抛错
	return dir
}

// filterSensitive 过滤敏感键（小写比较）
// Python: _filter_sensitive (L34-35)
func filterSensitive(metadata map[string]any) map[string]any {
	result := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if sensitiveKeys[strings.ToLower(k)] {
			continue
		}
		result[k] = v
	}
	return result
}

// indentJSON 为紧凑 JSON 加 2 空格缩进（对齐 Python indent=2）
func indentJSON(compact []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return compact // 缩进失败退回紧凑形式（写入仍有效）
	}
	return buf.Bytes()
}
