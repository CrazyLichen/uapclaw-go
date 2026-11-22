package rsi

// RSI Harness 激活指针存储与安装器。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/harness_activation.py（995 行）。
// - 激活指针是工作区级索引；已装 Harness 版本由 installer 复制进各任务
//   harness/versions 目录（不可变、内容寻址）。
// - 广播回调由 17.9c Task 13 注入 AgentManager 实现。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// activationSchemaVersion 激活文档 schema 版本。
const activationSchemaVersion = 1

// ──────────────────────────── 结构体 ────────────────────────────

// PublishedHarnessRef 单个最终引擎 refs 解析结果。
type PublishedHarnessRef struct {
	// RefsPath 发布 refs 文件路径。
	RefsPath string
	// Role 角色。
	Role string
	// PackagePath 包目录。
	PackagePath string
	// Metadata refs roles 元数据。
	Metadata map[string]any
	// RefsPayload 原始 refs 文档。
	RefsPayload map[string]any
}

// HarnessActivationStore RSI 专用持久激活指针 + 不可变安装历史。
type HarnessActivationStore struct {
	root           string
	activationPath string
	lock           sync.Mutex
}

// NewHarnessActivationStore 构造（root 展开为绝对路径）。
func NewHarnessActivationStore(root string) *HarnessActivationStore {
	abs, err := filepath.Abs(expandUser(root))
	if err == nil {
		root = abs
	}
	return &HarnessActivationStore{root: root, activationPath: filepath.Join(root, "activation.json")}
}

// BroadcastFunc AgentManager 热加载广播缝（Task 13 注入）。
type BroadcastFunc func(old, new map[string]any) map[string]any

// HarnessInstaller 引擎发布 Harness 版本的安装/回退。
type HarnessInstaller struct {
	store           *TaskStore
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
	broadcast       BroadcastFunc
	activationStore *HarnessActivationStore
	installLock     sync.Mutex
}

// NewHarnessInstaller 构造（broadcast 可为 nil → attempted=0）。
func NewHarnessInstaller(store *TaskStore, adapterResolver func(scenario, artifactType string) ScenarioAdapter,
	broadcast BroadcastFunc, activationStore *HarnessActivationStore) *HarnessInstaller {
	if activationStore == nil {
		activationStore = NewHarnessActivationStore(store.TasksRoot())
	}
	return &HarnessInstaller{store: store, adapterResolver: adapterResolver,
		broadcast: broadcast, activationStore: activationStore}
}

// ──────────────────────────── 导出函数（ActivationStore） ────────────────────────────

// GetActive 激活记录（runtime_path 失效视为无激活）。
func (s *HarnessActivationStore) GetActive() map[string]any {
	active := s.readDocument()["active"]
	m, _ := active.(map[string]any)
	if m == nil {
		return nil
	}
	if s.validateRuntimePath(valueToString(m["runtime_path"]), true) == "" {
		return nil
	}
	return m
}

// ListHistory 历史记录。
func (s *HarnessActivationStore) ListHistory() []map[string]any {
	out := []map[string]any{}
	for _, item := range readAnyList(s.readDocument()["history"]) {
		out = append(out, item)
	}
	return out
}

// ListVersions 全部保留安装记录（按首次安装排序）。
func (s *HarnessActivationStore) ListVersions() []map[string]any {
	doc := s.readDocument()
	records := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range append(readAnyList(doc["history"]), asMap(doc["active"])) {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(valueToString(item["installation_id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		records = append(records, item)
	}
	sort.Slice(records, func(i, j int) bool {
		si, sj := installSequence(records[i]), installSequence(records[j])
		if si != sj {
			return si < sj
		}
		return valueToString(records[i]["installed_at"]) < valueToString(records[j]["installed_at"])
	})
	return records
}

// GetVersion 单条安装记录。
func (s *HarnessActivationStore) GetVersion(installationID string) map[string]any {
	wanted := strings.TrimSpace(installationID)
	if wanted == "" {
		return nil
	}
	for _, record := range s.ListVersions() {
		if valueToString(record["installation_id"]) == wanted {
			return record
		}
	}
	return nil
}

// Commit 记录写入为激活版本（历史轮换 + 序号续排，原子落盘）。
func (s *HarnessActivationStore) Commit(record map[string]any) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	installationID := strings.TrimSpace(valueToString(record["installation_id"]))
	if installationID == "" || filepath.Base(installationID) != installationID {
		return fmt.Errorf("RSI Harness installation_id 非法")
	}
	runtimePath := s.validateRuntimePath(valueToString(record["runtime_path"]), true)
	if runtimePath == "" {
		return fmt.Errorf("RSI Harness runtime_path 不存在或不是目录")
	}
	sha := strings.TrimSpace(valueToString(record["sha256"]))
	if len(sha) != 64 || !isHex(sha) {
		return fmt.Errorf("RSI Harness sha256 非法")
	}
	doc := s.readDocument()
	previous := asMap(doc["active"])
	history := []map[string]any{}
	for _, item := range readAnyList(doc["history"]) {
		id := strings.TrimSpace(valueToString(item["installation_id"]))
		if id == "" || id == installationID {
			continue
		}
		history = append(history, item)
	}
	if previous != nil && valueToString(previous["installation_id"]) != installationID {
		prevID := strings.TrimSpace(valueToString(previous["installation_id"]))
		filtered := []map[string]any{}
		for _, item := range history {
			if valueToString(item["installation_id"]) != prevID {
				filtered = append(filtered, item)
			}
		}
		history = append(filtered, previous)
	}
	normalized := map[string]any{}
	for k, v := range record {
		normalized[k] = v
	}
	normalized["runtime_path"] = runtimePath
	sequence := installSequence(normalized)
	if sequence < 1 {
		max := 0
		for _, item := range history {
			if v := installSequence(item); v > max {
				max = v
			}
		}
		if previous != nil {
			if v := installSequence(previous); v > max {
				max = v
			}
		}
		normalized["version_sequence"] = max + 1
	}
	return s.writeDocument(map[string]any{
		"schema_version": activationSchemaVersion,
		"active":         normalized,
		"history":        history,
	})
}

// ResolveActiveRuntimePath 激活 runtime 路径（无激活返回 ""）。
func (s *HarnessActivationStore) ResolveActiveRuntimePath() string {
	active := s.GetActive()
	if active == nil {
		return ""
	}
	return valueToString(active["runtime_path"])
}

// ──────────────────────────── 非导出（ActivationStore） ────────────────────────────

// readDocument 读激活文档（损坏/缺失按空文档兜底）。
func (s *HarnessActivationStore) readDocument() map[string]any {
	empty := map[string]any{"schema_version": activationSchemaVersion, "active": nil, "history": []any{}}
	if !fileExists(s.activationPath) {
		return empty
	}
	data, err := os.ReadFile(s.activationPath)
	if err != nil {
		return empty
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
		return empty
	}
	history := payload["history"]
	if _, ok := history.([]any); !ok {
		history = []any{}
	}
	return payload
}

// writeDocument 原子写激活文档。
func (s *HarnessActivationStore) writeDocument(document map[string]any) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.root, fmt.Sprintf(".activation.%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.activationPath)
}

// validateRuntimePath runtime 路径校验（必须在 root 内；requireExists 时目录存在）。
func (s *HarnessActivationStore) validateRuntimePath(value string, requireExists bool) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	runtime := expandUser(value)
	if a, err := filepath.Abs(runtime); err == nil {
		runtime = a
	}
	if !strings.HasPrefix(runtime, s.root) {
		return ""
	}
	if requireExists && !dirExists(runtime) {
		return ""
	}
	return runtime
}

// ──────────────────────────── 导出函数（Installer） ────────────────────────────

// Install 安装一个引擎发布 Harness 版本并安全热加载（锁内全流程）。
func (h *HarnessInstaller) Install(taskID string) (map[string]any, error) {
	h.installLock.Lock()
	defer h.installLock.Unlock()
	return h.installUnlocked(taskID)
}

// ListVersions 保留版本清单（不暴露本地路径）。
func (h *HarnessInstaller) ListVersions() map[string]any {
	active := h.activationStore.GetActive()
	activeID := ""
	if active != nil {
		activeID = strings.TrimSpace(valueToString(active["installation_id"]))
	}
	records := h.activationStore.ListVersions()
	initialID := ""
	if len(records) > 0 {
		initialID = strings.TrimSpace(valueToString(records[0]["installation_id"]))
	}
	versions := []map[string]any{}
	for _, record := range records {
		runtime := h.activationStore.validateRuntimePath(valueToString(record["runtime_path"]), false)
		id := strings.TrimSpace(valueToString(record["installation_id"]))
		versions = append(versions, map[string]any{
			"installation_id": id,
			"task_id":         record["task_id"],
			"node_id":         record["node_id"],
			"harness_name":    firstNonEmpty(valueToString(record["harness_name"]), valueToString(record["extension_name"])),
			"sha256":          record["sha256"],
			"installed_at":    record["installed_at"],
			"is_active":       id == activeID,
			"is_initial":      id == initialID,
			"available":       runtime != "" && dirExists(runtime),
		})
	}
	activeOut := any(nil)
	if activeID != "" {
		activeOut = activeID
	}
	return map[string]any{"active_installation_id": activeOut, "versions": versions}
}

// Rollback 回退到保留版本（RSI 任务必须全空闲）。
func (h *HarnessInstaller) Rollback(installationID string) (map[string]any, error) {
	h.installLock.Lock()
	defer h.installLock.Unlock()
	wanted := strings.TrimSpace(installationID)
	if wanted == "" {
		return nil, NewBadRequest("installation_id 必填", nil)
	}
	target := h.activationStore.GetVersion(wanted)
	if target == nil {
		return nil, NewBadRequest(fmt.Sprintf("未找到已安装的 RSI Harness 版本: %s", wanted), nil)
	}
	old := h.activationStore.GetActive()
	if old != nil && valueToString(old["installation_id"]) == wanted {
		response := installerResponse(old, valueToString(old["task_id"]), valueToString(old["node_id"]), true,
			asMap(old["hot_load"]))
		response["from_installation_id"] = wanted
		response["rolled_back"] = false
		return response, nil
	}
	if err := h.assertRollbackAllowed(); err != nil {
		return nil, err
	}
	if err := h.validateRollbackTarget(target); err != nil {
		return nil, err
	}
	target["status"] = "ACTIVE"
	target["activated_at"] = UtcnowISO()
	target["activation_reason"] = "rollback"
	hotLoad := h.broadcastVersions(old, target)
	target["hot_load"] = hotLoad
	if err := h.activationStore.Commit(target); err != nil {
		return nil, &RsiError{Code: "RSI_HARNESS_INSTALL_FAILED", Message: "RSI Harness 回退指针写入失败，已恢复旧版本"}
	}
	response := installerResponse(target, valueToString(target["task_id"]), valueToString(target["node_id"]), false, hotLoad)
	response["from_installation_id"] = anyOrNil(old, "installation_id")
	response["rolled_back"] = true
	return response, nil
}

// ──────────────────────────── 非导出（Installer） ────────────────────────────

// installUnlocked 安装主链（对齐 Python _install_unlocked 步骤序）。
func (h *HarnessInstaller) installUnlocked(taskID string) (map[string]any, error) {
	if taskID == "" || filepath.Base(taskID) != taskID {
		return nil, &RsiError{Code: "RSI_HARNESS_INVALID", Message: fmt.Sprintf("任务 task_id 非法: %s", taskID)}
	}
	task, err := h.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if task.Scenario != "HARNESS" {
		return nil, &RsiError{Code: "RSI_HARNESS_NOT_READY", Message: "只有 HARNESS 任务可以安装优化 Harness"}
	}
	if task.Status != string(TaskStatusCompleted) {
		return nil, &RsiError{Code: "RSI_HARNESS_NOT_READY", Message: fmt.Sprintf("任务 %s 尚未完成", taskID)}
	}
	runRoot := h.taskRunRoot(&task)
	refsPath := strings.TrimSpace(valueToString(task.Config["published_harness_refs_path"]))
	if refsPath == "" {
		return nil, &RsiError{Code: "RSI_HARNESS_NOT_PUBLISHED", Message: fmt.Sprintf("任务 %s 缺少 published_harness_refs_path", taskID)}
	}
	if !fileExists(refsPath) {
		return nil, &RsiError{Code: "RSI_HARNESS_NOT_PUBLISHED", Message: fmt.Sprintf("published harness refs 不存在: %s", refsPath)}
	}
	parsed, err := parsePublishedHarnessRefs(refsPath, runRoot)
	if err != nil {
		return nil, err
	}
	packageSha, err := hashHarnessPackage(parsed.PackagePath)
	if err != nil {
		return nil, &RsiError{Code: "RSI_HARNESS_INVALID", Message: fmt.Sprintf("Harness 发布物校验失败: %v", err)}
	}
	extensionName, manifestPath, err := readManifestExtensionName(parsed.PackagePath)
	if err != nil {
		return nil, err
	}
	// 已装同 ID 版本：内容一致则幂等返回，不一致即冲突。
	taskVersionsRoot := filepath.Join(expandUser(h.store.TasksRoot()), taskID, "harness", "versions")
	installationID := fmt.Sprintf("%s-%s", extensionName, packageSha[:16])
	runtimePath := filepath.Join(taskVersionsRoot, extensionName, installationID)
	if dirExists(runtimePath) {
		if existing, err := hashHarnessPackage(runtimePath); err != nil || existing != packageSha {
			return nil, &RsiError{Code: "RSI_HARNESS_INSTALL_CONFLICT",
				Message: fmt.Sprintf("安装版本 %s 已存在但内容 hash 不一致", installationID)}
		}
		active := h.activationStore.GetActive()
		if active != nil && valueToString(active["installation_id"]) == installationID {
			response := installerResponse(active, taskID, valueToString(active["node_id"]), true, asMap(active["hot_load"]))
			response["already_active"] = true
			return response, nil
		}
	} else {
		staging := filepath.Join(taskVersionsRoot, extensionName, fmt.Sprintf(".staging-%d", time.Now().UnixNano()))
		if err := copyTree(parsed.PackagePath, staging); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
			return nil, err
		}
		if err := os.Rename(staging, runtimePath); err != nil {
			_ = os.RemoveAll(staging)
			return nil, err
		}
		installedRefsPath := filepath.Join(runtimePath, "harness_refs.yaml")
		if err := writeVersionRefs(installedRefsPath, parsed, extensionName); err != nil {
			return nil, err
		}
	}
	old := h.activationStore.GetActive()
	record := map[string]any{
		"installation_id":   installationID,
		"task_id":           taskID,
		"node_id":           valueToString(task.Config["node_id"]),
		"harness_name":      extensionName,
		"extension_name":    extensionName,
		"manifest_path":     manifestPath,
		"runtime_path":      runtimePath,
		"sha256":            packageSha,
		"installed_at":      UtcnowISO(),
		"activated_at":      UtcnowISO(),
		"activation_reason": "install",
		"status":            "ACTIVE",
	}
	// 先热加载，指针后提交（失败恢复旧版本）。
	hotLoad := h.broadcastVersions(old, record)
	record["hot_load"] = hotLoad
	if err := h.activationStore.Commit(record); err != nil {
		h.broadcastVersions(record, old)
		return nil, &RsiError{Code: "RSI_HARNESS_INSTALL_FAILED", Message: "RSI Harness active 指针写入失败，已恢复旧版本"}
	}
	// 任务 provenance 落 task.json（热加载失败时安装留痕）。
	_ = h.store.MergeConfig(taskID, map[string]any{
		"installed_harness": map[string]any{
			"installation_id": installationID, "runtime_path": runtimePath, "sha256": packageSha,
		},
	})
	response := installerResponse(record, taskID, valueToString(record["node_id"]), false, hotLoad)
	response["already_active"] = false
	return response, nil
}

// assertRollbackAllowed 回退门禁：存在排队/运行/暂停任务即拒。
func (h *HarnessInstaller) assertRollbackAllowed() error {
	blocking := []string{}
	for _, task := range h.store.List("", "") {
		switch upper(task.Status) {
		case "QUEUED", "RUNNING", "PAUSED":
			blocking = append(blocking, task.TaskID)
		}
	}
	if len(blocking) > 0 {
		sort.Strings(blocking)
		return NewTaskStateConflict("存在排队、运行或暂停的 RSI 任务，不能回退 Harness: " + strings.Join(blocking, ", "))
	}
	return nil
}

// validateRollbackTarget 回退目标校验（runtime 存在 + 内容 hash 一致）。
func (h *HarnessInstaller) validateRollbackTarget(record map[string]any) error {
	runtime := h.activationStore.validateRuntimePath(valueToString(record["runtime_path"]), false)
	if runtime == "" {
		return &RsiError{Code: "RSI_HARNESS_INVALID", Message: "回退目标的 runtime_path 非法"}
	}
	if !dirExists(runtime) {
		return &RsiError{Code: "RSI_HARNESS_NOT_READY", Message: "回退目标的本地 Harness 版本不存在"}
	}
	expectedHash := strings.ToLower(strings.TrimSpace(valueToString(record["sha256"])))
	if actual, err := hashHarnessPackage(runtime); err != nil || strings.ToLower(actual) != expectedHash {
		return &RsiError{Code: "RSI_HARNESS_INVALID", Message: "回退目标 Harness 内容已变化"}
	}
	return nil
}

// broadcastVersions 广播（nil 广播缝 → attempted=0）。
func (h *HarnessInstaller) broadcastVersions(old, new map[string]any) map[string]any {
	if h.broadcast == nil {
		return map[string]any{"attempted": 0, "succeeded": 0, "failed": []any{}}
	}
	result := h.broadcast(old, new)
	if result == nil {
		return map[string]any{"attempted": 0, "succeeded": 0, "failed": []any{}}
	}
	return result
}

// taskRunRoot 任务 run 目录边界校验。
func (h *HarnessInstaller) taskRunRoot(task *RsiTask) string {
	taskRoot := expandUser(filepath.Join(h.store.TasksRoot(), task.TaskID))
	runRoot := filepath.Join(taskRoot, "run")
	if !strings.HasPrefix(taskRoot, expandUser(h.store.TasksRoot())) {
		return ""
	}
	if !dirExists(runRoot) {
		return ""
	}
	return runRoot
}

// ──────────────────────────── refs 解析（模块级） ────────────────────────────

// parsePublishedHarnessRefs 解析最终 refs 并强制任务边界。
// 相对 refs 相对 refs_path.parent 解析（对齐引擎装载器）。
func parsePublishedHarnessRefs(refsPath, runRoot string) (*PublishedHarnessRef, error) {
	invalid := func(msg string) *RsiError { return &RsiError{Code: "RSI_HARNESS_INVALID", Message: msg} }
	if !strings.HasPrefix(refsPath, runRoot) {
		return nil, invalid("published refs 超出任务 run 边界: " + refsPath)
	}
	data, err := os.ReadFile(refsPath)
	if err != nil {
		return nil, invalid("published harness refs 不存在: " + refsPath)
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil || payload == nil {
		return nil, invalid("published harness refs 必须是 mapping")
	}
	rawRefs, ok := payload["harness_refs"].(map[string]any)
	if !ok || len(rawRefs) == 0 {
		return nil, invalid("published harness refs 缺少 harness_refs mapping")
	}
	entries := [][2]string{}
	for role, ref := range rawRefs {
		if strings.TrimSpace(role) != "" && valueToString(ref) != "" {
			entries = append(entries, [2]string{strings.TrimSpace(role), valueToString(ref)})
		}
	}
	if len(entries) != 1 {
		return nil, invalid("单 Harness 发布物必须恰好一个 role")
	}
	role, rawRef := entries[0][0], entries[0][1]
	pkg := expandUser(rawRef)
	if !filepath.IsAbs(pkg) {
		pkg = filepath.Join(filepath.Dir(refsPath), pkg)
	}
	if a, err := filepath.Abs(pkg); err == nil {
		pkg = a
	}
	if !strings.HasPrefix(pkg, runRoot) {
		return nil, invalid("Harness 包超出任务 run 边界: " + pkg)
	}
	if !dirExists(pkg) {
		return nil, invalid("Harness 包目录不存在: " + pkg)
	}
	if _, _, err := readManifestExtensionName(pkg); err != nil {
		return nil, err
	}
	metadata, err := roleMetadata(payload, role, pkg, refsPath)
	if err != nil {
		return nil, err
	}
	return &PublishedHarnessRef{RefsPath: refsPath, Role: role, PackagePath: pkg,
		Metadata: metadata, RefsPayload: payload}, nil
}

// roleMetadata refs roles 元数据一致性校验（对齐 Python _role_metadata）。
func roleMetadata(payload map[string]any, role, resolvedPath, refsPath string) (map[string]any, error) {
	invalid := func(msg string) *RsiError { return &RsiError{Code: "RSI_HARNESS_INVALID", Message: msg} }
	rawRoles, hasRoles := payload["roles"]
	if !hasRoles || rawRoles == nil {
		return map[string]any{"role": role, "member_name": role, "harness_ref_path": resolvedPath}, nil
	}
	list, ok := rawRoles.([]any)
	if !ok {
		return nil, invalid("published harness refs 的 roles 必须是 list")
	}
	matches := []map[string]any{}
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entryRole := strings.TrimSpace(valueToString(entry["role"]))
		entryMember := strings.TrimSpace(valueToString(entry["member_name"]))
		if entryRole == role || entryMember == role {
			matches = append(matches, entry)
		}
	}
	if len(matches) != 1 {
		return nil, invalid(fmt.Sprintf("roles 中必须存在与 harness_refs role '%s' 对应的唯一项", role))
	}
	metadata := map[string]any{}
	for k, v := range matches[0] {
		metadata[k] = v
	}
	if entryRef := strings.TrimSpace(valueToString(metadata["harness_ref_path"])); entryRef != "" {
		refPath := expandUser(entryRef)
		if !filepath.IsAbs(refPath) {
			refPath = filepath.Join(filepath.Dir(refsPath), refPath)
		}
		if refPath != resolvedPath {
			return nil, invalid(fmt.Sprintf("roles[%s] 的 harness_ref_path 与 harness_refs 不一致", role))
		}
	}
	metadata["harness_ref_path"] = resolvedPath
	return metadata, nil
}

// hashHarnessPackage 包路径与内容的确定性 SHA-256。
func hashHarnessPackage(packagePath string) (string, error) {
	if !dirExists(packagePath) {
		return "", fmt.Errorf("Harness package must be a directory: %s", packagePath)
	}
	digest := sha256.New()
	var files []string
	_ = filepath.Walk(packagePath, func(item string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		files = append(files, item)
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		rel := func(p string) string { return strings.ReplaceAll(strings.TrimPrefix(p, packagePath+"/"), "\\", "/") }
		return rel(files[i]) < rel(files[j])
	})
	for _, item := range files {
		info, err := os.Lstat(item)
		if err != nil {
			continue
		}
		relative := strings.ReplaceAll(strings.TrimPrefix(item, packagePath+"/"), "\\", "/")
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(item)
			digest.Write([]byte("L"))
			digest.Write([]byte(fmt.Sprintf("%08x", len(relative))))
			digest.Write([]byte(relative))
			digest.Write([]byte(fmt.Sprintf("%08x", len(target))))
			digest.Write([]byte(target))
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		digest.Write([]byte("F"))
		digest.Write([]byte(fmt.Sprintf("%08x", len(relative))))
		digest.Write([]byte(relative))
		data, _ := os.ReadFile(item)
		digest.Write(data)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// readManifestExtensionName manifest → 扩展名（严格字符校验）。
func readManifestExtensionName(pkg string) (string, string, error) {
	manifest, err := harnessConfigPath(pkg)
	if err != nil {
		return "", "", err
	}
	var payload map[string]any
	if filepath.Base(manifest) == "manifest.json" {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return "", "", &RsiError{Code: "RSI_HARNESS_INVALID", Message: "Harness manifest 不可读: " + manifest}
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
			return "", "", &RsiError{Code: "RSI_HARNESS_INVALID", Message: "Harness manifest 必须是 mapping: " + manifest}
		}
	} else {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return "", "", &RsiError{Code: "RSI_HARNESS_INVALID", Message: "Harness manifest 不可读: " + manifest}
		}
		if err := yaml.Unmarshal(data, &payload); err != nil || payload == nil {
			return "", "", &RsiError{Code: "RSI_HARNESS_INVALID", Message: "Harness manifest 必须是 mapping: " + manifest}
		}
	}
	extensionName := strings.TrimSpace(firstNonEmpty(
		valueToString(payload["extension_name"]), valueToString(payload["id"]), filepath.Base(pkg)))
	if extensionName == "" || extensionName == "." || extensionName == ".." ||
		filepath.Base(extensionName) != extensionName ||
		strings.ContainsAny(extensionName, "/\\:") {
		return "", "", &RsiError{Code: "RSI_HARNESS_INVALID", Message: fmt.Sprintf("Harness extension_name 非法: %q", extensionName)}
	}
	return extensionName, manifest, nil
}

// writeVersionRefs 安装 refs 自包含化（保留引擎元数据）。
func writeVersionRefs(refsPath string, parsed *PublishedHarnessRef, packageName string) error {
	payload := map[string]any{}
	for k, v := range parsed.RefsPayload {
		payload[k] = v
	}
	payload["harness_refs"] = map[string]any{parsed.Role: packageName}
	if roles, ok := payload["roles"].([]any); ok {
		rewritten := []any{}
		for _, item := range roles {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			roleName := strings.TrimSpace(valueToString(entry["role"]))
			memberName := strings.TrimSpace(valueToString(entry["member_name"]))
			if roleName == parsed.Role || memberName == parsed.Role {
				entry["harness_ref_path"] = packageName
			}
			rewritten = append(rewritten, entry)
		}
		payload["roles"] = rewritten
	}
	if err := os.MkdirAll(filepath.Dir(refsPath), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(refsPath, data, 0o644)
}

// installerResponse 统一安装响应形状。
func installerResponse(record map[string]any, taskID, nodeID string, alreadyActive bool, hotLoad map[string]any) map[string]any {
	return map[string]any{
		"installation_id": record["installation_id"],
		"task_id":         taskID,
		"node_id":         anyOrNilStr(nodeID),
		"runtime_path":    record["runtime_path"],
		"sha256":          record["sha256"],
		"status":          record["status"],
		"activated_at":    record["activated_at"],
		"already_active":  alreadyActive,
		"hot_load":        hotLoad,
	}
}

// ──────────────────────────── 小助手 ────────────────────────────

// installSequence 版本序号（非法 → 0）。
func installSequence(item map[string]any) int {
	return safeInt(item["version_sequence"])
}

// asMap any → map（nil 安全）。
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// readAnyList any → []map（畸形过滤）。
func readAnyList(v any) []map[string]any {
	items, _ := v.([]any)
	out := []map[string]any{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// anyOrNil 记录字段取值。
func anyOrNil(record map[string]any, key string) any {
	if record == nil {
		return nil
	}
	return record[key]
}

// anyOrNilStr 空串 → nil。
func anyOrNilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isHex 十六进制校验。
func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}
