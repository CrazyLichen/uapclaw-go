package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// modelCommandParams command.model 请求参数
//
// Python: params.get("action"/"target"/"model"/"env_updates")
type modelCommandParams struct {
	// Action 动作：add_model / switch_model / 空
	Action string
	// Target add_model 的目标模型名
	Target string
	// Model switch_model 的目标模型名
	Model string
	// EnvUpdates 需写入进程环境变量的键值（保留 any 以便复刻 Python 的类型报错）
	EnvUpdates map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseModelCommandParams 解析 command.model 参数。
//
// Python: params = request.params or {}；str(params.get(...) or "").strip()
// 偏差 D7：Python str(None) == "None"，Go 的 JSON null 解到 string 为空串，
// 该边界不复刻（无实际调用方会传 null）。
func parseModelCommandParams(raw json.RawMessage) modelCommandParams {
	var p modelCommandParams
	if len(raw) == 0 {
		return p
	}
	var wire struct {
		Action     string         `json:"action"`
		Target     string         `json:"target"`
		Model      string         `json:"model"`
		EnvUpdates map[string]any `json:"env_updates"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("command.model failed to parse params")
		return p
	}
	return modelCommandParams{
		Action:     wire.Action,
		Target:     wire.Target,
		Model:      wire.Model,
		EnvUpdates: wire.EnvUpdates,
	}
}

// modelErrorResponse 构造 ok=false 的 command.model 响应并记录错误日志。
//
// Python: except Exception → logger.exception + ok=False, {"error": str(e)}
func modelErrorResponse(request *schema.AgentRequest, msg string) *schema.AgentResponse {
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "command.model").
		Str("request_id", request.RequestID).
		Msgf("command.model failed: %s", msg)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": msg}))
}

// maskModelEnvForLog 返回掩码 API_KEY 后的副本。
//
// Python: {k: (v if k != "API_KEY" else "***") for k, v in env_updates.items()}
func maskModelEnvForLog(env map[string]any) map[string]any {
	masked := make(map[string]any, len(env))
	for k, v := range env {
		if k == "API_KEY" {
			masked[k] = "***"
			continue
		}
		masked[k] = v
	}
	return masked
}

// stringEnvMap 把已校验为字符串的 env 映射转成 map[string]string。
func stringEnvMap(env map[string]any) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// getenvOr 读取环境变量，未设置时返回缺省值（对齐 Python os.getenv 语义：
// 已设置为空串时返回空串，不返回缺省值）。
func getenvOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// handleCommandAddDir 处理 command.add_dir 请求。写入受信目录配置，返回 ok=true。
func (s *AgentServer) handleCommandAddDir(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 解析 params 获取目录路径
	var params struct {
		Dir string `json:"dir"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("command.add_dir failed to parse params")
		}
	}

	if params.Dir != "" && s.config != nil {
		// 将目录添加到 trusted_directories 配置
		trustedDirs := s.config.Get("trusted_directories")
		var dirs []string
		if arr, ok := trustedDirs.([]any); ok {
			for _, d := range arr {
				if s, ok := d.(string); ok {
					dirs = append(dirs, s)
				}
			}
		}
		dirs = append(dirs, params.Dir)
		if err := s.config.Set("trusted_directories", dirs); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Str("dir", params.Dir).
				Msg("failed to write trusted dirs config")
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{
					"error": map[string]any{
						"code":    "CONFIG_WRITE_ERROR",
						"message": "写入受信目录配置失败",
					},
				}),
			), nil
		}
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleCommandChrome 处理 command.chrome 请求。空操作，返回 ok=true。
func (s *AgentServer) handleCommandChrome(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleCommandCompact 处理 command.compact 请求。
// Python: _handle_command_compact() → agent.compress_context(session_id, return_state=True)
func (s *AgentServer) handleCommandCompact(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := "default"
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	mode, subMode := applyResolvedModeToRequest(request)
	if mode == "auto_harness" {
		mode = "agent"
	}
	projectDir := resolveRequestProjectDir(request)

	agent, err := s.agentManager.GetAgent(ctx, request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandCompact: failed to get Agent")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"error": err.Error()}),
		), err
	}

	resultData, err := agent.CompressContext(ctx, sessionID)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"error": err.Error()}),
		), err
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(resultData),
	), nil
}

// handleCommandContext 处理 command.context 请求。
// Python: _handle_command_context() → agent.get_context_usage(session_id)
func (s *AgentServer) handleCommandContext(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := "default"
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	mode, subMode := applyResolvedModeToRequest(request)
	if mode == "auto_harness" {
		mode = "agent"
	}
	projectDir := resolveRequestProjectDir(request)

	agent, err := s.agentManager.GetAgent(ctx, request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandContext: failed to get Agent")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"error": err.Error()}),
		), err
	}

	resultData, err := agent.GetContextUsage(ctx, sessionID)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"error": err.Error()}),
		), err
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(resultData),
	), nil
}

// handleCommandRecap 处理 command.recap 请求。
// Python: _handle_command_recap() → agent.generate_recap(session_id)
func (s *AgentServer) handleCommandRecap(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := "default"
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	mode, subMode := applyResolvedModeToRequest(request)
	if mode == "auto_harness" {
		mode = "agent"
	}
	projectDir := resolveRequestProjectDir(request)

	agent, err := s.agentManager.GetAgent(ctx, request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandRecap: failed to get Agent")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"status": "failed", "error": err.Error()}),
		), err
	}

	resultData, err := agent.GenerateRecap(ctx, sessionID)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{"status": "failed", "error": err.Error()}),
		), err
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(resultData),
	), nil
}

// handleCommandDiff 处理 command.diff 请求。
// Python: /diff 命令，调用 DiffService.GetTurnDiffs 获取 turn diff 列表。
func (s *AgentServer) handleCommandDiff(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 解析参数：session_id（必填），project_dir（可选）
	var params struct {
		SessionID  string `json:"session_id"`
		ProjectDir string `json:"project_dir"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("command.diff failed to parse params")
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithPayload(map[string]any{
					"diffs": []any{},
					"error": fmt.Sprintf("参数解析失败: %v", err),
				}),
			), nil
		}
	}

	if params.SessionID == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"diffs": []any{},
				"error": "session_id is required",
			}),
		), nil
	}

	// 调用 DiffService 获取 turn diffs
	ds := utils.GetDiffService()
	turnDiffs := ds.GetTurnDiffs(params.SessionID, params.ProjectDir)

	// 转换为前端可用的格式
	diffs := make([]any, len(turnDiffs))
	for i, td := range turnDiffs {
		diffs[i] = td
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"diffs": diffs,
		}),
	), nil
}

// handleCommandModel 处理 command.model 请求。
//
// Python: _handle_command_model (L2277-2352) 三分支：
//
//	add_model    → {"type":"model_added","name":target}
//	switch_model → 写 os.environ + 清内存配置缓存 + 重载 agent 配置
//	其它/缺省    → {"current":MODEL_NAME 或 "unknown","available":["default-model"]}
//
// 失败一律以 ok=false + payload.error 返回，**不返回 error**：返回 error 会被
// handle_envelope.go 转成框架错误帧，与 Python 的 ok=false 响应不一致。
func (s *AgentServer) handleCommandModel(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseModelCommandParams(request.Params)

	switch params.Action {
	case "add_model":
		target := strings.TrimSpace(params.Target)
		logger.Info(logComponent).
			Str("event_type", "command_model_add").
			Str("request_id", request.RequestID).
			Str("target", target).
			Msg("command.model add_model")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{"type": "model_added", "name": target})), nil

	case "switch_model":
		target := strings.TrimSpace(params.Model)
		// Python 先掩码 API_KEY 再打日志
		logger.Info(logComponent).
			Str("event_type", "command_model_switch").
			Str("request_id", request.RequestID).
			Str("target", target).
			Any("env_updates", maskModelEnvForLog(params.EnvUpdates)).
			Msg("command.model switch_model")

		if len(params.EnvUpdates) == 0 {
			// Python: ok=False, {"error": "No env_updates provided"}（不记 error 日志）
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{"error": "No env_updates provided"})), nil
		}

		for k, v := range params.EnvUpdates {
			strVal, isStr := v.(string)
			if !isStr {
				// Python: os.environ[k] = v 对非 str 抛 TypeError，被外层 except 捕获
				return modelErrorResponse(request, fmt.Sprintf("str expected, not %T", v)), nil
			}
			if err := os.Setenv(k, strVal); err != nil {
				return modelErrorResponse(request, err.Error()), nil
			}
		}
		logger.Info(logComponent).
			Str("event_type", "command_model_env_updated").
			Str("model_name", getenvOr("MODEL_NAME", "unknown")).
			Msg("command.model os.environ updated")

		// Python: clear_config_cache()（失败仅 debug）
		commmem.ClearConfigCache()
		logger.Info(logComponent).
			Str("event_type", "command_model_cache_cleared").
			Msg("command.model config cache cleared")

		// Python: await reload_agents_config(None, env_updates)（失败仅 debug）
		if s.agentManager != nil {
			if err := s.agentManager.ReloadAgentsConfig(ctx, nil, stringEnvMap(params.EnvUpdates)); err != nil {
				logger.Debug(logComponent).
					Err(err).
					Msg("command.model reload_agents_config skipped")
			} else {
				logger.Info(logComponent).
					Str("event_type", "command_model_agents_reloaded").
					Msg("command.model agent config reloaded")
			}
		}

		current := getenvOr("MODEL_NAME", "unknown")
		logger.Info(logComponent).
			Str("event_type", "command_model_switched").
			Str("current", current).
			Msg("command.model switch completed")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"current":   current,
				"requested": target,
				"type":      "switched",
				"applied":   true,
			})), nil

	default:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"current":   getenvOr("MODEL_NAME", "unknown"),
				"available": []any{"default-model"},
			})), nil
	}
}

// handleCommandMCP 处理 command.mcp 请求。stub：返回空服务器列表。
func (s *AgentServer) handleCommandMCP(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"servers": []any{},
		}),
	), nil
}

// handleCommandSandbox 处理 command.sandbox 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleCommandSandbox(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleCommandResume 处理 command.resume 请求。stub：返回 ok=true。
func (s *AgentServer) handleCommandResume(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleCommandSession 处理 command.session 请求。stub：返回空 URL。
func (s *AgentServer) handleCommandSession(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"url": "",
		}),
	), nil
}

// handleCommandStatus 处理 command.status 请求。返回版本、配置路径、模型信息等诊断信息。
//
// Python: _handle_command_status。
func (s *AgentServer) handleCommandStatus(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	status := map[string]any{
		"version":       version.Version,
		"project_name":  version.ProjectName,
		"build_info":    version.BuildInfo(),
		"config_path":   "",
		"config_dir":    pathutil.ConfigDir(),
		"workspace_dir": pathutil.WorkspaceDir(),
		"model_name":    "",
		"initialized":   pathutil.IsInitialized(),
	}

	// 从配置读取模型名称
	if s.config != nil {
		status["config_path"] = s.config.Path()
		if modelName := s.config.Get("model.name"); modelName != nil {
			status["model_name"] = fmt.Sprintf("%v", modelName)
		}
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(status),
	), nil
}
