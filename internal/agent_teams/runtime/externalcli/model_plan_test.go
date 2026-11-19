package externalcli

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ModelPlanSuite plan_external_model 全规则测试。
// Python 对照：external_model.py 各规则 + test_external_model.py 语义。
type ModelPlanSuite struct{ suite.Suite }

func TestModelPlanSuite(t *testing.T) {
	suite.Run(t, new(ModelPlanSuite))
}

func (s *ModelPlanSuite) entry() *schema.ExternalCliAgentSpec {
	return &schema.ExternalCliAgentSpec{
		CLIAgent: "dsh",
		BuiltinModels: []schema.ExternalCliBuiltinModel{
			{Name: "fast", Efforts: []string{"low", "high"}, DefaultEffort: "low"},
		},
	}
}

func (s *ModelPlanSuite) pool(entries ...models.ModelPoolEntry) *fakePool {
	return &fakePool{entries: entries}
}

func (s *ModelPlanSuite) Test互斥() {
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", ModelHint: "m", BuiltinHint: "fast"},
		s.entry(), nil)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "mutually exclusive")
}

func (s *ModelPlanSuite) TestEffort单独拒绝() {
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", EffortHint: "low"}, s.entry(), nil)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "'effort' requires 'builtin_model'")
}

func (s *ModelPlanSuite) Test原生Agent拒绝Builtin() {
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "", BuiltinHint: "fast"}, nil, nil)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "unavailable for native agents")
}

func (s *ModelPlanSuite) Test原生Agent全空双Nil() {
	plan, err := PlanExternalModel(PlanArgs{CLIAgent: "", ModelHint: "x"}, nil, nil)
	s.Require().NoError(err)
	s.Nil(plan.Primary)
	s.Empty(plan.Display)
}

func (s *ModelPlanSuite) TestPool命中() {
	pool := s.pool(models.ModelPoolEntry{
		ModelName: "gpt-x", APIProvider: "openai",
		APIBaseURL: "https://gw.example/v1", APIKey: "sk-1",
	})
	plan, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", ModelHint: "gpt-x"}, s.entry(), pool)
	s.Require().NoError(err)
	s.Require().NotNil(plan.Primary)
	s.Equal("gpt-x", plan.Primary.Model)
	s.Equal("https://gw.example/v1", plan.Primary.APIBase) // 端点配置
	s.Equal("sk-1", plan.Primary.APIKey)
	s.Equal("gpt-x", plan.Display)
}

func (s *ModelPlanSuite) TestPool未命中三档文案() {
	pool := s.pool(models.ModelPoolEntry{ModelName: "other", APIProvider: "openai"})
	// 目录内名字 → 重定向到 builtin_model（三档第一档）
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", ModelHint: "fast"}, s.entry(), pool)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "pass it as 'builtin_model' instead")
	// 未知名字 → 列池再列目录（第二档）
	_, err = PlanExternalModel(PlanArgs{CLIAgent: "dsh", ModelHint: "nope"}, s.entry(), pool)
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "is not offered by the team model pool")
	s.Contains(pe.Message, "the team model pool offers: other")
	s.Contains(pe.Message, "Built-in models of this cli_agent")
	// 无目录条目 → 只列池（第三档）
	_, err = PlanExternalModel(PlanArgs{CLIAgent: "dsh", ModelHint: "nope"}, nil, pool)
	s.Require().ErrorAs(err, &pe)
	s.NotContains(pe.Message, "Built-in models")
}

func (s *ModelPlanSuite) TestPoolProvider过滤不兼容() {
	// claude 的 provider 过滤（IsAnthropicProvider）拒绝非 Anthropic 条目
	// → "is unavailable or incompatible"（dsh 未注册后端时无过滤器，走 nil 直通）
	pool := s.pool(models.ModelPoolEntry{ModelName: "m1", APIProvider: "unsuitable"})
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "claude", ModelHint: "m1"}, s.entry(), pool)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "is unavailable or incompatible")
}

func (s *ModelPlanSuite) TestBuiltin命中() {
	plan, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", BuiltinHint: "fast", EffortHint: "high"},
		s.entry(), nil)
	s.Require().NoError(err)
	s.Require().NotNil(plan.Primary)
	s.Equal("fast", plan.Primary.Model)
	s.Equal("high", plan.Primary.Effort) // 无端点仅 effort
	s.Empty(plan.Primary.APIBase)
	s.Equal("fast", plan.Display)
}

func (s *ModelPlanSuite) TestBuiltinEffort缺省取Default() {
	plan, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", BuiltinHint: "fast"}, s.entry(), nil)
	s.Require().NoError(err)
	s.Equal("low", plan.Primary.Effort) // effort_hint or model.default_effort
}

func (s *ModelPlanSuite) TestBuiltinEffort不支持() {
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", BuiltinHint: "fast", EffortHint: "ultra"},
		s.entry(), nil)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "supported efforts: low, high")
}

func (s *ModelPlanSuite) TestBuiltin不在目录() {
	_, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh", BuiltinHint: "ghost"}, s.entry(), nil)
	var pe *PlanError
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "this cli_agent offers: fast")
	// 无目录 → declares no builtin_models
	_, err = PlanExternalModel(PlanArgs{CLIAgent: "dsh", BuiltinHint: "ghost"}, nil, nil)
	s.Require().ErrorAs(err, &pe)
	s.Contains(pe.Message, "declares no builtin_models")
}

func (s *ModelPlanSuite) Test全空双Nil() {
	plan, err := PlanExternalModel(PlanArgs{CLIAgent: "dsh"}, s.entry(), nil)
	s.Require().NoError(err)
	s.Nil(plan.Primary)
	s.Empty(plan.Display)
}

// fakePool poolSource 测试替身。
type fakePool struct{ entries []models.ModelPoolEntry }

func (f *fakePool) GetModelPool() []models.ModelPoolEntry { return f.entries }
