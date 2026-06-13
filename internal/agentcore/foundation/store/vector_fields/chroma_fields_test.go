package vector_fields

import "testing"

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
