package message_handler

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 消息处理器
// 入站：Channel → MessageHandler → Transport → AgentServer
// 出站：AgentServer → Transport → MessageHandler → Channel
//
// 本次为骨架实现，后续完善转发链路。
type MessageHandler struct {
	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageHandler 创建消息处理器
func NewMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

// HandleInbound 处理入站消息（用户→Agent）
func (mh *MessageHandler) HandleInbound(_ context.Context, _ *schema.Message) error {
	logger.Debug(logComponent).Msg("HandleInbound: 骨架实现，暂不转发")
	return nil
}

// StartOutboundLoop 启动出站消息循环（Agent→用户）
func (mh *MessageHandler) StartOutboundLoop(_ context.Context) error {
	logger.Debug(logComponent).Msg("StartOutboundLoop: 骨架实现，暂不启动")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
