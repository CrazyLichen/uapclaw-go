package tracker

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// newTestBackend 建一个指向 test-team 的 TeamBackend + in-memory 库。
//
// 不建 team 行 / 不建成员行——各用例按需自己建，以便验证「行不存在时不宣告」
// 这条门（leader 在 build_team 之前的状态）。
func newTestBackend(t *testing.T) (*tools.TeamBackend, *database.InMemoryTeamDatabase) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	if err := db.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tb := tools.NewTeamBackend("test-team", "alice", false, db,
		messager.NewInProcessMessager(atschema.MessagerTransportConfig{}))
	t.Cleanup(func() { _ = db.Close() })
	return tb, db
}

// createPeer 在库里建一个同伴成员行。
//
// CreateMember 自带 UpdatedAt = 当前毫秒时间戳，因此每建一个同伴，
// GetMembersMaxUpdatedAt 都会推进——roster 的 delta 分支正是靠这个触发。
func createPeer(t *testing.T, db *database.InMemoryTeamDatabase, teamName, memberName, displayName string) {
	t.Helper()
	ok := db.Member().CreateMember(
		context.Background(), memberName, teamName, displayName, "{}",
		"ready", "teammate", "", "", "build_mode", "", "",
	)
	if !ok {
		t.Fatalf("createPeer(%s) 失败", memberName)
	}
}
