package web

import (
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// webDeliveryRegistry HTTP 请求级 outbound 注册表（非 WebSocket 连接表），
// 用于 Agent 回复路由。
// Python: gateway/channel_manager/web/web_delivery_registry.py:15-122
// WebDeliveryRegistry（一比一；B3 前置件，随 27-A B2 回收，见 spec 风险 1）。
type webDeliveryRegistry struct {
	mu                      sync.RWMutex
	requestOutbounds        map[string]*httpOutboundBase
	sessionRequestOutbounds map[string]map[string]bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newWebDeliveryRegistry 创建空注册表。
// Python: web_delivery_registry.py:19-23 __init__。
func newWebDeliveryRegistry() *webDeliveryRegistry {
	return &webDeliveryRegistry{
		requestOutbounds:        map[string]*httpOutboundBase{},
		sessionRequestOutbounds: map[string]map[string]bool{},
	}
}

// register 注册请求级 HTTP outbound（不 register_ws），返回 outbound id。
// Python: web_delivery_registry.py:32-48 register。
func (r *webDeliveryRegistry) register(outbound *httpOutboundBase) string {
	oid := outbound.outboundID
	if oid == "" {
		oid = uuid.NewString()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestOutbounds[oid] = outbound
	sid := outbound.sessionID
	if sid != "" {
		bucket, ok := r.sessionRequestOutbounds[sid]
		if !ok {
			bucket = map[string]bool{}
			r.sessionRequestOutbounds[sid] = bucket
		}
		bucket[oid] = true
	}
	return oid
}

// unregister 从路由表移除请求 outbound 并关闭它。
// Python: web_delivery_registry.py:50-72 unregister。
func (r *webDeliveryRegistry) unregister(outbound *httpOutboundBase) {
	oid := outbound.outboundID
	sid := outbound.sessionID
	r.mu.Lock()
	if oid != "" {
		delete(r.requestOutbounds, oid)
	}
	if sid != "" && oid != "" {
		if bucket, ok := r.sessionRequestOutbounds[sid]; ok {
			delete(bucket, oid)
			if len(bucket) == 0 {
				delete(r.sessionRequestOutbounds, sid)
			}
		}
	}
	r.mu.Unlock()
	outbound.closeBase()
}

// clear 关闭并清空全部 HTTP 请求 outbound（channel 停止 / shutdown）。
// Python: web_delivery_registry.py:74-95 clear。
func (r *webDeliveryRegistry) clear() {
	r.mu.Lock()
	outs := make([]*httpOutboundBase, 0, len(r.requestOutbounds))
	for _, out := range r.requestOutbounds {
		outs = append(outs, out)
	}
	r.requestOutbounds = map[string]*httpOutboundBase{}
	r.sessionRequestOutbounds = map[string]map[string]bool{}
	r.mu.Unlock()
	for _, out := range outs {
		out.closeBase()
	}
}

// lookup 按 id 返回存活 HTTP outbound，否则 nil。
// Python: web_delivery_registry.py:97-110 lookup。
func (r *webDeliveryRegistry) lookup(outboundID string) *httpOutboundBase {
	oid := trimWSID(outboundID)
	if oid == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out, ok := r.requestOutbounds[oid]
	if !ok {
		return nil
	}
	if out.isClosed() {
		r.pruneLocked(oid, out)
		return nil
	}
	return out
}

// pruneLocked 移除已关闭 outbound（调用方须持写锁）。
// Python: web_delivery_registry.py:112-126 _prune_outbound。
func (r *webDeliveryRegistry) pruneLocked(oid string, outbound *httpOutboundBase) {
	delete(r.requestOutbounds, oid)
	sid := outbound.sessionID
	if sid == "" {
		return
	}
	if bucket, ok := r.sessionRequestOutbounds[sid]; ok {
		delete(bucket, oid)
		if len(bucket) == 0 {
			delete(r.sessionRequestOutbounds, sid)
		}
	}
}

// peersForSession 返回绑定到 session_id 的存活 HTTP outbound 集合
// （WS 客户端由传输层另行加入）。
// Python: web_delivery_registry.py:128-141 peers_for_session。
func (r *webDeliveryRegistry) peersForSession(sessionID string) []*httpOutboundBase {
	sid := trimWSID(sessionID)
	peers := []*httpOutboundBase{}
	if sid == "" {
		return peers
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for oid := range r.sessionRequestOutbounds[sid] {
		out, ok := r.requestOutbounds[oid]
		if !ok {
			continue
		}
		if out.isClosed() {
			r.pruneLocked(oid, out)
			continue
		}
		peers = append(peers, out)
	}
	return peers
}

// trimWSID 去空白（对齐 Python str(x or "").strip()）。
func trimWSID(value string) string {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
		value = value[1:]
	}
	for len(value) > 0 && (value[len(value)-1] == ' ' || value[len(value)-1] == '\t') {
		value = value[:len(value)-1]
	}
	return value
}

// logDeliveryClearFailed 停止时清理失败的调试日志（对齐 Python clear 的 except 分支）。
func logDeliveryClearFailed(err error) {
	logger.Debug(logComponent).Err(err).Msg("[WebDelivery] outbound clear close failed")
}
