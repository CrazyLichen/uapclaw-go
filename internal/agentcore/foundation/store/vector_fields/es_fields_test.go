package vector_fields

import "testing"

func TestNewESVectorField(t *testing.T) {
	e := NewESVectorField("embedding")
	if e.DatabaseType != DatabaseTypeES {
		t.Errorf("DatabaseType = %v, want %v", e.DatabaseType, DatabaseTypeES)
	}
	if e.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", e.IndexType, IndexTypeHNSW)
	}
	if e.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", e.VectorFieldName)
	}
	if e.NumCandidates != 100 {
		t.Errorf("NumCandidates = %v, want 100", e.NumCandidates)
	}
}

func TestESVectorField_Validate_正常(t *testing.T) {
	e := NewESVectorField("embedding")
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestESVectorField_Validate_NumCandidates无效(t *testing.T) {
	e := NewESVectorField("embedding")
	e.NumCandidates = -1
	if err := e.Validate(); err == nil {
		t.Error("NumCandidates=-1 时 Validate 应返回错误")
	}
}

func TestESVectorField_ToDict_construct(t *testing.T) {
	e := NewESVectorField("embedding")
	dict := ToDict(e, StageConstruct)
	// construct 阶段无特有字段（ExtraConstruct 为 nil），结果应为空
	if len(dict) != 0 {
		t.Errorf("construct 阶段 dict 应为空，实际 %v", dict)
	}
}

func TestESVectorField_ToDict_search(t *testing.T) {
	e := NewESVectorField("embedding")
	dict := ToDict(e, StageSearch)
	if v, ok := dict["NumCandidates"]; !ok {
		t.Error("search 阶段应包含 NumCandidates")
	} else if v != 100 {
		t.Errorf("NumCandidates = %v, want 100", v)
	}
}

func TestESVectorField_ToDict_ExtraFields(t *testing.T) {
	e := NewESVectorField("embedding")
	e.ExtraConstruct = map[string]any{"custom_param": 42}
	e.ExtraSearch = map[string]any{"search_param": "value"}

	constructDict := ToDict(e, StageConstruct)
	if v, ok := constructDict["custom_param"]; !ok {
		t.Error("construct 阶段应包含 ExtraConstruct 展开的 custom_param")
	} else if v != 42 {
		t.Errorf("custom_param = %v, want 42", v)
	}

	searchDict := ToDict(e, StageSearch)
	if v, ok := searchDict["search_param"]; !ok {
		t.Error("search 阶段应包含 ExtraSearch 展开的 search_param")
	} else if v != "value" {
		t.Errorf("search_param = %v, want value", v)
	}
}
