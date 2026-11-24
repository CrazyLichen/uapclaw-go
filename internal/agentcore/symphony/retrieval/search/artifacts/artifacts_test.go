package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/common"
)

// TestLoadCatalogRecords 解析/过滤/排序/三键元数据
func TestLoadCatalogRecords(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "catalog.jsonl")
	content := `{"name": "nB", "cid": "c2", "worker_id": "w2", "description": "d2", "retrieval_text": "r2", "branch_path": ["x", "", "y"], "skill_path": "/s2", "category": "cat"}` + "\n" +
		`{"choice_id": "nA", "payload": "c1"}` + "\n" +
		`{"name": "", "cid": ""}` + "\n" // choice/payload 双空 → 剔除
	_ = os.WriteFile(p, []byte(content), 0o644)
	records, err := loadCatalogRecords(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("记录数不符: %d", len(records))
	}
	// 排序按 (payload, choice_id)：c1 在前
	if records[0].Payload != "c1" || records[1].Payload != "c2" {
		t.Fatalf("排序不符: %+v", records)
	}
	r2 := records[1]
	if r2.ChoiceID != "nB" || r2.WorkerID != "w2" || len(r2.BranchPath) != 2 {
		t.Fatalf("字段不符: %+v", r2)
	}
	if r2.Metadata["category"] != "cat" || r2.Metadata["worker_id"] != "w2" {
		t.Fatalf("三键元数据不符: %v", r2.Metadata)
	}
}

// TestBuildTreeRoot 树构建：leaf→items、分支递归、空剪枝、路由描述拼接
func TestBuildTreeRoot(t *testing.T) {
	nodes := []any{
		map[string]any{"cid": "1", "type": "branch", "description": "分组一"},
		map[string]any{"cid": "1.1", "type": "leaf", "description": "技能A", "select_when": "需要A", "dont_select_when": "禁A"},
		map[string]any{"cid": "1.2", "type": "leaf"},
		map[string]any{"cid": "2", "type": "branch", "description": "空分组"}, // 空分支剪除
	}
	root := BuildTreeRoot(nodes, nil)
	if root.NodeID != "ROOT" || root.Label != "ROOT" {
		t.Fatalf("根不符: %+v", root)
	}
	// 空分支 2 被剪除，只剩分支 1
	if len(root.Children) != 1 || root.Children[0].NodeID != "1" {
		t.Fatalf("空分支应剪除: %+v", root)
	}
	br := root.Children[0]
	if br.Label != "1" || len(br.Items) != 2 {
		t.Fatalf("分支结构不符: %+v", br)
	}
	// leaf 描述拼接
	item := br.Items[0]
	// Python label = record.name or item_id or lastSeg（描述不参与 label）
	if item.Label != "1" {
		t.Fatalf("leaf label 不符: %q", item.Label)
	}
	if !strings.Contains(item.Description, "Select when: 需要A") || !strings.Contains(item.Description, "Don't select when: 禁A") {
		t.Fatalf("路由描述拼接不符: %q", item.Description)
	}
	// 分支描述（无 leaf 的空描述）
	if br.Description != "分组一" {
		t.Fatalf("分支描述不符: %q", br.Description)
	}
}

// TestBuildTreeRoot_WithCatalog catalog 记录参与 leaf 命名与描述回落
func TestBuildTreeRoot_WithCatalog(t *testing.T) {
	// leaf 1.1 的 parent 分支节点必须存在（Python children_map 只收分支）
	nodes := []any{
		map[string]any{"cid": "1", "type": "branch"},
		map[string]any{"cid": "1.1", "type": "leaf"},
	}
	records := []CatalogRecord{{ChoiceID: "chA", Payload: "1.1", Name: "SkillA", Description: "描述A"}}
	root := BuildTreeRoot(nodes, records)
	if len(root.Children) != 1 || len(root.Children[0].Items) != 1 {
		t.Fatalf("leaf 应挂分支 1: %+v", root)
	}
	item := root.Children[0].Items[0]
	if item.ItemID != "SkillA" || item.Payload != "1.1" || item.Label != "SkillA" || item.Description != "描述A" {
		t.Fatalf("catalog 驱动 leaf 不符: %+v", item)
	}
}

// TestLoadRetrieverIndex 端到端加载
func TestLoadRetrieverIndex(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"mode": "full", "artifacts": {"catalog": "catalog.jsonl"}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "catalog.jsonl"), []byte(`{"name": "SkillA", "cid": "1.1", "worker_id": "wa"}`+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "tree_index.yaml"), []byte("nodes:\n  - cid: 1\n    type: branch\n  - cid: 1.1\n    type: leaf\n    description: dA\n    worker_id: wtree\n"), 0o644)

	idx, err := LoadRetrieverIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if idx.IndexDir == "" || len(idx.CatalogRecords) != 1 || len(idx.Choices) != 1 {
		t.Fatalf("索引加载不符: %+v", idx)
	}
	// 树 worker_id 回填目录记录
	if idx.CatalogRecords[0].WorkerID != "wtree" || idx.CatalogRecords[0].Metadata["worker_id"] != "wtree" {
		t.Fatalf("worker_id 回填不符: %+v", idx.CatalogRecords[0])
	}
	if idx.Choices[0].ChoiceID != "SkillA" || idx.Choices[0].Payload != "1.1" {
		t.Fatalf("choices 不符: %+v", idx.Choices)
	}
	if idx.TreeRoot.NodeID != "ROOT" || len(idx.TreeRoot.Children) != 1 || len(idx.TreeRoot.Children[0].Items) != 1 {
		t.Fatalf("树根不符: %+v", idx.TreeRoot)
	}
	// manifest.mode
	if idx.Manifest["mode"] != "full" {
		t.Fatalf("manifest 不符: %v", idx.Manifest)
	}
}

// TestLoadRetrieverIndex_S3 D-16-4-1 登记错误
func TestLoadRetrieverIndex_S3(t *testing.T) {
	if _, err := LoadRetrieverIndex("s3://bucket/idx"); err == nil || !strings.Contains(err.Error(), "D-16-4-1") {
		t.Fatalf("S3 目录应返回登记错误: %v", err)
	}
}

// TestLoadRetrieverIndex_缺catalog 错误传播
func TestLoadRetrieverIndex_缺catalog(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadRetrieverIndex(dir); err == nil {
		t.Fatal("缺 catalog 文件应报错")
	}
}

// TestLoadTreeWorkerIDs 无 worker_id 节点忽略
func TestLoadTreeWorkerIDs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.yaml")
	_ = os.WriteFile(p, []byte("nodes:\n  - cid: 1\n    worker_id: w1\n  - cid: 2\n    type: leaf\n"), 0o644)
	m, err := loadTreeWorkerIDs(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["1"] != "w1" {
		t.Fatalf("映射不符: %v", m)
	}
}

// TestApplyTreeWorkerIDs 回填与保留
func TestApplyTreeWorkerIDs(t *testing.T) {
	records := []CatalogRecord{
		{ChoiceID: "a", Payload: "1", WorkerID: "", Metadata: map[string]any{}},
		{ChoiceID: "b", Payload: "2", WorkerID: "orig", Metadata: map[string]any{}},
		{ChoiceID: "c", Payload: "3", WorkerID: "", Metadata: map[string]any{}},
	}
	got := applyTreeWorkerIDs(records, map[string]string{"1": "w1"})
	if got[0].WorkerID != "w1" || got[0].Metadata["worker_id"] != "w1" {
		t.Fatalf("回填不符: %+v", got[0])
	}
	// 无映射的记录保留原样（含 b 保留 orig）
	if got[1].WorkerID != "orig" || got[2].WorkerID != "" {
		t.Fatalf("未映射记录应保留: %+v", got)
	}
	// 空映射 → 原列表
	same := applyTreeWorkerIDs(records, map[string]string{})
	if len(same) != 3 {
		t.Fatalf("空映射应原样返回")
	}
}

// TestComposeRoutingDescription 防重复拼接
func TestComposeRoutingDescription(t *testing.T) {
	// 已含 "Select when:" 时不再追加
	got := composeRoutingDescription("Select when: 已有", "新增", "")
	if strings.Count(got, "Select when:") != 1 {
		t.Fatalf("防重复失败: %q", got)
	}
	// 全空 → 空串
	if composeRoutingDescription("", "", "") != "" {
		t.Fatal("全空应返回空串")
	}
}

// TestParseSimpleNodesYAML_artifacts 行式解析
func TestParseSimpleNodesYAML_artifacts(t *testing.T) {
	out := parseSimpleNodesYAML("nodes:\n- cid: 1\n  type: leaf\n- cid: 2\n  type: leaf\n")
	nodes := out["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("节点数不符: %v", nodes)
	}
	if nodes[0].(map[string]any)["cid"] != "1" {
		t.Fatalf("字段不符: %v", nodes[0])
	}
}

// TestLoadYAMLLike 优先级
func TestLoadYAMLLike(t *testing.T) {
	out := loadYAMLLike("nodes:\n- cid: x\n")
	if out["nodes"] == nil {
		t.Fatal("YAML 应解析成功")
	}
}

// TestHitsToSearchResult 组装语义
func TestHitsToSearchResult(t *testing.T) {
	records := []map[string]any{
		{"choice_id": "c1", "resolved_payload": "p1"},
		{"raw_output": "c2raw", "resolved_payload": "p2"},
	}
	got := HitsToSearchResult("bm25", "index", 12.5, []map[string]any{{"e": 1}}, records)
	if len(got.Payloads) != 2 || got.Payloads[0] != "p1" {
		t.Fatalf("payloads 不符: %v", got.Payloads)
	}
	if got.SelectedPayload != "p1" || got.SelectedRank != 1 {
		t.Fatalf("首选不符: %+v", got)
	}
	if got.SummaryLines[0] != "1. c1 -> p1 (source=index)" {
		t.Fatalf("摘要行不符: %q", got.SummaryLines[0])
	}
	if got.SummaryLines[1] != "2. c2raw -> p2 (source=index)" {
		t.Fatalf("raw_output 回落不符: %q", got.SummaryLines[1])
	}
	if got.ElapsedMs != 12.5 || len(got.TraceEvents) != 1 {
		t.Fatalf("元数据不符: %+v", got)
	}
	// 空候选：selected 空、rank -1
	empty := HitsToSearchResult("m", "s", 0, nil, nil)
	if empty.SelectedPayload != "" || empty.SelectedRank != -1 {
		t.Fatalf("空候选语义不符: %+v", empty)
	}
}

// TestSearchResultStruct 字段面（对齐 service/models.py SearchResult）
func TestSearchResultStruct(t *testing.T) {
	r := SearchResult{Method: "m"}
	if r.Method != "m" || r.SelectedRank != 0 {
		t.Fatalf("零值不符: %+v", r)
	}
	var _ = common.RetrieverNode{} // common 包引用保持
	var _ = json.Marshal          // json 引用保持
}
