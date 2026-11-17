package externalcli

// 外部 CLI 后端会话的尽力清理：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/session_cleanup.py（115 行全文）。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	session "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CleanupExternalCLIBackendSessions 删除外部 CLI 成员的持久化后端原生会话。
// Python: cleanup_external_cli_backend_sessions (session_cleanup.py:20)
func CleanupExternalCLIBackendSessions(
	ctx context.Context,
	sessionID string,
	teamNames []string,
	db database.TeamDatabase,
) {
	session, err := restoreSessionForCleanup(ctx, sessionID)
	if err != nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Err(err).
			Msg("Failed to restore session for external CLI cleanup")
		return
	}
	for _, teamName := range teamNames {
		backendConfigs := externalCLIConfigsForTeam(session, teamName)
		if len(backendConfigs) == 0 {
			continue
		}
		members, err := db.Member().GetTeamMembers(ctx, teamName, "")
		if err != nil {
			logger.Warn(logComponent).Str("team", teamName).Err(err).
				Msg("Failed to list members for external CLI cleanup")
			continue
		}
		for _, member := range members {
			cliAgentPtr := database.GetMemberCLIAgent(member)
			if cliAgentPtr == nil || *cliAgentPtr == "" {
				continue
			}
			cliAgent := *cliAgentPtr
			config, ok := backendConfigs[cliAgent]
			if !ok {
				continue
			}
			cleanupExternalCLIBackendSession(cliAgent, sessionID, member.MemberName, config)
		}
	}
}

// CleanupExternalCLIBackendSession 后端支持时删除一个后端原生会话。
// claude 的删除面（delete_claude_session）归属 15.7 的 claudecode provider——
// 本批未落地，走 Python except 分支的告警形状并在此登记 ⤵️15.7（D-15.13-x
// 关联：claude 分支体随 15.7 回填）。
//
// Python: cleanup_external_cli_backend_session (session_cleanup.py:55)
func CleanupExternalCLIBackendSession(
	cliAgent string,
	teamSessionID string,
	memberName string,
	config *schema.ExternalCliAgentSpec,
) {
	cleanupExternalCLIBackendSession(cliAgent, teamSessionID, memberName, config)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cleanupExternalCLIBackendSession 内部实现（对齐 Python 同名函数逐分支）。
func cleanupExternalCLIBackendSession(
	cliAgent string, teamSessionID string, memberName string, config *schema.ExternalCliAgentSpec,
) {
	if cliAgent != "claude" {
		return
	}
	// Python: delete_claude_session(team_session_id=..., member_name=..., cwd=config.cwd)
	// ⤵️ 15.7：claudecode provider 未落地，删除面不存在；按 Python 的异常分支
	// 形状记 Debug（会话缺席）并返回。
	logger.Debug(logComponent).
		Str("backend", cliAgent).
		Str("session", teamSessionID).
		Str("member", memberName).
		Msg("External CLI backend session cleanup deferred to 15.7 (delete_claude_session)")
}

// externalCLIConfigsForTeam 从团队命名空间读出 spec 并按 cli_agent 索引。
// Python: _external_cli_configs_for_team (session_cleanup.py:87)
func externalCLIConfigsForTeam(
	sess metadataSessionReader, teamName string,
) map[string]*schema.ExternalCliAgentSpec {
	bucket := metadata.ReadTeamNamespace(sess, teamName)
	if bucket == nil {
		return nil
	}
	specData, ok := bucket["spec"]
	if !ok || specData == nil {
		return nil
	}
	spec, err := decodeTeamAgentSpec(specData)
	if err != nil {
		logger.Warn(logComponent).Str("team", teamName).Err(err).
			Msg("Failed to parse team spec for external CLI cleanup")
		return nil
	}
	out := make(map[string]*schema.ExternalCliAgentSpec, len(spec.ExternalCliAgents))
	for i := range spec.ExternalCliAgents {
		// Python 一行推导：{config.cli_agent: config for config in
		// spec.external_cli_agents}（session_cleanup.py:104）——无任何键过滤；
		// Go map 键无冲突问题，含 "/" 或空键照常收录。
		out[spec.ExternalCliAgents[i].CLIAgent] = &spec.ExternalCliAgents[i]
	}
	return out
}

// metadataSessionReader 团队命名空间读取所需的最小会话面。
type metadataSessionReader = sessioninterfaces.SessionFacade

// restoreSessionForCleanup 按会话 id 恢复团队会话（Python
// create_agent_team_session(session_id=..., source_metadata_enabled=False) +
// pre_run；pre_run 失败 → error 返回，调用方记 Warn 并终止清理）。
func restoreSessionForCleanup(ctx context.Context, sessionID string) (sessioninterfaces.SessionFacade, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("empty session id for external CLI cleanup")
	}
	sess := session.NewAgentTeamSession(session.WithAgentTeamSessionID(sessionID))
	if err := sess.PreRun(ctx); err != nil {
		return nil, err
	}
	return sess, nil
}

// decodeTeamAgentSpec 把团队命名空间里的 spec 数据还原为 TeamAgentSpec。
// Python: TeamAgentSpec.model_validate(spec_data)。
func decodeTeamAgentSpec(specData any) (*schema.TeamAgentSpec, error) {
	switch v := specData.(type) {
	case *schema.TeamAgentSpec:
		return v, nil
	case schema.TeamAgentSpec:
		return &v, nil
	case map[string]any:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var spec schema.TeamAgentSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, err
		}
		return &spec, nil
	default:
		return nil, fmt.Errorf("unsupported team spec payload type %T", specData)
	}
}
