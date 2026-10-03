package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSessionReleaseInfo 解析后的 session 级团队释放信息。
// Python: TeamSessionReleaseInfo (openjiuwen/agent_teams/runtime/manager.py)
type TeamSessionReleaseInfo struct {
	// TeamNames 该 session 中已持久化的所有团队名
	TeamNames []string
	// DBConfig 第一个可解析的数据库配置
	DBConfig database.DatabaseConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveTeamSessionReleaseInfo 从 session checkpoint 解析团队释放信息。
// Python: TeamRuntimeManager.resolve_team_session_release_info(session_id)
//
// 返回 nil 表示 session 无团队 bucket。
// 返回 error 表示 bucket 存在但无法解析出有效的 db_config。
func ResolveTeamSessionReleaseInfo(sessionID string) (*TeamSessionReleaseInfo, error) {
	if sessionID == "" {
		return nil, nil
	}

	// Python: session = create_agent_team_session(session_id=session_id)
	sess := session.CreateAgentTeamSession(sessionID, nil, "")
	if err := sess.PreRun(context.Background()); err != nil {
		logger.Warn(mgrLogComponent).Str("session_id", sessionID).Err(err).
			Msg("resolve_team_session_release_info: 恢复 session 状态失败")
		return nil, nil
	}

	// Python: teams = read_teams_bucket(session)
	teams := metadata.ReadTeamsBucket(sess)
	if len(teams) == 0 {
		return nil, nil
	}

	// Python: team_names = read_team_names_in_session(session)
	teamNames := metadata.ReadTeamNamesInSession(sess)
	sort.Strings(teamNames)

	var parseErrors []string
	var dbConfig *database.DatabaseConfig

	// Python: 遍历 team bucket，取第一个包含有效 db_config 的 context
	for _, teamName := range teamNames {
		bucket := teams[teamName]
		if bucket == nil {
			parseErrors = append(parseErrors, fmt.Sprintf("team bucket '%s' missing", teamName))
			continue
		}
		contextData, ok := bucket["context"]
		if !ok || contextData == nil {
			parseErrors = append(parseErrors, fmt.Sprintf("team bucket '%s' missing context", teamName))
			continue
		}
		// 从 map[string]any 解析 db_config
		cfg, err := parseDBConfigFromContext(contextData)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("team bucket '%s' context parsing failed: %s", teamName, err))
			continue
		}
		dbConfig = cfg
		break
	}

	if dbConfig == nil {
		details := strings.Join(parseErrors, "; ")
		if details == "" {
			details = "no parseable team bucket found"
		}
		return nil, fmt.Errorf("cannot resolve team session release info for %s: %s", sessionID, details)
	}

	return &TeamSessionReleaseInfo{
		TeamNames: teamNames,
		DBConfig:  *dbConfig,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveAnyTeamSessionReleaseInfo 从多个 sessionID 中取第一个可解析的 release info。
// Python: TeamRuntimeManager._resolve_any_team_session_release_info(session_ids)
func resolveAnyTeamSessionReleaseInfo(sessionIDs []string) *TeamSessionReleaseInfo {
	if len(sessionIDs) == 0 {
		return nil
	}
	for _, sid := range sessionIDs {
		info, err := ResolveTeamSessionReleaseInfo(sid)
		if err != nil {
			logger.Warn(mgrLogComponent).Str("session_id", sid).Err(err).
				Msg("resolve_any: 跳过 session")
			continue
		}
		if info != nil {
			return info
		}
	}
	return nil
}

// parseDBConfigFromContext 从 context 数据中解析 db_config。
// contextData 为 map[string]any 格式（来自 session bucket 的 "context" 字段）。
func parseDBConfigFromContext(contextData any) (*database.DatabaseConfig, error) {
	// 先转为 JSON 再反序列化，确保类型正确
	jsonBytes, err := json.Marshal(contextData)
	if err != nil {
		return nil, fmt.Errorf("marshal context data: %w", err)
	}

	var ctx struct {
		DBConfig *database.DatabaseConfig `json:"db_config"`
	}
	if err := json.Unmarshal(jsonBytes, &ctx); err != nil {
		return nil, fmt.Errorf("unmarshal context: %w", err)
	}
	if ctx.DBConfig == nil {
		return nil, fmt.Errorf("db_config is missing")
	}
	return ctx.DBConfig, nil
}

// dropSessionTablesForSession 为指定 sessionID 释放动态表。
// 从 release_info 获取 DB 实例，调用 DropSessionTablesByID。
func dropSessionTablesForSession(ctx context.Context, sessionID string, dbConfig database.DatabaseConfig) error {
	db := spawn.GetSharedDB(dbConfig)
	if db == nil {
		return fmt.Errorf("GetSharedDB returned nil for session %s", sessionID)
	}
	if err := db.Initialize(ctx); err != nil {
		return fmt.Errorf("db initialize failed for session %s: %w", sessionID, err)
	}
	_, err := db.DropSessionTablesByID(ctx, sessionID)
	return err
}

// removeTeamDir 删除团队文件系统目录。
// Python: shutil.rmtree(team_home(team_name))
// 失败时仅记录警告，不返回 error（对齐 Python 行为）。
func removeTeamDir(teamName string) {
	teamDir := agent_teams.TeamHome(teamName)
	info, err := os.Stat(teamDir)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.RemoveAll(teamDir); err != nil {
		logger.Warn(mgrLogComponent).Str("team_dir", teamDir).Err(err).
			Msg("delete_team: 删除团队目录失败")
	} else {
		logger.Info(mgrLogComponent).Str("team_dir", teamDir).
			Msg("delete_team: 已删除团队目录")
	}
}

// getCheckpointer 获取全局 Checkpointer。
func getCheckpointer() interfaces.Checkpointer {
	return checkpointer.GetCheckpointer()
}

// sessionExists 检查 session 是否存在（通过 Checkpointer）。
func sessionExists(ctx context.Context, sessionID string) bool {
	cp := getCheckpointer()
	if cp == nil {
		return false
	}
	exists, err := cp.SessionExists(ctx, sessionID)
	if err != nil {
		return false
	}
	return exists
}

// releaseCheckpoint 释放指定 session 的 checkpoint。
func releaseCheckpoint(ctx context.Context, sessionID string) error {
	cp := getCheckpointer()
	if cp == nil {
		return nil
	}
	return cp.Release(ctx, sessionID)
}

// deleteTeamDB 从数据库删除 team_info 行（级联删除 members/tasks/messages）。
// Python: db.team.delete_team(team_name)
func deleteTeamDB(ctx context.Context, dbConfig database.DatabaseConfig, teamName string) bool {
	db := spawn.GetSharedDB(dbConfig)
	if db == nil {
		return false
	}
	if err := db.Initialize(ctx); err != nil {
		logger.Error(mgrLogComponent).Err(err).Str("team_name", teamName).
			Msg("deleteTeamDB: 数据库初始化失败")
		return false
	}
	return db.Team().DeleteTeam(ctx, teamName)
}
