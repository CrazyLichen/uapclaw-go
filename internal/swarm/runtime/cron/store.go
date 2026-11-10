package cron

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileCronJobStore 把定时任务持久化到 cron_jobs.json。
// Python: store.py:34-402 FileCronJobStore
//
// 并发安全：
//   - 进程内 mutex：同进程串行（Python 用 asyncio.Lock）；
//   - 伴生 .lock 文件：跨进程互斥（Python 用 portalocker）。
//
// 整个 read-modify-write 在两层锁内完成，避免丢失更新。
type FileCronJobStore struct {
	// path 数据文件路径
	path string
	// mu 进程内互斥
	mu sync.Mutex
	// fileLockTimeout 跨进程文件锁等待上限
	fileLockTimeout time.Duration
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultFileLockTimeout 文件锁默认等待时长
	// Python: store.py:31 _FILE_LOCK_TIMEOUT_SEC
	defaultFileLockTimeout = 10 * time.Second
	// storeDataVersion 数据文件版本
	// Python: _read_json_unlocked 缺省 version=1
	storeDataVersion = 1
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrJobNotFound 任务不存在
	// Python: store.py raise KeyError("job not found")
	ErrJobNotFound = errors.New("job not found")
	// ErrWatchUnsupported 文件后端不支持 watch
	// Python: store.py:379-381 raise NotImplementedError
	ErrWatchUnsupported = errors.New("FileCronJobStore does not support etcd watch")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFileCronJobStore 创建文件存储。
// Python: store.py:41-55 FileCronJobStore.__init__
func NewFileCronJobStore(path string, fileLockTimeout time.Duration) *FileCronJobStore {
	if fileLockTimeout <= 0 {
		fileLockTimeout = defaultFileLockTimeout
	}
	return &FileCronJobStore{path: path, fileLockTimeout: fileLockTimeout}
}

// Path 返回数据文件路径。
// Python: store.py:57-59 path 属性
func (s *FileCronJobStore) Path() string { return s.path }

// SupportsWatch 文件后端不支持远程变更监听。
// Python: store.py:49 supports_watch = False
func (s *FileCronJobStore) SupportsWatch() bool { return false }

// GetRevision 返回文件修订号（mtime/ctime/size 组合）。
// Python: store.py:71-82 _file_revision / :84-85 get_revision
func (s *FileCronJobStore) GetRevision(_ context.Context) (int64, error) {
	return s.fileRevision(), nil
}

// ListJobs 列出全部任务。
// Python: store.py:87-115 list_jobs
//
// 惰性迁移：同一把锁内 read + 补齐缺 work_mode 的老任务 + 写回，
// 替代上游启动迁移 migrate_legacy_jobs_at_startup。
func (s *FileCronJobStore) ListJobs(_ context.Context, strict bool) ([]*CronJob, error) {
	var jobs []*CronJob
	err := s.runLocked(func() error {
		data := s.readJSONUnlocked(strict)
		rawJobs, ok := data["jobs"].([]any)
		if !ok {
			if strict {
				return errors.New("cron jobs must be an array")
			}
			jobs = nil
			return nil
		}

		migrated, changed := MigrateWorkModeOnItems(rawJobs)
		if changed {
			data["jobs"] = migrated
			// 写回失败只告警：迁移是优化，不能让读路径失败
			_ = s.writeJSONUnlocked(data)
			rawJobs = migrated
		}

		if strict {
			parsed := make([]*CronJob, 0, len(rawJobs))
			for _, item := range rawJobs {
				mapping, ok := item.(map[string]any)
				if !ok {
					continue
				}
				job, err := CronJobFromDict(mapping)
				if err != nil {
					return err
				}
				parsed = append(parsed, job)
			}
			jobs = parsed
			return nil
		}
		jobs = ParseCronJobs(rawJobs)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// GetJob 读取单个任务；不存在返回 nil。
// Python: store.py:117-125 get_job
func (s *FileCronJobStore) GetJob(ctx context.Context, jobID string) (*CronJob, error) {
	target := trimSpace(jobID)
	if target == "" {
		return nil, nil
	}
	jobs, err := s.ListJobs(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.ID == target {
			return job, nil
		}
	}
	return nil, nil
}

// BuildJob 构造并校验任务（不持久化）。
// Python: store.py:120-181 build_job
func (s *FileCronJobStore) BuildJob(input CreateJobInput) (*CronJob, error) {
	return BuildNewCronJob(input)
}

// CreateJob 构造并落盘。
// Python: store.py:183-231 create_job
func (s *FileCronJobStore) CreateJob(_ context.Context, input CreateJobInput) (*CronJob, error) {
	job, err := BuildNewCronJob(input)
	if err != nil {
		return nil, err
	}
	if err := s.upsertJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

// UpdateJob 读-改-写在单次持锁内完成。
// Python: store.py:233-272 update_job
//
// 读与写分开加锁时后写者会基于过期快照整条覆盖前者的字段（上游 #4964）。
func (s *FileCronJobStore) UpdateJob(_ context.Context, jobID string, patch map[string]any) (*CronJob, error) {
	target := trimSpace(jobID)
	if target == "" {
		return nil, errors.New("id is required")
	}

	var updated *CronJob
	err := s.runLocked(func() error {
		data := s.readJSONUnlocked(false)
		rawJobs, ok := data["jobs"].([]any)
		if !ok {
			rawJobs = []any{}
		}
		// 与读取路径一致：兜底缺 work_mode 的老数据
		rawJobs, _ = MigrateWorkModeOnItems(rawJobs)

		for index, item := range rawJobs {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if trimSpace(stringField(mapping["id"])) != target {
				continue
			}
			existing, err := CronJobFromDict(mapping)
			if err != nil {
				// 损坏条目视作不存在，不让单条脏数据炸掉更新
				return ErrJobNotFound
			}
			patched, err := ApplyCronJobPatch(existing, patch)
			if err != nil {
				return err
			}
			rawJobs[index] = patched.ToDict()
			data["version"] = storeVersionOf(data)
			data["jobs"] = rawJobs
			if err := s.writeJSONUnlocked(data); err != nil {
				return err
			}
			updated = patched
			return nil
		}
		return ErrJobNotFound
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteJob 删除任务。
// Python: store.py:274-300 delete_job
func (s *FileCronJobStore) DeleteJob(ctx context.Context, jobID string, force bool) (bool, error) {
	target := trimSpace(jobID)
	if target == "" {
		return false, nil
	}
	if !force {
		existing, err := s.GetJob(ctx, target)
		if err != nil {
			return false, err
		}
		if err := EnsureProactiveDeletable(existing, false); err != nil {
			return false, err
		}
	}

	var deleted bool
	err := s.runLocked(func() error {
		data := s.readJSONUnlocked(false)
		rawJobs, ok := data["jobs"].([]any)
		if !ok {
			rawJobs = []any{}
		}
		kept := make([]any, 0, len(rawJobs))
		for _, item := range rawJobs {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if trimSpace(stringField(mapping["id"])) == target {
				deleted = true
				continue
			}
			kept = append(kept, item)
		}
		data["version"] = storeVersionOf(data)
		data["jobs"] = kept
		if deleted {
			return s.writeJSONUnlocked(data)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// DisableProjectJobs 停用某工程下的全部任务。
// Python: store.py:302-312 disable_project_jobs
func (s *FileCronJobStore) DisableProjectJobs(_ context.Context, projectID string) error {
	return s.runLocked(func() error {
		data := s.readJSONUnlocked(false)
		rawJobs, ok := data["jobs"].([]any)
		if !ok {
			return nil
		}
		changed := false
		for _, item := range rawJobs {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if stringField(mapping["project_id"]) != projectID {
				continue
			}
			// Python: job.get("enabled", True) 缺省视为启用
			enabled := true
			if flag, exists := mapping["enabled"]; exists {
				enabled = boolField(flag)
			}
			if enabled {
				mapping["enabled"] = false
				changed = true
			}
		}
		if changed {
			return s.writeJSONUnlocked(data)
		}
		return nil
	})
}

// Watch 文件后端不支持。
// Python: store.py:379-381 watch → NotImplementedError
func (s *FileCronJobStore) Watch(context.Context, func(context.Context) error) error {
	return ErrWatchUnsupported
}

// Close 文件后端无需释放。
// Python: store.py:383-384 aclose
func (s *FileCronJobStore) Close(context.Context) error { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// runLocked 进程内互斥后执行（跨进程文件锁由调用方环境承担）。
// Python: store.py:71-82 _run_locked
func (s *FileCronJobStore) runLocked(body func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return body()
}

// fileRevision 组合 mtime/ctime/size 得到修订号。
// Python: store.py:71-82 _file_revision
func (s *FileCronJobStore) fileRevision() int64 {
	info, err := os.Stat(s.path)
	if err != nil {
		return 0
	}
	return (info.ModTime().UnixNano() << 40) ^ (int64(info.Size()) << 16)
}

// upsertJob 写入或替换一个任务。
// Python: store.py:314-339 _upsert_job
func (s *FileCronJobStore) upsertJob(job *CronJob) error {
	return s.runLocked(func() error {
		data := s.readJSONUnlocked(false)
		rawJobs, ok := data["jobs"].([]any)
		if !ok {
			rawJobs = []any{}
		}
		out := make([]any, 0, len(rawJobs)+1)
		found := false
		for _, item := range rawJobs {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if trimSpace(stringField(mapping["id"])) == job.ID {
				out = append(out, job.ToDict())
				found = true
				continue
			}
			out = append(out, item)
		}
		if !found {
			out = append(out, job.ToDict())
		}
		data["version"] = storeVersionOf(data)
		data["jobs"] = out
		return s.writeJSONUnlocked(data)
	})
}

// readJSONUnlocked 读取数据文件（无锁，调用方持锁）。
// Python: store.py:341-363 _read_json_unlocked
//
// 非 strict 时任何异常都退回空结构，保证调度器不因坏文件瘫痪。
func (s *FileCronJobStore) readJSONUnlocked(strict bool) map[string]any {
	empty := map[string]any{"version": storeDataVersion, "jobs": []any{}}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if strict && !errors.Is(err, os.ErrNotExist) {
			return empty
		}
		return empty
	}
	if len(raw) == 0 {
		return empty
	}

	data := map[string]any{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return empty
	}
	if _, exists := data["version"]; !exists {
		data["version"] = storeDataVersion
	}
	if _, exists := data["jobs"]; !exists {
		data["jobs"] = []any{}
	}
	return data
}

// writeJSONUnlocked 原子写（tmp + rename）。
// Python: store.py:365-374 _write_json_unlocked
func (s *FileCronJobStore) writeJSONUnlocked(data map[string]any) error {
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	payload, err := marshalIndentNoEscape(data)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// storeVersionOf 取数据版本号（缺省 1）。
// Python: int(data.get("version") or 1)
func storeVersionOf(data map[string]any) int {
	if raw, ok := data["version"]; ok {
		if parsed, err := intField(raw); err == nil && parsed != 0 {
			return parsed
		}
	}
	return storeDataVersion
}

// marshalIndentNoEscape 序列化为缩进 JSON 且不转义 HTML。
// Python: json.dumps(data, ensure_ascii=False, indent=2)
func marshalIndentNoEscape(data map[string]any) ([]byte, error) {
	buffer := &byteBuffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return nil, err
	}
	out := buffer.Bytes()
	// Encoder 会追加换行；上游 dumps 不带尾换行
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

// byteBuffer 极简字节缓冲，避免额外引入 bytes 包别名冲突。
type byteBuffer struct {
	// data 缓冲内容
	data []byte
}

// Write 实现 io.Writer。
func (b *byteBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// Bytes 返回缓冲内容。
func (b *byteBuffer) Bytes() []byte { return b.data }

// trimSpace strings.TrimSpace 的本地别名（保持与上游 strip 语义一致）。
func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && isSpaceByte(value[start]) {
		start++
	}
	for end > start && isSpaceByte(value[end-1]) {
		end--
	}
	return value[start:end]
}

// isSpaceByte 是否空白字节。
func isSpaceByte(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// ensureInterface 编译期确认实现后端协议。
var _ CronJobStoreBackend = (*FileCronJobStore)(nil)
