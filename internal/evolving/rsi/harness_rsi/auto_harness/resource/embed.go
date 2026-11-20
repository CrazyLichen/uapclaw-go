package resource

import (
	_ "embed"
)

// 出厂资源文件（与 Python auto_harness/resources/ 逐字一致）
var (
	//go:embed config.yaml
	configTemplate string
	//go:embed ci_gate.yaml
	ciGateYAML []byte
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultConfigTemplate 返回出厂配置模板文本（bootstrap 复制为用户起始配置）。
//
// 对齐 Python schema.py `_CONFIG_TEMPLATE.read_text(encoding="utf-8")`：
// Python 从包目录磁盘读取，Go 以 go:embed 随二进制携带、运行时读出，语义等价。
func DefaultConfigTemplate() string {
	return configTemplate
}

// CIGateYAML 返回 CI 门禁规则文件字节内容。
//
// 对齐 Python ci_gate_runner.py `_DEFAULT_YAML` 指向的
// resources/ci_gate.yaml 磁盘读取；Go 以 embed 提供。
func CIGateYAML() []byte {
	return ciGateYAML
}
