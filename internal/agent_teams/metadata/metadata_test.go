package metadata_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSessionFacade 用于测试的内存 SessionFacade 实现
type fakeSessionFacade struct {
	data map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeSessionFacade() *fakeSessionFacade {
	return &fakeSessionFacade{data: make(map[string]any)}
}

func (f *fakeSessionFacade) GetSessionID() string            { return "test-session" }
func (f *fakeSessionFacade) UpdateState(data map[string]any) { f.data = data }
func (f *fakeSessionFacade) GetState(key state.StateKey) (any, error) {
	return f.data[key.String()], nil
}
func (f *fakeSessionFacade) DumpState() map[string]any { return f.data }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}

// 编译时检查
var _ interfaces.SessionFacade = (*fakeSessionFacade)(nil)

// ──────────────────────────── 测试 ────────────────────────────

// TestReadTeamsBucket_空命名空间 测试空命名空间返回空 map
// Python: read_teams_bucket(session) 空时返回 {}
func TestReadTeamsBucket_空命名空间(t *testing.T) {
	sess := newFakeSessionFacade()
	result := metadata.ReadTeamsBucket(sess)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestReadTeamNamespace_存在 测试读取已存在的 team 桶
// Python: read_team_namespace(session, team_name) 存在时返回 dict
func TestReadTeamNamespace_存在(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		metadata.TeamsKey: map[string]any{
			"team1": map[string]any{"spec": "data", "db_state": "created"},
		},
	})
	result := metadata.ReadTeamNamespace(sess, "team1")
	assert.NotNil(t, result)
	assert.Equal(t, "data", result["spec"])
}

// TestReadTeamNamespace_不存在 测试读取不存在的 team 桶返回 nil
// Python: read_team_namespace(session, team_name) 不存在时返回 None
func TestReadTeamNamespace_不存在(t *testing.T) {
	sess := newFakeSessionFacade()
	result := metadata.ReadTeamNamespace(sess, "no_team")
	assert.Nil(t, result)
}

// TestReadTeamNamesInSession_正常 测试列出 session 中的 team 名
// Python: read_team_names_in_session(session) -> list[str]
func TestReadTeamNamesInSession_正常(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		metadata.TeamsKey: map[string]any{
			"team1": map[string]any{},
			"team2": map[string]any{},
		},
	})
	names := metadata.ReadTeamNamesInSession(sess)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "team1")
	assert.Contains(t, names, "team2")
}

// TestReadTeamDBState_正常 测试读取 db_state 字段
// Python: read_team_db_state(session, team_name) -> str | None
func TestReadTeamDBState_正常(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		metadata.TeamsKey: map[string]any{
			"team1": map[string]any{metadata.TeamDBStateKey: "created"},
		},
	})
	result := metadata.ReadTeamDBState(sess, "team1")
	assert.Equal(t, "created", result)
}

// TestReadTeamDBState_不存在 测试 db_state 不存在时返回空字符串
// Python: read_team_db_state(session, team_name) 不存在时返回 None
func TestReadTeamDBState_不存在(t *testing.T) {
	sess := newFakeSessionFacade()
	result := metadata.ReadTeamDBState(sess, "no_team")
	assert.Equal(t, "", result)
}

// TestWriteTeamNamespace_覆盖 测试整体覆盖 team 桶
// Python: write_team_namespace(session, team_name, payload) 全量覆写
func TestWriteTeamNamespace_覆盖(t *testing.T) {
	sess := newFakeSessionFacade()
	metadata.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	result := metadata.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v1", result["spec"])

	metadata.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v2"})
	result = metadata.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v2", result["spec"])
}

// TestMergeTeamNamespace_浅合并不覆盖无关key 测试浅合并保留其他 key
// Python: merge_team_namespace(session, team_name, partial) 浅合并
func TestMergeTeamNamespace_浅合并不覆盖无关key(t *testing.T) {
	sess := newFakeSessionFacade()
	metadata.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1", "context": "c1"})
	metadata.MergeTeamNamespace(sess, "team1", map[string]any{"spec": "v2"})
	result := metadata.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v2", result["spec"])
	assert.Equal(t, "c1", result["context"])
}

// TestMergeTeamNamespace_桶不存在时创建 测试合并到不存在的桶
// Python: merge_team_namespace(session, team_name, partial) 桶不存在时自动创建
func TestMergeTeamNamespace_桶不存在时创建(t *testing.T) {
	sess := newFakeSessionFacade()
	metadata.MergeTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	result := metadata.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v1", result["spec"])
}

// TestMergeTeamDBState_写入后读取 测试 db_state 写入和读取
// Python: merge_team_db_state(session, team_name, state) 写入后 read_team_db_state 读取
func TestMergeTeamDBState_写入后读取(t *testing.T) {
	sess := newFakeSessionFacade()
	metadata.MergeTeamDBState(sess, "team1", "created")
	result := metadata.ReadTeamDBState(sess, "team1")
	assert.Equal(t, "created", result)
}

// TestRemoveTeamNamespace_存在时删除 测试删除已存在的桶
// Python: remove_team_namespace(session, team_name) -> bool
func TestRemoveTeamNamespace_存在时删除(t *testing.T) {
	sess := newFakeSessionFacade()
	metadata.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	removed := metadata.RemoveTeamNamespace(sess, "team1")
	assert.True(t, removed)
	assert.Nil(t, metadata.ReadTeamNamespace(sess, "team1"))
}

// TestRemoveTeamNamespace_不存在返回false 测试删除不存在的桶返回 false
// Python: remove_team_namespace(session, team_name) 不存在时返回 False
func TestRemoveTeamNamespace_不存在返回false(t *testing.T) {
	sess := newFakeSessionFacade()
	removed := metadata.RemoveTeamNamespace(sess, "no_team")
	assert.False(t, removed)
}
