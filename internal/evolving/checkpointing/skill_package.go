package checkpointing

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 排除规则常量。
// 对应 Python: _EXCLUDE_DIR_NAMES / _EXCLUDE_FILE_NAMES
// 打包技能是为了"分享"，分享时只携带技能本身，
// 不携带演进历史和本地治理数据。
var excludeDirNames = map[string]bool{
	"evolution":   true,
	"archive":     true,
	"__pycache__": true,
	".git":        true,
}

var excludeFileNames = map[string]bool{
	"evolutions.json": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillID 生成全局唯一技能标识。
//
// 对应 Python: new_skill_id() → "sk_{uuid12}"
func NewSkillID() string {
	return fmt.Sprintf("sk_%012x", time.Now().UnixNano()&0xFFFFFFFFFFFF)
}

// ReadSkillIDFromContent 从 SKILL.md frontmatter 读取 skill_id。
//
// 对应 Python: read_skill_id_from_content(content)
func ReadSkillIDFromContent(content string) string {
	frontmatter := parseTopLevelFrontmatter(content)
	if v, ok := frontmatter["skill_id"]; ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// EnsureSkillIDInContent 确保 SKILL.md frontmatter 包含 skill_id。
//
// 返回 (updatedContent, skillID)。
// 对应 Python: ensure_skill_id_in_content(content)
func EnsureSkillIDInContent(content string) (string, string) {
	existing := ReadSkillIDFromContent(content)
	if existing != "" {
		return content, existing
	}

	skillID := NewSkillID()
	// 对齐 Python: stripped = content.lstrip("\ufeff")
	stripped := strings.TrimLeft(content, "\ufeff")
	if strings.HasPrefix(stripped, "---") {
		closing := strings.Index(stripped[3:], "---")
		if closing != -1 {
			head := stripped[:3+closing]
			tail := stripped[3+closing:]
			if !strings.HasSuffix(head, "\n") {
				head += "\n"
			}
			head += fmt.Sprintf("skill_id: %s\n", skillID)
			updated := head + tail
			if updated != content {
				return updated, skillID
			}
			return content, skillID
		}
	}

	// 对齐 Python: 没有 frontmatter 时插入新 frontmatter
	updated := fmt.Sprintf("---\nskill_id: %s\n---\n\n%s", skillID, strings.TrimLeft(content, " \t\n\r"))
	return strings.TrimSpace(updated) + "\n", skillID
}

// PackSkillDirectory 将技能目录打包为 tar.gz。
//
// 排除演进本地产物（evolution/archive/.git 等）。
// 当 skillMDRelpath 和 skillMDContent 提供时，
// tarball 使用提供的 SKILL.md 内容而非磁盘文件。
// 对应 Python: pack_skill_directory(skill_dir, skill_md_relpath, skill_md_content)
func PackSkillDirectory(skillDir string, skillMDRelpath string, skillMDContent string) ([]byte, error) {
	root, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, fmt.Errorf("解析技能目录路径失败: %w", err)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := listPackableFilePaths(root)

	overrideArcname := ""
	if skillMDRelpath != "" {
		overrideArcname = filepath.ToSlash(skillMDRelpath)
	}

	for _, absPath := range files {
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			continue
		}
		arcname := filepath.ToSlash(rel)

		// 对齐 Python: override_arcname 和 skill_md_content 替换
		if overrideArcname != "" && skillMDContent != "" && arcname == overrideArcname {
			payload := []byte(skillMDContent)
			info := &tar.Header{
				Name: arcname,
				Size: int64(len(payload)),
				Mode: 0644,
			}
			if err := tw.WriteHeader(info); err != nil {
				return nil, fmt.Errorf("写入 tar header 失败: %w", err)
			}
			if _, err := tw.Write(payload); err != nil {
				return nil, fmt.Errorf("写入 tar 内容失败: %w", err)
			}
			continue
		}

		f, err := os.Open(absPath)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			f.Close()
			continue
		}
		header.Name = arcname
		if err := tw.WriteHeader(header); err != nil {
			f.Close()
			return nil, fmt.Errorf("写入 tar header 失败: %w", err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return nil, fmt.Errorf("写入 tar 内容失败: %w", err)
		}
		f.Close()
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnpackSkillPackage 将技能包 tar.gz 解压到目标目录。
//
// 使用安全路径检查（防路径遍历），对应 Python tarfile.data_filter。
// 对应 Python: unpack_skill_package(package_bytes, dest_dir)
func UnpackSkillPackage(packageBytes []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	buf := bytes.NewReader(packageBytes)
	gzReader, err := gzip.NewReader(buf)
	if err != nil {
		return fmt.Errorf("打开 gzip 失败: %w", err)
	}
	defer gzReader.Close()

	tr := tar.NewReader(gzReader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 条目失败: %w", err)
		}

		// 安全路径检查（防路径遍历）
		targetPath := filepath.Join(destDir, filepath.FromSlash(header.Name))
		if !isSafePath(destDir, targetPath) {
			return fmt.Errorf("不安全的路径: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("创建父目录失败: %w", err)
			}
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("创建文件失败: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("写入文件失败: %w", err)
			}
			f.Close()
		}
	}
	return nil
}

// ListPackableFiles 列出技能目录中可打包的文件路径。
//
// 对应 Python: list_packable_files(skill_dir)
func ListPackableFiles(skillDir string) ([]string, error) {
	root, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, fmt.Errorf("解析技能目录路径失败: %w", err)
	}
	return listPackableFilePaths(root), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// shouldPackRelative 判断相对路径是否应该打包。
// 对应 Python: _should_pack_relative(relative)
func shouldPackRelative(relPath string) bool {
	if relPath == "" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	// 对齐 Python: if parts[0] in _EXCLUDE_DIR_NAMES
	if excludeDirNames[parts[0]] {
		return false
	}
	// 对齐 Python: if relative.name in _EXCLUDE_FILE_NAMES
	filename := filepath.Base(relPath)
	if excludeFileNames[filename] {
		return false
	}
	// 对齐 Python: if relative.name.startswith(".")
	if strings.HasPrefix(filename, ".") {
		return false
	}
	return true
}

// listPackableFilePaths 列出可打包文件的绝对路径。
func listPackableFilePaths(root string) []string {
	var result []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if rel != "" && !shouldPackRelative(rel) {
			return filepath.SkipDir
		}
		return nil
	})
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if shouldPackRelative(rel) {
			result = append(result, path)
		}
		return nil
	})
	sort.Strings(result)
	return result
}

// isSafePath 检查目标路径是否在指定目录内（防路径遍历）。
func isSafePath(baseDir, targetPath string) bool {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) || absTarget == absBase
}

// parseTopLevelFrontmatter 解析 Markdown frontmatter 中的顶层标量字段。
// 对应 Python: parse_top_level_frontmatter(content)
func parseTopLevelFrontmatter(content string) map[string]string {
	text := strings.TrimSpace(content)
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}
	}
	end := strings.Index(text[3:], "---")
	if end == -1 {
		return map[string]string{}
	}
	fmText := strings.TrimSpace(text[3 : 3+end])
	result := map[string]string{}
	keyRe := regexp.MustCompile(`^([a-zA-Z_-]+):\s*(.*)`)
	for _, line := range strings.Split(fmText, "\n") {
		m := keyRe.FindStringSubmatch(line)
		if m != nil {
			result[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return result
}
