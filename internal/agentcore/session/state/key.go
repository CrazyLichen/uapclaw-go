package state

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StateKey 状态访问键，封装 string/map/slice/all 四态
// 内部用 value 字段存储实际值，keyType 标识具体类型
type StateKey struct {
	// keyType 状态键类型
	keyType StateKeyType
	// value 存储实际值（string / map[string]any / []any / allStateKeySentinel）
	value any
}

// ──────────────────────────── 枚举 ────────────────────────────

// StateKeyType 标识 StateKey 的类型
type StateKeyType int

const (
	// StateKeyString 字符串路径类型
	StateKeyString StateKeyType = iota
	// StateKeyMap map schema 类型
	StateKeyMap
	// StateKeyList list schema 类型
	StateKeyList
	// StateKeyAll 全部状态哨兵类型（用于 GetGlobal/GetAgent 返回完整快照）
	// G-02 修复：新增类型，对齐 Python key=None 语义
	StateKeyAll
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// G-02 修复：定义 AllStateKey 哨兵值，用于 GetGlobal/GetAgent 中表示"获取全部全局状态"。
// Python 中 key=None 表示获取全部，Go 中用哨兵值替代零值判断，
// 避免零值 StateKey（含 StringKey("")）与"获取全部"语义混淆。

// allStateKeySentinel AllStateKey 的内部哨兵值
var allStateKeySentinel = struct{}{}

// AllStateKey 表示获取全部全局状态的哨兵 StateKey。
// 用于 AgentStateCollection.GetGlobal / GetAgent 和 WorkflowStateCollection.GetGlobal。
// 对齐 Python 中 key=None 返回完整全局状态的语义。
var AllStateKey = StateKey{keyType: StateKeyAll, value: allStateKeySentinel}

// ──────────────────────────── 导出函数 ────────────────────────────

// StringKey 创建字符串路径键，如 "a.b.c" 或 "${ref.path}"
func StringKey(path string) StateKey {
	return StateKey{keyType: StateKeyString, value: path}
}

// SchemaKey 创建 map schema 键，用于批量按 schema 读取
// 构造时深拷贝传入的 map，防止外部修改
func SchemaKey(schema map[string]any) StateKey {
	return StateKey{keyType: StateKeyMap, value: utils.DeepCopyMap(schema)}
}

// ListKey 创建 list schema 键，用于按列表 schema 读取
// 构造时深拷贝传入的 slice，防止外部修改
func ListKey(keys []any) StateKey {
	return StateKey{keyType: StateKeyList, value: utils.DeepCopySlice(keys)}
}

// IsZero 判断 StateKey 是否为零值（未设置）
func (k StateKey) IsZero() bool {
	return k.value == nil
}

// IsAll 判断 StateKey 是否为 AllStateKey（获取全部全局状态的哨兵值）
// G-02 修复：替代之前的零值判断，语义更清晰
func (k StateKey) IsAll() bool {
	return k.keyType == StateKeyAll
}

// Type 返回 StateKey 的类型
func (k StateKey) Type() StateKeyType {
	return k.keyType
}

// String 返回字符串值，仅当 Type 为 StateKeyString 时有效，否则返回空字符串
func (k StateKey) String() string {
	if k.keyType == StateKeyString {
		return k.value.(string)
	}
	return ""
}

// Map 返回 map 值的深拷贝，仅当 Type 为 StateKeyMap 时有效，否则返回 nil
func (k StateKey) Map() map[string]any {
	if k.keyType == StateKeyMap {
		return utils.DeepCopyMap(k.value.(map[string]any))
	}
	return nil
}

// List 返回 slice 值的深拷贝，仅当 Type 为 StateKeyList 时有效，否则返回 nil
func (k StateKey) List() []any {
	if k.keyType == StateKeyList {
		return utils.DeepCopySlice(k.value.([]any))
	}
	return nil
}
