package schema

// 本文件承载权限配置段的纯数据类型。
//
// 归属说明（2026-11-27，15.5 实施时的依赖重构）：
// 这两个类型原定义在 internal/agentcore/harness/security/models.go，导致
// 依赖环 schema → security → resources → schema（security 的 tiered_policy
// 读 resources 的 builtin_rules.yaml，而 resources 的 extension_loader 需要
// schema 的 AgentTemplateSpec）。
//
// 二者是纯数据（无方法、无行为、零包内依赖），因此下沉到 schema 包——
// schema 是纯数据层，本就不该反向依赖 security。
//
// Python 对应：openjiuwen/harness/security/permission_engine/models.py:135
// PermissionsSection 是 TypedDict(total=False)，同为纯数据声明。

// ──────────────────────────── 结构体 ────────────────────────────

// ApprovalOverrideEntry 用户/CLI 覆盖条目
//
// match_type 表示 pattern 作用在哪种输入上（如 path 对路径参数、command 对命令文本）；
// pattern 则是该维度上的具体表达式（re:… 正则或路径/通配写法）。
//
// Python: permission_engine/models.py 的 approval_overrides 元素
type ApprovalOverrideEntry struct {
	// ID 覆盖条目标识
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Tools 适用的工具列表
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	// MatchType 匹配类型（如 path、command）
	MatchType string `json:"match_type,omitempty" yaml:"match_type,omitempty"`
	// Pattern 匹配模式（正则或通配）
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	// Action 执行动作（allow/ask/deny）
	Action string `json:"action,omitempty" yaml:"action,omitempty"`
}

// PermissionsSection 权限配置段。
// Python: permission_engine/models.py:135 PermissionsSection(TypedDict, total=False)
//
// 与 agent YAML 中 permissions: 段落常见字段对齐。
// schema（可选）：建议写 tiered_policy 等，便于人类阅读或与旧文档对齐；
// 引擎不根据该字段切换实现路径。
type PermissionsSection struct {
	// Enabled 是否启用权限系统
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Schema 权限策略模式名称
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
	// Defaults 默认权限策略
	Defaults map[string]any `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	// Tools 工具级权限策略
	Tools map[string]any `json:"tools,omitempty" yaml:"tools,omitempty"`
	// Rules 权限规则列表
	Rules []map[string]any `json:"rules,omitempty" yaml:"rules,omitempty"`
	// ApprovalOverrides 用户/CLI 覆盖条目
	ApprovalOverrides []ApprovalOverrideEntry `json:"approval_overrides,omitempty" yaml:"approval_overrides,omitempty"`
	// ExternalDirectory 外部目录权限映射
	ExternalDirectory map[string]string `json:"external_directory,omitempty" yaml:"external_directory,omitempty"`
}
