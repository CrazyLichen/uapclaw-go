package attachmentsafety

import (
	"encoding/base64"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSanitizeUploadFilename_Python锚点 逐条对齐 _sanitize_name/safe_upload_filename 行为。
// Python: attachment_safety.py:34-48/67-94。
func TestSanitizeUploadFilename_Python锚点(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		fallback string
		want     string
	}{
		{"目录剥离", "/etc/passwd", "", "passwd"},
		// PurePosixPath 不以反斜杠分目录（Python: attachment_safety.py:36 用 PurePosixPath.name），
		// 反斜杠属非法字符集被整段替换为单个 _
		{"Windows 反斜杠整段替换", `C:\temp\报告.md`, "", "C_temp_报告.md"},
		{"非法字符整段替换为单个下划线", "a<b>:c", "", "a_b_c"},
		{"BIDI 控制符剔除", "a\u200eb\u202ec", "", "abc"},
		{"尾部点空格剥离", "name. ", "", "name"},
		{"Windows 保留名拒绝后回落", "CON", "doc-1.md", "doc-1.md"},
		{"COM 设备名拒绝", "COM1", "", "upload"},
		{"全非法回落 fallback", "", "document-1.md", "document-1.md"},
		{"非 ASCII 原样保留", "中文文件名.txt", "", "中文文件名.txt"},
		{"点与点点拒绝", "..", "f.md", "f.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeUploadFilename(tc.filename, tc.fallback); got != tc.want {
				t.Fatalf("SanitizeUploadFilename(%q,%q) = %q, want %q", tc.filename, tc.fallback, got, tc.want)
			}
		})
	}
}

// TestTruncateFilenameBytes_180字节保扩展名不劈UTF8 对齐 _truncate_filename_bytes。
// Python: attachment_safety.py:60-73。
func TestTruncateFilenameBytes_180字节保扩展名不劈UTF8(t *testing.T) {
	// 中文每字 3 字节：stem 60 字 + .txt(4) = 184 字节 > 180 → stem 截到 176 字节
	long := strings.Repeat("中", 60) + ".txt"
	got := SanitizeUploadFilename(long, "")
	if len(got) > 180 {
		t.Fatalf("截断后 %d 字节 > 180", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("截断劈开了 UTF-8 序列")
	}
	if !strings.HasSuffix(got, ".txt") {
		t.Fatalf("截断丢了扩展名: %q", got)
	}
	// 后缀本身超上限 → 整名截断
	hugeSuffix := "a" + strings.Repeat("b", 200) + ".txt"
	if got := SanitizeUploadFilename(hugeSuffix, ""); len(got) > 180 {
		t.Fatalf("整名截断后 %d 字节 > 180", len(got))
	}
	// ≤180 原样
	if got := SanitizeUploadFilename(strings.Repeat("中", 30)+".md", ""); got != strings.Repeat("中", 30)+".md" {
		t.Fatalf("未超限名字被改动: %q", got)
	}
}

// TestForbiddenFormats_逐字集合 对齐 FORBIDDEN_DOCUMENT_EXTENSIONS 29 项与排序输出。
// Python: attachment_safety.py:110-148。
func TestForbiddenFormats_逐字集合(t *testing.T) {
	// 集合成员逐字对齐 Python: attachment_safety.py:110-143（29 项）；
	// 顺序用 sort.Strings 机器生成，杜绝手排笔误。
	want := []string{
		".exe", ".dll", ".msi", ".scr", ".bat", ".cmd", ".ps1", ".vbs", ".wsf",
		".hta", ".jar", ".lnk", ".bin", ".so", ".dylib", ".app", ".dmg", ".pkg",
		".command", ".scpt", ".scptd", ".workflow", ".xpc", ".bundle",
		".framework", ".kext", ".prefpane", ".saver", ".component",
	}
	sort.Strings(want)
	got := ForbiddenFormats()
	if len(got) != len(want) {
		t.Fatalf("ForbiddenFormats() = %d 项, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ForbiddenFormats()[%d] = %q, want %q（须排序）", i, got[i], want[i])
		}
	}
}

// TestIsForbiddenDocument_大小写与补点 对齐 is_forbidden_document。
// Python: attachment_safety.py:155-161。
func TestIsForbiddenDocument_大小写与补点(t *testing.T) {
	if !IsForbiddenDocument("virus.EXE", "") {
		t.Fatal("大写 .EXE 应命中黑名单")
	}
	if !IsForbiddenDocument("", "bat") {
		t.Fatal("无点后缀 bat 应自动补点命中")
	}
	if IsForbiddenDocument("notes.txt", "") {
		t.Fatal(".txt 不在黑名单")
	}
	if IsForbiddenDocument("README", "") {
		t.Fatal("无后缀不应拒绝")
	}
}

// TestSafeSessionDirname_点目录与截断 对齐 safe_session_dirname。
// Python: attachment_safety.py:164-183。
func TestSafeSessionDirname_点目录与截断(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "default"},
		{".", "default"},
		{"..", "default"},
		{"...", "default"}, // Windows 尾点剥离后等价 ".."
		{"a/b:c", "a_b_c"}, // 非法字符替换
		{"ok-id_1.2", "ok-id_1.2"},
	}
	for _, tc := range cases {
		if got := SafeSessionDirname(tc.in); got != tc.want {
			t.Fatalf("SafeSessionDirname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// 120 字符上限
	long := strings.Repeat("a", 200)
	if got := SafeSessionDirname(long); len(got) != 120 {
		t.Fatalf("会话名截断后 %d 字符, want 120", len([]rune(got)))
	}
}

// TestStripDataURIPrefix 对齐 strip_data_uri_prefix。
// Python: attachment_safety.py:186-195。
func TestStripDataURIPrefix(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("hello"))
	if got := StripDataURIPrefix("data:image/png;base64," + payload); got != payload {
		t.Fatalf("data-uri 剥离失败: %q", got)
	}
	if got := StripDataURIPrefix(payload); got != payload {
		t.Fatalf("无前缀应原样返回: %q", got)
	}
	if got := StripDataURIPrefix("data:image/png;base64"); got != "" {
		t.Fatalf("data: 前缀但无逗号应返回空: %q", got)
	}
}

// TestImageMimeHelpers 对齐 image_suffix_for_mime/supported_image_suffixes/ensure_image_upload_filename。
// Python: attachment_safety.py:156-178。
func TestImageMimeHelpers(t *testing.T) {
	if got := ImageSuffixForMime("image/jpeg"); got != ".jpg" {
		t.Fatalf("image/jpeg 规范后缀 = %q, want .jpg", got)
	}
	if got := ImageSuffixForMime("image/heic"); got != "" {
		t.Fatalf("不支持类型应返回空: %q", got)
	}
	for _, s := range []string{".png", ".jpg", ".webp", ".gif", ".jpeg", ".jfif"} {
		if _, ok := SupportedImageSuffixes()[s]; !ok {
			t.Fatalf("SupportedImageSuffixes 缺 %q", s)
		}
	}
	// .jpeg 别名保留原名，不再拼 .jpg
	if got := EnsureImageUploadFilename("sample.jpeg", ".jpg"); got != "sample.jpeg" {
		t.Fatalf(".jpeg 别名应保留原名: %q", got)
	}
	if got := EnsureImageUploadFilename("sample", ".png"); got != "sample.png" {
		t.Fatalf("无后缀应补规范后缀: %q", got)
	}
}
