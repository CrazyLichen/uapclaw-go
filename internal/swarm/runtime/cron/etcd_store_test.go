package cron

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/etcd"
)

// fakeEtcdCron 极简假 etcd：内存 kv + 按路径分发。
type fakeEtcdCron struct {
	// data 内存键值
	data map[string]string
	// revisions 每次写入递增
	revisions int64
	// server 测试服务
	server *httptest.Server
}

// newFakeEtcdCron 启动假 etcd。
func newFakeEtcdCron(t *testing.T) *fakeEtcdCron {
	t.Helper()
	fake := &fakeEtcdCron{data: map[string]string{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v3/kv/range":
			fake.handleRange(t, w, body)
		case "/v3/kv/put":
			fake.handlePut(t, w, body)
		case "/v3/kv/deleterange":
			fake.handleDelete(t, w, body)
		case "/v3/kv/txn":
			fake.handleTxn(t, w, body)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// handleRange 前缀或单键读取。
func (f *fakeEtcdCron) handleRange(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	key := decodeB64(stringField(body["key"]))
	kvs := make([]any, 0)
	for storedKey, value := range f.data {
		// 单键精确匹配或前缀匹配（假实现简化：range_end 存在即前缀扫描）
		_, hasRangeEnd := body["range_end"]
		if (hasRangeEnd && len(storedKey) >= len(key) && storedKey[:len(key)] == key) ||
			(!hasRangeEnd && storedKey == key) {
			kvs = append(kvs, map[string]any{
				"key":             encodeB64(storedKey),
				"value":           encodeB64(value),
				"mod_revision":    "1",
				"create_revision": "1",
				"version":         "1",
			})
		}
	}
	f.revisions++
	_ = json.NewEncoder(w).Encode(map[string]any{
		"header": map[string]any{"revision": "1"},
		"kvs":    kvs,
	})
}

// handlePut 写入。
func (f *fakeEtcdCron) handlePut(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	key := decodeB64(stringField(body["key"]))
	f.data[key] = decodeB64(stringField(body["value"]))
	f.revisions++
	_ = json.NewEncoder(w).Encode(map[string]any{"header": map[string]any{"revision": "1"}})
}

// handleDelete 删除。
func (f *fakeEtcdCron) handleDelete(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	delete(f.data, decodeB64(stringField(body["key"])))
	f.revisions++
	_ = json.NewEncoder(w).Encode(map[string]any{"header": map[string]any{"revision": "1"}})
}

// handleTxn CAS 写入（假实现恒成功）。
func (f *fakeEtcdCron) handleTxn(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	success, _ := body["success"].([]any)
	if len(success) > 0 {
		if request, ok := success[0].(map[string]any); ok {
			if put, ok := request["request_put"].(map[string]any); ok {
				key := decodeB64(stringField(put["key"]))
				f.data[key] = decodeB64(stringField(put["value"]))
			}
		}
	}
	f.revisions++
	_ = json.NewEncoder(w).Encode(map[string]any{
		"header":    map[string]any{"revision": "1"},
		"succeeded": true,
	})
}

// store 返回指向假服务的 etcd 存储。
func (f *fakeEtcdCron) store() *EtcdCronJobStore {
	client := etcd.NewEtcdJsonClient([]string{f.server.URL}, 5*time.Second)
	return NewEtcdCronJobStore(nil, "", client, 5*time.Second)
}

// encodeB64 编码。
func encodeB64(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// decodeB64 解码。
func decodeB64(value string) string {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// ──────────────────────────── 基本属性 ────────────────────────────

// TestEtcdCronJobStore_支持watch etcd 后端 supports_watch 为真。
// Python: etcd_store.py:39 supports_watch = True
func TestEtcdCronJobStore_支持watch(t *testing.T) {
	require.True(t, newFakeEtcdCron(t).store().SupportsWatch())
}

// ──────────────────────────── 创建与读取 ────────────────────────────

// TestEtcdCreateJob_写入可读 创建后能读到。
// Python: etcd_store.py:155-227
func TestEtcdCreateJob_写入可读(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	req, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.Equal(t, created.Name, req.Name)
}

// TestEtcdListJobs_列出全部 前缀扫描返回全部任务。
// Python: etcd_store.py:89-120
func TestEtcdListJobs_列出全部(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	_, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	second := baseCreateInput()
	second.Name = "第二个"
	_, err = store.CreateJob(ctx, second)
	require.NoError(t, err)

	jobs, err := store.ListJobs(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
}

// TestEtcdGetJob_不存在返回nil 未知 ID 返回 nil。
// Python: etcd_store.py:122-141
func TestEtcdGetJob_不存在返回nil(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	job, err := store.GetJob(context.Background(), "never")
	require.NoError(t, err)
	require.Nil(t, job)
}

// ──────────────────────────── 更新与删除 ────────────────────────────

// TestEtcdUpdateJob_改名 补丁生效。
// Python: etcd_store.py:229-274
func TestEtcdUpdateJob_改名(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	updated, err := store.UpdateJob(ctx, created.ID, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, "新名", updated.Name)
}

// TestEtcdUpdateJob_不存在报错 未知 ID 返回 ErrJobNotFound。
func TestEtcdUpdateJob_不存在报错(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	_, err := store.UpdateJob(context.Background(), "never", map[string]any{"name": "x"})
	require.ErrorIs(t, err, ErrJobNotFound)
}

// TestEtcdDeleteJob_删除成功 删除后读不到。
// Python: etcd_store.py:276-288
func TestEtcdDeleteJob_删除成功(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	deleted, err := store.DeleteJob(ctx, created.ID, false)
	require.NoError(t, err)
	require.True(t, deleted)

	job, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, job)
}

// TestEtcdDeleteJob_proactive受保护 未 force 拒删。
func TestEtcdDeleteJob_proactive受保护(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	input := baseCreateInput()
	mode := proactiveTickMode
	input.Mode = &mode
	created, err := store.CreateJob(ctx, input)
	require.NoError(t, err)

	_, err = store.DeleteJob(ctx, created.ID, false)
	require.ErrorIs(t, err, ErrProactiveJobProtected)
}

// TestEtcdDeleteJob_不存在返回false 未知 ID 返回假。
func TestEtcdDeleteJob_不存在返回false(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	deleted, err := store.DeleteJob(context.Background(), "never", false)
	require.NoError(t, err)
	require.False(t, deleted)
}

// ──────────────────────────── 修订号与关闭 ────────────────────────────

// TestEtcdGetRevision_有值 读取后修订号非负。
// Python: etcd_store.py:86-87
func TestEtcdGetRevision_有值(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx := context.Background()

	_, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	revision, err := store.GetRevision(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, revision, int64(0))
}

// TestEtcdClose_可调用 关闭不报错。
// Python: etcd_store.py:304-305
func TestEtcdClose_可调用(t *testing.T) {
	require.NoError(t, newFakeEtcdCron(t).store().Close(context.Background()))
}

// TestEtcdWatch_取消退出 context 取消后退出。
// Python: etcd_store.py:290-302
func TestEtcdWatch_取消退出(t *testing.T) {
	store := newFakeEtcdCron(t).store()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// watch 会先回调一次再进入监听；取消后应尽快返回
	require.Error(t, store.Watch(ctx, func(context.Context) error { return nil }))
}

// TestEtcdListJobs_不可达返回空 strict=false 连接失败返回空而非错误。
// Python: etcd_store.py:96-102
func TestEtcdListJobs_不可达返回空(t *testing.T) {
	client := etcd.NewEtcdJsonClient([]string{"http://127.0.0.1:1"}, 200*time.Millisecond)
	store := NewEtcdCronJobStore(nil, "", client, 200*time.Millisecond)

	jobs, err := store.ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestEtcdListJobs_不可达strict报错 strict 模式下连接失败上抛。
func TestEtcdListJobs_不可达strict报错(t *testing.T) {
	client := etcd.NewEtcdJsonClient([]string{"http://127.0.0.1:1"}, 200*time.Millisecond)
	store := NewEtcdCronJobStore(nil, "", client, 200*time.Millisecond)

	_, err := store.ListJobs(context.Background(), true)
	require.Error(t, err)
}
