package agents

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// kvCacheSessionAdapter 把 SessionFacade 适配为 kv_cache 的窄接口。
//
// 存在原因：kv_cache 包不反向 import session（避免循环依赖），故 LineageSession
// 与 TurnNumSession 里的 GetState 签名是「无 error、key 为 string」；而
// SessionFacade.GetState 返回 (any, error) 且 key 为 state.StateKey。
// 本适配器做这两处转换，语义不变（错误在内部吞掉并返回 nil，对齐 Python
// hook.py:47/55 的 try/except 全吞）。
type kvCacheSessionAdapter struct {
	// sess 被适配的门面会话
	sess sessioninterfaces.SessionFacade
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newKVCacheSessionAdapter 包装门面会话；nil 时返回 nil（kv_cache 侧均容忍 nil）。
// 返回具体类型（而非接口），因为调用方既要把它当 LineageSession（血缘解析）
// 又要当 TurnNumSession（轮次解析）——Python 侧是同一个 session 对象同时满足两用。
func newKVCacheSessionAdapter(sess sessioninterfaces.SessionFacade) *kvCacheSessionAdapter {
	if sess == nil {
		return nil
	}
	return &kvCacheSessionAdapter{sess: sess}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetSessionID 委托门面会话。
func (a *kvCacheSessionAdapter) GetSessionID() string { return a.sess.GetSessionID() }

// GetCacheIdentity 委托门面会话（同类型，无需转换）。
func (a *kvCacheSessionAdapter) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return a.sess.GetCacheIdentity()
}

// GetParentSessionID 委托门面会话。
func (a *kvCacheSessionAdapter) GetParentSessionID() string { return a.sess.GetParentSessionID() }

// GetEnv 委托门面会话。
func (a *kvCacheSessionAdapter) GetEnv(key string, defaultValue ...any) any {
	return a.sess.GetEnv(key, defaultValue...)
}

// GetState 读会话状态，key 从 string 转成 state.StateKey。
// 错误在内部吞掉并返回 nil（对齐 Python hook.py:47 的 try/except）。
func (a *kvCacheSessionAdapter) GetState(key string) any {
	v, err := a.sess.GetState(state.StringKey(key))
	if err != nil {
		return nil
	}
	return v
}

// UpdateState 写会话状态（门面方法无返回值，无需转换）。
func (a *kvCacheSessionAdapter) UpdateState(data map[string]any) {
	a.sess.UpdateState(data)
}
