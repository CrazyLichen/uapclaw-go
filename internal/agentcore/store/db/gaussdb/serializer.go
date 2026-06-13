package gaussdb

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// gaussStringSerializer GaussDB 字符串序列化器。
//
// 对应 Python: GaussString.bind_processor()
//
// 确保所有绑定到 string 列的非 string 值在进入驱动前被转换为 string。
// 特别处理 time.Time → "2006-01-02 15:04:05.000000" 格式，
// 对标 Python 的 datetime.strftime('%Y-%m-%d %H:%M:%S.%f')。
//
// 跳过 parent_processor 说明：
// Python 在类型转换后还会调用 String.bind_processor 的返回值（parent_processor），
// 但在 PostgreSQL 方言下 parent_processor 是空操作（直接返回原值），
// 因此 Go 跳过此步骤不会影响当前行为。若未来方言行为变化需重新评估。
type gaussStringSerializer struct{}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Value 实现 schema.SerializerInterface，将 Go 值转换为数据库值。
// 对标 Python GaussString.bind_processor().process(value)：
//   - string → 直接返回
//   - time.Time → "2006-01-02 15:04:05.000000"
//   - nil → nil
//   - 其他 → fmt.Sprintf("%v", v)
func (s gaussStringSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue interface{}) (interface{}, error) {
	switch v := fieldValue.(type) {
	case string:
		return v, nil
	case time.Time:
		return v.Format("2006-01-02 15:04:05.000000"), nil
	case nil:
		return nil, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// Scan 实现 schema.SerializerInterface，从数据库值扫描到 Go 值。
// 对标 Python GaussString 的 result_processor 行为：
//   - string → 通过 field.Set 设置
//   - []byte → string(v) 后通过 field.Set 设置
//   - nil → 不设置
//   - 其他 → fmt.Sprintf("%v", v) 后通过 field.Set 设置
func (s gaussStringSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	if dbValue == nil {
		return nil
	}
	var strVal string
	switch v := dbValue.(type) {
	case string:
		strVal = v
	case []byte:
		strVal = string(v)
	default:
		strVal = fmt.Sprintf("%v", v)
	}
	return field.Set(ctx, dst, strVal)
}
