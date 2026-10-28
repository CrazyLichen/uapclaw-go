package wswrite

import (
	"sync"

	"github.com/gorilla/websocket"
)

// ──────────────────────────── 结构体 ────────────────────────────

// registry 连接写锁注册表。
type registry struct {
	// mu 保护 conns 表
	mu sync.RWMutex
	// conns conn → 串行化写锁
	conns map[*websocket.Conn]*sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultRegistry 进程级默认注册表（gateway 与 web 连接体系共用）。
var defaultRegistry = &registry{
	conns: make(map[*websocket.Conn]*sync.Mutex),
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Register 登记连接，分配写锁。
func Register(conn *websocket.Conn) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.conns[conn] = &sync.Mutex{}
}

// Unregister 移除连接写锁。
func Unregister(conn *websocket.Conn) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	delete(defaultRegistry.conns, conn)
}

// Write 持连接写锁写 TextMessage（未注册的连接退化为直接写）。
func Write(conn *websocket.Conn, data []byte) error {
	if mu := lookup(conn); mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// Lock 写帧辅助：持连接写锁执行 fn（未注册的连接退化为直接执行）。
// 供需要自定义错误处理的写点复用同一把连接写锁。
func Lock(conn *websocket.Conn, fn func()) {
	if mu := lookup(conn); mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	fn()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lookup 查询连接写锁（未注册返回 nil）。
func lookup(conn *websocket.Conn) *sync.Mutex {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	return defaultRegistry.conns[conn]
}
