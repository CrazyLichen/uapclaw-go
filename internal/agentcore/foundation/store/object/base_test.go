package object

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── ListOption 测试 ────────────────────────────

func TestWithMaxObjects(t *testing.T) {
	opts := NewListOptions(WithMaxObjects(50))
	assert.Equal(t, 50, opts.MaxObjects)
}

func TestListOptions_Default(t *testing.T) {
	opts := NewListOptions()
	assert.Equal(t, 100, opts.MaxObjects)
}

func TestListOptions_Multiple(t *testing.T) {
	opts := NewListOptions(WithMaxObjects(200), WithMaxObjects(300))
	// 最后一个 WithMaxObjects 生效
	assert.Equal(t, 300, opts.MaxObjects)
}

// ──────────────────────────── ObjectStorageConfig 测试 ────────────────────────────

func TestObjectStorageConfig_EnvFallback(t *testing.T) {
	// 设置环境变量
	os.Setenv("OBS_SERVER", "https://obs.example.com")
	os.Setenv("OBS_ACCESS_KEY_ID", "test-ak")
	os.Setenv("OBS_SECRET_ACCESS_KEY", "test-sk")
	os.Setenv("OBS_REGION", "cn-north-4")
	defer func() {
		os.Unsetenv("OBS_SERVER")
		os.Unsetenv("OBS_ACCESS_KEY_ID")
		os.Unsetenv("OBS_SECRET_ACCESS_KEY")
		os.Unsetenv("OBS_REGION")
	}()

	cfg := ObjectStorageConfig{}
	cfg.ApplyEnvFallback()

	assert.Equal(t, "https://obs.example.com", cfg.Server)
	assert.Equal(t, "test-ak", cfg.AccessKeyID)
	assert.Equal(t, "test-sk", cfg.SecretAccessKey)
	assert.Equal(t, "cn-north-4", cfg.RegionName)
}

func TestObjectStorageConfig_StructOverEnv(t *testing.T) {
	os.Setenv("OBS_SERVER", "https://env.example.com")
	defer os.Unsetenv("OBS_SERVER")

	cfg := ObjectStorageConfig{
		Server: "https://struct.example.com",
	}
	cfg.ApplyEnvFallback()

	// 结构体字段优先，不从环境变量读取
	assert.Equal(t, "https://struct.example.com", cfg.Server)
}

func TestObjectStorageConfig_PartialEnvFallback(t *testing.T) {
	os.Setenv("OBS_SERVER", "https://env.example.com")
	os.Setenv("OBS_REGION", "cn-south-1")
	defer func() {
		os.Unsetenv("OBS_SERVER")
		os.Unsetenv("OBS_REGION")
	}()

	cfg := ObjectStorageConfig{
		AccessKeyID: "struct-ak",
	}
	cfg.ApplyEnvFallback()

	// 非空字段保留结构体值，空字段从环境变量读取
	assert.Equal(t, "https://env.example.com", cfg.Server)
	assert.Equal(t, "struct-ak", cfg.AccessKeyID)
	assert.Equal(t, "cn-south-1", cfg.RegionName)
}

func TestObjectStorageConfig_NoEnvSet(t *testing.T) {
	// 确保环境变量不存在
	os.Unsetenv("OBS_SERVER")
	os.Unsetenv("OBS_ACCESS_KEY_ID")
	os.Unsetenv("OBS_SECRET_ACCESS_KEY")
	os.Unsetenv("OBS_REGION")

	cfg := ObjectStorageConfig{}
	cfg.ApplyEnvFallback()

	// 无环境变量时，所有字段保持空值
	assert.Equal(t, "", cfg.Server)
	assert.Equal(t, "", cfg.AccessKeyID)
	assert.Equal(t, "", cfg.SecretAccessKey)
	assert.Equal(t, "", cfg.RegionName)
}
