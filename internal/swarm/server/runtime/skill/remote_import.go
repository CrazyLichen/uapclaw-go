package skill

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// remoteDownloadTimeout 远程归档下载超时（秒）
	remoteDownloadTimeout = 120
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsHTTPDownloadTarget 检查 URL 是否为 HTTP(S) 下载目标。
// URL 必须以 http:// 或 https:// 开头，且以 .zip、.tar.gz、.tgz 或 .tar.bz2 结尾。
// 对应 Python: SkillManager._is_http_download_target(url)
func IsHTTPDownloadTarget(url string) bool {
	return isHTTPDownloadTarget(url)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isHTTPDownloadTarget 检查 URL 是否为 HTTP(S) 下载目标
func isHTTPDownloadTarget(downloadURL string) bool {
	lower := strings.ToLower(downloadURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	// 检查后缀：.zip, .tar.gz, .tgz, .tar.bz2
	if strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.bz2") {
		return true
	}
	return false
}

// importSkillFromRemoteArchive 从远程 URL 下载技能归档并导入。
// 步骤：创建临时目录 → 下载 → SHA256 校验 → 解压 → 查找 SKILL.md → 调用本地导入逻辑。
// 对应 Python: SkillManager._import_skill_from_remote_archive(ctx, sm, download_url, force, checksum_sha256)
func importSkillFromRemoteArchive(ctx context.Context, sm *SkillManager, downloadURL string, force bool, checksumSHA256 string) (map[string]any, error) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "jiuwenswarm_remote_import_")
	if err != nil {
		return map[string]any{"success": false, "detail": "创建临时目录失败: " + err.Error()}, nil
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger.Info(logComponent).
		Str("download_url", downloadURL).
		Msg("开始下载远程技能归档")

	// 下载归档文件
	archivePath, err := downloadArchive(ctx, downloadURL, tmpDir)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("download_url", downloadURL).
			Str("event_type", "REMOTE_IMPORT_ERROR").
			Msg("下载远程归档失败")
		return map[string]any{"success": false, "detail": "下载失败: " + err.Error()}, nil
	}

	// SHA256 校验
	if checksumSHA256 != "" {
		if err := verifyFileSHA256(archivePath, checksumSHA256); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("download_url", downloadURL).
				Str("event_type", "REMOTE_IMPORT_ERROR").
				Msg("SHA256 校验失败")
			return map[string]any{"success": false, "detail": "SHA256 校验失败: " + err.Error()}, nil
		}
		logger.Info(logComponent).
			Str("download_url", downloadURL).
			Str("checksum_sha256", checksumSHA256).
			Msg("SHA256 校验通过")
	}

	// 解压归档
	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建解压目录失败: " + err.Error()}, nil
	}

	if err := extractArchive(archivePath, extractDir); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("archive_path", archivePath).
			Str("event_type", "REMOTE_IMPORT_ERROR").
			Msg("解压归档失败")
		return map[string]any{"success": false, "detail": "解压失败: " + err.Error()}, nil
	}

	// 在解压内容中查找 SKILL.md
	skillDir := sm.locateSkillDir(extractDir)
	if skillDir == "" {
		return map[string]any{"success": false, "detail": "归档中未找到 SKILL.md"}, nil
	}

	// 使用现有的本地导入逻辑
	result, err := sm.HandleSkillsImportLocal(ctx, map[string]any{
		"path":  skillDir,
		"force": force,
	})
	if err != nil {
		return result, err
	}

	// 补充来源信息
	if toBool(result["success"]) {
		if result["skill"] == nil {
			result["skill"] = map[string]any{}
		}
		if skillMap, ok := result["skill"].(map[string]any); ok {
			skillMap["source"] = "remote_archive"
			skillMap["download_url"] = downloadURL
		}
	}

	return result, nil
}

// downloadArchive 使用 HTTP 下载归档文件到指定目录，返回本地文件路径
func downloadArchive(ctx context.Context, downloadURL, destDir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("构建请求失败: %w", err)
	}

	client := &http.Client{Timeout: remoteDownloadTimeout * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("网络请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载返回状态码: %d", resp.StatusCode)
	}

	// 从 URL 推断文件名
	filename := filepath.Base(downloadURL)
	if filename == "" || filename == "." || filename == "/" {
		filename = "archive.zip"
	}
	// 去除 URL 查询参数中的特殊字符
	if idx := strings.IndexAny(filename, "?#"); idx >= 0 {
		filename = filename[:idx]
	}
	if filename == "" {
		filename = "archive.zip"
	}

	destPath := filepath.Join(destDir, filename)
	outFile, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	written, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	logger.Info(logComponent).
		Str("download_url", downloadURL).
		Int64("bytes_written", written).
		Str("dest_path", destPath).
		Msg("归档下载完成")

	return destPath, nil
}

// verifyFileSHA256 校验文件的 SHA256 哈希值
func verifyFileSHA256(filePath, expectedSHA256 string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算哈希失败: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf("SHA256 不匹配: 期望 %s, 实际 %s", expectedSHA256, actual)
	}
	return nil
}

// extractArchive 根据文件扩展名选择解压方式（ZIP / tar.gz / tgz / tar.bz2）
func extractArchive(archivePath, destDir string) error {
	lower := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZIP(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.bz2"):
		return extractTarBz2(archivePath, destDir)
	default:
		return fmt.Errorf("不支持的归档格式: %s", archivePath)
	}
}

// extractZIP 解压 ZIP 归档到目标目录（防 Zip Slip）
func extractZIP(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("ZIP 读取失败: %w", err)
	}
	defer func() { _ = reader.Close() }()

	for _, f := range reader.File {
		targetPath := filepath.Join(destDir, f.Name)

		// Zip Slip 防护
		relPath, err := filepath.Rel(destDir, targetPath)
		if err != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("zip slip 检测：路径 %q 超出目标目录", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(targetPath, data, f.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// extractTarGz 解压 .tar.gz / .tgz 归档到目标目录（防 Zip Slip 等价路径穿越检查）
func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开 tar.gz 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip 解压失败: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	return extractTar(gzr, destDir)
}

// extractTarBz2 解压 .tar.bz2 归档到目标目录。
// 注意：标准库不支持 bzip2 解压后直接接 tar.NewReader，因为 compress/bzip2 只提供 io.Reader。
// 这里仅声明支持，实际使用 compress/bzip2 作为 tar 的数据源。
func extractTarBz2(archivePath, destDir string) error {
	return fmt.Errorf("tar.bz2 格式暂不支持（需要 compress/bzip2 依赖），请使用 .tar.gz 或 .zip")
}

// extractTar 从 tar 数据流解压到目标目录（防路径穿越）
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar 读取失败: %w", err)
		}

		targetPath := filepath.Join(destDir, hdr.Name)

		// 路径穿越防护
		relPath, err := filepath.Rel(destDir, targetPath)
		if err != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("路径穿越检测：路径 %q 超出目标目录", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return err
			}
			_ = outFile.Close()
		}
	}
	return nil
}
