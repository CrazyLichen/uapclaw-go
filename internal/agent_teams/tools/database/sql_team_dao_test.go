package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLTeamDao_CreateTeam(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Team()

	ok := dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt")
	assert.True(t, ok)

	// 对齐 Python: IntegrityError → False
	ok = dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt")
	assert.False(t, ok)
}

func TestSQLTeamDao_GetTeam(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "desc", "prompt")

	team, err := dao.GetTeam(context.Background(), "team1")
	require.NoError(t, err)
	require.NotNil(t, team)
	assert.Equal(t, "Team One", team.DisplayName)

	// 对齐 Python: Optional[Team] → nil
	team2, err := dao.GetTeam(context.Background(), "nonexist")
	assert.Nil(t, team2)
	assert.NoError(t, err)
}

func TestSQLTeamDao_TeamExists(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Team()

	assert.False(t, dao.TeamExists(context.Background(), "team1"))
	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "")
	assert.True(t, dao.TeamExists(context.Background(), "team1"))
}

func TestSQLTeamDao_DeleteTeam(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "")
	assert.True(t, dao.DeleteTeam(context.Background(), "team1"))
	assert.False(t, dao.TeamExists(context.Background(), "team1"))
}

func TestSQLTeamDao_GetTeamUpdatedAt(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Team()

	dao.CreateTeam(context.Background(), "team1", "Team One", "leader1", "", "")
	ts := dao.GetTeamUpdatedAt(context.Background(), "team1")
	assert.True(t, ts > 0)

	ts2 := dao.GetTeamUpdatedAt(context.Background(), "nonexist")
	assert.Equal(t, int64(0), ts2)
}
