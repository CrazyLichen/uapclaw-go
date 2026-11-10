package etcd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EtcdJsonClient etcd `/v3/kv/*` 与 `/v3/watch` 的 JSON 客户端。
// Python: client.py:90-264 EtcdJsonClient
//
// 多端点轮询：每次请求从 endpoints 依次取下一个，失败换下一个端点重试，
// 重试次数等于端点个数（上游同）。
type EtcdJsonClient struct {
	// endpoints 归一化后的端点列表
	endpoints []string
	// timeout 单次请求超时
	timeout time.Duration
	// mu 保护 index 与 client 的并发访问（Python 单线程无锁，Go 需加锁）
	mu sync.Mutex
	// index 轮询下标
	index int
	// client HTTP 客户端（惰性创建）
	client *http.Client
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrEtcd etcd 请求失败
	// Python: client.py:20-21 EtcdError
	ErrEtcd = errors.New("etcd request failed")
	// ErrEtcdCas 比较并交换失败（ModRevision 不匹配）
	// Python: client.py:24-25 EtcdCasError
	ErrEtcdCas = errors.New("etcd compare-and-swap failed")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEtcdJsonClient 创建客户端；非 http(s) 端点被丢弃。
// Python: client.py:93-104 EtcdJsonClient.__init__
func NewEtcdJsonClient(endpoints []string, timeout time.Duration) *EtcdJsonClient {
	normalized := make([]string, 0, len(endpoints))
	for _, item := range endpoints {
		if endpoint := normalizeEndpoint(item); endpoint != "" {
			normalized = append(normalized, endpoint)
		}
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &EtcdJsonClient{endpoints: normalized, timeout: timeout}
}

// Endpoints 返回归一化后的端点副本。
// Python: client.py:106-108 endpoints 属性
func (c *EtcdJsonClient) Endpoints() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.endpoints))
	copy(out, c.endpoints)
	return out
}

// Close 关闭空闲连接。
// Python: client.py:122-126 aclose
func (c *EtcdJsonClient) Close() error {
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client != nil {
		client.CloseIdleConnections()
	}
	return nil
}

// Range 读取单个键或区间。
// Python: client.py:163-180 range
func (c *EtcdJsonClient) Range(ctx context.Context, key []byte, rangeEnd []byte) (EtcdRangeResult, error) {
	body := map[string]any{"key": b64(key)}
	if rangeEnd != nil {
		body["range_end"] = b64(rangeEnd)
	}
	payload, err := c.postJSON(ctx, "/v3/kv/range", body)
	if err != nil {
		return EtcdRangeResult{}, err
	}

	kvs := make([]EtcdKv, 0)
	rawKvs, ok := payload["kvs"].([]any)
	if ok {
		for _, item := range rawKvs {
			if kv, parsed := parseKv(item); parsed {
				kvs = append(kvs, kv)
			}
		}
	}
	return EtcdRangeResult{Kvs: kvs, Revision: headerRevision(payload)}, nil
}

// Put 写入键值，返回写入后的集群版本。
// Python: client.py:182-187 put
func (c *EtcdJsonClient) Put(ctx context.Context, key, value []byte) (int64, error) {
	payload, err := c.postJSON(ctx, "/v3/kv/put", map[string]any{
		"key": b64(key), "value": b64(value),
	})
	if err != nil {
		return 0, err
	}
	return headerRevision(payload), nil
}

// Delete 删除单个键，返回删除后的集群版本。
// Python: client.py:189-194 delete
func (c *EtcdJsonClient) Delete(ctx context.Context, key []byte) (int64, error) {
	payload, err := c.postJSON(ctx, "/v3/kv/deleterange", map[string]any{"key": b64(key)})
	if err != nil {
		return 0, err
	}
	return headerRevision(payload), nil
}

// PutIfModRevision CAS 写入：仅当键的 ModRevision 等于给定值才成功。
// Python: client.py:196-224 put_if_mod_revision
//
// modRevision 为 0 表示"键必须不存在"。
func (c *EtcdJsonClient) PutIfModRevision(
	ctx context.Context, key, value []byte, modRevision int64,
) (int64, error) {
	body := map[string]any{
		"compare": []any{
			map[string]any{
				"result":       "EQUAL",
				"target":       "MOD",
				"key":          b64(key),
				"mod_revision": fmt.Sprintf("%d", modRevision),
			},
		},
		"success": []any{
			map[string]any{
				"request_put": map[string]any{
					"key": b64(key), "value": b64(value),
				},
			},
		},
		"failure": []any{},
	}
	payload, err := c.postJSON(ctx, "/v3/kv/txn", body)
	if err != nil {
		return 0, err
	}
	if succeeded, _ := payload["succeeded"].(bool); !succeeded {
		return 0, fmt.Errorf("%w: key=%q mod_revision=%d", ErrEtcdCas, key, modRevision)
	}
	return headerRevision(payload), nil
}

// WatchPrefix 持续产出前缀下变化键值的批次，直到流断开或 ctx 取消。
// Python: client.py:226-264 watch_prefix
//
// 返回的函数式迭代器（回调在每解析出一批时调用），与上游 AsyncIterator 等价。
// 回调返回 error 时终止监听并返回该错误。
func (c *EtcdJsonClient) WatchPrefix(
	ctx context.Context, prefix []byte, startRevision int64,
	onBatch func([]EtcdKv) error,
) error {
	c.mu.Lock()
	endpoints := append([]string{}, c.endpoints...)
	c.mu.Unlock()
	if len(endpoints) == 0 {
		return fmt.Errorf("%w: etcd endpoints are empty", ErrEtcd)
	}

	createRequest := map[string]any{
		"key":       b64(prefix),
		"range_end": b64(PrefixRangeEnd(prefix)),
	}
	if startRevision > 0 {
		createRequest["start_revision"] = fmt.Sprintf("%d", startRevision)
	}
	body := map[string]any{"create_request": createRequest}

	var lastErr error
	attempts := len(endpoints)
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		base := c.nextBase()
		requestURL := base + "/v3/watch"
		err := c.streamWatch(ctx, requestURL, body, onBatch)
		if err == nil {
			return nil
		}
		// ctx 取消属于正常终止
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("etcd watch failed")
	}
	return fmt.Errorf("%w: %v", ErrEtcd, lastErr)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeEndpoint 归一化端点：补 http:// 前缀、去尾斜杠、校验 scheme 与 host。
// Python: client.py:48-57 _normalize_endpoint
func normalizeEndpoint(raw string) string {
	text := strings.TrimRight(strings.TrimSpace(raw), "/")
	if text == "" {
		return ""
	}
	candidate := text
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/")
}

// nextBase 取下一个端点并推进轮询下标。
// Python: client.py:110-115 _next_base
func (c *EtcdJsonClient) nextBase() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.endpoints) == 0 {
		return ""
	}
	base := c.endpoints[c.index%len(c.endpoints)]
	c.index++
	return base
}

// httpClient 惰性创建 HTTP 客户端。
// Python: client.py:117-120 _http
func (c *EtcdJsonClient) httpClient() *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		c.client = &http.Client{Timeout: c.timeout}
	}
	return c.client
}

// postJSON 向 etcd 发送 JSON 请求，失败在多端点间轮询重试。
// Python: client.py:128-151 _post_json
func (c *EtcdJsonClient) postJSON(
	ctx context.Context, path string, body map[string]any,
) (map[string]any, error) {
	c.mu.Lock()
	attempts := len(c.endpoints)
	c.mu.Unlock()
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		base := c.nextBase()
		if base == "" {
			return nil, fmt.Errorf("%w: etcd endpoints are empty", ErrEtcd)
		}
		requestURL := base + path

		payload, err := c.doPostJSON(ctx, requestURL, body)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		return payload, nil
	}
	if lastErr == nil {
		lastErr = errors.New("etcd request failed")
	}
	return nil, fmt.Errorf("%w: %v", ErrEtcd, lastErr)
}

// doPostJSON 单次 POST 并把响应解析为对象。
// Python: client.py:134-146 单端点分支
func (c *EtcdJsonClient) doPostJSON(
	ctx context.Context, requestURL string, body map[string]any,
) (map[string]any, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	payload := map[string]any{}
	if err := decoder.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	// Python:139-146 status>=400 或响应带 error/message 即失败
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, messageOf(payload, response.StatusCode))
	}
	if text := messageOf(payload, 0); text != "" {
		return nil, errors.New(text)
	}
	return payload, nil
}

// messageOf 取响应体里的错误文案。
// Python: client.py:140-145 data.get("error") or data.get("message") or f"HTTP {code}"
func messageOf(payload map[string]any, statusCode int) string {
	if text, ok := payload["error"].(string); ok && text != "" {
		return text
	}
	if text, ok := payload["message"].(string); ok && text != "" {
		return text
	}
	if statusCode > 0 {
		return fmt.Sprintf("HTTP %d", statusCode)
	}
	return ""
}

// streamWatch 建立一次 watch 流并逐批解析。
// Python: client.py:248-259 单端点分支 + :267-281 _iter_watch_events
func (c *EtcdJsonClient) streamWatch(
	ctx context.Context, requestURL string, body map[string]any,
	onBatch func([]EtcdKv) error,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	// watch 是长连接：不复用带 Timeout 的客户端，避免被单次请求超时打断
	streamClient := &http.Client{}
	response, err := streamClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 {
		return fmt.Errorf("%s: HTTP %d", requestURL, response.StatusCode)
	}

	// watch 推送的是连续 JSON 对象流，json.Decoder 天然支持
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	for {
		var obj any
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		kvs, ok := extractWatchKvs(obj)
		if !ok {
			return errors.New("watch canceled or compacted")
		}
		if len(kvs) > 0 {
			if err := onBatch(kvs); err != nil {
				return err
			}
		}
	}
}

// extractWatchKvs 从一条 watch 响应里抽取变化键值。
// Python: client.py:284-305 _extract_watch_kvs
//
// 第二个返回值 ok=false 表示流被取消或压缩（上游抛 EtcdError）。
func extractWatchKvs(obj any) ([]EtcdKv, bool) {
	mapping, ok := obj.(map[string]any)
	if !ok {
		return nil, true
	}
	result, ok := mapping["result"].(map[string]any)
	if !ok {
		result = mapping
	}

	// Python:290-291 canceled / compact_revision → 报错
	if canceled, _ := result["canceled"].(bool); canceled {
		return nil, false
	}
	if compact, exists := result["compact_revision"]; exists && compact != nil {
		return nil, false
	}

	rawEvents, ok := result["events"].([]any)
	if !ok {
		return nil, true
	}
	out := make([]EtcdKv, 0, len(rawEvents))
	for _, item := range rawEvents {
		event, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kvSource, ok := event["kv"]
		if !ok {
			kvSource = event
		}
		if kv, parsed := parseKv(kvSource); parsed {
			out = append(out, kv)
		}
	}
	return out, true
}
