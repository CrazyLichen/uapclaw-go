package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronJobStore cron 任务 JSON 持久化。
// 对齐 Python: CronJobStore (store.py L15-238)。
// 路径默认 path.CronJobsPath()（gateway 与 agentserver 共享同一文件）。
type CronJobStore struct {
	// storePath JSON 文件路径
	storePath string
	// mu 互斥锁（对齐 Python asyncio.Lock）
	mu sync.Mutex
}

// cronStoreFile 文件结构（对齐 Python {"version": 1, "jobs": [...]}）
type cronStoreFile struct {
	// Version 存储格式版本
	Version int `json:"version"`
	// Jobs 任务条目列表（保持原始 dict 形态，损坏条目由 FromCronJobDict 跳过）
	Jobs []map[string]any `json:"jobs"`
}

// CreateJobParams CreateJob 参数。
// 对齐 Python create_job 关键字参数（指针字段区分"未提供"与"零值"）。
type CreateJobParams struct {
	// JobID 任务 ID（空则自动生成 uuid hex）
	JobID string
	// Name 任务名
	Name string
	// CronExpr cron 表达式
	CronExpr string
	// Timezone 时区
	Timezone string
	// Description 任务内容
	Description string
	// Targets 推送频道
	Targets string
	// Enabled 是否启用
	Enabled bool
	// WakeOffsetSeconds 提前唤醒秒数（nil → 默认 300）
	WakeOffsetSeconds *int
	// SessionID 会话 ID
	SessionID string
	// ChatType 群聊/私聊标记
	ChatType string
	// Mode 执行模式
	Mode string
	// DeleteAfterRun 执行后删除（nil → false）
	DeleteAfterRun *bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronJobStore 创建 store。p 为空时用默认 CronJobsPath()。
// 对齐 Python: CronJobStore.__init__(path=None)。
func NewCronJobStore(p string) *CronJobStore {
	if strings.TrimSpace(p) == "" {
		p = path.CronJobsPath()
	}
	return &CronJobStore{storePath: p}
}

// Path 返回存储路径。
// 对齐 Python: path 属性。
func (s *CronJobStore) Path() string {
	return s.storePath
}

// ListJobs 列出全部任务（updated_at+created_at 降序）。
// 对齐 Python: list_jobs (store.py L26-41)。损坏条目跳过保持系统健壮。
func (s *CronJobStore) ListJobs() ([]*CronJob, error) {
	data := s.readJSON()
	var jobs []*CronJob
	for _, item := range data.Jobs {
		job, err := FromCronJobDict(item)
		if err != nil {
			continue // 对齐 Python: ignore invalid entries
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].UpdatedAt != jobs[j].UpdatedAt {
			return jobs[i].UpdatedAt > jobs[j].UpdatedAt
		}
		return jobs[i].CreatedAt > jobs[j].CreatedAt
	})
	return jobs, nil
}

// GetJob 按 ID 获取任务（不存在返回 nil, nil）。
// 对齐 Python: get_job (store.py L43-50)。
func (s *CronJobStore) GetJob(jobID string) (*CronJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, nil
	}
	jobs, err := s.ListJobs()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.ID == jobID {
			return j, nil
		}
	}
	return nil, nil
}

// CreateJob 创建任务（upsert 语义：job_id 已存在直接覆盖）。
// 对齐 Python: create_job (store.py L52-94)。无唯一性校验——Agent 双写 + Gateway 转发双写天然幂等。
func (s *CronJobStore) CreateJob(params CreateJobParams) (*CronJob, error) {
	now := nowTimestamp()
	wos := 300
	if params.WakeOffsetSeconds != nil {
		wos = *params.WakeOffsetSeconds
	}
	dar := false
	if params.DeleteAfterRun != nil {
		dar = *params.DeleteAfterRun
	}
	mode := modeAny(params.Mode)
	job := &CronJob{
		ID:                strings.TrimSpace(params.JobID),
		Name:              strings.TrimSpace(params.Name),
		Enabled:           params.Enabled,
		CronExpr:          strings.TrimSpace(params.CronExpr),
		Timezone:          strings.TrimSpace(params.Timezone),
		WakeOffsetSeconds: wos,
		Description:       params.Description,
		Targets:           strings.TrimSpace(params.Targets),
		SessionID:         strings.TrimSpace(params.SessionID),
		CreatedAt:         now,
		UpdatedAt:         now,
		ChatType:          strings.TrimSpace(params.ChatType),
		Mode:              mode,
		DeleteAfterRun:    dar,
	}
	if job.ID == "" {
		job.ID = newJobID()
	}
	// round-trip 校验（对齐 Python: CronJob.from_dict(job.to_dict())）
	if _, err := FromCronJobDict(job.ToDict()); err != nil {
		return nil, err
	}
	if err := s.upsertJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

// UpdateJob 更新任务（patch 合并）。任务不存在返回错误。
// 对齐 Python: update_job (store.py L94-150)。
func (s *CronJobStore) UpdateJob(jobID string, patch map[string]any) (*CronJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("id is required")
	}
	existing, err := s.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("job not found")
	}

	if _, ok := patch["name"]; ok {
		existing.Name = strings.TrimSpace(strAny(patch["name"]))
	}
	if _, ok := patch["enabled"]; ok {
		existing.Enabled = boolAny(patch["enabled"])
		// 重新启用隐含不再过期（除非显式设置 expired）
		if existing.Enabled {
			if _, hasExpired := patch["expired"]; !hasExpired {
				existing.Expired = false
			}
		}
	}
	if _, ok := patch["cron_expr"]; ok {
		existing.CronExpr = strings.TrimSpace(strAny(patch["cron_expr"]))
		// 修改调度隐含不再过期（除非显式设置 expired）
		if _, hasExpired := patch["expired"]; !hasExpired {
			existing.Expired = false
		}
	}
	if _, ok := patch["timezone"]; ok {
		existing.Timezone = strings.TrimSpace(strAny(patch["timezone"]))
	}
	if raw, ok := patch["wake_offset_seconds"]; ok {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		if n < 0 {
			n = 0
		}
		existing.WakeOffsetSeconds = n
	}
	if _, ok := patch["description"]; ok {
		existing.Description = strAny(patch["description"])
	}
	if _, ok := patch["targets"]; ok {
		existing.Targets = strings.TrimSpace(strAny(patch["targets"]))
	}
	if _, ok := patch["session_id"]; ok {
		existing.SessionID = strings.TrimSpace(strAny(patch["session_id"]))
	}
	if _, ok := patch["chat_type"]; ok {
		existing.ChatType = strings.TrimSpace(strAny(patch["chat_type"]))
	}
	if _, ok := patch["expired"]; ok {
		existing.Expired = boolAny(patch["expired"])
	}
	if _, ok := patch["mode"]; ok {
		existing.Mode = modeAny(patch["mode"])
	}
	if _, ok := patch["delete_after_run"]; ok {
		existing.DeleteAfterRun = boolAny(patch["delete_after_run"])
	}

	existing.UpdatedAt = nowTimestamp()
	// round-trip 校验
	if _, err := FromCronJobDict(existing.ToDict()); err != nil {
		return nil, err
	}
	if err := s.upsertJob(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteJob 删除任务。返回是否实际删除。
// 对齐 Python: delete_job (store.py L152-174)。
func (s *CronJobStore) DeleteJob(jobID string) (bool, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readJSONUnlocked()
	kept := make([]map[string]any, 0, len(data.Jobs))
	deleted := false
	for _, item := range data.Jobs {
		if strings.TrimSpace(strAny(item["id"])) == jobID {
			deleted = true
			continue
		}
		kept = append(kept, item)
	}
	if deleted {
		data.Jobs = kept
		if err := s.writeJSONUnlocked(data); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// upsertJob 插入或覆盖任务。
// 对齐 Python: _upsert_job (store.py L176-196)。
func (s *CronJobStore) upsertJob(job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readJSONUnlocked()
	out := make([]map[string]any, 0, len(data.Jobs)+1)
	found := false
	for _, item := range data.Jobs {
		if strings.TrimSpace(strAny(item["id"])) == job.ID {
			out = append(out, job.ToDict())
			found = true
		} else {
			out = append(out, item)
		}
	}
	if !found {
		out = append(out, job.ToDict())
	}
	data.Jobs = out
	return s.writeJSONUnlocked(data)
}

// readJSON 读文件（加锁）。
func (s *CronJobStore) readJSON() *cronStoreFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readJSONUnlocked()
}

// readJSONUnlocked 读文件（不加锁）。
// 对齐 Python: _read_json_unlocked (store.py L202-217)。任何异常回退空 store。
// 先解顶层 map 再逐条过滤 jobs（对齐 Python isinstance(item, dict) 跳过非对象条目）。
func (s *CronJobStore) readJSONUnlocked() *cronStoreFile {
	data := &cronStoreFile{Version: 1}
	raw, err := os.ReadFile(s.storePath)
	if err != nil {
		return data
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return &cronStoreFile{Version: 1}
	}
	if v, ok := top["version"]; ok {
		if n, err := intAny(v); err == nil {
			data.Version = n
		}
	}
	if data.Version == 0 {
		data.Version = 1
	}
	if rawJobs, ok := top["jobs"].([]any); ok {
		for _, item := range rawJobs {
			if m, isMap := item.(map[string]any); isMap {
				data.Jobs = append(data.Jobs, m)
			}
		}
	}
	if data.Jobs == nil {
		data.Jobs = []map[string]any{}
	}
	return data
}

// writeJSONUnlocked 原子写（tmp + rename）。
// 对齐 Python: _write_json_unlocked (store.py L219-225)。
func (s *CronJobStore) writeJSONUnlocked(data *cronStoreFile) error {
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.storePath)
}

// nowTimestamp 当前 Unix 秒时间戳（含小数精度，对齐 Python time.time()）。
func nowTimestamp() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// newJobID 生成任务 ID（uuid hex 无连字符，对齐 Python uuid.uuid4().hex）。
func newJobID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
