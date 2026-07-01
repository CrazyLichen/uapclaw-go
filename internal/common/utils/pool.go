// utils 包提供通用工具函数。
//
// pool.go 实现引用计数资源池和 HTTP Transport 连接池管理。
// 对应 Python：
//   - openjiuwen/core/common/clients/ref_counted.py（RefCountedResource）
//   - openjiuwen/core/common/clients/connector_pool.py（ConnectorPoolManager + ConnectorPoolConfig + TcpConnectorPool）
//
// Go 版本使用 http.Transport 替代 Python 的 aiohttp.TCPConnector，
// Transport 内部自带连接池，无需像 Python 那样手动管理连接。
// 但引用计数 + 共享 Transport 实例的模式仍然需要。

package utils

import (
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RefCountedResource 引用计数资源基类。
//
// 对应 Python: RefCountedResource
// 跟踪 ref_count/created_at/last_used/closed 状态。
type RefCountedResource struct {
	refCount  atomic.Int64
	closed    atomic.Bool
	createdAt time.Time
	lastUsed  atomic.Int64 // Unix 时间戳（纳秒）
}

// TransportConfig HTTP Transport 连接池配置。
//
// 对应 Python: ConnectorPoolConfig
type TransportConfig struct {
	MaxIdleConns        int           // 总最大空闲连接（默认 100）
	MaxIdleConnsPerHost int           // 每主机最大空闲连接（默认 30）
	MaxConnsPerHost     int           // 每主机最大连接数（默认 0=无限制）
	IdleConnTimeout     time.Duration // 空闲连接超时（默认 90s）
	TLSHandshakeTimeout time.Duration // TLS 握手超时（默认 10s）
	DisableKeepAlives   bool          // 禁用 keep-alive
	SSLVerify           bool          // SSL 验证（默认 true）
	TTL                 time.Duration // Transport 生存时间（默认 1h）
	MaxIdleTime         time.Duration // 最大空闲时间（默认 5min）
}

// RefCountedTransport 引用计数的 HTTP Transport。
//
// 对应 Python: TcpConnectorPool (ConnectorPool)
type RefCountedTransport struct {
	RefCountedResource
	config    TransportConfig
	transport *http.Transport
}

// TransportPool Transport 连接池管理器。
//
// 对应 Python: ConnectorPoolManager
// 按配置 key 复用 Transport 实例，超限时淘汰最久未用的。
type TransportPool struct {
	transports map[string]*RefCountedTransport
	mu         sync.Mutex
	maxPool    int
}

// ResourcePool 泛型资源池，按配置 key 复用资源实例。
//
// 对应 Python: BaseRefResourceMgr
// 通用版本，供未来其他资源（如 Redis 连接池）复用。
type ResourcePool[T any] struct {
	resources map[string]*refCountedEntry[T]
	mu        sync.Mutex
	maxPool   int
	factory   func(key string, config any) (*T, error)
	keyFunc   func(config any) string
}

// refCountedEntry 泛型引用计数资源条目。
type refCountedEntry[T any] struct {
	resource  *T
	refCount  int
	createdAt time.Time
	lastUsed  time.Time
	closed    bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// transportPoolSingleton 全局 TransportPool 单例持有器。
var transportPoolSingleton Singleton[TransportPool]

// ──────────────────────────── 导出函数 ────────────────────────────

// InitRefCount 初始化引用计数（初始值为 1）。
// 需要在嵌入 RefCountedResource 的结构体创建时调用。
func (r *RefCountedResource) InitRefCount() {
	r.refCount.Store(1)
	r.createdAt = time.Now()
	r.lastUsed.Store(time.Now().UnixNano())
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IncRef 增加引用计数，返回新的计数。
// 对应 Python: increment_ref()
func (r *RefCountedResource) IncRef() int64 {
	if r.closed.Load() {
		return 0
	}
	r.lastUsed.Store(time.Now().UnixNano())
	return r.refCount.Add(1)
}

// DecRef 减少引用计数，返回 true 表示计数降至 0。
// 对应 Python: decrement_ref()
func (r *RefCountedResource) DecRef() bool {
	if r.closed.Load() {
		return false
	}
	newCount := r.refCount.Add(-1)
	return newCount <= 0
}

// IsClosed 返回资源是否已关闭。
func (r *RefCountedResource) IsClosed() bool {
	return r.closed.Load()
}

// RefCount 返回当前引用计数。
func (r *RefCountedResource) RefCount() int64 {
	return r.refCount.Load()
}

// CreatedAt 返回创建时间。
func (r *RefCountedResource) CreatedAt() time.Time {
	return r.createdAt
}

// LastUsed 返回最后使用时间。
func (r *RefCountedResource) LastUsed() time.Time {
	nano := r.lastUsed.Load()
	if nano == 0 {
		return r.createdAt
	}
	return time.Unix(0, nano)
}

// IsExpired 检查资源是否过期。
// 对应 Python: is_expired()
func (r *RefCountedResource) IsExpired(ttl, maxIdle time.Duration) bool {
	now := time.Now()
	if ttl > 0 && now.Sub(r.createdAt) > ttl {
		return true
	}
	if maxIdle > 0 && now.Sub(r.LastUsed()) > maxIdle {
		return true
	}
	return false
}

// MarkClosed 标记资源为已关闭。
func (r *RefCountedResource) MarkClosed() {
	r.closed.Store(true)
}

// DefaultTransportConfig 返回默认 Transport 配置。
func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 30,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
		SSLVerify:           true,
		TTL:                 1 * time.Hour,
		MaxIdleTime:         5 * time.Minute,
	}
}

// GenerateKey 根据配置生成唯一键（MD5 哈希）。
// 对应 Python: ConnectorPoolConfig.generate_key()
func (c TransportConfig) GenerateKey() string {
	// 按字段名排序拼接
	type kv struct {
		k string
		v string
	}
	fields := []kv{
		{"disable_keep_alives", fmt.Sprintf("%v", c.DisableKeepAlives)},
		{"idle_conn_timeout", c.IdleConnTimeout.String()},
		{"max_conns_per_host", fmt.Sprintf("%d", c.MaxConnsPerHost)},
		{"max_idle_conns", fmt.Sprintf("%d", c.MaxIdleConns)},
		{"max_idle_conns_per_host", fmt.Sprintf("%d", c.MaxIdleConnsPerHost)},
		{"max_idle_time", c.MaxIdleTime.String()},
		{"ssl_verify", fmt.Sprintf("%v", c.SSLVerify)},
		{"tls_handshake_timeout", c.TLSHandshakeTimeout.String()},
		{"ttl", c.TTL.String()},
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].k < fields[j].k
	})

	keyStr := ""
	for i, f := range fields {
		if i > 0 {
			keyStr += "&"
		}
		keyStr += f.k + ":" + f.v
	}
	hash := md5.Sum([]byte(keyStr))
	return fmt.Sprintf("%x", hash)
}

// NewRefCountedTransport 创建引用计数的 HTTP Transport。
func NewRefCountedTransport(config TransportConfig) *RefCountedTransport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     config.IdleConnTimeout,
		TLSHandshakeTimeout: config.TLSHandshakeTimeout,
		DisableKeepAlives:   config.DisableKeepAlives,
		ForceAttemptHTTP2:   true,
	}

	if !config.SSLVerify {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	t := &RefCountedTransport{
		config:    config,
		transport: transport,
	}
	t.InitRefCount()
	return t
}

// Transport 返回底层 http.Transport。
func (t *RefCountedTransport) Transport() *http.Transport {
	return t.transport
}

// Close 关闭 Transport。
func (t *RefCountedTransport) Close() error {
	if t.closed.Swap(true) {
		return nil // 已经关闭
	}
	t.transport.CloseIdleConnections()
	return nil
}

// IsExpired 检查 Transport 是否过期。
func (t *RefCountedTransport) IsExpired() bool {
	return t.RefCountedResource.IsExpired(t.config.TTL, t.config.MaxIdleTime)
}

// GetTransportPool 获取全局 TransportPool 单例。
func GetTransportPool() *TransportPool {
	return transportPoolSingleton.Get(func() *TransportPool {
		return &TransportPool{
			transports: make(map[string]*RefCountedTransport),
			maxPool:    100,
		}
	})
}

// Acquire 获取或创建 Transport，返回 *RefCountedTransport。
// 对应 Python: ConnectorPoolManager.get_connector_pool()
func (p *TransportPool) Acquire(config TransportConfig) (*RefCountedTransport, error) {
	key := config.GenerateKey()

	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查已有 pool
	if transport, ok := p.transports[key]; ok {
		if transport.IsClosed() {
			delete(p.transports, key)
		} else {
			transport.IncRef()
			return transport, nil
		}
	}

	// 超限时淘汰
	if len(p.transports) >= p.maxPool {
		p.evictOldest()
	}

	// 创建新的
	transport := NewRefCountedTransport(config)
	p.transports[key] = transport
	return transport, nil
}

// Release 释放 Transport 引用。
// 对应 Python: ConnectorPoolManager.release_connector_pool()
func (p *TransportPool) Release(config TransportConfig) {
	key := config.GenerateKey()

	p.mu.Lock()
	defer p.mu.Unlock()

	transport, ok := p.transports[key]
	if !ok || transport.IsClosed() {
		return
	}

	if transport.DecRef() && transport.IsExpired() {
		delete(p.transports, key)
		_ = transport.Close()
	}
}

// CloseAll 关闭所有 Transport。
// 对应 Python: ConnectorPoolManager.close_all()
func (p *TransportPool) CloseAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, transport := range p.transports {
		_ = transport.Close()
		delete(p.transports, key)
	}
	return nil
}

// Stats 获取连接池统计信息。
// 对应 Python: ConnectorPoolManager.get_stats()
func (p *TransportPool) Stats() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()

	transports := make(map[string]any, len(p.transports))
	for key, transport := range p.transports {
		transports[key] = map[string]any{
			"ref_count":  transport.RefCount(),
			"closed":     transport.IsClosed(),
			"created_at": transport.CreatedAt(),
			"last_used":  transport.LastUsed(),
		}
	}

	return map[string]any{
		"total_transports": len(p.transports),
		"max_pools":        p.maxPool,
		"transports":       transports,
	}
}

// NewResourcePool 创建泛型资源池。
func NewResourcePool[T any](maxPool int, factory func(key string, config any) (*T, error), keyFunc func(config any) string) *ResourcePool[T] {
	return &ResourcePool[T]{
		resources: make(map[string]*refCountedEntry[T]),
		maxPool:   maxPool,
		factory:   factory,
		keyFunc:   keyFunc,
	}
}

// Acquire 获取或创建资源。
// 对应 Python: BaseRefResourceMgr.acquire()
func (p *ResourcePool[T]) Acquire(config any) (*T, error) {
	key := p.keyFunc(config)

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.resources[key]; ok && !entry.closed {
		entry.refCount++
		entry.lastUsed = time.Now()
		return entry.resource, nil
	}

	// 超限淘汰
	if len(p.resources) >= p.maxPool {
		p.evictOldestGeneric()
	}

	resource, err := p.factory(key, config)
	if err != nil {
		return nil, err
	}

	p.resources[key] = &refCountedEntry[T]{
		resource:  resource,
		refCount:  1,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}
	return resource, nil
}

// Release 释放资源引用。
// 对应 Python: BaseRefResourceMgr.release()
func (p *ResourcePool[T]) Release(config any) {
	key := p.keyFunc(config)

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.resources[key]
	if !ok || entry.closed {
		return
	}

	entry.refCount--
	if entry.refCount <= 0 {
		delete(p.resources, key)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evictOldest 淘汰最久未用的空闲 Transport。
// 对应 Python: _evict_oldest_pool()
func (p *TransportPool) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, transport := range p.transports {
		if transport.RefCount() == 0 && !transport.IsClosed() {
			lastUsed := transport.LastUsed()
			if oldestKey == "" || lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = lastUsed
			}
		}
	}

	if oldestKey != "" {
		transport := p.transports[oldestKey]
		_ = transport.Close()
		delete(p.transports, oldestKey)
	}
}

// evictOldestGeneric 淘汰最久未用的空闲资源。
func (p *ResourcePool[T]) evictOldestGeneric() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range p.resources {
		if entry.refCount == 0 && !entry.closed {
			if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastUsed
			}
		}
	}

	if oldestKey != "" {
		delete(p.resources, oldestKey)
	}
}
