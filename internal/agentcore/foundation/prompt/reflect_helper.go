package prompt

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// accessStructField 使用 reflect 从结构体或结构体指针中按名称访问字段。
// 字段名匹配规则：优先精确匹配导出字段名，再尝试不区分大小写匹配。
// 对应 Python: getattr(value, node)
func accessStructField(current any, field string) (any, error) {
	rv := reflect.ValueOf(current)

	// 解引用指针
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, fmt.Errorf("nil pointer when accessing field %q", field)
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("cannot access field %q on type %T (not a struct)", field, current)
	}

	// 精确匹配
	f := rv.FieldByName(field)
	if f.IsValid() && canExport(f) {
		return f.Interface(), nil
	}

	// 不区分大小写匹配（Python getattr 是大小写敏感的，但 Go 结构体字段
	// 通常首字母大写而 Python 属性首字母小写，因此做兼容处理）
	for i := 0; i < rv.NumField(); i++ {
		sf := rv.Type().Field(i)
		if canExportField(sf) && strings.EqualFold(sf.Name, field) {
			return rv.Field(i).Interface(), nil
		}
	}

	return nil, fmt.Errorf("field %q not found in type %T", field, current)
}

// canExport 检查 reflect.Value 对应的字段是否可导出。
func canExport(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	// 通过 CanInterface 间接判断：不可导出的字段 CanInterface 返回 false
	return v.CanInterface()
}

// canExportField 检查结构体字段是否可导出。
func canExportField(sf reflect.StructField) bool {
	return sf.PkgPath == "" && unicode.IsUpper(rune(sf.Name[0]))
}
