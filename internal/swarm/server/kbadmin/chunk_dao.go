package kbadmin

import (
	"errors"

	"gorm.io/gorm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrChunkNotFound 分块不存在
var ErrChunkNotFound = errors.New("chunk not found")

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkDAO kb_chunk_meta 与 kb_chunk_revision 表数据访问
type ChunkDAO struct {
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChunkDAO 构造 ChunkDAO
func NewChunkDAO(db *gorm.DB) *ChunkDAO { return &ChunkDAO{db: db} }

// CreateMeta 新建分块元数据
func (d *ChunkDAO) CreateMeta(m *KBChunkMeta) error { return d.db.Create(m).Error }

// GetMeta 按 ID 查询分块元数据；不存在返回 ErrChunkNotFound
func (d *ChunkDAO) GetMeta(id string) (*KBChunkMeta, error) {
	var m KBChunkMeta
	if err := d.db.First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &m, nil
}

// ListByDoc 按文档列出分块
func (d *ChunkDAO) ListByDoc(docID string) ([]KBChunkMeta, error) {
	var list []KBChunkMeta
	err := d.db.Where("doc_id = ?", docID).Find(&list).Error
	return list, err
}

// UpdateMeta 更新分块元数据
func (d *ChunkDAO) UpdateMeta(m *KBChunkMeta) error { return d.db.Save(m).Error }

// DeleteMeta 按 ID 删除分块元数据
func (d *ChunkDAO) DeleteMeta(id string) error {
	return d.db.Delete(&KBChunkMeta{}, "id = ?", id).Error
}

// DeleteByDoc 按 docID 删除全部 chunk 台账行（重解析前清旧分块：
// 向量库侧由 embedStep 先删后建对齐，两侧同步防陈旧 chunk 累积）
func (d *ChunkDAO) DeleteByDoc(docID string) error {
	return d.db.Delete(&KBChunkMeta{}, "doc_id = ?", docID).Error
}

// CreateRevision 追加版本记录
func (d *ChunkDAO) CreateRevision(r *KBChunkRevision) error { return d.db.Create(r).Error }

// ListRevisions 按分块列出版本（版本号倒序）
func (d *ChunkDAO) ListRevisions(chunkID string) ([]KBChunkRevision, error) {
	var list []KBChunkRevision
	err := d.db.Where("chunk_id = ?", chunkID).Order("revision DESC").Find(&list).Error
	return list, err
}
