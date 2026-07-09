package runtime

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentManager Agent 实例管理器（stub，10.3.12）。
//
// 管理 UapClaw 实例的创建、获取和配置重载。
// 当前为 stub 实现，后续替换为完整逻辑。
// 对齐 Python AgentManager：jiuwenswarm/server/runtime/agent_manager.py
type AgentManager struct {
	// stubAgent 默认 stub Agent 实例
	stubAgent *UapClaw
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentManager 创建 AgentManager stub 实例。
func NewAgentManager() *AgentManager {
	return &AgentManager{
		stubAgent: NewUapClaw(),
	}
}

// GetAgent 获取 Agent 实例，不存在则自动创建 stub 实例。
// 对齐 Python AgentManager.get_agent：异步，自动创建。
func (am *AgentManager) GetAgent(channelID, mode, projectDir, subMode string) (*UapClaw, error) {
	return am.stubAgent, nil
}

// GetAgentNoWait 获取已有 Agent 实例，不自动创建。
// 对齐 Python AgentManager.get_agent_nowait：同步，不创建。
// 找不到时返回 nil。
func (am *AgentManager) GetAgentNoWait(channelID, mode, projectDir, subMode string) *UapClaw {
	return am.stubAgent
}

// ReloadAgentsConfig 重载 Agent 配置。
// 对齐 Python AgentManager.reload_agents_config (agent_manager.py L308-340)。
// 当前实现：env 注入 os.environ；agent reload 和 team evolution 标记 TODO。
func (am *AgentManager) ReloadAgentsConfig(configPayload map[string]any, envOverrides map[string]any) error {
	// 1. env 注入 os.environ（对齐 Python: for env_key, env_value in env_overrides.items(): os.environ[key] = str(env_value)）
	for key, val := range envOverrides {
		s, ok := val.(string)
		if !ok && val != nil {
			s = fmt.Sprintf("%v", val)
		}
		if val == nil || s == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, s)
		}
	}

	// TODO(⤵️ agent reload): 遍历所有 agent 调用 reload_agent_config
	// 对齐 Python: for channel_id, agents in self.agents.items():
	//     for _, agent in agents.items():
	//         await agent.reload_agent_config(config_base=config, env_overrides=env)

	// TODO(⤵️ team evolution): 更新 team evolution config
	// 对齐 Python: team_manager.update_evolution_config(team_config)

	return nil
}

// CreateSession 创建会话。
// 对齐 Python AgentManager.create_session(channel_id, session_id) -> str。
// stub：创建会话目录 + metadata.json。
func (am *AgentManager) CreateSession(channelID, sessionID string) (string, error) {
	if sessionID == "" {
		sessionID = makeSessionID()
	}
	// 会话目录：~/.uapclaw/agent/sessions/{sessionID}
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".uapclaw", "agent", "sessions")
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return sessionID, err
	}
	// 写入 metadata.json
	metadataPath := filepath.Join(sessionDir, "metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		metadata := `{"session_id": "` + sessionID + `", "created_at": "` + time.Now().Format(time.RFC3339) + `"}`
		_ = os.WriteFile(metadataPath, []byte(metadata), 0o644)
	}
	return sessionID, nil
}

// Initialize 初始化 AgentManager。
// 对齐 Python AgentManager.initialize(channel_id, extra_config) -> dict|None。
// stub：返回默认 capabilities。
func (am *AgentManager) Initialize(channelID string, extraConfig map[string]any) (map[string]any, error) {
	return map[string]any{
		"capabilities": map[string]any{},
	}, nil
}

// RecreateAgent 重建 Agent 实例。
// 对齐 Python AgentManager.recreate_agent。
// stub：返回空列表。
func (am *AgentManager) RecreateAgent(channelID string, immediate bool) []string {
	return nil
}

// CancelAllInflightWork 取消所有在途任务。
// 对齐 Python AgentManager.cancel_all_inflight_work。
// stub：直接返回 nil。
func (am *AgentManager) CancelAllInflightWork(_ context.Context) error {
	return nil
}

// Cleanup 清理资源。
// 对齐 Python AgentManager.cleanup。
// stub：直接返回 nil。
func (am *AgentManager) Cleanup() error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeSessionID 生成会话 ID。
// 格式：sess_{hex_timestamp}_{6_random_hex}，对齐 Python _make_session_id。
func makeSessionID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("sess_%s_%x", ts, suffix)
}
