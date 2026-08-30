package lite

// ──────────────────────────── 结构体 ────────────────────────────

// WriteResult 写入结果。对齐 Python WriteResult (conflict_types.py)
type WriteResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// Mode 写入模式
	Mode WriteMode
	// ConflictDetected 是否检测到冲突
	ConflictDetected bool
	// ConflictingFiles 冲突文件列表
	ConflictingFiles []string
	// Note 备注
	Note string
	// Error 错误信息
	Error string
	// Type 类型
	Type string
}

// ──────────────────────────── 枚举 ────────────────────────────

// WriteMode 写入模式枚举。对齐 Python WriteMode
type WriteMode int

const (
	// WriteModeCreate 创建
	WriteModeCreate WriteMode = iota
	// WriteModeAppend 追加
	WriteModeAppend
	// WriteModeSkip 跳过
	WriteModeSkip
)

// String 返回写入模式的字符串表示。对齐 Python WriteMode.value
func (m WriteMode) String() string {
	switch m {
	case WriteModeCreate:
		return "create"
	case WriteModeAppend:
		return "append"
	case WriteModeSkip:
		return "skip"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ToDict 转为字典。对齐 Python WriteResult.to_dict()
func (w *WriteResult) ToDict() map[string]any {
	result := map[string]any{
		"success": w.Success,
		"path":    w.Path,
		"mode":    w.Mode.String(),
	}
	if w.Type != "" {
		result["type"] = w.Type
	}
	if w.ConflictDetected {
		result["conflict_detected"] = true
		result["conflicting_files"] = w.ConflictingFiles
	}
	if w.Note != "" {
		result["note"] = w.Note
	}
	if w.Error != "" {
		result["error"] = w.Error
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
