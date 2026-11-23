package rsi

// projector.go 单测：节点投影规则、进度/树派生、增量、恢复重建、合并。

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestRegisterRoot与派生序 根=ROOT、派生节点 N<序号>。
func TestRegisterRoot与派生序(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t1", nil, "基线")
	n1 := p.OnNodeCreated("t1", map[string]any{"node": map[string]any{
		"ref": "cand-1", "outcome": "ADOPTED", "accepted": true, "score": 0.9, "summary": "首个候选"}})
	if n1 == nil || n1.NodeID != "N1" || n1.Type != "ADOPTED" || n1.ParentID == nil || *n1.ParentID != "ROOT" {
		t.Fatalf("投影漂移: %+v", n1)
	}
	// 同 ref 二次事件：派生序计数生成新节点（对齐 Python：仅 node_id 碰撞才幂等）。
	if second := p.OnNodeCreated("t1", map[string]any{"node": map[string]any{"ref": "cand-1"}}); second.NodeID != "N2" {
		t.Fatalf("派生序漂移: %s", second.NodeID)
	}
	// ref="root" 映射到 ROOT，已存在 → 返回缓存节点（Python node_id 幂等路径）。
	if root := p.OnNodeCreated("t1", map[string]any{"node": map[string]any{"ref": "root"}}); root.NodeID != "ROOT" {
		t.Fatalf("root 幂等漂移: %s", root.NodeID)
	}
	// 缺 ref 返回 nil。
	if p.OnNodeCreated("t1", map[string]any{"node": map[string]any{"outcome": "REJECTED"}}) != nil {
		t.Fatalf("缺 ref 应为 nil")
	}
}

// TestMapType_outcome优先 对齐 Python _map_type：outcome 大写命中优先，否则 accepted 兜底。
func TestMapType_outcome优先(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	if n := p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "pruned"}}); n.Type != "PRUNED" {
		t.Fatalf("got %s", n.Type)
	}
	if n := p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "b", "accepted": false}}); n.Type != "REJECTED" {
		t.Fatalf("got %s", n.Type)
	}
}

// TestDeriveProgress_metric快照与404 真未知 404；仅 metric 中间态零值。
func TestDeriveProgress_metric快照与404(t *testing.T) {
	p := NewProjector(t.TempDir())
	if _, err := p.DeriveProgress("ghost"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("应 404: %v", err)
	}
	// 仅 metric（root 注册前 P2 先到）不 404。
	p.OnProgressMetric("t", map[string]any{"iteration": 2, "total_iterations": 10, "score": 0.8, "baseline": 0.5})
	got, err := p.DeriveProgress("t")
	if err != nil {
		t.Fatal(err)
	}
	if got["iteration"] != 2 || got["total_iterations"] != 10 {
		t.Fatalf("metric 漂移: %v", got)
	}
}

// TestDeriveProgress_节点计数缺省 无 metric 时按非 ROOT/PROVISIONAL 节点数计数。
func TestDeriveProgress_节点计数缺省(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "ADOPTED"}})
	got, _ := p.DeriveProgress("t")
	if got["iteration"] != 1 {
		t.Fatalf("计数漂移: %v", got)
	}
}

// TestOnNodeStage_覆盖式描述 对齐 web §9.2：覆盖而非追加，详情入 extra.stage。
func TestOnNodeStage_覆盖式描述(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "REJECTED", "summary": "旧描述"}})
	node := p.OnNodeStage("t", map[string]any{
		"node_ref": "a", "stage": map[string]any{"name": "执行手术中", "detail": "x"}})
	if node == nil || node.Description == nil || *node.Description != "执行手术中" {
		t.Fatalf("阶段覆盖漂移: %+v", node)
	}
	if node.Extra["stage"].(map[string]any)["detail"] != "x" {
		t.Fatalf("extra.stage 漂移")
	}
}

// TestDeriveTreeDelta_基于事件 P3 增量排除已知节点。
func TestDeriveTreeDelta_基于事件(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "ADOPTED"}})
	delta := p.DeriveTreeDelta("t", map[string]bool{"ROOT": true, "N1": true})
	if len(delta["nodes"].([]map[string]any)) != 0 {
		t.Fatalf("增量应为空")
	}
	delta = p.DeriveTreeDelta("t", map[string]bool{"ROOT": true})
	if len(delta["nodes"].([]map[string]any)) != 1 {
		t.Fatalf("应含 N1")
	}
}

// TestLoadFromDisk_恢复重建 落盘 → 新投影器重建（含根保障/父指针归一）。
func TestLoadFromDisk_恢复重建(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "ADOPTED", "accepted": true}})
	p.OnProgressMetric("t", map[string]any{"iteration": 1})
	if _, err := os.Stat(filepath.Join(root, "t", "tree.json")); err != nil {
		t.Fatalf("tree.json 未落盘: %v", err)
	}
	// 新投影器从磁盘重建。
	p2 := NewProjector(root)
	if !p2.LoadFromDisk("t") {
		t.Fatalf("恢复失败")
	}
	tree, err := p2.DeriveTree("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree["nodes"].([]map[string]any)) != 2 {
		t.Fatalf("重建节点数漂移: %d", len(tree["nodes"].([]map[string]any)))
	}
	// 已缓存时 LoadFromDisk 恒 true（Python 判 task_id in nodes）。
	if !p2.LoadFromDisk("t") {
		t.Fatalf("缓存命中应 true")
	}
	// 文件不存在 false。
	if p2.LoadFromDisk("ghost") {
		t.Fatalf("缺失应 false")
	}
}

// TestMergeProviderTree_epoch权威 epoch 投影下本地未知节点被清除。
func TestMergeProviderTree_epoch权威(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "stale", "outcome": "REJECTED"}})
	tree, err := p.MergeProviderTree("t", map[string]any{
		"iteration": 3,
		"nodes": []any{
			map[string]any{"node_id": "ROOT", "type": "ROOT", "adopted": true, "score": 0.4},
			map[string]any{"node_id": "N1", "type": "ADOPTED", "adopted": true,
				"iteration": 1, "extra": map[string]any{"iteration_unit": "epoch"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes := tree["nodes"].([]map[string]any)
	if len(nodes) != 2 {
		t.Fatalf("epoch 权威清除漂移: %d", len(nodes))
	}
	if tree["iteration"] != 3 {
		t.Fatalf("epoch iteration 漂移: %v", tree["iteration"])
	}
}

// TestMergeProviderTree_部分快照不删本地 非 epoch 投影 append-only。
func TestMergeProviderTree_部分快照不删本地(t *testing.T) {
	p := NewProjector(t.TempDir())
	p.RegisterRoot("t", nil, "")
	p.OnNodeCreated("t", map[string]any{"node": map[string]any{"ref": "a", "outcome": "ADOPTED"}})
	if _, err := p.MergeProviderTree("t", map[string]any{"nodes": []any{}}); err != nil {
		t.Fatal(err)
	}
	tree, _ := p.DeriveTree("t")
	if len(tree["nodes"].([]map[string]any)) != 2 {
		t.Fatalf("本地节点被误删")
	}
}

// TestMergeNode_thresholdStop本地优先 Provider 快照不得覆盖服务侧停止判定。
func TestMergeNode_thresholdStop本地优先(t *testing.T) {
	local := RsiTreeNode{NodeID: "N1", Type: "PRUNED",
		Extra: map[string]any{"threshold_stop_cancelled": true}, Description: strPtr("已达标，停止此候选")}
	provider := RsiTreeNode{NodeID: "N1", Type: "REJECTED", Description: strPtr("空回复拒绝")}
	merged := mergeNode(local, provider)
	if merged.Description == nil || *merged.Description != "已达标，停止此候选" {
		t.Fatalf("本地判定被覆盖")
	}
}

// TestNormalizeProviderNodeID h0 与 artifact 根归一。
func TestNormalizeProviderNodeID(t *testing.T) {
	if normalizeProviderNodeID("t", "h0") != "ROOT" {
		t.Fatalf("h0 归一漂移")
	}
	if normalizeProviderNodeID("t", "artifact:t:root") != "ROOT" {
		t.Fatalf("artifact root 归一漂移")
	}
	if normalizeProviderNodeID("t", "artifact:t:node:0") != "ROOT" {
		t.Fatalf("artifact node0 归一漂移")
	}
	if normalizeProviderNodeID("t", "N3") != "N3" {
		t.Fatalf("普通 ID 不应改写")
	}
}

// TestSafeInt_float防御 对齐 Python：bool/None → 0，float 可转。
func TestSafeInt_float防御(t *testing.T) {
	if safeInt(nil) != 0 || safeInt(true) != 0 {
		t.Fatalf("nil/bool 应 0")
	}
	var num json.Number = "7"
	if safeInt(num) != 7 || safeInt(3.9) != 3 {
		t.Fatalf("数值转换漂移")
	}
}

// TestSafeFloat_非有限值 nil 对齐 Python math.isfinite 守卫。
func TestSafeFloat_非有限值(t *testing.T) {
	if safeFloat(math.Inf(1)) != nil {
		t.Fatalf("Inf 应 nil")
	}
	if safeFloat(0.5) == nil {
		t.Fatalf("有限值不应 nil")
	}
}

// TestReconcileProgramThresholdStops_HARNESS空操作 D-9-1：无 scorecard 恒 0。
func TestReconcileProgramThresholdStops_HARNESS空操作(t *testing.T) {
	p := NewProjector(t.TempDir())
	if got := p.ReconcileProgramThresholdStops("t"); got != 0 {
		t.Fatalf("HARNESS 任务应 0: %d", got)
	}
}
