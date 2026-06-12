package gaussdb

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"
)

// ──────────────────────────── GaussDialector 测试 ────────────────────────────

// TestGaussDialector_Name 验证 Name 返回 "gaussdb"。
func TestGaussDialector_Name(t *testing.T) {
	d := GaussDialector{}
	if got := d.Name(); got != "gaussdb" {
		t.Errorf("Name() = %q, want %q", got, "gaussdb")
	}
}

// TestGaussDialector_Name_非Postgres 验证 Name 不返回 "postgres"。
func TestGaussDialector_Name_非Postgres(t *testing.T) {
	d := GaussDialector{}
	if d.Name() == "postgres" {
		t.Error("Name() 返回了 'postgres'，应该返回 'gaussdb'")
	}
}

// TestGaussDialector_Migrator_方法存在 验证 Migrator 方法签名正确。
func TestGaussDialector_Migrator_方法存在(t *testing.T) {
	d := GaussDialector{}
	// 仅验证方法存在且签名正确，不调用（需要真实 *gorm.DB）
	_ = d.Migrator
}

// TestGaussOpen_返回GaussDialector 验证 GaussOpen 返回正确类型。
func TestGaussOpen_返回GaussDialector(t *testing.T) {
	d := GaussOpen("host=localhost")
	if _, ok := d.(GaussDialector); !ok {
		t.Error("GaussOpen 未返回 GaussDialector 类型")
	}
}

// TestGaussNew_返回GaussDialector 验证 GaussNew 返回正确类型。
func TestGaussNew_返回GaussDialector(t *testing.T) {
	d := GaussNew(postgres.Config{DSN: "host=localhost"})
	if _, ok := d.(GaussDialector); !ok {
		t.Error("GaussNew 未返回 GaussDialector 类型")
	}
}

// TestGaussDialector_DataTypeOf_UUID 验证 UUID 类型映射为 varchar(36)。
func TestGaussDialector_DataTypeOf_UUID(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "uuid",
	}
	got := d.DataTypeOf(field)
	want := "varchar(36)"
	if got != want {
		t.Errorf("DataTypeOf(uuid) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_Enum 验证 ENUM 类型映射为 varchar。
func TestGaussDialector_DataTypeOf_Enum(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "enum",
	}
	got := d.DataTypeOf(field)
	want := "varchar"
	if got != want {
		t.Errorf("DataTypeOf(enum) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_String 验证 String 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_String(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.String,
	}
	got := d.DataTypeOf(field)
	// postgres 对 schema.String 返回 "text" 或 "varchar(n)"
	if !strings.HasPrefix(got, "text") && !strings.HasPrefix(got, "varchar") {
		t.Errorf("DataTypeOf(String) = %q, 期望 text 或 varchar 前缀", got)
	}
}

// TestGaussDialector_DataTypeOf_Int 验证 Int 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_Int(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.Int,
	}
	got := d.DataTypeOf(field)
	// postgres 对 schema.Int 返回 "integer" 或 "bigint" 等
	if got == "" {
		t.Error("DataTypeOf(Int) 返回空字符串")
	}
}

// TestGaussDialector_DataTypeOf_Bool 验证 Bool 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_Bool(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.Bool,
	}
	got := d.DataTypeOf(field)
	if got != "boolean" {
		t.Errorf("DataTypeOf(Bool) = %q, want %q", got, "boolean")
	}
}

// TestGaussDialector_DataTypeOf_UUIDMixedCase 验证 UUID 类型（大写）也能映射。
func TestGaussDialector_DataTypeOf_UUIDMixedCase(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "Uuid",
	}
	got := d.DataTypeOf(field)
	want := "varchar(36)"
	if got != want {
		t.Errorf("DataTypeOf(Uuid) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_EnumMixedCase 验证 ENUM 类型（大写）也能映射。
func TestGaussDialector_DataTypeOf_EnumMixedCase(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "ENUM",
	}
	got := d.DataTypeOf(field)
	want := "varchar"
	if got != want {
		t.Errorf("DataTypeOf(ENUM) = %q, want %q", got, want)
	}
}
