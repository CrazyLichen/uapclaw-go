package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

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
				Msg("command.add_dir 参数解析失败")
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
				Msg("写入受信目录配置失败")
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
// 对齐 Python: _handle_command_compact() → agent.compress_context(session_id, return_state=True)
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

	agent, err := s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandCompact: 获取 Agent 失败")
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
// 对齐 Python: _handle_command_context() → agent.get_context_usage(session_id)
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

	agent, err := s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandContext: 获取 Agent 失败")
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
// 对齐 Python: _handle_command_recap() → agent.generate_recap(session_id)
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

	agent, err := s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("handleCommandRecap: 获取 Agent 失败")
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

// handleCommandDiff 处理 command.diff 请求。stub：返回空差异列表。
func (s *AgentServer) handleCommandDiff(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"diffs": []any{},
		}),
	), nil
}

// handleCommandModel 处理 command.model 请求。stub：从 params 读取 action/model，返回 ok=true。
func (s *AgentServer) handleCommandModel(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		Action string `json:"action"`
		Model  string `json:"model"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("command.model 参数解析失败")
		}
	}

	logger.Debug(logComponent).
		Str("request_id", request.RequestID).
		Str("action", params.Action).
		Str("model", params.Model).
		Msg("command.model 请求")

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
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
// 对齐 Python _handle_command_status。
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
