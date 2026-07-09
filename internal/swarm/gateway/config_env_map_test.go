package gateway

import (
	"os"
	"testing"
)

func TestBuildEnvMap(t *testing.T) {
	// 设置一些环境变量
	_ = os.Setenv("MODEL_PROVIDER", "openai")
	_ = os.Setenv("MODEL_NAME", "gpt-4")
	defer func() {
		_ = os.Unsetenv("MODEL_PROVIDER")
		_ = os.Unsetenv("MODEL_NAME")
	}()

	env := BuildEnvMap()

	if env["MODEL_PROVIDER"] != "openai" {
		t.Errorf("期望 MODEL_PROVIDER=openai，实际 %v", env["MODEL_PROVIDER"])
	}
	if env["MODEL_NAME"] != "gpt-4" {
		t.Errorf("期望 MODEL_NAME=gpt-4，实际 %v", env["MODEL_NAME"])
	}
}

func TestBuildEnvMap_未设置的环境变量(t *testing.T) {
	// 确保未设置的环境变量返回空字符串
	_ = os.Unsetenv("VIDEO_PROVIDER")
	env := BuildEnvMap()
	if env["VIDEO_PROVIDER"] != "" {
		t.Errorf("期望空字符串，实际 %v", env["VIDEO_PROVIDER"])
	}
}

func TestConfigSetEnvMap_条目数量(t *testing.T) {
	// 对齐 Python _CONFIG_SET_ENV_MAP 的条目数
	if len(configSetEnvMap) < 30 {
		t.Errorf("configSetEnvMap 条目数过少: %d，期望至少 30", len(configSetEnvMap))
	}
}
