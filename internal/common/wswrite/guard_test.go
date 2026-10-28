package wswrite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// dialTestWS 起本地 WS 服务并返回客户端连接。
func dialTestWS(t *testing.T) *websocket.Conn {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestRegisterAndUnregister 注册后可查到锁，注销后查不到。
func TestRegisterAndUnregister(t *testing.T) {
	conn := dialTestWS(t)
	Register(conn)
	if lookup(conn) == nil {
		t.Fatal("Register 后 lookup 应返回非 nil 写锁")
	}
	Unregister(conn)
	if lookup(conn) != nil {
		t.Fatal("Unregister 后 lookup 应返回 nil")
	}
}

// TestLookup_未注册连接 未注册连接返回 nil（退化直写路径）。
func TestLookup_未注册连接(t *testing.T) {
	conn := dialTestWS(t)
	if lookup(conn) != nil {
		t.Fatal("未注册连接 lookup 应返回 nil")
	}
}

// TestWrite_未注册连接直写 未注册连接退化为直接写，消息可达。
func TestWrite_未注册连接直写(t *testing.T) {
	// 服务端回显式：升级后把收到的消息读出来交给测试
	var got sync.Map
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage {
				got.Store("last", data)
			}
		}
	}))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	defer conn.Close()

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	if err := Write(conn, payload); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	// 服务端读循环异步执行，轮询等待消息到达
	deadline := time.Now().Add(3 * time.Second)
	var gotData []byte
	for time.Now().Before(deadline) {
		if v, ok := got.Load("last"); ok {
			gotData = v.([]byte)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gotData == nil {
		t.Fatal("服务端未收到消息")
	}
	if string(gotData) != string(payload) {
		t.Fatalf("消息不一致: %s", gotData)
	}
}

// TestLock_并发写不panic 注册连接上多 goroutine 并发 Lock 写，
// gorilla 的并发写 panic 若未互斥会在此暴露。
func TestLock_并发写不panic(t *testing.T) {
	conn := dialTestWS(t)
	Register(conn)
	defer Unregister(conn)

	payload := []byte(`{"n":0}`)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				Lock(conn, func() {
					if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
						return
					}
				})
			}
		}()
	}
	wg.Wait()
}
