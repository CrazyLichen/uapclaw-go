package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLTeamDao_CreateTeam(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Team()

	ok := dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt", "autonomous", false)
	assert.True(t, ok)

	// Python: IntegrityError → False
	ok = dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt", "autonomous", false)
	assert.False(t, ok)
}

func TestSQLTeamDao_GetTeam(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt", "autonomous", false)

	team, err := dao.GetTeam(context.Background(), "team1")
	require.NoError(t, err)
	require.NotNil(t, team)
	assert.Equal(t, "Team One", team.DisplayName)

	// Python: Optional[Team] → nil
	team2, err := dao.GetTeam(context.Background(), "nonexist")
	assert.Nil(t, team2)
	assert.NoError(t, err)
}

func TestSQLTeamDao_TeamExists(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Team()

	assert.False(t, dao.TeamExists(context.Background(), "team1"))
	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "", "autonomous", false)
	assert.True(t, dao.TeamExists(context.Background(), "team1"))
}

func TestSQLTeamDao_DeleteTeam(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "", "autonomous", false)
	assert.True(t, dao.DeleteTeam(context.Background(), "team1"))
	assert.False(t, dao.TeamExists(context.Background(), "team1"))
}

func TestSQLTeamDao_GetTeamUpdatedAt(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "", "autonomous", false)
	ts := dao.GetTeamUpdatedAt(context.Background(), "team1")
	assert.True(t, ts > 0)

	ts2 := dao.GetTeamUpdatedAt(context.Background(), "nonexist")
	assert.Equal(t, int64(0), ts2)
}

// TestCreateTeam_写入能力列 验证 dispatch_mode 与 enable_task_verification 落库。
// Python: TeamDao.create_team (team_dao.py L25-60)
func TestCreateTeam_写入能力列(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := context.Background()
	ok := db.Team().CreateTeam(ctx, "t1", "T1", "leader1", "desc", "", "scheduled", true)
	require.True(t, ok, "CreateTeam 应成功")

	team, err := db.Team().GetTeam(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, team, "团队行应存在")
	assert.Equal(t, "scheduled", team.DispatchMode, "DispatchMode 应为 scheduled")
	assert.True(t, team.EnableTaskVerification, "EnableTaskVerification 应为 true")
}

// TestCreateTeam_默认能力列 验证零值写入。
func TestCreateTeam_默认能力列(t *testing.T) {
	db := newTestSqlDB(t)
	ok := db.Team().CreateTeam(context.Background(), "t2", "T2", "leader2", "desc", "", "autonomous", false)
	require.True(t, ok, "CreateTeam 应成功")

	team, err := db.Team().GetTeam(context.Background(), "t2")
	require.NoError(t, err)
	require.NotNil(t, team)
	assert.Equal(t, "autonomous", team.DispatchMode)
	assert.False(t, team.EnableTaskVerification)
}

// TestEnsureTeamInfoCapabilityColumns_幂等 验证重复调用不报错、不重复加列。
func TestEnsureTeamInfoCapabilityColumns_幂等(t *testing.T) {
	db := newTestSqlDB(t)
	ensureTeamInfoCapabilityColumns(db.db)
	ensureTeamInfoCapabilityColumns(db.db)

	cols, err := db.db.Migrator().ColumnTypes(&Team{})
	require.NoError(t, err)
	n := 0
	for _, c := range cols {
		if c.Name() == "dispatch_mode" {
			n++
		}
	}
	assert.Equal(t, 1, n, "dispatch_mode 列应恰好 1 个")
}
