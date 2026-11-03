package lite

// ──────────────────────────── 常量 ────────────────────────────

const (
	// segmenterVersion 分词器标识，写入 meta。词典/库升级时改这里 → 触发全量重建。
	segmenterVersion = "gse-zh_s@v1.3.1"
	// maxFTSTokens 关键词检索最多使用的 token 数（AND 连接，过多会让召回过窄）。
	maxFTSTokens = 5
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemorySegmenterAsync 后台预热分词器（Task 4 填入实现）。
func InitMemorySegmenterAsync() {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// segmenterID 返回分词器标识；不可用时返回 "none"（写入 meta，避免反复重建）。
func segmenterID() string { return "none" }
