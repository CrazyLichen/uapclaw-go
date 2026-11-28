package kbadmin

import (
	"errors"
	"os"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UploadedFile kb.file.upload_complete 收尾结果（X 边界拍板：不含解析任务字段，
// 触发解析挂点 ⤵️30.2 由 30.2 批在此返回结构上扩展）。
type UploadedFile struct {
	UploadID  string `json:"upload_id"`
	KBID      string `json:"kb_id"`
	FileName  string `json:"file_name"`
	Size      int64  `json:"size"`
	FinalPath string `json:"final_path"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetUploadStore 注入槽位表（Domain 组装期调用；UploadStore 生命周期独立于 SQLite）
func (s *DocService) SetUploadStore(ups *UploadStore) { s.uploads = ups }

// InitUpload kb.doc.upload 编排：校验 KB 存在 → 登记槽位并认领去重落盘路径。
// Python 对应物：无（30.5 新增；数据面分帧复用 27.8 file.upload_chunk 协议语义）。
func (s *DocService) InitUpload(kbID, fileName string, totalSize int64, expectedHash string) (*UploadSlot, error) {
	if _, err := s.store.KB().Get(kbID); err != nil {
		return nil, ErrKBNotFound
	}
	return s.uploads.InitUpload(kbID, fileName, totalSize, expectedHash)
}

// CompleteUpload kb.file.upload_complete 编排：sha256 复算一致后返回文件标识。
// **不触发解析**（30.5 X 边界拍板，⤵️30.2 在此加挂点：接管文件 + 建解析任务）。
// 文件保留在 kb_uploads/{upload_id}/{file_name} 原位，FinalPath 即该路径。
func (s *DocService) CompleteUpload(uploadID string) (*UploadedFile, error) {
	slot, err := s.uploads.Complete(uploadID)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(slot.ResolvedPath)
	if err != nil {
		return nil, err
	}
	return &UploadedFile{
		UploadID: slot.UploadID, KBID: slot.KBID, FileName: slot.FileName,
		Size: fi.Size(), FinalPath: slot.ResolvedPath,
	}, nil
}

// CancelUpload kb.doc.cancel_upload 编排：清理临时分片
func (s *DocService) CancelUpload(uploadID string) error { return s.uploads.Cancel(uploadID) }

// DownloadChunk kb.doc.download 数据面编排：KB 暂存文件有界分块读（反向流式，
// 前端聚合为 blob URL——聚合 UI ⤵️30.8）。
func (s *DocService) DownloadChunk(path string, offset, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		limit = 512 * 1024
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, ErrKBUploadNotFound
		}
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if offset < 0 || int64(offset) > fi.Size() {
		return nil, false, ErrKBUploadNotFound
	}
	buf := make([]byte, limit)
	n, err := f.ReadAt(buf, int64(offset))
	if err != nil && n == 0 {
		return nil, false, ErrKBUploadNotFound
	}
	return buf[:n], int64(offset+n) >= fi.Size(), nil
}
