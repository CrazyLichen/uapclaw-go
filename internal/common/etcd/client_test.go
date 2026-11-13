package etcd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 前缀区间 ────────────────────────────

// TestPrefixRangeEnd_递增末字节 末字节 +1。
// Python: client.py:38-45 prefix_range_end
func TestPrefixRangeEnd_递增末字节(t *testing.T) {
	require.Equal(t, []byte("abd"), PrefixRangeEnd([]byte("abc")))
	require.Equal(t, []byte("b"), PrefixRangeEnd([]byte("a")))
}

// TestPrefixRangeEnd_进位 末字节为 0xFF 时向前进位并截断。
// Python: client.py:41-44 循环找 <0xFF 的字节
func TestPrefixRangeEnd_进位(t *testing.T) {
	require.Equal(t, []byte{0x61, 0x63}, PrefixRangeEnd([]byte{0x61, 0x62, 0xFF}))
}

// TestPrefixRangeEnd_全FF返零 全 0xFF 返回 "\x00"。
// Python: client.py:45 return b"\x00"
func TestPrefixRangeEnd_全FF返零(t *testing.T) {
	require.Equal(t, []byte{0x00}, PrefixRangeEnd([]byte{0xFF, 0xFF}))
	require.Equal(t, []byte{0x00}, PrefixRangeEnd(nil))
}

// ──────────────────────────── base64 测试分组 ────────────────────────────

// TestB64往返 编解码可逆，空串解码为空字节。
// Python: client.py:28-35 _b64 / _unb64
func TestB64往返(t *testing.T) {
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("abc")), b64([]byte("abc")))
	require.Equal(t, []byte("abc"), unb64(b64([]byte("abc"))))
	require.Equal(t, []byte{}, unb64(""))
}

// TestUnb64非法输入返回空 非法 base64 不 panic。
func TestUnb64非法输入返回空(t *testing.T) {
	require.Equal(t, []byte{}, unb64("!!!not-base64!!!"))
}

// ──────────────────────────── 端点归一化 ────────────────────────────

// TestNormalizeEndpoint_补协议 无协议补 http://。
// Python: client.py:48-57 _normalize_endpoint
func TestNormalizeEndpoint_补协议(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:2379", normalizeEndpoint("127.0.0.1:2379"))
}

// TestNormalizeEndpoint_去尾斜杠 尾部斜杠被去掉。
func TestNormalizeEndpoint_去尾斜杠(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:2379", normalizeEndpoint("http://127.0.0.1:2379/"))
}

// TestNormalizeEndpoint_拒绝非法 scheme 非 http/https 返回空。
// Python: client.py:53-54 scheme 校验
func TestNormalizeEndpoint_拒绝非法scheme(t *testing.T) {
	require.Equal(t, "", normalizeEndpoint("ftp://127.0.0.1:2379"))
	require.Equal(t, "", normalizeEndpoint("   "))
}

// ──────────────────────────── 假 etcd 服务 ────────────────────────────

// fakeEtcd 记录请求并返回预设响应。
type fakeEtcd struct {
	// server 测试服务器
	server *httptest.Server
	// paths 收到的请求路径
	paths []string
	// bodies 收到的请求体
	bodies []map[string]any
	// respond 根据路径生成响应体
	respond func(path string, body map[string]any) (int, map[string]any)
}

// newFakeEtcd 启动假 etcd 服务。
func newFakeEtcd(t *testing.T, respond func(string, map[string]any) (int, map[string]any)) *fakeEtcd {
	t.Helper()
	fake := &fakeEtcd{respond: respond}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&body)

		fake.paths = append(fake.paths, r.URL.Path)
		fake.bodies = append(fake.bodies, body)

		status, payload := 200, map[string]any{}
		if fake.respond != nil {
			status, payload = fake.respond(r.URL.Path, body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// client 返回指向假服务的客户端。
func (f *fakeEtcd) client() *EtcdJsonClient {
	return NewEtcdJsonClient([]string{f.server.URL}, 5*time.Second)
}

// ──────────────────────────── Range 测试分组 ────────────────────────────

// TestRange_解析结果 base64 解码与版本号解析。
// Python: client.py:163-180 range + :75-87 _parse_kv
func TestRange_解析结果(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{
			"header": map[string]any{"revision": "42"},
			"kvs": []any{
				map[string]any{
					"key":             b64([]byte("k1")),
					"value":           b64([]byte("v1")),
					"mod_revision":    "7",
					"create_revision": "3",
					"version":         "2",
				},
			},
		}
	})

	result, err := fake.client().Range(context.Background(), []byte("k1"), nil)
	require.NoError(t, err)
	require.Equal(t, int64(42), result.Revision)
	require.Len(t, result.Kvs, 1)
	require.Equal(t, []byte("k1"), result.Kvs[0].Key)
	require.Equal(t, []byte("v1"), result.Kvs[0].Value)
	require.Equal(t, int64(7), result.Kvs[0].ModRevision)
	require.Equal(t, int64(3), result.Kvs[0].CreateRevision)
	require.Equal(t, int64(2), result.Kvs[0].Version)
}

// TestRange_带rangeEnd rangeEnd 出现在请求体里。
// Python: client.py:169-171
func TestRange_带rangeEnd(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{"header": map[string]any{"revision": "1"}}
	})

	_, err := fake.client().Range(context.Background(), []byte("a"), []byte("b"))
	require.NoError(t, err)
	require.Len(t, fake.bodies, 1)
	require.Equal(t, b64([]byte("a")), fake.bodies[0]["key"])
	require.Equal(t, b64([]byte("b")), fake.bodies[0]["range_end"])
}

// TestRange_空结果 无 kvs 返回空切片。
func TestRange_空结果(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{}
	})
	result, err := fake.client().Range(context.Background(), []byte("missing"), nil)
	require.NoError(t, err)
	require.Empty(t, result.Kvs)
	require.Equal(t, int64(0), result.Revision)
}

// ──────────────────────────── Put / Delete 测试分组 ────────────────────────────

// TestPut_返回版本 请求体含 base64 键值，返回 header.revision。
// Python: client.py:182-187 put
func TestPut_返回版本(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{"header": map[string]any{"revision": "9"}}
	})

	revision, err := fake.client().Put(context.Background(), []byte("k"), []byte("v"))
	require.NoError(t, err)
	require.Equal(t, int64(9), revision)
	require.Equal(t, b64([]byte("k")), fake.bodies[0]["key"])
	require.Equal(t, b64([]byte("v")), fake.bodies[0]["value"])
}

// TestDelete_走deleterange 删除走 /v3/kv/deleterange。
// Python: client.py:189-194
func TestDelete_走deleterange(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{"header": map[string]any{"revision": "10"}}
	})

	revision, err := fake.client().Delete(context.Background(), []byte("k"))
	require.NoError(t, err)
	require.Equal(t, int64(10), revision)
	require.Equal(t, "/v3/kv/deleterange", fake.paths[0])
}

// ──────────────────────────── CAS 测试分组 ────────────────────────────

// TestPutIfModRevision_成功 请求体是 compare/success/failure 五段式。
// Python: client.py:207-224
func TestPutIfModRevision_成功(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{
			"header":    map[string]any{"revision": "11"},
			"succeeded": true,
		}
	})

	revision, err := fake.client().PutIfModRevision(
		context.Background(), []byte("k"), []byte("v"), 5)
	require.NoError(t, err)
	require.Equal(t, int64(11), revision)

	body := fake.bodies[0]
	compare, ok := body["compare"].([]any)
	require.True(t, ok)
	require.Len(t, compare, 1)
	first := compare[0].(map[string]any)
	require.Equal(t, "EQUAL", first["result"])
	require.Equal(t, "MOD", first["target"])
	require.Equal(t, "5", first["mod_revision"])
	require.NotNil(t, body["success"])
	require.NotNil(t, body["failure"])
}

// TestPutIfModRevision_失败报CAS错误 succeeded 为假时报 ErrEtcdCas。
// Python: client.py:222-223
func TestPutIfModRevision_失败报CAS错误(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{"succeeded": false}
	})

	_, err := fake.client().PutIfModRevision(context.Background(), []byte("k"), []byte("v"), 5)
	require.ErrorIs(t, err, ErrEtcdCas)
}

// ──────────────────────────── 错误与重试 ────────────────────────────

// TestPostJSON_HTTP错误 状态码 >=400 时报错并带服务端文案。
// Python: client.py:139-145
func TestPostJSON_HTTP错误(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 500, map[string]any{"error": "internal boom"}
	})

	_, err := fake.client().Put(context.Background(), []byte("k"), []byte("v"))
	require.ErrorIs(t, err, ErrEtcd)
	require.Contains(t, err.Error(), "internal boom")
}

// TestPostJSON_响应带message视为失败 200 但带 message 也报错。
// Python: client.py:139 data.get("message") 分支
func TestPostJSON_响应带message视为失败(t *testing.T) {
	fake := newFakeEtcd(t, func(string, map[string]any) (int, map[string]any) {
		return 200, map[string]any{"message": "etcd: duplicate key"}
	})

	_, err := fake.client().Put(context.Background(), []byte("k"), []byte("v"))
	require.ErrorIs(t, err, ErrEtcd)
	require.Contains(t, err.Error(), "duplicate key")
}

// TestPostJSON_多端点重试 首个端点失败后切到第二个端点成功。
// Python: client.py:131-151 多端点轮询
func TestPostJSON_多端点重试(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"header":{"revision":"3"}}`))
	}))
	defer good.Close()

	client := NewEtcdJsonClient([]string{bad.URL, good.URL}, 5*time.Second)
	revision, err := client.Put(context.Background(), []byte("k"), []byte("v"))
	require.NoError(t, err)
	require.Equal(t, int64(3), revision)
}

// TestPostJSON_端点全空 无有效端点时报错。
// Python: client.py:112-113 raise EtcdError("etcd endpoints are empty")
func TestPostJSON_端点全空(t *testing.T) {
	client := NewEtcdJsonClient([]string{"ftp://bad", ""}, time.Second)
	require.Empty(t, client.Endpoints())

	_, err := client.Put(context.Background(), []byte("k"), []byte("v"))
	require.ErrorIs(t, err, ErrEtcd)
}

// ──────────────────────────── Watch 测试分组 ────────────────────────────

// TestWatchPrefix_流式解析 连续 JSON 对象逐个解析并回调。
// Python: client.py:267-305 _iter_watch_events / _extract_watch_kvs
func TestWatchPrefix_流式解析(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 两个连续 JSON 对象，模拟 watch 推送
		_, _ = w.Write([]byte(`{"result":{"events":[{"kv":{"key":"` + b64([]byte("k1")) + `","value":"` + b64([]byte("v1")) + `"}}]}}`))
		_, _ = w.Write([]byte(`{"result":{"events":[{"kv":{"key":"` + b64([]byte("k2")) + `","value":"` + b64([]byte("v2")) + `"}}]}}`))
	}))
	defer server.Close()

	client := NewEtcdJsonClient([]string{server.URL}, 5*time.Second)
	batches := make([][]EtcdKv, 0)

	err := client.WatchPrefix(context.Background(), []byte("k"), 0, func(kvs []EtcdKv) error {
		batches = append(batches, kvs)
		if len(batches) == 2 {
			return errors.New("够了")
		}
		return nil
	})
	require.Error(t, err)
	require.Len(t, batches, 2)
	require.Equal(t, []byte("k1"), batches[0][0].Key)
	require.Equal(t, []byte("k2"), batches[1][0].Key)
}

// TestWatchPrefix_取消视为终止 流结束（EOF）时正常返回。
func TestWatchPrefix_取消视为终止(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	}))
	defer server.Close()

	client := NewEtcdJsonClient([]string{server.URL}, 5*time.Second)
	called := false
	err := client.WatchPrefix(context.Background(), []byte("k"), 0, func([]EtcdKv) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	require.False(t, called)
}

// TestWatchPrefix_canceled视为错误 canceled 为真时报错。
// Python: client.py:290-291
func TestWatchPrefix_canceled视为错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"canceled":true,"cancel_reason":"compacted"}}`))
	}))
	defer server.Close()

	client := NewEtcdJsonClient([]string{server.URL}, 5*time.Second)
	err := client.WatchPrefix(context.Background(), []byte("k"), 0, func([]EtcdKv) error { return nil })
	require.ErrorIs(t, err, ErrEtcd)
}

// TestWatchPrefix_startRevision 起始版本号写入 create_request。
// Python: client.py:242-243
func TestWatchPrefix_startRevision(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&received)
		w.Header().Set("Content-Type", "application/json")
	}))
	defer server.Close()

	client := NewEtcdJsonClient([]string{server.URL}, 5*time.Second)
	_ = client.WatchPrefix(context.Background(), []byte("k"), 99, func([]EtcdKv) error { return nil })

	createRequest, ok := received["create_request"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "99", createRequest["start_revision"])
	require.Equal(t, b64(PrefixRangeEnd([]byte("k"))), createRequest["range_end"])
}

// ──────────────────────────── 其他 ────────────────────────────

// TestExtractWatchKvs_非对象返回空 非 map 输入安全返回。
// Python: client.py:285-286
func TestExtractWatchKvs_非对象返回空(t *testing.T) {
	kvs, ok := extractWatchKvs("not-a-map")
	require.True(t, ok)
	require.Empty(t, kvs)
}

// TestClose_可重复调用 重复关闭不报错。
// Python: client.py:122-126
func TestClose_可重复调用(t *testing.T) {
	client := NewEtcdJsonClient([]string{"127.0.0.1:2379"}, time.Second)
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
}

// TestParseInt64Field_各类输入 数字/字符串/缺省/非法。
// Python: client.py:158-161 int(header.get("revision") or 0)
func TestParseInt64Field_各类输入(t *testing.T) {
	cases := []struct {
		value any
		want  int64
		ok    bool
	}{
		{nil, 0, true},
		{json.Number("42"), 42, true},
		{float64(7), 7, true},
		{"13", 13, true},
		{"", 0, true},
		{"abc", 0, false},
		{[]string{"x"}, 0, false},
	}
	for _, c := range cases {
		got, ok := parseInt64Field(c.value)
		require.Equal(t, c.ok, ok, "value=%v", c.value)
		if c.ok {
			require.Equal(t, c.want, got, "value=%v", c.value)
		}
	}
}
