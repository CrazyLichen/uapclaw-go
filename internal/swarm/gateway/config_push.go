package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// configReloadTimeout 配置重载请求超时时间
const configReloadTimeout = 10 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// OnConfigSavedFunc 配置保存回调函数类型。
// 对齐 Python: _on_config_saved(updated_env_keys, env_updates=..., config_payload=...)。
//
// 参数说明：
//   - updatedKeys: 变更的环境变量键集合
//   - envUpdates: 增量环境变量更新
//   - configPayload: 完整配置快照
type OnConfigSavedFunc func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error

// PushInitialConfig 推送初始配置给 AgentServer。
// 在 GatewayServer.Start() 中，AgentClient 连接成功后调用。
// 对齐 Python: app_gateway.py L879-882 set_or_update_server_config + agent.reload_config。
func (s *GatewayServer) PushInitialConfig(ctx context.Context) error {
	return s.pushConfigToAgentServer(ctx)
}

// OnConfigSaved 返回配置保存回调，供 WebHandler 注册。
// 对齐 Python: WebHandlersBindParams(on_config_saved=_on_config_saved)。
func (s *GatewayServer) OnConfigSaved() OnConfigSavedFunc {
	return s.onConfigSavedImpl
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pushConfigToAgentServer 构造 agent.reload_config E2A 请求并发送给 AgentServer。
// 对齐 Python: _on_config_saved → client.send_request(reload_env)。
// 配置始终通过 E2A 请求传递，AgentServer 不共享 Config 指针。
func (s *GatewayServer) pushConfigToAgentServer(ctx context.Context) error {
	configData, _ := s.config.Raw()
	if configData == nil {
		configData = make(map[string]any)
	}

	requestID := "config-reload-" + uuid.New().String()[:8]
	envelope := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldReqMethod(string(schema.ReqMethodAgentReloadConfig)),
		e2a.WithFieldParams(map[string]any{
			"config": configData,
			"env":    BuildEnvMap(),
		}),
	)

	resp, err := s.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		return fmt.Errorf("发送 agent.reload_config 失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("AgentServer 拒绝配置重载: %v", resp.Payload)
	}
	return nil
}

// onConfigSavedImpl 配置保存回调实现。
// 对齐 Python: _on_config_saved (app_gateway.py L919-989)。
//
// 执行步骤：
//  1. 本地缓存更新（当前空操作，接口预留）
//  2. 构造 agent.reload_config E2A 请求
//  3. 检查 reload 响应
//  4. 条件发 browser.runtime_restart
//  5. 异常兜底（当前仅 warn，后续补充 _schedule_restart）
func (s *GatewayServer) onConfigSavedImpl(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error {
	// 步骤1：本地缓存更新（对齐 Python: set_or_update_server_config）
	// 当前 WebSocket client 是空操作，接口预留
	// TODO(⤵️ 扩展系统): 扩展 client 实现本地缓存

	// 步骤2：构造 agent.reload_config E2A 请求
	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), configReloadTimeout)
	defer reloadCancel()

	if configPayload == nil {
		configPayload = make(map[string]any)
	}
	if envUpdates == nil {
		envUpdates = make(map[string]any)
	}

	requestID := "agent-reload-" + uuid.New().String()[:8]
	envelope := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldReqMethod(string(schema.ReqMethodAgentReloadConfig)),
		e2a.WithFieldParams(map[string]any{
			"config": configPayload,
			"env":    envUpdates,
		}),
	)

	resp, err := s.agentClient.SendRequest(reloadCtx, envelope)
	if err != nil {
		// 步骤5（异常兜底）
		logger.Warn(logComponentAppGateway).
			Err(err).
			Msg("agent.reload_config 发送失败")
		return err
	}

	// 步骤3：检查 reload 响应
	if !resp.OK {
		errPayload := make(map[string]any)
		if resp.Payload != nil {
			errPayload = resp.Payload
		}
		logger.Warn(logComponentAppGateway).
			Bool("ok", resp.OK).
			Interface("payload", errPayload).
			Msg("agent.reload_config 响应非 OK")
		return fmt.Errorf("AgentServer 拒绝配置重载: %v", errPayload)
	}

	logger.Info(logComponentAppGateway).Msg("配置热重载成功，已通知 AgentServer")

	// 步骤4：条件发 browser.runtime_restart（对齐 Python browser_runtime_keys）
	if ShouldBrowserRestart(updatedKeys) {
		restartID := "browser-restart-" + uuid.New().String()[:8]
		restartEnvelope := e2a.E2AFromAgentFields(restartID,
			e2a.WithFieldReqMethod(string(schema.ReqMethodBrowserRuntimeRestart)),
		)
		if _, err := s.agentClient.SendRequest(reloadCtx, restartEnvelope); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Msg("browser.runtime_restart 发送失败")
		} else {
			logger.Info(logComponentAppGateway).Msg("browser.runtime_restart 已发送")
		}
	}

	return nil
}
