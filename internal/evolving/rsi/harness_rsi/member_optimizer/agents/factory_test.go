// factory_test.go 对齐 Python factory.py 的可测路径。
// Python: agents/factory.py。

package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	harnessschema "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
)

// TestPackageWriteAllowed_声明路径 对齐 Python package_write_allowed 非 repair 分支。
func TestPackageWriteAllowed_声明路径(t *testing.T) {
	declared := []string{"prompt_sections/identity.md", "skills"}
	require.True(t, PackageWriteAllowed("prompt_sections/identity.md", declared, false))
	require.True(t, PackageWriteAllowed("skills/my_skill/SKILL.md", declared, false))
	require.False(t, PackageWriteAllowed("tools/x.py", declared, false))
	// 绝对路径 / 点开头 / .. / 受保护文件名 / 受保护顶层目录
	require.False(t, PackageWriteAllowed("/abs/path", declared, false))
	require.False(t, PackageWriteAllowed(".hidden", declared, false))
	require.False(t, PackageWriteAllowed("../escape", declared, false))
	require.False(t, PackageWriteAllowed("plan.yaml", declared, false))
	require.False(t, PackageWriteAllowed("current_harnesses/solver/identity.md", declared, false))
	require.False(t, PackageWriteAllowed("evaluation/x", declared, false))
}

// TestPackageWriteAllowed_repair模式 对齐 repair 分支。
func TestPackageWriteAllowed_repair模式(t *testing.T) {
	// repair 放宽到五顶层目录 / 三顶层文件 / 四后缀
	require.True(t, PackageWriteAllowed("prompt_sections/identity.md", nil, true))
	require.True(t, PackageWriteAllowed("harness.yaml", nil, true))
	require.True(t, PackageWriteAllowed("tools/x.py", nil, true))
	// Python repair 分支：suffix in {".yaml",".yml",".json",".py"} —— txt 不允许
	require.False(t, PackageWriteAllowed("notes.txt", nil, true))
	require.False(t, PackageWriteAllowed("plan.yaml", nil, true)) // 受保护名单仍拒绝
}

// TestRenderPrompt_占位缺失报错 对齐 _render_prompt KeyError。
func TestRenderPrompt_占位缺失报错(t *testing.T) {
	out, err := RenderPrompt("role_attribution.md", map[string]string{})
	require.NoError(t, err) // role_attribution.md 无 {{PLACEHOLDER}} 占位
	require.NotEmpty(t, out)

	// mechanism_attribution.md 含三组占位；缺失任一报错
	_, err = RenderPrompt("mechanism_attribution.md", map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing prompt placeholder value:")
}

// TestRenderPrompt_枚举注入 对照 create_mechanism_attribution_agent 的占位值格式。
func TestRenderPrompt_枚举注入(t *testing.T) {
	out, err := RenderPrompt("mechanism_attribution.md", map[string]string{
		"MECHANISM_TYPE_VALUES":       joinBulletValues(harnessschema2MechanismValues()),
		"FAILURE_SIGNATURE_VALUES":    joinBulletValues(harnessschema2FailureValues()),
		"OPTIMIZATION_SURFACE_VALUES": joinBulletValues(harnessschema2SurfaceValues()),
	})
	require.NoError(t, err)
	require.Contains(t, out, "- prompt")
	require.NotContains(t, out, "{{MECHANISM_TYPE_VALUES}}")
}

// TestLoadMemberOptimizerModel_错误路径 对齐三处 RuntimeError。
func TestLoadMemberOptimizerModel_错误路径(t *testing.T) {
	_, err := LoadMemberOptimizerModel("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_config_ref is required for member optimization")

	_, err = LoadMemberOptimizerModel("/nonexistent/ref.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_config_ref not found")

	dir := t.TempDir()
	// 非 dict 顶层（JSON 数组）
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte(`[1,2]`), 0o644)
	_, err = LoadMemberOptimizerModel(path)
	require.Error(t, err)
}

// TestLoadMemberOptimizerModel_成功路径 完整配置载入预算化模型。
func TestLoadMemberOptimizerModel_成功路径(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.yaml")
	os.WriteFile(path, []byte(`model:
  model_client_config:
    client_provider: OpenAI
    client_id: c1
    api_key: k
    api_base: https://api.example.com
    max_retries: 5
  model_request_config:
    max_tokens: null
`), 0o644)
	model, err := LoadMemberOptimizerModel(path)
	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, 0, model.ClientConfig.MaxRetries) // without_inner_sdk_retries
}

// TestFormatActionDefinitions 对照 _format_action_definitions 输出格式。
func TestFormatActionDefinitions(t *testing.T) {
	// 空定义返回空串
	require.Empty(t, FormatActionDefinitions(nil))
	one := []harnessschema.ActionDefinition{{
		Group: "prompt", Operation: "edit", Purpose: "adjust identity",
		Function: "edit_prompt_section",
	}}
	out := FormatActionDefinitions(one)
	require.Contains(t, out, "## Action Definitions")
	require.Contains(t, out, "- prompt/edit: adjust identity (function=edit_prompt_section)")
}

// TestOptimizerAgentResources_句柄释放 无操作 ID 时 Release 安全。
func TestOptimizerAgentResources_句柄释放(t *testing.T) {
	handle, err := OptimizerAgentResources(struct{}{})
	require.NoError(t, err)
	require.NoError(t, handle.Release()) // operationID 为空：no-op
}

// ───────────────────────── 枚举注入辅助（对齐 factory.py:513-523） ─────────────────────────

func harnessschema2MechanismValues() []string {
	return memberoptimizer.MechanismTypeValuesExcludingRail()
}

func harnessschema2FailureValues() []string {
	return memberoptimizer.FailureSignatureValuesExcludingConfigOrRailMismatch()
}

func harnessschema2SurfaceValues() []string {
	return memberoptimizer.OptimizationSurfaceValuesExcludingRail()
}
