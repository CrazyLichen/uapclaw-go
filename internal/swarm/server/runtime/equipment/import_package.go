package equipment

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadTemplateSpec 加载模板 spec（团队兼容性校验用）。
// Python: load_agent_template_package (openjiuwen.harness.resources)。
func loadTemplateSpec(pkgDir string) (hschema.AgentTemplateSpec, error) {
	return hresources.LoadAgentTemplatePackage(filepath.Join(pkgDir, "manifest.json"))
}

// installAgentGroup 校验并安装分组定义（拷入 built_in + installed=true）。
// Python: install_agent_group (:4482-4498) + _install_package (:4411-4460)。
func installAgentGroup(params map[string]any) error {
	packageID, err := rejectPackageNameAny(params["id"], "agent_group")
	if err != nil {
		return err
	}
	// allowlist 门卫：个人版恒放行（D-4）
	resolved, _, ok := resolveShowPackageDir(packageID, "agent_group")
	if !ok {
		return &PackageConflictError{Message: "agent_group package not found: " + packageID}
	}
	if err := assertNoSymlinks(resolved); err != nil {
		return err
	}
	if _, err := loadGroupBundleStrict(resolved); err != nil {
		return err
	}
	return installPackage(packageID, installSpec{
		kind: agentGroupKind, kindLabel: "agent_group",
		packageType: "agent_group", isPlugin: false})
}

// installSpec 安装规格。
type installSpec struct {
	kind        string
	kindLabel   string
	packageType string
	isPlugin    bool
}

// installPackage 安装落位：built_in/local 冲突校验 + resources 拷入 built_in + installed=true。
// Python: _install_package (:4411-4460)。
func installPackage(packageID string, spec installSpec) error {
	builtInRoot := equipmentBuiltInRoot(spec.kind, "")
	localRoot := equipmentLocalRoot(spec.kind, "")
	builtInDir := filepath.Join(builtInRoot, packageID)
	localDir := filepath.Join(localRoot, packageID)
	resourcesRoot := resourcesKindRoot(spec.kind)
	resourceDir := ""
	if resourcesRoot != "" {
		resourceDir = filepath.Join(resourcesRoot, packageID)
	}
	if isDir(builtInDir) && isDir(localDir) {
		return &PackageConflictError{Message: spec.kindLabel + " package conflict: " +
			packageID + " exists in both local and built_in"}
	}
	createdBuiltIn := false
	var source string
	if isDir(builtInDir) {
		source = "builtin"
	} else if isDir(localDir) {
		source = "local"
	} else {
		if resourceDir == "" || !isDir(resourceDir) {
			return &PackageConflictError{Message: spec.kindLabel + " package not found: " + packageID}
		}
		if _, vmErr := validatePackageManifest(resourceDir, spec.kindLabel, spec.packageType); vmErr != nil {
			return vmErr
		}
		if err := os.MkdirAll(builtInRoot, 0o755); err != nil {
			return err
		}
		if err := copyTreeHelper(resourceDir, builtInDir); err != nil {
			_ = os.RemoveAll(builtInDir)
			return err
		}
		source = "builtin"
		createdBuiltIn = true
	}
	var upsertErr error
	switch {
	case spec.kind == agentGroupKind:
		upsertErr = UpsertAgentGroupMarketplaceEntry(packageID, true, source)
	case spec.isPlugin:
		upsertErr = upsertMarketplaceEntry(spec.kind, packageID,
			map[string]any{"installed": true, "source": source})
	default:
		upsertErr = UpsertAgentTemplateMarketplaceEntry(packageID, true, source)
	}
	if upsertErr != nil {
		if createdBuiltIn {
			_ = os.RemoveAll(builtInDir)
		}
		return upsertErr
	}
	return nil
}

// loadGroupBundleStrict 分组包严格校验加载。
// Python: load_agent_group_package（create_agent_group :4034 调用形态）。
func loadGroupBundleStrict(pkgDir string) (*experts.LoadedAgentGroupPackage, error) {
	return loadGroupBundle(pkgDir)
}

// copyTree 递归拷贝目录（生产实现；测试助手同名迁移至此）。
func copyTreeHelper(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if isSymlink(p) {
			return &PackageConflictError{Message: "symbolic links are not allowed in package: " + filepath.Base(src)}
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// ──────────────────────────── 导入链 ────────────────────────────

// ImportAgentTemplate params['path'] → {'id': package_id}。
// Python: import_agent_template (:4379-4387)。
func ImportAgentTemplate(params map[string]any) (map[string]any, error) {
	return importPackageFromPath(params, importSpec{
		kind: agentTemplateKind, kindLabel: "agent_template", packageType: "agent_template"})
}

// ImportAgentGroup 导入并安装一个分组定义。
// Python: import_agent_group (:4389-4398)——import 后立即 install_agent_group。
// 本地半区语义：D-1 下 install_agent_group 的 Hub 安装态写段随本地安装一致
// （_install_package 本地段：拷入 built_in + upsert installed=true）。
func ImportAgentGroup(params map[string]any) (map[string]any, error) {
	result, err := importPackageFromPath(params, importSpec{
		kind: agentGroupKind, kindLabel: "agent_group", packageType: "agent_group"})
	if err != nil {
		return nil, err
	}
	if err := installAgentGroup(params); err != nil {
		return nil, err
	}
	return result, nil
}

// importSpec 导入规格。
type importSpec struct {
	// kind 设备目录名
	kind string
	// kindLabel 错误文案标签
	kindLabel string
	// packageType manifest.package_type
	packageType string
}

// importPackageFromPath zip/tar/目录包导入 local/{id}/。
// Python: _import_package_from_path (:4345-4377)。
func importPackageFromPath(params map[string]any, spec importSpec) (map[string]any, error) {
	src, err := resolveImportSourcePath(params)
	if err != nil {
		return nil, err
	}
	if isDir(src) {
		pkgRoot, rootErr := findPackageRoot(src, spec.kindLabel)
		if rootErr != nil {
			return nil, rootErr
		}
		return commitImportedPackage(pkgRoot, spec)
	}
	if info, statErr := os.Stat(src); statErr == nil && !info.IsDir() {
		tmp, tmpErr := os.MkdirTemp("", "uapclaw_pkg_import_")
		if tmpErr != nil {
			return nil, tmpErr
		}
		defer os.RemoveAll(tmp)
		extractDir := filepath.Join(tmp, "contents")
		if extractErr := extractArchive(src, extractDir); extractErr != nil {
			return nil, extractErr
		}
		pkgRoot, rootErr := findPackageRoot(extractDir, spec.kindLabel)
		if rootErr != nil {
			return nil, rootErr
		}
		// Python :4363-4373：分组 zip 根目录名与 manifest.name 不符时改名
		if spec.kind == agentGroupKind && filepath.Base(pkgRoot) == "contents" {
			manifest, _ := readPackageManifest(pkgRoot)
			if manifest == nil {
				return nil, &PackageConflictError{
					Message: spec.kindLabel + " package missing/corrupt manifest.json"}
			}
			packageID, idErr := packageIDFromManifest(manifest, spec.packageType, spec.kindLabel)
			if idErr != nil {
				return nil, idErr
			}
			if filepath.Base(pkgRoot) != packageID {
				canonicalRoot := filepath.Join(tmp, packageID)
				if err := os.Rename(pkgRoot, canonicalRoot); err != nil {
					return nil, err
				}
				pkgRoot = canonicalRoot
			}
		}
		return commitImportedPackage(pkgRoot, spec)
	}
	return nil, &PackageConflictError{Message: "不支持的路径类型: " + src}
}

// resolveImportSourcePath 校验 params.path：本地绝对路径、无 URL、无符号链接。
// Python: _resolve_import_source_path (:4152-4171)。
func resolveImportSourcePath(params map[string]any) (string, error) {
	raw, _ := params["path"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &PackageConflictError{Message: "缺少参数: path"}
	}
	if strings.Contains(strings.ToLower(raw), "://") {
		return "", &PackageConflictError{Message: "仅支持本地文件路径，不支持 URL 协议"}
	}
	if strings.Contains(raw, "\x00") {
		return "", &PackageConflictError{Message: "path 包含非法字符"}
	}
	expanded := raw
	if strings.HasPrefix(expanded, "~") {
		home, _ := os.UserHomeDir()
		expanded = filepath.Join(home, expanded[1:])
	}
	if !filepath.IsAbs(expanded) {
		return "", &PackageConflictError{Message: "path 仅支持绝对路径"}
	}
	if _, err := os.Stat(expanded); err != nil {
		return "", &PackageConflictError{Message: "路径不存在: " + raw}
	}
	if isSymlink(expanded) {
		return "", &PackageConflictError{Message: "path 不支持符号链接: " + raw}
	}
	return expanded, nil
}

// findPackageRoot 包根：本目录，或唯一含 manifest.json 的子目录。
// Python: _find_package_root (:4173-4185)。
func findPackageRoot(base, kindLabel string) (string, error) {
	if _, err := os.Stat(filepath.Join(base, "manifest.json")); err == nil {
		return base, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", &PackageConflictError{Message: kindLabel + " package missing/corrupt manifest.json"}
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(base, entry.Name(), "manifest.json")); err == nil {
				candidates = append(candidates, filepath.Join(base, entry.Name()))
			}
		}
	}
	if len(candidates) != 1 {
		return "", &PackageConflictError{Message: kindLabel + " package missing/corrupt manifest.json"}
	}
	return candidates[0], nil
}

// packageIDFromManifest 读包标识并拒绝不安全目录名。
// Python: _package_id_from_manifest (:4187-4198)。
func packageIDFromManifest(manifest map[string]any, packageType, kindLabel string) (string, error) {
	key := "name"
	if packageType == "plugin" {
		key = "id"
	}
	raw, _ := manifest[key].(string)
	if strings.TrimSpace(raw) == "" {
		return "", &PackageConflictError{Message: kindLabel + " package missing id"}
	}
	return rejectPackageNameAny(strings.TrimSpace(raw), kindLabel)
}

// extractArchive zip/tar/tar.gz 解包（成员名/链接/限额检查）。
// Python: _extract_archive (:4109-4150)。
func extractArchive(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	name := strings.ToLower(filepath.Base(src))
	var extractErr error
	switch {
	case strings.HasSuffix(name, ".zip"):
		extractErr = extractZip(src, dest)
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar"):
		extractErr = extractTar(src, dest)
	default:
		return &PackageConflictError{Message: "unsupported archive format"}
	}
	if extractErr != nil {
		logger.Warn(logger.ComponentAgentServer).Err(extractErr).
			Str("archive", src).Msg("[equipment] failed to extract archive")
		return &PackageConflictError{Message: "failed to extract archive: " + filepath.Base(src)}
	}
	return nil
}

// rejectArchiveMemberName 拒绝绝对路径/穿越成员名。
// Python: _reject_archive_member_name。
func rejectArchiveMemberName(memberName string) error {
	clean := filepath.ToSlash(memberName)
	if strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") {
		return &PackageConflictError{Message: "archive member path escapes: " + memberName}
	}
	return nil
}

// extractZip zip 解包。Python: _extract_archive zip 分支。
func extractZip(src, dest string) error {
	reader, openErr := zip.OpenReader(src)
	if openErr != nil {
		return openErr
	}
	defer reader.Close()
	totalBytes := int64(0)
	fileCount := 0
	for _, info := range reader.File {
		if err := rejectArchiveMemberName(info.Name); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &PackageConflictError{Message: "archive symbolic links are not allowed"}
		}
		if info.FileInfo().IsDir() {
			continue
		}
		fileCount++
		totalBytes += int64(info.UncompressedSize64)
		if err := checkArchiveLimits(fileCount, totalBytes); err != nil {
			return err
		}
	}
	for _, file := range reader.File {
		target := filepath.Join(dest, filepath.FromSlash(file.Name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		out, createErr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if createErr != nil {
			rc.Close()
			return createErr
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		if copyErr != nil {
			out.Close()
			return copyErr
		}
		out.Close()
	}
	return nil
}

// extractTar tar/tar.gz 解包。Python: _extract_archive tar 分支。
func extractTar(src, dest string) error {
	f, openErr := os.Open(src)
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	var tr *tar.Reader
	if strings.HasSuffix(strings.ToLower(src), ".tar.gz") {
		gz, gzErr := gzip.NewReader(f)
		if gzErr != nil {
			return gzErr
		}
		defer gz.Close()
		tr = tar.NewReader(gz)
	} else {
		tr = tar.NewReader(f)
	}
	totalBytes := int64(0)
	fileCount := 0
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if err := rejectArchiveMemberName(header.Name); err != nil {
			return err
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return &PackageConflictError{Message: "archive links are not allowed"}
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return &PackageConflictError{Message: "archive contains unsupported special file"}
		}
		if header.Typeflag == tar.TypeReg {
			fileCount++
			totalBytes += header.Size
		}
		if err := checkArchiveLimits(fileCount, totalBytes); err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(header.Name))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, createErr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if createErr != nil {
			return createErr
		}
		_, copyErr := io.Copy(out, tr)
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// maxImportFileCount / maxImportTotalBytes 解包限额常量。
// Python: _MAX_IMPORT_FILE_COUNT = 10_000 / _MAX_IMPORT_TOTAL_BYTES = 512MiB（:83-84）。
const (
	maxImportFileCount  = 10000
	maxImportTotalBytes = 512 * 1024 * 1024
)

// checkArchiveLimits 拒绝过大的归档。
// Python: _check_archive_limits (:4101-4107)。
func checkArchiveLimits(fileCount int, totalBytes int64) error {
	const maxFiles = maxImportFileCount
	const maxBytes = maxImportTotalBytes
	if fileCount > maxFiles {
		return &PackageConflictError{Message: "archive contains too many files"}
	}
	if totalBytes > maxBytes {
		return &PackageConflictError{Message: "archive too large"}
	}
	return nil
}

// commitImportedPackage 校验 + 拷入 local/{id}/ + upsert installed=false。
// Python: _commit_imported_package (:4266-4343)。
func commitImportedPackage(pkgRoot string, spec importSpec) (map[string]any, error) {
	if _, vmErr := validatePackageManifest(pkgRoot, spec.kindLabel, spec.packageType); vmErr != nil {
		return nil, vmErr
	}
	manifest, _ := readPackageManifest(pkgRoot)
	if manifest == nil {
		return nil, &PackageConflictError{
			Message: spec.kindLabel + " package missing/corrupt manifest.json: " + filepath.Base(pkgRoot)}
	}
	packageID, err := packageIDFromManifest(manifest, spec.packageType, spec.kindLabel)
	if err != nil {
		return nil, err
	}
	if spec.kind == agentTemplateKind || spec.kind == pluginPackagesKind {
		// Python: _require_import_readme
		if _, err := os.Stat(filepath.Join(pkgRoot, "README.md")); err != nil {
			return nil, &PackageConflictError{Message: spec.kindLabel + " package missing README.md"}
		}
	}
	if spec.kind == agentTemplateKind {
		if err := validateAgentTemplateImportLayout(pkgRoot, manifest); err != nil {
			return nil, err
		}
		if err := validateManifestImportSkillPaths(manifest, pkgRoot, "agent_template"); err != nil {
			return nil, err
		}
	}
	if spec.kind == pluginPackagesKind {
		if err := validateManifestImportSkillPaths(manifest, pkgRoot, "plugin"); err != nil {
			return nil, err
		}
	}
	if spec.kind == agentGroupKind {
		// Python :4296-4310：显示名可用性 + name=目录名 + 无符号链接 + skills 路径 + 严格加载
		for _, displayName := range dedupe(i18nValues(manifest["display_name"], packageID)) {
			if err := assertAgentGroupNameAvailable(strings.TrimSpace(displayName)); err != nil {
				return nil, err
			}
		}
		if filepath.Base(pkgRoot) != packageID {
			return nil, &PackageConflictError{Message: "agent_group manifest name must match its package directory"}
		}
		if err := assertNoSymlinks(pkgRoot); err != nil {
			return nil, err
		}
		if err := validateManifestImportSkillPaths(manifest, pkgRoot, "agent_group"); err != nil {
			return nil, err
		}
		if _, err := loadGroupBundleStrict(pkgRoot); err != nil {
			return nil, err
		}
	}
	if err := assertPackageIDAvailable(packageID, spec.kindLabel); err != nil {
		return nil, err
	}
	localRoot := equipmentLocalRoot(spec.kind, "")
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return nil, err
	}
	stagingParent, mkErr := os.MkdirTemp(localRoot, "."+packageID+".staging-")
	if mkErr != nil {
		return nil, mkErr
	}
	defer os.RemoveAll(stagingParent)
	staged := filepath.Join(stagingParent, packageID)
	dest := filepath.Join(localRoot, packageID)
	if err := copyTreeHelper(pkgRoot, staged); err != nil {
		return nil, err
	}
	if err := os.Rename(staged, dest); err != nil {
		return nil, err
	}
	var upsertErr error
	switch spec.kind {
	case pluginPackagesKind:
		upsertErr = upsertMarketplaceEntry(spec.kind, packageID,
			map[string]any{"installed": false, "source": "local"})
	case agentGroupKind:
		upsertErr = UpsertAgentGroupMarketplaceEntry(packageID, false, "local")
	default:
		upsertErr = UpsertAgentTemplateMarketplaceEntry(packageID, false, "local")
	}
	if upsertErr != nil {
		_ = os.RemoveAll(dest)
		if rmErr := removeMarketplaceEntry(spec.kind, packageID); rmErr != nil {
			logger.Warn(logger.ComponentAgentServer).Err(rmErr).
				Str("kind", spec.kind).Str("package", packageID).
				Msg("[extension_package_manager] failed to roll back marketplace entry")
		}
		return nil, upsertErr
	}
	return map[string]any{"id": packageID}, nil
}

// i18nValues display 值全语言集合。
func i18nValues(raw any, fallback string) []string {
	values := i18nOf(raw, fallback)
	return []string{fmt.Sprint(values["zh"]), fmt.Sprint(values["en"])}
}

// validateAgentTemplateImportLayout 导入模板必须有 persona markdown。
// Python: _validate_agent_template_import_layout (:4210-4240)。
func validateAgentTemplateImportLayout(pkgRoot string, manifest map[string]any) error {
	persona, ok := manifest["persona"].(map[string]any)
	if !ok {
		return &PackageConflictError{Message: `agent_template package missing persona (expected {"dir": "./persona"})`}
	}
	rawDir, _ := persona["dir"].(string)
	if strings.TrimSpace(rawDir) == "" {
		return &PackageConflictError{Message: "agent_template package missing persona.dir"}
	}
	rel := strings.TrimSpace(strings.ReplaceAll(rawDir, "\\", "/"))
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return &PackageConflictError{Message: "agent_template package persona dir escapes package root"}
	}
	rootResolved, _ := filepath.Abs(pkgRoot)
	personaDir, _ := filepath.Abs(filepath.Join(pkgRoot, rel))
	if personaDir == rootResolved || !subpathOf(rootResolved, personaDir) {
		return &PackageConflictError{Message: "agent_template package persona dir escapes package root"}
	}
	if !isDir(personaDir) {
		return &PackageConflictError{Message: "agent_template package persona dir not found: " + rawDir}
	}
	hasMD := false
	entries, readErr := os.ReadDir(personaDir)
	if readErr != nil {
		return &PackageConflictError{Message: "agent_template package persona dir not found: " + rawDir}
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".md" {
			hasMD = true
		}
	}
	if !hasMD {
		return &PackageConflictError{Message: "agent_template package persona dir has no markdown files: " + rawDir}
	}
	return nil
}

// validateManifestImportSkillPaths 拒绝宿主专属技能路径。
// Python: _validate_manifest_import_skill_paths (:4242-4264)。
func validateManifestImportSkillPaths(manifest map[string]any, pkgRoot, kindLabel string) error {
	skills, _ := manifest["skills"].([]any)
	if skills == nil {
		return nil
	}
	rootResolved, _ := filepath.Abs(pkgRoot)
	for _, skill := range skills {
		var rawDir string
		if m, ok := skill.(map[string]any); ok {
			rawDir, _ = m["dir"].(string)
		} else if s, ok := skill.(string); ok {
			rawDir = s
		}
		if strings.TrimSpace(rawDir) == "" {
			continue
		}
		if filepath.IsAbs(rawDir) || strings.Contains(rawDir, "..") ||
			strings.Contains(rawDir, "\\") && filepath.VolumeName(rawDir) != "" {
			return &PackageConflictError{Message: kindLabel + " skill dir must be package-relative"}
		}
		rel := strings.TrimSpace(strings.ReplaceAll(rawDir, "\\", "/"))
		if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
			return &PackageConflictError{Message: kindLabel + " skill dir must be package-relative"}
		}
		skillDir, _ := filepath.Abs(filepath.Join(pkgRoot, rel))
		if skillDir == rootResolved || !subpathOf(rootResolved, skillDir) {
			return &PackageConflictError{Message: kindLabel + " skill dir must be package-relative"}
		}
	}
	return nil
}
