package state

// ──────────────────────────── 结构体 ────────────────────────────

// StateKey 状态访问键，封装 string/map/slice 三态
// 内部用 value 字段存储实际值，keyType 标识具体类型
type StateKey struct {
	keyType StateKeyType
	value   any // 存储 string / map[string]any / []any
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
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StringKey 创建字符串路径键，如 "a.b.c" 或 "${ref.path}"
func StringKey(path string) StateKey {
	return StateKey{keyType: StateKeyString, value: path}
}

// SchemaKey 创建 map schema 键，用于批量按 schema 读取
// 构造时深拷贝传入的 map，防止外部修改
func SchemaKey(schema map[string]any) StateKey {
	return StateKey{keyType: StateKeyMap, value: deepCopyMap(schema)}
}

// ListKey 创建 list schema 键，用于按列表 schema 读取
// 构造时深拷贝传入的 slice，防止外部修改
func ListKey(keys []any) StateKey {
	return StateKey{keyType: StateKeyList, value: deepCopySlice(keys)}
}

// ──────────────────────────── StateKey 方法 ────────────────────────────

// IsZero 判断 StateKey 是否为零值（未设置）
func (k StateKey) IsZero() bool {
	return k.value == nil
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
		return deepCopyMap(k.value.(map[string]any))
	}
	return nil
}

// List 返回 slice 值的深拷贝，仅当 Type 为 StateKeyList 时有效，否则返回 nil
func (k StateKey) List() []any {
	if k.keyType == StateKeyList {
		return deepCopySlice(k.value.([]any))
	}
	return nil
}
