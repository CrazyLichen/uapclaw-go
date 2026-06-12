package gaussdb

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// TestGaussDialector_Initialize_正常初始化 验证 Initialize 完成后
// LOCKING 子句构建器已正确注册、序列化器已注册、无错误返回。
// 同时覆盖 db.ClauseBuilders 为 nil 的分支。
func TestGaussDialector_Initialize_正常初始化(t *testing.T) {
	// 使用 sqlite 内存数据库创建 *sql.DB 连接池
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// 创建 GaussDialector，注入 sqlite 的 *sql.DB 作为 Conn
	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				Conn: sqlDB,
			},
		},
	}

	// 构造 *gorm.DB 供 Initialize 使用（ClauseBuilders 为 nil，覆盖 nil 分支）
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	db.ClauseBuilders = nil

	// 调用 Initialize
	if err := dialector.Initialize(db); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}

	// 验证 ClauseBuilders 已注册
	if db.ClauseBuilders == nil {
		t.Fatal("Initialize 后 ClauseBuilders 为 nil")
	}
	forBuilder, ok := db.ClauseBuilders["FOR"]
	if !ok {
		t.Fatal("Initialize 后 ClauseBuilders 中未注册 'FOR' 键")
	}
	if forBuilder == nil {
		t.Fatal("Initialize 后 'FOR' ClauseBuilder 为 nil")
	}
}

// TestGaussDialector_Initialize_ClauseBuilders已存在 验证 Initialize 在
// db.ClauseBuilders 非 nil 时不覆盖已有映射，仅追加 FOR 键。
func TestGaussDialector_Initialize_ClauseBuilders已存在(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				Conn: sqlDB,
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}

	// 预设 ClauseBuilders，包含一个自定义键
	existingBuilder := func(c clause.Clause, b clause.Builder) { c.Build(b) }
	db.ClauseBuilders = map[string]clause.ClauseBuilder{
		"EXISTING": existingBuilder,
	}

	if err := dialector.Initialize(db); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}

	// 验证已有键仍存在
	if _, ok := db.ClauseBuilders["EXISTING"]; !ok {
		t.Fatal("Initialize 覆盖了已有的 ClauseBuilders 映射")
	}
	// 验证 FOR 键已注册
	if _, ok := db.ClauseBuilders["FOR"]; !ok {
		t.Fatal("Initialize 未注册 'FOR' ClauseBuilder")
	}
}

// TestGaussDialector_Initialize_LOCKING子句构建器 验证 Initialize 注册的
// FOR ClauseBuilder 是 gaussLockingClauseBuilder（通过行为间接验证）。
func TestGaussDialector_Initialize_LOCKING子句构建器(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				Conn: sqlDB,
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}

	if err := dialector.Initialize(db); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}

	// 间接验证：通过构建 LOCKING 子句确认构建器行为正确
	// gaussLockingClauseBuilder 在遇到 clause.Locking 时仅输出 "FOR <strength>"
	builder, ok := db.ClauseBuilders["FOR"]
	if !ok {
		t.Fatal("未注册 FOR ClauseBuilder")
	}

	// 构造一个包含 Locking 表达式的 Clause
	lockingClause := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Options:  "NOWAIT", // GaussDB 应忽略此选项
		},
	}

	// 使用 gorm.Statement 作为 Builder 来捕获输出
	stmt := &gorm.Statement{DB: db, Clauses: map[string]clause.Clause{}}
	builder(lockingClause, stmt)

	got := stmt.SQL.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("LOCKING 子句构建结果 = %q, want %q", got, want)
	}
}

// TestGaussDialector_Initialize_序列化器注册 验证 Initialize 注册了
// gauss_string 序列化器。
func TestGaussDialector_Initialize_序列化器注册(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				Conn: sqlDB,
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}

	if err := dialector.Initialize(db); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}

	// 验证 gauss_string 序列化器已注册
	serializer, ok := schema.GetSerializer("gauss_string")
	if !ok {
		t.Fatal("Initialize 后 gauss_string 序列化器未注册")
	}
	if _, ok := serializer.(gaussStringSerializer); !ok {
		t.Errorf("gauss_string 序列化器类型 = %T, want gaussStringSerializer", serializer)
	}
}

// TestGaussDialector_Initialize_Postgres初始化失败 验证当 postgres 初始化
// 失败时，Initialize 返回错误并记录日志。
func TestGaussDialector_Initialize_Postgres初始化失败(t *testing.T) {
	// 使用无法解析的 DSN，pgx.ParseConfig 将返回错误
	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				DSN: "postgres://invalid\x00dsn",
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}

	err = dialector.Initialize(db)
	if err == nil {
		t.Fatal("期望 Initialize 返回错误，但返回 nil")
	}
}

// TestGaussDialector_Migrator_返回GaussMigrator 验证 Migrator 返回
// 包含正确配置的 GaussMigrator 实例。
func TestGaussDialector_Migrator_返回GaussMigrator(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	dialector := GaussDialector{
		Dialector: postgres.Dialector{
			Config: &postgres.Config{
				Conn: sqlDB,
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 内存数据库失败: %v", err)
	}

	if err := dialector.Initialize(db); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}

	m := dialector.Migrator(db)
	if m == nil {
		t.Fatal("Migrator 返回 nil")
	}
	gm, ok := m.(GaussMigrator)
	if !ok {
		t.Errorf("Migrator 类型 = %T, want GaussMigrator", m)
	}
	_ = gm
}
