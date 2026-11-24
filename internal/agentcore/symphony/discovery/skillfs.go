// skillfs.go 对齐 Python discovery/skillfs.py（902 行）：live 目录与冻结快照。
// _recoverIndexDirectoryUnlocked 为偏差登记复刻（D-16-4-2）：Python 来源
// symphony/agent/retrieval_toolkit.py:2003-2037，回收条件=16.5 实施
// retrieval_toolkit.go 时收口去重。
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	buildio "github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build/io"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/search/artifacts"
	lexruntime "github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/search/runtime"
)

// 覆盖段常量（对齐 skillfs.py:29-30）
const (
	overlaySegment = "newly_installed_skills"
	overlayNodeID  = "__SYMPHONY_LIVE_SKILL_OVERLAY__"
	// indexLockFilename 发布锁文件名
	indexLockFilename = ".skill-index.publish.lock"
	// commitFilename 提交状态文件名（Python retrieval_toolkit COMMIT_FILENAME）
	commitFilename = "commit.json"
)

// safeSegmentRe 段名安全化（对齐 _SAFE_SEGMENT）
var safeSegmentRe = regexp.MustCompile(`[^0-9A-Za-z._+\-\x{3400}-\x{9fff}]+`)

// 包级 lexical 缓存（对齐 skillfs.py:31-35，容量 4）
var lexicalCache = newLRU(4)

// 索引快照缓存（对齐 skillfs.py:36-39，容量 16）
var indexSnapshotCache = newLRU(16)

// ──────────────────────────── 数据类型 ────────────────────────────

// SkillFSArtifact 当前 live 清单经一代 taxonomy 的投影
type SkillFSArtifact struct {
	// Root 索引根
	Root string
	// Layout 布局（tree|flat）
	Layout string
	// IndexState 索引状态（missing|fresh|stale）
	IndexState string
	// Inventory 清单
	Inventory SkillInventory
	// Items 可见记录
	Items []SkillRecord
	// Fingerprint 组合指纹
	Fingerprint string
}

// SkillPromptEntry 提示词条目
type SkillPromptEntry struct {
	// WorkerID 技能标识
	WorkerID string
	// Description 描述
	Description string
	// Source 来源
	Source string
}

// SkillPromptBranch 提示词分支行
type SkillPromptBranch struct {
	// Path 分支路径
	Path string
	// Label 展示名
	Label string
	// Description 描述
	Description string
}

// SkillPromptSnapshot 冻结提示词快照
type SkillPromptSnapshot struct {
	// Mode 策略（small|large-flat|indexed|indexed-stale）
	Mode string
	// TotalCount 总数
	TotalCount int
	// Entries 注入条目
	Entries []SkillPromptEntry
	// EstimatedCandidateTokens 估 token
	EstimatedCandidateTokens int
	// CandidateBudgetTokens 预算
	CandidateBudgetTokens int
	// IndexState 索引状态
	IndexState string
	// Branches 分支行
	Branches []SkillPromptBranch
	// OmittedBranchCount 被裁分支数
	OmittedBranchCount int
}

// AllCandidatesIncluded 全量注入判定（mode=="small"）
func (s SkillPromptSnapshot) AllCandidatesIncluded() bool { return s.Mode == "small" }

// OmittedCount 被裁条目数
func (s SkillPromptSnapshot) OmittedCount() int {
	omitted := s.TotalCount - len(s.Entries)
	if omitted < 0 {
		omitted = 0
	}
	return omitted
}

// DirectoryEntry 内存目录的模型可见行
type DirectoryEntry struct {
	// Path 路径
	Path string
	// Kind 类型（dir|skill）
	Kind string
	// Label 展示名
	Label string
	// Description 描述
	Description string
	// WorkerID 技能标识（skill 行有值）
	WorkerID string
	// Depth 深度
	Depth int
}

// SkillIndexSnapshot 会话钉住的序列化 taxonomy 代际
type SkillIndexSnapshot struct {
	// Nodes taxonomy 节点
	Nodes []map[string]any
	// RecordHashes worker_id→hash 有序对
	RecordHashes [][2]string
	// Fingerprint 指纹
	Fingerprint string

	taxonomyFingerprint string
}

// TaxonomyFingerprint 节点集指纹（构造时计算）
func (s *SkillIndexSnapshot) TaxonomyFingerprint() string {
	if s.taxonomyFingerprint == "" {
		data, _ := json.Marshal(s.Nodes) // Go json.Marshal 键序稳定（结构体内建序）
		sum := sha256.Sum256(data)
		s.taxonomyFingerprint = hex.EncodeToString(sum[:])
	}
	return s.taxonomyFingerprint
}

// ToDict 序列化（对齐 skillfs.py:59-65）
func (s *SkillIndexSnapshot) ToDict() map[string]any {
	hashes := map[string]string{}
	for _, pair := range s.RecordHashes {
		hashes[pair[0]] = pair[1]
	}
	// nodes 转 []any 保证 FromDict 的 []any 断言可通过（JSON 往返形态）
	nodes := make([]any, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes = append(nodes, n)
	}
	return map[string]any{"nodes": nodes, "record_hashes": hashes, "fingerprint": s.Fingerprint}
}

// SkillIndexSnapshotFromDict 反序列化与五重结构校验。
// 对齐 Python from_dict（skillfs.py:67-84）+ _validate_taxonomy_nodes（:845-880）。
func SkillIndexSnapshotFromDict(payload map[string]any) (*SkillIndexSnapshot, error) {
	if payload == nil {
		return nil, errors.New("pinned index snapshot must be a mapping")
	}
	fingerprint := strings.TrimSpace(stringOr(payload, "fingerprint"))
	rawNodes, nodesIsList := payload["nodes"].([]any)
	// record_hashes 兼容 map[string]any 与 map[string]string（ToDict 往返形态）
	rawHashesAny, hashesIsMapAny := payload["record_hashes"].(map[string]any)
	if !hashesIsMapAny {
		if m, ok := payload["record_hashes"].(map[string]string); ok {
			converted := map[string]any{}
			for k, v := range m {
				converted[k] = v
			}
			rawHashesAny = converted
			hashesIsMapAny = true
		}
	}
	rawHashes := rawHashesAny
	hashesIsMap := hashesIsMapAny
	if !nodesIsList {
		return nil, errors.New("pinned index snapshot nodes must be an array of objects")
	}
	if !hashesIsMap || len(rawHashes) == 0 {
		return nil, errors.New("pinned index snapshot record_hashes must be a non-empty string mapping")
	}
	if fingerprint == "" {
		return nil, errors.New("pinned index snapshot fingerprint is required")
	}
	nodes := make([]map[string]any, 0, len(rawNodes))
	for _, rn := range rawNodes {
		m, ok := rn.(map[string]any)
		if !ok {
			return nil, errors.New("pinned index snapshot nodes must be an array of objects")
		}
		nodes = append(nodes, m)
	}
	// record_hashes 有序对 + 键值非空校验
	keys := make([]string, 0, len(rawHashes))
	for k := range rawHashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	recordHashes := make([][2]string, 0, len(keys))
	expectedWorkerIDs := map[string]bool{}
	for _, k := range keys {
		v := fmt.Sprintf("%v", rawHashes[k])
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return nil, errors.New("pinned index snapshot record_hashes must be a non-empty string mapping")
		}
		recordHashes = append(recordHashes, [2]string{k, v})
		expectedWorkerIDs[k] = true
	}
	if err := validateTaxonomyNodes(nodes, expectedWorkerIDs); err != nil {
		return nil, err
	}
	return &SkillIndexSnapshot{Nodes: nodes, RecordHashes: recordHashes, Fingerprint: fingerprint}, nil
}

// validateTaxonomyNodes taxonomy 节点结构校验。
// 对齐 Python _validate_taxonomy_nodes（skillfs.py:845-880）。
func validateTaxonomyNodes(nodes []map[string]any, expectedWorkerIDs map[string]bool) error {
	identifiers := map[string]bool{}
	branchIDs := map[string]bool{}
	leafWorkerIDs := map[string]bool{}
	for _, node := range nodes {
		cid := strings.TrimSpace(stringOr(node, "cid"))
		kind := strings.ToLower(strings.TrimSpace(stringOr(node, "type")))
		workerID := strings.TrimSpace(stringOr(node, "worker_id"))
		if cid == "" || cid == overlayNodeID || (kind != "branch" && kind != "leaf") {
			return errors.New("pinned index snapshot contains invalid taxonomy nodes")
		}
		if identifiers[cid] {
			return errors.New("pinned index snapshot contains invalid taxonomy nodes")
		}
		if kind == "leaf" && (workerID == "" || leafWorkerIDs[workerID]) {
			return errors.New("pinned index snapshot contains invalid taxonomy nodes")
		}
		identifiers[cid] = true
		if kind == "branch" {
			branchIDs[cid] = true
		} else {
			leafWorkerIDs[workerID] = true
		}
	}
	for cid := range identifiers {
		if strings.Contains(cid, ".") {
			parent := cid[:strings.LastIndex(cid, ".")]
			if !branchIDs[parent] {
				return errors.New("pinned index snapshot contains an orphan taxonomy node")
			}
		}
	}
	if len(leafWorkerIDs) != len(expectedWorkerIDs) {
		return errors.New("pinned index snapshot does not match its record hashes")
	}
	for wid := range expectedWorkerIDs {
		if !leafWorkerIDs[wid] {
			return errors.New("pinned index snapshot does not match its record hashes")
		}
	}
	return nil
}

// ──────────────────────────── 目录视图 ────────────────────────────

// SkillDirectoryView 标准 RetrieverNode 树的路径投影
type SkillDirectoryView struct {
	root               common.RetrieverNode
	recordByID         map[string]SkillRecord
	catalogByPayload   map[string]artifacts.CatalogRecord
	nodeByPath         map[string]common.RetrieverNode
	recordPathByID     map[string]string
	recordIDByMetaPath map[string]string
	children           map[string][]DirectoryEntry
	skillCounts        map[string]int
}

// NewSkillDirectoryView 构建路径投影（对齐 skillfs.py:149-164）。
func NewSkillDirectoryView(root common.RetrieverNode, records []SkillRecord, catalogByPayload map[string]artifacts.CatalogRecord) *SkillDirectoryView {
	v := &SkillDirectoryView{
		root:               root,
		recordByID:         map[string]SkillRecord{},
		catalogByPayload:   catalogByPayload,
		nodeByPath:         map[string]common.RetrieverNode{"/": root},
		recordPathByID:     map[string]string{},
		recordIDByMetaPath: map[string]string{},
		children:           map[string][]DirectoryEntry{},
		skillCounts:        map[string]int{},
	}
	for _, r := range records {
		v.recordByID[r.WorkerID] = r
	}
	v.indexPaths()
	return v
}

// RecordByID 按 worker_id 取记录（对齐 Python view.record_by_id 公有属性）。
func (v *SkillDirectoryView) RecordByID(workerID string) (SkillRecord, bool) {
	r, ok := v.recordByID[workerID]
	return r, ok
}

// NodeByPath 取树节点（对齐 Python view.node_by_path 公有属性）。
func (v *SkillDirectoryView) NodeByPath(path string) (common.RetrieverNode, bool) {
	n, ok := v.nodeByPath[path]
	return n, ok
}

// RecordPathByID 取技能记录路径（对齐 Python view.record_path_by_id 公有属性）。
func (v *SkillDirectoryView) RecordPathByID(workerID string) string {
	return v.recordPathByID[workerID]
}

// NormalizePath 路径规范化（拒绝 ..，根为 "/"）。
// 对齐 skillfs.py:166-173。
func (v *SkillDirectoryView) NormalizePath(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		raw = "/"
	}
	cleaned := strings.TrimLeft(raw, "/")
	parts := strings.Split(cleaned, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			return "", errors.New("Skill directory paths must not contain '..'")
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return "/", nil
	}
	return "/" + strings.Join(safe, "/"), nil
}

// Children 子条目（不存在路径报错，对齐 :175-179）
func (v *SkillDirectoryView) Children(path string) ([]DirectoryEntry, error) {
	normalized, err := v.NormalizePath(path)
	if err != nil {
		return nil, err
	}
	if _, ok := v.nodeByPath[normalized]; !ok {
		return nil, fmt.Errorf("No such Skill directory: %s", normalized)
	}
	return v.children[normalized], nil
}

// SkillCount 该分类及全部后代可见技能数（对齐 :181-183）
func (v *SkillDirectoryView) SkillCount(path string) int {
	normalized, err := v.NormalizePath(path)
	if err != nil {
		return 0
	}
	return v.skillCounts[normalized]
}

// SearchableEntries 可见目录与技能条目（允许 META.md 虚拟寻址，递归默认开）。
// 对齐 skillfs.py:213-221（recursive 取默认 True）。
func (v *SkillDirectoryView) SearchableEntries(paths []string, maxDepth int) []DirectoryEntry {
	return v.scopedEntries(paths, maxDepth, true, boolPtr(true), false, true)
}

// TreeEntries 范围根 + 后代树序（对齐 skillfs.py:202-211）。
func (v *SkillDirectoryView) TreeEntries(paths []string, maxDepth int, directoriesOnly bool) []DirectoryEntry {
	return v.scopedEntries(paths, maxDepth, true, boolPtr(true), directoriesOnly, false)
}

// Entries 四参入口（对齐 skillfs.py:185-200：directory_entry 决定 include_root
// 与 recursive=None 语义）。
func (v *SkillDirectoryView) Entries(paths []string, maxDepth int, recursive bool, directoriesOnly bool, directoryEntry bool) []DirectoryEntry {
	if directoryEntry {
		return v.scopedEntries(paths, maxDepth, true, nil, directoriesOnly, false)
	}
	return v.scopedEntries(paths, maxDepth, directoryEntry, boolPtr(recursive), directoriesOnly, false)
}

func boolPtr(b bool) *bool { return &b }

// ScopedRecords 范围内记录（去重保序）。
// 对齐 skillfs.py:223-226。
func (v *SkillDirectoryView) ScopedRecords(paths []string, maxDepth int) []SkillRecord {
	entries := v.SearchableEntries(paths, maxDepth)
	var out []SkillRecord
	seen := map[string]bool{}
	for _, e := range entries {
		if e.WorkerID != "" && !seen[e.WorkerID] {
			seen[e.WorkerID] = true
			out = append(out, v.recordByID[e.WorkerID])
		}
	}
	return out
}

// MetadataPath META.md 虚拟寻址路径（不读盘，spec §80②）。
// 对齐 skillfs.py:228-232。
func (v *SkillDirectoryView) MetadataPath(workerID string) (string, error) {
	p, ok := v.recordPathByID[workerID]
	if !ok {
		return "", fmt.Errorf("Unknown Skill ID: %s", workerID)
	}
	return p + "/META.md", nil
}

// ResolveMetadataPath 虚拟路径→记录（对齐 :234-239）。
func (v *SkillDirectoryView) ResolveMetadataPath(path string) (SkillRecord, error) {
	normalized, err := v.NormalizePath(path)
	if err != nil {
		return SkillRecord{}, err
	}
	wid, ok := v.recordIDByMetaPath[normalized]
	if !ok {
		return SkillRecord{}, fmt.Errorf("No such Skill metadata path: %s", normalized)
	}
	return v.recordByID[wid], nil
}

// CategoryText 有序分类名与描述。
// 对齐 skillfs.py:241-254。
func (v *SkillDirectoryView) CategoryText(workerID string) string {
	path := v.recordPathByID[workerID]
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		path = path[:idx]
	} else {
		path = "/"
	}
	type level struct{ label, desc string }
	var levels []level
	for path != "" && path != "." && path != "/" {
		if node, ok := v.nodeByPath[path]; ok {
			levels = append(levels, level{node.Label, searchableCategoryDescription(node.Description)})
		}
		if idx := strings.LastIndex(path, "/"); idx > 0 {
			path = path[:idx]
		} else {
			path = "/"
		}
	}
	var parts []string
	for i := len(levels) - 1; i >= 0; i-- {
		if levels[i].label != "" {
			parts = append(parts, levels[i].label)
		}
		if levels[i].desc != "" {
			parts = append(parts, levels[i].desc)
		}
	}
	return strings.Join(parts, "\n")
}

// scopedEntries 范围条目收集。
// 对齐 skillfs.py:256-300（includeRoot/recursive/directoriesOnly/allowMetadata 四参数）。
func (v *SkillDirectoryView) scopedEntries(paths []string, maxDepth int, includeRoot bool, recursive *bool, directoriesOnly bool, allowMetadata bool) []DirectoryEntry {
	var rows []DirectoryEntry
	seen := map[string]bool{}
	for _, rawPath := range paths {
		path, err := v.NormalizePath(rawPath)
		if err != nil {
			continue // 非法路径跳过（Python 直接抛错由上层捕获；此处保持容错收集）
		}
		var candidates []DirectoryEntry
		var workerID string
		if allowMetadata {
			workerID = v.recordIDByMetaPath[path]
		}
		if workerID != "" {
			record := v.recordByID[workerID]
			name := record.Name
			if name == "" {
				name = workerID
			}
			candidates = []DirectoryEntry{{
				Path: v.recordPathByID[workerID], Kind: "skill", Label: name,
				Description: record.Description, WorkerID: workerID,
			}}
		} else {
			node, ok := v.nodeByPath[path]
			if !ok {
				continue
			}
			if includeRoot {
				description := node.Description
				if description == "" && recursive != nil && *recursive && path == "/" {
					description = "Installed Skills"
				}
				candidates = append(candidates, DirectoryEntry{Path: path, Kind: "dir", Label: node.Label, Description: description})
			}
			if recursive != nil {
				candidates = append(candidates, v.walk(path, *recursive, maxDepth)...)
			}
		}
		for _, entry := range candidates {
			if directoriesOnly && entry.Kind != "dir" {
				continue
			}
			key := entry.Kind + "\x00" + entry.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, entry)
		}
	}
	return rows
}

// walk 深度优先行收集（对齐 skillfs.py:302-315）。
func (v *SkillDirectoryView) walk(path string, recursive bool, maxDepth int) []DirectoryEntry {
	var rows []DirectoryEntry
	var visit func(current string, depth int)
	visit = func(current string, depth int) {
		for _, entry := range v.children[current] {
			relativeDepth := depth + 1
			if maxDepth >= 0 && relativeDepth > maxDepth { // -1 = 无限制（Python None）
				continue
			}
			entry.Depth = relativeDepth
			rows = append(rows, entry)
			if recursive && entry.Kind == "dir" {
				visit(entry.Path, relativeDepth)
			}
		}
	}
	visit(path, 0)
	return rows
}

// indexPaths 树→路径投影主链（含 overlay 优先与段名唯一化）。
// 对齐 skillfs.py:317-389。
func (v *SkillDirectoryView) indexPaths() {
	var visit func(node common.RetrieverNode, path string)
	visit = func(node common.RetrieverNode, path string) {
		type candidate struct {
			preferred, kind, identity string
			node                      *common.RetrieverNode
			record                    *SkillRecord
		}
		var candidates []candidate
		for i := range node.Children {
			child := &node.Children[i]
			preferred := child.Label
			if child.NodeID == overlayNodeID {
				preferred = overlaySegment
			} else if preferred == "" {
				preferred = child.NodeID
				if idx := strings.LastIndex(child.NodeID, "."); idx >= 0 {
					preferred = child.NodeID[idx+1:]
				}
			}
			candidates = append(candidates, candidate{preferred, "dir", child.NodeID, child, nil})
		}
		for _, item := range node.Items {
			catalog, ok := v.catalogByPayload[item.Payload]
			if !ok {
				continue
			}
			workerID := strings.TrimSpace(catalog.WorkerID)
			record, ok := v.recordByID[workerID]
			if !ok {
				continue
			}
			r := record
			candidates = append(candidates, candidate{workerID, "skill", workerID, nil, &r})
		}
		// 排序键（对齐 :337-345）
		sort.SliceStable(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			ka := []string{segmentBase(a.preferred), strings.ToLower(segmentBase(a.preferred)), a.kind, strings.ToLower(a.identity), a.identity}
			kb := []string{segmentBase(b.preferred), strings.ToLower(segmentBase(b.preferred)), b.kind, strings.ToLower(b.identity), b.identity}
			for x := range ka {
				if ka[x] != kb[x] {
					return ka[x] < kb[x]
				}
			}
			return false
		})
		overlayPresent := false
		for _, c := range candidates {
			if c.kind == "dir" && c.identity == overlayNodeID {
				overlayPresent = true
			}
		}
		used := map[string]bool{}
		if overlayPresent {
			used[strings.ToLower(overlaySegment)] = true
		}
		type resolvedItem struct {
			segment, kind string
			node          *common.RetrieverNode
			record        *SkillRecord
		}
		var resolved []resolvedItem
		for _, c := range candidates {
			segment := uniqueSegment(c.preferred, c.identity, used)
			if c.kind == "dir" && c.identity == overlayNodeID {
				segment = overlaySegment
			}
			resolved = append(resolved, resolvedItem{segment, c.kind, c.node, c.record})
		}
		sort.SliceStable(resolved, func(i, j int) bool {
			a, b := resolved[i], resolved[j]
			la := strings.ToLower(a.segment) + "\x00" + a.segment + "\x00" + a.kind
			lb := strings.ToLower(b.segment) + "\x00" + b.segment + "\x00" + b.kind
			return la < lb
		})
		var entries []DirectoryEntry
		type childPair struct {
			node common.RetrieverNode
			path string
		}
		var childNodes []childPair
		for _, r := range resolved {
			entryPath := joinPath(path, r.segment)
			if r.kind == "dir" {
				v.nodeByPath[entryPath] = *r.node
				label := r.node.Label
				if label == "" {
					label = r.segment
				}
				entries = append(entries, DirectoryEntry{Path: entryPath, Kind: "dir", Label: label, Description: r.node.Description})
				childNodes = append(childNodes, childPair{*r.node, entryPath})
				continue
			}
			metadataPath := entryPath + "/META.md"
			v.recordPathByID[r.record.WorkerID] = entryPath
			v.recordIDByMetaPath[metadataPath] = r.record.WorkerID
			label := r.record.Name
			if label == "" {
				label = r.record.WorkerID
			}
			entries = append(entries, DirectoryEntry{Path: entryPath, Kind: "skill", Label: label, Description: r.record.Description, WorkerID: r.record.WorkerID})
		}
		v.children[path] = entries
		count := 0
		for _, e := range entries {
			if e.Kind == "skill" {
				count++
			}
		}
		v.skillCounts[path] = count
		for _, cp := range childNodes {
			visit(cp.node, cp.path)
			v.skillCounts[path] += v.skillCounts[cp.path]
		}
	}
	visit(v.root, "/")
}

// ──────────────────────────── SkillFS ────────────────────────────

// RecordsProvider 记录提供回调（Python SkillRecordsProvider；返回清单，
// 调用方 ScanSkillDirectories/InventoryFromRecords 已产出 SkillInventory）
type RecordsProvider func() (SkillInventory, error)

// VisibleSkillNames 可见名单提供（nil/空表=全可见；Python VisibleSkillNames）
type VisibleSkillNames func() map[string]bool

// SkillFS 会话本地 live 目录
type SkillFS struct {
	recordsProvider   RecordsProvider
	settings          DiscoverySettings
	visibleSkillNames VisibleSkillNames
	artifactRoot      string
	indexRoot         string
	pinIndexRevision  bool
	indexSnapshot     *SkillIndexSnapshot
	bodyCache         map[string]map[string]string // worker_id → (hash → body)
	lexicalCacheKey   string
	lexicalIndex      *lexruntime.LexicalIndex
	artifact          SkillFSArtifact
	view              *SkillDirectoryView
}

// SkillFSOptions 构造选项（对齐 Python 关键字参数）
type SkillFSOptions struct {
	// Settings 设置（必填）
	Settings DiscoverySettings
	// VisibleSkillNames 可见名单（可 nil）
	VisibleSkillNames VisibleSkillNames
	// ArtifactRoot 产物根（必填）
	ArtifactRoot string
	// IndexRoot 索引根（可空=ArtifactRoot）
	IndexRoot string
	// PinIndexRevision 是否钉住索引版本
	PinIndexRevision bool
	// PinnedIndexSnapshot 钉住的快照（三态：nil 未提供/提供 nil/提供值）
	PinnedSnapshot *SkillIndexSnapshot
	// SnapshotProvided 区分「显式提供 nil」与「未提供」
	SnapshotProvided bool
}

// NewSkillFS 构造并首刷。
// 对齐 skillfs.py:392-437：钉住判定（pin 或显式提供快照）、快照来源三态。
func NewSkillFS(recordsProvider RecordsProvider, opts SkillFSOptions) (*SkillFS, error) {
	if recordsProvider == nil {
		return nil, errors.New("records_provider must be callable")
	}
	artifactRoot, err := resolveUserPath(opts.ArtifactRoot)
	if err != nil {
		return nil, err
	}
	indexRoot := artifactRoot
	if opts.IndexRoot != "" {
		indexRoot, err = resolveUserPath(opts.IndexRoot)
		if err != nil {
			return nil, err
		}
	}
	fs := &SkillFS{
		recordsProvider:   recordsProvider,
		settings:          opts.Settings,
		visibleSkillNames: opts.VisibleSkillNames,
		artifactRoot:      artifactRoot,
		indexRoot:         indexRoot,
		bodyCache:         map[string]map[string]string{},
	}
	fs.pinIndexRevision = opts.PinIndexRevision || opts.SnapshotProvided
	if opts.SnapshotProvided {
		fs.indexSnapshot = opts.PinnedSnapshot
	} else if fs.pinIndexRevision && opts.Settings.UseExistingIndex {
		fs.indexSnapshot = CaptureIndexSnapshot(fs.indexRoot)
	} else {
		fs.indexSnapshot = nil
	}
	if err := fs.Refresh(); err != nil {
		return nil, err
	}
	return fs, nil
}

// Artifact 当前产物（不刷新）
func (fs *SkillFS) Artifact() SkillFSArtifact { return fs.artifact }

// Settings 设置
func (fs *SkillFS) Settings() DiscoverySettings { return fs.settings }

// PinnedIndexSnapshot 钉住的索引快照
func (fs *SkillFS) PinnedIndexSnapshot() *SkillIndexSnapshot { return fs.indexSnapshot }

// Directory 目录视图（先刷新）
func (fs *SkillFS) Directory() *SkillDirectoryView {
	_ = fs.Refresh()
	return fs.view
}

// SelectionCards 选择卡表（worker_id → name/description）。
// 对齐 skillfs.py:456-464。
func (fs *SkillFS) SelectionCards(refresh bool) map[string]map[string]string {
	if refresh {
		_ = fs.Refresh()
	}
	out := map[string]map[string]string{}
	for _, record := range fs.artifact.Items {
		description := record.Description
		if description == "" {
			description = record.Name
		}
		if description == "" {
			description = record.WorkerID
		}
		out[record.WorkerID] = map[string]string{"name": record.WorkerID, "description": description}
	}
	return out
}

// PromptSnapshot 提示词快照（四态推导 + 冻结）。
// 对齐 skillfs.py:467-499。
func (fs *SkillFS) PromptSnapshot(refresh bool) SkillPromptSnapshot {
	if refresh {
		_ = fs.Refresh()
	}
	items := append([]SkillRecord{}, fs.artifact.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		li, lj := strings.ToLower(items[i].WorkerID), strings.ToLower(items[j].WorkerID)
		if li != lj {
			return li < lj
		}
		return items[i].WorkerID < items[j].WorkerID
	})
	var rendered strings.Builder
	for _, item := range items {
		rendered.WriteString("- " + item.WorkerID + ": " + strings.Join(strings.Fields(item.Description), " ") + "\n")
	}
	estimated := estimateTokens(rendered.String())
	budget := fs.settings.CandidateBudgetTokens()
	small := estimated < budget
	var selected []SkillRecord
	if small {
		selected = items
	} else {
		selected = fs.largePromptItems(items)
	}
	mode := "large-flat"
	if small {
		mode = "small"
	} else if fs.artifact.Layout == "tree" && fs.artifact.IndexState == "fresh" {
		mode = "indexed"
	} else if fs.artifact.Layout == "tree" {
		mode = "indexed-stale"
	}
	var branches []SkillPromptBranch
	omitted := 0
	if !small && fs.artifact.Layout == "tree" {
		branches, omitted = fs.promptBranches()
	}
	entries := make([]SkillPromptEntry, 0, len(selected))
	for _, item := range selected {
		description := item.Description
		if description == "" {
			description = item.Name
		}
		entries = append(entries, SkillPromptEntry{item.WorkerID, description, item.Source})
	}
	return SkillPromptSnapshot{
		Mode: mode, TotalCount: len(items), Entries: entries,
		EstimatedCandidateTokens: estimated, CandidateBudgetTokens: budget,
		IndexState: fs.artifact.IndexState, Branches: branches, OmittedBranchCount: omitted,
	}
}

// largePromptItems large-flat 裁剪（preferred 优先 + 截断）。
// 对齐 skillfs.py:641-646。
func (fs *SkillFS) largePromptItems(items []SkillRecord) []SkillRecord {
	preferred := map[string]bool{}
	for _, name := range fs.settings.PromptPreferredSkills {
		preferred[strings.ToLower(name)] = true
	}
	var selected []SkillRecord
	for _, item := range items {
		if preferred[strings.ToLower(item.WorkerID)] || preferred[strings.ToLower(item.Name)] {
			selected = append(selected, item)
		}
	}
	if len(selected) > fs.settings.MaxListEntries {
		selected = selected[:fs.settings.MaxListEntries]
	}
	return selected
}

// ReadBody 技能正文读取（worker_id 维度缓存、越界防护）。
// 对齐 skillfs.py:526-556。
func (fs *SkillFS) ReadBody(record SkillRecord) string {
	key := record.ContentHash
	if cached, ok := fs.bodyCache[record.WorkerID][key]; ok {
		return cached
	}
	body := ""
	resolved, err := resolveUserPath(record.SkillFile)
	if err == nil {
		if record.SourceRoot != "" {
			root, rerr := resolveUserPath(record.SourceRoot)
			if rerr != nil || !withinRoot(resolved, root) {
				err = errors.New("Skill source escapes its authorized root")
			}
		}
		if err == nil {
			data, rerr := os.ReadFile(resolved)
			if rerr == nil {
				body = string(data)
			}
		}
	}
	// 清理同 worker_id 旧缓存（对齐 :551-553）
	delete(fs.bodyCache, record.WorkerID)
	if fs.bodyCache[record.WorkerID] == nil {
		fs.bodyCache[record.WorkerID] = map[string]string{}
	}
	fs.bodyCache[record.WorkerID][key] = body
	return body
}

// SearchContent live 范围词法检索（指纹键控缓存）。
// 对齐 skillfs.py:558-600。
func (fs *SkillFS) SearchContent(records []SkillRecord, query string, caseInsensitive bool, fixedStrings bool, termMode bool) ([]string, error) {
	cacheKey := fs.artifact.Fingerprint
	if fs.lexicalIndex == nil || cacheKey != fs.lexicalCacheKey {
		if cached := lexicalCache.get(cacheKey); cached != nil {
			fs.lexicalIndex = cached.(*lexruntime.LexicalIndex)
		} else {
			docs := make([]lexruntime.LexicalDocument, 0, len(fs.artifact.Items))
			for _, item := range fs.artifact.Items {
				docs = append(docs, lexruntime.LexicalDocument{
					Key: item.WorkerID, Name: item.Name, Description: item.Description,
					Body: fs.ReadBody(item), Aliases: item.Aliases,
					Category: fs.view.CategoryText(item.WorkerID),
				})
			}
			fs.lexicalIndex = lexruntime.NewLexicalIndex(docs)
			lexicalCache.put(cacheKey, fs.lexicalIndex)
		}
		fs.lexicalCacheKey = cacheKey
	}
	keys := make([]string, 0, len(records))
	for _, r := range records {
		keys = append(keys, r.WorkerID)
	}
	var hits []lexruntime.LexicalHit
	var err error
	if termMode {
		hits, err = fs.lexicalIndex.SearchTerms(query, keys)
	} else {
		hits, err = fs.lexicalIndex.Search(query, keys, caseInsensitive, fixedStrings)
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Key)
	}
	return out, nil
}

// ContentMatchSnippet 紧凑行隐藏证据。
// 对齐 skillfs.py:601-606。
func (fs *SkillFS) ContentMatchSnippet(record SkillRecord, queries []string) string {
	if fs.lexicalIndex == nil {
		return ""
	}
	return fs.lexicalIndex.MatchSnippet(record.WorkerID, queries, 180, 96)
}

// MissingContentTerms 缺词（对齐 :607-612）。
func (fs *SkillFS) MissingContentTerms(query string) []string {
	if fs.lexicalIndex == nil {
		return nil
	}
	return fs.lexicalIndex.MissingTerms(query)
}

// Refresh 现扫 + 索引对比 + 树重建。
// 对齐 skillfs.py:614-639。
func (fs *SkillFS) Refresh() error {
	inventory, err := fs.recordsProvider()
	if err != nil {
		return err
	}
	visible := map[string]bool(nil)
	if fs.visibleSkillNames != nil {
		visible = fs.visibleSkillNames()
	}
	items := inventory.Items
	if visible != nil {
		var filtered []SkillRecord
		for _, item := range items {
			if visible[item.WorkerID] {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	snapshot := fs.indexSnapshot
	if fs.settings.UseExistingIndex && !fs.pinIndexRevision {
		snapshot = CaptureIndexSnapshot(fs.indexRoot)
	}
	currentHashes := map[string]string{}
	for _, item := range items {
		currentHashes[item.WorkerID] = item.ContentHash
	}
	var indexState string
	switch {
	case snapshot == nil:
		indexState = "missing"
	case snapshotHashesEqual(snapshot, currentHashes):
		indexState = "fresh"
	default:
		indexState = "stale"
	}
	root, catalog := buildLiveTree(items, snapshot)
	view := NewSkillDirectoryView(root, items, catalog)
	fingerprintPayload := map[string]any{
		"inventory": inventory.Fingerprint,
		"visible":   workerIDs(items),
		"index":     snapshotFingerprint(snapshot),
		"taxonomy":  snapshotTaxonomyFingerprint(snapshot),
		"state":     indexState,
	}
	data, _ := json.Marshal(fingerprintPayload)
	sum := sha256.Sum256(data)
	layout := "flat"
	if snapshot != nil {
		layout = "tree"
	}
	fs.artifact = SkillFSArtifact{
		Root: fs.artifactRoot, Layout: layout, IndexState: indexState,
		Inventory: inventory, Items: items, Fingerprint: hex.EncodeToString(sum[:]),
	}
	fs.view = view
	return nil
}

// snapshotHashesEqual 索引哈希与现扫一致性
func snapshotHashesEqual(snapshot *SkillIndexSnapshot, current map[string]string) bool {
	if len(snapshot.RecordHashes) != len(current) {
		return false
	}
	for _, pair := range snapshot.RecordHashes {
		if current[pair[0]] != pair[1] {
			return false
		}
	}
	return true
}

// workerIDs 提取 worker_id 列表
func workerIDs(items []SkillRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.WorkerID)
	}
	return out
}

func snapshotFingerprint(snapshot *SkillIndexSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.Fingerprint
}

func snapshotTaxonomyFingerprint(snapshot *SkillIndexSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.TaxonomyFingerprint()
}

// promptBranches 分支行展开（全目录下钻/混技能停层/封顶）。
// 对齐 skillfs.py:501-525。
func (fs *SkillFS) promptBranches() ([]SkillPromptBranch, int) {
	limit := fs.settings.MaxListEntries
	rootEntries, err := fs.view.Children("/")
	if err != nil {
		return nil, 0
	}
	var dirs []DirectoryEntry
	for _, e := range rootEntries {
		if e.Kind == "dir" {
			dirs = append(dirs, e)
		}
	}
	frontier := make([]SkillPromptBranch, 0, len(dirs))
	for _, e := range dirs {
		if len(frontier) >= limit {
			break
		}
		frontier = append(frontier, SkillPromptBranch{e.Path, e.Label, e.Description})
	}
	omitted := len(dirs) - len(frontier)
	if omitted > 0 {
		return frontier, omitted
	}
	// 无截断时逐层展开：全部子节点仍是目录 → 继续；否则停层（对齐 :511-525）
	for {
		var expanded []SkillPromptBranch
		allDirs := true
		for _, branch := range frontier {
			children, err := fs.view.Children(branch.Path)
			if err != nil || len(children) == 0 {
				expanded = append(expanded, branch)
				continue
			}
			mixed := false
			for _, c := range children {
				if c.Kind != "dir" {
					mixed = true
					break
				}
			}
			if mixed {
				allDirs = false
				expanded = append(expanded, branch)
				continue
			}
			for _, c := range children {
				expanded = append(expanded, SkillPromptBranch{c.Path, branch.Label + " > " + c.Label, c.Description})
			}
		}
		if sameBranches(frontier, expanded) || len(expanded) > limit {
			return frontier, 0
		}
		if !allDirs {
			return expanded, 0
		}
		frontier = expanded
	}
}

// sameBranches 分支行集合等值
func sameBranches(a, b []SkillPromptBranch) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Label != b[i].Label {
			return false
		}
	}
	return true
}

// ──────────────────────────── 索引快照装载 ────────────────────────────

// CaptureIndexSnapshot 读发布锁下的已提交 taxonomy 代际。
// 对齐 skillfs.py:652-667（capture_index_snapshot）：flock 锁 + 快照装载 +
// 失败走 D-16-4-2 复刻的恢复函数。
func CaptureIndexSnapshot(root string) *SkillIndexSnapshot {
	indexRoot, err := resolveUserPath(root)
	if err != nil {
		return nil
	}
	indexDir := filepath.Join(indexRoot, "index")
	_ = os.MkdirAll(indexRoot, 0o755)
	lockPath := filepath.Join(indexRoot, indexLockFilename)
	lockFile, lerr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if lerr == nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX) // portalocker 等价
		defer func() {
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
		}()
	}
	if snapshot := loadIndexSnapshot(indexDir); snapshot != nil {
		return snapshot
	}
	recovered := recoverIndexDirectoryUnlocked(indexDir, true, func(candidate string) bool {
		return loadIndexSnapshot(candidate) != nil
	})
	if recovered {
		return loadIndexSnapshot(indexDir)
	}
	return nil
}

// isCompleteIndex 完整索引判定（D-16-4-2 复刻依赖，Python retrieval_toolkit 私有件）
func isCompleteIndex(indexDir string) bool {
	for _, name := range []string{"tree_index.yaml", "catalog.jsonl", "manifest.json", commitFilename} {
		st, err := os.Stat(filepath.Join(indexDir, name))
		if err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// recoverIndexDirectoryUnlocked 中断目录交换恢复。
// 偏差登记复刻 D-16-4-2：Python retrieval_toolkit.py:2003-2037 一比一；
// 回收条件=16.5 实施 retrieval_toolkit.go 时收口去重。
func recoverIndexDirectoryUnlocked(indexDir string, replaceInvalid bool, validator func(string) bool) bool {
	if isCompleteIndex(indexDir) && !replaceInvalid {
		return false
	}
	parent := filepath.Dir(indexDir)
	base := filepath.Base(indexDir)
	var candidates []string
	for _, pattern := range []string{"." + base + ".staging-*", "." + base + ".backup-*"} {
		matches, _ := filepath.Glob(filepath.Join(parent, pattern))
		for _, path := range matches {
			valid := false
			if validator != nil {
				valid = validator(path)
			} else {
				valid = isCompleteIndex(path)
			}
			st, serr := os.Stat(path)
			if serr == nil && st.IsDir() && valid {
				candidates = append(candidates, path)
			}
		}
	}
	if len(candidates) == 0 {
		return false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		// 排序键（对齐 :2025-2032）：有 commit.json > staging > mtime，降序
		ki := []int{boolToInt(isCompleteIndex(filepath.Join(candidates[i], commitFilename))), 0, 0}
		ki[1] = boolToInt(strings.Contains(filepath.Base(candidates[i]), ".staging-"))
		if st, err := os.Stat(candidates[i]); err == nil {
			ki[2] = int(st.ModTime().UnixNano() / 1000)
		}
		kj := []int{boolToInt(isCompleteIndex(filepath.Join(candidates[j], commitFilename))), 0, 0}
		kj[1] = boolToInt(strings.Contains(filepath.Base(candidates[j]), ".staging-"))
		if st, err := os.Stat(candidates[j]); err == nil {
			kj[2] = int(st.ModTime().UnixNano() / 1000)
		}
		for x := range ki {
			if ki[x] != kj[x] {
				return ki[x] > kj[x]
			}
		}
		return false
	})
	selected := candidates[0]
	if _, err := os.Stat(indexDir); err == nil {
		_ = os.RemoveAll(indexDir) // shutil.rmtree(ignore_errors=True)
	}
	if err := os.Rename(selected, indexDir); err != nil {
		return false
	}
	return true
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// loadIndexSnapshot 无副作用的代际装载与交叉校验。
// 对齐 skillfs.py:670-714（_load_index_snapshot）。
func loadIndexSnapshot(indexDir string) *SkillIndexSnapshot {
	required := []string{"tree_index.yaml", "catalog.jsonl", "manifest.json", commitFilename}
	var sigParts []string
	for _, name := range required {
		p := filepath.Join(indexDir, name)
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			return nil
		}
		sigParts = append(sigParts, fmt.Sprintf("%s:%d:%d:%d", name, st.Size(), st.ModTime().UnixNano(), st.Mode()))
	}
	cacheKey := indexDir + "\x00" + strings.Join(sigParts, "\x01")
	if cached := indexSnapshotCache.get(cacheKey); cached != nil {
		return cached.(*SkillIndexSnapshot)
	}
	stateData, err := os.ReadFile(filepath.Join(indexDir, commitFilename))
	if err != nil {
		return nil
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		return nil
	}
	tree, err := buildio.LoadTreePreset(filepath.Join(indexDir, "tree_index.yaml"))
	if err != nil {
		return nil
	}
	nodes, _ := tree["nodes"].([]any)
	hashesAny := state["source_file_hashes"]
	if hashesAny == nil {
		hashesAny = state["record_hashes"]
	}
	hashes, ok := hashesAny.(map[string]any)
	fingerprint := strings.TrimSpace(stringOr(state, "fingerprint"))
	if !ok || len(hashes) == 0 || fingerprint == "" {
		return nil
	}
	// nodes 保持原始 []any（FromDict 内部逐项断言 map）
	hashStrings := map[string]any{}
	for k, v := range hashes {
		hashStrings[k] = v
	}
	snapshot, err := SkillIndexSnapshotFromDict(map[string]any{"nodes": nodes, "record_hashes": hashStrings, "fingerprint": fingerprint})
	if err != nil {
		return nil
	}
	loaded, lerr := artifacts.LoadRetrieverIndex(indexDir)
	if lerr != nil {
		return nil
	}
	expectedIDs := map[string]bool{}
	for _, pair := range snapshot.RecordHashes {
		expectedIDs[pair[0]] = true
	}
	catalogIDs := make([]string, 0, len(loaded.CatalogRecords))
	for _, r := range loaded.CatalogRecords {
		catalogIDs = append(catalogIDs, strings.TrimSpace(r.WorkerID))
	}
	if len(catalogIDs) != len(expectedIDs) || len(uniqueStrings(catalogIDs)) != len(catalogIDs) {
		return nil
	}
	if !sameStringSet(catalogIDs, expectedIDs) {
		return nil
	}
	manifestIDsAny, ok := loaded.Manifest["worker_ids"].([]any)
	if !ok {
		return nil
	}
	var manifestIDs []string
	for _, v := range manifestIDsAny {
		manifestIDs = append(manifestIDs, fmt.Sprintf("%v", v))
	}
	if len(manifestIDs) != len(expectedIDs) || len(uniqueStrings(manifestIDs)) != len(manifestIDs) {
		return nil
	}
	if !sameStringSet(manifestIDs, expectedIDs) {
		return nil
	}
	count := -1
	if c, ok := loaded.Manifest["count"].(float64); ok {
		count = int(c)
	}
	if count != len(expectedIDs) {
		return nil
	}
	indexSnapshotCache.put(cacheKey, snapshot)
	return snapshot
}

// buildLiveTree live 树构建（flat 直挂或 taxonomy 投影 + overlay）。
// 对齐 skillfs.py:717-808（_build_live_tree）。
func buildLiveTree(items []SkillRecord, snapshot *SkillIndexSnapshot) (common.RetrieverNode, map[string]artifacts.CatalogRecord) {
	catalog := map[string]artifacts.CatalogRecord{}
	if snapshot == nil {
		var rootItems []common.RetrieverItem
		for _, item := range items {
			name := item.Name
			if name == "" {
				name = item.WorkerID
			}
			rootItems = append(rootItems, common.RetrieverItem{ItemID: item.WorkerID, Payload: item.WorkerID, Label: name, Description: item.Description})
			catalog[item.WorkerID] = artifacts.CatalogRecord{
				ChoiceID: item.WorkerID, Payload: item.WorkerID,
				WorkerID: item.WorkerID, Name: item.Name, Description: item.Description,
			}
		}
		return common.RetrieverNode{NodeID: "ROOT", Label: "ROOT", Description: "Installed Skills", Items: rootItems}, catalog
	}
	leafCIDByWorker := map[string]string{}
	for _, node := range snapshot.Nodes {
		workerID := strings.TrimSpace(stringOr(node, "worker_id"))
		cid := strings.TrimSpace(stringOr(node, "cid"))
		if strings.EqualFold(strings.TrimSpace(stringOr(node, "type")), "leaf") && workerID != "" && cid != "" {
			leafCIDByWorker[workerID] = cid
		}
	}
	var indexedRecords []artifacts.CatalogRecord
	var overlayItems []common.RetrieverItem
	for _, item := range items {
		cid, ok := leafCIDByWorker[item.WorkerID]
		if ok {
			record := artifacts.CatalogRecord{
				ChoiceID: item.WorkerID, Payload: cid, WorkerID: item.WorkerID,
				Name: item.Name, Description: item.Description,
			}
			indexedRecords = append(indexedRecords, record)
			catalog[cid] = record
		} else {
			overlayItems = append(overlayItems, common.RetrieverItem{ItemID: item.WorkerID, Payload: item.WorkerID, Label: item.Name, Description: item.Description})
			catalog[item.WorkerID] = artifacts.CatalogRecord{
				ChoiceID: item.WorkerID, Payload: item.WorkerID,
				WorkerID: item.WorkerID, Name: item.Name, Description: item.Description,
			}
		}
	}
	// 可见 leaf/branch cid 集合（对齐 :748-757）
	visibleLeafCIDs := map[string]bool{}
	for _, r := range indexedRecords {
		visibleLeafCIDs[r.Payload] = true
	}
	visibleBranchCIDs := map[string]bool{}
	for cid := range visibleLeafCIDs {
		parent := cid
		if idx := strings.LastIndex(cid, "."); idx >= 0 {
			parent = cid[:idx]
		} else {
			parent = ""
		}
		for parent != "" {
			visibleBranchCIDs[parent] = true
			if idx := strings.LastIndex(parent, "."); idx >= 0 {
				parent = parent[:idx]
			} else {
				parent = ""
			}
		}
	}
	var visibleNodes []map[string]any
	for _, node := range snapshot.Nodes {
		kind := strings.ToLower(strings.TrimSpace(stringOr(node, "type")))
		cid := strings.TrimSpace(stringOr(node, "cid"))
		if kind == "leaf" && visibleLeafCIDs[cid] {
			visibleNodes = append(visibleNodes, node)
		} else if kind == "branch" && visibleBranchCIDs[cid] {
			visibleNodes = append(visibleNodes, node)
		}
	}
	// 描述字段净化（对齐 :762-770）
	safeNodes := make([]map[string]any, 0, len(visibleNodes))
	for _, node := range visibleNodes {
		copied := map[string]any{}
		for k, v := range node {
			copied[k] = v
		}
		for _, key := range []string{"description", "select_when", "dont_select_when"} {
			if _, ok := copied[key]; ok {
				copied[key] = SanitizeModelText(stringOr(node, key))
			}
		}
		safeNodes = append(safeNodes, copied)
	}
	root := artifacts.BuildTreeRoot(mapSliceToAny(safeNodes), indexedRecords)
	if len(overlayItems) > 0 {
		sort.SliceStable(overlayItems, func(i, j int) bool {
			li, lj := strings.ToLower(overlayItems[i].ItemID), strings.ToLower(overlayItems[j].ItemID)
			if li != lj {
				return li < lj
			}
			return overlayItems[i].ItemID < overlayItems[j].ItemID
		})
		overlay := common.RetrieverNode{
			NodeID: overlayNodeID, Label: overlaySegment,
			Description: fmt.Sprintf("%d of %d Skills are not in the other categories. Search here or omit category to search all Skills, including these.", len(overlayItems), len(items)),
			Items:       overlayItems,
		}
		root = common.RetrieverNode{
			NodeID: root.NodeID, Label: root.Label, Description: root.Description,
			Children: append(append([]common.RetrieverNode{}, root.Children...), overlay),
			Items:    root.Items,
		}
	}
	return root, catalog
}

// searchableCategoryDescription 分类描述净化（过滤前缀行）。
// 对齐 skillfs.py:810-818。
func searchableCategoryDescription(value string) string {
	var lines []string
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "covers ") ||
			strings.HasPrefix(lower, "representative ") || strings.HasPrefix(lower, "don't select when:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

// uniqueSegment 段名唯一化（哈希后缀 + 递增）。
// 对齐 skillfs.py:883-895。
func uniqueSegment(value, identity string, used map[string]bool) string {
	base := segmentBase(value)
	candidate := base
	if used[strings.ToLower(candidate)] {
		sum := sha256.Sum256([]byte(identity))
		candidate = fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:])[:8])
	}
	index := 2
	for used[strings.ToLower(candidate)] {
		candidate = fmt.Sprintf("%s-%d", base, index)
		index++
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

// segmentBase 段名安全化（非法字符→-，首尾 .- 剥离，空回落 unnamed）。
// 对齐 skillfs.py:898-900。
func segmentBase(value string) string {
	out := strings.Trim(safeSegmentRe.ReplaceAllString(strings.TrimSpace(value), "-"), ".-")
	if out == "" {
		return "unnamed"
	}
	return out
}

// joinPath 路径拼接（对齐 skillfs.py:903-904）
func joinPath(parent, segment string) string {
	if parent == "/" {
		return "/" + segment
	}
	return parent + "/" + segment
}

// estimateTokens token 估算（len/4 上取整）。
// 对齐 skillfs.py:907-908。
func estimateTokens(value string) int {
	n := len([]rune(value))
	if n < 0 {
		n = 0
	}
	return (n + 3) / 4
}

// resolveUserPath ~ 展开与绝对化
func resolveUserPath(path string) (string, error) {
	expanded := path
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		expanded = filepath.Join(home, expanded[1:])
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return expanded, nil
	}
	return abs, nil
}

// mapSliceToAny []map[string]any → []any
func mapSliceToAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

// uniqueStrings 去重
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// sameStringSet 集合等值
func sameStringSet(list []string, set map[string]bool) bool {
	if len(list) != len(set) {
		return false
	}
	for _, s := range list {
		if !set[s] {
			return false
		}
	}
	return true
}

// NodeByPathMap 暴露路径→节点映射（对齐 Python view.node_by_path 公有属性的只读遍历需求）。
func (v *SkillDirectoryView) NodeByPathMap() map[string]common.RetrieverNode {
	return v.nodeByPath
}
