package vector_fields

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewChromaVectorField(t *testing.T) {
	c := NewChromaVectorField("embedding", 16, 100, 10.0)
	if c.DatabaseType != DatabaseTypeChroma {
		t.Errorf("DatabaseType = %v, want %v", c.DatabaseType, DatabaseTypeChroma)
	}
	if c.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", c.IndexType, IndexTypeHNSW)
	}
	if c.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", c.VectorFieldName)
	}
	if c.MaxNeighbors != 16 {
		t.Errorf("MaxNeighbors = %v, want 16", c.MaxNeighbors)
	}
	if c.EfConstruction != 100 {
		t.Errorf("EfConstruction = %v, want 100", c.EfConstruction)
	}
	if c.EfSearch != 10.0 {
		t.Errorf("EfSearch = %v, want 10.0", c.EfSearch)
	}
}

func TestChromaVectorField_Validate_正常(t *testing.T) {
	c := NewChromaVectorField("embedding", 16, 100, 10.0)
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestChromaVectorField_Validate_参数无效(t *testing.T) {
	tests := []struct {
		name           string
		maxNeighbors   int
		efConstruction int
		efSearch       float64
		wantErr        bool
	}{
		{"MaxNeighbors太小", 1, 100, 10.0, true},
		{"MaxNeighbors太大", 2049, 100, 10.0, true},
		{"MaxNeighbors下界", 2, 100, 10.0, false},
		{"MaxNeighbors上界", 2048, 100, 10.0, false},
		{"EfConstruction为零", 16, 0, 10.0, true},
		{"EfConstruction为负数", 16, -1, 10.0, true},
		{"EfConstruction下界", 16, 1, 10.0, false},
		{"EfSearch为零", 16, 100, 0.0, true},
		{"EfSearch为负数", 16, 100, -1.0, true},
		{"EfSearch下界", 16, 100, 1.0, false},
		{"合法值", 16, 100, 10.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChromaVectorField("embedding", tt.maxNeighbors, tt.efConstruction, tt.efSearch)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChromaVectorField_ToDict_construct(t *testing.T) {
	c := NewChromaVectorField("embedding", 16, 100, 10.0)
	dict := ToDict(c, StageConstruct)
	if v, ok := dict["MaxNeighbors"]; !ok {
		t.Error("construct 阶段应包含 MaxNeighbors")
	} else if v != 16 {
		t.Errorf("MaxNeighbors = %v, want 16", v)
	}
	if v, ok := dict["EfConstruction"]; !ok {
		t.Error("construct 阶段应包含 EfConstruction")
	} else if v != 100 {
		t.Errorf("EfConstruction = %v, want 100", v)
	}
	if v, ok := dict["EfSearch"]; !ok {
		t.Error("construct 阶段应包含 EfSearch")
	} else if v != 10.0 {
		t.Errorf("EfSearch = %v, want 10.0", v)
	}
}

func TestChromaVectorField_ToDict_search(t *testing.T) {
	c := NewChromaVectorField("embedding", 16, 100, 10.0)
	dict := ToDict(c, StageSearch)
	// 无 ExtraSearch 时，search 阶段应为空
	if len(dict) != 0 {
		t.Errorf("search 阶段无 ExtraSearch 时 dict 应为空，实际 %v", dict)
	}
}

func TestChromaVectorField_ToDict_ExtraSearch(t *testing.T) {
	c := NewChromaVectorField("embedding", 16, 100, 10.0)
	c.ExtraSearch = map[string]any{
		"resize_factor": 1.5,
		"num_threads":   4,
	}
	dict := ToDict(c, StageSearch)
	if v, ok := dict["resize_factor"]; !ok {
		t.Error("search 阶段应包含 ExtraSearch 展开的 resize_factor")
	} else if v != 1.5 {
		t.Errorf("resize_factor = %v, want 1.5", v)
	}
	if v, ok := dict["num_threads"]; !ok {
		t.Error("search 阶段应包含 ExtraSearch 展开的 num_threads")
	} else if v != 4 {
		t.Errorf("num_threads = %v, want 4", v)
	}
}

// TestNewDefaultChromaVectorField 测试默认参数构造
func TestNewDefaultChromaVectorField(t *testing.T) {
	f := NewDefaultChromaVectorField()
	assert.Equal(t, "embedding", f.VectorFieldName)
	assert.Equal(t, DatabaseTypeChroma, f.DatabaseType)
	assert.Equal(t, IndexTypeHNSW, f.IndexType)
	assert.Equal(t, 16, f.MaxNeighbors)
	assert.Equal(t, 100, f.EfConstruction)
	assert.Equal(t, 100.0, f.EfSearch)
}

// TestNewChromaVectorFieldFromName 测试从字段名创建
func TestNewChromaVectorFieldFromName(t *testing.T) {
	f := NewChromaVectorFieldFromName("my_vector")
	assert.Equal(t, "my_vector", f.VectorFieldName)
	assert.Equal(t, DatabaseTypeChroma, f.DatabaseType)
	assert.Equal(t, IndexTypeHNSW, f.IndexType)
	assert.Equal(t, 16, f.MaxNeighbors)
}

// TestChromaVectorField_ToConstructDict 测试 ToConstructDict 便捷方法
func TestChromaVectorField_ToConstructDict(t *testing.T) {
	f := NewDefaultChromaVectorField()
	result := f.ToConstructDict()

	// stage:"-" 的字段应被移除
	assert.NotContains(t, result, "DatabaseType")
	assert.NotContains(t, result, "IndexType")
	assert.NotContains(t, result, "VectorFieldName")

	// stage:"construct" 的字段应保留
	assert.Equal(t, 16, result["MaxNeighbors"])
	assert.Equal(t, 100, result["EfConstruction"])
	assert.Equal(t, 100.0, result["EfSearch"])

	// stage:"search" 的字段应被移除
	assert.NotContains(t, result, "ExtraSearch")
}

// TestChromaVectorField_ToSearchDict_无额外参数 测试无 ExtraSearch 时返回空
func TestChromaVectorField_ToSearchDict_无额外参数(t *testing.T) {
	f := NewDefaultChromaVectorField()
	result := f.ToSearchDict()
	assert.Empty(t, result)
}

// TestChromaVectorField_ToSearchDict_有额外参数 测试 ExtraSearch 展开
func TestChromaVectorField_ToSearchDict_有额外参数(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor": 1.5,
		"num_threads":   4,
	}
	result := f.ToSearchDict()

	// ExtraSearch 应被展开
	assert.NotContains(t, result, "ExtraSearch")
	assert.Equal(t, 1.5, result["resize_factor"])
	assert.Equal(t, 4, result["num_threads"])

	// construct 阶段字段不应出现
	assert.NotContains(t, result, "MaxNeighbors")
	assert.NotContains(t, result, "EfConstruction")
}

// TestChromaVectorField_ValidateExtraSearch_合法 测试合法的 ExtraSearch
func TestChromaVectorField_ValidateExtraSearch_合法(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor":  1.5,
		"num_threads":    4,
		"batch_size":     100,
		"sync_threshold": 1000,
	}
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}

// TestChromaVectorField_ValidateExtraSearch_resizeFactor类型错误 测试非法类型
func TestChromaVectorField_ValidateExtraSearch_resizeFactor类型错误(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor": "invalid",
	}
	err := f.ValidateExtraSearch()
	assert.Error(t, err)
}

// TestChromaVectorField_ValidateExtraSearch_numThreads类型错误 测试 int 属性传 float
func TestChromaVectorField_ValidateExtraSearch_numThreads类型错误(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{
		"num_threads": 3.14,
	}
	err := f.ValidateExtraSearch()
	assert.Error(t, err)
}

// TestChromaVectorField_ValidateExtraSearch_空字典 测试空字典
func TestChromaVectorField_ValidateExtraSearch_空字典(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{}
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}

// TestChromaVectorField_ValidateExtraSearch_nil 测试 nil 不校验
func TestChromaVectorField_ValidateExtraSearch_nil(t *testing.T) {
	f := NewDefaultChromaVectorField()
	require.NoError(t, f.ValidateExtraSearch())
}

// TestChromaVectorField_ValidateExtraSearch_int类型合法 测试 resize_factor 传 int 也合法
func TestChromaVectorField_ValidateExtraSearch_int类型合法(t *testing.T) {
	f := NewDefaultChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor": 2,
	}
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}
