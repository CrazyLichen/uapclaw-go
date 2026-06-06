package logger

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewRotationConfig(t *testing.T) {
	cfg := NewRotationConfig()
	if cfg.MaxSize != defaultMaxSizeBytes {
		t.Errorf("期望 MaxSize = %d，实际 %d", defaultMaxSizeBytes, cfg.MaxSize)
	}
	if cfg.MaxBackups != defaultMaxBackups {
		t.Errorf("期望 MaxBackups = %d，实际 %d", defaultMaxBackups, cfg.MaxBackups)
	}
	if cfg.MaxAge != 0 {
		t.Errorf("期望 MaxAge = 0，实际 %d", cfg.MaxAge)
	}
	if cfg.Compress {
		t.Error("期望 Compress = false")
	}
}

func TestNewRotatingWriter(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/test.log"

	cfg := NewRotationConfig()
	lj := NewRotatingWriter(filePath, cfg)

	if lj == nil {
		t.Fatal("期望返回 lumberjack.Logger 实例")
	}
	if lj.Filename != filePath {
		t.Errorf("期望 Filename = %s，实际 %s", filePath, lj.Filename)
	}
	// lumberjack MaxSize 单位是 MB
	expectedMB := defaultMaxSizeBytes / 1024 / 1024
	if lj.MaxSize != expectedMB {
		t.Errorf("期望 MaxSize = %d，实际 %d", expectedMB, lj.MaxSize)
	}
	if lj.MaxBackups != defaultMaxBackups {
		t.Errorf("期望 MaxBackups = %d，实际 %d", defaultMaxBackups, lj.MaxBackups)
	}
}

func TestNewRotatingWriter_零值使用默认(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/test.log"

	cfg := RotationConfig{} // 所有字段为零值
	lj := NewRotatingWriter(filePath, cfg)

	expectedMB := defaultMaxSizeBytes / 1024 / 1024
	if lj.MaxSize != expectedMB {
		t.Errorf("期望 MaxSize 使用默认值 %d MB，实际 %d", expectedMB, lj.MaxSize)
	}
	if lj.MaxBackups != defaultMaxBackups {
		t.Errorf("期望 MaxBackups 使用默认值 %d，实际 %d", defaultMaxBackups, lj.MaxBackups)
	}
}

func TestMutexWriter(t *testing.T) {
	var buf bytes.Buffer
	mw := NewMutexWriter(&buf)

	data := []byte("hello world")
	n, err := mw.Write(data)
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if n != len(data) {
		t.Errorf("期望写入 %d 字节，实际 %d", len(data), n)
	}
	if buf.String() != "hello world" {
		t.Errorf("期望 hello world，实际 %q", buf.String())
	}
}

func TestMutexWriter_并发写入(t *testing.T) {
	var buf bytes.Buffer
	mw := NewMutexWriter(&buf)

	// 并发写入 100 个 goroutine
	const goroutines = 100
	const msg = "x"
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, _ = mw.Write([]byte(msg))
			done <- struct{}{}
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 验证写入的总字节数
	if len(buf.String()) != goroutines {
		t.Errorf("期望 %d 字节，实际 %d", goroutines, len(buf.String()))
	}
}

func TestMutexWriter_Close(t *testing.T) {
	// 测试底层 writer 不是 Closer 的情况
	var buf bytes.Buffer
	mw := NewMutexWriter(&buf)
	if err := mw.Close(); err != nil {
		t.Errorf("期望 Close 成功，实际 %v", err)
	}
}

func TestEnsureLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := tmpDir + "/sub/dir/test.log"

	if err := EnsureLogDir(logPath); err != nil {
		t.Fatalf("EnsureLogDir 失败: %v", err)
	}

	// 验证目录已创建
	dir := tmpDir + "/sub/dir"
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("期望目录 %s 已创建", dir)
	}
}

func TestNewRotatingWriter_实际写入(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/test.log"

	cfg := NewRotationConfig()
	lj := NewRotatingWriter(filePath, cfg)

	msg := "测试日志消息\n"
	n, err := lj.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if n != len(msg) {
		t.Errorf("期望写入 %d 字节，实际 %d", len(msg), n)
	}

	// 验证文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if !strings.Contains(string(data), "测试日志消息") {
		t.Errorf("期望文件包含 '测试日志消息'，实际 %q", string(data))
	}
}
