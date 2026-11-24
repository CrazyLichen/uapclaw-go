// models.go 对齐 Python retrieval/build/models.py（31 行）：索引文件名常量与目录记录。
package build

// 索引产物文件名常量（对齐 Python models.py:6-9）
const (
	// IndexManifestFilename manifest 文件名
	IndexManifestFilename = "manifest.json"
	// TreeIndexFilename 树索引文件名
	TreeIndexFilename = "tree_index.yaml"
	// TreeHTMLFilename 树 HTML 文件名
	TreeHTMLFilename = "tree_index.html"
	// CatalogFilename 目录文件名
	CatalogFilename = "catalog.jsonl"
)

// CatalogRecord 目录记录（frozen dataclass → Go 只读结构体）
type CatalogRecord struct {
	// WorkerID 技能唯一标识
	WorkerID string
	// CID 节点 cid
	CID string
	// Name 名称
	Name string
	// Description 描述
	Description string
	// SkillPath 技能文件路径
	SkillPath string
	// BranchPath 分支路径段
	BranchPath []string
	// Category 分类
	Category string
	// RetrievalText 检索文本
	RetrievalText string
	// Metadata 附加元数据
	Metadata map[string]any
}
