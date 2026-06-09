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
