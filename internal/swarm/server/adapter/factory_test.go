package adapter

import (
	"fmt"
	"os"
	"testing"
)

// TestResolveSDKChoice_未设置环境变量 验证默认值返回 harness
func TestResolveSDKChoice_未设置环境变量(t *testing.T) {
	_ = os.Unsetenv(sdkEnvVar)
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_空值 验证空值返回默认
func TestResolveSDKChoice_空值(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "")
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_harness 验证 harness 值
func TestResolveSDKChoice_harness(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "harness")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestResolveSDKChoice_pi 验证 pi 值（预留）
func TestResolveSDKChoice_pi(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "pi")
	got := ResolveSDKChoice()
	if got != "pi" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "pi")
	}
}

// TestResolveSDKChoice_未知值 验证未知值回退默认
func TestResolveSDKChoice_未知值(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "unknown_sdk")
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_大小写 验证大小写不敏感
func TestResolveSDKChoice_大小写(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "HARNESS")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestResolveSDKChoice_前后空格 验证 trim
func TestResolveSDKChoice_前后空格(t *testing.T) {
	_ = os.Setenv(sdkEnvVar, "  harness  ")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestCreateAdapter_未知SDK 验证未知 SDK 返回错误
func TestCreateAdapter_未知SDK(t *testing.T) {
	_, err := CreateAdapter("unknown", "agent")
	if err == nil {
		t.Error("CreateAdapter() 应返回错误，得到 nil")
	}
}

// TestCreateAdapter_pi未实现 验证 pi SDK 返回未实现错误
func TestCreateAdapter_pi未实现(t *testing.T) {
	_, err := CreateAdapter("pi", "agent")
	if err == nil {
		t.Error("CreateAdapter() 应返回未实现错误，得到 nil")
	}
}

// TestCreateAdapter_harnessAgentMode 验证 harness agent 模式返回 DeepAdapter
func TestCreateAdapter_harnessAgentMode(t *testing.T) {
	adapter, err := CreateAdapter("harness", "agent")
	if err != nil {
		t.Fatalf("CreateAdapter() error: %v", err)
	}
	typeName := fmt.Sprintf("%T", adapter)
	if typeName != "*adapter.DeepAdapter" {
		t.Errorf("CreateAdapter() type = %s, want *adapter.DeepAdapter", typeName)
	}
}

// TestCreateAdapter_harnessCodeMode 验证 harness code 模式返回 CodeAdapter
func TestCreateAdapter_harnessCodeMode(t *testing.T) {
	adapter, err := CreateAdapter("harness", "code")
	if err != nil {
		t.Fatalf("CreateAdapter() error: %v", err)
	}
	typeName := fmt.Sprintf("%T", adapter)
	if typeName != "*adapter.CodeAdapter" {
		t.Errorf("CreateAdapter() type = %s, want *adapter.CodeAdapter", typeName)
	}
}

// TestCreateAdapter_空SDK默认harness 验证空 SDK 使用默认 harness
func TestCreateAdapter_空SDK默认harness(t *testing.T) {
	_ = os.Unsetenv(sdkEnvVar)
	adapter, err := CreateAdapter("", "agent")
	if err != nil {
		t.Fatalf("CreateAdapter() error: %v", err)
	}
	typeName := fmt.Sprintf("%T", adapter)
	if typeName != "*adapter.DeepAdapter" {
		t.Errorf("CreateAdapter() type = %s, want *adapter.DeepAdapter", typeName)
	}
}
