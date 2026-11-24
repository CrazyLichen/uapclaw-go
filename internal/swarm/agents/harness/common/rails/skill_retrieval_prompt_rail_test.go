package rails

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	toolkits "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// newTestSkillRoot 建临时技能目录。
func newTestSkillRoot(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: the " + name + " skill\n---\n\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func enableCfg() map[string]any {
	return map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}}
}

func newTestToolkit(t *testing.T, root string) *toolkits.SkillRetrievalToolkit {
	t.Helper()
	tk, err := toolkits.NewSkillRetrievalToolkit(toolkits.ToolkitOptions{
		SkillDirectories: func() []string { return []string{root} },
		ConfigBase:       enableCfg(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// TestNewSkillRetrievalPromptRail_构造期冻结
func TestNewSkillRetrievalPromptRail_构造期冻结(t *testing.T) {
	root := newTestSkillRoot(t, "alpha", "beta")
	tk := newTestToolkit(t, root)
	// 构造后再加技能，冻结快照不变
	newTestSkillRoot(t, "gamma")
	rail, err := NewSkillRetrievalPromptRail(RailOptions{Toolkit: tk, ConfigBase: enableCfg()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := rail.promptSnapshot()
	if len(snapshot.Entries) != 2 {
		t.Fatalf("frozen snapshot should have 2 entries, got %d", len(snapshot.Entries))
	}
}

// TestNewSkillRetrievalPromptRail_非法notice上限
func TestNewSkillRetrievalPromptRail_非法notice上限(t *testing.T) {
	zero := 0
	if _, err := NewSkillRetrievalPromptRail(RailOptions{ConfigBase: enableCfg(), IncrementalNoticeMaxChars: &zero}); err == nil {
		t.Fatal("zero notice max chars must fail")
	}
}

// TestSkillRetrievalPromptRail_空快照兜底
func TestSkillRetrievalPromptRail_空快照兜底(t *testing.T) {
	rail, err := NewSkillRetrievalPromptRail(RailOptions{ConfigBase: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := rail.emptyPromptSnapshot()
	if snapshot.Mode != "small" || snapshot.IndexState != "missing" || snapshot.CandidateBudgetTokens < 1 {
		t.Fatalf("empty snapshot shape wrong: %+v", snapshot)
	}
}

// TestBuildCandidateAppendix_中文全量与分支行
func TestBuildCandidateAppendix_中文全量与分支行(t *testing.T) {
	snapshot := discovery.SkillPromptSnapshot{
		Mode:       "small",
		TotalCount: 1,
		Entries: []discovery.SkillPromptEntry{
			{WorkerID: "pdf reader", Description: "reads pdfs"},
		},
		CandidateBudgetTokens: 2000,
		IndexState:            "missing",
	}
	cn := buildCandidateAppendix("cn", snapshot)
	if !strings.Contains(cn, "## 已安装 Skill") || !strings.Contains(cn, "`pdf reader`: reads pdfs") {
		t.Fatalf("cn appendix shape wrong:\n%s", cn)
	}
	en := buildCandidateAppendix("en", snapshot)
	if !strings.Contains(en, "## Installed Skills") || !strings.Contains(en, "Skills:") {
		t.Fatalf("en appendix shape wrong:\n%s", en)
	}
	// 空条目+空分支 → 无可用提示
	empty := buildCandidateAppendix("cn", discovery.SkillPromptSnapshot{Mode: "small", IndexState: "missing", CandidateBudgetTokens: 1})
	if !strings.Contains(empty, "当前没有可用 Skill。") {
		t.Fatalf("empty cn appendix wrong:\n%s", empty)
	}
	// large-flat 无条目 → 提示用 skill_index
	emptyLarge := buildCandidateAppendix("cn", discovery.SkillPromptSnapshot{Mode: "large-flat", IndexState: "missing", CandidateBudgetTokens: 1})
	if !strings.Contains(emptyLarge, "未展示示例") {
		t.Fatalf("large empty appendix wrong:\n%s", emptyLarge)
	}
}

// TestCompactBranchDescription_语义行压缩
func TestCompactBranchDescription_语义行压缩(t *testing.T) {
	desc := "Data tools\n\nSelect when: user needs csv\nCovers xlsx, csv\n\nDon't select when: images"
	got := compactBranchDescription(desc, "fallback")
	if strings.Contains(got, "Covers") || strings.Contains(got, "Don't select") {
		t.Fatalf("semantic lines should be dropped: %s", got)
	}
	if !strings.Contains(got, "Data tools") || !strings.Contains(got, "Select when: user needs csv") {
		t.Fatalf("primary+select when should survive: %s", got)
	}
	if !strings.Contains(got, "\\`") == false && strings.Contains(got, "`") {
		t.Fatal("backticks must be escaped")
	}
}

// TestCompactBranchDescription_空回落
func TestCompactBranchDescription_空回落(t *testing.T) {
	if got := compactBranchDescription("", "lab"); got != "lab" {
		t.Fatalf("want fallback got %s", got)
	}
}

// TestAppendToolResultReminder_三级适配
func TestAppendToolResultReminder_三级适配(t *testing.T) {
	// output 键优先
	inputs := &agentinterfaces.ToolCallInputs{ToolResult: map[string]any{"output": "body"}}
	appendToolResultReminder(inputs, "N")
	if inputs.ToolResult.(map[string]any)["output"] != "body\n\nN" {
		t.Fatalf("output append got %v", inputs.ToolResult)
	}
	// result 键次选
	inputs = &agentinterfaces.ToolCallInputs{ToolResult: map[string]any{"result": ""}}
	appendToolResultReminder(inputs, "N")
	if inputs.ToolResult.(map[string]any)["result"] != "N" {
		t.Fatalf("result append got %v", inputs.ToolResult)
	}
	// 均无 → system_reminder 键
	inputs = &agentinterfaces.ToolCallInputs{ToolResult: map[string]any{"other": 1}}
	appendToolResultReminder(inputs, "N")
	if inputs.ToolResult.(map[string]any)["system_reminder"] != "N" {
		t.Fatalf("system_reminder got %v", inputs.ToolResult)
	}
	// 纯字符串
	inputs = &agentinterfaces.ToolCallInputs{ToolResult: "plain"}
	appendToolResultReminder(inputs, "N")
	if inputs.ToolResult != "plain\n\nN" {
		t.Fatalf("string append got %v", inputs.ToolResult)
	}
	// nil → 不动
	inputs = &agentinterfaces.ToolCallInputs{ToolResult: nil}
	appendToolResultReminder(inputs, "N")
	if inputs.ToolResult != nil {
		t.Fatalf("nil result untouched got %v", inputs.ToolResult)
	}
}

// TestModelToolName_提取
func TestModelToolName_提取(t *testing.T) {
	if got := modelToolName(map[string]any{"function": map[string]any{"name": "f1"}}); got != "f1" {
		t.Fatalf("function name got %s", got)
	}
	if got := modelToolName(map[string]any{"name": "n1"}); got != "n1" {
		t.Fatalf("plain name got %s", got)
	}
}

// TestGetCallbacks_三事件注册
func TestGetCallbacks_三事件注册(t *testing.T) {
	root := newTestSkillRoot(t, "alpha")
	tk := newTestToolkit(t, root)
	rail, err := NewSkillRetrievalPromptRail(RailOptions{Toolkit: tk, ConfigBase: enableCfg()})
	if err != nil {
		t.Fatal(err)
	}
	callbacks := rail.GetCallbacks()
	for _, event := range []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterToolCall,
	} {
		if _, ok := callbacks[event]; !ok {
			t.Fatalf("missing callback %s", event)
		}
	}
}

// TestSkillRetrievalPromptRail_BeforeModelCall关闭路径恢复legacy
func TestSkillRetrievalPromptRail_BeforeModelCall关闭路径恢复legacy(t *testing.T) {
	rail, err := NewSkillRetrievalPromptRail(RailOptions{ConfigBase: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if rail.sessionEnabled {
		t.Fatal("switch off → session disabled")
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatal(err)
	}
}

// TestInit_异常路径空快照兜底（promptSkillFS 环境损坏场景由空表覆盖）
func TestInit_异常路径空快照兜底(t *testing.T) {
	rail, err := NewSkillRetrievalPromptRail(RailOptions{ConfigBase: enableCfg()})
	if err != nil {
		t.Fatal(err)
	}
	// 无 agent/toolkit/environment → init 走 empty snapshot 分支
	if err := rail.Init(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if rail.frozenPromptSnapshot == nil || rail.frozenPromptSnapshot.Mode != "small" {
		t.Fatalf("init should set empty snapshot, got %+v", rail.frozenPromptSnapshot)
	}
	// Uninit 后状态清空
	if err := rail.Uninit(nil); err != nil {
		t.Fatal(err)
	}
	if rail.systemPromptBuilder != nil || rail.frozenPromptSnapshot != nil {
		t.Fatal("uninit should clear state")
	}
}
