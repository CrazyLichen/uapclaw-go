package browser_move

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserTaskProgressState 浏览器任务进度状态，记录任务执行过程中的状态信息。
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:44-116 (BrowserTaskProgressState)
type BrowserTaskProgressState struct {
	// RequestID 请求标识
	RequestID string
	// Status 任务状态
	Status string
	// CompletedSteps 已完成的步骤
	CompletedSteps []string
	// RemainingSteps 剩余步骤
	RemainingSteps []string
	// NextStep 下一步
	NextStep string
	// CompletionEvidence 完成证据
	CompletionEvidence []string
	// MissingRequirements 缺失需求
	MissingRequirements []string
	// RecentToolSteps 近期工具步骤
	RecentToolSteps []string
	// LastPageURL 最近页面 URL
	LastPageURL string
	// LastPageTitle 最近页面标题
	LastPageTitle string
	// LastScreenshot 最近截图（类型不固定，对齐 Python Optional[Any]）
	LastScreenshot any
	// LastWorkerFinal 最近 Worker 最终输出
	LastWorkerFinal string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserTaskProgressStateFromDict 从字典构造 BrowserTaskProgressState。
// data 为 nil 或非字典类型时返回零值状态。
//
// 对齐 Python: BrowserTaskProgressState.from_dict
func NewBrowserTaskProgressStateFromDict(data map[string]any) *BrowserTaskProgressState {
	if data == nil {
		return &BrowserTaskProgressState{
			Status:              "unknown",
			CompletedSteps:      []string{},
			RemainingSteps:      []string{},
			CompletionEvidence:  []string{},
			MissingRequirements: []string{},
			RecentToolSteps:     []string{},
		}
	}

	// 解析 last_page 子对象
	lastPage := map[string]any{}
	if lp, ok := data["last_page"]; ok {
		if lpMap, ok := lp.(map[string]any); ok {
			lastPage = lpMap
		}
	}

	return &BrowserTaskProgressState{
		RequestID:           trimStr(data["request_id"]),
		Status:              statusWithDefault(trimStr(data["status"])),
		CompletedSteps:      strSlice(data["completed_steps"]),
		RemainingSteps:      strSlice(data["remaining_steps"]),
		NextStep:            trimStr(data["next_step"]),
		CompletionEvidence:  strSlice(data["completion_evidence"]),
		MissingRequirements: strSlice(data["missing_requirements"]),
		RecentToolSteps:     strSlice(data["recent_tool_steps"]),
		LastPageURL:         trimStr(lastPage["url"]),
		LastPageTitle:       trimStr(lastPage["title"]),
		LastScreenshot:      data["last_screenshot"],
		LastWorkerFinal:     trimStr(data["last_worker_final"]),
	}
}

// IsEmpty 判断进度状态是否为空（初始/未更新状态）。
//
// 对齐 Python: BrowserTaskProgressState.is_empty
func (s *BrowserTaskProgressState) IsEmpty() bool {
	return s.Status == "unknown" &&
		len(s.CompletedSteps) == 0 &&
		len(s.RemainingSteps) == 0 &&
		s.NextStep == "" &&
		len(s.CompletionEvidence) == 0 &&
		len(s.MissingRequirements) == 0 &&
		len(s.RecentToolSteps) == 0 &&
		s.LastPageURL == "" &&
		s.LastPageTitle == "" &&
		s.LastWorkerFinal == "" &&
		(s.LastScreenshot == nil || s.LastScreenshot == "")
}

// ToDict 将进度状态转换为字典，next_step/last_worker_final/request_id 为空时输出 nil。
//
// 对齐 Python: BrowserTaskProgressState.to_dict
func (s *BrowserTaskProgressState) ToDict() map[string]any {
	result := map[string]any{
		"status":               s.Status,
		"completed_steps":      copySlice(s.CompletedSteps),
		"remaining_steps":      copySlice(s.RemainingSteps),
		"next_step":            nilIfEmpty(s.NextStep),
		"completion_evidence":  copySlice(s.CompletionEvidence),
		"missing_requirements": copySlice(s.MissingRequirements),
		"recent_tool_steps":    copySlice(s.RecentToolSteps),
		"last_page": map[string]any{
			"url":   s.LastPageURL,
			"title": s.LastPageTitle,
		},
		"last_screenshot":   s.LastScreenshot,
		"last_worker_final": nilIfEmpty(s.LastWorkerFinal),
		"request_id":        nilIfEmpty(s.RequestID),
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trimStr 将值转为字符串并去除首尾空白，nil 或空值返回 ""。
func trimStr(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// statusWithDefault 处理 status 字段：空值回退为 "unknown"。
func statusWithDefault(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// strSlice 将值转换为 []string，跳过空白项。对齐 Python 中 [str(item).strip() for item in data.get(...) or [] if str(item).strip()]。
func strSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	// 支持 []any（JSON 反序列化结果）和 []string（ToDict 输出）
	switch slice := v.(type) {
	case []any:
		result := make([]string, 0, len(slice))
		for _, item := range slice {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(slice))
		for _, item := range slice {
			s := strings.TrimSpace(item)
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return []string{}
	}
}

// copySlice 返回字符串切片的拷贝，避免外部修改原切片。
func copySlice(src []string) []string {
	if src == nil {
		return []string{}
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// nilIfEmpty 空字符串返回 nil，否则返回原值。对齐 Python 中 self.xxx or None。
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
