package extensions

import (
	"context"
	"testing"
)

// TestNewExtensionLoader 测试 ExtensionLoader 构造
func TestNewExtensionLoader(t *testing.T) {
	loader := NewExtensionLoader(nil)
	if loader == nil {
		t.Error("NewExtensionLoader(nil) = nil, want non-nil")
	}
}

// TestExtensionLoader_AddSearchPath 测试空实现不报错
func TestExtensionLoader_AddSearchPath(t *testing.T) {
	loader := NewExtensionLoader(nil)
	loader.AddSearchPath("/tmp/test")
	// 空实现，只要不 panic 即可
}

// TestExtensionLoader_DiscoverExtensionRoots 测试返回空列表
func TestExtensionLoader_DiscoverExtensionRoots(t *testing.T) {
	loader := NewExtensionLoader(nil)
	result := loader.DiscoverExtensionRoots()
	if result != nil {
		t.Errorf("DiscoverExtensionRoots() = %v, want nil", result)
	}
}

// TestExtensionLoader_LoadExtension 测试返回错误
func TestExtensionLoader_LoadExtension(t *testing.T) {
	loader := NewExtensionLoader(nil)
	_, err := loader.LoadExtension(context.Background(), "/tmp/test")
	if err == nil {
		t.Error("LoadExtension() should return error (stub)")
	}
}

// TestNewExtensionManager 测试 ExtensionManager 构造
func TestNewExtensionManager(t *testing.T) {
	mgr := NewExtensionManager(nil)
	if mgr == nil {
		t.Error("NewExtensionManager(nil) = nil, want non-nil")
	}
}

// TestExtensionManager_LoadAllExtensions 测试返回错误
func TestExtensionManager_LoadAllExtensions(t *testing.T) {
	mgr := NewExtensionManager(nil)
	err := mgr.LoadAllExtensions(context.Background())
	if err == nil {
		t.Error("LoadAllExtensions() should return error (stub)")
	}
}

// TestExtensionManager_ShutdownAllExtensions 测试空实现不报错
func TestExtensionManager_ShutdownAllExtensions(t *testing.T) {
	mgr := NewExtensionManager(nil)
	err := mgr.ShutdownAllExtensions(context.Background())
	if err != nil {
		t.Errorf("ShutdownAllExtensions() error: %v", err)
	}
}

// TestExtensionManager_ListExtensions 测试返回空列表
func TestExtensionManager_ListExtensions(t *testing.T) {
	mgr := NewExtensionManager(nil)
	result := mgr.ListExtensions()
	if result != nil {
		t.Errorf("ListExtensions() = %v, want nil", result)
	}
}
