package kbadmin

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrDocDuplicate 同 KB 内 content_hash 冲突（spec §4 DOC_DUPLICATE）
var ErrDocDuplicate = errors.New("doc duplicate in kb")

// ErrDocParseActive 文档解析在途（pending/processing 不可 reparse）
var ErrDocParseActive = errors.New("doc parse in progress")

// ──────────────────────────── 结构体 ────────────────────────────

// DocService 文档管理编排（30.2 真实现：解析任务/重解析/取消/列表；
// 上传分帧编排见 doc_upload.go，数据面见 upload_store.go）
type DocService struct {
	// store kbadmin SQLite 存储
	store *Store
	// uploads 30.5 上传槽位表
	uploads *UploadStore
	// enqueue 解析任务提交口（worker 装配期接线；nil 时任务仅落库不入队——测试用）
	enqueue func(taskID string)
	// cancelFn 取消在途任务的回调（worker 装配期接线；nil 走 DAO 直接标 cancelled）
	cancelFn func(taskID string) bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDocService 构造 DocService
func NewDocService(store *Store) *DocService { return &DocService{store: store} }

// SetEnqueue 注入任务唤醒回调（worker 装配期调用）
func (s *DocService) SetEnqueue(fn func(taskID string)) { s.enqueue = fn }

// SetCancelFn 注入在途取消回调（worker 装配期调用）
func (s *DocService) SetCancelFn(fn func(taskID string) bool) { s.cancelFn = fn }

// SubmitParseTask 建解析任务：KB 存在校验 → hash 去重 → 建 kb_doc(pending) +
// kb_task(pending) → 唤醒 worker（spec §3 数据流入口）
func (s *DocService) SubmitParseTask(kbID, fileName, finalPath string, size int64, contentHash string) (*KBTask, *KBDoc, error) {
	if _, err := s.store.KB().Get(kbID); err != nil {
		return nil, nil, ErrKBNotFound
	}
	// 去重判据：同 KB 内已存在同 hash 且非 failed/cancelled 的文档（可重传修复）
	existing, err := s.store.Doc().ListByKB(kbID)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range existing {
		if d.ContentHash == contentHash && d.ParseStatus != "failed" && d.ParseStatus != "cancelled" {
			return nil, nil, ErrDocDuplicate
		}
	}
	// contentHash 为空时现场计算（upload 链已在 Complete 复算过，此为兜底）
	if contentHash == "" {
		var herr error
		if contentHash, herr = fileSha256(finalPath); herr != nil {
			return nil, nil, herr
		}
	}
	now := time.Now().Unix()
	doc := &KBDoc{ID: uuid.NewString(), KBID: kbID, FileName: fileName,
		ParseStatus: "pending", ContentHash: contentHash,
		MetaJSON:      mustJSONString(map[string]any{"size": size, "final_path": finalPath}),
		LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.store.Doc().Create(doc); err != nil {
		return nil, nil, err
	}
	task := &KBTask{ID: uuid.NewString(), DocID: doc.ID, KBID: kbID,
		Status: "pending", CreatedAt: now}
	if err := s.store.Task().Create(task); err != nil {
		_ = s.store.Doc().Delete(doc.ID) // 回滚
		return nil, nil, err
	}
	if s.enqueue != nil {
		s.enqueue(task.ID)
	}
	return task, doc, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mustJSONString map→JSON 字符串（失败返回 "{}"，仅元数据展示字段）
func mustJSONString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
