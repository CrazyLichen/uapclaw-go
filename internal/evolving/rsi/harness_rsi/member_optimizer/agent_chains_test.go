package memberoptimizer

import (
	"context"
	"strings"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// wireFullBridge 装配完整桥（ParseJSONObject 本地、invoke 序列脚本）。
func wireFullBridge(t *testing.T, script []string) *[]string {
	t.Helper()
	saved := SnapshotAgentsBridge()
	seen := &[]string{}
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		*seen = append(*seen, params.UserMessage)
		text := script[0]
		if len(script) > 1 {
			script = script[1:]
		}
		raw, err := testParseJSONObject(text)
		if err != nil {
			return nil, &StructuredOutputExhaustedError{Msg: "parse failed: " + err.Error()}
		}
		return raw, nil
	}
	agentsBridge.InvokeAgent = func(_ context.Context, agent any, message, sessionID string) (map[string]any, error) {
		*seen = append(*seen, message)
		text := script[0]
		if len(script) > 1 {
			script = script[1:]
		}
		return map[string]any{"text": text}, nil
	}
	agentsBridge.ParseJSONObject = testParseJSONObject
	agentsBridge.ParseYamlOrJSONObject = testParseJSONObject
	agentsBridge.CreateActionPlanningAgent = func(context.Context, PlannerAgentParams) (any, error) {
		return "fake-planner", nil
	}
	agentsBridge.CreateActionExecutionAgent = func(context.Context, ExecutorAgentParams) (any, error) {
		return "fake-executor", nil
	}
	t.Cleanup(func() { agentsBridge = saved })
	return seen
}

func TestPlannerAgentCreatePlan_重试消息链(t *testing.T) {
	// fake InvokeStructured 不复刻内部重试循环（该循环由 agents 包实现并单测覆盖）；
	// 此处验证 CreatePlan 的消息构造与解析链路。
	script := []string{`{"actions": [], "action_waves": []}`}
	seen := wireFullBridge(t, script)
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake", modelConfigRef: ""}
	params := PlannerCreateParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	}
	raw, err := agent.CreatePlan(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, raw)
	require.Len(t, *seen, 1)
	require.Contains(t, (*seen)[0], "## Current Action Contract")
}

func TestPlannerAgentCreatePlan_耗尽拒绝(t *testing.T) {
	script := []string{"bad1", "bad2", "bad3"}
	wireFullBridge(t, script)
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake", modelConfigRef: ""}
	_, err := agent.CreatePlan(context.Background(), PlannerCreateParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	})
	require.Error(t, err)
	var exhausted *StructuredOutputExhaustedError
	require.ErrorAs(t, err, &exhausted)
}

func TestPlannerAgentCreatePlan_会话ID同胞命名(t *testing.T) {
	wireFullBridge(t, []string{`{"actions": [], "action_waves": []}`})
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake", modelConfigRef: ""}
	_, err := agent.CreatePlan(context.Background(), PlannerCreateParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
		OptimizationExperience: map[string]any{"sibling_generation": map[string]any{
			"candidate_id": "cand-one",
		}},
	})
	require.NoError(t, err)
}

func TestPlannerAgentCreatePlan_桥未注册拒绝(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	agent := &MemberActionPlannerAgent{modelConfigRef: ""}
	_, err := agent.CreatePlan(context.Background(), PlannerCreateParams{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "agents bridge not registered")
}

func TestExecutorAgentExecuteAction_状态失败重试与成功(t *testing.T) {
	script := []string{
		`{"status": "failed", "errors": ["baseline missing"]}`,
		`{"status": "succeeded", "errors": [], "file_writes": [{"path": "runtime.yaml", "content": "max_iterations: 9\n"}]}`,
	}
	seen := wireFullBridge(t, script)
	// worktree 带 runtime.yaml 基线（modify 校验）
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "runtime.yaml"), []byte("max_iterations: 5\n"), 0o644)
	agent := NewMemberActionExecutorAgent("", nil, nil, 300)
	action := MemberOptimizationAction{
		ActionID: "cfg1", Role: "solver", ActionGroup: "config", Operation: "modify",
		TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
		Constraints: map[string]any{},
	}
	result, err := agent.ExecuteAction(context.Background(), ws, action, "PLAN", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "succeeded", result["status"])
	require.Equal(t, []any{"runtime.yaml"}, result["changed_files"])
	require.Len(t, *seen, 2)
	// 第二次消息携带重试反馈
	require.Contains(t, (*seen)[1], "## Previous Invalid Output")
	// 文件确实写入
	content, _ := os.ReadFile(filepath.Join(ws, "runtime.yaml"))
	require.Contains(t, string(content), "max_iterations: 9")
}

func TestExecutorAgentExecuteAction_modify基线缺失(t *testing.T) {
	wireFullBridge(t, []string{`{}`})
	agent := NewMemberActionExecutorAgent("", nil, nil, 300)
	action := MemberOptimizationAction{
		ActionID: "m1", Role: "solver", ActionGroup: "config", Operation: "modify",
		TargetPath: "missing.yaml", DeclaredWritePaths: []string{"missing.yaml"},
	}
	result, err := agent.ExecuteAction(context.Background(), t.TempDir(), action, "P", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", result["status"])
	require.Contains(t, result["error"], "modify baseline mismatch")
}

func TestExecutorAgentExecuteAction_无声明变更拒绝(t *testing.T) {
	// agent 报成功但 file_writes 为空 → 无变更 → 重试耗尽 → failed
	script := []string{
		`{"status": "succeeded", "errors": []}`,
		`{"status": "succeeded", "errors": []}`,
	}
	wireFullBridge(t, script)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "runtime.yaml"), []byte("max_iterations: 5\n"), 0o644)
	agent := NewMemberActionExecutorAgent("", nil, &harnessrsi.OutputRepairConfig{MaxAttempts: 2, MaxStagnantAttempts: 1}, 300)
	action := MemberOptimizationAction{
		ActionID: "n1", Role: "solver", ActionGroup: "config", Operation: "modify",
		TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
		Constraints: map[string]any{},
	}
	result, err := agent.ExecuteAction(context.Background(), ws, action, "P", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", result["status"])
	require.Contains(t, result["error"], "agent reported success but no declared_write_paths changed")
	require.NotNil(t, result["output_repair"])
}

func TestExecutorAgentExecuteAction_桥未注册拒绝(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "runtime.yaml"), []byte("x\n"), 0o644)
	agent := NewMemberActionExecutorAgent("", nil, nil, 300)
	result, err := agent.ExecuteAction(context.Background(), ws, MemberOptimizationAction{
		ActionID: "x", Role: "r", ActionGroup: "config", Operation: "modify",
		TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
	}, "P", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", result["status"])
	require.Contains(t, result["error"], "agents bridge not registered")
}


func TestExportWrappers_转发链(t *testing.T) {
	// BindImmutableHypotheses / AnnotateActionBundles 导出包装（c1 planner 集成用）
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
	})
	hyp := []map[string]any{{"source_issue_id": "i1", "hypothesis_id": "h1", "required_behavior": "X"}}
	BindImmutableHypotheses(plan, hyp)
	metadata := plan["metadata"].(map[string]any)
	require.Equal(t, "immutable_optimization_hypotheses", metadata["semantic_authority"])
	AnnotateActionBundles(plan, hyp)
	require.Len(t, metadata["action_bundles"].([]any), 1)
}

func TestActionToMap_YAML往返(t *testing.T) {
	action := mkActionStruct()
	m := actionToMap(action)
	require.Equal(t, "a1", m["action_id"])
	require.Equal(t, "solver", m["role"])
}

func TestActionFieldValue_字段面(t *testing.T) {
	action := mkActionStruct()
	require.Equal(t, "a1", action.ActionFieldValue("action_id"))
	require.Equal(t, "solver", action.ActionFieldValue("role"))
	require.Equal(t, "config", action.ActionFieldValue("action_group"))
	require.Equal(t, "modify", action.ActionFieldValue("operation"))
	require.Equal(t, "runtime.yaml", action.ActionFieldValue("target_path"))
	require.Equal(t, "dependency_succeeded", action.ActionFieldValue("run_if"))
	require.Equal(t, []any{"i1"}, action.ActionFieldValue("attributed_issue_ids"))
	require.Nil(t, action.ActionFieldValue("unknown"))
}

func TestExecuteActionFieldValue_双轨(t *testing.T) {
	// map 双轨
	m := map[string]any{"role": "solver", "attributed_issue_ids": []any{"i1"}}
	key, ok := ActionBundleKey(m)
	require.True(t, ok)
	require.Equal(t, "solver", key[0])
	// getter 双轨
	struct1 := mkActionStruct()
	_, ok = ActionBundleKey(*struct1)
	require.True(t, ok)
}

func TestAppendManifestEntry_新文件与去重(t *testing.T) {
	// 新 manifest
	ws := t.TempDir()
	manifest := filepath.Join(ws, "tools", "tools.yaml")
	appendManifestEntry(manifest, "tools", map[string]any{"file": "tools/a.py", "class_name": "A"})
	data, _ := os.ReadFile(manifest)
	require.Contains(t, string(data), "tools/a.py")
	// 重复 file 替换（file_path 别名命中）
	appendManifestEntry(manifest, "tools", map[string]any{"file": "tools/a.py", "class_name": "B"})
	data, _ = os.ReadFile(manifest)
	require.Contains(t, string(data), "class_name: B")
	require.Equal(t, 1, strings.Count(string(data), "tools/a.py"))
}

func TestMechanismAttributorCreateDeepAgent_桥未注册(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	agent := NewMechanismAttributorAgent("", 1, "", nil, nil)
	_, err := agent.AttributeRoleMechanisms(context.Background(), "solver", nil, nil, "s")
	require.Error(t, err)
	require.Contains(t, err.Error(), "agents bridge not registered")
}

func TestPlannerCreateDeepAgent_桥未注册(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	agent := &MemberActionPlannerAgent{modelConfigRef: ""}
	_, err := agent.createDeepAgent(context.Background(), PlannerCreateParams{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "agents bridge not registered")
}

func TestBuildDeclaredFileContext_目录多文件截断(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "d"), 0o755)
	for i := 0; i < 25; i++ {
		os.WriteFile(filepath.Join(root, "d", "f"+itoa(i)+".txt"), []byte("x"), 0o644)
	}
	got := BuildDeclaredFileContext(root, []string{"d"}, 100)
	require.Contains(t, got, "Directory listing truncated at 20 files.")
}
