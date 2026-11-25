package adapter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponentWorkspace workspace 面板日志组件。
const logComponentWorkspace = logger.ComponentAgentServer

// 目录删除安全上限（设计：限深限量；未给精确数字时取保守值）。
// Python: service.py:26-29。
const (
	defaultMaxDeleteDepth   = 32
	defaultMaxDeleteEntries = 5000
	defaultPreviewMaxBytes  = 65536
	// download 半区（Python service.py:311-384 download_payload）已由 27.8
	// sealed-asset 下载链覆盖，本批不移植（spec D-B2-13）。
	defaultDownloadMaxBytes = 100 * 1024 * 1024
)

// WorkspaceError 业务错误；code 对齐 Web HTTP error.code。
// Python: service.py:32-40 WorkspaceError。
type WorkspaceError struct {
	// Code HTTP 错误码（BAD_REQUEST/NOT_FOUND/FORBIDDEN/INTERNAL_ERROR）
	Code string
	// Message 错误消息
	Message string
	// Details 附加明细
	Details map[string]any
}

func (e *WorkspaceError) Error() string { return e.Message }

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspaceService 对单一工作区根目录提供 list / delete / preview / du。
// Python: service.py:42-49 WorkspaceService（tenant_root + 三个上限字段）。
// 单机版无租户/配额概念（D-B2-4）；download_payload 不移植（D-B2-13）。
type WorkspaceService struct {
	// tenantRoot 工作区根目录
	tenantRoot string
	// maxDeleteDepth 目录删除深度上限
	maxDeleteDepth int
	// maxDeleteEntries 目录删除条目上限
	maxDeleteEntries int
	// maxDownloadBytes 下载字节上限（保留字段，对齐 Python 构造签名）
	maxDownloadBytes int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkspaceService 构造服务（上限取默认值）。
func NewWorkspaceService(tenantRoot string) *WorkspaceService {
	return &WorkspaceService{
		tenantRoot:       tenantRoot,
		maxDeleteDepth:   defaultMaxDeleteDepth,
		maxDeleteEntries: defaultMaxDeleteEntries,
		maxDownloadBytes: defaultDownloadMaxBytes,
	}
}

// ListTree 列工作区目录树（含 zone / deletable）。
// Python: service.py:68-123 list_tree。
func (s *WorkspaceService) ListTree(relativePath string) (map[string]any, *WorkspaceError) {
	rel, perr := workspaceNormalizeRelativePath(relativePath)
	if perr != "" {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "invalid relative_path"}
	}
	target, rerr := s.resolve(rel)
	if rerr != nil {
		return nil, rerr
	}
	st, err := os.Stat(target)
	if err != nil {
		// 根目录尚未物化：返回空列表，避免前端整页失败；子路径仍 404。
		if rel == "" {
			return map[string]any{"relative_path": rel, "entries": []any{}}, nil
		}
		return nil, &WorkspaceError{Code: "NOT_FOUND", Message: "path not found"}
	}
	if !st.IsDir() {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "relative_path is not a directory"}
	}

	dirEntries, rerr2 := os.ReadDir(target)
	if rerr2 != nil {
		return nil, &WorkspaceError{Code: "INTERNAL_ERROR", Message: rerr2.Error()}
	}
	children := []string{}
	isDirMap := map[string]bool{}
	for _, entry := range dirEntries {
		children = append(children, entry.Name())
		isDirMap[entry.Name()] = entry.IsDir()
	}
	sorted := workspaceSortedChildren(children, func(name string) bool { return isDirMap[name] })

	result := []map[string]any{}
	for _, child := range sorted {
		childRel := child
		if rel != "" {
			childRel = rel + "/" + child
		}
		childRel = strings.ReplaceAll(childRel, "\\", "/")
		zone := workspaceClassifyZone(childRel)
		childPath := filepath.Join(target, child)
		cst, serr := os.Stat(childPath)
		if serr != nil {
			continue
		}
		isDir := isDirMap[child]
		var size any
		if isDir {
			size = nil // 可选懒加载；P0 目录占用为 null
		} else {
			size = cst.Size()
		}
		mimeHint := ""
		if !isDir {
			mimeHint = workspaceMimeHint(child)
		}
		result = append(result, map[string]any{
			"name":          child,
			"relative_path": childRel,
			"is_dir":        isDir,
			"size_bytes":    size,
			"mtime_ms":      cst.ModTime().UnixMilli(),
			"ctime_ms":      fileCTimeMs(cst), // 对齐 Python st_ctime
			"zone":          string(zone),
			"deletable":     workspaceIsDeletable(childRel),
			"mime_hint":     mimeHint,
		})
	}
	return map[string]any{"relative_path": rel, "entries": result}, nil
}

// DeleteEntries 批量删除（逐项独立返回结果）。
// Python: service.py:125-129 delete_entries。
func (s *WorkspaceService) DeleteEntries(relativePaths []any) map[string]any {
	results := []map[string]any{}
	for _, raw := range relativePaths {
		results = append(results, s.deleteOne(fmt.Sprint(raw)))
	}
	return map[string]any{"results": results}
}

// PreviewFile 预览文件：文本返回截断正文；二进制 content=null；目录 → 400。
// Python: service.py:255-309 preview_file。
func (s *WorkspaceService) PreviewFile(relativePath string, maxBytes any) (map[string]any, *WorkspaceError) {
	rel, perr := workspaceNormalizeRelativePath(relativePath)
	if perr != "" {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "invalid relative_path"}
	}
	if rel == "" {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "relative_path required"}
	}
	target, rerr := s.resolve(rel)
	if rerr != nil {
		return nil, rerr
	}
	st, err := os.Stat(target)
	if err != nil {
		return nil, &WorkspaceError{Code: "NOT_FOUND", Message: "path not found"}
	}
	if st.IsDir() {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "path is a directory"}
	}
	if !st.Mode().IsRegular() {
		return nil, &WorkspaceError{Code: "BAD_REQUEST", Message: "path is not a file"}
	}

	limit := defaultPreviewMaxBytes
	if n, ok := maxBytes.(float64); ok && n > 0 {
		limit = int(n)
		if limit > 1024*1024 {
			limit = 1024 * 1024 // adapter 侧 min(max_bytes, 1MiB)，Python adapter :213
		}
	}
	if limit <= 0 {
		limit = defaultPreviewMaxBytes
	}
	handle, oerr := os.Open(target)
	if oerr != nil {
		return nil, &WorkspaceError{Code: "INTERNAL_ERROR", Message: oerr.Error()}
	}
	defer handle.Close()
	// 只读前 limit(+1) 字节，避免超大文件整文件进内存导致 OOM。
	sample := make([]byte, limit+1)
	n, _ := handle.Read(sample)

	truncated := n > limit
	sample = sample[:minInt(n, limit)]
	if looksBinary(sample) {
		return map[string]any{"relative_path": rel, "truncated": truncated, "content": nil}, nil
	}
	// 对齐 Python decode("utf-8")（严格）失败后 decode("utf-8", errors="replace")
	text := string(sample)
	if !utf8.Valid(sample) {
		text = strings.ToValidUTF8(string(sample), "\uFFFD")
	}
	return map[string]any{"relative_path": rel, "truncated": truncated, "content": text}, nil
}

// MeasureUsedBytes 全量 du（du -s -B1 优先；不可用时 walk 兜底）。
// Python: service.py:386-391 + :407-434 measure_used_bytes。
func (s *WorkspaceService) MeasureUsedBytes() int64 {
	root := filepath.Clean(s.tenantRoot)
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return 0
	}
	return measureUsedBytes(root)
}

// measureUsedBytes 运行 /usr/bin/du -s -B1；失败走 walk 兜底。
// Python: service.py:407-434 measure_used_bytes。
func measureUsedBytes(root string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/du", "-s", "-B1", root)
	output, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
		token := strings.TrimSpace(strings.SplitN(line, "\t", 2)[0])
		if n, perr := strconv.ParseInt(token, 10, 64); perr == nil && n > 0 {
			return n
		}
	} else {
		logger.Debug(logComponentWorkspace).Err(err).
			Str("root", root).Msg("[workspace] du unavailable, walk fallback")
	}
	var total int64
	_ = filepath.Walk(root, func(pathValue string, info os.FileInfo, ferr error) error {
		if ferr != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolve 解析根下相对路径；越界/穿越报错。
// Python: service.py:51-66 resolve。
func (s *WorkspaceService) resolve(relativePath string) (string, *WorkspaceError) {
	rel, perr := workspaceNormalizeRelativePath(relativePath)
	if perr != "" {
		return "", &WorkspaceError{Code: "BAD_REQUEST", Message: "invalid relative_path"}
	}
	root := filepath.Clean(s.tenantRoot)
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	if rel == "" {
		return root, nil
	}
	joined := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if candidate, err := filepath.EvalSymlinks(joined); err == nil {
		joined = candidate
	}
	if !workspaceCommonPathPrefix(root, joined) {
		return "", &WorkspaceError{Code: "FORBIDDEN", Message: "path escapes tenant root"}
	}
	return joined, nil
}

// deleteOne 删除单个条目（symlink 只删节点、真实路径二次校验）。
// Python: service.py:131-204 _delete_one。
func (s *WorkspaceService) deleteOne(relativePath string) map[string]any {
	rel, perr := workspaceNormalizeRelativePath(relativePath)
	if perr != "" {
		return map[string]any{"relative_path": relativePath, "ok": false, "error": "bad_path"}
	}
	if rel == "" {
		return map[string]any{"relative_path": relativePath, "ok": false, "error": "forbidden_zone"}
	}
	if !workspaceIsDeletable(rel) {
		return map[string]any{"relative_path": rel, "ok": false, "error": "forbidden_zone"}
	}

	root := filepath.Clean(s.tenantRoot)
	lexical := filepath.Join(root, filepath.FromSlash(rel))
	if !workspaceCommonPathPrefix(root, filepath.Clean(lexical)) {
		return map[string]any{"relative_path": rel, "ok": false, "error": "bad_path"}
	}

	isLink := isSymlink(lexical)
	if !isLink {
		// Broken symlinks：exists() 为 False 但 is_symlink() 为 True —— 仍可删。
		if _, err := os.Lstat(lexical); err != nil {
			return map[string]any{"relative_path": rel, "ok": false, "error": "not_found"}
		}
	}

	if isLink {
		// 只删链接节点本身，绝不跟随目标删除（zone 绕过 / 跨租户清空）。
		if err := os.Remove(lexical); err != nil {
			logger.Warn(logComponentWorkspace).Str("path", rel).Err(err).
				Msg("[workspace] delete unlink failed")
			return map[string]any{"relative_path": rel, "ok": false, "error": "io_error"}
		}
		return map[string]any{"relative_path": rel, "ok": true, "error": nil, "freed_bytes": 0}
	}

	// 真实文件/目录：resolve 后对最终路径复核 tenant + deletable zone（纵深防御）。
	resolved, err := filepath.EvalSymlinks(lexical)
	if err != nil {
		return map[string]any{"relative_path": rel, "ok": false, "error": "not_found"}
	}
	if !workspaceCommonPathPrefix(root, resolved) {
		return map[string]any{"relative_path": rel, "ok": false, "error": "bad_path"}
	}
	relToRoot, rerr := filepath.Rel(root, resolved)
	if rerr != nil || !workspaceIsDeletable(filepath.ToSlash(relToRoot)) {
		return map[string]any{"relative_path": rel, "ok": false, "error": "forbidden_zone"}
	}

	var freed int64
	rst, _ := os.Stat(resolved)
	if rst != nil && rst.IsDir() {
		f, werr := s.assertDirDeleteBudget(resolved)
		if werr != nil {
			return map[string]any{"relative_path": rel, "ok": false, "error": werr.Message}
		}
		freed = f
		if err := rmtreeNoFollow(resolved); err != nil {
			logger.Warn(logComponentWorkspace).Str("path", rel).Err(err).Msg("[workspace] delete failed")
			return map[string]any{"relative_path": rel, "ok": false, "error": "io_error"}
		}
	} else {
		if rst != nil {
			freed = rst.Size()
		}
		if err := os.Remove(resolved); err != nil {
			logger.Warn(logComponentWorkspace).Str("path", rel).Err(err).Msg("[workspace] delete failed")
			return map[string]any{"relative_path": rel, "ok": false, "error": "io_error"}
		}
	}
	return map[string]any{"relative_path": rel, "ok": true, "error": nil, "freed_bytes": freed}
}

// assertDirDeleteBudget 校验删除预算，返回目录内文件占用估算（不跟随 symlink）。
// Python: service.py:206-234 _assert_dir_delete_budget。
func (s *WorkspaceService) assertDirDeleteBudget(root string) (int64, *WorkspaceError) {
	depthCap := s.maxDeleteDepth
	if depthCap < 1 {
		depthCap = 1
	}
	entryCap := s.maxDeleteEntries
	if entryCap < 1 {
		entryCap = 1
	}
	count := 0
	var freed int64
	var bizErr *WorkspaceError
	err := walkNoFollow(root, func(_ string, info os.FileInfo, isSymlinkEntry bool, relDepth int) error {
		if bizErr != nil {
			return bizErr
		}
		if relDepth >= depthCap {
			bizErr = &WorkspaceError{Code: "BAD_REQUEST", Message: "delete_too_deep"}
			return bizErr
		}
		count++
		if count > entryCap {
			bizErr = &WorkspaceError{Code: "BAD_REQUEST", Message: "delete_too_many"}
			return bizErr
		}
		if !info.IsDir() && !isSymlinkEntry {
			freed += info.Size()
		}
		return nil
	})
	if bizErr != nil {
		return 0, bizErr
	}
	if err != nil {
		return 0, &WorkspaceError{Code: "FORBIDDEN", Message: "path escapes tenant root"}
	}
	return freed, nil
}

// looksBinary 二进制判定：含 NUL 或不可打印字符比例 ≥15%。
// Python: service.py:394-404 _looks_binary。
func looksBinary(sample []byte) bool {
	if len(sample) == 0 {
		return false
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	textish := 0
	for _, b := range sample {
		if b == 9 || b == 10 || b == 13 || (b >= 32 && b <= 126) || b >= 0x80 {
			textish++
		}
	}
	return float64(textish)/float64(len(sample)) < 0.85
}

// isSymlink 判断路径是否为符号链接（lstat 判定，不跟随）。
func isSymlink(pathValue string) bool {
	info, err := os.Lstat(pathValue)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// walkNoFollow 不跟随 symlink 的目录遍历（回调带相对深度与 symlink 标记）。
// Python: os.walk(followlinks=False)。
func walkNoFollow(root string, fn func(pathValue string, info os.FileInfo, isSymlinkEntry bool, relDepth int) error) error {
	return walkNoFollowDir(root, root, 0, fn)
}

// walkNoFollowDir 递归实现。
func walkNoFollowDir(root, dir string, depth int, fn func(pathValue string, info os.FileInfo, isSymlinkEntry bool, relDepth int) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		info, ierr := entry.Info()
		if ierr != nil {
			continue
		}
		symlink := info.Mode()&os.ModeSymlink != 0
		if err := fn(full, info, symlink, depth); err != nil {
			return err
		}
		if entry.IsDir() && !symlink {
			if err := walkNoFollowDir(root, full, depth+1, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// fileCTimeMs 从 Stat_t 取 inode 变更时间（对齐 Python st_ctime；
// 平台无 Stat_t.Ctim 时回退 mtime）。
func fileCTimeMs(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Sec*1000 + st.Ctim.Nsec/1e6
	}
	return fi.ModTime().UnixMilli()
}

// rmtreeNoFollow 删除目录树（文件/目录 symlink 都不跟随）。
// Python: service.py:236-253 _rmtree_nofollow（后序删除：先子后父）。
func rmtreeNoFollow(root string) error {
	var paths []string
	err := walkNoFollowDir(root, root, 0, func(pathValue string, info os.FileInfo, isSymlinkEntry bool, _ int) error {
		paths = append(paths, pathValue)
		return nil
	})
	if err != nil {
		return err
	}
	// 按路径长度降序删除（子路径恒长于父路径，后序等价）
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if len(paths[j]) > len(paths[i]) {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return os.Remove(root)
}

// minInt 整数最小值。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
