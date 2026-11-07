package skill

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// buildZipArchive 在 destPath 生成一个 ZIP 归档，files 为「相对路径 → 内容」。
func buildZipArchive(t *testing.T, destPath string, files map[string]string, dirs []string) {
	t.Helper()
	f, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("创建 zip 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for _, d := range dirs {
		hdr := &zip.FileHeader{Name: d, Method: zip.Store}
		hdr.SetMode(os.ModeDir | 0o755)
		if _, err := zw.CreateHeader(hdr); err != nil {
			t.Fatalf("写入 zip 目录项失败: %v", err)
		}
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 zip 内容失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
}

// buildTarGzArchive 在 destPath 生成一个 .tar.gz 归档。
// entries 为「名字 → 内容」，名字以 "/" 结尾者视为目录。
func buildTarGzArchive(t *testing.T, destPath string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("创建 tar.gz 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
			hdr.Size = 0
		} else {
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("写入 tar 头失败: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatalf("写入 tar 内容失败: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("关闭 tar 失败: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败: %v", err)
	}
}

// sha256OfFile 计算文件的 sha256 十六进制串。
func sha256OfFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestIsHTTPDownloadTarget 验证 HTTP(S) 下载目标判定。
func TestIsHTTPDownloadTarget(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https 有效", "https://example.com/a.zip", true},
		{"http 有效", "http://example.com/a.tar.gz", true},
		{"前后空白可容忍", "  https://example.com/a.zip  ", true},
		{"file scheme 不算", "file:///tmp/a.zip", false},
		{"ftp scheme 不算", "ftp://example.com/a.zip", false},
		{"无主机名不算", "https:///a.zip", false},
		{"纯路径不算", "/tmp/a.zip", false},
		{"空串不算", "", false},
		{"非法 URL 不算", "http://[::1]:namedport/a.zip", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsHTTPDownloadTarget(tt.url); got != tt.want {
				t.Fatalf("IsHTTPDownloadTarget(%q) = %v, 期望 %v", tt.url, got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestHostMatchesPattern 验证通配符主机名匹配。
func TestHostMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		pattern string
		want    bool
	}{
		{"后缀规则命中", "api.example.com", ".example.com", true},
		{"后缀规则要求点边界", "apiexample.com", ".example.com", false},
		{"前缀通配命中子域名", "openjiuwen-market.obs.cn-north-4.myhuaweicloud.com",
			"*.obs.*.myhuaweicloud.com", true},
		{"逐段通配命中", "a.b.c.com", "a.*.c.com", true},
		{"逐段通配不匹配末尾", "a.b.c.com", "a.*.c.org", false},
		{"段数不同不匹配", "a.b.com", "a.b.c.com", false},
		{"完全相同命中", "example.com", "example.com", true},
		{"完全不同不命中", "example.com", "other.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostMatchesPattern(tt.host, tt.pattern); got != tt.want {
				t.Fatalf("hostMatchesPattern(%q, %q) = %v, 期望 %v", tt.host, tt.pattern, got, tt.want)
			}
		})
	}
}

// TestAssertImportLocalDownloadURLAllowed 验证下载 URL 白名单校验。
func TestAssertImportLocalDownloadURLAllowed(t *testing.T) {
	t.Run("默认白名单命中华为云 OBS", func(t *testing.T) {
		err := assertImportLocalDownloadURLAllowed(
			"https://openjiuwen-market.obs.cn-north-4.myhuaweicloud.com/skill.zip")
		if err != nil {
			t.Fatalf("应通过白名单，实际 %v", err)
		}
	})

	t.Run("默认白名单外的主机被拒", func(t *testing.T) {
		err := assertImportLocalDownloadURLAllowed("https://evil.example.com/skill.zip")
		if err == nil || !strings.Contains(err.Error(), "不在白名单中") {
			t.Fatalf("应报不在白名单，实际 %v", err)
		}
	})

	t.Run("非 HTTPS 被拒", func(t *testing.T) {
		err := assertImportLocalDownloadURLAllowed("http://example.com/skill.zip")
		if err == nil || !strings.Contains(err.Error(), "必须使用 HTTPS") {
			t.Fatalf("应报必须 HTTPS，实际 %v", err)
		}
	})

	t.Run("缺少主机名被拒", func(t *testing.T) {
		err := assertImportLocalDownloadURLAllowed("https:///skill.zip")
		if err == nil || !strings.Contains(err.Error(), "缺少主机名") {
			t.Fatalf("应报缺少主机名，实际 %v", err)
		}
	})

	t.Run("环境变量覆盖白名单并可跳过空项", func(t *testing.T) {
		t.Setenv(importLocalAllowedDownloadHostsEnv, " , *.example.com ,, ")
		err := assertImportLocalDownloadURLAllowed("https://cdn.example.com/skill.zip")
		if err != nil {
			t.Fatalf("环境变量白名单应命中，实际 %v", err)
		}
	})

	t.Run("环境变量全为空项时回落默认白名单", func(t *testing.T) {
		t.Setenv(importLocalAllowedDownloadHostsEnv, " , , ")
		err := assertImportLocalDownloadURLAllowed("https://evil.example.com/skill.zip")
		if err == nil || !strings.Contains(err.Error(), "不在白名单中") {
			t.Fatalf("应回落默认白名单并拒绝，实际 %v", err)
		}
	})
}

// TestVerifyFileSHA256 验证 SHA256 校验。
func TestVerifyFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	want := sha256OfFile(t, path)

	t.Run("大小写不敏感匹配", func(t *testing.T) {
		if err := verifyFileSHA256(path, strings.ToUpper(want)); err != nil {
			t.Fatalf("大写摘要应通过，实际 %v", err)
		}
	})

	t.Run("不匹配报错", func(t *testing.T) {
		err := verifyFileSHA256(path, strings.Repeat("0", 64))
		if err == nil || !strings.Contains(err.Error(), "SHA256 不匹配") {
			t.Fatalf("应报不匹配，实际 %v", err)
		}
	})

	t.Run("文件不存在报错", func(t *testing.T) {
		err := verifyFileSHA256(filepath.Join(dir, "missing.txt"), want)
		if err == nil || !strings.Contains(err.Error(), "打开文件失败") {
			t.Fatalf("应报打开文件失败，实际 %v", err)
		}
	})
}

// TestExtractArchive_格式分发 验证按扩展名分发与不支持格式报错。
func TestExtractArchive_格式分发(t *testing.T) {
	dir := t.TempDir()

	t.Run("不支持的格式报错", func(t *testing.T) {
		err := extractArchive(filepath.Join(dir, "a.rar"), dir)
		if err == nil || !strings.Contains(err.Error(), "不支持的归档格式") {
			t.Fatalf("应报不支持的格式，实际 %v", err)
		}
	})

	t.Run("tar.bz2 声明不支持", func(t *testing.T) {
		err := extractArchive(filepath.Join(dir, "a.tar.bz2"), dir)
		if err == nil || !strings.Contains(err.Error(), "暂不支持") {
			t.Fatalf("应报暂不支持，实际 %v", err)
		}
	})
}

// TestExtractZIP 验证 ZIP 解压（含目录项与 Zip Slip 防护）。
func TestExtractZIP(t *testing.T) {
	t.Run("正常解压文件与目录", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "a.zip")
		buildZipArchive(t, archive, map[string]string{
			"pkg/sub/SKILL.md": "# skill",
		}, []string{"pkg/", "pkg/sub/"})

		dest := filepath.Join(dir, "out")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := extractZIP(archive, dest); err != nil {
			t.Fatalf("解压失败: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dest, "pkg", "sub", "SKILL.md"))
		if err != nil {
			t.Fatalf("读取解压文件失败: %v", err)
		}
		if string(data) != "# skill" {
			t.Fatalf("内容 = %q, 期望 %q", string(data), "# skill")
		}
	})

	t.Run("Zip Slip 被拒绝", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "evil.zip")
		buildZipArchive(t, archive, map[string]string{
			"../escaped.txt": "bad",
		}, nil)

		dest := filepath.Join(dir, "out")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		err := extractZIP(archive, dest)
		if err == nil || !strings.Contains(err.Error(), "zip slip") {
			t.Fatalf("应检测到 zip slip，实际 %v", err)
		}
	})

	t.Run("归档非法时报读取失败", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "bad.zip")
		if err := os.WriteFile(archive, []byte("not a zip"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		err := extractZIP(archive, dir)
		if err == nil || !strings.Contains(err.Error(), "ZIP 读取失败") {
			t.Fatalf("应报 ZIP 读取失败，实际 %v", err)
		}
	})
}

// TestExtractTarGz 验证 .tar.gz 解压（含目录项、普通文件与路径穿越防护）。
func TestExtractTarGz(t *testing.T) {
	t.Run("正常解压", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "a.tar.gz")
		buildTarGzArchive(t, archive, map[string]string{
			"pkg/":             "",
			"pkg/SKILL.md":     "# skill",
			"pkg/scripts/x.sh": "echo hi",
		})

		dest := filepath.Join(dir, "out")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := extractTarGz(archive, dest); err != nil {
			t.Fatalf("解压失败: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dest, "pkg", "scripts", "x.sh"))
		if err != nil {
			t.Fatalf("读取解压文件失败: %v", err)
		}
		if string(data) != "echo hi" {
			t.Fatalf("内容 = %q, 期望 %q", string(data), "echo hi")
		}
	})

	t.Run("路径穿越被拒绝", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "evil.tar.gz")
		buildTarGzArchive(t, archive, map[string]string{
			"../escaped.txt": "bad",
		})

		dest := filepath.Join(dir, "out")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		err := extractTarGz(archive, dest)
		if err == nil || !strings.Contains(err.Error(), "路径穿越检测") {
			t.Fatalf("应检测到路径穿越，实际 %v", err)
		}
	})

	t.Run("gzip 非法时报解压失败", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "bad.tar.gz")
		if err := os.WriteFile(archive, []byte("not gzip"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		err := extractTarGz(archive, dir)
		if err == nil || !strings.Contains(err.Error(), "gzip 解压失败") {
			t.Fatalf("应报 gzip 解压失败，实际 %v", err)
		}
	})

	t.Run("归档不存在时报打开失败", func(t *testing.T) {
		err := extractTarGz(filepath.Join(t.TempDir(), "missing.tar.gz"), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "打开 tar.gz 失败") {
			t.Fatalf("应报打开失败，实际 %v", err)
		}
	})
}

// TestExtractTar_BadStream 验证 tar 流损坏时报读取失败。
func TestExtractTar_BadStream(t *testing.T) {
	err := extractTar(bytes.NewReader([]byte("garbage-data-not-tar")), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "tar 读取失败") {
		t.Fatalf("应报 tar 读取失败，实际 %v", err)
	}
}

// TestDownloadArchive 验证 HTTP 下载（成功、状态码错误、非法 URL、文件名清洗）。
func TestDownloadArchive(t *testing.T) {
	t.Run("成功下载并落盘", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("archive-bytes"))
		}))
		defer srv.Close()

		dest := t.TempDir()
		path, err := downloadArchive(context.Background(), srv.URL+"/skill.zip", dest)
		if err != nil {
			t.Fatalf("下载失败: %v", err)
		}
		if filepath.Base(path) != "skill.zip" {
			t.Fatalf("文件名 = %q, 期望 skill.zip", filepath.Base(path))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取下载文件失败: %v", err)
		}
		if string(data) != "archive-bytes" {
			t.Fatalf("内容 = %q", string(data))
		}
	})

	t.Run("非 200 状态码报错", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := downloadArchive(context.Background(), srv.URL+"/skill.zip", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "下载返回状态码") {
			t.Fatalf("应报状态码错误，实际 %v", err)
		}
	})

	t.Run("非法请求 URL 报构建失败", func(t *testing.T) {
		_, err := downloadArchive(context.Background(), "http://[::1]:namedport/a.zip", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "构建请求失败") {
			t.Fatalf("应报构建请求失败，实际 %v", err)
		}
	})

	t.Run("连接失败报网络错误", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		url := srv.URL + "/skill.zip"
		srv.Close()

		_, err := downloadArchive(context.Background(), url, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "网络请求失败") {
			t.Fatalf("应报网络请求失败，实际 %v", err)
		}
	})

	t.Run("无文件名路径回落默认文件名", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("x"))
		}))
		defer srv.Close()

		dest := t.TempDir()
		// 基名完全为空时才回落：URL 末尾带 ? 会被清洗成空串（filepath.Base 对
		// "http://host:port/" 返回主机名而非空，故用带查询串的形态覆盖回落分支）
		path, err := downloadArchive(context.Background(), srv.URL+"/a.zip?", dest)
		if err != nil {
			t.Fatalf("下载失败: %v", err)
		}
		if filepath.Base(path) != "a.zip" {
			t.Fatalf("文件名 = %q, 期望 a.zip", filepath.Base(path))
		}
	})
}

// TestImportSkillFromRemoteArchive 验证远程导入主流程的各失败分支。
func TestImportSkillFromRemoteArchive(t *testing.T) {
	ctx := context.Background()

	t.Run("白名单外主机返回失败明细", func(t *testing.T) {
		res, err := importSkillFromRemoteArchive(ctx, nil, "https://evil.example.com/a.zip", false, "")
		if err != nil {
			t.Fatalf("白名单拒绝不应返回 error，实际 %v", err)
		}
		if toBool(res["success"]) {
			t.Fatalf("应返回 success=false，实际 %+v", res)
		}
		if detail, _ := res["detail"].(string); !strings.Contains(detail, "不在白名单中") {
			t.Fatalf("detail = %q", detail)
		}
	})

	t.Run("下载失败返回失败明细", func(t *testing.T) {
		// 白名单放行本地回环（u.Hostname() 已剥离端口，故白名单只写主机名），
		// 再让该 https 地址连接失败，覆盖「下载失败」分支。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		hostPort := strings.TrimPrefix(srv.URL, "http://")
		srv.Close()
		t.Setenv(importLocalAllowedDownloadHostsEnv, "127.0.0.1")

		res, err := importSkillFromRemoteArchive(ctx, nil, "https://"+hostPort+"/a.zip", false, "")
		if err != nil {
			t.Fatalf("下载失败不应返回 error，实际 %v", err)
		}
		if toBool(res["success"]) {
			t.Fatalf("应返回 success=false，实际 %+v", res)
		}
		if detail, _ := res["detail"].(string); !strings.Contains(detail, "下载失败") {
			t.Fatalf("detail = %q", detail)
		}
	})
}
