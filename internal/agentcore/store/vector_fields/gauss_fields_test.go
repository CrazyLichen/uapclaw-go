package vector_fields

import "testing"

func TestNewGaussDiskANN(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	if g.DatabaseType != DatabaseTypeGauss {
		t.Errorf("DatabaseType = %v, want %v", g.DatabaseType, DatabaseTypeGauss)
	}
	if g.IndexType != IndexTypeDiskANN {
		t.Errorf("IndexType = %v, want %v", g.IndexType, IndexTypeDiskANN)
	}
	if g.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", g.VectorFieldName)
	}
	if !g.EnablePQ {
		t.Error("EnablePQ 应为 true")
	}
	if g.PGNseg != 128 {
		t.Errorf("PGNseg = %v, want 128", g.PGNseg)
	}
	if g.PGNclus != 16 {
		t.Errorf("PGNclus = %v, want 16", g.PGNclus)
	}
	if g.NumParallels != 32 {
		t.Errorf("NumParallels = %v, want 32", g.NumParallels)
	}
	if g.QuantizationType != "lvq" {
		t.Errorf("QuantizationType = %v, want lvq", g.QuantizationType)
	}
	if g.SubgraphCount != 1 {
		t.Errorf("SubgraphCount = %v, want 1", g.SubgraphCount)
	}
}

func TestGaussDiskANN_Validate_正常(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	if err := g.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestGaussDiskANN_Validate_PGNseg无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.PGNseg = 0
	if err := g.Validate(); err == nil {
		t.Error("PGNseg=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_PGNclus无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.PGNclus = 0
	if err := g.Validate(); err == nil {
		t.Error("PGNclus=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_NumParallels无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.NumParallels = 0
	if err := g.Validate(); err == nil {
		t.Error("NumParallels=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_QuantizationType无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.QuantizationType = "invalid"
	if err := g.Validate(); err == nil {
		t.Error("QuantizationType='invalid' 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_SubgraphCount无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.SubgraphCount = 0
	if err := g.Validate(); err == nil {
		t.Error("SubgraphCount=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_ToDict_construct(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	dict := ToDict(g, StageConstruct)
	if _, ok := dict["EnablePQ"]; !ok {
		t.Error("construct 阶段应包含 EnablePQ")
	}
	if _, ok := dict["PGNseg"]; !ok {
		t.Error("construct 阶段应包含 PGNseg")
	}
	if _, ok := dict["QuantizationType"]; !ok {
		t.Error("construct 阶段应包含 QuantizationType")
	}
}
