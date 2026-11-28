package kbadmin

import (
	"errors"

	"gorm.io/gorm"
)

// kb_task 通信面（30.2 spec §5：progress 拉全量兜底 + subscribe 推送注册）。
// 推送扇出经 Domain.fanoutTaskEvent（观察者出口），WS 帧写出由 server 侧
// SetPushFunc 注入。

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskProgress 拉任务全量（断线重连兜底）：按 task_ids 精确取，缺省时按 kb_id
// 过滤，两者皆空返回全量任务
func (d *Domain) TaskProgress(taskIDs []string, kbID string) ([]KBTask, error) {
	var rows []KBTask
	q := d.Worker.store.DB().Model(&KBTask{})
	if len(taskIDs) > 0 {
		q = q.Where("id IN ?", taskIDs)
	} else if kbID != "" {
		q = q.Where("kb_id = ?", kbID)
	}
	if err := q.Order("created_at DESC").Find(&rows).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []KBTask{}, nil
		}
		return nil, err
	}
	return rows, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// subscribeChannel 登记 kb.task.subscribe 订阅（幂等）
func (d *Domain) subscribeChannel(channelID string) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	if d.subscribers == nil {
		d.subscribers = make(map[string]struct{})
	}
	d.subscribers[channelID] = struct{}{}
}
