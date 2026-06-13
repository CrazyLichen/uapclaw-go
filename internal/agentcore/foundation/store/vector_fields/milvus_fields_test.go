package vector_fields

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMilvusAUTO(t *testing.T) {
	f := NewMilvusAUTO("embedding")
	if f.DatabaseType != DatabaseTypeMilvus {
		t.Errorf("DatabaseType = %v, want %v", f.DatabaseType, DatabaseTypeMilvus)
	}
	if f.IndexType != IndexTypeAUTO {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeAUTO)
	}
	if f.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", f.VectorFieldName)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	// AUTO 无额外字段，construct/search 输出均为空
	constructDict := ToDict(f, StageConstruct)
	if len(constructDict) != 0 {
		t.Errorf("ToDict(construct) = %v, want empty", constructDict)
	}
	searchDict := ToDict(f, StageSearch)
	if len(searchDict) != 0 {
		t.Errorf("ToDict(search) = %v, want empty", searchDict)
	}
}

func TestMilvusFLAT(t *testing.T) {
	f := NewMilvusFLAT("embedding")
	if f.IndexType != IndexTypeFLAT {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeFLAT)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestMilvusHNSW(t *testing.T) {
	f := NewMilvusHNSW("embedding", 30, 360, 2.0)
	if f.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeHNSW)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 M 和 EfConstruction
	constructDict := ToDict(f, StageConstruct)
	if constructDict["M"] != 30 {
		t.Errorf("construct M = %v, want 30", constructDict["M"])
	}
	if constructDict["EfConstruction"] != 360 {
		t.Errorf("construct EfConstruction = %v, want 360", constructDict["EfConstruction"])
	}

	// search 阶段应输出 EfSearchFactor
	searchDict := ToDict(f, StageSearch)
	if searchDict["EfSearchFactor"] != 2.0 {
		t.Errorf("search EfSearchFactor = %v, want 2.0", searchDict["EfSearchFactor"])
	}
}

func TestMilvusHNSW_校验失败(t *testing.T) {
	tests := []struct {
		name    string
		m       int
		efc     int
		efsf    float64
		wantErr bool
	}{
		{"M太小", 1, 360, 2.0, true},
		{"M太大", 2049, 360, 2.0, true},
		{"EfConstruction为零", 30, 0, 2.0, true},
		{"EfSearchFactor为零", 30, 360, 0.0, true},
		{"合法值", 30, 360, 2.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusHNSW("embedding", tt.m, tt.efc, tt.efsf)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMilvusIVF(t *testing.T) {
	f := NewMilvusIVF("embedding", 128, 8)
	if f.IndexType != IndexTypeIVF {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeIVF)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 Nlist
	constructDict := ToDict(f, StageConstruct)
	if constructDict["Nlist"] != 128 {
		t.Errorf("construct Nlist = %v, want 128", constructDict["Nlist"])
	}

	// search 阶段应输出 Nprobe
	searchDict := ToDict(f, StageSearch)
	if searchDict["Nprobe"] != 8 {
		t.Errorf("search Nprobe = %v, want 8", searchDict["Nprobe"])
	}
}

func TestMilvusIVF_校验失败(t *testing.T) {
	tests := []struct {
		name    string
		nlist   int
		nprobe  int
		wantErr bool
	}{
		{"nlist为零", 0, 8, true},
		{"nprobe为零", 128, 0, true},
		{"nprobe大于nlist", 8, 128, true},
		{"合法值", 128, 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusIVF("embedding", tt.nlist, tt.nprobe)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMilvusSCANN(t *testing.T) {
	f := NewMilvusSCANN("embedding", 128, 8, true, 200)
	if f.IndexType != IndexTypeSCANN {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeSCANN)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 Nlist 和 WithRawData
	constructDict := ToDict(f, StageConstruct)
	if constructDict["Nlist"] != 128 {
		t.Errorf("construct Nlist = %v, want 128", constructDict["Nlist"])
	}
	if constructDict["WithRawData"] != true {
		t.Errorf("construct WithRawData = %v, want true", constructDict["WithRawData"])
	}

	// search 阶段应输出 Nprobe 和 ReorderK
	searchDict := ToDict(f, StageSearch)
	if searchDict["Nprobe"] != 8 {
		t.Errorf("search Nprobe = %v, want 8", searchDict["Nprobe"])
	}
	if searchDict["ReorderK"] != 200 {
		t.Errorf("search ReorderK = %v, want 200", searchDict["ReorderK"])
	}
}

func TestMilvusSCANN_校验失败(t *testing.T) {
	tests := []struct {
		name     string
		nlist    int
		nprobe   int
		reorderK int
		wantErr  bool
	}{
		{"ReorderK为负数", 128, 8, -1, true},
		{"nprobe大于nlist", 8, 128, 200, true},
		{"合法值", 128, 8, 200, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusSCANN("embedding", tt.nlist, tt.nprobe, true, tt.reorderK)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
