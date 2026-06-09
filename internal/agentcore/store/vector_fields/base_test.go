package vector_fields

import (
	"fmt"
	"testing"
)

// ──── DatabaseType 枚举测试 ────

// TestDatabaseType_String 验证枚举值与 Python 字符串一致
func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		dt       DatabaseType
		expected string
	}{
		{DatabaseTypeMilvus, "milvus"},
		{DatabaseTypeChroma, "chroma"},
		{DatabaseTypePG, "pg"},
		{DatabaseTypeGauss, "gauss"},
		{DatabaseTypeES, "es"},
	}
	for _, tt := range tests {
		if got := tt.dt.String(); got != tt.expected {
			t.Errorf("DatabaseType(%d).String() = %q, 期望 %q", tt.dt, got, tt.expected)
		}
	}
}

// TestDatabaseType_无效值 验证未定义枚举值的字符串输出
func TestDatabaseType_无效值(t *testing.T) {
	dt := DatabaseType(999)
	if got := dt.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}

// TestDatabaseType_所有枚举值 验证所有枚举值在有效范围内
func TestDatabaseType_所有枚举值(t *testing.T) {
	for i := DatabaseTypeMilvus; i <= DatabaseTypeES; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("DatabaseType(%d) 缺少字符串表示", i)
		}
	}
}

// ──── IndexType 枚举测试 ────

// TestIndexType_String 验证枚举值与 Python 字符串一致
func TestIndexType_String(t *testing.T) {
	tests := []struct {
		it       IndexType
		expected string
	}{
		{IndexTypeAUTO, "auto"},
		{IndexTypeHNSW, "hnsw"},
		{IndexTypeFLAT, "flat"},
		{IndexTypeIVF, "ivf"},
		{IndexTypeSCANN, "scann"},
		{IndexTypeIVFFlat, "ivfflat"},
	}
	for _, tt := range tests {
		if got := tt.it.String(); got != tt.expected {
			t.Errorf("IndexType(%d).String() = %q, 期望 %q", tt.it, got, tt.expected)
		}
	}
}

// TestIndexType_无效值 验证未定义枚举值的字符串输出
func TestIndexType_无效值(t *testing.T) {
	it := IndexType(999)
	if got := it.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}

// TestIndexType_所有枚举值 验证所有枚举值在有效范围内
func TestIndexType_所有枚举值(t *testing.T) {
	for i := IndexTypeAUTO; i <= IndexTypeIVFFlat; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("IndexType(%d) 缺少字符串表示", i)
		}
	}
}

// ──── VectorField 构造测试 ────

// TestNewVectorField_基本创建 验证基本创建
func TestNewVectorField_基本创建(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, "embedding")
	if vf.DatabaseType != DatabaseTypeMilvus {
		t.Errorf("DatabaseType = %v, 期望 %v", vf.DatabaseType, DatabaseTypeMilvus)
	}
	if vf.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, 期望 %v", vf.IndexType, IndexTypeHNSW)
	}
	if vf.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %q, 期望 %q", vf.VectorFieldName, "embedding")
	}
}

// TestVectorField_Validate_基类默认 验证基类 Validate 返回 nil
func TestVectorField_Validate_基类默认(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeAUTO, "embedding")
	if err := vf.Validate(); err != nil {
		t.Errorf("基类 Validate() 应返回 nil, 实际: %v", err)
	}
}

// ──── parseVFTag 标签解析测试 ────

// TestParseVFTag_各格式 验证各种标签格式的解析结果
func TestParseVFTag_各格式(t *testing.T) {
	tests := []struct {
		name         string
		tag          string
		wantStage    string
		wantKeepZero bool
	}{
		{"construct", "construct", "construct", false},
		{"search", "search", "search", false},
		{"dash", "-", "-", false},
		{"construct_keepzero", "construct,keepzero", "construct", true},
		{"search_keepzero", "search,keepzero", "search", true},
		{"空字符串", "", "", false},
		{"unknown", "unknown", "unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, keepZero := parseVFTag(tt.tag)
			if stage != tt.wantStage {
				t.Errorf("stage = %q, 期望 %q", stage, tt.wantStage)
			}
			if keepZero != tt.wantKeepZero {
				t.Errorf("keepZero = %v, 期望 %v", keepZero, tt.wantKeepZero)
			}
		})
	}
}

// ──── 测试用子类 ────

// testVectorField 测试用子类，模拟未来子类的字段标记方式
type testVectorField struct {
	VectorField
	// ConstructParam 构建阶段参数
	ConstructParam int `vf:"construct"`
	// SearchParam 搜索阶段参数
	SearchParam float64 `vf:"search"`
	// KeepZeroParam 带零值保留的搜索参数
	KeepZeroParam int `vf:"search,keepzero"`
	// IgnoredField 无 vf 标签，应被过滤
	IgnoredField string
	// ExtraConstruct 构建阶段额外参数
	ExtraConstruct map[string]any `vf:"construct"`
	// ExtraSearch 搜索阶段额外参数
	ExtraSearch map[string]any `vf:"search"`
}

// ──── ToDict 反射机制测试 ────

// TestToDict_Construct阶段 验证只输出 construct 阶段字段
func TestToDict_Construct阶段(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		SearchParam:    100.0,
	}
	got := ToDict(vf, StageConstruct)
	if _, ok := got["ConstructParam"]; !ok {
		t.Error("construct 阶段应包含 ConstructParam")
	}
	if got["ConstructParam"] != 30 {
		t.Errorf("ConstructParam = %v, 期望 30", got["ConstructParam"])
	}
	if _, ok := got["SearchParam"]; ok {
		t.Error("construct 阶段不应包含 SearchParam")
	}
}

// TestToDict_Search阶段 验证只输出 search 阶段字段
func TestToDict_Search阶段(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		SearchParam:    100.0,
	}
	got := ToDict(vf, StageSearch)
	if _, ok := got["SearchParam"]; !ok {
		t.Error("search 阶段应包含 SearchParam")
	}
	if got["SearchParam"] != 100.0 {
		t.Errorf("SearchParam = %v, 期望 100.0", got["SearchParam"])
	}
	if _, ok := got["ConstructParam"]; ok {
		t.Error("search 阶段不应包含 ConstructParam")
	}
}

// TestToDict_零值过滤 验证默认行为下零值不输出
func TestToDict_零值过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 0,
		SearchParam:    0.0,
	}
	got := ToDict(vf, StageConstruct)
	if _, ok := got["ConstructParam"]; ok {
		t.Error("零值 ConstructParam 应被过滤")
	}
	got = ToDict(vf, StageSearch)
	if _, ok := got["SearchParam"]; ok {
		t.Error("零值 SearchParam 应被过滤")
	}
}

// TestToDict_KeepZero保留零值 验证 keepzero 修饰符保留零值
func TestToDict_KeepZero保留零值(t *testing.T) {
	vf := &testVectorField{
		VectorField:   VectorField{DatabaseType: DatabaseTypePG, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		KeepZeroParam: 0,
	}
	got := ToDict(vf, StageSearch)
	if _, ok := got["KeepZeroParam"]; !ok {
		t.Error("keepzero 字段零值应保留")
	}
	if got["KeepZeroParam"] != 0 {
		t.Errorf("KeepZeroParam = %v, 期望 0", got["KeepZeroParam"])
	}
}

// TestToDict_内部字段过滤 验证 vf:"-" 内部字段不输出
func TestToDict_内部字段过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
	}
	for _, stage := range []string{StageConstruct, StageSearch} {
		got := ToDict(vf, stage)
		if _, ok := got["DatabaseType"]; ok {
			t.Errorf("%s 阶段不应包含 DatabaseType", stage)
		}
		if _, ok := got["IndexType"]; ok {
			t.Errorf("%s 阶段不应包含 IndexType", stage)
		}
		if _, ok := got["VectorFieldName"]; ok {
			t.Errorf("%s 阶段不应包含 VectorFieldName", stage)
		}
	}
}

// TestToDict_无标签字段过滤 验证无 vf 标签字段不输出
func TestToDict_无标签字段过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		IgnoredField:   "should_not_appear",
		ConstructParam: 30,
	}
	got := ToDict(vf, StageConstruct)
	if _, ok := got["IgnoredField"]; ok {
		t.Error("无 vf 标签字段不应输出")
	}
}

// TestToDict_Extra合并 验证 ExtraConstruct/ExtraSearch 展开合并
func TestToDict_Extra合并(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: map[string]any{"custom_key": "custom_value"},
		ExtraSearch:    map[string]any{"search_key": 42},
	}
	got := ToDict(vf, StageConstruct)
	if got["custom_key"] != "custom_value" {
		t.Errorf("ExtraConstruct 合并后 custom_key = %v, 期望 %q", got["custom_key"], "custom_value")
	}
	if _, ok := got["ExtraConstruct"]; ok {
		t.Error("ExtraConstruct 不应作为 key 出现在结果中")
	}
	got = ToDict(vf, StageSearch)
	if got["search_key"] != 42 {
		t.Errorf("ExtraSearch 合并后 search_key = %v, 期望 42", got["search_key"])
	}
}

// TestToDict_ExtraNil和空 验证 Extra 为 nil 或空时不影响输出
func TestToDict_ExtraNil和空(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: nil,
		ExtraSearch:    map[string]any{},
	}
	got := ToDict(vf,  StageConstruct)
	if got["ConstructParam"] != 30 {
		t.Errorf("ConstructParam = %v, 期望 30", got["ConstructParam"])
	}
	// construct 阶段不应包含 nil ExtraConstruct
	if _, ok := got["ExtraConstruct"]; ok {
		t.Error("nil ExtraConstruct 不应作为 key 出现")
	}
	got = ToDict(vf,  StageSearch)
	// KeepZeroParam 有 keepzero 标记，零值也会输出
	if _, ok := got["KeepZeroParam"]; !ok {
		t.Error("KeepZeroParam 有 keepzero 标记，零值也应输出")
	}
}

// TestToDict_Extra覆盖 验证 Extra 字段 key 覆盖普通字段 key
func TestToDict_Extra覆盖(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: map[string]any{"ConstructParam": 999},
	}
	got := ToDict(vf, StageConstruct)
	if got["ConstructParam"] != 999 {
		t.Errorf("Extra 覆盖后 ConstructParam = %v, 期望 999", got["ConstructParam"])
	}
}

// TestToDict_嵌入子类 验证子类嵌入 VectorField 后字段正确读取
func TestToDict_嵌入子类(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeChroma, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 16,
		SearchParam:    100.0,
		KeepZeroParam:  0,
	}
	got := ToDict(vf, StageConstruct)
	if got["ConstructParam"] != 16 {
		t.Errorf("ConstructParam = %v, 期望 16", got["ConstructParam"])
	}
	got = ToDict(vf, StageSearch)
	if got["SearchParam"] != 100.0 {
		t.Errorf("SearchParam = %v, 期望 100.0", got["SearchParam"])
	}
	if got["KeepZeroParam"] != 0 {
		t.Errorf("KeepZeroParam = %v, 期望 0", got["KeepZeroParam"])
	}
}

// TestToDict_基类实例 验证纯基类实例 ToDict 返回空 map
func TestToDict_基类实例(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, "embedding")
	got := ToDict(vf, StageConstruct)
	if len(got) != 0 {
		t.Errorf("纯基类 ToDict 应返回空 map, 实际: %v", got)
	}
	got = ToDict(vf, StageSearch)
	if len(got) != 0 {
		t.Errorf("纯基类 ToDict 应返回空 map, 实际: %v", got)
	}
}
