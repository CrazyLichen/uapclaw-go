package utils

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── RefCountedResource 测试 ────────────────────────────

func TestRefCountedResource_Basic(t *testing.T) {
	var r RefCountedResource
	r.InitRefCount()

	if r.RefCount() != 1 {
		t.Fatalf("RefCount() = %d, want 1", r.RefCount())
	}
	if r.IsClosed() {
		t.Fatal("IsClosed() should be false initially")
	}

	newCount := r.IncRef()
	if newCount != 2 {
		t.Fatalf("IncRef() = %d, want 2", newCount)
	}

	reachedZero := r.DecRef()
	if reachedZero {
		t.Fatal("DecRef() should return false when count is still > 0")
	}

	reachedZero = r.DecRef()
	if !reachedZero {
		t.Fatal("DecRef() should return true when count reaches 0")
	}
}

func TestRefCountedResource_Closed(t *testing.T) {
	var r RefCountedResource
	r.InitRefCount()

	r.MarkClosed()
	if !r.IsClosed() {
		t.Fatal("IsClosed() should be true after MarkClosed()")
	}

	// IncRef on closed resource should return 0
	count := r.IncRef()
	if count != 0 {
		t.Fatalf("IncRef() on closed = %d, want 0", count)
	}

	// DecRef on closed resource should return false
	if r.DecRef() {
		t.Fatal("DecRef() on closed should return false")
	}
}

func TestRefCountedResource_Timestamps(t *testing.T) {
	var r RefCountedResource
	r.InitRefCount()

	createdAt := r.CreatedAt()
	if createdAt.IsZero() {
		t.Fatal("CreatedAt() should not be zero")
	}

	lastUsed := r.LastUsed()
	if lastUsed.IsZero() {
		t.Fatal("LastUsed() should not be zero")
	}

	// IncRef should update lastUsed
	time.Sleep(10 * time.Millisecond)
	r.IncRef()
	newLastUsed := r.LastUsed()
	if !newLastUsed.After(lastUsed) {
		t.Fatal("IncRef() should update LastUsed()")
	}
}

func TestRefCountedResource_IsExpired(t *testing.T) {
	var r RefCountedResource
	r.InitRefCount()

	// 未过期
	if r.IsExpired(1*time.Hour, 1*time.Hour) {
		t.Fatal("IsExpired() should be false for fresh resource")
	}

	// TTL 过期（模拟：createdAt 在过去 2 小时）
	r.createdAt = time.Now().Add(-2 * time.Hour)
	if !r.IsExpired(1*time.Hour, 0) {
		t.Fatal("IsExpired() should be true when TTL exceeded")
	}

	// MaxIdle 过期（模拟：lastUsed 在过去 10 分钟）
	r.InitRefCount()
	r.lastUsed.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	if !r.IsExpired(0, 5*time.Minute) {
		t.Fatal("IsExpired() should be true when MaxIdleTime exceeded")
	}
}

// ──────────────────────────── TransportConfig 测试 ────────────────────────────

func TestTransportConfig_Default(t *testing.T) {
	cfg := DefaultTransportConfig()
	if cfg.MaxIdleConns != 100 {
		t.Fatalf("MaxIdleConns = %d, want 100", cfg.MaxIdleConns)
	}
	if cfg.MaxIdleConnsPerHost != 30 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 30", cfg.MaxIdleConnsPerHost)
	}
	if cfg.SSLVerify != true {
		t.Fatal("SSLVerify should be true by default")
	}
}

func TestTransportConfig_GenerateKey(t *testing.T) {
	cfg1 := DefaultTransportConfig()
	cfg2 := DefaultTransportConfig()

	key1 := cfg1.GenerateKey()
	key2 := cfg2.GenerateKey()

	if key1 != key2 {
		t.Fatal("same config should generate same key")
	}
	if len(key1) != 32 {
		t.Fatalf("MD5 key length = %d, want 32", len(key1))
	}
}

func TestTransportConfig_DifferentKeys(t *testing.T) {
	cfg1 := DefaultTransportConfig()
	cfg2 := DefaultTransportConfig()
	cfg2.MaxIdleConns = 200

	key1 := cfg1.GenerateKey()
	key2 := cfg2.GenerateKey()

	if key1 == key2 {
		t.Fatal("different configs should generate different keys")
	}
}

// ──────────────────────────── RefCountedTransport 测试 ────────────────────────────

func TestRefCountedTransport_Create(t *testing.T) {
	cfg := DefaultTransportConfig()
	transport := NewRefCountedTransport(cfg)

	if transport.Transport() == nil {
		t.Fatal("Transport() should not be nil")
	}
	if transport.RefCount() != 1 {
		t.Fatalf("RefCount() = %d, want 1", transport.RefCount())
	}
	if transport.IsClosed() {
		t.Fatal("IsClosed() should be false")
	}
}

func TestRefCountedTransport_Close(t *testing.T) {
	cfg := DefaultTransportConfig()
	transport := NewRefCountedTransport(cfg)

	err := transport.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !transport.IsClosed() {
		t.Fatal("IsClosed() should be true after Close()")
	}

	// 重复关闭不应报错
	err = transport.Close()
	if err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRefCountedTransport_NoSSLVerify(t *testing.T) {
	cfg := DefaultTransportConfig()
	cfg.SSLVerify = false
	transport := NewRefCountedTransport(cfg)

	if transport.Transport().TLSClientConfig == nil {
		t.Fatal("TLSClientConfig should not be nil when SSLVerify is false")
	}
	if !transport.Transport().TLSClientConfig.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify should be true when SSLVerify is false")
	}
}

// ──────────────────────────── TransportPool 测试 ────────────────────────────

func TestTransportPool_Acquire(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	transport, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if transport == nil {
		t.Fatal("Acquire() should return non-nil transport")
	}
	if transport.RefCount() != 1 {
		t.Fatalf("RefCount() = %d, want 1", transport.RefCount())
	}
}

func TestTransportPool_AcquireSameConfig(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	transport1, _ := pool.Acquire(cfg)
	transport2, _ := pool.Acquire(cfg)

	// 相同配置应返回同一实例
	if transport1 != transport2 {
		t.Fatal("same config should return same transport instance")
	}
	if transport1.RefCount() != 2 {
		t.Fatalf("RefCount() = %d, want 2", transport1.RefCount())
	}
}

func TestTransportPool_Release(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	transport, _ := pool.Acquire(cfg)
	if transport.RefCount() != 1 {
		t.Fatalf("RefCount() = %d, want 1 after Acquire", transport.RefCount())
	}

	// 再获取一次
	_, _ = pool.Acquire(cfg)
	if transport.RefCount() != 2 {
		t.Fatalf("RefCount() = %d, want 2 after second Acquire", transport.RefCount())
	}

	// 释放一次
	pool.Release(cfg)
	if transport.RefCount() != 1 {
		t.Fatalf("RefCount() = %d, want 1 after Release", transport.RefCount())
	}
}

func TestTransportPool_CloseAll(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg1 := DefaultTransportConfig()
	cfg2 := DefaultTransportConfig()
	cfg2.MaxIdleConns = 200

	_, _ = pool.Acquire(cfg1)
	_, _ = pool.Acquire(cfg2)

	stats := pool.Stats()
	total := stats["total_transports"].(int)
	if total != 2 {
		t.Fatalf("total_transports = %d, want 2", total)
	}

	_ = pool.CloseAll()

	stats = pool.Stats()
	total = stats["total_transports"].(int)
	if total != 0 {
		t.Fatalf("total_transports = %d after CloseAll, want 0", total)
	}
}

func TestTransportPool_Stats(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	_, _ = pool.Acquire(cfg)

	stats := pool.Stats()
	if stats["total_transports"] != 1 {
		t.Fatalf("total_transports = %v, want 1", stats["total_transports"])
	}
	if stats["max_pools"] != 10 {
		t.Fatalf("max_pools = %v, want 10", stats["max_pools"])
	}
}

// ──────────────────────────── ResourcePool 测试 ────────────────────────────

func TestResourcePool_Acquire(t *testing.T) {
	var createdCount atomic.Int32

	pool := NewResourcePool[string](10,
		func(key string, config any) (*string, error) {
			createdCount.Add(1)
			v := "resource-" + key
			return &v, nil
		},
		func(config any) string {
			return config.(string)
		},
	)

	r1, err := pool.Acquire("key1")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if *r1 != "resource-key1" {
		t.Fatalf("Acquire() = %q, want %q", *r1, "resource-key1")
	}

	r2, _ := pool.Acquire("key1")
	if r1 != r2 {
		t.Fatal("same key should return same resource")
	}
	if createdCount.Load() != 1 {
		t.Fatalf("createdCount = %d, want 1", createdCount.Load())
	}
}

func TestResourcePool_Release(t *testing.T) {
	pool := NewResourcePool[string](10,
		func(key string, config any) (*string, error) {
			v := "resource-" + key
			return &v, nil
		},
		func(config any) string {
			return config.(string)
		},
	)

	_, _ = pool.Acquire("key1")
	_, _ = pool.Acquire("key1") // ref=2
	pool.Release("key1")        // ref=1

	// 仍然可以获取
	r, _ := pool.Acquire("key1")
	if r == nil {
		t.Fatal("Acquire() after Release should still work")
	}
}

// TestRefCountedTransport_IsExpired 测试 Transport 过期检测
func TestRefCountedTransport_IsExpired(t *testing.T) {
	cfg := DefaultTransportConfig()
	transport := NewRefCountedTransport(cfg)

	// 刚创建不应过期
	if transport.IsExpired() {
		t.Error("新创建的 Transport 不应过期")
	}

	// 修改 TTL 使其过期
	transport.config.TTL = 1 * time.Nanosecond
	// 等待过期
	time.Sleep(10 * time.Millisecond)
	if !transport.IsExpired() {
		t.Error("TTL 过期后应返回 true")
	}
}

// TestRefCountedTransport_IsExpired_MaxIdle 测试最大空闲时间过期
func TestRefCountedTransport_IsExpired_MaxIdle(t *testing.T) {
	cfg := DefaultTransportConfig()
	transport := NewRefCountedTransport(cfg)

	// 修改 MaxIdleTime 使其过期
	transport.config.MaxIdleTime = 1 * time.Nanosecond
	transport.config.TTL = 0 // 禁用 TTL
	time.Sleep(10 * time.Millisecond)
	if !transport.IsExpired() {
		t.Error("MaxIdleTime 过期后应返回 true")
	}
}

// TestGetTransportPool 测试全局 TransportPool 单例
func TestGetTransportPool(t *testing.T) {
	pool := GetTransportPool()
	if pool == nil {
		t.Fatal("GetTransportPool() 不应返回 nil")
	}
}

// TestTransportPool_ReleaseExpired 测试释放过期 Transport
func TestTransportPool_ReleaseExpired(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	cfg.TTL = 1 * time.Nanosecond
	_, _ = pool.Acquire(cfg)

	// 等待过期
	time.Sleep(10 * time.Millisecond)

	// 释放，过期时应删除
	pool.Release(cfg)

	// 验证已从池中移除
	stats := pool.Stats()
	if stats["total_transports"] != 0 {
		t.Errorf("过期 Transport 应被移除，total_transports = %v", stats["total_transports"])
	}
}

// TestTransportPool_EvictOldest 测试淘汰最久未用的空闲 Transport
func TestTransportPool_EvictOldest(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    2,
	}

	// 创建两个 transport
	cfg1 := DefaultTransportConfig()
	cfg2 := DefaultTransportConfig()
	cfg2.MaxIdleConns = 200
	_, _ = pool.Acquire(cfg1)
	_, _ = pool.Acquire(cfg2)

	// 释放使其引用计数降为 0
	pool.Release(cfg1)
	pool.Release(cfg2)

	// 超限时应淘汰最久未用的
	cfg3 := DefaultTransportConfig()
	cfg3.MaxIdleConns = 300
	transport, err := pool.Acquire(cfg3)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	if transport == nil {
		t.Fatal("Acquire 应返回 transport")
	}
}

// TestTransportPool_AcquireClosedTransport 测试获取已关闭的 Transport 后创建新的
func TestTransportPool_AcquireClosedTransport(t *testing.T) {
	pool := &TransportPool{
		transports: make(map[string]*RefCountedTransport),
		maxPool:    10,
	}

	cfg := DefaultTransportConfig()
	transport1, _ := pool.Acquire(cfg)
	_ = transport1.Close()

	// 再次获取，因为已关闭，应创建新的
	transport2, _ := pool.Acquire(cfg)
	if transport2.IsClosed() {
		t.Error("新获取的 Transport 不应是已关闭的")
	}
}

// TestResourcePool_ReleaseToZero 测试释放到引用计数为 0 时从池中删除
func TestResourcePool_ReleaseToZero(t *testing.T) {
	pool := NewResourcePool[string](10,
		func(key string, config any) (*string, error) {
			v := "resource-" + key
			return &v, nil
		},
		func(config any) string {
			return config.(string)
		},
	)

	_, _ = pool.Acquire("key1") // ref=1
	pool.Release("key1")        // ref=0，应删除

	// 再次获取应是新实例
	r, _ := pool.Acquire("key1")
	if r == nil {
		t.Fatal("Acquire 应能重新创建资源")
	}
}

// TestRefCountedResource_LastUsed_零值 测试 lastUsed 为 0 时返回 createdAt
func TestRefCountedResource_LastUsed_零值(t *testing.T) {
	var r RefCountedResource
	r.InitRefCount()
	r.lastUsed.Store(0) // 强制设为 0

	lastUsed := r.LastUsed()
	if !lastUsed.Equal(r.CreatedAt()) {
		t.Error("lastUsed 为 0 时应返回 createdAt")
	}
}

// ──────────────────────────── 集成测试 ────────────────────────────

func TestTransportWithHTTPClient(t *testing.T) {
	cfg := DefaultTransportConfig()
	transport := NewRefCountedTransport(cfg)

	client := &http.Client{
		Transport: transport.Transport(),
		Timeout:   5 * time.Second,
	}

	// 验证 client 可用于 HTTP 请求（不实际发送请求，仅验证构造）
	if client.Transport == nil {
		t.Fatal("client.Transport should not be nil")
	}

	_ = transport.Close()
}
