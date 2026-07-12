package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	web "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// configReloadTimeout 配置重载请求超时时间
const configReloadTimeout = 10 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// OnConfigSaved 返回配置保存回调，供 WebHandler 注册。
// 对齐 Python: WebHandlersBindParams(on_config_saved=_on_config_saved)。
func (s *GatewayServer) OnConfigSaved() web.OnConfigSavedFunc {
	return s.onConfigSavedImpl
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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
		// 对齐 Python：ValidationError 是配置格式问题，不需要重启 gateway
		// 检查错误消息中是否包含 ValidationError / validation error / Field required
		errStr := ""
		if errMsg, ok := errPayload["error"]; ok {
			errStr = fmt.Sprintf("%v", errMsg)
		}
		if isValidationError(errStr) {
			logger.Warn(logComponentAppGateway).
				Str("error", errStr).
				Msg("agent.reload_config validation error (non-fatal)")
			return nil
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

// isValidationError 检查错误字符串是否为 ValidationError 类型。
// 对齐 Python：any(kw in err_str for kw in ("ValidationError", "validation error", "Field required"))。
func isValidationError(errStr string) bool {
	return strings.Contains(errStr, "ValidationError") ||
		strings.Contains(errStr, "validation error") ||
		strings.Contains(errStr, "Field required")
}
