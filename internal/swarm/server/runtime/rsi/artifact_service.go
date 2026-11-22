package rsi

// ArtifactService：产物/快照服务。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/artifact_service.py（139 行）。
// 快照落在 <task_dir>/snapshots/A<node_id>.zip。

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// snapshotSubdir 快照子目录。
const snapshotSubdir = "snapshots"

// ArtifactService 产物快照服务（与 TaskStore 同根）。
type ArtifactService struct {
	tasksRoot string
}

// NewArtifactService 构造。
func NewArtifactService(tasksRoot string) *ArtifactService {
	return &ArtifactService{tasksRoot: tasksRoot}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeSnapshot 把引擎本地产物路径打包为 zip 快照。
// 仅 ADOPTED 节点触发（调用方保证）；nodeRef 为引擎侧引用，nodeID 为服务侧
// 稳定 ID；返回 artifact_id（A<node_id>），无产物文件返回 ""。
func (s *ArtifactService) MakeSnapshot(taskID, nodeRef, nodeID string, artifacts []RsiArtifactPath) string {
	usable := s.resolveArtifactPaths(taskID, artifacts)
	if len(usable) == 0 {
		return ""
	}
	taskDir := filepath.Join(s.tasksRoot, taskID)
	snapshotsDir := filepath.Join(taskDir, snapshotSubdir)
	if err := os.MkdirAll(snapshotsDir, 0o755); err != nil {
		return ""
	}
	artifactID := "A" + nodeID
	zipPath := filepath.Join(snapshotsDir, artifactID+".zip")
	if fileExists(zipPath) {
		return artifactID
	}
	if err := writeSnapshotZip(zipPath, usable); err != nil {
		return ""
	}
	return artifactID
}

// Locate rsi.artifact.download 定位（内部 v3 §4.5 / web §8.3）。
func (s *ArtifactService) Locate(taskID string, artifactID *string) (RsiArtifactFile, error) {
	taskDir := filepath.Join(s.tasksRoot, taskID)
	if !dirExists(taskDir) {
		return RsiArtifactFile{}, NewTaskNotFound(taskID)
	}
	snapshotsDir := filepath.Join(taskDir, snapshotSubdir)
	if artifactID != nil && *artifactID != "" {
		zipPath := filepath.Join(snapshotsDir, *artifactID+".zip")
		if !fileExists(zipPath) {
			return RsiArtifactFile{}, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("artifact 不存在: %s", *artifactID)}
		}
		return RsiArtifactFile{Path: zipPath, Kind: string(ArtifactKindHarnessPlugin), IsBest: false}, nil
	}
	best := s.latestSnapshot(taskID, true)
	if best == "" {
		return RsiArtifactFile{}, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("任务 %s 尚无产物快照", taskID)}
	}
	return RsiArtifactFile{Path: best, Kind: string(ArtifactKindHarnessPlugin), IsBest: true}, nil
}

// BestArtifact task.get / report.get 的 best_artifact 对象。
func (s *ArtifactService) BestArtifact(taskID string) map[string]any {
	best := s.latestSnapshot(taskID, false)
	if best == "" {
		return nil
	}
	return map[string]any{
		"artifact_id": strings.TrimSuffix(filepath.Base(best), ".zip"),
		"name":        filepath.Base(best),
		"adopted":     true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveArtifactPaths 产物路径解析：绝对路径直接用；相对路径挂 task run_dir 下。
func (s *ArtifactService) resolveArtifactPaths(taskID string, artifacts []RsiArtifactPath) [][2]string {
	taskDir := filepath.Join(s.tasksRoot, taskID)
	usable := [][2]string{}
	for _, art := range artifacts {
		if art.Path == "" {
			continue
		}
		p := expandUser(art.Path)
		if !filepath.IsAbs(p) {
			p = filepath.Join(taskDir, p)
		}
		if fileExists(p) || (art.Format == "dir" && dirExists(p)) {
			usable = append(usable, [2]string{art.Role, p})
		}
	}
	return usable
}

// latestSnapshot 最新 adopted 快照（includeRoot=false 时排除 AROOT）。
func (s *ArtifactService) latestSnapshot(taskID string, includeRoot bool) string {
	snapshotsDir := filepath.Join(s.tasksRoot, taskID, snapshotSubdir)
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return ""
	}
	var candidates []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "A") || !strings.HasSuffix(name, ".zip") {
			continue
		}
		if !includeRoot && strings.TrimSuffix(name, ".zip") == "AROOT" {
			continue
		}
		candidates = append(candidates, filepath.Join(snapshotsDir, name))
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		return fileMtime(candidates[i]).Before(fileMtime(candidates[j]))
	})
	return candidates[len(candidates)-1]
}

// writeSnapshotZip 打包快照（多产物 arcname 前缀角色；目录递归；防符号链接逃逸）。
func writeSnapshotZip(zipPath string, usable [][2]string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, item := range usable {
		role, src := item[0], item[1]
		arcname := filepath.Base(src)
		if len(usable) > 1 {
			arcname = role + "_" + arcname
		}
		if err := addPathToZip(zw, src, arcname); err != nil {
			zw.Close()
			return err
		}
	}
	return zw.Close()
}

// addPathToZip 单路径递归写入。
func addPathToZip(zw *zip.Writer, src, arcname string) error {
	if dirExists(src) {
		var files []string
		_ = filepath.Walk(src, func(item string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				files = append(files, item)
			}
			return nil
		})
		sort.Strings(files)
		for _, source := range files {
			rel, err := filepath.Rel(src, source)
			if err != nil {
				continue
			}
			if err := addFileToZip(zw, source, arcname+"/"+filepath.ToSlash(rel)); err != nil {
				return err
			}
		}
		return nil
	}
	return addFileToZip(zw, src, arcname)
}

// addFileToZip 单文件写入。
func addFileToZip(zw *zip.Writer, source, name string) error {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		// 插件链接不得把宿主无关文件拉进下载。
		return nil
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// fileMtime 修改时间助手。
func fileMtime(path string) timeTime { return statMtime(path) }

// timeTime 时间别名。
type timeTime = time.Time

// statMtime 文件修改时间。
func statMtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
