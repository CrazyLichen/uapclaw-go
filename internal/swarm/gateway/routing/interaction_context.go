package routing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// OriginMetadata 原始 metadata（RawMessage 区分：缺失键 / 显式 null / 对象）
	OriginMetadata json.RawMessage `json:"origin_metadata"`
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

// LoadPendingInteraction 按 ID 加载交互上下文；文件不存在/解析失败/必填字段缺失返回 nil。
//
// Python: load (L94-103)：文件不存在 → None；cls(**fields) 重建（必填缺失
// TypeError → except → None）；解析失败 warning + None。
func LoadPendingInteraction(interactionID string) *PendingInteraction {
	fp := filepath.Join(interactionsDir(), interactionID+".json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).
				Str("interaction_id", interactionID).
				Err(err).
				Msg("PendingInteraction load failed")
		}
		return nil
	}
	pi, err := unmarshalPendingInteraction(raw)
	if err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", interactionID).
			Err(err).
			Msg("PendingInteraction load failed")
		return nil
	}
	return pi
}

// FindGroupPending 查群聊追问（直达加载 gpq_{session}_{user}，不过滤 status/TTL）。
//
// Python: find_group_pending (L149-151)
func FindGroupPending(sessionID, userID string) *PendingInteraction {
	return LoadPendingInteraction(fmt.Sprintf("gpq_%s_%s", sessionID, userID))
}

// FindAllGroupPending 收集全部 pending 且未过期的群聊追问。
//
// Python: find_all_group_pending (L133-146)：gpq_* glob → 重建 dataclass →
// status=="pending" 且未过期才收集；解析失败 debug 跳过。
// 注：Python 全仓零消费方，对齐原则照实实现（spec §1）。
func FindAllGroupPending() []*PendingInteraction {
	var result []*PendingInteraction
	dir := interactionsDir()
	fps, _ := filepath.Glob(filepath.Join(dir, "gpq_*.json"))
	for _, fp := range fps {
		pi := parseInteractionFile(fp)
		if pi == nil {
			continue
		}
		if pi.Status == StatusPending && float64(time.Now().Unix())-pi.CreatedAt <= DefaultTTL {
			result = append(result, pi)
		}
	}
	return result
}

// FindPendingInteraction 扫描 iact_{channel}_* 与 gpq_* 两前缀，按五层过滤取最旧。
//
// Python: find_pending (L106-130)，过滤顺序不可调换（执行顺序纪律）：
//  1. 解析失败 → debug 跳过
//  2. status != pending → 跳过
//  3. 过期 → 顺带 Remove + 跳过
//  4. principal_id 非空且不匹配 → 跳过
//  5. target_user_id 非空且不匹配 → 跳过
//  6. 取 created_at 最旧
// 尾部无条件 cleanup_expired。
func FindPendingInteraction(channelID, principalID string) *PendingInteraction {
	dir := interactionsDir()
	var oldest *PendingInteraction
	now := float64(time.Now().Unix())
	for _, prefix := range []string{"iact_" + channelID + "_", "gpq_"} {
		fps, _ := filepath.Glob(filepath.Join(dir, prefix+"*.json"))
		for _, fp := range fps {
			pi := parseInteractionFile(fp)
			if pi == nil {
				continue // 解析失败 debug 已记
			}
			if pi.Status != StatusPending {
				continue
			}
			if now-pi.CreatedAt > DefaultTTL {
				pi.Remove() // Python L118: 过期顺带删除
				continue
			}
			if pi.PrincipalID != "" && pi.PrincipalID != principalID {
				continue
			}
			if pi.TargetUserID != "" && pi.TargetUserID != principalID {
				continue
			}
			if oldest == nil || pi.CreatedAt < oldest.CreatedAt {
				oldest = pi
			}
		}
	}
	CleanupExpired(DefaultTTL) // Python L129: 尾部无条件清理
	return oldest
}

// CleanupExpired 清理过期交互文件（宽松解析：字符串时间戳容忍），返回删除数。
//
// Python: cleanup_expired (L154-170)：*.json glob → 裸 dict 取 created_at：
//   - 缺失 → 0 → 恒过期 → 删除（data.get("created_at", 0)）
//   - 字符串数字 → float() 转换成功 → 用
//   - 字符串非数字 → ValueError → except → 跳过不删
//   - JSON 解析失败 → debug 跳过
func CleanupExpired(ttl float64) int {
	dir := interactionsDir()
	fps, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	count := 0
	now := float64(time.Now().Unix())
	for _, fp := range fps {
		raw, err := os.ReadFile(fp)
		if err != nil {
			logger.Debug(logComponent).Str("file", fp).Err(err).
				Msg("PendingInteraction cleanup interaction file failed")
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			logger.Debug(logComponent).Str("file", fp).Err(err).
				Msg("PendingInteraction cleanup interaction file failed")
			continue
		}
		created, ok := extractCreatedAt(data)
		if !ok {
			continue // Python: float("abc") ValueError → except 跳过
		}
		if now-created > ttl {
			if err := os.Remove(fp); err == nil {
				count++
			}
		}
	}
	if count > 0 {
		logger.Info(logComponent).
			Int("count", count).
			Msg("PendingInteraction cleanup expired")
	}
	return count
}

// BuildResumeContent 构建任务恢复提示词（dm/group 两套模板，逐字对齐 Python）。
//
// Python: build_resume_content (L172-189)。中文提示词一比一复制，禁止改写。
func (pi *PendingInteraction) BuildResumeContent(answer string) string {
	if pi.Mode == ModeDM {
		return "[任务恢复] 之前的群聊请求：" + pi.OriginContent + "（来自 " + pi.OriginSenderName + "）\n" +
			"你之前的追问：" + pi.Question + "\n" +
			"principal 的回答：" + answer + "\n" +
			"请综合「原始请求」和「principal 的回答」中的所有信息继续完成任务，" +
			"原始请求中已明确提供的信息直接使用即可，不要再次追问。" +
			"并将结果回复到群聊。"
	}
	targetName := pi.TargetUserName
	if targetName == "" {
		targetName = "用户"
	}
	return "[任务恢复] 你正在处理的群聊请求：" + pi.OriginContent + "（来自 " + pi.OriginSenderName + "）\n" +
		"你之前追问了 " + targetName + "：" + pi.Question + "\n" +
		targetName + " 的回答：" + answer + "\n" +
		"请综合「原始请求」和「用户的回答」中的所有信息来完成任务。" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问或要求补充。" +
		"现在请立即执行任务（如设置提醒、创建日程等），不要只是确认或记录信息，并将执行结果回复到群聊。"
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

// UnmarshalJSON 对齐 Python dataclass 重建语义（spec §6 决策2 三类默认值）：
//   - 必填字段缺失 → error（Python TypeError → 文件跳过）
//   - 可选字段缺失 → dataclass 默认值：status→"pending"、origin_metadata→空 map、
//     created_at/updated_at→当前时间（default_factory=time.time 构造期求值）
//   - 未知键 → 忽略（Python k in dataclass_fields；Go json 天然忽略）
func (pi *PendingInteraction) UnmarshalJSON(data []byte) error {
	var alias pendingInteractionAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	// Python dataclass 必填字段（无默认值）：缺失 → TypeError
	required := map[string]*string{
		"interaction_id":     alias.InteractionID,
		"mode":               alias.Mode,
		"origin_channel_id":  alias.OriginChannelID,
		"origin_session_id":  alias.OriginSessionID,
		"origin_content":     alias.OriginContent,
		"origin_sender_name": alias.OriginSenderName,
		"origin_sender_id":   alias.OriginSenderID,
		"question":           alias.Question,
		"target_user_id":     alias.TargetUserID,
	}
	for name, v := range required {
		if v == nil {
			return fmt.Errorf("missing required field %q", name)
		}
	}

	now := float64(time.Now().UnixMilli()) / 1000.0
	pi.InteractionID = *alias.InteractionID
	pi.Mode = *alias.Mode
	pi.OriginChannelID = *alias.OriginChannelID
	pi.OriginSessionID = *alias.OriginSessionID
	pi.OriginContent = *alias.OriginContent
	pi.OriginSenderName = *alias.OriginSenderName
	pi.OriginSenderID = *alias.OriginSenderID
	pi.Question = *alias.Question
	pi.TargetUserID = *alias.TargetUserID
	if alias.TargetUserName != nil {
		pi.TargetUserName = *alias.TargetUserName
	}
	// 缺失键 origin_metadata → 空 map（dataclass default_factory=dict，spec 决策2）
	// 显式 null → nil map（Python None，spec 决策4）——用 RawMessage 三态区分
	pi.OriginMetadata = map[string]any{}
	if len(alias.OriginMetadata) > 0 && string(alias.OriginMetadata) != "null" {
		var meta map[string]any
		if err := json.Unmarshal(alias.OriginMetadata, &meta); err != nil {
			return fmt.Errorf("origin_metadata: %w", err)
		}
		pi.OriginMetadata = meta
	} else if string(alias.OriginMetadata) == "null" {
		pi.OriginMetadata = nil
	}
	if alias.PrincipalID != nil {
		pi.PrincipalID = *alias.PrincipalID
	}
	// 可选默认：status → "pending"（dataclass default）
	pi.Status = StatusPending
	if alias.Status != nil {
		pi.Status = *alias.Status
	}
	// 可选默认：时间 → 当前时间（default_factory=time.time）
	pi.CreatedAt = now
	if alias.CreatedAt != nil {
		pi.CreatedAt = *alias.CreatedAt
	}
	pi.UpdatedAt = now
	if alias.UpdatedAt != nil {
		pi.UpdatedAt = *alias.UpdatedAt
	}
	return nil
}

// unmarshalPendingInteraction 解析字节流为 PendingInteraction（UnmarshalJSON 封装）
func unmarshalPendingInteraction(raw []byte) (*PendingInteraction, error) {
	var pi PendingInteraction
	if err := json.Unmarshal(raw, &pi); err != nil {
		return nil, err
	}
	return &pi, nil
}

// parseInteractionFile 读文件并解析（失败 Debug 日志 + nil）。
//
// Python: find_pending/find_all_group_pending 内层 try 块的共享语义
// （读文件 → json.loads → cls(**fields)；except → logger.debug + continue）。
// 注意：与 load() 的 warning 级别不同——Python 扫描场景用 debug。
func parseInteractionFile(fp string) *PendingInteraction {
	raw, err := os.ReadFile(fp)
	if err != nil {
		logger.Debug(logComponent).
			Str("file", fp).
			Err(err).
			Msg("PendingInteraction parse interaction file failed")
		return nil
	}
	pi, err := unmarshalPendingInteraction(raw)
	if err != nil {
		logger.Debug(logComponent).
			Str("file", fp).
			Err(err).
			Msg("PendingInteraction parse interaction file failed")
		return nil
	}
	return pi
}

// extractCreatedAt 从裸 dict 提取 created_at（对齐 Python float(data.get("created_at", 0))）：
// 数字 → 直接用；字符串数字 → ParseFloat；字符串非数字 → !ok（跳过）；
// 缺失 → 0（恒过期语义）；JSON null → 0。
func extractCreatedAt(data map[string]any) (float64, bool) {
	v, exists := data["created_at"]
	if !exists || v == nil {
		return 0, true // Python: get 默认 0
	}
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false // Python: float("abc") → ValueError → except 跳过
		}
		return f, true
	default:
		// Python: float(其他类型如 list/dict) → TypeError → except 跳过
		return 0, false
	}
}
