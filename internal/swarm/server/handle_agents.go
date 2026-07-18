package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentsListParams agents.list 请求参数
type agentsListParams struct {
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// agentsGetParams agents.get 请求参数
type agentsGetParams struct {
	// Name agent 名称
	Name string `json:"name"`
}

// agentsCreateParams agents.create 请求参数
type agentsCreateParams struct {
	// Name 名称
	Name string `json:"name"`
	// Description 描述
	Description string `json:"description"`
	// Prompt 系统提示词
	Prompt string `json:"prompt"`
	// Location 存储位置
	Location string `json:"location"`
	// Model 模型名称
	Model string `json:"model,omitempty"`
	// Tools 允许的工具列表
	Tools []string `json:"tools,omitempty"`
	// Color 颜色标识
	Color string `json:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode string `json:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope string `json:"memory_scope,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// WhenToUse 调度描述
	WhenToUse string `json:"when_to_use,omitempty"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty"`
	// Skills 预加载 skill
	Skills []string `json:"skills,omitempty"`
	// Generate 是否使用 LLM 生成 whenToUse 和 prompt
	Generate bool `json:"generate,omitempty"`
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// agentsUpdateParams agents.update 请求参数
type agentsUpdateParams struct {
	// Name agent 名称（必填，标识要更新的 agent）
	Name string `json:"name"`
	// Description 描述
	Description *string `json:"description,omitempty"`
	// WhenToUse 调度描述
	WhenToUse *string `json:"when_to_use,omitempty"`
	// Prompt 系统提示词
	Prompt *string `json:"prompt,omitempty"`
	// Model 模型名称
	Model *string `json:"model,omitempty"`
	// Tools 允许的工具列表
	Tools []string `json:"tools,omitempty"`
	// Color 颜色标识
	Color *string `json:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode *string `json:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope *string `json:"memory_scope,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty"`
	// Skills 预加载 skill
	Skills []string `json:"skills,omitempty"`
	// Generate 是否使用 LLM 生成 whenToUse 和 prompt
	Generate bool `json:"generate,omitempty"`
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// agentsDeleteParams agents.delete 请求参数
type agentsDeleteParams struct {
	// Name agent 名称
	Name string `json:"name"`
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// agentsEnableParams agents.enable 请求参数
type agentsEnableParams struct {
	// Name agent 名称
	Name string `json:"name"`
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// agentsDisableParams agents.disable 请求参数
type agentsDisableParams struct {
	// Name agent 名称
	Name string `json:"name"`
	// WorkspaceDir 工作空间目录（可选覆盖）
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAgentConfigService 获取 AgentConfigService 实例。
// 对齐 Python: service = AgentConfigService(workspace_dir)
// 如果请求参数中指定了 workspace_dir，则创建临时 service，否则使用 server 级别的 service。
func (s *AgentServer) getAgentConfigService(workspaceDir string) *runtime.AgentConfigService {
	if workspaceDir != "" {
		return runtime.NewAgentConfigService(workspaceDir)
	}
	return s.agentConfigService
}

// handleAgentsList 处理 agents.list 请求。
// 对齐 Python: _handle_agents_list
func (s *AgentServer) handleAgentsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 对齐 Python: service = AgentConfigService(workspace_dir); agents = service.list_agents()
	var params agentsListParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		// 允许空参数
		params = agentsListParams{}
	}

	svc := s.getAgentConfigService(params.WorkspaceDir)
	agents := svc.ListAgents()

	// 转换为 []map[string]any
	agentList := make([]any, 0, len(agents))
	for _, a := range agents {
		agentList = append(agentList, agentDefinitionToMap(a))
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"agents": agentList,
		}),
	), nil
}

// handleAgentsGet 处理 agents.get 请求。
// 对齐 Python: _handle_agents_get
func (s *AgentServer) handleAgentsGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params agentsGetParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "参数解析失败",
			}),
		), nil
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "agent name is required",
			}),
		), nil
	}

	svc := s.getAgentConfigService("")
	agent := svc.GetAgent(name)
	if agent == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("Agent 不存在: %s", name),
			}),
		), nil
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"agent": agentDefinitionToMap(agent),
		}),
	), nil
}

// handleAgentsCreate 处理 agents.create 请求。
// 对齐 Python: _handle_agents_create
func (s *AgentServer) handleAgentsCreate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params agentsCreateParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("参数解析失败: %v", err),
			}),
		), nil
	}

	svc := s.getAgentConfigService(params.WorkspaceDir)

	// 步骤 1: 如果 generate=true，使用 LLM 生成 whenToUse 和 prompt
	// 对齐 Python: if generate: llm_result = await self._generate_agent_with_llm(name, description)
	generated := false
	if params.Generate && params.Name != "" && params.Description != "" {
		llmResult := runtime.GenerateAgentWithLLM(context.Background(), s.resolveModel(), params.Name, params.Description)
		if llmResult != nil {
			params.WhenToUse = llmResult.WhenToUse
			params.Prompt = llmResult.SystemPrompt
			generated = true
		}
	}

	// 步骤 2: 创建 agent
	// 对齐 Python: agent = service.create_agent(p)
	location := runtime.AgentSource(params.Location)
	if location == "" {
		location = runtime.AgentSourceLocal
	}
	createParams := &runtime.CreateAgentParams{
		Name:            params.Name,
		Description:     params.Description,
		Prompt:          params.Prompt,
		Location:        location,
		Model:           params.Model,
		Tools:           params.Tools,
		Color:           params.Color,
		PermissionMode:  params.PermissionMode,
		MemoryScope:     params.MemoryScope,
		DisallowedTools: params.DisallowedTools,
		WhenToUse:       params.WhenToUse,
		MaxIterations:   params.MaxIterations,
		Skills:          params.Skills,
	}

	agent, err := svc.CreateAgent(createParams)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": err.Error(),
			}),
		), nil
	}

	// 步骤 3: 自动在 config.yaml 中启用新创建的 agent
	// 对齐 Python: upsert_subagent_in_config(agent.name, enabled=True)
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(agent.Name, true); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", agent.Name).Msg("agents.create upsert config failed")
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"agent":       agentDefinitionToMap(agent),
			"generated":   generated,
			"applied":     applied,
			"reload_error": reloadError,
		}),
	), nil
}

// handleAgentsUpdate 处理 agents.update 请求。
// 对齐 Python: _handle_agents_update
func (s *AgentServer) handleAgentsUpdate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params agentsUpdateParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("参数解析失败: %v", err),
			}),
		), nil
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "agent name is required",
			}),
		), nil
	}

	svc := s.getAgentConfigService(params.WorkspaceDir)

	// 步骤 1: 如果 generate=true 且有 description，使用 LLM 生成
	generated := false
	if params.Generate && name != "" && params.Description != nil && *params.Description != "" {
		llmResult := runtime.GenerateAgentWithLLM(context.Background(), s.resolveModel(), name, *params.Description)
		if llmResult != nil {
			params.WhenToUse = &llmResult.WhenToUse
			params.Prompt = &llmResult.SystemPrompt
			generated = true
		}
	}

	// 步骤 2: 更新 agent
	updateParams := &runtime.UpdateAgentParams{
		Description:     params.Description,
		WhenToUse:       params.WhenToUse,
		Prompt:          params.Prompt,
		Model:           params.Model,
		Tools:           params.Tools,
		Color:           params.Color,
		PermissionMode:  params.PermissionMode,
		MemoryScope:     params.MemoryScope,
		DisallowedTools: params.DisallowedTools,
		MaxIterations:   params.MaxIterations,
		Skills:          params.Skills,
	}

	agent, err := svc.UpdateAgent(name, updateParams)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": err.Error(),
			}),
		), nil
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"agent":     agentDefinitionToMap(agent),
			"generated": generated,
		}),
	), nil
}

// handleAgentsDelete 处理 agents.delete 请求。
// 对齐 Python: _handle_agents_delete
func (s *AgentServer) handleAgentsDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params agentsDeleteParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("参数解析失败: %v", err),
			}),
		), nil
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "agent name is required",
			}),
		), nil
	}

	svc := s.getAgentConfigService(params.WorkspaceDir)

	// 步骤 1: 删除 agent 文件
	// 对齐 Python: ok = service.delete_agent(name)
	ok, err := svc.DeleteAgent(name)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": err.Error(),
			}),
		), nil
	}

	// 步骤 2: 自动从 config.yaml 中移除被删除的 agent
	// 对齐 Python: remove_subagent_from_config(name)
	applied := true
	reloadError := ""
	if _, rmErr := runtime.RemoveSubagentFromConfig(name); rmErr != nil {
		applied = false
		reloadError = rmErr.Error()
		logger.Warn(logComponent).Err(rmErr).Str("agent_name", name).Msg("agents.delete remove config failed")
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok":           ok,
			"applied":      applied,
			"reload_error": reloadError,
		}),
	), nil
}

// handleAgentsEnable 处理 agents.enable 请求。
// 对齐 Python: _handle_agents_set_enabled(ws, request, send_lock, True)
func (s *AgentServer) handleAgentsEnable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(request, true)
}

// handleAgentsDisable 处理 agents.disable 请求。
// 对齐 Python: _handle_agents_set_enabled(ws, request, send_lock, False)
func (s *AgentServer) handleAgentsDisable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(request, false)
}

// handleAgentsToolsList 处理 agents.tools_list 请求。
// 对齐 Python: _handle_agents_tools_list
func (s *AgentServer) handleAgentsToolsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	svc := s.getAgentConfigService("")
	result := svc.ListAvailableTools()

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(result),
	), nil
}

// handleAgentsSetEnabled 处理 agents.enable/disable 请求。
// 对齐 Python: _handle_agents_set_enabled(ws, request, send_lock, enabled)
func (s *AgentServer) handleAgentsSetEnabled(request *schema.AgentRequest, enabled bool) (*schema.AgentResponse, error) {
	var params agentsEnableParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("参数解析失败: %v", err),
			}),
		), nil
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "agent name is required",
			}),
		), nil
	}

	svc := s.getAgentConfigService(params.WorkspaceDir)

	// 步骤 1: 验证 agent 存在
	// 对齐 Python: agent = service.get_agent(name); if agent is None: raise ValueError(...)
	agent := svc.GetAgent(name)
	if agent == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("Agent 不存在: %s", name),
			}),
		), nil
	}
	if agent.Source == runtime.AgentSourceBuiltin {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("不能启用/禁用内置 agent: %s", name),
			}),
		), nil
	}

	// 步骤 2: 更新 config.yaml 中的 enabled 状态
	// 对齐 Python: upsert_subagent_in_config(name, enabled=enabled)
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(name, enabled); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", name).Bool("enabled", enabled).Msg("agents set enabled failed")
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"name":          name,
			"enabled":       enabled,
			"applied":       applied,
			"reload_error":  reloadError,
		}),
	), nil
}

// resolveModel 获取当前模型实例（用于 LLM 生成）。
// 对齐 Python: self._resolve_model(None)
func (s *AgentServer) resolveModel() *llm.Model {
	// TODO(#agent-config): 从 AgentManager 获取当前模型
	return nil
}

// agentDefinitionToMap 将 AgentDefinition 转换为 map[string]any 用于 JSON 响应。
func agentDefinitionToMap(a *runtime.AgentDefinition) map[string]any {
	m := map[string]any{
		"name":        a.Name,
		"description": a.Description,
		"source":      string(a.Source),
		"tools":       a.Tools,
	}
	if a.Prompt != "" {
		m["prompt"] = a.Prompt
	}
	if a.FilePath != "" {
		m["file_path"] = a.FilePath
	}
	if a.Model != "" {
		m["model"] = a.Model
	}
	if a.Color != "" {
		m["color"] = a.Color
	}
	if a.PermissionMode != "" {
		m["permission_mode"] = a.PermissionMode
	}
	if a.MemoryScope != "" {
		m["memory_scope"] = a.MemoryScope
	}
	if a.ShadowedBy != "" {
		m["shadowed_by"] = string(a.ShadowedBy)
	}
	if a.Enabled != nil {
		m["enabled"] = *a.Enabled
	}
	if a.WhenToUse != "" {
		m["when_to_use"] = a.WhenToUse
	}
	if a.MaxIterations != nil {
		m["max_iterations"] = *a.MaxIterations
	}
	if len(a.DisallowedTools) > 0 {
		m["disallowed_tools"] = a.DisallowedTools
	}
	if len(a.Skills) > 0 {
		m["skills"] = a.Skills
	}
	return m
}
