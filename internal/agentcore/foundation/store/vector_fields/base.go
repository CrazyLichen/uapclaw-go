package vector_fields

import (
	"fmt"
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

type VectorField struct {
	// DatabaseType 向量数据库类型
	DatabaseType DatabaseType `vf:"-"`
	// IndexType 索引类型
	IndexType IndexType `vf:"-"`
	// VectorFieldName 向量字段名
	VectorFieldName string `vf:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

type DatabaseType int

type IndexType int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DatabaseTypeMilvus Milvus 向量数据库
	DatabaseTypeMilvus DatabaseType = iota
	// DatabaseTypeChroma ChromaDB 向量数据库
	DatabaseTypeChroma
	// DatabaseTypePG PostgreSQL + pgvector 向量数据库
	DatabaseTypePG
	// DatabaseTypeGauss GaussDB 向量数据库
	DatabaseTypeGauss
	// DatabaseTypeES Elasticsearch 向量数据库
	DatabaseTypeES
)

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
	// IndexTypeDiskANN DiskANN 索引（GaussDB 专用）
	IndexTypeDiskANN
)

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
		"gauss",
		"es",
	}
	// indexTypeStrings IndexType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
	indexTypeStrings = [...]string{
		"auto",
		"hnsw",
		"flat",
		"ivf",
		"scann",
		"diskann",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

func NewVectorField(dbType DatabaseType, indexType IndexType, fieldName string) *VectorField {
	return &VectorField{
		DatabaseType:    dbType,
		IndexType:       indexType,
		VectorFieldName: fieldName,
	}
}

func (dt DatabaseType) String() string {
	if dt >= 0 && int(dt) < len(databaseTypeStrings) {
		return databaseTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}

func (it IndexType) String() string {
	if it >= 0 && int(it) < len(indexTypeStrings) {
		return indexTypeStrings[it]
	}
	return fmt.Sprintf("UNKNOWN(%d)", it)
}

func (vf *VectorField) Validate() error {
	return nil
}

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
