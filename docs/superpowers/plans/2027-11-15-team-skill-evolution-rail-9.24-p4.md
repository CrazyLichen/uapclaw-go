# TeamSkillEvolutionRail (9.24 P4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement TeamSkillEvolutionRail — the team-skill evolution rail that triggers when all team tasks complete, analyzes aggregated team trajectories, generates evolution records, and routes them through approval.

**Architecture:** Pure independent struct embedding `*EvolutionRail` base class, implementing `EvolutionExtension` interface's 10 methods. Sibling of `SkillEvolutionRail`, zero sharing, one-to-one alignment with Python `team_skill_evolution_rail.py`. DeepAdapter stubs backfilled to route `team_skill_evolve_` prefixed requests.

**Tech Stack:** Go 1.22+, existing packages: `evolving/checkpointing`, `evolving/experience`, `evolving/signal`, `evolving/trajectory`, `evolving/optimizer/skill_call`, `evolving/updater/single_dim`

**Design spec:** `docs/superpowers/specs/2027-11-15-team-skill-evolution-rail-9.24-p4-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go` | TeamSkillEvolutionRail struct + constructor + EvolutionExtension 10 methods + public API + private helpers + module-level functions |
| Create | `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go` | Unit tests |
| Modify | `internal/agentcore/harness/rails/evolution/doc.go` | Add file entry + update package overview |
| Modify | `internal/swarm/server/adapter/deep_adapter.go` | Add `teamSkillEvolutionRail` field + backfill switch branch |
| Modify | `internal/swarm/server/adapter/deep_adapter_team.go` | Backfill 3 stub functions |
| Modify | `IMPLEMENTATION_PLAN.md` | Update 9.24 P4 status ☐→✅ |

---

### Task 1: 常量 + 模块级辅助函数

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go`

- [ ] **Step 1: Write failing test for isCompletedTeamTaskView**

```go
func TestIsCompletedTeamTaskView(t *testing.T) {
	// 全部完成
	assert.True(t, isCompletedTeamTaskView(map[string]any{
		"tasks": []map[string]any{
			{"name": "a", "status": "completed"},
			{"name": "b", "status": "completed"},
		},
	}))
	// 有 pending
	assert.False(t, isCompletedTeamTaskView(map[string]any{
		"tasks": []map[string]any{
			{"name": "a", "status": "completed"},
			{"name": "b", "status": "pending"},
		},
	}))
	// 无 completed
	assert.False(t, isCompletedTeamTaskView(map[string]any{
		"tasks": []map[string]any{
			{"name": "a", "status": "in_progress"},
		},
	}))
	// 字符串结果包含 completed 且无非终态
	assert.True(t, isCompletedTeamTaskView("All tasks completed"))
	assert.False(t, isCompletedTeamTaskView("Task pending and completed"))
	// 空结果
	assert.False(t, isCompletedTeamTaskView(""))
	assert.False(t, isCompletedTeamTaskView(nil))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestIsCompletedTeamTaskView -v`
Expected: FAIL — function not defined

- [ ] **Step 3: Create team_skill_evolution_rail.go with package header, imports, constants, and module-level functions**

Write the file with:
- `package evolution`
- Imports (context, encoding/json, fmt, os, path/filepath, regexp, strings, time + internal packages)
- Constants block:
  - `teamSkillDefaultEvolutionTimeoutSecs = 720.0`
  - `teamSkillKinds` map: `{"team-skill": true, "swarm-skill": true}`
  - `teamTaskNonTerminalStates` slice: `["pending", "claimed", "in_progress", "blocked"]`
  - LLM Policy vars: `teamUserRequestLLMPolicy`, `teamTrajectoryIssueLLMPolicy`, `teamRecordLLMPolicy` (matching Python values)
  - Regex: `skillMDRE`, `experienceRecordHeadingRE` (same as SkillEvolutionRail — independent copy)
- Module-level function `isCompletedTeamTaskView(result any) bool` — Python: `is_completed_team_task_view()`. Lowercase the string representation, check "completed" present and no non-terminal state present.
- Module-level function `inferTeamSkillFromTrajectory(traj *trajectory.Trajectory, knownSkills []string, store *checkpointing.EvolutionStore) string` — Python: `infer_team_skill_from_trajectory()`. Iterate trajectory steps, collect texts and skill_tool payloads, call `checkpointing.InferSkillFromTexts`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestIsCompletedTeamTaskView -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "feat(evolution): add TeamSkillEvolutionRail constants and module-level helpers (isCompletedTeamTaskView, inferTeamSkillFromTrajectory)"
```

---

### Task 2: 结构体定义 + 构造器 + Option 函数

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go`

- [ ] **Step 1: Write failing test for NewTeamSkillEvolutionRail**

```go
func TestNewTeamSkillEvolutionRail(t *testing.T) {
	llmModel := llm.NewModel(model_clients.NewMockModelClient(), "test-model")
	rail := NewTeamSkillEvolutionRail(
		[]string{t.TempDir()},
		llmModel,
		"test-model",
		"cn",
	)
	assert.NotNil(t, rail)
	assert.NotNil(t, rail.EvolutionRail)
	assert.Equal(t, 80, rail.Priority())
	assert.True(t, rail.autoScan)
	assert.False(t, rail.autoSave)  // 默认 false（团队需审批）
	assert.Equal(t, 720.0, rail.GetEvolutionTotalTimeoutSecs())
	assert.NotNil(t, rail.generator)
	assert.NotNil(t, rail.scorer)
	assert.NotNil(t, rail.manager)
	assert.NotNil(t, rail.approvalRuntime)
	assert.NotNil(t, rail.teamSignalDetector)
	assert.NotNil(t, rail.experienceTracker)
}

func TestNewTeamSkillEvolutionRail_WithOptions(t *testing.T) {
	llmModel := llm.NewModel(model_clients.NewMockModelClient(), "test-model")
	rail := NewTeamSkillEvolutionRail(
		[]string{t.TempDir()},
		llmModel,
		"test-model",
		"cn",
		WithTeamSkillAutoSave(true),
		WithTeamSkillEvolutionTimeout(300),
		WithTeamSkillEvalInterval(10),
		WithTeamSkillTeamID("team-1"),
	)
	assert.True(t, rail.autoSave)
	assert.Equal(t, 300.0, rail.GetEvolutionTotalTimeoutSecs())
	assert.Equal(t, 10, rail.evalInterval)
	assert.Equal(t, "team-1", rail.teamID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestNewTeamSkillEvolutionRail -v`
Expected: FAIL — type not defined

- [ ] **Step 3: Implement TeamSkillEvolutionRail struct, constructor, and Option functions**

Add to `team_skill_evolution_rail.go`:

1. `TeamSkillEvolutionRail` struct (fields per design spec — no Sharing fields)
2. `TeamSkillEvolutionRailOption` type
3. Option functions: `WithTeamSkillAutoScan`, `WithTeamSkillAutoSave`, `WithTeamSkillEvalInterval`, `WithTeamSkillEvolutionTimeout`, `WithTeamSkillTeamID`, `WithTeamSkillTrajectorySource`, `WithTeamSkillTrajectorySink`, `WithTeamSkillMemberRole`, `WithTeamSkillUserRequestLLMPolicy`, `WithTeamSkillTrajectoryIssueLLMPolicy`, `WithTeamSkillRecordLLMPolicy`, `WithTeamSkillEvaluateLLMPolicy`, `WithTeamSkillSimplifyLLMPolicy`, `WithTeamSkillAsyncEvolution`, `WithTeamSkillMaxConcurrentEvolution`, `WithTeamSkillDisabledSkills`, `WithTeamSkillTrajectoriesDir`
4. `NewTeamSkillEvolutionRail` constructor:
   - Initialize EvolutionRail base with `NewEvolutionRail(rail, WithDefaultMemberRole("leader"), WithEvolutionTrigger(TriggerAfterInvoke), ...)`
   - Initialize all components: EvolutionStore, TeamSkillExperienceOptimizer, ExperienceScorer, ExperienceManager (kind="team-skill"), EvolutionApprovalRuntime, SingleDimUpdater, OnlineEvolutionOrchestrator (requestIDPrefix="team_skill_evolve", stageSource="team_skill_experience_updater"), TeamSignalDetector, ExperienceTracker
   - Set defaults: autoScan=true, autoSave=false, evolutionTotalTimeoutSecs=720.0, evalInterval=5

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestNewTeamSkillEvolutionRail -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "feat(evolution): add TeamSkillEvolutionRail struct, constructor, and Option functions"
```

---

### Task 3: EvolutionExtension 10 个方法

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go`

- [ ] **Step 1: Write failing tests for key extension methods**

```go
func TestTeamSkillEvolutionRail_OnBeforeInvoke(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	rail.passiveEvolutionPending = true
	_ = rail.OnBeforeInvoke(context.Background(), nil)
	assert.False(t, rail.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_AllowEvolutionTrigger(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	// 默认：passiveEvolutionPending=false, hostCompletion=nil
	assert.False(t, rail.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
	// passiveEvolutionPending=true
	rail.passiveEvolutionPending = true
	assert.True(t, rail.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
	// autoScan=false 时不触发
	rail.autoScan = false
	assert.False(t, rail.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
}

func TestTeamSkillEvolutionRail_GetEvolutionTotalTimeoutSecs(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	assert.Equal(t, 720.0, rail.GetEvolutionTotalTimeoutSecs())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_On -v`
Expected: FAIL

- [ ] **Step 3: Implement all 10 EvolutionExtension methods**

1. `OnBeforeInvoke` — reset `passiveEvolutionPending = false`
2. `OnAfterModelCall` — return nil (no-op)
3. `OnAfterToolCall` — (a) call `recordPresentedExperienceDetail` (b) if autoScan && toolName=="view_task": check `isCompletedTeamTaskView`, mark `passiveEvolutionPending=true`, log
4. `OnAfterInvoke` — return nil
5. `OnAfterTaskIteration` — return nil
6. `OnAfterEvolutionTriggered` — if `hostCompletionPendingSessionID == traj.SessionID`, set nil
7. `AllowEvolutionTrigger` — return `autoScan && (passiveEvolutionPending || hostCompletionPendingSessionID == currentBuilderSessionID)`
8. `SnapshotForEvolution` — build snapshot with `skill_name="team-skill"`, collect presentedEntries from experienceTracker
9. `RunEvolution` — full team evolution flow (aggregate trajectory → detect used team skill → TeamSignalDetector → handleEvolutionFromSignals → evaluatePresentedEntries). Log extensively, emit progress events.
10. `GetEvolutionTotalTimeoutSecs` — return `evolutionTotalTimeoutSecs`

Also implement helper `currentBuilderSessionID()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "feat(evolution): implement TeamSkillEvolutionRail EvolutionExtension 10 methods"
```

---

### Task 4: 公开 API 方法

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go`

- [ ] **Step 1: Write failing tests for public API**

```go
func TestTeamSkillEvolutionRail_NotifyTeamCompleted(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	rail.autoScan = true
	// 需要 builder，通过 BeforeInvoke 间接初始化
	ok, err := rail.NotifyTeamCompleted(context.Background())
	// 无 builder 时返回 false
	assert.False(t, ok)
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_ApproveRejectRecord(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	err := rail.ApproveRecord(context.Background(), "nonexistent")
	assert.NoError(t, err) // 无 pending 时不报错
	err = rail.RejectRecord(context.Background(), "nonexistent")
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_Accessors(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	assert.NotNil(t, rail.EvolutionStore())
	assert.NotNil(t, rail.Scorer())
	assert.NotNil(t, rail.Generator())
	assert.NotNil(t, rail.TeamSignalDetector())
	assert.NotNil(t, rail.ApprovalRuntime())
	assert.True(t, rail.AutoScan())
	assert.False(t, rail.AutoSave())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_Notify -v`
Expected: FAIL

- [ ] **Step 3: Implement all public API methods**

1. `NotifyTeamCompleted(ctx) (bool, error)` — check autoScan, check builder != nil, set `hostCompletionPendingSessionID = builder.SessionID`
2. `RequestUserEvolution(ctx, skillName, userIntent, autoApprove) (*EvolutionRequestResult, error)` — build trajectory, aggregate, detect signals, append user intent signal, handleEvolutionFromSignals, build approval event
3. `ApproveRecord(ctx, requestID) error` — delegate to `approvalRuntime.ApprovePendingRequest`, log
4. `RejectRecord(ctx, requestID) error` — delegate to `approvalRuntime.RejectPendingRequest`, log
5. `RequestSimplify(ctx, skillName, userIntent) (*SimplifyRequestResult, error)` — delegate to `manager.RequestSimplify`, build approval event
6. `OnApproveSimplify(ctx, requestID) (map[string]int, error)` — delegate to `manager.ApproveSimplify`
7. `OnRejectSimplify(requestID)` — delegate to `manager.RejectSimplify`
8. `RequestRebuild(ctx, skillName, userIntent, minScore) (string, error)` — delegate to `manager.RequestRebuild`
9. `RecordPresentedExperiences(ctx, skillName, snippet, sessionID, recordIDs)` — delegate to `experienceTracker`
10. Accessor methods: `EvolutionStore()`, `Scorer()`, `Generator()`, `TeamSignalDetector()`, `ApprovalRuntime()`, `AutoScan()`, `SetAutoScan()`, `AutoSave()`, `SetAutoSave()`, `EvolutionConfig()`
11. `SetTrajectorySource(source)` — bind trajectory source
12. `UpdateLLM(llmModel, model)` — update generator/scorer/detector LLM references
13. `SetSysOperation(op)` — set SysOperation on skill_ops

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "feat(evolution): implement TeamSkillEvolutionRail public API methods"
```

---

### Task 5: 私有辅助方法

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go`

- [ ] **Step 1: Write failing tests for private helpers**

```go
func TestTeamSkillEvolutionRail_isTeamSkill(t *testing.T) {
	rail := newTeamSkillEvolutionRailForTest(t)
	// 在临时目录创建 team-skill
	skillDir := filepath.Join(rail.EvolutionStore().BaseDirs()[0], "my-team-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	skillMD := `---
kind: team-skill
---
# My Team Skill
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644))
	assert.True(t, rail.isTeamSkill(context.Background(), "my-team-skill"))

	// regular skill
	regDir := filepath.Join(rail.EvolutionStore().BaseDirs()[0], "my-reg-skill")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "SKILL.md"), []byte("# Regular\n"), 0o644))
	assert.False(t, rail.isTeamSkill(context.Background(), "my-reg-skill"))
}

func TestTeamSkillEvolutionRail_extractToolArgs(t *testing.T) {
	args := teamExtractToolArgs(map[string]any{"skill_name": "foo"})
	assert.Equal(t, "foo", args["skill_name"])
	args = teamExtractToolArgs(`{"skill_name":"bar"}`)
	assert.Equal(t, "bar", args["skill_name"])
	args = teamExtractToolArgs(42)
	assert.Empty(t, args)
}

func TestTeamSkillEvolutionRail_isExperienceDetailRelativePath(t *testing.T) {
	assert.True(t, teamIsExperienceDetailRelativePath("evolution/some-record.md"))
	assert.False(t, teamIsExperienceDetailRelativePath("evolution/scripts/run.sh"))
	assert.False(t, teamIsExperienceDetailRelativePath("SKILL.md"))
	assert.False(t, teamIsExperienceDetailRelativePath("../escape.md"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_is -v`
Expected: FAIL

- [ ] **Step 3: Implement all private helper methods**

Team-specific methods (use `team` prefix in Go to avoid name collision since we chose "pure independent"):
1. `isTeamSkill(ctx, name) bool` — read SKILL.md, parse frontmatter, check `kind ∈ {"team-skill", "swarm-skill"}`
2. `detectUsedTeamSkill(ctx, trajectory) string` — list skills, filter team skills, call `inferTeamSkillFromTrajectory`
3. `teamSkillForExperienceDetailFile(ctx, filePath) string` — resolve path, iterate team skills
4. `aggregateTeamTrajectory(trajectory) *Trajectory` — check trajectorySource, call `source.GetTrajectory(teamID, sessionID, filter_collaborative=true)`
5. `detectUserRequest(ctx, messages, skillContent) (*signal.UserIntent, error)` — delegate to `teamSignalDetector.DetectUserIntent`

Duplicate-but-independent methods (prefixed `team` to avoid compile collision):
6. `teamExtractToolArgs(toolArgs) map[string]any` — same logic as `extractToolArgs`
7. `teamExtractToolContent(inputs) string` — same logic as SkillEvolutionRail.extractToolContent
8. `teamExtractPresentedRecordIDs(content) []string` — same logic using experienceRecordHeadingRE
9. `teamIsExperienceDetailRelativePath(path) bool` — same logic as `isExperienceDetailRelativePath`
10. `detectExperienceDetailRead(inputs) string` — team-skill variant: check `_isTeamSkill` instead of `_isRegularSkill`
11. `recordPresentedExperienceDetail(ctx, cbc, inputs)` — call detectExperienceDetailRead + extractToolContent + extractPresentedRecordIDs + experienceTracker
12. `consumePresentedEntries(session) []PresentedRecordEntry` — delegate to experienceTracker
13. `evaluatePresentedEntries(ctx, entries)` — delegate to experienceTracker
14. `markPassiveEvolutionPending()` — idempotent set flag
15. `isActiveRequestSubject(ctx, skillName) bool` — check store.SkillExists + isTeamSkill
16. `detectActiveRequestSignals(ctx, skillName, trajectory) []*EvolutionSignal` — use TeamSignalDetector.DetectTrajectorySignals, filter by skillName
17. `emitProgress(stage, message, opts...)` — log + BuildEvolutionProgressEvent("team", ...) with prefix "[Team Skill Evolution]"
18. `buildRecordApprovalEvent(skillName, request, proposal) *OutputSchema` — use BuildTeamSkillApprovalEventFromRecords + AttachEvolutionMeta
19. `emitRecordApprovalEvent(skillName, pending, proposal)` — emitHostEvent + emitProgress
20. `handleEvolutionFromSignals(...)` — stageEvolutionFromSignals + approvalRuntime.FinalizeStagedEvolutionRequest with team-specific approval/auto-approved callbacks
21. `stageEvolutionFromSignals(...)` — delegate to onlineOrchestrator.Evolve
22. `appendUniqueSignal(signals, sig)` — team version (same logic, independent function)
23. `dumpTrajectoryDebug(trajectory)` — write JSON debug file

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestTeamSkillEvolutionRail_ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "feat(evolution): implement TeamSkillEvolutionRail private helper methods"
```

---

### Task 6: doc.go 更新

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`

- [ ] **Step 1: Update doc.go**

Update package overview to mention P4, add `team_skill_evolution_rail.go` to file tree.

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/doc.go
git commit -m "docs(evolution): update doc.go with TeamSkillEvolutionRail entry"
```

---

### Task 7: DeepAdapter 回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`

- [ ] **Step 1: Add teamSkillEvolutionRail field to DeepAdapter struct**

In `deep_adapter.go`, after `skillEvolutionRail *evolution.SkillEvolutionRail`:
```go
// teamSkillEvolutionRail 团队技能演进护栏
// ✅ 已回填：TeamSkillEvolutionRail（对齐 Python: _team_skill_rail: TeamSkillEvolutionRail | None）
teamSkillEvolutionRail *evolution.TeamSkillEvolutionRail
```

- [ ] **Step 2: Backfill findTeamSkillRail**

Replace stub in `deep_adapter_team.go`:
```go
func (d *DeepAdapter) findTeamSkillRail() sainterfaces.AgentRail {
	if d.teamSkillEvolutionRail != nil {
		return d.teamSkillEvolutionRail
	}
	if d.instance == nil {
		return nil
	}
	for _, rail := range d.instance.Rails() {
		if _, ok := rail.(*evolution.TeamSkillEvolutionRail); ok {
			d.teamSkillEvolutionRail = rail.(*evolution.TeamSkillEvolutionRail)
			return rail
		}
	}
	return nil
}
```

- [ ] **Step 3: Backfill handleTeamSkillEvolveApproval**

Replace stub in `deep_adapter_team.go`:
```go
func (d *DeepAdapter) handleTeamSkillEvolveApproval(ctx context.Context, requestID string, answers any, sessionID string, channelID string) bool {
	rail := d.findTeamSkillRail()
	if rail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleTeamSkillEvolveApproval: TeamSkillEvolutionRail 未初始化")
		return false
	}
	teamRail := rail.(*evolution.TeamSkillEvolutionRail)

	parsedAnswers := parseApprovalAnswersFromAny(answers)
	approved := parseApprovalAnswers(parsedAnswers)

	if approved {
		err := teamRail.ApproveRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleTeamSkillEvolveApproval: ApproveRecord 失败")
			return false
		}
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleTeamSkillEvolveApproval: 已批准")
	} else {
		err := teamRail.RejectRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleTeamSkillEvolveApproval: RejectRecord 失败")
			return false
		}
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleTeamSkillEvolveApproval: 已拒绝")
	}

	// 排空结果事件
	d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "resolved")
	return true
}
```

- [ ] **Step 4: Backfill pushTeamSkillEvolveResolutionStatus**

Replace stub in `deep_adapter_team.go`:
```go
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID string, status string) error {
	rail := d.findTeamSkillRail()
	if rail == nil {
		return nil
	}
	teamRail := rail.(*evolution.TeamSkillEvolutionRail)
	events := teamRail.DrainPendingHostEvents(true, nil)
	if len(events) == 0 {
		return nil
	}
	for _, event := range events {
		d.pushEventToFrontend(event, requestID, "", requestID)
	}
	logger.Info(logComponent).Str("request_id", requestID).Str("status", status).Int("events", len(events)).Msg("pushTeamSkillEvolveResolutionStatus 完成")
	return nil
}
```

- [ ] **Step 5: Backfill deep_adapter.go switch branch**

In `HandleUserAnswer`, replace:
```go
case strings.HasPrefix(requestID, "team_skill_evolve_"):
    // ⤵️ 10.6.3-10: handle_team_skill_evolve_approval 依赖 P4 TeamSkillEvolutionRail
    resolved = false
```
with:
```go
case strings.HasPrefix(requestID, "team_skill_evolve_"):
    // ✅ 已回填：handle_team_skill_evolve_approval（对齐 Python 10.6.3-10）
    resolved = d.handleTeamSkillEvolveApproval(ctx, requestID, answers, sessionID, channelID)
```

- [ ] **Step 6: Verify compilation**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/...`
Expected: no errors

- [ ] **Step 7: Run existing adapter tests**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/adapter/... -v`
Expected: all existing tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_team.go
git commit -m "feat(adapter): backfill DeepAdapter team skill evolution approval stubs"
```

---

### Task 8: 整体测试覆盖率验证

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go`

- [ ] **Step 1: Review and add missing test cases**

Ensure test coverage for:
- `OnAfterToolCall` with view_task interception
- `SnapshotForEvolution` includes presentedEntries
- `OnAfterEvolutionTriggered` consumes hostCompletionPendingSessionID
- `NotifyTeamCompleted` with/without builder
- `RequestUserEvolution` basic flow
- `RequestSimplify` / `OnApproveSimplify` / `OnRejectSimplify`
- `RequestRebuild`
- `RecordPresentedExperiences`
- `detectExperienceDetailRead` with team-skill filter
- `aggregateTeamTrajectory` with/without trajectorySource
- `isActiveRequestSubject`
- `emitProgress` (verify BuildEvolutionProgressEvent called with "team" railKind)
- `buildRecordApprovalEvent` uses BuildTeamSkillApprovalEventFromRecords

- [ ] **Step 2: Run coverage check**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/...`
Expected: overall coverage ≥ 85%

- [ ] **Step 3: Fix any coverage gaps**

Add additional tests if coverage below target.

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/team_skill_evolution_rail_test.go
git commit -m "test(evolution): complete TeamSkillEvolutionRail unit test coverage"
```

---

### Task 9: IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: Update 9.24 P4 status**

Find line with `9.24 | 🔄 | EvolutionRail` and update P4 status from `☐` to `✅`:
Change: `P4(☐ 团队演化: TeamSkillEvolutionRail+team审批回填)` → `P4(✅ 团队演化: TeamSkillEvolutionRail+team审批回填)`

Also update the DeepAdapter stub markers: remove `⤵️ 10.6.3-10` annotations from the 3 functions in deep_adapter_team.go since they are now backfilled, but keep the `⤵️` for `findTeamSkillRail`'s Rail instance injection (that still depends on 10.6.3-10).

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update 9.24 P4 status to completed"
```

---

### Task 10: 全量编译验证

- [ ] **Step 1: Kill any existing go build/test processes**

Run: `pgrep -f 'go (build|test)' | xargs -r kill`

- [ ] **Step 2: Full build**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: no errors

- [ ] **Step 3: Full test suite for evolution package**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/rails/evolution/... -v -count=1`
Expected: all tests PASS

- [ ] **Step 4: Full test suite for adapter package**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/... -v -count=1`
Expected: all tests PASS

- [ ] **Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address final compilation and test issues for TeamSkillEvolutionRail P4"
```
