package resource

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultConfigTemplate_关键段存在 验证模板含 git/gitcode/budget 等段与 token env 名
func TestDefaultConfigTemplate_关键段存在(t *testing.T) {
	tpl := DefaultConfigTemplate()
	for _, want := range []string{
		"git:", "gitcode:", "budget:", "ci_gate:", "fix_loop:", "agent:", "extensions:",
		"GITCODE_ACCESS_TOKEN", "develop-auto-harness",
	} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("出厂配置模板缺少关键内容 %q", want)
		}
	}
}

// TestDefaultConfigTemplate_可解析 验证模板是合法 YAML
func TestDefaultConfigTemplate_可解析(t *testing.T) {
	var data map[string]any
	if err := yaml.Unmarshal([]byte(DefaultConfigTemplate()), &data); err != nil {
		t.Fatalf("出厂配置模板不是合法 YAML: %v", err)
	}
}

// TestCIGateYAML_可解析 验证 CI 门禁规则是合法 YAML 且含 ci_gates 门禁列表
func TestCIGateYAML_可解析(t *testing.T) {
	raw := CIGateYAML()
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		t.Fatalf("ci_gate.yaml 不是合法 YAML: %v", err)
	}
	gates, ok := data["ci_gates"].([]any)
	if !ok || len(gates) == 0 {
		t.Fatalf("ci_gate.yaml 缺少 ci_gates 门禁列表: %v", data["ci_gates"])
	}
	if !strings.Contains(string(raw), "IMMUTABLE") {
		t.Fatalf("ci_gate.yaml 缺少 IMMUTABLE 保护标注")
	}
}
