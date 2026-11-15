package store

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryStore 内存图状态存储：{ session_id: { ns: state } }
// Python: store/inmemory.py:13-47
type InMemoryStore struct {
	// store 最新图状态：session_id → ns → state
	store map[string]map[string]*pregel.GraphState
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryStore 构造内存存储
// Python: store/inmemory.py:14-16（defaultdict）
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{store: map[string]map[string]*pregel.GraphState{}}
}

// Get 读取状态，返回深拷贝；未命中返回 (nil, nil)（对齐 Python Optional=None）
// Python: store/inmemory.py:18-19
func (s *InMemoryStore) Get(_ context.Context, sessionID, ns string) (*pregel.GraphState, error) {
	return s.store[sessionID][ns].Clone(), nil
}

// Save 保存状态的深拷贝（对齐 Python 双向 deepcopy——存副本防外部篡改）
// Python: store/inmemory.py:22-24
func (s *InMemoryStore) Save(_ context.Context, sessionID, ns string, state *pregel.GraphState) error {
	sub, ok := s.store[sessionID]
	if !ok {
		sub = map[string]*pregel.GraphState{}
		s.store[sessionID] = sub
	}
	sub[ns] = state.Clone()
	return nil
}

// Delete 删除状态：session 不存在静默返回；ns 空串删整个 session；
// 否则前缀匹配删除，session 删空后连带删除外层键。
// Python: store/inmemory.py:27-39
func (s *InMemoryStore) Delete(_ context.Context, sessionID, ns string) error {
	sub, ok := s.store[sessionID]
	if !ok {
		return nil // 会话不存在，无事可删
	}
	if ns == "" {
		delete(s.store, sessionID)
		return nil
	}
	deleteNSByPrefix(sub, ns)
	// session 删空后清理外层键
	if len(sub) == 0 {
		delete(s.store, sessionID)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deleteNSByPrefix 按前缀收集并逐个删除命名空间
// Python: store/inmemory.py:42-47（_delete_ns_by_prefix）
func deleteNSByPrefix(sub map[string]*pregel.GraphState, prefix string) {
	var toDelete []string
	for ns := range sub {
		if strings.HasPrefix(ns, prefix) {
			toDelete = append(toDelete, ns)
		}
	}
	for _, ns := range toDelete {
		delete(sub, ns)
	}
}
