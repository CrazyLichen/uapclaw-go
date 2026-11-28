package kbadmin

import (
	"errors"

	"gorm.io/gorm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrTaskNotFound 任务不存在
var ErrTaskNotFound = errors.New("task not found")

// ──────────────────────────── 结构体 ────────────────────────────

// TaskDAO kb_task 表数据访问
type TaskDAO struct {
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskDAO 构造 TaskDAO
func NewTaskDAO(db *gorm.DB) *TaskDAO { return &TaskDAO{db: db} }

// Create 新建任务
func (d *TaskDAO) Create(task *KBTask) error { return d.db.Create(task).Error }

// Get 按 ID 查询任务；不存在返回 ErrTaskNotFound
func (d *TaskDAO) Get(id string) (*KBTask, error) {
	var task KBTask
	if err := d.db.First(&task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return &task, nil
}

// ListByStatus 按状态列任务（孤儿任务恢复用 ListByStatus("processing")）
func (d *TaskDAO) ListByStatus(status string) ([]KBTask, error) {
	var list []KBTask
	err := d.db.Where("status = ?", status).Find(&list).Error
	return list, err
}

// Update 更新任务
func (d *TaskDAO) Update(task *KBTask) error { return d.db.Save(task).Error }
