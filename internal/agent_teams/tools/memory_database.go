package tools

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 接口 ────────────────────────────

// InMemoryTeamDatabase 内存数据库替代实现接口。⤵️ 回填: 9.65a
type InMemoryTeamDatabase interface {
	database.TeamDatabase
}
