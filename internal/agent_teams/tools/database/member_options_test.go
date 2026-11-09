package database

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestLoadMemberOptions_空与非法输入 验证空串、非法 JSON、非对象、类型不符都退回空配置。
func TestLoadMemberOptions_空与非法输入(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"空串", ""},
		{"非法 JSON", "{not json"},
		{"非对象", `"just a string"`},
		{"null", "null"},
		{"字段类型不符", `{"lifetime": {"nested": 1}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := LoadMemberOptions(c.raw)
			assert.Nil(t, opts.ModelRef)
			assert.Nil(t, opts.Lifetime)
			assert.Nil(t, opts.Worktree)
			assert.Empty(t, opts.Extra)
		})
	}
}

// TestDumpMemberOptions_空配置返回空串 对齐上游"空配置返回 None"。
func TestDumpMemberOptions_空配置返回空串(t *testing.T) {
	assert.Equal(t, "", DumpMemberOptions(TeamMemberOptions{}))
}

// TestMemberOptions_往返不丢未知键 未知键必须原样保留（pydantic extra="allow"）。
func TestMemberOptions_往返不丢未知键(t *testing.T) {
	raw := `{"lifetime":"shutdown","future_key":{"a":1},"another":42}`

	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.Lifetime)
	assert.Equal(t, MemberLifetimeShutdown, *opts.Lifetime)
	assert.Equal(t, map[string]any{"a": float64(1)}, opts.Extra["future_key"])
	assert.Equal(t, float64(42), opts.Extra["another"])

	out := DumpMemberOptions(opts)
	var back map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &back))
	assert.Equal(t, "shutdown", back["lifetime"])
	assert.Contains(t, back, "future_key")
	assert.Contains(t, back, "another")
}

// TestMemberAgentTemplate_未知键保留 模板段的 extra="allow" 也要保留。
func TestMemberAgentTemplate_未知键保留(t *testing.T) {
	raw := `{"agent_template":{"ref":{"name":"x"},"version":"1.2","tpl_extra":"keep"}}`

	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.AgentTemplate)
	assert.Equal(t, "1.2", opts.AgentTemplate.Version)

	out := DumpMemberOptions(opts)
	var back map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &back))
	tpl, ok := back["agent_template"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "keep", tpl["tpl_extra"])
	assert.Equal(t, "1.2", tpl["version"])
}

// TestMergeLegacyMemberOptions_options优先 已有 model_ref 时不覆盖。
func TestMergeLegacyMemberOptions_options优先(t *testing.T) {
	options := `{"model_ref":{"model_name":"from-options"}}`
	merged := MergeLegacyMemberOptions(options, map[string]any{"model_name": "from-mapping"}, "")

	opts := LoadMemberOptions(merged)
	require.NotNil(t, opts.ModelRef)
	require.NotNil(t, opts.ModelRef.ModelName)
	assert.Equal(t, "from-options", *opts.ModelRef.ModelName)
}

// TestMergeLegacyMemberOptions_从mapping补齐 options 没写 model_ref 时用 mapping。
func TestMergeLegacyMemberOptions_从mapping补齐(t *testing.T) {
	merged := MergeLegacyMemberOptions("", map[string]any{"model_name": "m1", "model_index": 3}, "")

	opts := LoadMemberOptions(merged)
	require.NotNil(t, opts.ModelRef)
	require.NotNil(t, opts.ModelRef.ModelName)
	assert.Equal(t, "m1", *opts.ModelRef.ModelName)
	require.NotNil(t, opts.ModelRef.ModelIndex)
	assert.Equal(t, 3, *opts.ModelRef.ModelIndex)
}

// TestMergeLegacyMemberOptions_回退到modelRefJSON mapping 为空时读 legacy 列。
func TestMergeLegacyMemberOptions_回退到modelRefJSON(t *testing.T) {
	merged := MergeLegacyMemberOptions("", nil, `{"model_name":"legacy"}`)

	opts := LoadMemberOptions(merged)
	require.NotNil(t, opts.ModelRef)
	require.NotNil(t, opts.ModelRef.ModelName)
	assert.Equal(t, "legacy", *opts.ModelRef.ModelName)
}

// TestMergeLegacyMemberOptions_全空 没有任何来源时返回空串。
func TestMergeLegacyMemberOptions_全空(t *testing.T) {
	assert.Equal(t, "", MergeLegacyMemberOptions("", nil, ""))
	assert.Equal(t, "", MergeLegacyMemberOptions("", nil, "{bad"))
}

// TestBuildMemberOptions_全字段 验证各字段都写进去。
func TestBuildMemberOptions_全字段(t *testing.T) {
	lifetime := MemberLifetimeDormant
	cli := "claude"
	raw := BuildMemberOptions(MemberOptionsInput{
		ModelRef:            map[string]any{"model_name": "m1", "route_tag": "tag"},
		FallbackModelRef:    map[string]any{"model_name": "fb"},
		BuiltinModel:        &MemberBuiltinModel{Model: "builtin", Effort: strPtrOrNil("high")},
		CLIAgent:            &cli,
		Worktree:            &MemberWorktreeOptions{Isolation: strPtrOrNil("worktree")},
		PermissionsOverride: map[string]string{"bash": "deny"},
		Lifetime:            &lifetime,
		AgentTemplate:       &MemberAgentTemplate{Version: "v1"},
	})

	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.ModelRef)
	assert.Equal(t, "m1", *opts.ModelRef.ModelName)
	require.NotNil(t, opts.FallbackModelRef)
	assert.Equal(t, "fb", *opts.FallbackModelRef.ModelName)
	require.NotNil(t, opts.BuiltinModel)
	assert.Equal(t, "builtin", opts.BuiltinModel.Model)
	require.NotNil(t, opts.CLIAgent)
	assert.Equal(t, "claude", *opts.CLIAgent)
	require.NotNil(t, opts.Worktree)
	assert.Equal(t, "worktree", *opts.Worktree.Isolation)
	assert.Equal(t, map[string]string{"bash": "deny"}, opts.PermissionsOverride)
	require.NotNil(t, opts.Lifetime)
	assert.Equal(t, MemberLifetimeDormant, *opts.Lifetime)
	require.NotNil(t, opts.AgentTemplate)
	assert.Equal(t, "v1", opts.AgentTemplate.Version)
}

// TestBuildMemberOptions_平铺worktree worktree 为空时用平铺两字段。
func TestBuildMemberOptions_平铺worktree(t *testing.T) {
	raw := BuildMemberOptions(MemberOptionsInput{
		WorktreeIsolation: "copy",
		WorktreePath:      "/tmp/wt",
	})

	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.Worktree)
	assert.Equal(t, "copy", *opts.Worktree.Isolation)
	assert.Equal(t, "/tmp/wt", *opts.Worktree.Path)
}

// TestBuildMemberOptions_空输入 全空返回空串。
func TestBuildMemberOptions_空输入(t *testing.T) {
	assert.Equal(t, "", BuildMemberOptions(MemberOptionsInput{}))
}

// TestSetMemberWorktreeOptions_三分支 覆盖替换 / 平铺 / 清空。
func TestSetMemberWorktreeOptions_三分支(t *testing.T) {
	t.Run("显式对象替换", func(t *testing.T) {
		raw := SetMemberWorktreeOptions("", &MemberWorktreeOptions{Path: strPtrOrNil("/a")}, "", "")
		opts := LoadMemberOptions(raw)
		require.NotNil(t, opts.Worktree)
		assert.Equal(t, "/a", *opts.Worktree.Path)
	})
	t.Run("平铺字段构造", func(t *testing.T) {
		raw := SetMemberWorktreeOptions("", nil, "worktree", "/b")
		opts := LoadMemberOptions(raw)
		require.NotNil(t, opts.Worktree)
		assert.Equal(t, "worktree", *opts.Worktree.Isolation)
		assert.Equal(t, "/b", *opts.Worktree.Path)
	})
	t.Run("全空清空该段", func(t *testing.T) {
		base := BuildMemberOptions(MemberOptionsInput{
			Worktree: &MemberWorktreeOptions{Path: strPtrOrNil("/a")},
		})
		raw := SetMemberWorktreeOptions(base, nil, "", "")
		opts := LoadMemberOptions(raw)
		assert.Nil(t, opts.Worktree)
	})
}

// TestSetMemberBuiltinModel 设置与清空内置模型段。
func TestSetMemberBuiltinModel(t *testing.T) {
	raw := SetMemberBuiltinModel("", &MemberBuiltinModel{Model: "m"})
	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.BuiltinModel)
	assert.Equal(t, "m", opts.BuiltinModel.Model)

	cleared := SetMemberBuiltinModel(raw, nil)
	assert.Nil(t, LoadMemberOptions(cleared).BuiltinModel)
}

// TestAppendMemberServedModel_同模型不变 与最新一条相同则原样返回。
func TestAppendMemberServedModel_同模型不变(t *testing.T) {
	first := AppendMemberServedModel("", "m1", 100)
	second := AppendMemberServedModel(first, "m1", 200)
	assert.Equal(t, first, second, "同模型不算变更，应原样返回")
}

// TestAppendMemberServedModel_上限5条 历史只保留最新 5 条。
func TestAppendMemberServedModel_上限5条(t *testing.T) {
	raw := ""
	for i := 0; i < 8; i++ {
		raw = AppendMemberServedModel(raw, "m"+itoa(i), int64(i))
	}
	history := LoadMemberOptions(raw).ServedModels
	require.Len(t, history, maxServedModels)
	assert.Equal(t, "m3", history[0].Model)
	assert.Equal(t, "m7", history[len(history)-1].Model)
}

// TestSetMemberPermissionsOverride 替换权限收窄段。
func TestSetMemberPermissionsOverride(t *testing.T) {
	raw := SetMemberPermissionsOverride("", map[string]string{"write_file": "ask"})
	assert.Equal(t, map[string]string{"write_file": "ask"}, LoadMemberOptions(raw).PermissionsOverride)

	cleared := SetMemberPermissionsOverride(raw, nil)
	assert.Nil(t, LoadMemberOptions(cleared).PermissionsOverride)
}

// TestPromoteMemberFallbackModel 兜底提为当前模型，同时清掉内置模型。
func TestPromoteMemberFallbackModel(t *testing.T) {
	base := BuildMemberOptions(MemberOptionsInput{
		FallbackModelRef: map[string]any{"model_name": "fb"},
		BuiltinModel:     &MemberBuiltinModel{Model: "builtin"},
	})

	raw := PromoteMemberFallbackModel(base)
	opts := LoadMemberOptions(raw)
	require.NotNil(t, opts.ModelRef)
	assert.Equal(t, "fb", *opts.ModelRef.ModelName)
	assert.Nil(t, opts.BuiltinModel, "兜底是外部端点，内置模型不再适用")
}

// TestPromoteMemberFallbackModel_无兜底原样返回 没有兜底时不动原串。
func TestPromoteMemberFallbackModel_无兜底原样返回(t *testing.T) {
	raw := `{"lifetime":"shutdown"}`
	assert.Equal(t, raw, PromoteMemberFallbackModel(raw))
}

// TestGetMember_从两种记录形态读取 getter 支持 *TeamMember 与 map。
func TestGetMember_从两种记录形态读取(t *testing.T) {
	lifetime := MemberLifetimeShutdown
	cli := "codex"
	raw := BuildMemberOptions(MemberOptionsInput{
		ModelRef:            map[string]any{"model_name": "m1"},
		FallbackModelRef:    map[string]any{"model_name": "fb"},
		BuiltinModel:        &MemberBuiltinModel{Model: "builtin"},
		CLIAgent:            &cli,
		Worktree:            &MemberWorktreeOptions{Path: strPtrOrNil("/wt")},
		PermissionsOverride: map[string]string{"bash": "deny"},
		Lifetime:            &lifetime,
		AgentTemplate:       &MemberAgentTemplate{Version: "v2"},
	})
	raw = AppendMemberServedModel(raw, "m1", 10)

	records := map[string]any{
		"结构体": &TeamMember{MemberName: "m", Options: raw},
		"map": map[string]any{"options": raw},
	}
	for name, rec := range records {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, GetMemberModelRef(rec))
			assert.Equal(t, "m1", *GetMemberModelRef(rec).ModelName)
			require.NotNil(t, GetMemberFallbackModelRef(rec))
			assert.Equal(t, "fb", *GetMemberFallbackModelRef(rec).ModelName)
			require.NotNil(t, GetMemberBuiltinModel(rec))
			assert.Equal(t, "builtin", GetMemberBuiltinModel(rec).Model)
			require.Len(t, GetMemberServedModels(rec), 1)
			require.NotNil(t, GetMemberCLIAgent(rec))
			assert.Equal(t, "codex", *GetMemberCLIAgent(rec))
			require.NotNil(t, GetMemberWorktree(rec))
			assert.Equal(t, "/wt", *GetMemberWorktree(rec).Path)
			assert.Equal(t, map[string]string{"bash": "deny"}, GetMemberPermissionsOverride(rec))
			require.NotNil(t, GetMemberAgentTemplate(rec))
			assert.Equal(t, "v2", GetMemberAgentTemplate(rec).Version)
			require.NotNil(t, GetMemberLifetime(rec))
			assert.Equal(t, MemberLifetimeShutdown, *GetMemberLifetime(rec))
		})
	}
}

// TestGetMemberOptions_空记录 空记录与 nil 返回空配置。
func TestGetMemberOptions_空记录(t *testing.T) {
	assert.Empty(t, GetMemberOptions(nil).Extra)
	assert.Nil(t, GetMemberModelRef(nil))
	assert.Nil(t, GetMemberLifetime((*TeamMember)(nil)))
	assert.Nil(t, GetMemberLifetime(map[string]any{}))
	assert.Nil(t, GetMemberLifetime(42), "非结构体非 map 的记录应安全返回 nil")
}

// TestMemberLifetime_取值 枚举取值与上游 Literal 逐字一致。
func TestMemberLifetime_取值(t *testing.T) {
	assert.Equal(t, "shutdown", string(MemberLifetimeShutdown))
	assert.Equal(t, "dormant", string(MemberLifetimeDormant))
}

// TestMaxServedModels_取值 常量与上游一致。
func TestMaxServedModels_取值(t *testing.T) {
	assert.Equal(t, 5, maxServedModels)
}

// itoa 测试内的十进制转换，避免为一行引入 strconv 依赖差异。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// TestStrPtrOrNil_空串转nil 空串必须转 nil，否则 omitempty 失效会写出空字段。
func TestStrPtrOrNil_空串转nil(t *testing.T) {
	assert.Nil(t, strPtrOrNil(""))
	got := strPtrOrNil("x")
	require.NotNil(t, got)
	assert.Equal(t, "x", *got)
}

// TestCapitalizeFirst 首字母大写；空串原样返回。
func TestCapitalizeFirst(t *testing.T) {
	assert.Equal(t, "", capitalizeFirst(""))
	assert.Equal(t, "Options", capitalizeFirst("options"))
}

// TestRecordValueString_非字符串值 map 里同名键不是字符串时返回空串。
func TestRecordValueString_非字符串值(t *testing.T) {
	assert.Equal(t, "", recordValueString(map[string]any{"options": 123}, "options"))
	assert.Equal(t, "", recordValueString(map[string]any{}, "options"))
}

// TestFlattenOptions_模板无已知字段 模板段只有未知键时也要产出该段。
func TestFlattenOptions_模板无已知字段(t *testing.T) {
	opts := TeamMemberOptions{
		AgentTemplate: &MemberAgentTemplate{Extra: map[string]any{"only_extra": "v"}},
	}
	out := flattenOptions(opts)
	tpl, ok := out["agent_template"].(map[string]any)
	require.True(t, ok, "模板段必须存在")
	assert.Equal(t, "v", tpl["only_extra"])
}

// TestLoadMemberOptions_模板段非对象 agent_template 不是对象时忽略该段。
func TestLoadMemberOptions_模板段非对象(t *testing.T) {
	opts := LoadMemberOptions(`{"agent_template":"not-a-map"}`)
	assert.Nil(t, opts.AgentTemplate)
}

// ──────────────────────────── 迁移测试 ────────────────────────────

// newMigrationTestDB 起一个内存 SQLite，返回 *gorm.DB。
func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:mig_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db
}

// TestEnsureTeamMemberOptionsColumn_补列并合并legacy 旧库没有 options 列时补列，
// 并把 model_ref_json 的内容合并进 options。
func TestEnsureTeamMemberOptionsColumn_补列并合并legacy(t *testing.T) {
	db := newMigrationTestDB(t)

	// 造一个"升级前"的表：没有 options 列，只有 model_ref_json
	require.NoError(t, db.Exec(`CREATE TABLE team_member (
		member_name TEXT NOT NULL,
		team_name TEXT NOT NULL,
		display_name TEXT,
		desc TEXT,
		agent_card TEXT,
		status TEXT,
		execution_status TEXT,
		mode TEXT,
		role TEXT,
		prompt TEXT,
		model_ref_json TEXT,
		updated_at INTEGER,
		PRIMARY KEY (member_name, team_name)
	)`).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO team_member (member_name, team_name, model_ref_json) VALUES (?, ?, ?)`,
		"m1", "team1", `{"model_name":"legacy-model"}`,
	).Error)

	ensureTeamMemberOptionsColumn(db)

	// options 列已补上，且 legacy model_ref 合并进来了
	var row TeamMember
	require.NoError(t, db.Where("member_name = ? AND team_name = ?", "m1", "team1").First(&row).Error)
	opts := LoadMemberOptions(row.Options)
	require.NotNil(t, opts.ModelRef)
	require.NotNil(t, opts.ModelRef.ModelName)
	assert.Equal(t, "legacy-model", *opts.ModelRef.ModelName)
}

// TestEnsureTeamMemberOptionsColumn_options已有时不覆盖 已有 model_ref 以 options 为准。
func TestEnsureTeamMemberOptionsColumn_options已有时不覆盖(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&TeamMember{}))
	// legacy 列已从模型删除，测试用原生 SQL 造出"升级前"的行
	require.NoError(t, db.Exec("ALTER TABLE team_member ADD COLUMN model_ref_json TEXT").Error)
	require.NoError(t, db.Exec(
		`INSERT INTO team_member (member_name, team_name, options, model_ref_json) VALUES (?,?,?,?)`,
		"m2", "team1", `{"model_ref":{"model_name":"from-options"}}`, `{"model_name":"from-legacy"}`,
	).Error)

	ensureTeamMemberOptionsColumn(db)

	var row TeamMember
	require.NoError(t, db.Where("member_name = ?", "m2").First(&row).Error)
	opts := LoadMemberOptions(row.Options)
	require.NotNil(t, opts.ModelRef)
	require.NotNil(t, opts.ModelRef.ModelName)
	assert.Equal(t, "from-options", *opts.ModelRef.ModelName)
}

// TestEnsureTeamMemberOptionsColumn_表不存在时安全返回 空库不 panic。
func TestEnsureTeamMemberOptionsColumn_表不存在时安全返回(t *testing.T) {
	db := newMigrationTestDB(t)
	assert.NotPanics(t, func() { ensureTeamMemberOptionsColumn(db) })
}

// ──────────────────────────── OpenTaskCountsByAssignee 测试 ────────────────────────────

// TestOpenTaskCountsByAssignee_内存实现 只统计非终态、已指派的任务。
func TestOpenTaskCountsByAssignee_内存实现(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	require.NoError(t, db.Initialize(ctx))

	mk := func(taskID, assignee, status string) {
		task := &TeamTaskBase{TaskID: taskID, TeamName: "t1", Title: taskID, Status: status}
		if assignee != "" {
			a := assignee
			task.Assignee = &a
		}
		_, err := db.Task().CreateTask(ctx, task)
		require.NoError(t, err)
	}
	mk("k1", "alice", fsm.TaskStatusClaimed)
	mk("k2", "alice", fsm.TaskStatusPending)
	mk("k3", "alice", fsm.TaskStatusCompleted) // 终态不计
	mk("k4", "bob", fsm.TaskStatusCancelled)   // 终态不计
	mk("k5", "", fsm.TaskStatusPending)        // 未指派不计

	counts, err := db.Task().OpenTaskCountsByAssignee(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 2, counts["alice"], "只算非终态")
	assert.NotContains(t, counts, "bob", "全终态的成员不出现在结果里")
}

// TestOpenTaskCountsByAssignee_SQL实现 与内存实现同语义。
func TestOpenTaskCountsByAssignee_SQL实现(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:otc_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	dao := &SQLTaskDao{db: gdb}
	ctx := context.Background()
	state := &sessionctx.SessionState{}
	state.SetSessionID("t1")
	ctx = sessionctx.WithSessionState(ctx, state)

	table := dao.taskTableName(ctx)
	require.NoError(t, gdb.Exec("CREATE TABLE "+table+` (
		task_id TEXT PRIMARY KEY, team_name TEXT, title TEXT, content TEXT,
		status TEXT, assignee TEXT, revision INTEGER, observed_revision INTEGER,
		change_notes TEXT, priority INTEGER, created_at INTEGER, updated_at INTEGER
	)`).Error)
	require.NoError(t, gdb.Exec("INSERT INTO "+table+` (task_id, team_name, status, assignee) VALUES
		('k1','t1','claimed','alice'),
		('k2','t1','pending','alice'),
		('k3','t1','completed','alice'),
		('k4','t1','cancelled','bob')`).Error)

	counts, err := dao.OpenTaskCountsByAssignee(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 2, counts["alice"])
	assert.NotContains(t, counts, "bob")
}

// TestOpenTaskCountsByAssignee_SQL表不存在返回错误 表缺失时把错误上抛而非静默空表。
func TestOpenTaskCountsByAssignee_SQL表不存在返回错误(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:otc_missing_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	dao := &SQLTaskDao{db: gdb}
	ctx := context.Background()
	state := &sessionctx.SessionState{}
	state.SetSessionID("nope")
	ctx = sessionctx.WithSessionState(ctx, state)

	_, err = dao.OpenTaskCountsByAssignee(ctx, "t1")
	assert.Error(t, err, "表不存在应返回错误")
}

// TestTerminalTaskStatuses_取值 终态集合与上游一致。
func TestTerminalTaskStatuses_取值(t *testing.T) {
	assert.ElementsMatch(t, []string{fsm.TaskStatusCompleted, fsm.TaskStatusCancelled}, fsm.TerminalTaskStatuses())
}

// ──────────────────────────── 成员删除 / 状态窄投影测试 ────────────────────────────

// TestDeleteUnstartedMember_内存实现 只删 UNSTARTED 行。
func TestDeleteUnstartedMember_内存实现(t *testing.T) {
	ctx := context.Background()
	db := NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(ctx))

	ok := db.Member().CreateMember(ctx, "m1", "t1", "M1", "{}", "unstarted", "teammate", "", "", "build_mode", "", "")
	require.True(t, ok)
	assert.True(t, db.Member().DeleteUnstartedMember(ctx, "m1", "t1"), "UNSTARTED 行应删除成功")
	m, _ := db.Member().GetMember(ctx, "m1", "t1")
	assert.Nil(t, m)

	// 已离开 UNSTARTED 的行拒绝删除
	require.True(t, db.Member().CreateMember(ctx, "m2", "t1", "M2", "{}", "unstarted", "teammate", "", "", "build_mode", "", ""))
	db.Member().UpdateMemberStatus(ctx, "m2", "t1", "ready")
	assert.False(t, db.Member().DeleteUnstartedMember(ctx, "m2", "t1"), "READY 行不应被删")
	m2, _ := db.Member().GetMember(ctx, "m2", "t1")
	assert.NotNil(t, m2)

	assert.False(t, db.Member().DeleteUnstartedMember(ctx, "ghost", "t1"), "不存在的行返回 false")
}

// TestGetMemberStatus_内存实现 返回状态与存在性。
func TestGetMemberStatus_内存实现(t *testing.T) {
	ctx := context.Background()
	db := NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(ctx))
	require.True(t, db.Member().CreateMember(ctx, "m1", "t1", "M1", "{}", "unstarted", "teammate", "", "", "build_mode", "", ""))

	status, found := db.Member().GetMemberStatus(ctx, "t1", "m1")
	assert.True(t, found)
	assert.Equal(t, "unstarted", status)

	_, found = db.Member().GetMemberStatus(ctx, "t1", "ghost")
	assert.False(t, found)
}

// TestDeleteUnstartedMember_SQL实现 与内存实现同语义。
func TestDeleteUnstartedMember_SQL实现(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&TeamMember{}))
	dao := &SQLMemberDao{db: db}
	ctx := context.Background()

	require.True(t, dao.CreateMember(ctx, "m1", "t1", "M1", "{}", "unstarted", "teammate", "", "", "build_mode", "", ""))
	assert.True(t, dao.DeleteUnstartedMember(ctx, "m1", "t1"))
	_, err := dao.GetMember(ctx, "m1", "t1")
	assert.NoError(t, err)

	require.True(t, dao.CreateMember(ctx, "m2", "t1", "M2", "{}", "unstarted", "teammate", "", "", "build_mode", "", ""))
	require.True(t, dao.UpdateMemberStatus(ctx, "m2", "t1", "ready"))
	assert.False(t, dao.DeleteUnstartedMember(ctx, "m2", "t1"))
}

// TestGetMemberStatus_SQL实现 与内存实现同语义。
func TestGetMemberStatus_SQL实现(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&TeamMember{}))
	dao := &SQLMemberDao{db: db}
	ctx := context.Background()

	require.True(t, dao.CreateMember(ctx, "m1", "t1", "M1", "{}", "unstarted", "teammate", "", "", "build_mode", "", ""))
	status, found := dao.GetMemberStatus(ctx, "t1", "m1")
	assert.True(t, found)
	assert.Equal(t, "unstarted", status)

	_, found = dao.GetMemberStatus(ctx, "t1", "ghost")
	assert.False(t, found)
}

// TestDeleteUnstartedMember_SQL执行报错 表缺失时返回 false 而非 panic。
func TestDeleteUnstartedMember_SQL执行报错(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:del_err_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	dao := &SQLMemberDao{db: gdb}
	assert.False(t, dao.DeleteUnstartedMember(context.Background(), "m", "t"), "表不存在应安全返回 false")
}
