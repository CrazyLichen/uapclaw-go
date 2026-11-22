package rsi

// RsiProjector：单投影函数（事件 ↔ tree/进度/推送 同源；内部 v3 §4.4）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/projector.py（615 行）。
//
// 推/拉同源：DeriveProgress / DeriveTree / DeriveTreeDelta 都取自 nodes
// （根 + 派生节点投影缓存）与 metric（最新 progress.metric 快照）。
// 事件尽力而为、快照兜底（一致性 §8.6）：节点树持久化在内存 + tree.json
// 落盘，进程重启后由恢复通道（LoadFromDisk）重建。

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// projectorRoot 服务侧稳定根节点 ID（Python _ROOT）。
const projectorRoot = "ROOT"

// ──────────────────────────── 结构体 ────────────────────────────

// Projector 节点/进度投影（事件投影缓存 + 快照重建，推/拉一致）。
type Projector struct {
	tasksRoot string
	lock      sync.Mutex
	nodes     map[string]map[string]RsiTreeNode
	metric    map[string]map[string]any
	// refIndex 引擎侧稳定候选引用（node.created.node.ref）→ 服务侧 node_id
	// 索引。恢复通道反查前，本索引是 stage 事件定位的关键映射
	// （内部 v3 §4.4 投影规则：node_ref → N<序号>）。
	refIndex map[string]map[string]string
	// reconciledProgramStops 已执行过阈值停止对账的任务集合。
	reconciledProgramStops map[string]bool
}

// NewProjector 构造投影器。
func NewProjector(tasksRoot string) *Projector {
	return &Projector{
		tasksRoot:              tasksRoot,
		nodes:                  map[string]map[string]RsiTreeNode{},
		metric:                 map[string]map[string]any{},
		refIndex:               map[string]map[string]string{},
		reconciledProgramStops: map[string]bool{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// OnProviderNode Provider 完整节点投影（对齐 Python on_provider_node）：
// 不重编号保留原 ID，仅 provider 任务根 ID 归一为服务侧 ROOT。
// ⚠️ D-9-1 连带：Python 先经 artifact_adapter.provider_node_to_dict 转换
// （ARTIFACT 轨裁剪）；Go 侧 provider 节点已为 dict 形态直接投影。
func (p *Projector) OnProviderNode(taskID string, raw map[string]any) *RsiTreeNode {
	rawNodeID := valueToString(raw["node_id"])
	if rawNodeID == "" {
		return nil
	}
	nodeID := normalizeProviderNodeID(taskID, rawNodeID)
	node := &RsiTreeNode{
		NodeID:             nodeID,
		Iteration:          safeInt(raw["iteration"]),
		Type:               strings.ToUpper(firstNonEmpty(valueToString(raw["type"]), "REJECTED")),
		Adopted:            boolValue(raw["adopted"]),
		Score:              safeFloat(raw["score"]),
		Description:        optAnyStr(raw["description"]),
		SnapshotArtifactID: optAnyStr(raw["snapshot_artifact_id"]),
		FailureReason:      optAnyStr(raw["failure_reason"]),
		FailureClass:       optAnyStr(raw["failure_class"]),
	}
	if raw["parent_id"] != nil {
		pid := normalizeProviderNodeID(taskID, valueToString(raw["parent_id"]))
		node.ParentID = &pid
	}
	if changes, ok := raw["changes"].([]any); ok {
		node.Changes = toStringMaps(changes)
	}
	if extra, ok := raw["extra"].(map[string]any); ok {
		node.Extra = extra
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	p.ensureNodesLocked(taskID)[nodeID] = *node
	p.refIndex[taskID][nodeID] = nodeID
	p.refIndex[taskID][rawNodeID] = nodeID
	p.persistLocked(taskID)
	return node
}

// RegisterRoot 注册根节点（服务侧在任务创建/首跑时调用）。
func (p *Projector) RegisterRoot(taskID string, baseline *float64, description string) {
	p.lock.Lock()
	defer p.lock.Unlock()
	nodes := p.ensureNodesLocked(taskID)
	if _, ok := nodes[projectorRoot]; ok {
		return
	}
	nodes[projectorRoot] = RsiTreeNode{
		NodeID: projectorRoot, Iteration: 0, ParentID: nil,
		Type: "ROOT", Adopted: true, Score: baseline,
		Description: strPtr(description),
	}
	p.refIndex[taskID]["root"] = projectorRoot
	p.persistLocked(taskID)
}

// OnNodeCreated “node.created“ 事件 → 节点投影（内部 v3 §4.4 规则表）。
func (p *Projector) OnNodeCreated(taskID string, payload map[string]any) *RsiTreeNode {
	node, _ := payload["node"].(map[string]any)
	if node == nil {
		node = payload
	}
	nodeRef := ""
	if node != nil {
		nodeRef = valueToString(node["ref"])
	}
	if nodeRef == "" {
		return nil
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	nodes := p.ensureNodesLocked(taskID)
	// 服务侧稳定 ID：根=ROOT，其余 N<序号>（按派生序）。
	derivedSeq := 0
	for _, n := range nodes {
		if n.NodeID != projectorRoot {
			derivedSeq++
		}
	}
	nodeID := "N" + itoa(derivedSeq+1)
	if nodeRef == "root" {
		nodeID = projectorRoot
	}
	if existing, ok := nodes[nodeID]; ok {
		return &existing
	}
	parentID := p.mapParentID(taskID, node["parent_ref"])
	tree := RsiTreeNode{
		NodeID:        nodeID,
		Iteration:     len(nodes),
		ParentID:      parentID,
		Type:          mapType(node),
		Adopted:       boolValue(node["accepted"]),
		Score:         safeFloat(node["score"]),
		Description:   optStr(valueToString(node["summary"])),
		FailureReason: optStr(valueToString(node["failure_reason"])),
		FailureClass:  optStr(valueToString(node["failure_class"])),
	}
	if changes, ok := node["changes"].([]any); ok {
		tree.Changes = toStringMaps(changes)
	}
	nodes[nodeID] = tree
	p.refIndex[taskID][nodeRef] = nodeID
	p.persistLocked(taskID)
	return &tree
}

// OnNodeStage “node.stage“ 事件 → 同节点 description 动态更新（web §9.2）。
// 统一动态阶段语义：description 反映「当前」阶段（覆盖而非追加），阶段详情
// 落在 extra.stage。终态由后续整体替换节点，避免中间态文案残留（web §9.2）。
func (p *Projector) OnNodeStage(taskID string, payload map[string]any) *RsiTreeNode {
	nodeRef := valueToString(payload["node_ref"])
	if nodeRef == "" {
		return nil
	}
	stage, _ := payload["stage"].(map[string]any)
	if stage == nil {
		stage = map[string]any{}
	}
	stageName := valueToString(stage["name"])
	p.lock.Lock()
	defer p.lock.Unlock()
	nodes := p.nodes[taskID]
	if len(nodes) == 0 {
		return nil
	}
	node := p.resolveNodeLocked(taskID, nodeRef)
	if node == nil {
		return nil
	}
	if stageName != "" {
		node.Description = strPtr(stageName)
		extra := map[string]any{}
		if node.Extra != nil {
			for k, v := range node.Extra {
				extra[k] = v
			}
		}
		extra["stage"] = stage
		node.Extra = extra
	}
	p.persistLocked(taskID)
	return node
}

// OnProgressMetric “progress.metric“ → 最新值语义快照（可合并丢弃中间值）。
func (p *Projector) OnProgressMetric(taskID string, payload map[string]any) {
	p.lock.Lock()
	defer p.lock.Unlock()
	metric := p.ensureMetricLocked(taskID)
	for _, key := range []string{"iteration", "total_iterations", "score", "baseline", "node_ref", "metrics"} {
		if v, ok := payload[key]; ok {
			metric[key] = v
		}
	}
	if len(p.nodes[taskID]) > 0 {
		p.persistLocked(taskID)
	}
}

// DeriveProgress “rsi.task.get“ 的 progress（web §6.3）与 P2 推送同源。
// 真未知任务（无节点也无 metric 快照）才 404；仅有 metric 的合法中间态仍返回零值。
func (p *Projector) DeriveProgress(taskID string) (map[string]any, error) {
	p.lock.Lock()
	nodes := p.nodes[taskID]
	metric := p.metric[taskID]
	p.lock.Unlock()
	if nodes == nil && len(metric) == 0 {
		return nil, NewTaskNotFound(taskID)
	}
	iteration := 0
	if v, ok := metric["iteration"]; ok {
		iteration = safeInt(v)
	} else {
		for _, n := range nodes {
			if n.Type != "ROOT" && n.Type != "PROVISIONAL" {
				iteration++
			}
		}
	}
	return map[string]any{
		"iteration":        iteration,
		"total_iterations": safeInt(metric["total_iterations"]),
		"score":            safeFloat(metric["score"]),
		"baseline":         safeFloat(metric["baseline"]),
	}, nil
}

// DeriveTree “rsi.tree.get“ 全量出参（web §9.1）。
func (p *Projector) DeriveTree(taskID string) (map[string]any, error) {
	p.lock.Lock()
	nodes := p.nodes[taskID]
	metric := p.metric[taskID]
	p.lock.Unlock()
	if nodes == nil {
		return nil, NewTaskNotFound(taskID)
	}
	depth := 0
	for _, node := range nodes {
		d := 0
		visited := map[string]bool{}
		cursor := node
		for cursor.ParentID != nil && !visited[cursor.NodeID] {
			visited[cursor.NodeID] = true
			d++
			next, ok := nodes[*cursor.ParentID]
			if !ok {
				break
			}
			cursor = next
		}
		if d > depth {
			depth = d
		}
	}
	out := []map[string]any{}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if nodes[keys[i]].Iteration != nodes[keys[j]].Iteration {
			return nodes[keys[i]].Iteration < nodes[keys[j]].Iteration
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		out = append(out, nodes[k].ToDict())
	}
	return map[string]any{
		"nodes":     out,
		"depth":     depth,
		"iteration": safeInt(metric["iteration"]),
	}, nil
}

// DeriveTreeDelta P3 推送增量：基于事件而非文件 diff（内部 v3 §4.4）。
func (p *Projector) DeriveTreeDelta(taskID string, sinceNodeIDs map[string]bool) map[string]any {
	p.lock.Lock()
	nodes := p.nodes[taskID]
	p.lock.Unlock()
	if nodes == nil {
		nodes = map[string]RsiTreeNode{}
	}
	list := make([]RsiTreeNode, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Iteration < list[j].Iteration })
	out := []map[string]any{}
	for _, n := range list {
		if !sinceNodeIDs[n.NodeID] {
			out = append(out, n.ToDict())
		}
	}
	return map[string]any{"nodes": out}
}

// LoadFromDisk 读取 tree.json 一次并重建可丢弃内存缓存。
func (p *Projector) LoadFromDisk(taskID string) bool {
	path := p.treePath(taskID)
	p.lock.Lock()
	defer p.lock.Unlock()
	if _, ok := p.nodes[taskID]; ok {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		Nodes  []map[string]any `json:"nodes"`
		Metric map[string]any   `json:"metric"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	nodes := map[string]RsiTreeNode{}
	for _, item := range doc.Nodes {
		node := treeNodeFromWeb(item, taskID)
		if node != nil {
			nodes[node.NodeID] = *node
		}
	}
	if len(nodes) > 0 {
		p.nodes[taskID] = nodes
		idx := map[string]string{}
		for nodeID := range nodes {
			idx[nodeID] = nodeID
		}
		p.refIndex[taskID] = idx
	}
	if doc.Metric != nil {
		p.metric[taskID] = doc.Metric
	}
	p.ensureRootLocked(taskID)
	p.normalizeParentsLocked(taskID)
	return true
}

// ReconcileProgramThresholdStops 修正 solved 节点之后落盘的空回复拒绝节点。
// ⚠️ D-9-1 连带：阈值来源 program_threshold_provider._solved_threshold 属
// ARTIFACT 轨（随 17.3 回填）；本批 HARNESS 轨无 run/scorecard.json，前置守卫
// 恒返回 0——方法体保留对齐（事件序扫描 + 类型改写），阈值分支待 17.3。
func (p *Projector) ReconcileProgramThresholdStops(taskID string) int {
	p.lock.Lock()
	if p.reconciledProgramStops[taskID] {
		p.lock.Unlock()
		return 0
	}
	p.lock.Unlock()
	taskDir := filepath.Join(p.tasksRoot, taskID)
	eventsPath := filepath.Join(taskDir, "events.jsonl")
	cardPath := filepath.Join(taskDir, "run", "scorecard.json")
	if !fileExists(eventsPath) || !fileExists(cardPath) {
		return 0
	}
	// 阈值解析 _solved_threshold 属 program_threshold_provider（D-9-1，随 17.3 回填）：
	// 文件存在而阈值源缺失时与 Python 的 ImportError 兜底同样按 0 处理。
	return 0
}

// MergeProviderTree 对账 Provider 快照且不丢弃本地更富字段。
// Provider 快照是恢复/回填来源：事件投影（tree.json）拥有 staged 描述、
// 快照 ID 与详细 changes 等展示字段；搜索历史 append-only，部分快照不得删本地节点。
// ⚠️ D-9-1 连带：Python 先经 artifact_adapter.provider_tree_to_web 转换
// （ARTIFACT 轨裁剪）；Go 侧调用方直接传 web 格式 map（HARNESS 路径）。
func (p *Projector) MergeProviderTree(taskID string, raw map[string]any) (map[string]any, error) {
	rawNodes, _ := raw["nodes"].([]any)
	epochProjection := false
	for _, item := range rawNodes {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		extra, _ := m["extra"].(map[string]any)
		if extra != nil && valueToString(extra["iteration_unit"]) == "epoch" {
			epochProjection = true
		}
	}
	incoming := []*RsiTreeNode{}
	for _, item := range rawNodes {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if node := treeNodeFromWeb(m, taskID); node != nil {
			incoming = append(incoming, node)
		}
	}
	p.lock.Lock()
	nodes := p.ensureNodesLocked(taskID)
	p.ensureRootLocked(taskID)
	refIndex := p.refIndex[taskID]
	if epochProjection {
		// 旧 Harness 查询把候选投影成 G* 平级节点；仅规范 epoch 快照对这些任务权威。
		incomingIDs := map[string]bool{}
		for _, node := range incoming {
			incomingIDs[node.NodeID] = true
		}
		for nodeID := range nodes {
			if nodeID != projectorRoot && !incomingIDs[nodeID] {
				delete(nodes, nodeID)
			}
		}
		for ref, nodeID := range refIndex {
			if _, ok := nodes[nodeID]; !ok {
				delete(refIndex, ref)
			}
		}
	}
	for _, inc := range incoming {
		if inc.NodeID == projectorRoot {
			root := nodes[projectorRoot]
			if root.Score == nil && inc.Score != nil {
				root.Score = inc.Score
			}
			if (root.Description == nil || *root.Description == "") && inc.Description != nil {
				root.Description = inc.Description
			}
			continue
		}
		existing, ok := nodes[inc.NodeID]
		if ok {
			nodes[inc.NodeID] = mergeNode(existing, *inc)
		} else {
			nodes[inc.NodeID] = normalizeNode(*inc)
		}
		refIndex[inc.NodeID] = inc.NodeID
	}
	p.normalizeParentsLocked(taskID)
	metric := p.ensureMetricLocked(taskID)
	nodeIteration := 0
	for _, node := range nodes {
		if node.NodeID == projectorRoot || node.Type == "PROVISIONAL" || node.Type == "CANDIDATE" {
			continue
		}
		if node.Iteration > nodeIteration {
			nodeIteration = node.Iteration
		}
	}
	if epochProjection {
		metric["iteration"] = safeInt(raw["iteration"])
	} else {
		metric["iteration"] = maxInt(
			safeInt(metric["iteration"]),
			safeInt(raw["iteration"]),
			nodeIteration,
		)
	}
	p.persistLocked(taskID)
	p.lock.Unlock()
	// Python 在 with 块结束后才派生返回——先解锁再调 DeriveTree（避免重入死锁）。
	return p.DeriveTree(taskID)
}

// SyncProviderTree 兼容别名（对齐 Python sync_provider_tree）。
func (p *Projector) SyncProviderTree(taskID string, tree map[string]any) (map[string]any, error) {
	return p.MergeProviderTree(taskID, tree)
}

// PersistNodeUpdate 快照/描述等节点字段变更后回写落盘（单节点更新）。
func (p *Projector) PersistNodeUpdate(taskID string, node RsiTreeNode) {
	p.lock.Lock()
	defer p.lock.Unlock()
	nodes := p.nodes[taskID]
	if nodes == nil {
		return
	}
	if _, ok := nodes[node.NodeID]; !ok {
		return
	}
	nodes[node.NodeID] = node
	p.persistLocked(taskID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// treePath tree.json 落盘路径。
func (p *Projector) treePath(taskID string) string {
	return filepath.Join(p.tasksRoot, taskID, "tree.json")
}

// ensureNodesLocked 取/建任务节点表与 ref 索引。
func (p *Projector) ensureNodesLocked(taskID string) map[string]RsiTreeNode {
	if p.nodes[taskID] == nil {
		p.nodes[taskID] = map[string]RsiTreeNode{}
	}
	if p.refIndex[taskID] == nil {
		p.refIndex[taskID] = map[string]string{}
	}
	return p.nodes[taskID]
}

// ensureMetricLocked 取/建任务 metric 快照。
func (p *Projector) ensureMetricLocked(taskID string) map[string]any {
	if p.metric[taskID] == nil {
		p.metric[taskID] = map[string]any{}
	}
	return p.metric[taskID]
}

// persistLocked tree.json 原子落盘（schema_version=2，对齐 Python _persist_locked）。
func (p *Projector) persistLocked(taskID string) {
	if len(p.nodes[taskID]) == 0 {
		return
	}
	dir := filepath.Join(p.tasksRoot, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	out := []map[string]any{}
	for _, n := range p.nodes[taskID] {
		out = append(out, n.ToDict())
	}
	payload := map[string]any{
		"schema_version": 2,
		"task_id":        taskID,
		"nodes":          out,
		"metric":         p.metric[taskID],
		"saved_at":       UtcnowISO(),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, "tree.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dir, "tree.json"))
}

// ensureRootLocked 根节点存在性保障（对齐 Python _ensure_root_locked）。
func (p *Projector) ensureRootLocked(taskID string) {
	nodes := p.ensureNodesLocked(taskID)
	root, ok := nodes[projectorRoot]
	if !ok {
		nodes[projectorRoot] = RsiTreeNode{
			NodeID: projectorRoot, Iteration: 0, ParentID: nil,
			Type: "ROOT", Adopted: true, Score: nil,
			Description: strPtr("基线"),
		}
		return
	}
	root.Iteration = 0
	root.ParentID = nil
	root.Type = "ROOT"
	root.Adopted = true
	root.FailureReason = nil
	root.FailureClass = nil
	nodes[projectorRoot] = root
	p.refIndex[taskID]["root"] = projectorRoot
}

// normalizeParentsLocked 孤儿/自环父指针归一到根（对齐 Python）。
func (p *Projector) normalizeParentsLocked(taskID string) {
	nodes := p.nodes[taskID]
	for id, node := range nodes {
		if id == projectorRoot {
			continue
		}
		if node.ParentID == nil || *node.ParentID == id {
			root := projectorRoot
			node.ParentID = &root
			nodes[id] = node
			continue
		}
		if _, ok := nodes[*node.ParentID]; !ok {
			root := projectorRoot
			node.ParentID = &root
			nodes[id] = node
		}
	}
}

// mapParentID 引擎侧稳定引用 → 服务侧节点 ID 反查（node.created 先于子节点出现）。
func (p *Projector) mapParentID(taskID string, parentRef any) *string {
	nodes := p.nodes[taskID]
	ref := valueToString(parentRef)
	if ref == "" || strings.EqualFold(ref, "root") {
		if _, ok := nodes[projectorRoot]; ok {
			return strPtr(projectorRoot)
		}
		return nil
	}
	if mapped, ok := p.refIndex[taskID][ref]; ok {
		if _, ok := nodes[mapped]; ok {
			return strPtr(mapped)
		}
	}
	return nil
}

// resolveNodeLocked node_ref 定位节点（root/规范化 ref/refIndex 反查）。
func (p *Projector) resolveNodeLocked(taskID, nodeRef string) *RsiTreeNode {
	nodes := p.nodes[taskID]
	if len(nodes) == 0 {
		return nil
	}
	if strings.EqualFold(nodeRef, "root") {
		if n, ok := nodes[projectorRoot]; ok {
			return &n
		}
		return nil
	}
	normalized := normalizeProviderNodeID(taskID, nodeRef)
	if n, ok := nodes[normalized]; ok {
		return &n
	}
	if mapped, ok := p.refIndex[taskID][nodeRef]; ok {
		if n, ok := nodes[mapped]; ok {
			return &n
		}
	}
	return nil
}

// normalizeNode adopted 节点终态归一（对齐 Python _normalize_node）。
func normalizeNode(node RsiTreeNode) RsiTreeNode {
	if node.Adopted {
		node.Type = "ADOPTED"
		node.FailureReason = nil
		node.FailureClass = nil
	}
	return node
}

// mergeNode 本地节点与 Provider 快照合并（对齐 Python _merge_node 全分支）。
func mergeNode(local, provider RsiTreeNode) RsiTreeNode {
	if local.Extra != nil && local.Extra["threshold_stop_cancelled"] == true {
		// Provider 持久树把被放弃的模型等待记成空回复拒绝；服务侧先看到
		// 达标事件并落盘了更准确的停止判定，以本地为准。
		return local
	}
	if provider.Extra != nil && provider.Extra["iteration_unit"] == "epoch" {
		if provider.SnapshotArtifactID == nil {
			provider.SnapshotArtifactID = local.SnapshotArtifactID
		}
		if provider.Type == "PROVISIONAL" {
			if provider.Description == nil {
				provider.Description = local.Description
			}
			extra := map[string]any{}
			for k, v := range orMap(local.Extra) {
				extra[k] = v
			}
			for k, v := range orMap(provider.Extra) {
				extra[k] = v
			}
			provider.Extra = extra
		}
		return normalizeNode(provider)
	}
	adopted := local.Adopted || provider.Adopted
	extra := map[string]any{}
	for k, v := range orMap(local.Extra) {
		extra[k] = v
	}
	for k, v := range orMap(provider.Extra) {
		extra[k] = v
	}
	providerTerminalType := ""
	switch provider.Type {
	case "ADOPTED", "REJECTED", "PRUNED":
		providerTerminalType = provider.Type
	}
	paperExtra, _ := provider.Extra["paper"].(map[string]any)
	paperPending := provider.Type == "PROVISIONAL" && paperExtra != nil && valueToString(paperExtra["outcome"]) == "pending"
	merged := RsiTreeNode{
		NodeID:             local.NodeID,
		Iteration:          local.Iteration,
		ParentID:           local.ParentID,
		Type:               providerTerminalType,
		Adopted:            adopted,
		Score:              local.Score,
		Description:        local.Description,
		SnapshotArtifactID: local.SnapshotArtifactID,
		Changes:            local.Changes,
		Extra:              extra,
	}
	if local.Iteration <= 0 {
		merged.Iteration = provider.Iteration
	}
	if merged.Type == "" {
		if adopted {
			merged.Type = "ADOPTED"
		} else {
			merged.Type = firstNonEmpty(local.Type, provider.Type, "REJECTED")
		}
	} else if !adopted && merged.Type == "" {
		merged.Type = firstNonEmpty(local.Type, provider.Type, "REJECTED")
	}
	if merged.Score == nil {
		merged.Score = provider.Score
	}
	if paperPending && provider.Description != nil {
		merged.Description = provider.Description
	} else if merged.Description == nil {
		merged.Description = provider.Description
	}
	if merged.SnapshotArtifactID == nil {
		merged.SnapshotArtifactID = provider.SnapshotArtifactID
	}
	if merged.ParentID == nil {
		merged.ParentID = provider.ParentID
	}
	if !adopted {
		merged.FailureReason = firstStrPtr(local.FailureReason, provider.FailureReason)
		merged.FailureClass = firstStrPtr(local.FailureClass, provider.FailureClass)
	} else {
		merged.FailureReason = nil
		merged.FailureClass = nil
	}
	if merged.Changes == nil {
		merged.Changes = provider.Changes
	}
	return normalizeNode(merged)
}

// mapType node.created 载荷 outcome → 节点类型（对齐 Python _map_type）。
func mapType(node map[string]any) string {
	outcome := strings.ToUpper(valueToString(node["outcome"]))
	switch outcome {
	case "ADOPTED", "REJECTED", "PROVISIONAL", "PRUNED":
		return outcome
	}
	if boolValue(node["accepted"]) {
		return "ADOPTED"
	}
	return "REJECTED"
}

// treeNodeFromWeb web 格式字典 → RsiTreeNode（对齐 Python _tree_node_from_web）。
func treeNodeFromWeb(raw map[string]any, taskID string) *RsiTreeNode {
	rawNodeID := valueToString(raw["node_id"])
	if rawNodeID == "" {
		return nil
	}
	nodeID := normalizeProviderNodeID(taskID, rawNodeID)
	var parentID *string
	if raw["parent_id"] != nil {
		pid := normalizeProviderNodeID(taskID, valueToString(raw["parent_id"]))
		parentID = &pid
	}
	node := &RsiTreeNode{
		NodeID:             nodeID,
		Iteration:          safeInt(raw["iteration"]),
		ParentID:           parentID,
		Type:               strings.ToUpper(firstNonEmpty(valueToString(raw["type"]), "REJECTED")),
		Adopted:            boolValue(raw["adopted"]),
		Score:              safeFloat(raw["score"]),
		Description:        optAnyStr(raw["description"]),
		SnapshotArtifactID: optAnyStr(raw["snapshot_artifact_id"]),
		FailureReason:      optAnyStr(raw["failure_reason"]),
		FailureClass:       optAnyStr(raw["failure_class"]),
	}
	if changes, ok := raw["changes"].([]any); ok {
		node.Changes = toStringMaps(changes)
	}
	if extra, ok := raw["extra"].(map[string]any); ok {
		node.Extra = extra
	}
	return node
}

// normalizeProviderNodeID agent-core provider 根 ID 归一到服务侧 ROOT
// （对齐 Python _normalize_provider_node_id：h0 / artifact:<task>:root / :node:0）。
func normalizeProviderNodeID(taskID, nodeID string) string {
	if nodeID == "h0" {
		return projectorRoot
	}
	if taskID != "" && (nodeID == "artifact:"+taskID+":root" || nodeID == "artifact:"+taskID+":node:0") {
		return projectorRoot
	}
	return nodeID
}

// safeFloat 宽松转 float（畸形载荷防御，对齐 Python _safe_float）。
func safeFloat(value any) *float64 {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		return nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil
		}
		return &v
	case float32:
		f := float64(v)
		return &f
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case json.Number:
		f, err := v.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil
		}
		return &f
	default:
		return nil
	}
}

// safeInt 宽松转 int；nil/bool 返回 0（畸形引擎载荷防御，对齐 Python _safe_int）。
func safeInt(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case bool:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	default:
		return 0
	}
}

// ──────────────────────────── 通用小助手 ────────────────────────────

// itoa 小整数转字符串。
func itoa(v int) string {
	return fmtInt(v)
}

// fmtInt int → 十进制字符串。
func fmtInt(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// maxInt 多值取最大。
func maxInt(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// strPtr 字符串转指针。
func strPtr(s string) *string { return &s }

// optStr 空串转 nil（对齐 Python str(x) or None）。
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// optAnyStr any 取字符串后空串转 nil。
func optAnyStr(v any) *string { return optStr(valueToString(v)) }

// firstStrPtr 第一个非 nil 字符串指针。
func firstStrPtr(vals ...*string) *string {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// firstNonEmpty 第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// boolValue any → bool（nil 为 false）。
func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

// orMap nil map 归一空 map。
func orMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// toStringMaps []any → []map[string]string（仅收 dict 元素，对齐 Python）。
func toStringMaps(items []any) []map[string]string {
	out := []map[string]string{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			conv := map[string]string{}
			for k, v := range m {
				conv[k] = valueToString(v)
			}
			out = append(out, conv)
		}
	}
	return out
}

// fileExists 文件存在性判断。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
