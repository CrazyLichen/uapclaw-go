//go:build test

package local

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// TestDecodeOutput_GBK 表驱动测试子进程输出的编码解码。
func TestDecodeOutput_GBK(t *testing.T) {
	// "你好" 的 GBK/cp936 编码字节
	gbkHello := []byte{0xC4, 0xE3, 0xBA, 0xC3}

	tests := []struct {
		name    string
		raw     []byte
		encName string
		want    string
	}{
		{"空输入", []byte{}, "gbk", ""},
		{"utf-8缺省直通", []byte("hello"), "", "hello"},
		{"utf-8显式", []byte("你好"), "utf-8", "你好"},
		{"gbk中文", gbkHello, "gbk", "你好"},
		{"cp936别名", gbkHello, "cp936", "你好"},
		{"gb2312别名", gbkHello, "gb2312", "你好"},
		{"GBK大小写", gbkHello, "GBK", "你好"},
		{"utf-8直通不受gbk影响", []byte("ascii"), "gbk", "ascii"},
		{"未知编码回退原样", []byte{0xC4, 0xE3}, "nonexistent-enc", string([]byte{0xC4, 0xE3})},
		{"big5繁体", []byte{0xA4, 0xA4, 0xA4, 0xE5}, "big5", "中文"}, // Big5 双字节 "中文"
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeOutput(tt.raw, tt.encName); got != tt.want {
				t.Errorf("decodeOutput(%v, %q) = %q, want %q", tt.raw, tt.encName, got, tt.want)
			}
		})
	}
}

// TestDecodeOutput_GB18030 测试 gb18030 解码。
func TestDecodeOutput_GB18030(t *testing.T) {
	// "你" 的 gb18030 编码与 GBK 相同
	raw := []byte{0xC4, 0xE3}
	if got := decodeOutput(raw, "gb18030"); got != "你" {
		t.Errorf("gb18030 解码 = %q, want 你", got)
	}
}

// TestDetectShellEncoding_Linux 测试非 Windows 平台的环境变量检测。
func TestDetectShellEncoding_Linux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("非 Windows 分支用例，Windows 上跳过")
	}
	t.Setenv("LC_ALL", "zh_CN.GBK")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LANG", "")
	if got := detectShellEncoding(); got != "GBK" {
		t.Errorf("LC_ALL=zh_CN.GBK 应检测为 GBK，got %q", got)
	}

	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := detectShellEncoding(); got != "UTF-8" {
		t.Errorf("LANG=en_US.UTF-8 应检测为 UTF-8，got %q", got)
	}

	t.Setenv("LANG", "")
	if got := detectShellEncoding(); got != defaultShellEncoding {
		t.Errorf("无环境变量应回落 %q，got %q", defaultShellEncoding, got)
	}
}

// gbkHelloBytes "你好" 的 GBK 编码字节
var gbkHelloBytes = []byte{0xC4, 0xE3, 0xBA, 0xC3}

// TestHelperProcess 子进程辅助入口（经典 os/exec helper 模式）：
// 通过环境变量 GO_HELPER_PROCESS=1 + GO_HELPER_MODE 控制输出内容，
// 避免依赖外部命令（cat/printf），跨平台且不受系统命令环境影响。
// 普通测试运行时 GO_HELPER_PROCESS 为空，立即返回（表现为空测试）。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_HELPER_MODE") {
	case "gbk":
		_, _ = os.Stdout.Write(append(bytes.Clone(gbkHelloBytes), []byte(" GBK")...))
	case "utf8":
		_, _ = os.Stdout.WriteString("中文utf8")
	case "stream":
		_, _ = os.Stdout.Write(append(bytes.Clone(gbkHelloBytes), []byte(" stream")...))
	}
	os.Exit(0)
}

// helperCommand 启动输出指定模式内容的子进程。
func helperCommand(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess$")
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1", "GO_HELPER_MODE="+mode)
	return cmd
}

// TestInvoke_GBK输出按指定编码解码 端到端：子进程输出 GBK 字节，
// handler 按 encoding=gbk 解码后应得到正确中文。
// 回归：曾直接把原始字节当 UTF-8（scanner.Text()），Windows cmd 的
// GBK 输出全部乱码。
func TestInvoke_GBK输出按指定编码解码(t *testing.T) {
	cmd := helperCommand("gbk")
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, "gbk", 30)

	data, invokeErr := handler.Invoke(t.Context())
	if invokeErr != nil {
		t.Fatalf("Invoke 失败: %v", invokeErr)
	}
	if !strings.Contains(data.Stdout, "你好 GBK") {
		t.Errorf("GBK 输出应解码为「你好 GBK」，stdout=%q stderr=%q exit=%d", data.Stdout, data.Stderr, data.ExitCode)
	}
}

// TestInvoke_UTF8缺省直通 未指定编码时按 UTF-8 直通，行为不变。
func TestInvoke_UTF8缺省直通(t *testing.T) {
	cmd := helperCommand("utf8")
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, "", 30)

	data, invokeErr := handler.Invoke(t.Context())
	if invokeErr != nil {
		t.Fatalf("Invoke 失败: %v", invokeErr)
	}
	if !strings.Contains(data.Stdout, "中文utf8") {
		t.Errorf("UTF-8 输出应保持不变，stdout=%q stderr=%q exit=%d", data.Stdout, data.Stderr, data.ExitCode)
	}
}

// TestStream_GBK输出按指定编码解码 流式路径同样按 encoding 解码。
func TestStream_GBK输出按指定编码解码(t *testing.T) {
	cmd := helperCommand("stream")
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, "gbk", 30)

	ch, streamErr := handler.Stream(t.Context())
	if streamErr != nil {
		t.Fatalf("Stream 失败: %v", streamErr)
	}
	var collected strings.Builder
	for event := range ch {
		if event.Type == StreamEventTypeStdout {
			collected.WriteString(event.Data)
		}
	}
	if !strings.Contains(collected.String(), "你好 stream") {
		t.Errorf("流式 GBK 输出应解码为「你好 stream」，实际: %q", collected.String())
	}
}

// TestDetectShellEncoding_WindowsACP Windows 上应返回 GetACP 代码页名。
// 仅在 Windows 环境编译执行（CI linux 跳过），验证 GetACP 路径非空且形如 cpNNNN。
func TestDetectShellEncoding_WindowsACP(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 专属用例")
	}
	got := detectShellEncoding()
	if !strings.HasPrefix(got, "cp") {
		t.Errorf("Windows 应返回 cpNNNN 形式的代码页，got %q", got)
	}
	if _, err := os.Stat(`C:\Windows`); err != nil {
		t.Logf("警告：异常的 Windows 环境，检测结果 %q", got)
	}
}

// TestFsReadFile_GBK编码 text 模式按 encoding 参数解码 GBK 文件。
// 回归：曾直接 string(content) 忽略 encoding，读 GBK 文件乱码。
func TestFsReadFile_GBK编码(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := t.Context()
	tmpDir := t.TempDir()
	gbkPath := filepath.Join(tmpDir, "gbk.txt")

	// 写入 "你好文件" 的 GBK 字节
	gbkHello := append([]byte{0xC4, 0xE3, 0xBA, 0xC3}, []byte(" file")...)
	if err := os.WriteFile(gbkPath, gbkHello, 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	res, err := fsOp.ReadFile(ctx, gbkPath, sysop.WithFsMode("text"), sysop.WithFsEncoding("gbk"))
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if res.Data == nil || !strings.Contains(res.Data.Content, "你好 file") {
		t.Errorf("GBK 文件应解码为「你好 file」，实际: %q", res.Data)
	}
}

// TestFsReadFile_Tail_GBK编码 tail 模式同样按 encoding 解码。
func TestFsReadFile_Tail_GBK编码(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := t.Context()
	tmpDir := t.TempDir()
	gbkPath := filepath.Join(tmpDir, "gbk_tail.txt")

	content := "ascii line\n" + string([]byte{0xC4, 0xE3, 0xBA, 0xC3}) + " tail\n"
	if err := os.WriteFile(gbkPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	res, err := fsOp.ReadFile(ctx, gbkPath, sysop.WithFsMode("text"), sysop.WithFsEncoding("gbk"), sysop.WithFsTail(1))
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if res.Data == nil || !strings.Contains(res.Data.Content, "你好 tail") {
		t.Errorf("tail 行应解码为「你好 tail」，实际: %q", res.Data)
	}
}

// TestFsReadFileStream_GBK不截断字符 text 模式流式读取按 encoding 解码且
// 不从多字节字符中间切块。
func TestFsReadFileStream_GBK不截断字符(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := t.Context()
	tmpDir := t.TempDir()
	gbkPath := filepath.Join(tmpDir, "gbk_stream.txt")

	// GBK "你好你好你好" + ASCII 填充，强制多块切分
	raw := bytes.Repeat([]byte{0xC4, 0xE3, 0xBA, 0xC3}, 10)
	if err := os.WriteFile(gbkPath, raw, 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	ch, err := fsOp.ReadFileStream(ctx, gbkPath,
		sysop.WithFsMode("text"), sysop.WithFsEncoding("gbk"), sysop.WithFsChunkSize(5))
	if err != nil {
		t.Fatalf("ReadFileStream 失败: %v", err)
	}
	var collected strings.Builder
	for c := range ch {
		collected.WriteString(c.Data.ChunkContent)
	}
	// 每块都是合法 UTF-8（rune 未被截断）且拼接后完整还原
	if got := collected.String(); got != strings.Repeat("你好", 10) {
		t.Errorf("GBK 流式读取应还原「你好」×10，实际: %q", got)
	}
	for _, r := range []rune(collected.String()) {
		if r == utf8.RuneError {
			t.Fatalf("流式块存在被截断的字符: %q", collected.String())
		}
	}
}
