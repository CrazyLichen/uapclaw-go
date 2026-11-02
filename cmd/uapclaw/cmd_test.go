package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildAppURL_监听地址转访问地址 测试通配地址替换与端口拼接
func TestBuildAppURL_监听地址转访问地址(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:19000":    "http://127.0.0.1:19000",
		"[::]:19000":       "http://127.0.0.1:19000",
		"127.0.0.1:19000":  "http://127.0.0.1:19000",
		"192.168.1.5:8080": "http://192.168.1.5:8080",
	}
	for addr, want := range cases {
		if got := buildAppURL(addr); got != want {
			t.Errorf("buildAppURL(%q) = %q, 期望 %q", addr, got, want)
		}
	}
	// 无端口（SplitHostPort 失败）时原样拼接，不再二次切分
	if got := buildAppURL("127.0.0.1"); got != "http://127.0.0.1" {
		t.Errorf("buildAppURL(无端口) = %q, 期望 http://127.0.0.1", got)
	}
}

// TestPrintAppBanner_输出界面地址 测试启动横幅写入 stdout
func TestPrintAppBanner_输出界面地址(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	old := os.Stdout
	os.Stdout = f
	printAppBanner("http://127.0.0.1:19000")
	os.Stdout = old
	if err := f.Close(); err != nil {
		t.Fatalf("关闭临时文件失败: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("读取横幅输出失败: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "http://127.0.0.1:19000") {
		t.Errorf("横幅缺少界面地址: %q", out)
	}
	if !strings.Contains(out, "Ctrl+C") {
		t.Errorf("横幅缺少停止提示: %q", out)
	}
}

// TestShouldOpenBrowser_标志与环境变量 测试 --no-browser 与 UAPCLAW_NO_BROWSER 的关闭语义
func TestShouldOpenBrowser_标志与环境变量(t *testing.T) {
	// 默认开启
	t.Setenv("UAPCLAW_NO_BROWSER", "")
	if !shouldOpenBrowser(newAppCmd()) {
		t.Error("默认应自动打开浏览器")
	}
	// nil cmd 不 panic，且回落到 env 判断
	if !shouldOpenBrowser(nil) {
		t.Error("nil cmd 时应按 env 判断（默认开启）")
	}

	// --no-browser 标志
	cmd := newAppCmd()
	if err := cmd.Flags().Set("no-browser", "true"); err != nil {
		t.Fatalf("设置 no-browser 失败: %v", err)
	}
	if shouldOpenBrowser(cmd) {
		t.Error("--no-browser 应关闭自动打开")
	}

	// 环境变量（多种真值写法）
	for _, v := range []string{"1", "true", "yes", "on", " TRUE "} {
		t.Setenv("UAPCLAW_NO_BROWSER", v)
		if shouldOpenBrowser(newAppCmd()) {
			t.Errorf("UAPCLAW_NO_BROWSER=%q 应关闭自动打开", v)
		}
	}
	// 非真值不关闭
	t.Setenv("UAPCLAW_NO_BROWSER", "0")
	if !shouldOpenBrowser(newAppCmd()) {
		t.Error("UAPCLAW_NO_BROWSER=0 不应关闭自动打开")
	}
}

// TestOpenBrowser_命令缺失时静默 测试无可执行命令时不 panic、不阻断
func TestOpenBrowser_命令缺失时静默(t *testing.T) {
	t.Setenv("PATH", "")
	openBrowser("http://127.0.0.1:19000") // 不应 panic
}

// TestReadExtensionDirs_分号分割 测试 ';' 分割与 strip
func TestReadExtensionDirs_分号分割(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	got := readExtensionDirs(map[string]any{"extension_dirs": dir1 + " ; " + dir2})
	if len(got) != 2 {
		t.Fatalf("dirs = %v, 期望 2", got)
	}
	// 相对路径转绝对（按 cwd）
	got2 := readExtensionDirs(map[string]any{"extension_dirs": "./relative/path"})
	if len(got2) != 1 {
		t.Fatalf("dirs2 = %v, 期望 1", got2)
	}
	if !filepath.IsAbs(got2[0]) {
		t.Errorf("相对路径应转绝对: %q", got2[0])
	}
}

// TestReadExtensionDirs_非字符串与缺失 测试仅支持字符串（对齐 Python）
func TestReadExtensionDirs_非字符串与缺失(t *testing.T) {
	if got := readExtensionDirs(nil); len(got) != 0 {
		t.Errorf("nil 应返回空: %v", got)
	}
	if got := readExtensionDirs(map[string]any{}); len(got) != 0 {
		t.Errorf("缺失段应返回空: %v", got)
	}
	if got := readExtensionDirs(map[string]any{"extension_dirs": []string{"/a"}}); len(got) != 0 {
		t.Errorf("非字符串应返回空（对齐 Python isinstance(str) 检查）: %v", got)
	}
	if got := readExtensionDirs("不是map"); len(got) != 0 {
		t.Errorf("extensions 非 map 应返回空: %v", got)
	}
}

// TestReadExtensionDirs_空段丢弃 测试空字符串段被过滤
func TestReadExtensionDirs_空段丢弃(t *testing.T) {
	got := readExtensionDirs(map[string]any{"extension_dirs": "/a;; ;/b"})
	if len(got) != 2 {
		t.Errorf("dirs = %v, 期望 2（空段丢弃）", got)
	}
}

// TestApplyWebAddrFlags 覆盖 --host/--port 写环境变量与空值不覆盖两条分支
func TestApplyWebAddrFlags(t *testing.T) {
	t.Setenv("UAPCLAW_WEB_HOST", "")
	t.Setenv("UAPCLAW_WEB_PORT", "")

	cmd := newAppCmd()
	if err := cmd.Flags().Set("host", "0.0.0.0"); err != nil {
		t.Fatalf("设置 host 失败: %v", err)
	}
	if err := cmd.Flags().Set("port", "19099"); err != nil {
		t.Fatalf("设置 port 失败: %v", err)
	}
	applyWebAddrFlags(cmd)

	if got := os.Getenv("UAPCLAW_WEB_HOST"); got != "0.0.0.0" {
		t.Errorf("UAPCLAW_WEB_HOST = %q，期望 0.0.0.0", got)
	}
	if got := os.Getenv("UAPCLAW_WEB_PORT"); got != "19099" {
		t.Errorf("UAPCLAW_WEB_PORT = %q，期望 19099", got)
	}

	// flag 为空时不覆盖已有值
	t.Setenv("UAPCLAW_WEB_HOST", "keep-host")
	applyWebAddrFlags(newAppCmd())
	if got := os.Getenv("UAPCLAW_WEB_HOST"); got != "keep-host" {
		t.Errorf("空 flag 不应覆盖环境变量，实际 %q", got)
	}
}
