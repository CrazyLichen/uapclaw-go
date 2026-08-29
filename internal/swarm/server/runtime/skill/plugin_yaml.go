package skill

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pluginYAMLPayload plugin.yaml 内容结构（对齐 Python: plugin_yaml_payload）
type pluginYAMLPayload struct {
	// Name 技能名称
	Name string `yaml:"name"`
	// Version 版本号
	Version string `yaml:"version"`
	// DisplayName 显示名称
	DisplayName string `yaml:"display_name"`
	// Description 描述
	Description string `yaml:"description"`
	// Runtime 运行时配置
	Runtime pluginRuntime `yaml:"runtime"`
	// Metadata 元数据
	Metadata pluginMetadata `yaml:"metadata"`
}

// pluginRuntime 运行时配置
type pluginRuntime struct {
	// Type 类型
	Type string `yaml:"type"`
}

// pluginMetadata 元数据
type pluginMetadata struct {
	// Author 作者
	Author string `yaml:"author"`
	// Tags 标签列表
	Tags []string `yaml:"tags"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTeamskillsPublishZip 构建 TeamSkills 发布规范化 ZIP（对齐 Python: _build_teamskills_publish_zip_from_root）。
// 步骤：
//  1. 定位 SKILL.md
//  2. 解析 meta 提取 name/description/display_name/author/tags
//  3. 生成 plugin.yaml
//  4. 创建规范化 ZIP（{skill_name}/plugin.yaml + {skill_name}/{skill_name}/*）
//  5. 计算 SHA256
func buildTeamskillsPublishZip(skillDir string, pluginVersion string) ([]byte, string, error) {
	// 定位 SKILL.md
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMdPath); err != nil {
		// 尝试子目录
		entries, _ := os.ReadDir(skillDir)
		for _, e := range entries {
			if e.IsDir() {
				candidate := filepath.Join(skillDir, e.Name(), "SKILL.md")
				if _, err2 := os.Stat(candidate); err2 == nil {
					skillDir = filepath.Join(skillDir, e.Name())
					skillMdPath = candidate
					break
				}
			}
		}
	}

	meta, err := parseSKILLMd(skillMdPath)
	if err != nil || meta == nil {
		return nil, "", fmt.Errorf("SKILL.md 解析失败: %s", skillMdPath)
	}

	skillName := strings.TrimSpace(toString(meta["name"]))
	if skillName == "" {
		return nil, "", fmt.Errorf("SKILL.md frontmatter 缺少 name")
	}
	description := strings.TrimSpace(toString(meta["description"]))
	if description == "" {
		description = skillName
	}
	displayName := strings.TrimSpace(toString(meta["display_name"]))
	if displayName == "" {
		displayName = skillName
	}
	author := strings.TrimSpace(toString(meta["author"]))
	if author == "" {
		author = "unknown"
	}

	// 标签规范化（对齐 Python: tags 默认 ["teamskills"]）
	var normalizedTags []string
	if tags, ok := meta["tags"].([]any); ok {
		for _, t := range tags {
			if s := strings.TrimSpace(toString(t)); s != "" {
				normalizedTags = append(normalizedTags, s)
			}
		}
	} else if tagStr := strings.TrimSpace(toString(meta["tags"])); tagStr != "" {
		normalizedTags = append(normalizedTags, tagStr)
	}
	if len(normalizedTags) == 0 {
		normalizedTags = []string{"teamskills"}
	}

	// 生成 plugin.yaml
	payload := pluginYAMLPayload{
		Name:        skillName,
		Version:     pluginVersion,
		DisplayName: displayName,
		Description: description,
		Runtime:     pluginRuntime{Type: "skill"},
		Metadata:    pluginMetadata{Author: author, Tags: normalizedTags},
	}
	pluginYAML, err := yaml.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("生成 plugin.yaml 失败: %w", err)
	}

	// 创建规范化 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// 写入 plugin.yaml
	w, err := zipWriter.Create(fmt.Sprintf("%s/plugin.yaml", skillName))
	if err != nil {
		return nil, "", fmt.Errorf("创建 ZIP 条目失败: %w", err)
	}
	if _, err := w.Write(pluginYAML); err != nil {
		return nil, "", fmt.Errorf("写入 plugin.yaml 失败: %w", err)
	}

	// 写入 README.md（如果存在）
	readmePath := filepath.Join(filepath.Dir(skillDir), "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		w, err := zipWriter.Create(fmt.Sprintf("%s/README.md", skillName))
		if err == nil {
			_, _ = w.Write(data)
		}
	}

	// 遍历 skillDir 下所有文件，写入 {skill_name}/{skill_name}/* 结构
	if err := filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		arcName := fmt.Sprintf("%s/%s/%s", skillName, skillName, filepath.ToSlash(rel))
		w, err := zipWriter.Create(arcName)
		if err != nil {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		_, _ = io.Copy(w, f)
		return nil
	}); err != nil {
		return nil, "", fmt.Errorf("遍历 skillDir 失败: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("关闭 ZIP 失败: %w", err)
	}

	// 计算 SHA256
	hash := sha256.Sum256(buf.Bytes())
	checksum := hex.EncodeToString(hash[:])

	return buf.Bytes(), checksum, nil
}

// buildTeamskillsPublishZipFromPath 从 path 或 file 参数构建发布 ZIP。
// 对齐 Python: _prepare_teamskills_publish_zip
func buildTeamskillsPublishZipFromPath(pathRaw, fileRaw, pluginVersion string) ([]byte, string, error) {
	if fileRaw != "" {
		// 从现有 ZIP 解压再规范化
		srcZip, err := filepath.Abs(fileRaw)
		if err != nil {
			return nil, "", fmt.Errorf("ZIP 路径无效: %s", fileRaw)
		}
		if !fileExists(srcZip) {
			return nil, "", fmt.Errorf("ZIP 文件不存在: %s", srcZip)
		}
		if !strings.HasSuffix(strings.ToLower(srcZip), ".zip") {
			return nil, "", fmt.Errorf("file 必须是 .zip 文件")
		}

		// 解压到临时目录
		tmpDir, err := os.MkdirTemp("", "zip_stage_*")
		if err != nil {
			return nil, "", fmt.Errorf("创建临时目录失败: %w", err)
		}
		defer func() { _ = safeRmtree(tmpDir) }()

		if err := extractZipFile(srcZip, tmpDir); err != nil {
			return nil, "", fmt.Errorf("解压 ZIP 失败: %w", err)
		}
		return buildTeamskillsPublishZip(tmpDir, pluginVersion)
	}

	// 从目录构建
	root, err := filepath.Abs(pathRaw)
	if err != nil {
		return nil, "", fmt.Errorf("路径无效: %s", pathRaw)
	}
	if !dirExists(root) {
		return nil, "", fmt.Errorf("目录不存在: %s", root)
	}
	return buildTeamskillsPublishZip(root, pluginVersion)
}

// extractZipFile 解压 ZIP 文件到指定目录
func extractZipFile(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 ZIP 失败: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)

		// 安全检查：防止 Zip Slip
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("ZIP 条目路径不安全: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("创建父目录失败: %w", err)
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		_ = os.WriteFile(path, data, 0o644)
	}
	return nil
}

// pluginYAMLToMap 将 pluginYAMLPayload 转为 map[string]any（用于 JSON 兼容输出）
func pluginYAMLToMap(p *pluginYAMLPayload) map[string]any {
	tags := make([]any, len(p.Metadata.Tags))
	for i, t := range p.Metadata.Tags {
		tags[i] = t
	}
	return map[string]any{
		"name":         p.Name,
		"version":      p.Version,
		"display_name": p.DisplayName,
		"description":  p.Description,
		"runtime":      map[string]any{"type": p.Runtime.Type},
		"metadata":     map[string]any{"author": p.Metadata.Author, "tags": tags},
	}
}

// marshalPluginYAML 将 pluginYAMLPayload 序列化为 YAML 字节
func marshalPluginYAML(p *pluginYAMLPayload) ([]byte, error) {
	// 先转为 JSON 再转为 map 以确保 yaml.v3 兼容
	jsonData, err := json.Marshal(pluginYAMLToMap(p))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(jsonData, &m); err != nil {
		return nil, err
	}
	return yaml.Marshal(m)
}
