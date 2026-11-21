package codebuddy

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// ConfigSuite CodebuddyConfig 校验套件。
type ConfigSuite struct{ suite.Suite }

// TestConfigSuite 套件入口。
func TestConfigSuite(t *testing.T) { suite.Run(t, new(ConfigSuite)) }

// TestFromMapping_最小合法 最小配置通过并填默认值。
func (s *ConfigSuite) TestFromMapping_最小合法() {
	config, err := CodebuddyConfigFromMapping(CodebuddyConfig{}.Mapping())
	s.Require().NoError(err)
	s.Equal("codebuddy", config.CliPath)
	s.Equal(600, config.TurnTimeoutSeconds)
	s.Equal(120, config.InactivityTimeoutSeconds)
	s.Equal(1024, config.EventBufferCapacity)
}

// TestFromMapping_显式空CliPath回默认 空对象走默认。
func (s *ConfigSuite) TestFromMapping_显式空CliPath回默认() {
	config, err := CodebuddyConfigFromMapping(map[string]any{})
	s.Require().NoError(err)
	s.Equal("codebuddy", config.CliPath)
}

// TestFromMapping_未知键拒绝 与 dsh FromMapping 同语义。
func (s *ConfigSuite) TestFromMapping_未知键拒绝() {
	_, err := CodebuddyConfigFromMapping(map[string]any{"nope": 1})
	s.Require().Error(err)
	s.Contains(err.Error(), "unknown codebuddy configuration fields")
}

// TestFromMapping_类型漂移拒绝 字符串字段非字符串报错。
func (s *ConfigSuite) TestFromMapping_类型漂移拒绝() {
	_, err := CodebuddyConfigFromMapping(map[string]any{"model": 42})
	s.Require().Error(err)
	s.Contains(err.Error(), "model must be a string")
}

// TestFromMapping_数值字段类型漂移拒绝 数值字段非数字报错。
func (s *ConfigSuite) TestFromMapping_数值字段类型漂移拒绝() {
	_, err := CodebuddyConfigFromMapping(map[string]any{"turn_timeout_seconds": "x"})
	s.Require().Error(err)
	s.Contains(err.Error(), "turn_timeout_seconds must be a number")
}

// TestFromMapping_env类型漂移拒绝 env 非字符串值报错。
func (s *ConfigSuite) TestFromMapping_env类型漂移拒绝() {
	_, err := CodebuddyConfigFromMapping(map[string]any{"env": map[string]any{"K": 1}})
	s.Require().Error(err)
	s.Contains(err.Error(), "env must map strings to strings")
}

// TestFromMapping_往返 Mapping 导出后可原样重建。
func (s *ConfigSuite) TestFromMapping_往返() {
	original := CodebuddyConfig{
		CliPath: "/usr/local/bin/codebuddy", Model: "hy4flash-free",
		PermissionMode: "default", Cwd: "/tmp", McpConfig: "{}",
		Env:                      map[string]string{"FOO": "bar"},
		EventBufferCapacity:      64,
		TurnTimeoutSeconds:       30,
		InactivityTimeoutSeconds: 5,
	}
	config, err := CodebuddyConfigFromMapping(original.Mapping())
	s.Require().NoError(err)
	s.Equal(original, config)
}

// TestNewCodebuddyConfig_非法负数 提供负数超时报错。
func (s *ConfigSuite) TestNewCodebuddyConfig_非法负数() {
	_, err := NewCodebuddyConfig(CodebuddyConfig{TurnTimeoutSeconds: -1})
	s.Require().Error(err)
	_, err = NewCodebuddyConfig(CodebuddyConfig{InactivityTimeoutSeconds: -1})
	s.Require().Error(err)
	_, err = NewCodebuddyConfig(CodebuddyConfig{EventBufferCapacity: -1})
	s.Require().Error(err)
}
