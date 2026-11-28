package kbadmin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/attachments"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrFileTooLarge 超过单文件上限（spec §5 FILE_TOO_LARGE）
var ErrFileTooLarge = errors.New("file too large")

// ErrUploadChannelBusy 同 KB 同名在途上传/并发帧守卫（spec §5 UPLOAD_CHANNEL_BUSY）
var ErrUploadChannelBusy = errors.New("upload channel busy")

// ErrKBUploadNotFound upload_id 不存在（超时清理后引用，spec §5 KB_UPLOAD_NOT_FOUND）
var ErrKBUploadNotFound = errors.New("kb upload not found")

// ErrHashMismatch 收尾 hash 比对不符（spec §5 UPLOAD_HASH_MISMATCH）
var ErrHashMismatch = errors.New("upload hash mismatch")

// ──────────────────────────── 常量 ────────────────────────────

// MaxFileBytes KB 单文件字节上限（30.5 固定 50MiB；KB config 可调 ⤵️30.2，D-30-5-1）
const MaxFileBytes = attachments.MaxFileBytes

// uploadTTLDefault 槽位默认存活时长（超时未 complete 视为泄漏，自动清理）
const uploadTTLDefault = 30 * time.Minute

// ──────────────────────────── 结构体 ────────────────────────────

// UploadSlot 一次 KB 上传的槽位
type UploadSlot struct {
	UploadID     string    `json:"upload_id"`
	KBID         string    `json:"kb_id"`
	FileName     string    `json:"file_name"`
	TotalSize    int64     `json:"total_size"`
	ExpectedHash string    `json:"expected_hash"`
	ResolvedPath string    `json:"resolved_path"`
	CreatedAt    time.Time `json:"created_at"`
}

// UploadStore KB 上传槽位登记表（内存单点，全局一份；落盘根目录下每 upload 一子目录）
type UploadStore struct {
	mu       sync.Mutex
	slots    map[string]*UploadSlot // upload_id → slot
	inflight map[string]string      // kbID+"/"+fileName → upload_id（串行守卫）
	root     string
	ttl      time.Duration
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUploadStore 构造槽位表；root 为落盘根目录（生产装配传 WorkspaceDir()/kb_uploads）
func NewUploadStore(root string) *UploadStore {
	return &UploadStore{
		slots:    map[string]*UploadSlot{},
		inflight: map[string]string{},
		root:     root,
		ttl:      uploadTTLDefault,
	}
}

// InitUpload 登记槽位：超限拒绝 → 串行守卫 → 建落盘目录 → 认领去重路径
func (s *UploadStore) InitUpload(kbID, fileName string, totalSize int64, expectedHash string) (*UploadSlot, error) {
	if totalSize > MaxFileBytes {
		return nil, ErrFileTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := kbID + "/" + fileName
	if _, exists := s.inflight[key]; exists {
		return nil, ErrUploadChannelBusy
	}
	id, err := newUploadID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	safe := attachments.SafeUploadFilename(fileName, "upload")
	target := attachments.UniqueUploadPath(filepath.Join(dir, safe))
	slot := &UploadSlot{
		UploadID: id, KBID: kbID, FileName: safe,
		TotalSize: totalSize, ExpectedHash: strings.ToLower(expectedHash),
		ResolvedPath: target, CreatedAt: time.Now(),
	}
	s.slots[id] = slot
	s.inflight[key] = id
	return slot, nil
}

// Get 取槽位（不存在返 ErrKBUploadNotFound）
func (s *UploadStore) Get(uploadID string) (*UploadSlot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.slots[uploadID]
	if !ok {
		return nil, ErrKBUploadNotFound
	}
	return slot, nil
}

// Complete 收尾：sha256 复算比对；一致则摘除槽位（文件保留，由 DocService 移交 KB 暂存区）；
// 不符则清理分片目录并返回 ErrHashMismatch。
func (s *UploadStore) Complete(uploadID string) (*UploadSlot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.slots[uploadID]
	if !ok {
		return nil, ErrKBUploadNotFound
	}
	s.release(slot)
	sum, err := fileSha256(slot.ResolvedPath)
	if err != nil {
		return nil, err
	}
	if slot.ExpectedHash != "" && sum != slot.ExpectedHash {
		_ = os.RemoveAll(filepath.Dir(slot.ResolvedPath))
		return nil, ErrHashMismatch
	}
	return slot, nil
}

// Cancel 取消并清理分片目录
func (s *UploadStore) Cancel(uploadID string) error {
	s.mu.Lock()
	slot, ok := s.slots[uploadID]
	if ok {
		s.release(slot)
	}
	s.mu.Unlock()
	if !ok {
		return ErrKBUploadNotFound
	}
	return os.RemoveAll(filepath.Dir(slot.ResolvedPath))
}

// CleanupExpired 清理超时未 complete 的槽位（装配方挂后台 goroutine 周期调用）
func (s *UploadStore) CleanupExpired() {
	s.mu.Lock()
	var expired []*UploadSlot
	now := time.Now()
	for _, slot := range s.slots {
		if now.Sub(slot.CreatedAt) > s.ttl {
			expired = append(expired, slot)
			s.release(slot)
		}
	}
	s.mu.Unlock()
	for _, slot := range expired {
		_ = os.RemoveAll(filepath.Dir(slot.ResolvedPath))
	}
	s.cleanupOrphanDirs(now)
}

// cleanupOrphanDirs janitor（D-30-5-R1 A 拍板）：清扫「无槽位引用且 mtime 超龄」的
// upload 子目录——已完成/断线遗留目录滞留 kb_uploads 的问题；在途槽位目录跳过。
func (s *UploadStore) cleanupOrphanDirs(now time.Time) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return
	}
	s.mu.Lock()
	live := make(map[string]struct{}, len(s.slots))
	for _, slot := range s.slots {
		live[filepath.Dir(slot.ResolvedPath)] = struct{}{}
	}
	s.mu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, entry.Name())
		if _, used := live[dir]; used {
			continue
		}
		fi, err := entry.Info()
		if err != nil || now.Sub(fi.ModTime()) <= s.ttl {
			continue
		}
		_ = os.RemoveAll(dir)
	}
}

// FileSha256 单文件 sha256（十六进制小写；集成测试与收尾比对同口径）
func FileSha256(p string) (string, error) { return fileSha256(p) }

// ──────────────────────────── 非导出函数 ────────────────────────────

// release 摘除槽位与 KB+文件名在途索引（持锁调用）
func (s *UploadStore) release(slot *UploadSlot) {
	delete(s.slots, slot.UploadID)
	delete(s.inflight, slot.KBID+"/"+slot.FileName)
}

// newUploadID crypto/rand 生成 32 位十六进制上传标识
func newUploadID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// fileSha256 流式计算文件 sha256（不整载内存）
func fileSha256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
