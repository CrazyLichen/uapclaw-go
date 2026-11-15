package harnessrsi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSchema_TeamIssue_序列化往返 JSON 序列化字段名 snake_case 对齐 dataclass
func TestSchema_TeamIssue_序列化往返(t *testing.T) {
	issue := TeamIssue{
		IssueID:   "i-1",
		Category:  "tool",
		Severity:  "high",
		Summary:   "工具缺失",
		AffectedCases: []string{"c1", "c2"},
		Evidence:  []map[string]any{{"kind": "trace"}},
	}
	raw, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip TeamIssue
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.IssueID != "i-1" || roundTrip.Severity != "high" {
		t.Fatalf("round-trip mismatch: %+v", roundTrip)
	}
	for _, key := range []string{"issue_id", "affected_cases", "suspected_team_scope", "optimization_target", "target_members"} {
		if !strings.Contains(string(raw), `"key"`) && !strings.Contains(string(raw), key) {
			t.Fatalf("missing snake_case key %q in %s", key, raw)
		}
	}
}

// TestSchema_ActionDefinition_字段默认值 零值结构体 12 字段默认与 Python dataclass 对齐
func TestSchema_ActionDefinition_字段默认值(t *testing.T) {
	action := ActionDefinition{Name: "swap_tool", Group: "g", Operation: "op", Function: "fn"}
	if action.RequiresSearch || action.RequiresInstall || action.IsDestructive {
		t.Fatal("bool fields must default false")
	}
	if action.OptimizableModules != nil || action.DependencyResources != nil || action.ParametersSchema != nil || action.ValidationRules != nil {
		t.Fatal("collection fields must default to zero (Python default_factory produces empty lists; Go uses nil)")
	}
}

// TestSchema_DataArtifact_可选指针 Cases/Score 为可选字段（nil 序列化时省略）
func TestSchema_DataArtifact_可选指针(t *testing.T) {
	artifact := DatasetArtifact{DatasetID: "d", DatasetDir: "/tmp", DatasetFiles: []string{"a.json"}}
	raw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"cases"`) {
		t.Fatalf("nil Cases must be omitted: %s", raw)
	}
}

// TestProgress_EventDefaults 进度事件 9 字段默认值与回调类型可用
func TestProgress_EventDefaults(t *testing.T) {
	event := OptimizationProgressEvent{Phase: "evaluate"}
	if event.Epoch != 0 || event.BatchIndex != 0 || event.Stage != "" || event.Message != "" {
		t.Fatal("defaults mismatch")
	}
	if event.Score != nil || event.Improved != nil || event.Metrics != nil || event.Artifacts != nil {
		t.Fatal("optional fields must default nil")
	}
	var callback ProgressCallback = func(OptimizationProgressEvent) {}
	var decision SeedOptimizationDecisionCallback = func(map[string]any) (bool, error) { return true, nil }
	callback(event)
	if ok, err := decision(nil); !ok || err != nil {
		t.Fatalf("decision callback failed: %v %v", ok, err)
	}
}
