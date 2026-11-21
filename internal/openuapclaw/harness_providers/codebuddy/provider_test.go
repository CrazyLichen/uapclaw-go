package codebuddy

import (
	"testing"

	"github.com/stretchr/testify/suite"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// ProviderSuite CodebuddyProvider 套件。
type ProviderSuite struct{ suite.Suite }

// TestProviderSuite 套件入口。
func TestProviderSuite(t *testing.T) { suite.Run(t, new(ProviderSuite)) }

// TestCreate_经FromMapping Create 走配置校验并返回未启动 harness。
func (s *ProviderSuite) TestCreate_经FromMapping() {
	provider := &CodebuddyProvider{}
	harness, err := provider.Create(CodebuddyConfig{}.Mapping())
	s.Require().NoError(err)
	s.Equal("codebuddy", harness.Card().Name)
	s.Equal(harnessprotocol.HarnessStateTerminated, harness.State())
}

// TestCreate_非法配置拒绝 未知键报错。
func (s *ProviderSuite) TestCreate_非法配置拒绝() {
	provider := &CodebuddyProvider{}
	_, err := provider.Create(harnessprotocol.JsonObject{"nope": 1})
	s.Require().Error(err)
}

// TestModelConfig_映射与fallback拒绝 effort/fallback 显式拒绝。
func (s *ProviderSuite) TestModelConfig_映射与fallback拒绝() {
	provider := &CodebuddyProvider{}
	model := "hy4flash-free"
	values, err := provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model}, nil)
	s.Require().NoError(err)
	s.Equal("hy4flash-free", values["model"])

	_, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model, Effort: strPtr("high")}, nil)
	s.Require().Error(err)
	_, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model}, &harnessprotocol.ModelChoice{})
	s.Require().Error(err)
}

// TestRegister_工厂可取 init 注册（RegisterBuiltinProvider，D-15.22-b 收编）经 ResolveProvider 可取用。
func (s *ProviderSuite) TestRegister_工厂可取() {
	resolved, err := providers.ResolveProvider("codebuddy")
	s.Require().NoError(err)
	s.Equal("codebuddy", resolved.Card().Name)
}
