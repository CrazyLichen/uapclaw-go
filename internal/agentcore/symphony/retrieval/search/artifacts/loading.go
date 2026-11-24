// loading.go 对齐 Python search/artifacts/loading.py（274 行）。
// S3 物化分支未实现（D-16-4-1）：is_s3_uri 判定保留，S3 目录返回登记错误。
package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build/io"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/common"
	"gopkg.in/yaml.v3"
)

// CatalogRecord 检索候选目录记录（choice_id/payload 视角）。
// 对齐 Python loading.py:12-21（与 build.CatalogRecord 同名异义，勿混淆）。
type CatalogRecord struct {
	// ChoiceID 选择标识（name/choice_id 回落）
	ChoiceID string
	// Payload 负载（cid/payload 回落）
	Payload string
	// WorkerID 技能标识（缺省空）
	WorkerID string
	// Name 名称（缺省空）
	Name string
	// Description 描述（缺省空）
	Description string
	// RetrievalText 检索文本（缺省空）
	RetrievalText string
	// BranchPath 分支路径（缺省空）
	BranchPath []string
	// Metadata 元数据（skill_path/category/worker_id 三键契约）
	Metadata map[string]any
}

// LoadedRetrieverIndex 加载完成的检索索引
type LoadedRetrieverIndex struct {
	// IndexDir 索引目录
	IndexDir string
	// TreeRoot 树根
	TreeRoot common.RetrieverNode
	// Choices 选择项
	Choices []common.RetrieverChoice
	// CatalogRecords 目录记录
	CatalogRecords []CatalogRecord
	// Manifest 清单
	Manifest map[string]any
}

// LoadRetrieverIndex 加载索引目录：manifest → catalog → tree worker_id 回填 → 树构建 → choices。
// 对齐 Python load_retriever_index（loading.py:33-55）。
func LoadRetrieverIndex(indexDir string) (*LoadedRetrieverIndex, error) {
	baseDir, err := materializeIndexDir(indexDir)
	if err != nil {
		return nil, err
	}
	manifest, err := loadManifestFile(baseDir)
	if err != nil {
		return nil, err
	}
	catalogPath := filepath.Join(baseDir, artifactPath(manifest, "catalog", "catalog.jsonl"))
	treePath := filepath.Join(baseDir, artifactPath(manifest, "tree_index", "tree_index.yaml"))
	catalogRecords, err := loadCatalogRecords(catalogPath)
	if err != nil {
		return nil, err
	}
	workerIDsByCID, err := loadTreeWorkerIDs(treePath)
	if err != nil {
		return nil, err
	}
	catalogRecords = applyTreeWorkerIDs(catalogRecords, workerIDsByCID)
	treeRoot, err := LoadTreeRoot(treePath, catalogRecords)
	if err != nil {
		return nil, err
	}
	choices := make([]common.RetrieverChoice, 0, len(catalogRecords))
	for _, record := range catalogRecords {
		desc := record.Description
		if desc == "" {
			desc = record.RetrievalText
		}
		choices = append(choices, common.RetrieverChoice{
			ChoiceID:    record.ChoiceID,
			Payload:     record.Payload,
			Description: desc,
		})
	}
	return &LoadedRetrieverIndex{
		IndexDir:       baseDir,
		TreeRoot:       treeRoot,
		Choices:        choices,
		CatalogRecords: catalogRecords,
		Manifest:       manifest,
	}, nil
}

// materializeIndexDir 索引目录物化（本地 resolve；S3 登记错误）。
// 对齐 Python _materialize_index_dir（loading.py:58-62）。
func materializeIndexDir(indexDir string) (string, error) {
	raw := strings.TrimSpace(indexDir)
	if io.IsS3URI(raw) {
		return "", fmt.Errorf("s3 index dir not implemented in Go (deviation D-16-4-1): %s", raw)
	}
	abs, err := filepath.Abs(indexDir)
	if err != nil {
		return indexDir, nil
	}
	return abs, nil
}

// loadCatalogRecords 读 catalog（choice_id/payload 视角）并按 (payload, choice_id) 排序。
// 对齐 Python load_catalog_records（loading.py:65-95）。
func loadCatalogRecords(path string) ([]CatalogRecord, error) {
	var records []CatalogRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return records, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			return nil, err
		}
		choiceID := strings.TrimSpace(firstStr(payload, "name", "choice_id"))
		candidatePayload := strings.TrimSpace(firstStr(payload, "cid", "payload"))
		if choiceID == "" || candidatePayload == "" {
			continue
		}
		name := strings.TrimSpace(firstStr(payload, "name", ""))
		if name == "" {
			name = choiceID
		}
		var branchPath []string
		if bp, ok := payload["branch_path"].([]any); ok {
			for _, item := range bp {
				s := strings.TrimSpace(toStringValue(item))
				if s != "" {
					branchPath = append(branchPath, s)
				}
			}
		}
		records = append(records, CatalogRecord{
			ChoiceID:      choiceID,
			Payload:       candidatePayload,
			WorkerID:      strings.TrimSpace(firstStr(payload, "worker_id", "")),
			Name:          name,
			Description:   strings.TrimSpace(firstStr(payload, "description", "")),
			RetrievalText: strings.TrimSpace(firstStr(payload, "retrieval_text", "")),
			BranchPath:    branchPath,
			Metadata: map[string]any{
				"skill_path": strings.TrimSpace(firstStr(payload, "skill_path", "")),
				"category":   strings.TrimSpace(firstStr(payload, "category", "")),
				"worker_id":  strings.TrimSpace(firstStr(payload, "worker_id", "")),
			},
		})
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Payload != records[j].Payload {
			return records[i].Payload < records[j].Payload
		}
		return records[i].ChoiceID < records[j].ChoiceID
	})
	return records, nil
}

// LoadTreeRoot 从 tree_index.yaml 构建（nodes + catalog）。
// 对齐 Python load_tree_root（loading.py:98-102）。
func LoadTreeRoot(path string, catalogRecords []CatalogRecord) (common.RetrieverNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return common.RetrieverNode{}, err
	}
	payload := loadYAMLLike(string(data))
	rawNodes, _ := payload["nodes"].([]any)
	return BuildTreeRoot(rawNodes, catalogRecords), nil
}

// BuildTreeRoot 从内存 taxonomy 节点构建标准检索树。
// 对齐 Python build_tree_root（loading.py:105-109）。
func BuildTreeRoot(nodes []any, catalogRecords []CatalogRecord) common.RetrieverNode {
	recordByPayload := map[string]CatalogRecord{}
	for _, r := range catalogRecords {
		recordByPayload[r.Payload] = r
	}
	return buildTreeFromNodes(nodes, recordByPayload)
}

// loadTreeWorkerIDs 从 tree_index.yaml 提取 cid→worker_id 映射。
// 对齐 Python _load_tree_worker_ids（loading.py:112-123）。
func loadTreeWorkerIDs(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload := loadYAMLLike(string(data))
	out := map[string]string{}
	rawNodes, _ := payload["nodes"].([]any)
	for _, rawNode := range rawNodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			continue
		}
		cid := strings.TrimSpace(firstStr(node, "cid", ""))
		workerID := strings.TrimSpace(firstStr(node, "worker_id", ""))
		if cid != "" && workerID != "" {
			out[cid] = workerID
		}
	}
	return out, nil
}

// applyTreeWorkerIDs 用树的 worker_id 回填目录记录（replace 语义）。
// 对齐 Python _apply_tree_worker_ids（loading.py:126-140）。
func applyTreeWorkerIDs(records []CatalogRecord, workerIDsByCID map[string]string) []CatalogRecord {
	if len(workerIDsByCID) == 0 {
		return records
	}
	updated := make([]CatalogRecord, 0, len(records))
	for _, record := range records {
		workerID := strings.TrimSpace(record.WorkerID)
		if v, ok := workerIDsByCID[record.Payload]; ok && strings.TrimSpace(v) != "" {
			workerID = strings.TrimSpace(v)
		}
		if workerID == "" {
			updated = append(updated, record)
			continue
		}
		metadata := map[string]any{}
		for k, v := range record.Metadata {
			metadata[k] = v
		}
		metadata["worker_id"] = workerID
		record.WorkerID = workerID
		record.Metadata = metadata
		updated = append(updated, record)
	}
	return updated
}

// buildTreeFromNodes 节点树构建（leaf→items、分支递归、空分支剪除）。
// 对齐 Python _build_tree_from_nodes（loading.py:143-205）。
func buildTreeFromNodes(nodes []any, recordByPayload map[string]CatalogRecord) common.RetrieverNode {
	branchSpecs := map[string]map[string]any{}
	childrenMap := map[string][]string{}
	itemsByParent := map[string][]common.RetrieverItem{}

	for _, rawNode := range nodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			continue
		}
		cid := strings.TrimSpace(firstStr(node, "cid", ""))
		nodeType := strings.ToLower(strings.TrimSpace(firstStr(node, "type", "")))
		if cid == "" {
			continue
		}
		parentCID := ""
		if idx := strings.LastIndex(cid, "."); idx >= 0 {
			parentCID = cid[:idx]
		}
		if nodeType == "leaf" {
			record := recordByPayload[cid]
			lastSeg := cid
			if idx := strings.LastIndex(cid, "."); idx >= 0 {
				lastSeg = cid[idx+1:]
			}
			itemID := lastSeg
			if record.Name != "" {
				itemID = record.Name
			} else if record.ChoiceID != "" {
				itemID = record.ChoiceID
			}
			itemID = strings.TrimSpace(itemID)
			label := lastSeg
			if record.Name != "" {
				label = strings.TrimSpace(record.Name)
			}
			if label == "" {
				label = itemID
			}
			if label == "" {
				label = cid
			}
			desc := strings.TrimSpace(firstStr(node, "description", ""))
			if desc == "" && record.Description != "" {
				desc = strings.TrimSpace(record.Description)
			}
			itemsByParent[parentCID] = append(itemsByParent[parentCID], common.RetrieverItem{
				ItemID:      itemID,
				Payload:     cid,
				Label:       label,
				Description: composeRoutingDescription(desc, strings.TrimSpace(firstStr(node, "select_when", "")), strings.TrimSpace(firstStr(node, "dont_select_when", ""))),
			})
			continue
		}
		branchSpecs[cid] = node
		childrenMap[parentCID] = append(childrenMap[parentCID], cid)
	}

	var buildBranch func(cid string, root bool) *common.RetrieverNode
	buildBranch = func(cid string, root bool) *common.RetrieverNode {
		childIDs := append([]string(nil), childrenMap[cid]...)
		sort.Strings(childIDs)
		var children []common.RetrieverNode
		for _, childCID := range childIDs {
			if child := buildBranch(childCID, false); child != nil {
				children = append(children, *child)
			}
		}
		items := append([]common.RetrieverItem(nil), itemsByParent[cid]...)
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Payload != items[j].Payload {
				return items[i].Payload < items[j].Payload
			}
			return items[i].ItemID < items[j].ItemID
		})
		if !root && len(children) == 0 && len(items) == 0 {
			return nil
		}
		spec := branchSpecs[cid]
		label := ""
		if root {
			label = "ROOT"
		} else {
			label = cid
			if idx := strings.LastIndex(cid, "."); idx >= 0 {
				label = cid[idx+1:]
			}
			if cid == "" {
				label = "ROOT"
			}
		}
		description := ""
		if spec != nil {
			description = composeRoutingDescription(
				strings.TrimSpace(firstStr(spec, "description", "")),
				strings.TrimSpace(firstStr(spec, "select_when", "")),
				strings.TrimSpace(firstStr(spec, "dont_select_when", "")),
			)
		}
		nodeID := cid
		if root {
			nodeID = "ROOT"
		}
		return &common.RetrieverNode{
			NodeID:      nodeID,
			Label:       label,
			Description: description,
			Children:    children,
			Items:       items,
		}
	}

	if root := buildBranch("", true); root != nil {
		return *root
	}
	return common.RetrieverNode{NodeID: "ROOT", Label: "ROOT"}
}

// composeRoutingDescription 路由描述拼接（description + Select when + Don't select when）。
// 对齐 Python _compose_routing_description（loading.py:208-214）。
func composeRoutingDescription(description, selectWhen, dontSelectWhen string) string {
	var parts []string
	if d := strings.TrimSpace(description); d != "" {
		parts = append(parts, d)
	}
	if selectWhen != "" && !strings.Contains(description, "Select when:") {
		parts = append(parts, fmt.Sprintf("Select when: %s", selectWhen))
	}
	if dontSelectWhen != "" && !strings.Contains(description, "Don't select when:") {
		parts = append(parts, fmt.Sprintf("Don't select when: %s", dontSelectWhen))
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// loadManifestFile 读 manifest.json（不存在空表）。
// 对齐 Python _load_manifest（loading.py:217-221）。
func loadManifestFile(indexDir string) (map[string]any, error) {
	manifestPath := filepath.Join(indexDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// artifactPath manifest.artifacts 键回落（对齐 loading.py:224-230）。
func artifactPath(manifest map[string]any, key, fallback string) string {
	if arts, ok := manifest["artifacts"].(map[string]any); ok {
		if v := strings.TrimSpace(firstStr(arts, key, "")); v != "" {
			return v
		}
	}
	return fallback
}

// loadYAMLLike YAML 优先、简单解析回落。
// 对齐 Python _load_yaml_like（loading.py:233-242）。
func loadYAMLLike(text string) map[string]any {
	var payload map[string]any
	if err := yaml.Unmarshal([]byte(text), &payload); err == nil && payload != nil {
		return payload
	}
	return parseSimpleNodesYAML(text)
}

// parseSimpleNodesYAML 简单行式解析（对齐 loading.py:245-264）。
func parseSimpleNodesYAML(text string) map[string]any {
	var nodes []map[string]string
	var current map[string]string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r\n \t")
		stripped := strings.TrimSpace(line)
		if stripped == "" || stripped == "nodes:" {
			continue
		}
		if strings.HasPrefix(stripped, "- ") {
			if current != nil {
				nodes = append(nodes, current)
			}
			current = map[string]string{}
			stripped = stripped[2:]
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(stripped, ":")
		if !found {
			continue
		}
		current[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if current != nil {
		nodes = append(nodes, current)
	}
	out := make([]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{}
		for k, v := range n {
			m[k] = v
		}
		out = append(out, m)
	}
	return map[string]any{"nodes": out}
}

// firstStr 首个存在键的 Python str(x or "") 语义：falsy（nil/空串/false/0）回落空串，
// 其余值强制字符串化（str(float64(1.1))="1.1"、str(int 1)="1"）。
func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if t != "" {
				return t
			}
		case bool:
			if t { // Python False 为 falsy
				return "True"
			}
		case int:
			if t != 0 {
				return fmt.Sprintf("%d", t)
			}
		case float64:
			if t != 0 {
				return fmt.Sprintf("%v", t)
			}
		default:
			return fmt.Sprintf("%v", t)
		}
	}
	return ""
}

// toStringValue 值字符串化
func toStringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// CatalogRecordFromBuild 从 build 视角记录转换（SkillFS _build_live_tree 使用）。
// Python 无此转换（SkillFS 直接构造 loading.CatalogRecord），此处仅为内部便利。
var _ = build.IndexManifestFilename
