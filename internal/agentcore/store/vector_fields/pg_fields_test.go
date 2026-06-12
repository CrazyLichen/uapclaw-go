package vector_fields

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewPGVectorFieldHNSW(t *testing.T) {
	p := NewPGVectorFieldHNSW("embedding", 16, 64, 40)
	if p.DatabaseType != DatabaseTypePG {
		t.Errorf("DatabaseType = %v, want %v", p.DatabaseType, DatabaseTypePG)
	}
	if p.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", p.IndexType, IndexTypeHNSW)
	}
	if p.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", p.VectorFieldName)
	}
	if p.M != 16 {
		t.Errorf("M = %v, want 16", p.M)
	}
	if p.EfConstruction != 64 {
		t.Errorf("EfConstruction = %v, want 64", p.EfConstruction)
	}
	if p.EfSearch != 40 {
		t.Errorf("EfSearch = %v, want 40", p.EfSearch)
	}
}

func TestNewPGVectorFieldIVFFlat(t *testing.T) {
	p := NewPGVectorFieldIVFFlat("embedding", 100, 10)
	if p.DatabaseType != DatabaseTypePG {
		t.Errorf("DatabaseType = %v, want %v", p.DatabaseType, DatabaseTypePG)
	}
	if p.IndexType != IndexTypeIVF {
		t.Errorf("IndexType = %v, want %v", p.IndexType, IndexTypeIVF)
	}
	if p.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", p.VectorFieldName)
	}
	if p.Lists != 100 {
		t.Errorf("Lists = %v, want 100", p.Lists)
	}
	if p.Probes != 10 {
		t.Errorf("Probes = %v, want 10", p.Probes)
	}
}

func TestPGVectorField_Validate_HNSW正常(t *testing.T) {
	p := NewPGVectorFieldHNSW("embedding", 16, 64, 40)
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestPGVectorField_Validate_HNSW参数无效(t *testing.T) {
	tests := []struct {
		name          string
		m             int
		efConstruction int
		efSearch      int
		wantErr       bool
	}{
		{"M太小", 1, 64, 40, true},
		{"M太大", 2001, 64, 40, true},
		{"M下界", 2, 64, 40, false},
		{"M上界", 2000, 64, 40, false},
		{"EfConstruction为零", 16, 0, 40, true},
		{"EfConstruction为负数", 16, -1, 40, true},
		{"EfConstruction下界", 16, 1, 40, false},
		{"EfSearch为零", 16, 64, 0, true},
		{"EfSearch为负数", 16, 64, -1, true},
		{"EfSearch下界", 16, 64, 1, false},
		{"合法值", 16, 64, 40, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPGVectorFieldHNSW("embedding", tt.m, tt.efConstruction, tt.efSearch)
			err := p.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPGVectorField_Validate_IVFFlat正常(t *testing.T) {
	p := NewPGVectorFieldIVFFlat("embedding", 100, 10)
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestPGVectorField_Validate_IVFFlat参数无效(t *testing.T) {
	tests := []struct {
		name    string
		lists   int
		probes  int
		wantErr bool
	}{
		{"Lists为零", 0, 10, true},
		{"Lists为负数", -1, 10, true},
		{"Lists下界", 1, 10, false},
		{"Probes为零", 100, 0, true},
		{"Probes为负数", 100, -1, true},
		{"Probes下界", 100, 1, false},
		{"合法值", 100, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPGVectorFieldIVFFlat("embedding", tt.lists, tt.probes)
			err := p.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPGVectorField_ToDict_HNSW_construct(t *testing.T) {
	p := NewPGVectorFieldHNSW("embedding", 16, 64, 40)
	dict := ToDict(p, StageConstruct)
	if v, ok := dict["M"]; !ok {
		t.Error("construct 阶段应包含 M")
	} else if v != 16 {
		t.Errorf("M = %v, want 16", v)
	}
	if v, ok := dict["EfConstruction"]; !ok {
		t.Error("construct 阶段应包含 EfConstruction")
	} else if v != 64 {
		t.Errorf("EfConstruction = %v, want 64", v)
	}
	if v, ok := dict["EfSearch"]; !ok {
		t.Error("construct 阶段应包含 EfSearch")
	} else if v != 40 {
		t.Errorf("EfSearch = %v, want 40", v)
	}
}

func TestPGVectorField_ToDict_HNSW_search(t *testing.T) {
	p := NewPGVectorFieldHNSW("embedding", 16, 64, 40)
	dict := ToDict(p, StageSearch)
	// 无 ExtraSearch 时，search 阶段应为空
	if len(dict) != 0 {
		t.Errorf("search 阶段无 ExtraSearch 时 dict 应为空，实际 %v", dict)
	}
}

func TestPGVectorField_ToDict_IVFFlat_construct(t *testing.T) {
	p := NewPGVectorFieldIVFFlat("embedding", 100, 10)
	dict := ToDict(p, StageConstruct)
	if v, ok := dict["Lists"]; !ok {
		t.Error("construct 阶段应包含 Lists")
	} else if v != 100 {
		t.Errorf("Lists = %v, want 100", v)
	}
	if v, ok := dict["Probes"]; !ok {
		t.Error("construct 阶段应包含 Probes")
	} else if v != 10 {
		t.Errorf("Probes = %v, want 10", v)
	}
}

func TestPGVectorField_ToDict_IVFFlat_search(t *testing.T) {
	p := NewPGVectorFieldIVFFlat("embedding", 100, 10)
	dict := ToDict(p, StageSearch)
	// 无 ExtraSearch 时，search 阶段应为空
	if len(dict) != 0 {
		t.Errorf("search 阶段无 ExtraSearch 时 dict 应为空，实际 %v", dict)
	}
}

func TestPGVectorField_ToDict_ExtraSearch(t *testing.T) {
	p := NewPGVectorFieldHNSW("embedding", 16, 64, 40)
	p.ExtraSearch = map[string]any{
		"ef_search": 100,
	}
	dict := ToDict(p, StageSearch)
	if v, ok := dict["ef_search"]; !ok {
		t.Error("search 阶段应包含 ExtraSearch 展开的 ef_search")
	} else if v != 100 {
		t.Errorf("ef_search = %v, want 100", v)
	}
}
