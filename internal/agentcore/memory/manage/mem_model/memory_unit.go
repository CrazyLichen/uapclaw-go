package mem_model

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemoryUnit 记忆数据项基类。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (BaseMemoryUnit)
type BaseMemoryUnit struct {
	// MemType 记忆类型
	MemType MemoryType
	// MemID 记忆唯一标识
	MemID string
}

// GetMemType 返回记忆类型（实现 MemoryUnit 接口）。
func (u *BaseMemoryUnit) GetMemType() MemoryType { return u.MemType }

// GetMemID 返回记忆唯一标识（实现 MemoryUnit 接口）。
func (u *BaseMemoryUnit) GetMemID() string { return u.MemID }

// MemoryUnit 记忆数据项接口，所有记忆类型（FragmentMemoryUnit/VariableUnit/SummaryUnit）必须实现。
//
// 对齐 Python: BaseMemoryUnit（作为基类，Go 中用接口替代继承）
type MemoryUnit interface {
	// GetMemType 返回记忆类型
	GetMemType() MemoryType
	// GetMemID 返回记忆唯一标识
	GetMemID() string
}

// FragmentMemoryUnit 碎片记忆数据项，包含文本内容、关联消息 ID 和操作类型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (FragmentMemoryUnit)
type FragmentMemoryUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// Content 文本内容
	Content string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
	// OperationType 操作类型
	OperationType OperationType
}

// VariableUnit 变量记忆数据项。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (VariableUnit)
type VariableUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// VariableName 变量名
	VariableName string
	// VariableMem 变量值
	VariableMem string
}

// SummaryUnit 摘要记忆数据项。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (SummaryUnit)
type SummaryUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// Summary 摘要内容
	Summary string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
}

// ──────────────────────────── 枚举 ────────────────────────────

// MemoryType 记忆类型枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (MemoryType)
type MemoryType int

const (
	// MemoryTypeUserProfile 用户画像
	MemoryTypeUserProfile MemoryType = iota
	// MemoryTypeSemanticMemory 语义记忆
	MemoryTypeSemanticMemory
	// MemoryTypeEpisodicMemory 情景记忆
	MemoryTypeEpisodicMemory
	// MemoryTypeVariable 变量
	MemoryTypeVariable
	// MemoryTypeSummary 摘要
	MemoryTypeSummary
	// MemoryTypeUnknown 未知
	MemoryTypeUnknown
)

// OperationType 操作类型枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (OperationType)
type OperationType int

const (
	// OperationTypeAdd 新增
	OperationTypeAdd OperationType = iota
	// OperationTypeUpdate 更新
	OperationTypeUpdate
	// OperationTypeDelete 删除
	OperationTypeDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseMemoryType 从字符串解析 MemoryType，未匹配时返回 MemoryTypeUnknown。
func ParseMemoryType(s string) MemoryType {
	switch s {
	case "user_profile":
		return MemoryTypeUserProfile
	case "semantic_memory":
		return MemoryTypeSemanticMemory
	case "episodic_memory":
		return MemoryTypeEpisodicMemory
	case "variable":
		return MemoryTypeVariable
	case "summary":
		return MemoryTypeSummary
	default:
		return MemoryTypeUnknown
	}
}

// ParseOperationType 从字符串解析 OperationType，未匹配时返回 OperationTypeAdd。
func ParseOperationType(s string) OperationType {
	switch s {
	case "add":
		return OperationTypeAdd
	case "update":
		return OperationTypeUpdate
	case "delete":
		return OperationTypeDelete
	default:
		return OperationTypeAdd
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python MemoryType.value
func (mt MemoryType) String() string {
	switch mt {
	case MemoryTypeUserProfile:
		return "user_profile"
	case MemoryTypeSemanticMemory:
		return "semantic_memory"
	case MemoryTypeEpisodicMemory:
		return "episodic_memory"
	case MemoryTypeVariable:
		return "variable"
	case MemoryTypeSummary:
		return "summary"
	case MemoryTypeUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python OperationType.value
func (ot OperationType) String() string {
	switch ot {
	case OperationTypeAdd:
		return "add"
	case OperationTypeUpdate:
		return "update"
	case OperationTypeDelete:
		return "delete"
	default:
		return "add"
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
