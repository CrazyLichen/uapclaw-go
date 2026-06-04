package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestReloader_文件变更触发回调(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 创建初始配置
	os.WriteFile(cfgPath, []byte("key: old\n"), 0o644)

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	cfg.Load()

	// 创建热重载器
	reloader, err := NewReloader(cfg)
	if err != nil {
		t.Fatalf("NewReloader 失败: %v", err)
	}

	var reloadCount int32
	reloader.OnReload(func(data map[string]any) {
		atomic.AddInt32(&reloadCount, 1)
		if data["key"] != "new" {
			t.Errorf("期望 key=new，实际 %v", data["key"])
		}
	})

	if err := reloader.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer reloader.Stop()

	// 修改配置文件
	os.WriteFile(cfgPath, []byte("key: new\n"), 0o644)

	// 等待防抖 + 回调执行（防抖 500ms + 余量）
	time.Sleep(1 * time.Second)

	if atomic.LoadInt32(&reloadCount) < 1 {
		t.Error("期望至少触发一次回调")
	}
}

func TestReloader_防抖(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	os.WriteFile(cfgPath, []byte("key: initial\n"), 0o644)

	cfg, _ := New(cfgPath)
	cfg.Load()

	reloader, _ := NewReloader(cfg)
	reloader.debounce = 200 * time.Millisecond // 短防抖方便测试

	var reloadCount int32
	reloader.OnReload(func(data map[string]any) {
		atomic.AddInt32(&reloadCount, 1)
	})

	reloader.Start()
	defer reloader.Stop()

	// 快速连续修改 3 次
	for i := 0; i < 3; i++ {
		os.WriteFile(cfgPath, []byte("key: updated\n"), 0o644)
		time.Sleep(50 * time.Millisecond)
	}

	// 等待防抖结束
	time.Sleep(500 * time.Millisecond)

	// 防抖应该合并为 1 次回调
	count := atomic.LoadInt32(&reloadCount)
	if count != 1 {
		t.Errorf("防抖后期望 1 次回调，实际 %d 次", count)
	}
}

func TestReloader_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	os.WriteFile(cfgPath, []byte("key: value\n"), 0o644)

	cfg, _ := New(cfgPath)
	cfg.Load()

	reloader, _ := NewReloader(cfg)
	reloader.Start()

	// Stop 应该正常退出
	if err := reloader.Stop(); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
}

func TestReloader_非目标文件不触发(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	os.WriteFile(cfgPath, []byte("key: value\n"), 0o644)

	cfg, _ := New(cfgPath)
	cfg.Load()

	reloader, _ := NewReloader(cfg)
	reloader.debounce = 100 * time.Millisecond

	var reloadCount int32
	reloader.OnReload(func(data map[string]any) {
		atomic.AddInt32(&reloadCount, 1)
	})

	reloader.Start()
	defer reloader.Stop()

	// 修改同目录下的其他文件
	otherFile := filepath.Join(tmpDir, "other.yaml")
	os.WriteFile(otherFile, []byte("other: data\n"), 0o644)

	time.Sleep(300 * time.Millisecond)

	if atomic.LoadInt32(&reloadCount) != 0 {
		t.Error("非目标文件变更不应触发回调")
	}
}
