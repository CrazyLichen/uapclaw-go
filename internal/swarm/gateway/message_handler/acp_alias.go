package message_handler

// ACP 会话别名链（11.25 Task 6，⑦ 入站侧）。
// Python: message_handler.py L1237-1331——ensure/resolveInternal；
// resolveExternal 在 dispatch.go prepareAgentDispatchMessage 与 outbound.go 出站点共同使用。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureAcpAgentSession 经 session.create RPC 换取内部会话。
// Python: _ensure_acp_agent_session (L1237-1259)。
// 返回 ("", err) = ensure 失败（Python raise RuntimeError）。
func (mh *MessageHandler) ensureAcpAgentSession(ctx context.Context, sessionID string) (string, error) {
	requestID := fmt.Sprintf("acp-session-create-%x-%s", time.Now().UnixMilli(), randHex6())
	env := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldChannelID(acpChannelIDConst),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodSessionCreate)),
		e2a.WithFieldParams(map[string]any{"session_id": sessionID}),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixNano())/1e9),
	)
	resp, err := mh.agentClient.SendRequest(ctx, env)
	if err != nil {
		return "", fmt.Errorf("acp session.create failed: %w", err)
	}
	if !resp.OK {
		payload := resp.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		return "", fmt.Errorf("acp session.create failed: %v", payload["error"])
	}
	payload := resp.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	resolved := strVal(payload["sessionId"])
	if resolved == "" {
		resolved = strVal(payload["session_id"])
	}
	if resolved == "" {
		resolved = sessionID
	}
	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return "", fmt.Errorf("acp session.create returned empty session_id")
	}
	return resolved, nil
}

// resolveAcpInternalSessionID 外部会话 → 内部会话（双检锁 + 缓存）。
// Python: _resolve_acp_internal_session_id (L1261-1285)。
// ensure 失败 → Error 日志 + ("", false) 原样放行（design §4.4 拍板，
// Python raise 语义的 Go 映射：不阻断消息转发）。
func (mh *MessageHandler) resolveAcpInternalSessionID(ctx context.Context, externalSessionID string) (string, bool) {
	external := strings.TrimSpace(externalSessionID)
	if external == "" {
		return "", false
	}

	mh.acpSessionAliasMu.Lock()
	cached := mh.acpSessionAliases[external]
	mh.acpSessionAliasMu.Unlock()
	if cached != "" {
		return cached, cached != external
	}

	mh.acpSessionAliasMu.Lock()
	defer mh.acpSessionAliasMu.Unlock()
	// 双检（Python L1274-1276：async with lock 内再查一次）
	if cached = mh.acpSessionAliases[external]; cached != "" {
		return cached, cached != external
	}

	desired := external
	if !isKnownJiuwenswarmSessionID(external) {
		desired = GenerateChannelSessionID(acpChannelIDConst)
	}
	// ctx 透传：调用方（forwardLoop 的消息 ctx）取消时 session.create 可随之中断，
	// 避免连接已断仍阻塞在 RPC 上（Python asyncio 任务取消语义的 Go 等价）。
	ensured, err := mh.ensureAcpAgentSession(ctx, desired)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", external).
			Msg("acp session alias ensure failed, passing message through")
		return "", false
	}
	mh.acpSessionAliases[external] = ensured
	return ensured, ensured != external
}

// resolveAcpExternalSessionID 内部会话 → 外部会话（出站侧）。
// Python: _resolve_acp_external_session_id (L1313-1331)：
// metadata 键优先 → 别名反查 → 原样。
func (mh *MessageHandler) resolveAcpExternalSessionID(sessionID string, metadata map[string]any) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}
	original := ""
	if metadata != nil {
		original = strings.TrimSpace(strVal(metadata[acpOriginalSessionIDKey]))
	}
	if original != "" {
		return original
	}
	// 读锁：Python 出站反解不加锁（单线程无需）；Go 用 RLock 避免被在途
	// session.create 的写锁阻塞（该 RPC 可能耗时数秒）。
	mh.acpSessionAliasMu.RLock()
	defer mh.acpSessionAliasMu.RUnlock()
	for external, internal := range mh.acpSessionAliases {
		if internal == sid {
			return external
		}
	}
	return sid
}

// randHex6 生成 6 位 hex（secrets.token_hex(3) 等价）。
func randHex6() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
