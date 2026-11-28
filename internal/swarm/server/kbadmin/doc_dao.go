package kbadmin

import (
	"errors"

	"gorm.io/gorm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrDocNotFound 文档不存在
var ErrDocNotFound = errors.New("doc not found")

// ──────────────────────────── 结构体 ────────────────────────────

// DocDAO kb_doc 表数据访问
type DocDAO struct {
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDocDAO 构造 DocDAO
func NewDocDAO(db *gorm.DB) *DocDAO { return &DocDAO{db: db} }

// Create 新建文档记录
func (d *DocDAO) Create(doc *KBDoc) error { return d.db.Create(doc).Error }

// Get 按 ID 查询；不存在返回 ErrDocNotFound
func (d *DocDAO) Get(id string) (*KBDoc, error) {
	var doc KBDoc
	if err := d.db.First(&doc, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDocNotFound
		}
		return nil, err
	}
	return &doc, nil
}

// ListByKB 按 KB 全量列表（创建时间倒序）
func (d *DocDAO) ListByKB(kbID string) ([]KBDoc, error) {
	var list []KBDoc
	err := d.db.Where("kb_id = ?", kbID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// ListByKBAndStatus 按 KB 与解析状态过滤（创建时间倒序）
func (d *DocDAO) ListByKBAndStatus(kbID, status string) ([]KBDoc, error) {
	var list []KBDoc
	err := d.db.Where("kb_id = ? AND parse_status = ?", kbID, status).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

// ListByStatus 全库按解析状态过滤（停滞扫描与孤儿恢复用）
func (d *DocDAO) ListByStatus(status string) ([]KBDoc, error) {
	var list []KBDoc
	err := d.db.Where("parse_status = ?", status).Find(&list).Error
	return list, err
}

// Update 更新文档记录
func (d *DocDAO) Update(doc *KBDoc) error { return d.db.Save(doc).Error }

// Delete 按 ID 删除
func (d *DocDAO) Delete(id string) error { return d.db.Delete(&KBDoc{}, "id = ?", id).Error }
