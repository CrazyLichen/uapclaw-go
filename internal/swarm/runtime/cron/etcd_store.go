package cron

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/etcd"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EtcdCronJobStore 基于 etcd 的定时任务存储（一体机主备 Gateway 用）。
// Python: etcd_store.py:31-291 EtcdCronJobStore
//
// 每个任务一个键：`/jiuwenswarm/cron/jobs/{job_id}`。
// 连接失败不阻塞 Gateway 启动：list_jobs 在 etcd 可达前返回空，
// watch 循环带退避重试后再重载。
type EtcdCronJobStore struct {
	// prefix 键前缀（保证以 / 结尾）
	prefix string
	// client etcd JSON 客户端
	client *etcd.EtcdJsonClient
	// mu 进程内串行
	mu sync.Mutex
	// revision 最近一次观测到的集群修订号
	revision int64
	// loggedEmptyEndpoints 空端点告警只打一次
	loggedEmptyEndpoints bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultEtcdPrefix 默认键前缀
	// Python: etcd_store.py:27 _DEFAULT_PREFIX
	defaultEtcdPrefix = "/jiuwenswarm/cron/jobs/"
	// connectInitialDelay watch 重连初始退避
	// Python: etcd_store.py:28 _CONNECT_INITIAL_DELAY
	connectInitialDelay = time.Second
	// connectMaxDelay watch 重连最大退避
	// Python: etcd_store.py:29 _CONNECT_MAX_DELAY
	connectMaxDelay = 30 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEtcdCronJobStore 创建 etcd 存储。
// Python: etcd_store.py:42-61 EtcdCronJobStore.__init__
func NewEtcdCronJobStore(endpoints []string, prefix string, client *etcd.EtcdJsonClient, timeout time.Duration) *EtcdCronJobStore {
	normalizedPrefix := strings.TrimSpace(prefix)
	if normalizedPrefix == "" {
		normalizedPrefix = defaultEtcdPrefix
	}
	if !strings.HasSuffix(normalizedPrefix, "/") {
		normalizedPrefix += "/"
	}
	if client == nil {
		client = etcd.NewEtcdJsonClient(endpoints, timeout)
	}
	return &EtcdCronJobStore{prefix: normalizedPrefix, client: client}
}

// Prefix 返回键前缀。
func (s *EtcdCronJobStore) Prefix() string { return s.prefix }

// SupportsWatch etcd 后端支持远程变更监听。
// Python: etcd_store.py:39 supports_watch = True
func (s *EtcdCronJobStore) SupportsWatch() bool { return true }

// GetRevision 返回最近观测的集群修订号。
// Python: etcd_store.py:86-87 get_revision
func (s *EtcdCronJobStore) GetRevision(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision, nil
}

// ListJobs 列全部任务（前缀区间扫描）。
// Python: etcd_store.py:89-120 list_jobs
func (s *EtcdCronJobStore) ListJobs(ctx context.Context, strict bool) ([]*CronJob, error) {
	prefixBytes := s.prefixBytes()
	result, err := s.client.Range(ctx, prefixBytes, etcd.PrefixRangeEnd(prefixBytes))
	if err != nil {
		if strict {
			return nil, err
		}
		return nil, nil
	}
	if result.Revision != 0 {
		s.mu.Lock()
		s.revision = result.Revision
		s.mu.Unlock()
	}

	rawJobs := make([]any, 0, len(result.Kvs))
	for _, kv := range result.Kvs {
		item := map[string]any{}
		if err := json.Unmarshal(kv.Value, &item); err != nil {
			if strict {
				return nil, err
			}
			continue
		}
		rawJobs = append(rawJobs, item)
	}

	migrated, changed := MigrateWorkModeOnItems(rawJobs)
	if changed {
		// 迁移写回失败只告警：迁移是优化，不阻断读路径
		for _, entry := range migrated {
			mapping, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			jobID := trimSpace(stringField(mapping["id"]))
			if jobID == "" {
				continue
			}
			payload, marshalErr := json.Marshal(mapping)
			if marshalErr != nil {
				continue
			}
			_, _ = s.client.Put(ctx, s.jobKey(jobID), payload)
		}
	}

	if strict {
		jobs := make([]*CronJob, 0, len(migrated))
		for _, entry := range migrated {
			mapping, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			job, err := CronJobFromDict(mapping)
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, job)
		}
		return jobs, nil
	}
	return ParseCronJobs(migrated), nil
}

// GetJob 读单个任务；不存在返回 nil。
// Python: etcd_store.py:122-141 get_job
func (s *EtcdCronJobStore) GetJob(ctx context.Context, jobID string) (*CronJob, error) {
	target := trimSpace(jobID)
	if target == "" {
		return nil, nil
	}
	result, err := s.client.Range(ctx, s.jobKey(target), nil)
	if err != nil {
		return nil, nil
	}
	if result.Revision != 0 {
		s.mu.Lock()
		s.revision = result.Revision
		s.mu.Unlock()
	}
	if len(result.Kvs) == 0 {
		return nil, nil
	}
	item := map[string]any{}
	if err := json.Unmarshal(result.Kvs[0].Value, &item); err != nil {
		return nil, nil
	}
	job, err := CronJobFromDict(item)
	if err != nil {
		return nil, nil
	}
	return job, nil
}

// CreateJob 构造并写入。
// Python: etcd_store.py:155-227 create_job
func (s *EtcdCronJobStore) CreateJob(ctx context.Context, input CreateJobInput) (*CronJob, error) {
	job, err := BuildNewCronJob(input)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(job.ToDict())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	revision, putErr := s.client.Put(ctx, s.jobKey(job.ID), payload)
	if putErr == nil {
		s.revision = revision
	}
	s.mu.Unlock()
	if putErr != nil {
		return nil, s.unavailable("put")
	}
	return job, nil
}

// UpdateJob CAS 更新，冲突时重读重试一次。
// Python: etcd_store.py:229-274 update_job
func (s *EtcdCronJobStore) UpdateJob(ctx context.Context, jobID string, patch map[string]any) (*CronJob, error) {
	target := trimSpace(jobID)
	if target == "" {
		return nil, errors.New("id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, modRevision, err := s.getJobWithRev(ctx, target)
	if err != nil {
		return nil, s.unavailable("get")
	}
	if existing == nil {
		return nil, ErrJobNotFound
	}

	updated, err := ApplyCronJobPatch(existing, patch)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(updated.ToDict())
	if err != nil {
		return nil, err
	}

	revision, casErr := s.client.PutIfModRevision(ctx, s.jobKey(target), payload, modRevision)
	if casErr == nil {
		s.revision = revision
		return updated, nil
	}
	if !errors.Is(casErr, etcd.ErrEtcdCas) {
		return nil, s.unavailable("cas-put")
	}

	// CAS 失败：重读一次再试
	existing, modRevision, err = s.getJobWithRev(ctx, target)
	if err != nil {
		return nil, s.unavailable("get")
	}
	if existing == nil {
		return nil, ErrJobNotFound
	}
	updated, err = ApplyCronJobPatch(existing, patch)
	if err != nil {
		return nil, err
	}
	payload, err = json.Marshal(updated.ToDict())
	if err != nil {
		return nil, err
	}
	revision, casErr = s.client.PutIfModRevision(ctx, s.jobKey(target), payload, modRevision)
	if casErr != nil {
		return nil, s.unavailable("cas-put")
	}
	s.revision = revision
	return updated, nil
}

// DeleteJob 删除任务。
// Python: etcd_store.py:276-288 delete_job
func (s *EtcdCronJobStore) DeleteJob(ctx context.Context, jobID string, force bool) (bool, error) {
	target := trimSpace(jobID)
	if target == "" {
		return false, nil
	}
	existing, err := s.GetJob(ctx, target)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	if err := EnsureProactiveDeletable(existing, force); err != nil {
		return false, err
	}

	s.mu.Lock()
	revision, deleteErr := s.client.Delete(ctx, s.jobKey(target))
	if deleteErr == nil {
		s.revision = revision
	}
	s.mu.Unlock()
	if deleteErr != nil {
		return false, s.unavailable("delete")
	}
	return true, nil
}

// Watch 阻塞监听前缀变更，带指数退避重连。
// Python: etcd_store.py:290-302 watch
//
// 每次连接建立与每批变更都回调一次（上游同）。
func (s *EtcdCronJobStore) Watch(ctx context.Context, callback func(context.Context) error) error {
	delay := connectInitialDelay
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 空端点视为不可用，走退避重试
		if len(s.client.Endpoints()) == 0 {
			if !s.loggedEmptyEndpoints {
				s.loggedEmptyEndpoints = true
			}
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
			delay = nextDelay(delay)
			continue
		}
		if callback != nil {
			if err := callback(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}

		watchErr := s.client.WatchPrefix(ctx, s.prefixBytes(), 0, func([]etcd.EtcdKv) error {
			if callback == nil {
				return nil
			}
			return callback(ctx)
		})
		if watchErr == nil || errors.Is(watchErr, context.Canceled) {
			return watchErr
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return err
		}
		delay = nextDelay(delay)
	}
}

// Close 释放客户端资源。
// Python: etcd_store.py:304-305 aclose
func (s *EtcdCronJobStore) Close(context.Context) error {
	return s.client.Close()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jobKey 任务键（前缀 + jobID）。
// Python: etcd_store.py:63-64 _job_key
func (s *EtcdCronJobStore) jobKey(jobID string) []byte {
	return []byte(s.prefix + jobID)
}

// prefixBytes 前缀字节。
// Python: etcd_store.py:66-67 _prefix_bytes
func (s *EtcdCronJobStore) prefixBytes() []byte {
	return []byte(s.prefix)
}

// unavailable 构造不可用错误。
// Python: etcd_store.py:69-84 _unavailable
func (s *EtcdCronJobStore) unavailable(action string) error {
	if len(s.client.Endpoints()) == 0 {
		s.mu.Lock()
		s.loggedEmptyEndpoints = true
		s.mu.Unlock()
		return errors.New("etcd endpoints are empty")
	}
	return errors.New("etcd " + action + " failed")
}

// getJobWithRev 读任务与其 modRevision。
// Python: etcd_store.py:143-153 _get_job_with_rev
func (s *EtcdCronJobStore) getJobWithRev(ctx context.Context, jobID string) (*CronJob, int64, error) {
	result, err := s.client.Range(ctx, s.jobKey(jobID), nil)
	if err != nil {
		return nil, 0, err
	}
	if result.Revision != 0 {
		s.revision = result.Revision
	}
	if len(result.Kvs) == 0 {
		return nil, 0, nil
	}
	kv := result.Kvs[0]
	item := map[string]any{}
	if err := json.Unmarshal(kv.Value, &item); err != nil {
		return nil, kv.ModRevision, nil
	}
	job, err := CronJobFromDict(item)
	if err != nil {
		return nil, kv.ModRevision, nil
	}
	return job, kv.ModRevision, nil
}

// sleepCtx 可取消的等待。
func sleepCtx(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// nextDelay 退避翻倍并封顶。
// Python: etcd_store.py:298 delay = min(delay * 2, _CONNECT_MAX_DELAY)
func nextDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > connectMaxDelay {
		return connectMaxDelay
	}
	return next
}

// ensureInterface 编译期确认实现后端协议。
var _ CronJobStoreBackend = (*EtcdCronJobStore)(nil)
