package update

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryActionItem 记忆动作项，表示一条记忆的 ADD 或 DELETE 动作。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryActionItem)
type MemoryActionItem struct {
	// ID 记忆 ID
	ID string
	// Content 记忆内容
	Content string
	// Status 动作状态
	Status MemoryStatus
}

// MemCheckItem 记忆检查结果项。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemCheckItem)
type MemCheckItem struct {
	// InfoID 记忆 ID
	InfoID string
	// InfoText 记忆内容
	InfoText string
	// Result 检查结果
	Result CheckResult
	// RelatedInfos 关联的旧记忆
	RelatedInfos map[string]string
}

// MemUpdateChecker 记忆冲突检查器。
//
// ⤵️ 回填: 7.8 — 当前 stub 实现，直接返回所有新记忆为 ADD。
// 7.8 实现时替换为 LLM 驱动的冲突检查（使用 PromptApplier + Model）。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemUpdateChecker)
type MemUpdateChecker struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// CheckResult 记忆检查结果枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (CheckResult)
type CheckResult int

const (
	// CheckResultRedundant 冗余
	CheckResultRedundant CheckResult = iota
	// CheckResultConflicting 冲突
	CheckResultConflicting
	// CheckResultNone 共存
	CheckResultNone
)

// MemoryStatus 记忆动作状态枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryStatus)
type MemoryStatus int

const (
	// MemoryStatusAdd 添加
	MemoryStatusAdd MemoryStatus = iota
	// MemoryStatusDelete 删除
	MemoryStatusDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Check 检查新记忆与旧记忆的冗余/冲突。
//
// 当前 stub 实现：直接返回所有新记忆为 ADD（对齐 Python 中 base_chat_model=None 时
// MemUpdateChecker.Check() 的行为）。
//
// ⤵️ 回填: 7.8 — 7.8 实现时替换为 LLM 驱动的冲突检查。
func (c *MemUpdateChecker) Check(newMemories map[string]string, oldMemories map[string]string) ([]*MemoryActionItem, error) {
	result := make([]*MemoryActionItem, 0, len(newMemories))
	for id, content := range newMemories {
		result = append(result, &MemoryActionItem{
			ID:      id,
			Content: content,
			Status:  MemoryStatusAdd,
		})
	}
	return result, nil
}

// String 实现 fmt.Stringer 接口，对齐 Python CheckResult.value
func (cr CheckResult) String() string {
	switch cr {
	case CheckResultRedundant:
		return "redundant"
	case CheckResultConflicting:
		return "conflicting"
	case CheckResultNone:
		return "none"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python MemoryStatus.value
func (ms MemoryStatus) String() string {
	switch ms {
	case MemoryStatusAdd:
		return "add"
	case MemoryStatusDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
