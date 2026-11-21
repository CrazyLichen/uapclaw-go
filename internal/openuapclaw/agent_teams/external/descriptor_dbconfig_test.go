package external

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// TestDescriptorDBConfigRoundTrip db_config 接口字段经 ToJSON→FromJSON 往返
// 后按 db_type 分派回具体类型（此前 json 装不回接口字段，跨进程描述符必炸
// ——uapclawmcpserver 经 TeamJoinDescriptorFromEnv 解析必失败）。
func TestDescriptorDBConfigRoundTrip(t *testing.T) {
	d := &TeamJoinDescriptor{
		SessionID: "s1", TeamName: "t1", MemberName: "m1",
		DBConfig: database.DatabaseConfig{DBType: database.DatabaseTypeSQLite,
			ConnectionString: "/tmp/team.db", DBTimeout: 30, DBEnableWAL: true},
	}
	back, err := TeamJoinDescriptorFromJSON(d.ToJSON())
	require.NoError(t, err)
	require.NotNil(t, back.DBConfig)
	assert.Equal(t, database.DatabaseTypeSQLite, back.DBConfig.GetDBType())
	assert.Equal(t, "/tmp/team.db", back.DBConfig.GetConnectionString())
}

// TestDescriptorDBConfigMemory分派 memory 型分派到 MemoryDatabaseConfig。
func TestDescriptorDBConfigMemory分派(t *testing.T) {
	d := &TeamJoinDescriptor{
		SessionID: "s1", TeamName: "t1", MemberName: "m1",
		DBConfig: database.MemoryDatabaseConfig{DBType: database.DatabaseTypeMemory},
	}
	back, err := TeamJoinDescriptorFromJSON(d.ToJSON())
	require.NoError(t, err)
	require.NotNil(t, back.DBConfig)
	assert.Equal(t, database.DatabaseTypeMemory, back.DBConfig.GetDBType())
}

// TestDescriptorDBConfig无dbConfig db_config 缺失时字段为 nil（不报错）。
func TestDescriptorDBConfig无dbConfig(t *testing.T) {
	back, err := TeamJoinDescriptorFromJSON(
		`{"session_id":"s1","team_name":"t1","member_name":"m1"}`)
	require.NoError(t, err)
	assert.Nil(t, back.DBConfig)
}

// TestDescriptorDBConfig未知dbType 未知 db_type 报错（对齐 pydantic union
// 无匹配报 ValidationError）。
func TestDescriptorDBConfig未知dbType(t *testing.T) {
	_, err := TeamJoinDescriptorFromJSON(
		`{"session_id":"s1","team_name":"t1","member_name":"m1","db_config":{"db_type":"oracle"}}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown db_type")
}
