package rsi

// ArtifactFilesService：按 RSI tasks 根目录约束读取节点产物目录和文件。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/artifact_files_service.py（309 行）。

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxPreviewBytes 单文件预览上限。
const maxPreviewBytes = 10 * 1024 * 1024

// maxArchiveBytes 压缩包预览上限。
const maxArchiveBytes = 256 * 1024 * 1024

// archiveViewDir 任务级压缩包预览缓存目录。
const archiveViewDir = ".rsi_artifact_views"

// taskIDPattern 任务 ID 形状。
var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// publicTopLevelDirs 公开产物顶层目录（物化的数据集/refs/模型 YAML 含解密
// key，紧邻引擎产出——仅这些顶层目录是公开产物面）。
var publicTopLevelDirs = map[string]bool{"artifact": true, "run": true, "snapshots": true, archiveViewDir: true}

// textSuffixes 文本后缀集。
var textSuffixes = map[string]bool{
	".bib": true, ".cfg": true, ".csv": true, ".css": true, ".diff": true,
	".h": true, ".htm": true, ".html": true, ".ini": true, ".js": true,
	".json": true, ".jsonl": true, ".jsx": true, ".log": true, ".markdown": true,
	".md": true, ".patch": true, ".py": true, ".sh": true, ".sty": true,
	".tex": true, ".toml": true, ".ts": true, ".tsx": true, ".tsv": true,
	".txt": true, ".xml": true, ".yaml": true, ".yml": true,
}

// archiveViewLock 压缩包物化互斥（进程级 RLock 语义）。
var archiveViewLock sync.Mutex

// ──────────────────────────── 结构体 ────────────────────────────

// ArtifactFilesService 产物文件浏览服务。
type ArtifactFilesService struct {
	store           *TaskStore
	artifactService *ArtifactService
}

// NewArtifactFilesService 构造。
func NewArtifactFilesService(store *TaskStore, artifactService *ArtifactService) *ArtifactFilesService {
	if artifactService == nil {
		artifactService = NewArtifactService(store.TasksRoot())
	}
	return &ArtifactFilesService{store: store, artifactService: artifactService}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ListFiles 目录树（压缩包物化为任务级可复用预览缓存）。
func (s *ArtifactFilesService) ListFiles(params map[string]any) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	artifactID := strings.TrimSpace(valueToString(params["artifact_id"]))
	var target string
	if artifactID != "" {
		if valueToString(params["path"]) != "" {
			return nil, NewBadRequest("path 和 artifact_id 只能提供一个", nil)
		}
		if !taskIDPattern.MatchString(artifactID) {
			return nil, NewBadRequest("artifact_id 格式无效", nil)
		}
		s.resolveTaskRoot(taskID)
		// 只读快照查找：不走 download 服务（避免释放任务在用产物引用）。
		snapshot, err := s.artifactService.Locate(taskID, &artifactID)
		if err != nil {
			return nil, err
		}
		target = s.resolvePath(snapshot.Path, taskID)
	} else {
		target = s.resolvePath(valueToString(params["path"]), taskID)
	}
	if !fileExists(target) && !dirExists(target) {
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物路径不存在: %s", target)}
	}
	taskRoot := s.resolveTaskRoot(taskID)
	var root, initialPath string
	var candidates []string
	switch {
	case fileExists(target) && strings.EqualFold(filepath.Ext(target), ".zip"):
		root = s.materializeArchive(target, taskID)
		initialPath = selectInitialFile(root, artifactID != "")
		candidates = walkAll(root)
	case dirExists(target):
		root = target
		initialPath = orEmptyPath(selectInitialFile(root, false))
		candidates = walkAll(root)
	default:
		// 节点级 ref 可能直接指向单文件：不把无关同级文件当作节点产物。
		root = filepath.Dir(target)
		initialPath = target
		candidates = []string{target}
	}
	files := []map[string]any{}
	for _, item := range candidates {
		if !isPublicPath(item, taskRoot) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(item), "/")
		isNestedCache := contains(parts, archiveViewDir) && filepath.Base(root) != archiveViewDir
		if contains(parts, "__MACOSX") || isNestedCache {
			continue
		}
		info, err := os.Stat(item)
		if err != nil {
			continue
		}
		size := int64(0)
		if info.Mode().IsRegular() {
			size = info.Size()
		}
		files = append(files, map[string]any{
			"name":        filepath.Base(item),
			"path":        item,
			"isDirectory": info.IsDir(),
			"size":        size,
			"type":        mimeTypeOf(item),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		di, _ := files[i]["isDirectory"].(bool)
		dj, _ := files[j]["isDirectory"].(bool)
		if di != dj {
			return di
		}
		return strings.ToLower(valueToString(files[i]["name"])) < strings.ToLower(valueToString(files[j]["name"]))
	})
	initialOut := any(nil)
	if initialPath != "" {
		initialOut = initialPath
	}
	return map[string]any{"root": root, "initial_path": initialOut, "files": files}, nil
}

// ReadFile 单文件读取（文本/base64 双编码）。
func (s *ArtifactFilesService) ReadFile(params map[string]any) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	target := s.resolvePath(valueToString(params["path"]), taskID)
	if !fileExists(target) {
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物文件不存在: %s", target)}
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPreviewBytes {
		return nil, NewBadRequest("产物文件超出预览大小限制", nil)
	}
	contentBytes, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	encoding := "base64"
	content := base64.StdEncoding.EncodeToString(contentBytes)
	if textSuffixes[strings.ToLower(filepath.Ext(target))] {
		content = string(contentBytes)
		encoding = "text"
	}
	return map[string]any{
		"path": target, "name": filepath.Base(target), "size": info.Size(),
		"type": mimeTypeOf(target), "encoding": encoding, "content": content,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePath 路径解析 + 任务边界 + 公开目录约束。
func (s *ArtifactFilesService) resolvePath(rawPath, taskID string) string {
	pathValue := strings.TrimSpace(rawPath)
	if pathValue == "" {
		panic(&RsiError{Code: "BAD_REQUEST", Message: "path 必填"})
	}
	taskRoot := s.resolveTaskRoot(taskID)
	candidate := expandUser(pathValue)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(taskRoot, candidate)
	}
	if a, err := filepath.Abs(candidate); err == nil {
		candidate = a
	}
	if candidate == taskRoot {
		return candidate
	}
	if !strings.HasPrefix(candidate, taskRoot) {
		panic(&RsiError{Code: "PATH_INVALID", Message: "产物路径超出当前任务目录"})
	}
	if !isPublicPath(candidate, taskRoot) {
		panic(&RsiError{Code: "PATH_INVALID", Message: "产物路径不在公开产物目录"})
	}
	return candidate
}

// resolveTaskRoot 任务根校验（ID 形状 + task.json 存在 + 越界拒绝）。
func (s *ArtifactFilesService) resolveTaskRoot(taskID string) string {
	if !taskIDPattern.MatchString(taskID) {
		panic(NewTaskNotFound(taskID))
	}
	tasksRoot := s.store.TasksRoot()
	if a, err := filepath.Abs(tasksRoot); err == nil {
		tasksRoot = a
	}
	taskRoot := filepath.Join(tasksRoot, taskID)
	if !fileExists(filepath.Join(taskRoot, "task.json")) {
		panic(NewTaskNotFound(taskID))
	}
	if a, err := filepath.Abs(taskRoot); err == nil {
		taskRoot = a
	}
	if !strings.HasPrefix(taskRoot, tasksRoot) {
		panic(NewTaskNotFound(taskID))
	}
	return taskRoot
}

// materializeArchive 压缩包 → 任务级预览缓存（zip-slip/符号链接/总量守卫）。
func (s *ArtifactFilesService) materializeArchive(archivePath, taskID string) string {
	taskRoot := s.resolveTaskRoot(taskID)
	cacheRoot := filepath.Join(taskRoot, archiveViewDir)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		panic(err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		panic(err)
	}
	digestInput := fmt.Sprintf("%s:%d:%d", archivePath, info.Size(), info.ModTime().UnixNano())
	sum := shaFileBytes([]byte(digestInput))
	digest := sum[:24]
	viewRoot := filepath.Join(cacheRoot, digest)
	marker := filepath.Join(cacheRoot, digest+".ready")
	archiveViewLock.Lock()
	defer archiveViewLock.Unlock()
	if dirExists(viewRoot) && fileExists(marker) {
		return viewRoot
	}
	tempRoot := filepath.Join(cacheRoot, fmt.Sprintf(".%s.tmp", digest))
	_ = os.RemoveAll(tempRoot)
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		panic(err)
	}
	cleanup := func() { _ = os.RemoveAll(tempRoot) }
	reader, err := os.Open(archivePath)
	if err != nil {
		cleanup()
		panic(&RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物压缩包无法读取: %s", filepath.Base(archivePath))})
	}
	defer reader.Close()
	zr, err := zip.NewReader(reader, info.Size())
	if err != nil {
		cleanup()
		panic(&RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物压缩包无法读取: %s", filepath.Base(archivePath))})
	}
	totalSize := int64(0)
	for _, zf := range zr.File {
		memberName := strings.ReplaceAll(zf.Name, "\\", "/")
		if path.IsAbs(memberName) || contains(strings.Split(memberName, "/"), "..") {
			cleanup()
			panic(&RsiError{Code: "PATH_INVALID", Message: "产物压缩包包含非法路径"})
		}
		var parts []string
		for _, part := range strings.Split(memberName, "/") {
			if part != "" && part != "." {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			continue
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			cleanup()
			panic(&RsiError{Code: "PATH_INVALID", Message: "产物压缩包不允许包含符号链接"})
		}
		target := filepath.Join(tempRoot, filepath.Join(parts...))
		if !strings.HasPrefix(target, tempRoot) {
			cleanup()
			panic(&RsiError{Code: "PATH_INVALID", Message: "产物压缩包包含非法路径"})
		}
		if zf.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		totalSize += int64(zf.UncompressedSize64)
		if totalSize > maxArchiveBytes {
			cleanup()
			panic(NewBadRequest("产物压缩包超出预览大小限制", nil))
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			panic(err)
		}
		src, err := zf.Open()
		if err != nil {
			cleanup()
			panic(&RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物压缩包无法读取: %s", filepath.Base(archivePath))})
		}
		dst, err := os.Create(target)
		if err != nil {
			src.Close()
			cleanup()
			panic(err)
		}
		_, _ = io.Copy(dst, src)
		src.Close()
		dst.Close()
	}
	_ = os.RemoveAll(viewRoot)
	if err := os.Rename(tempRoot, viewRoot); err != nil {
		cleanup()
		panic(err)
	}
	_ = os.WriteFile(marker, []byte("ready\n"), 0o644)
	return viewRoot
}

// selectInitialFile 初始展示文件（harness 优先 manifest；paper 优先产出）。
func selectInitialFile(root string, preferHarness bool) string {
	preferred := []string{
		"main.pdf", "main.tex", "paper.pdf", "paper.tex", "report.md",
		"research_summary.md", "experiment_design.md", "README.md",
		"rsi_artifact_manifest.json",
	}
	if preferHarness {
		preferred = []string{"manifest.json", "harness_config.yaml", "harness_config.yml", "README.md"}
	}
	var files []string
	_ = filepath.Walk(root, func(item string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(item), "/")
		if contains(parts, "__MACOSX") || contains(parts, "__rsi_artifact__") {
			return nil
		}
		files = append(files, item)
		return nil
	})
	if len(files) == 0 {
		return ""
	}
	byName := map[string][]string{}
	for _, item := range files {
		name := strings.ToLower(filepath.Base(item))
		byName[name] = append(byName[name], item)
	}
	for _, name := range preferred {
		if list, ok := byName[strings.ToLower(name)]; ok && len(list) > 0 {
			sort.Slice(list, func(i, j int) bool { return initialSortKey(list[i], root) < initialSortKey(list[j], root) })
			return list[0]
		}
	}
	sort.Slice(files, func(i, j int) bool { return initialSortKey(files[i], root) < initialSortKey(files[j], root) })
	return files[0]
}

// initialSortKey 生成产出优先于拷贝输入（对齐 Python _initial_file_sort_key）。
func initialSortKey(p, root string) int {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		rel = p
	}
	relative := strings.ToLower(filepath.ToSlash(rel))
	priority := 0
	if strings.Contains(relative, "patched_paper") || strings.Contains("/"+relative+"/", "/final/") {
		priority -= 100
	}
	if strings.Contains("/"+relative+"/", "/output/") {
		priority -= 20
	}
	if strings.Contains("/"+relative+"/", "/input/") || strings.Contains(relative, "original") {
		priority += 40
	}
	return priority
}

// mimeTypeOf MIME 归一（对齐 Python _mime_type）。
func mimeTypeOf(p string) string {
	suffix := strings.ToLower(filepath.Ext(p))
	switch suffix {
	case ".jsonl":
		return "application/x-ndjson"
	case ".diff", ".log", ".patch", ".out":
		return "text/plain"
	case ".csv":
		return "text/csv"
	}
	if textSuffixes[suffix] {
		return "text/plain"
	}
	return "application/octet-stream"
}

// isPublicPath 路径是否属于公开产物子树。
func isPublicPath(p, taskRoot string) bool {
	rel, err := filepath.Rel(taskRoot, p)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) > 0 && publicTopLevelDirs[parts[0]]
}

// walkAll 递归列出全部条目。
func walkAll(root string) []string {
	out := []string{}
	_ = filepath.Walk(root, func(item string, info os.FileInfo, err error) error {
		if err == nil && item != root {
			out = append(out, item)
		}
		return nil
	})
	return out
}

// contains 字符串列表包含。
func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// orEmptyPath 空路径归一。
func orEmptyPath(p string) string { return p }
