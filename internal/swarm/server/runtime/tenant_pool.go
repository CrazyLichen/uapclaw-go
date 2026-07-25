// tenant_pool.go 实现 TenantAgentPool（10.3.14），AgentManager 管理器（单例）。
//
// 对齐 Python: jiuwenswarm/server/runtime/tenant_agent_pool.py
// 职责：
// 1. 管理 AgentManager 实例的创建和生命周期
// 2. 提供统一的函数调用接口
// 3. 调用 AgentManager 的方法（简单分发）

package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TenantAgentPool AgentManager 管理器（单例）。
// 对齐 Python: TenantAgentPool
//
// 职责：
// 1. 管理 AgentManager 实例的创建和生命周期
// 2. 提供统一的函数调用接口
// 3. 调用 AgentManager 的方法（简单分发）
type TenantAgentPool struct {
	// agentManager 单个 AgentManager 实例
	// 对齐 Python: self._agent_manager = AgentManager()
	agentManager *AgentManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// tapLogComponent 日志组件
const tapLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// tenantAgentPoolSingleton 全局 TenantAgentPool 单例持有器。
// 对齐 Python: TenantAgentPool._instance
var tenantAgentPoolSingleton utils.Singleton[TenantAgentPool]

// ──────────────────────────── 导出函数 ────────────────────────────

// GetInstance 获取 TenantAgentPool 单例实例。
// 对齐 Python: TenantAgentPool.get_instance()
func GetInstance() *TenantAgentPool {
	return tenantAgentPoolSingleton.Get(func() *TenantAgentPool {
		logger.Info(tapLogComponent).Msg("[TenantAgentPool] Created singleton instance")
		return &TenantAgentPool{
			agentManager: NewAgentManager(),
		}
	})
}

// ResetInstance 重置单例（仅用于测试）。
// 对齐 Python: TenantAgentPool.reset_instance()
func ResetInstance() {
	tenantAgentPoolSingleton.Reset()
}

// ProcessMessage 处理非流式请求（简单分发到 AgentManager）。
// 对齐 Python: TenantAgentPool.process_message(request)
func (p *TenantAgentPool) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	logger.Info(tapLogComponent).
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Msg("[TenantAgentPool] process_message called")

	resp, err := p.agentManager.ProcessMessage(ctx, request)
	if err != nil {
		logger.Error(tapLogComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Msg("[TenantAgentPool] Error in process_message")
		return nil, err
	}
	return resp, nil
}

// ProcessMessageStream 处理流式请求（简单分发到 AgentManager）。
// 对齐 Python: TenantAgentPool.process_message_stream(request)
func (p *TenantAgentPool) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	logger.Info(tapLogComponent).
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Msg("[TenantAgentPool] process_message_stream called")

	ch, err := p.agentManager.ProcessMessageStream(ctx, request)
	if err != nil {
		logger.Error(tapLogComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Msg("[TenantAgentPool] Error in process_message_stream")
		return nil, err
	}
	return ch, nil
}

// Cleanup 清理资源。
// 对齐 Python: TenantAgentPool.cleanup()
// 同时实现 utils.resettable 接口，供 Singleton.Reset 时自动调用。
func (p *TenantAgentPool) Cleanup() error {
	logger.Info(tapLogComponent).Msg("[TenantAgentPool] Cleaning up...")
	err := p.agentManager.Cleanup()
	if err != nil {
		logger.Error(tapLogComponent).Err(err).Msg("[TenantAgentPool] Cleanup failed")
		return err
	}
	logger.Info(tapLogComponent).Msg("[TenantAgentPool] Cleanup complete")
	return nil
}
