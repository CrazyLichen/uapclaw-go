package memberoptimizer

import (
	"context"
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// wireExecutorBridge 装配执行桥（测试进程不触发 agents init——blank import 成环；
// 桥内 InvokeAgent/CreateActionExecutionAgent 以本地 fake 承载，消息经 ExtractText
// 的 text 字段回传——与 agents 桥的响应形态一致）。
func wireExecutorBridge(t *testing.T, script []string) *[]string {
	t.Helper()
	saved := SnapshotAgentsBridge()
	seen := &[]string{}
	agentsBridge.InvokeAgent = func(_ context.Context, agent *agentface.Agent, message, sessionID string) (map[string]any, error) {
		*seen = append(*seen, message)
		text := script[0]
		if len(script) > 1 {
			script = script[1:]
		}
		return map[string]any{"text": text}, nil
	}
	agentsBridge.CreateActionExecutionAgent = func(context.Context, ExecutorAgentParams) (*agentface.Agent, error) {
		return &agentface.Agent{}, nil
	}
	t.Cleanup(func() { agentsBridge = saved })
	return seen
}

func TestMemberActionExecutor_Execute_确定性remove链(t *testing.T) {
	wireExecutorBridge(t, nil)
	// 整合区源 harness
	source := t.TempDir()
	os.MkdirAll(filepath.Join(source, "skills", "foo"), 0o755)
	os.WriteFile(filepath.Join(source, "skills", "foo", "SKILL.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(source, "skills", "skills.yaml"), []byte("skills:\n  - skills/foo\n"), 0o644)

	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_test",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: source, AttributedIssueIDs: []string{"i1"},
			OptimizationSurfaces: []string{"skill"},
		}},
		Actions: []MemberOptimizationAction{{
			ActionID: "rm1", Role: "solver", ActionGroup: "skill", Operation: "remove",
			TargetPath:         "skills/foo/SKILL.md",
			DeclaredWritePaths: []string{"skills/foo", "skills/skills.yaml"},
			AttributedIssueIDs: []string{"i1"},
		}},
		ActionWaves: [][]string{{"rm1"}},
	}
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	outDir := t.TempDir()
	results, err := executor.Execute(context.Background(), plan, outDir, "", "")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "succeeded", results[0].Status)
	require.Equal(t, "merged", results[0].MergeStatus)
	// 整合区中技能已删除
	integration := ResolveIntegrationWorktreePath(filepath.Join(outDir, "wt"), "solver")
	require.NoDirExists(t, filepath.Join(integration, "skills", "foo"))
	// execution_results.json 落盘
	require.FileExists(t, filepath.Join(outDir, "execution_results.json"))
}

func TestMemberActionExecutor_Execute_LLM写入链(t *testing.T) {
	// fake 执行 agent：结构化 file_writes JSON
	wireExecutorBridge(t, []string{`{"action_id": "add1", "status": "succeeded", "changed_files": ["prompt_sections/files/new.md"], "errors": [], "file_writes": [{"path": "prompt_sections/files/new.md", "content": "# new section\n"}]}`})
	source := t.TempDir()
	os.MkdirAll(filepath.Join(source, "prompt_sections", "files"), 0o755)
	os.WriteFile(filepath.Join(source, "prompt_sections", "sections.yaml"), []byte("sections: []\n"), 0o644)

	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_test",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: source, AttributedIssueIDs: []string{"i1"},
			OptimizationSurfaces: []string{"prompt_section"},
		}},
		Actions: []MemberOptimizationAction{{
			ActionID: "add1", Role: "solver", ActionGroup: "prompt", Operation: "add",
			TargetPath:         "prompt_sections/files/new.md",
			DeclaredWritePaths: []string{"prompt_sections/files/new.md", "prompt_sections/sections.yaml"},
			AttributedIssueIDs: []string{"i1"},
			Constraints:        map[string]any{"section_name": "new"},
		}},
		ActionWaves: [][]string{{"add1"}},
	}
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	outDir := t.TempDir()
	results, err := executor.Execute(context.Background(), plan, outDir, "", "")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "succeeded", results[0].Status)
	// 整合区文件与登记簿
	integration := ResolveIntegrationWorktreePath(filepath.Join(outDir, "wt"), "solver")
	require.FileExists(t, filepath.Join(integration, "prompt_sections", "files", "new.md"))
	sections, _ := os.ReadFile(filepath.Join(integration, "prompt_sections", "sections.yaml"))
	require.Contains(t, string(sections), "name: new")
}

func TestMemberActionExecutor_Execute_原子束同伴失败不合并(t *testing.T) {
	// 两个动作同束（同 role + issue i1）；第一个成功第二个失败 → 第一个改判 failed
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "identity.md"), []byte("i"), 0o644)
	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_test",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: source, AttributedIssueIDs: []string{"i1"},
			OptimizationSurfaces: []string{"config"},
		}},
		Actions: []MemberOptimizationAction{
			{
				ActionID: "ok1", Role: "solver", ActionGroup: "config", Operation: "modify",
				TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
				AttributedIssueIDs: []string{"i1"},
				// 策略非法（缺 constraints 等——运行时通过 remove 语义外路径制造失败：
				// 改用未知组触发 policy 失败）
			},
			{
				ActionID: "bad1", Role: "solver", ActionGroup: "bogus", Operation: "modify",
				TargetPath: "x.yaml", DeclaredWritePaths: []string{"x.yaml"},
				AttributedIssueIDs: []string{"i1"},
			},
		},
		ActionWaves: [][]string{{"ok1", "bad1"}},
	}
	// ok1 用 remove 语义成功；bad1 组非法 → 策略失败
	os.WriteFile(filepath.Join(source, "runtime.yaml"), []byte("max_iterations: 5\n"), 0o644)
	plan.Actions[0].ActionGroup = "config"
	plan.Actions[0].Operation = "remove"
	plan.Actions[0].TargetPath = "runtime.yaml"
	// 声明路径补全后动作束仍连通（无 depends_on → 连通性校验不触发）
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	outDir := t.TempDir()
	results, err := executor.Execute(context.Background(), plan, outDir, "", "")
	require.NoError(t, err)
	require.Len(t, results, 2)
	byID := map[string]*MemberActionExecutionResult{}
	for _, result := range results {
		byID[result.ActionID] = result
	}
	// bad1 策略失败
	require.Equal(t, "failed", byID["bad1"].Status)
	// ok1 成功但同伴失败 → 不合并
	require.Equal(t, "failed", byID["ok1"].Status)
	require.Equal(t, "not_merged", byID["ok1"].MergeStatus)
	require.Contains(t, byID["ok1"].Error, "atomic action bundle peer failed")
}

func TestMemberActionExecutor_Execute_角色失败后续波跳过(t *testing.T) {
	source := t.TempDir()
	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_test",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: source, AttributedIssueIDs: []string{"i1"},
		}},
		Actions: []MemberOptimizationAction{
			{
				ActionID: "fail1", Role: "solver", ActionGroup: "bogus", Operation: "modify",
				TargetPath: "x", DeclaredWritePaths: []string{"x"},
				AttributedIssueIDs: []string{"i1"},
			},
			{
				ActionID: "later1", Role: "solver", ActionGroup: "config", Operation: "modify",
				TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
				AttributedIssueIDs: []string{"i1"}, DependsOn: []string{"fail1"},
			},
		},
		ActionWaves: [][]string{{"fail1"}, {"later1"}},
	}
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	outDir := t.TempDir()
	results, err := executor.Execute(context.Background(), plan, outDir, "", "")
	require.NoError(t, err)
	require.Len(t, results, 2)
	byID := map[string]*MemberActionExecutionResult{}
	for _, result := range results {
		byID[result.ActionID] = result
	}
	require.Equal(t, "failed", byID["fail1"].Status)
	// Python 语义：should_run 检查先于 role_failed——依赖失败 → skipped（依赖失败理由）
	require.Equal(t, "skipped", byID["later1"].Status)
	require.Equal(t, "dependency failed: [fail1]", byID["later1"].Error)
}

func TestMemberActionExecutor_依赖失败回退执行(t *testing.T) {
	wireExecutorBridge(t, []string{`{"action_id": "fallback1", "status": "succeeded", "changed_files": ["runtime.yaml"], "errors": [], "file_writes": [{"path": "runtime.yaml", "content": "max_iterations: 7\n"}]}`})
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "runtime.yaml"), []byte("max_iterations: 5\n"), 0o644)
	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_test",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: source, AttributedIssueIDs: []string{"i1"},
		}},
		Actions: []MemberOptimizationAction{
			{
				ActionID: "search1", Role: "solver", ActionGroup: "skill", Operation: "search",
				TargetPath: "skills/", DeclaredWritePaths: []string{"skills/", "skills/skills.yaml"},
				AttributedIssueIDs: []string{"i1"}, CandidateQuery: "find",
			},
			{
				ActionID: "fallback1", Role: "solver", ActionGroup: "config", Operation: "modify",
				TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
				AttributedIssueIDs: []string{"i1"}, DependsOn: []string{"search1"},
				RunIf: "dependency_failed",
			},
		},
		ActionWaves: [][]string{{"search1", "fallback1"}},
	}
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	outDir := t.TempDir()
	results, err := executor.Execute(context.Background(), plan, outDir, "", "")
	require.NoError(t, err)
	require.Len(t, results, 2)
	byID := map[string]*MemberActionExecutionResult{}
	for _, result := range results {
		byID[result.ActionID] = result
	}
	// search 在执行器内确定性拒绝
	require.Equal(t, "failed", byID["search1"].Status)
	require.Contains(t, byID["search1"].Error, "external skill search is unavailable")
	// 回退动作 dependency_failed 命中 → 执行
	require.Equal(t, "succeeded", byID["fallback1"].Status)
}

func TestWriteFailedActionResult_产物字段(t *testing.T) {
	executor := NewMemberActionExecutor(nil, nil, 2, 2, 2, nil, nil, 300)
	action := mkActionStruct()
	result := executor.WriteFailedActionResult(action, t.TempDir(), "policy failure")
	require.Equal(t, "failed", result.Status)
	require.Empty(t, result.WorktreePath)
	require.FileExists(t, result.ArtifactPath)
	data, _ := os.ReadFile(result.ArtifactPath)
	require.Contains(t, string(data), `"error": "policy failure"`)
	require.Contains(t, string(data), "allowed_executor_tools")
}
