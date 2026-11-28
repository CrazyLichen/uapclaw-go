package kbadmin

import (
	"errors"

	"gorm.io/gorm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrKBNotFound 知识库不存在
var ErrKBNotFound = errors.New("kb not found")

// ──────────────────────────── 结构体 ────────────────────────────

// KBDAO kb_info 表数据访问
type KBDAO struct {
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKBDAO 构造 KBDAO
func NewKBDAO(db *gorm.DB) *KBDAO { return &KBDAO{db: db} }

// Create 新建知识库记录
func (d *KBDAO) Create(kb *KBInfo) error {
	return d.db.Create(kb).Error
}

// Get 按 ID 查询；不存在返回 ErrKBNotFound
func (d *KBDAO) Get(id string) (*KBInfo, error) {
	var kb KBInfo
	if err := d.db.First(&kb, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKBNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// List 全量列表（置顶优先、更新时间倒序）
func (d *KBDAO) List() ([]KBInfo, error) {
	var list []KBInfo
	err := d.db.Order("pinned DESC, updated_at DESC").Find(&list).Error
	return list, err
}

// Update 更新知识库记录
func (d *KBDAO) Update(kb *KBInfo) error {
	return d.db.Save(kb).Error
}

// Delete 按 ID 删除
func (d *KBDAO) Delete(id string) error {
	return d.db.Delete(&KBInfo{}, "id = ?", id).Error
}
