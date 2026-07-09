package server

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleHarnessPackagesGet 处理 harness.packages.get 请求。stub：返回空列表。
func (s *AgentServer) handleHarnessPackagesGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"packages": []any{},
		}),
	), nil
}

// handleHarnessPackagesScan 处理 harness.packages.scan 请求。stub：返回空列表。
func (s *AgentServer) handleHarnessPackagesScan(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"packages": []any{},
		}),
	), nil
}

// handleHarnessPackagesActivate 处理 harness.packages.activate 请求。stub：返回 ok=true。
func (s *AgentServer) handleHarnessPackagesActivate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleHarnessPackagesDeactivate 处理 harness.packages.deactivate 请求。stub：返回 ok=true。
func (s *AgentServer) handleHarnessPackagesDeactivate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleHarnessPackagesDelete 处理 harness.packages.delete 请求。stub：返回 ok=true。
func (s *AgentServer) handleHarnessPackagesDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleScheduleCheckConfig 处理 schedule.check_config 请求。stub：返回空配置。
func (s *AgentServer) handleScheduleCheckConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"config": map[string]any{},
		}),
	), nil
}

// handleScheduleUpdateConfig 处理 schedule.update_config 请求。stub：返回 ok=true。
func (s *AgentServer) handleScheduleUpdateConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleScheduleCreate 处理 schedule.create 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleScheduleCreate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleScheduleRun 处理 schedule.run 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleScheduleRun(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleScheduleList 处理 schedule.list 请求。stub：返回空列表。
func (s *AgentServer) handleScheduleList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"tasks": []any{},
		}),
	), nil
}

// handleScheduleStatus 处理 schedule.status 请求。stub：返回空状态。
func (s *AgentServer) handleScheduleStatus(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"status": "",
		}),
	), nil
}

// handleScheduleLogs 处理 schedule.logs 请求。stub：返回空列表。
func (s *AgentServer) handleScheduleLogs(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"logs": []any{},
		}),
	), nil
}

// handleScheduleCancel 处理 schedule.cancel 请求。stub：返回 ok=true。
func (s *AgentServer) handleScheduleCancel(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleScheduleDelete 处理 schedule.delete 请求。stub：返回 ok=true。
func (s *AgentServer) handleScheduleDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}
