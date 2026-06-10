package vector_fields

import (
	"fmt"
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorField 向量索引配置基类。
//
// 子类通过嵌入 VectorField 并在字段上添加 `vf:"construct"` 或 `vf:"search"`
// 结构体标签来标记字段所属阶段。ToDict(stage) 通过反射读取标签，
// 只输出匹配阶段的字段。
//
// 内部字段（DatabaseType、IndexType、VectorFieldName）用 `vf:"-"` 标记，
// 始终被过滤，不会出现在 ToDict 输出中。
//
// 对应 Python: vector_fields/base.py (VectorField)
type VectorField struct {
	// DatabaseType 向量数据库类型
	DatabaseType DatabaseType `vf:"-"`
	// IndexType 索引类型
	IndexType IndexType `vf:"-"`
	// VectorFieldName 向量字段名
	VectorFieldName string `vf:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// DatabaseType 向量数据库类型。
//
// 对应 Python: vector_fields/base.py (VectorField.database_type)
type DatabaseType int

const (
	// DatabaseTypeMilvus Milvus 向量数据库
	DatabaseTypeMilvus DatabaseType = iota
	// DatabaseTypeChroma ChromaDB 向量数据库
	DatabaseTypeChroma
	// DatabaseTypePG PostgreSQL + pgvector 向量数据库
	DatabaseTypePG
)

// IndexType 向量索引类型。
//
// 对应 Python: vector_fields/base.py (VectorField.index_type)
type IndexType int

const (
	// IndexTypeAUTO 自动选择索引类型
	IndexTypeAUTO IndexType = iota
	// IndexTypeHNSW HNSW 索引
	IndexTypeHNSW
	// IndexTypeFLAT FLAT 索引（精确搜索）
	IndexTypeFLAT
	// IndexTypeIVF IVF 索引
	IndexTypeIVF
	// IndexTypeSCANN SCANN 索引
	IndexTypeSCANN
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// StageConstruct 构建阶段（建索引时的参数）
	StageConstruct = "construct"
	// StageSearch 搜索阶段（查询时的参数）
	StageSearch = "search"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// databaseTypeStrings DatabaseType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
	databaseTypeStrings = [...]string{
		"milvus",
		"chroma",
		"pg",
	}
	// indexTypeStrings IndexType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
	indexTypeStrings = [...]string{
		"auto",
		"hnsw",
		"flat",
		"ivf",
		"scann",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorField 创建向量索引配置基类实例。
//
// 对应 Python: VectorField(vector_field=..., database_type=..., index_type=...)
func NewVectorField(dbType DatabaseType, indexType IndexType, fieldName string) *VectorField {
	return &VectorField{
		DatabaseType:    dbType,
		IndexType:       indexType,
		VectorFieldName: fieldName,
	}
}

// String 返回 DatabaseType 的字符串表示，与 Python 枚举值一致。
func (dt DatabaseType) String() string {
	if dt >= 0 && int(dt) < len(databaseTypeStrings) {
		return databaseTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}

// String 返回 IndexType 的字符串表示，与 Python 枚举值一致。
func (it IndexType) String() string {
	if it >= 0 && int(it) < len(indexTypeStrings) {
		return indexTypeStrings[it]
	}
	return fmt.Sprintf("UNKNOWN(%d)", it)
}

// Validate 校验配置参数，子类可覆盖实现自定义校验逻辑。
//
// 基类默认返回 nil。对应 Python: VectorField 子类的 @model_validator 逻辑。
func (vf *VectorField) Validate() error {
	return nil
}

// ToDict 将向量索引配置转为字典格式，只输出指定阶段的字段。
//
// v 参数为指向 VectorField 或其子类实例的指针。
// 通过反射读取 vf 结构体标签，过滤规则：
//   - vf:"-" 或无标签 → 跳过（内部字段）
//   - vf 标签 stage 与参数不匹配 → 跳过
//   - vf 标签 stage 匹配 → 检查零值和 Extra 合并
//   - 无 keepzero 修饰且为零值 → 跳过
//   - 字段名以 Extra 开头且类型为 map[string]any → 展开合并到结果
//
// 对应 Python: VectorField.to_dict(stage)
func ToDict(v any, stage string) map[string]any {
	result := make(map[string]any)
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	collectFields(rv, stage, result)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseVFTag 解析 vf 结构体标签，返回 stage 和是否 keepzero。
//
// 标签格式："<stage>" 或 "<stage>,keepzero"
// 示例："construct" → ("construct", false)
//
//	"search,keepzero" → ("search", true)
//	"-" → ("-", false)
//	"" → ("", false)
func parseVFTag(tag string) (stage string, keepZero bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	stage = parts[0]
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "keepzero" {
			keepZero = true
		}
	}
	return stage, keepZero
}

// collectFields 递归收集匹配 stage 的字段到 result 中。
// 处理嵌入结构体的提升字段，跳过嵌入字段本身。
func collectFields(v reflect.Value, stage string, result map[string]any) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// 匿名嵌入字段：递归处理其子字段
		if field.Anonymous && fieldValue.Kind() == reflect.Struct {
			collectFields(fieldValue, stage, result)
			continue
		}

		// 解析 vf 标签
		tagStr, ok := field.Tag.Lookup("vf")
		if !ok {
			continue // 无 vf 标签，当作内部字段跳过
		}
		fieldStage, keepZero := parseVFTag(tagStr)

		// 过滤内部字段和阶段不匹配的字段
		if fieldStage == "-" || fieldStage != stage {
			continue
		}

		// Extra 字段合并逻辑：字段名以 Extra 开头且类型为 map[string]any
		if strings.HasPrefix(field.Name, "Extra") && fieldValue.Type() == reflect.TypeOf(map[string]any{}) {
			if fieldValue.IsNil() {
				continue
			}
			extraMap := fieldValue.Interface().(map[string]any)
			for k, v := range extraMap {
				result[k] = v
			}
			continue
		}

		// 零值过滤（除非 keepzero）
		if !keepZero && fieldValue.IsZero() {
			continue
		}

		result[field.Name] = fieldValue.Interface()
	}
}
