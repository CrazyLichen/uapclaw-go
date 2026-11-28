package kbadmin

import "errors"

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrNotImplemented 方法待后续批次实现（分发层映射 KB_NOT_IMPLEMENTED）
var ErrNotImplemented = errors.New("kb method not implemented yet")

// ──────────────────────────── 结构体 ────────────────────────────

// DocService 文档管理编排骨架（30.5 已接上传分帧编排；解析任务/重解析/取消/预览 ⤵️30.2）
type DocService struct {
	store   *Store
	uploads *UploadStore
}

// ChunkService 分块管理编排骨架（30.6 回填：乐观锁编辑/启停/版本回滚/生成问题）
type ChunkService struct {
	store *Store
}

// SearchService 检索测试编排骨架（30.6 回填：调 retrieval Retrieve + 召回来源徽标）
type SearchService struct {
	store *Store
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDocService 构造 DocService
func NewDocService(store *Store) *DocService { return &DocService{store: store} }

// NewChunkService 构造 ChunkService
func NewChunkService(store *Store) *ChunkService { return &ChunkService{store: store} }

// NewSearchService 构造 SearchService
func NewSearchService(store *Store) *SearchService { return &SearchService{store: store} }

// Update 分块编辑（30.6 回填）
func (s *ChunkService) Update() error { return ErrNotImplemented }

// Test 检索测试（30.6 回填）
func (s *SearchService) Test() error { return ErrNotImplemented }
