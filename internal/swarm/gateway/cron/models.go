package cron

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronJob cron 任务持久化模型。
// 对齐 Python: CronJob dataclass (models.py L88-228)。
// JSON 字段名与 Python to_dict 输出一致（id/name/enabled/expired/cron_expr/timezone/wake_offset_seconds/description/targets/created_at/updated_at，可选 session_id/chat_type/mode/delete_after_run）。
type CronJob struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Name 任务名
	Name string `json:"name"`
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Expired 已过期（one-shot 执行后无下一次）
	Expired bool `json:"expired"`
	// CronExpr cron 表达式
	CronExpr string `json:"cron_expr"`
	// Timezone 时区（IANA）
	Timezone string `json:"timezone"`
	// WakeOffsetSeconds 提前唤醒秒数（默认 300）
	WakeOffsetSeconds int `json:"wake_offset_seconds"`
	// Description 任务内容（运行时发给 assistant 的 payload 文本）
	Description string `json:"description"`
	// Targets 推送频道 ID（如 "web"）；JSON 字段名沿用 targets 兼容旧数据
	Targets string `json:"targets"`
	// SessionID SessionMap 形态（feishu::chat_id::bot_id::...），仅 feishu_enterprise 投递用
	SessionID string `json:"session_id,omitempty"`
	// CreatedAt 创建时间（Unix 秒，None 为 0）
	CreatedAt float64 `json:"created_at"`
	// UpdatedAt 更新时间（Unix 秒，None 为 0）
	UpdatedAt float64 `json:"updated_at"`
	// ChatType 群聊("group")或私聊("p2p")标记，推送时决定 IMOutboundPipeline
	ChatType string `json:"chat_type,omitempty"`
	// Mode 执行模式（默认 "agent"）
	Mode string `json:"mode,omitempty"`
	// DeleteAfterRun 执行一次后自动删除（提醒类任务）
	DeleteAfterRun bool `json:"delete_after_run,omitempty"`
}

// CronTarget 推送目标（channel_id + 可选 session_id）。
// 对齐 Python: CronTarget dataclass (models.py L65-85)。
// Python 侧仅被 store._normalize_targets（零外部调用的死代码）引用，按死代码保留先例移植。
type CronTarget struct {
	// ChannelID 推送频道 ID
	ChannelID string `json:"channel_id"`
	// SessionID 会话 ID（可选；空串等价 Python None）
	SessionID string `json:"session_id"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetChannelWeb Web 频道
	TargetChannelWeb = "web"
	// TargetChannelTUI TUI 频道
	TargetChannelTUI = "tui"
	// TargetChannelFeishu 飞书频道
	TargetChannelFeishu = "feishu"
	// TargetChannelFeishuEnterprise 企业飞书前缀（完整形态 feishu_enterprise:<app_id>）
	TargetChannelFeishuEnterprise = "feishu_enterprise"
	// TargetChannelWeCom 企业微信频道
	TargetChannelWeCom = "wecom"
	// TargetChannelDingTalk 钉钉频道
	TargetChannelDingTalk = "dingtalk"
	// TargetChannelWeChat 微信频道
	TargetChannelWeChat = "wechat"
	// TargetChannelWhatsApp WhatsApp 频道
	TargetChannelWhatsApp = "whatsapp"
	// TargetChannelXiaoyi 小艺频道
	TargetChannelXiaoyi = "xiaoyi"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// validTargetChannels CronTargetChannel 枚举值集合（8 个枚举值）。
// 对齐 Python: CronTargetChannel(str, Enum)——8 个枚举值 + feishu_enterprise:<app_id> 前缀形态，合计 9 种渠道形态；feishu_enterprise 前缀形态不进本集合，由 feishuEnterpriseAppID 单独处理。
var validTargetChannels = map[string]bool{
	TargetChannelWeb:      true,
	TargetChannelTUI:      true,
	TargetChannelFeishu:   true,
	TargetChannelWeCom:    true,
	TargetChannelDingTalk: true,
	TargetChannelWeChat:   true,
	TargetChannelWhatsApp: true,
	TargetChannelXiaoyi:   true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToDict 转为 map（E2A payload 兼容）。
// 对齐 Python: CronJob.to_dict (models.py L115-137)。可选字段空值时不写入。
func (j *CronJob) ToDict() map[string]any {
	d := map[string]any{
		"id":                  j.ID,
		"name":                j.Name,
		"enabled":             j.Enabled,
		"expired":             j.Expired,
		"cron_expr":           j.CronExpr,
		"timezone":            j.Timezone,
		"wake_offset_seconds": j.WakeOffsetSeconds,
		"description":         j.Description,
		"targets":             j.Targets,
		"created_at":          j.CreatedAt,
		"updated_at":          j.UpdatedAt,
	}
	if j.SessionID != "" {
		d["session_id"] = j.SessionID
	}
	if j.ChatType != "" {
		d["chat_type"] = j.ChatType
	}
	if j.Mode != "" {
		d["mode"] = j.Mode
	}
	if j.DeleteAfterRun {
		d["delete_after_run"] = j.DeleteAfterRun
	}
	return d
}

// FromCronJobDict 从 map 反序列化 CronJob（严格校验）。
// 对齐 Python: CronJob.from_dict (models.py L139-228)。
// 校验/报错顺序与 Python 严格一致：wake_offset 转换 → description → id/name/cron_expr/timezone → 表达式校验 → targets → 归一化。
// 错误返回 error（对齐 Python raise ValueError）。
func FromCronJobDict(data map[string]any) (*CronJob, error) {
	if data == nil {
		return nil, fmt.Errorf("data is nil")
	}
	jobID := strings.TrimSpace(strAny(data["id"]))
	name := strings.TrimSpace(strAny(data["name"]))
	cronExpr := strings.TrimSpace(strAny(data["cron_expr"]))
	timezone := strings.TrimSpace(strAny(data["timezone"]))
	enabled := boolAny(data["enabled"])
	expired := boolAny(data["expired"])

	// wake_offset_seconds：int 转换失败报错，负数钳制为 0。
	// 对齐 Python: int(wake_offset_seconds_raw) 失败 raise ValueError；
	// 缺失时默认 300；显式 null 时 int(None) 同样报错（null 与缺失语义不同）。
	wos := 300
	if raw, ok := data["wake_offset_seconds"]; ok {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		wos = n
	}
	if wos < 0 {
		wos = 0
	}

	description := strings.TrimSpace(strAny(data["description"]))
	if description == "" {
		return nil, fmt.Errorf("description is required")
	}

	// targets 新格式为字符串；旧格式为 []map（取第一个非空 channel_id 兼容）
	targetsStr := ""
	if raw, ok := data["targets"]; ok {
		if s, isStr := raw.(string); isStr {
			targetsStr = strings.TrimSpace(s)
		} else if list, isList := raw.([]any); isList {
			for _, item := range list {
				if m, isMap := item.(map[string]any); isMap {
					ch := strings.TrimSpace(strAny(m["channel_id"]))
					if ch != "" {
						targetsStr = ch
						break
					}
				}
			}
		}
	}

	if jobID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}
	if timezone == "" {
		return nil, fmt.Errorf("timezone is required")
	}
	// 校验表达式（含时区）
	if err := ValidateCronExpression(cronExpr, timezone); err != nil {
		return nil, err
	}
	if targetsStr == "" {
		return nil, fmt.Errorf("targets is required")
	}

	targetsStr = NormalizeTargetChannelID(targetsStr, TargetChannelWeb)

	return &CronJob{
		ID:                jobID,
		Name:              name,
		Enabled:           enabled,
		Expired:           expired,
		CronExpr:          cronExpr,
		Timezone:          timezone,
		WakeOffsetSeconds: wos,
		Description:       description,
		Targets:           targetsStr,
		SessionID:         strings.TrimSpace(strAny(data["session_id"])),
		CreatedAt:         floatAny(data["created_at"], 0),
		UpdatedAt:         floatAny(data["updated_at"], 0),
		ChatType:          strings.TrimSpace(strAny(data["chat_type"])),
		Mode:              modeAny(data["mode"]),
		DeleteAfterRun:    boolAny(data["delete_after_run"]),
	}, nil
}

// ToDict 转为 map。
// 对齐 Python: CronTarget.to_dict (models.py L72-76)。
func (t *CronTarget) ToDict() map[string]any {
	return map[string]any{
		"channel_id": t.ChannelID,
		"session_id": t.SessionID,
	}
}

// FromCronTargetDict 从 map 反序列化 CronTarget（channel_id 必填）。
// 对齐 Python: CronTarget.from_dict (models.py L78-85)。
// 错误返回 error（对齐 Python raise ValueError）。
func FromCronTargetDict(data map[string]any) (*CronTarget, error) {
	if data == nil {
		return nil, fmt.Errorf("data is nil")
	}
	channelID := strings.TrimSpace(strAny(data["channel_id"]))
	if channelID == "" {
		return nil, fmt.Errorf("target.channel_id is required")
	}
	// session_id 仅字符串有效；空串等价 Python None（session_id or None）
	return &CronTarget{
		ChannelID: channelID,
		SessionID: strings.TrimSpace(strAny(data["session_id"])),
	}, nil
}

// IsValidTargetChannelID 校验目标频道 ID 是否合法。
// 对齐 Python: is_valid_target_channel_id (models.py L31-41)。
// feishu_enterprise:<app_id> 形态校验 app_id 非空。
func IsValidTargetChannelID(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "feishu_enterprise:") {
		return feishuEnterpriseAppID(s) != ""
	}
	return validTargetChannels[strings.ToLower(s)]
}

// AllTargetChannels 返回全部目标频道列表（CronTargetChannel 枚举值）。
// 对齐 Python: [e.value for e in CronTargetChannel]。
func AllTargetChannels() []string {
	return []string{
		TargetChannelWeb, TargetChannelTUI, TargetChannelFeishu,
		TargetChannelWeCom, TargetChannelDingTalk, TargetChannelWeChat,
		TargetChannelWhatsApp, TargetChannelXiaoyi,
	}
}

// NormalizeTargetChannelID 归一化目标频道 ID，非法回退 default。
// 对齐 Python: normalize_target_channel_id (models.py L44-57)。
func NormalizeTargetChannelID(raw string, defaultChannel string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return defaultChannel
	}
	if strings.HasPrefix(s, "feishu_enterprise:") {
		appID := feishuEnterpriseAppID(s)
		if appID != "" {
			return TargetChannelFeishuEnterprise + ":" + appID
		}
		return defaultChannel
	}
	if validTargetChannels[strings.ToLower(s)] {
		return strings.ToLower(s)
	}
	return defaultChannel
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// feishuEnterpriseAppID 提取 feishu_enterprise:<app_id>的 app_id。
// 对齐 Python: _feishu_enterprise_app_id (models.py L23-28)。忽略 :chat: 等后续后缀。
func feishuEnterpriseAppID(s string) string {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || strings.ToLower(strings.TrimSpace(parts[0])) != "feishu_enterprise" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// strAny 安全取字符串（nil/非字符串返回空串）。
func strAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolAny 将 any 值按 Python bool() 真值语义转 bool。
// 对齐 Python: bool(data.get("enabled", False))——缺失/None 为 false，
// 非零数值/非空字符串为 true（对齐 message_handler/convert.go toBool 先例）。
func boolAny(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != ""
	case []any:
		// 对齐 Python bool([]) 为 False、bool([x]) 为 True
		return len(val) > 0
	case map[string]any:
		// 对齐 Python bool({}) 为 False
		return len(val) > 0
	default:
		return true
	}
}

// intAny 按 Python int() 语义取整数（支持 int/int64/float64/bool/json.Number/数字字符串，float 截断）。
func intAny(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("not an int")
		}
		return int(i), nil
	case string:
		// 对齐 Python int("300") → 300（容忍首尾空白与正负号）
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, nil
		}
	}
	return 0, fmt.Errorf("not an int")
}

// floatAny 安全取浮点（对齐 Python float(x) if isinstance(x,(int,float)) else None）。
// bool 是 Python int 子类：float(True)=1.0、float(False)=0.0；其余类型回退默认值。
func floatAny(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case bool:
		if n {
			return 1.0
		}
		return 0.0
	}
	return def
}

// modeAny 归一化 mode 字段（空/非字符串默认 "agent"，其余小写化）。
// 对齐 Python: mode 处理 (models.py L203-208)。
func modeAny(v any) string {
	s := strAny(v)
	if s == "" {
		return "agent"
	}
	return strings.ToLower(strings.TrimSpace(s))
}
