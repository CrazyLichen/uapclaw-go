package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func intPtr(i int) *int         { return &i }
func f64Ptr(f float64) *float64 { return &f }
func strP(s string) *string     { return &s }

func validParams() DeploymentParams {
	return DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: "u", Provider: "openai",
	}
}

// TestDeployment_Validate 对齐 core/deployment.py:56 __post_init__ 逐分支。
func TestDeployment_Validate(t *testing.T) {
	// 必填字段非空（py:57-68）
	_, err := NewDeployment(DeploymentParams{Provider: "openai"}) // 缺 model_name
	require.ErrorContains(t, err, "must be a non-empty string")
	_, err = NewDeployment(DeploymentParams{ModelName: "m", APIBase: "u", Provider: "p"})
	require.ErrorContains(t, err, "Deployment.api_key")
	_, err = NewDeployment(DeploymentParams{ModelName: "m", APIKey: "k"})
	require.ErrorContains(t, err, "Deployment.provider")

	// 全缺省时 provider 默认 openai（py:40）、id 自动生成 uuid[:8]（py:28）
	d, err := NewDeployment(validParams())
	require.NoError(t, err)
	require.Equal(t, "openai", d.Provider)
	require.Len(t, d.ID, 8)

	// tpm/rpm 拒绝非正整数（py:70-80）
	bad := validParams()
	bad.TPM = intPtr(0)
	_, err = NewDeployment(bad)
	require.ErrorContains(t, err, "must be a positive int")
	bad = validParams()
	bad.RPM = intPtr(-1)
	_, err = NewDeployment(bad)
	require.ErrorContains(t, err, "must be a positive int")

	// consecutive_failures 拒绝负值（py:82-92）
	bad = validParams()
	bad.ConsecutiveFailures = -1
	_, err = NewDeployment(bad)
	require.ErrorContains(t, err, "must be >= 0")

	// timeout 拒绝非正数（py:94-103）
	bad = validParams()
	bad.Timeout = f64Ptr(0)
	_, err = NewDeployment(bad)
	require.ErrorContains(t, err, "must be a positive number")

	// bedrock 空 api_key 豁免分支不移植（D-2）：空 key 一律报错
	bedrock := DeploymentParams{ModelName: "m", APIBase: "u", Provider: "aws-bedrock"}
	_, err = NewDeployment(bedrock)
	require.ErrorContains(t, err, "api_key")
}

// TestDeploymentStatus 验证枚举字符串值（对齐 py .value）。
func TestDeploymentStatus(t *testing.T) {
	require.Equal(t, "healthy", DeploymentStatusHealthy.String())
	require.Equal(t, "cooldown", DeploymentStatusCooldown.String())
	s, err := DeploymentStatusFrom("cooldown")
	require.NoError(t, err)
	require.Equal(t, DeploymentStatusCooldown, s)
	_, err = DeploymentStatusFrom("bogus")
	require.Error(t, err)
}

// TestDeploymentToFromDict 验证 to_dict/from_dict 往返（py:105/128）。
func TestDeploymentToFromDict(t *testing.T) {
	p := validParams()
	p.ID = "abc12345"
	p.ModelID = "mid"
	p.TPM = intPtr(100)
	p.Timeout = f64Ptr(3.5)
	p.CooldownUntil = f64Ptr(123.0)
	p.Tags = []string{"tag-a"}
	p.EndpointProfile = strP("dashscope")
	p.FallbackTag = strP("qwen-plus")
	p.RequestDefaults = map[string]any{"temperature": 0.7}
	d, err := NewDeployment(p)
	require.NoError(t, err)

	dict := d.ToDict()
	require.Equal(t, "healthy", dict["status"])
	require.Equal(t, "qwen-plus", *dict["fallback_tag"].(*string))

	back, err := DeploymentFromDict(dict)
	require.NoError(t, err)
	require.Equal(t, d.ID, back.ID)
	require.Equal(t, "mid", back.ModelID)
	require.Equal(t, 100, *back.TPM)
	require.Equal(t, 3.5, *back.Timeout)
	require.Equal(t, 123.0, *back.CooldownUntil)
	require.Equal(t, []string{"tag-a"}, back.Tags)
	require.Equal(t, "qwen-plus", *back.FallbackTag)
	require.Equal(t, 0.7, back.RequestDefaults["temperature"])

	// from_dict 不修改调用方传入的字典（py:131 data = dict(data)）
	require.Equal(t, "healthy", dict["status"])
}
