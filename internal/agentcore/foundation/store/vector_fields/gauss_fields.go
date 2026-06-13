package vector_fields

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDiskANN GaussDB DiskANN 向量索引配置。
// DiskANN 是 GaussDB 的磁盘近似最近邻索引，支持大规模向量检索。
//
// 对应 Python: gauss_vector_store.py 中的 GSDISKANN 索引参数
type GaussDiskANN struct {
	VectorField
	// EnablePQ 是否启用产品量化
	EnablePQ bool `vf:"construct,keepzero"`
	// PGNseg 产品量化段数
	PGNseg int `vf:"construct,keepzero"`
	// PGNclus 产品量化聚类数
	PGNclus int `vf:"construct,keepzero"`
	// NumParallels 并行度
	NumParallels int `vf:"construct,keepzero"`
	// QuantizationType 量化类型（lvq/pq）
	QuantizationType string `vf:"construct"`
	// SubgraphCount 子图数量
	SubgraphCount int `vf:"construct,keepzero"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussDiskANN 创建 GaussDB DiskANN 索引配置，使用默认参数。
// fieldName 为向量字段名。
func NewGaussDiskANN(fieldName string) *GaussDiskANN {
	return &GaussDiskANN{
		VectorField: VectorField{
			DatabaseType:    DatabaseTypeGauss,
			IndexType:       IndexTypeDiskANN,
			VectorFieldName: fieldName,
		},
		EnablePQ:         true,
		PGNseg:           128,
		PGNclus:          16,
		NumParallels:     32,
		QuantizationType: "lvq",
		SubgraphCount:    1,
	}
}

// Validate 校验 GaussDiskANN 参数。
func (g *GaussDiskANN) Validate() error {
	if g.PGNseg <= 0 {
		return fmt.Errorf("PGNseg 必须大于 0，当前值: %d", g.PGNseg)
	}
	if g.PGNclus <= 0 {
		return fmt.Errorf("PGNclus 必须大于 0，当前值: %d", g.PGNclus)
	}
	if g.NumParallels <= 0 {
		return fmt.Errorf("NumParallels 必须大于 0，当前值: %d", g.NumParallels)
	}
	if g.QuantizationType != "lvq" && g.QuantizationType != "pq" {
		return fmt.Errorf("QuantizationType 必须为 lvq 或 pq，当前值: %s", g.QuantizationType)
	}
	if g.SubgraphCount <= 0 {
		return fmt.Errorf("SubgraphCount 必须大于 0，当前值: %d", g.SubgraphCount)
	}
	return nil
}
