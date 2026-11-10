package permissions

import (
	"strings"
	"testing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// autoFacts 构造 auto_decision 测试用的 ToolDecisionFacts。
func autoFacts(opts func(*ToolDecisionFacts)) ToolDecisionFacts {
	facts := ToolDecisionFacts{
		Capability: ToolCapability{
			ToolName:          "read_file",
			Category:          "path",
			OperationFamily:   "workspace_read",
			RiskTier:          "low",
			StaticSideEffects: map[string]struct{}{},
			FactsSource:       "host_static",
		},
		UntrustedArgs:        map[string]any{},
		ArgumentsValidObject: true,
		AccessesKnown:        true,
	}
	if opts != nil {
		opts(&facts)
	}
	return facts
}

// withEffects 构造副作用集合。
func withEffects(names ...string) map[string]struct{} {
	effects := make(map[string]struct{}, len(names))
	for _, name := range names {
		effects[name] = struct{}{}
	}
	return effects
}

// ──────────────────────────── 用例：DeterministicGuardRoute ───────────────────────────────

// TestDeterministicGuardRoute_searchSkill敏感查询 验证密钥特征硬拒绝。
func TestDeterministicGuardRoute_searchSkill敏感查询(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = SearchSkillTool
		f.UntrustedArgs = map[string]any{"query": "api_key = abc123"}
	})
	got := DeterministicGuardRoute(facts)
	if got == nil {
		t.Fatal("sensitive query should be denied")
	}
	if got.Level != AutoDecisionDenyLevel || got.Reason != "search_skill_sensitive_query" {
		t.Errorf("got %+v", got)
	}
	if got.Source != "deterministic_guard" {
		t.Errorf("source = %q", got.Source)
	}
}

// TestDeterministicGuardRoute_searchSkill正常查询 验证无密钥时放行。
func TestDeterministicGuardRoute_searchSkill正常查询(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = SearchSkillTool
		f.UntrustedArgs = map[string]any{"query": "how to sort a list"}
	})
	if got := DeterministicGuardRoute(facts); got != nil {
		t.Errorf("normal query should pass, got %+v", got)
	}
}

// TestDeterministicGuardRoute_非searchSkill 验证其他工具返回 nil。
func TestDeterministicGuardRoute_非searchSkill(t *testing.T) {
	if got := DeterministicGuardRoute(autoFacts(nil)); got != nil {
		t.Errorf("got %+v", got)
	}
}

// TestDeterministicGuardRoute_非字符串query 验证 query 非字符串时放行。
func TestDeterministicGuardRoute_非字符串query(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = SearchSkillTool
		f.UntrustedArgs = map[string]any{"query": 42}
	})
	if got := DeterministicGuardRoute(facts); got != nil {
		t.Errorf("got %+v", got)
	}
}

// ──────────────────────────── 用例：GenericMCPEgressEvidence ───────────────────────────────

// TestGenericMCPEgressEvidence_上传字段 验证 generic_mcp_upload_like_payload。
func TestGenericMCPEgressEvidence_上传字段(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"file_path": "/etc/passwd"}
	})
	got := GenericMCPEgressEvidence(facts)
	if !containsString(got, "generic_mcp_upload_like_payload") {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_密钥载荷 验证 generic_mcp_sensitive_egress。
func TestGenericMCPEgressEvidence_密钥载荷(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"payload": "api_key=secret123"}
	})
	got := GenericMCPEgressEvidence(facts)
	if !containsString(got, "generic_mcp_sensitive_egress") {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_external_send副作用 验证副作用触发上传标签。
func TestGenericMCPEgressEvidence_external_send副作用(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.Capability.StaticSideEffects = withEffects("external_send")
	})
	got := GenericMCPEgressEvidence(facts)
	if !containsString(got, "generic_mcp_upload_like_payload") {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_无风险 验证干净载荷返回空。
func TestGenericMCPEgressEvidence_无风险(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"query": "hello"}
	})
	got := GenericMCPEgressEvidence(facts)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_去重 验证重复标签只出现一次。
func TestGenericMCPEgressEvidence_去重(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.Capability.StaticSideEffects = withEffects("external_send")
		f.UntrustedArgs = map[string]any{"file": "/tmp/a"}
	})
	got := GenericMCPEgressEvidence(facts)
	count := 0
	for _, item := range got {
		if item == "generic_mcp_upload_like_payload" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("should dedupe, got %v", got)
	}
}

// ──────────────────────────── 用例：DeterministicDomainRoute ───────────────────────────────

// TestDeterministicDomainRoute_mcp 验证 mcp 一律 manual_only。
func TestDeterministicDomainRoute_mcp(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
	})
	got := DeterministicDomainRoute(facts, nil, nil, nil)
	if got == nil || got.Reason != "domain_policy_generic_mcp_manual" {
		t.Errorf("got %+v", got)
	}
	if got.Source != "manual_only" {
		t.Errorf("source = %q", got.Source)
	}
}

// TestDeterministicDomainRoute_cron各操作 验证 cron 路由表。
func TestDeterministicDomainRoute_cron各操作(t *testing.T) {
	cases := []struct {
		op     string
		reason string
		source string
	}{
		{"preview", "domain_policy_cron_preview", "semantic_reviewer"},
		{"create", "domain_policy_cron_mutation_manual", "semantic_reviewer"},
		{"delete", "domain_policy_cron_destructive", "semantic_reviewer"},
		{"toggle", "domain_policy_cron_destructive", "semantic_reviewer"},
		{"unknown_op", "domain_policy_cron_unknown_payload", "manual_only"},
	}
	for _, c := range cases {
		facts := autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.Category = "cron"
			f.UntrustedArgs = map[string]any{"operation": c.op}
		})
		got := DeterministicDomainRoute(facts, nil, nil, nil)
		if got == nil {
			t.Fatalf("op=%s should route", c.op)
		}
		if got.Reason != c.reason || got.Source != c.source {
			t.Errorf("op=%s got (%q,%q), want (%q,%q)", c.op, got.Reason, got.Source, c.reason, c.source)
		}
	}
}

// TestDeterministicDomainRoute_memory写入缺字段 验证三个 missing 原因。
func TestDeterministicDomainRoute_memory写入缺字段(t *testing.T) {
	cases := []struct {
		name   string
		args   map[string]any
		reason string
		source string
	}{
		{"缺namespace", map[string]any{}, "domain_policy_memory_namespace_missing", "manual_only"},
		{"缺scope", map[string]any{"namespace": "ns"}, "domain_policy_memory_scope_missing", "manual_only"},
		{"缺retention", map[string]any{"namespace": "ns", "scope": "user"}, "domain_policy_memory_retention_missing", "manual_only"},
		{"齐全", map[string]any{"namespace": "ns", "scope": "user", "retention": "30d"}, "domain_policy_memory_bounded_write", "semantic_reviewer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.Category = "memory"
				f.Capability.OperationFamily = "memory_write"
				f.UntrustedArgs = c.args
			})
			got := DeterministicDomainRoute(facts, nil, nil, nil)
			if got == nil || got.Reason != c.reason || got.Source != c.source {
				t.Errorf("got %+v, want (%q,%q)", got, c.reason, c.source)
			}
		})
	}
}

// TestDeterministicDomainRoute_memory非写入 验证非 memory_write 返回 nil。
func TestDeterministicDomainRoute_memory非写入(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "memory"
		f.Capability.OperationFamily = "memory_read"
	})
	if got := DeterministicDomainRoute(facts, nil, nil, nil); got != nil {
		t.Errorf("got %+v", got)
	}
}

// TestDeterministicDomainRoute_skill 验证只读与变更分流。
func TestDeterministicDomainRoute_skill(t *testing.T) {
	cases := []struct {
		op     string
		reason string
	}{
		{"read", "domain_policy_skill_readonly"},
		{"search", "domain_policy_skill_readonly"},
		{"list", "domain_policy_skill_readonly"},
		{"install", "domain_policy_skill_change"},
		{"uninstall", "domain_policy_skill_change"},
		{"weird", "domain_policy_skill_unknown_payload"},
	}
	for _, c := range cases {
		facts := autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.Category = "skill"
			f.UntrustedArgs = map[string]any{"operation": c.op}
		})
		got := DeterministicDomainRoute(facts, nil, nil, nil)
		if got == nil || got.Reason != c.reason {
			t.Errorf("op=%s got %+v, want %q", c.op, got, c.reason)
		}
	}
}

// TestDeterministicDomainRoute_未知类别 验证返回 nil。
func TestDeterministicDomainRoute_未知类别(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "unknown_category"
	})
	if got := DeterministicDomainRoute(facts, nil, nil, nil); got != nil {
		t.Errorf("got %+v", got)
	}
}

// TestDeterministicDomainRoute_browser硬拦截 验证 http scheme 被 deny。
func TestDeterministicDomainRoute_browser硬拦截(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.UntrustedArgs = map[string]any{"url": "http://example.com"}
	})
	got := DeterministicDomainRoute(facts, nil, nil, nil)
	if got == nil {
		t.Fatal("http should be handled")
	}
	if got.Level != AutoDecisionDenyLevel {
		t.Errorf("level = %q, want deny", got.Level)
	}
}

// ──────────────────────────── 用例：ToolSearchArgsValid ───────────────────────────────

// TestToolSearchArgsValid_progressive 验证渐进模式的封闭契约。
func TestToolSearchArgsValid_progressive(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		// Python 实测：query 必须存在（args.get("query") 为 None 时 _bounded_text 为 False）
		{"空", map[string]any{}, false},
		{"query", map[string]any{"query": "find tool"}, true},
		{"limit合法但无query", map[string]any{"limit": 20}, false},
		{"limit为1但无query", map[string]any{"limit": 1}, false},
		{"query+limit", map[string]any{"query": "x", "limit": 20}, true},
		{"limit为0", map[string]any{"query": "x", "limit": 0}, false},
		{"limit为21", map[string]any{"query": "x", "limit": 21}, false},
		{"limit为bool", map[string]any{"query": "x", "limit": true}, false},
		{"多余键", map[string]any{"other": 1}, false},
		{"query非串", map[string]any{"query": 42}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToolSearchArgsValid(c.args, true); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestToolSearchArgsValid_非progressive 验证目录模式的契约。
func TestToolSearchArgsValid_非progressive(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		// Python 实测：空 args 与 search 无 query 都返回 True
		// （末行 `operation != "get_schema" or ...` 对非 get_schema 恒真）
		{"空", map[string]any{}, true},
		{"list", map[string]any{"operation": "list"}, true},
		{"search带query", map[string]any{"operation": "search", "query": "x"}, true},
		{"search无query", map[string]any{"operation": "search"}, true},
		{"get_schema带tool", map[string]any{"operation": "get_schema", "tool": "x"}, true},
		{"get_schema无tool", map[string]any{"operation": "get_schema"}, false},
		{"非法operation", map[string]any{"operation": "delete"}, false},
		{"多余键", map[string]any{"operation": "list", "other": 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToolSearchArgsValid(c.args, false); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestToolSearchArgsValid_nil 验证 nil 返回 false。
func TestToolSearchArgsValid_nil(t *testing.T) {
	if ToolSearchArgsValid(nil, true) || ToolSearchArgsValid(nil, false) {
		t.Error("nil args should be false")
	}
}

// ──────────────────────────── 用例：SessionObservationArgsValid ───────────────────────────────

// TestSessionObservationArgsValid_五契约 验证五个封闭契约。
func TestSessionObservationArgsValid_五契约(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		args     map[string]any
		want     bool
		ok       bool
	}{
		{"tool_search", "tool_search", map[string]any{"query": "x"}, true, true},
		{"skill_index", "skill_index", map[string]any{"operation": "list"}, true, true},
		{"list_skill空", "list_skill", map[string]any{}, true, true},
		{"list_skill带query", "list_skill", map[string]any{"query": "x"}, true, true},
		{"get_current_goal空", "get_current_goal", map[string]any{}, true, true},
		{"get_current_goal非空", "get_current_goal", map[string]any{"x": 1}, false, true},
		{"recall合法", "recall_compressed_context", map[string]any{"query": "x", "memory_id": "abc-123"}, true, true},
		{"recall缺query", "recall_compressed_context", map[string]any{"memory_id": "abc"}, false, true},
		{"recall非法id", "recall_compressed_context", map[string]any{"query": "x", "memory_id": "../etc"}, false, true},
		{"recall点", "recall_compressed_context", map[string]any{"query": "x", "memory_id": "."}, false, true},
		{"未知工具", "unknown_tool", map[string]any{}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := SessionObservationArgsValid(c.toolName, c.args)
			if ok != c.ok || (ok && got != c.want) {
				t.Errorf("got (%v,%v), want (%v,%v)", got, ok, c.want, c.ok)
			}
		})
	}
}

// ──────────────────────────── 用例：TerminalLowRiskRoute ───────────────────────────────

// TestTerminalLowRiskRoute_低风险路径读 验证 allow。
func TestTerminalLowRiskRoute_低风险路径读(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "path"
		f.Capability.RiskTier = "low"
	})
	got := TerminalLowRiskRoute(facts)
	if got == nil || got.Level != AutoDecisionAllowLevel {
		t.Errorf("got %+v", got)
	}
	if got.Reason != "terminal_low_risk_allow" || got.Source != "hard_guard" {
		t.Errorf("got %+v", got)
	}
}

// TestTerminalLowRiskRoute_各排除条件 验证六类排除。
func TestTerminalLowRiskRoute_各排除条件(t *testing.T) {
	cases := []struct {
		name string
		opts func(*ToolDecisionFacts)
	}{
		{"有external_paths", func(f *ToolDecisionFacts) { f.ExternalPaths = []string{"/ext"} }},
		{"有write_paths", func(f *ToolDecisionFacts) { f.WritePaths = []string{"/w"} }},
		{"accesses未知", func(f *ToolDecisionFacts) { f.AccessesKnown = false }},
		{"lsp工具", func(f *ToolDecisionFacts) { f.Capability.ToolName = "lsp" }},
		{"非path类别", func(f *ToolDecisionFacts) { f.Capability.Category = "shell" }},
		{"高风险", func(f *ToolDecisionFacts) { f.Capability.RiskTier = "high" }},
		{"有副作用", func(f *ToolDecisionFacts) { f.Capability.StaticSideEffects = withEffects("memory_write") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.Category = "path"
				f.Capability.RiskTier = "low"
				c.opts(f)
			})
			if got := TerminalLowRiskRoute(facts); got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

// TestTerminalLowRiskRoute_硬拒绝时返回nil 验证 guard 优先。
func TestTerminalLowRiskRoute_硬拒绝时返回nil(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = SearchSkillTool
		f.Capability.Category = "path"
		f.UntrustedArgs = map[string]any{"query": "token: abc"}
	})
	if got := TerminalLowRiskRoute(facts); got != nil {
		t.Errorf("guard should win, got %+v", got)
	}
}

// ──────────────────────────── 用例：TerminalInternalRoute ───────────────────────────────

// TestTerminalInternalRoute_规范内部动作 验证 allow。
func TestTerminalInternalRoute_规范内部动作(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "session_list"
		f.Capability.Category = "internal"
		f.Capability.OperationFamily = "session_status"
		f.Capability.RiskTier = "low"
		f.UntrustedArgs = map[string]any{}
	})
	got := TerminalInternalRoute(facts, false, true)
	if got == nil || got.Level != AutoDecisionAllowLevel {
		t.Fatalf("got %+v", got)
	}
	if got.Reason != "canonical_internal_action_allow" || got.Source != "internal" {
		t.Errorf("got %+v", got)
	}
}

// TestTerminalInternalRoute_各排除条件 验证七类排除。
func TestTerminalInternalRoute_各排除条件(t *testing.T) {
	base := func() ToolDecisionFacts {
		return autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.ToolName = "session_list"
			f.Capability.OperationFamily = "session_status"
			f.Capability.RiskTier = "low"
		})
	}
	cases := []struct {
		name   string
		mutate func(*ToolDecisionFacts)
	}{
		{"facts_source非host_static", func(f *ToolDecisionFacts) { f.Capability.FactsSource = "runtime" }},
		{"alias冲突", func(f *ToolDecisionFacts) { f.Capability.AliasConflict = true }},
		{"参数非合法对象", func(f *ToolDecisionFacts) { f.ArgumentsValidObject = false }},
		{"高风险", func(f *ToolDecisionFacts) { f.Capability.RiskTier = "high" }},
		{"高弹性", func(f *ToolDecisionFacts) { f.Capability.HighFlex = true }},
		{"有external_paths", func(f *ToolDecisionFacts) { f.ExternalPaths = []string{"/e"} }},
		{"有write_paths", func(f *ToolDecisionFacts) { f.WritePaths = []string{"/w"} }},
		{"有command", func(f *ToolDecisionFacts) { f.Command = "ls" }},
		{"未知操作族", func(f *ToolDecisionFacts) { f.Capability.OperationFamily = "unknown_family" }},
		{"副作用不匹配", func(f *ToolDecisionFacts) { f.Capability.StaticSideEffects = withEffects("memory_write") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := base()
			c.mutate(&facts)
			if got := TerminalInternalRoute(facts, false, true); got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

// TestTerminalInternalRoute_internal_readonly需绑定校验 验证 readonly_binding_verified。
func TestTerminalInternalRoute_internal_readonly需绑定校验(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "cron_list_jobs"
		f.Capability.OperationFamily = "internal_readonly"
		f.Capability.RiskTier = "low"
	})
	if got := TerminalInternalRoute(facts, false, false); got != nil {
		t.Errorf("unverified readonly should return nil, got %+v", got)
	}
	if got := TerminalInternalRoute(facts, false, true); got == nil {
		t.Error("verified readonly should allow")
	}
}

// TestTerminalInternalRoute_subagent需校验 验证 subagent_runtime_control_verified。
func TestTerminalInternalRoute_subagent需校验(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "subagent_spawn"
		f.Capability.OperationFamily = "subagent_runtime_control"
		f.Capability.RiskTier = "low"
		f.Capability.StaticSideEffects = withEffects("delegation")
	})
	if got := TerminalInternalRoute(facts, false, true); got != nil {
		t.Errorf("unverified subagent should return nil, got %+v", got)
	}
	if got := TerminalInternalRoute(facts, true, true); got == nil {
		t.Error("verified subagent should allow")
	}
}

// ──────────────────────────── 用例：PostPolicyRoute ───────────────────────────────

// TestPostPolicyRoute_非allow直接返回 验证只处理 allow。
func TestPostPolicyRoute_非allow直接返回(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.HighFlex = true
	})
	for _, level := range []string{AutoDecisionAskLevel, AutoDecisionDenyLevel, ""} {
		if got := PostPolicyRoute(facts, level); got != nil {
			t.Errorf("level=%q should return nil, got %+v", level, got)
		}
	}
}

// TestPostPolicyRoute_高副作用转ask 验证高影响副作用。
func TestPostPolicyRoute_高副作用转ask(t *testing.T) {
	for _, effect := range []string{
		"browser_action", "delegation", "device_action", "dynamic_exec",
		"external_send", "memory_write", "schedule", "skill_change",
	} {
		facts := autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.StaticSideEffects = withEffects(effect)
		})
		got := PostPolicyRoute(facts, AutoDecisionAllowLevel)
		if got == nil || got.Reason != "high_effect_tool" {
			t.Errorf("effect=%s got %+v", effect, got)
		}
	}
}

// TestPostPolicyRoute_高弹性转ask 验证 high_flex。
func TestPostPolicyRoute_高弹性转ask(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.HighFlex = true
	})
	got := PostPolicyRoute(facts, AutoDecisionAllowLevel)
	if got == nil || got.Reason != "high_flex_tool" {
		t.Errorf("got %+v", got)
	}
	if got.Source != "post_policy" {
		t.Errorf("source = %q", got.Source)
	}
}

// TestPostPolicyRoute_干净allow放行 验证无高影响时返回 nil。
func TestPostPolicyRoute_干净allow放行(t *testing.T) {
	facts := autoFacts(nil)
	if got := PostPolicyRoute(facts, AutoDecisionAllowLevel); got != nil {
		t.Errorf("got %+v", got)
	}
}

// ──────────────────────────── 私有辅助 ────────────────────────────

// TestBoundedText 验证有界文本判定。
func TestBoundedText(t *testing.T) {
	cases := []struct {
		value      any
		limit      int
		allowEmpty bool
		want       bool
	}{
		{"abc", 10, false, true},
		{"abc", 2, false, false},
		{"", 10, true, true},
		{"", 10, false, false},
		{"  ", 10, false, false},
		{"a\x00b", 10, false, false},
		{42, 10, false, false},
		{nil, 10, false, false},
	}
	for _, c := range cases {
		if got := boundedText(c.value, c.limit, c.allowEmpty); got != c.want {
			t.Errorf("boundedText(%v,%d,%v) = %v, want %v", c.value, c.limit, c.allowEmpty, got, c.want)
		}
	}
}

// TestTextArg 验证按序取第一个非空串。
func TestTextArg(t *testing.T) {
	args := map[string]any{"a": "", "b": "  ", "c": "value", "d": "other"}
	if got := textArg(args, "a", "b", "c", "d"); got != "value" {
		t.Errorf("got %q", got)
	}
	if got := textArg(args, "missing"); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestHasTruthyEgressValue 验证真值判定。
func TestHasTruthyEgressValue(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false}, {false, false}, {true, true},
		{"", false}, {"  ", false}, {"x", true},
		{[]any{}, false}, {[]any{1}, true},
		{map[string]any{}, false}, {map[string]any{"k": 1}, true},
		{42, true},
	}
	for _, c := range cases {
		if got := hasTruthyEgressValue(c.in); got != c.want {
			t.Errorf("hasTruthyEgressValue(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestHasGenericMCPUploadField_嵌套 验证递归查找。
func TestHasGenericMCPUploadField_嵌套(t *testing.T) {
	if !hasGenericMCPUploadField(map[string]any{
		"outer": map[string]any{"file_path": "/a"},
	}) {
		t.Error("nested upload field should be found")
	}
	if !hasGenericMCPUploadField(map[string]any{
		"list": []any{map[string]any{"files": []any{"/a"}}},
	}) {
		t.Error("list-nested upload field should be found")
	}
	if hasGenericMCPUploadField(map[string]any{"file_path": ""}) {
		t.Error("empty value should not count")
	}
	if hasGenericMCPUploadField("plain string") {
		t.Error("plain string should be false")
	}
}

// TestHasSecretLikeEgressPayload_各形态 验证递归与字面量。
func TestHasSecretLikeEgressPayload_各形态(t *testing.T) {
	if !hasSecretLikeEgressPayload(map[string]any{"api_key": "abc"}) {
		t.Error("secret key with value should match")
	}
	if hasSecretLikeEgressPayload(map[string]any{"api_key": ""}) {
		t.Error("empty secret value should not match")
	}
	if !hasSecretLikeEgressPayload("token=abc123") {
		t.Error("secret literal should match")
	}
	if !hasSecretLikeEgressPayload([]any{"sk-abcdefgh1234"}) {
		t.Error("list-nested secret should match")
	}
	if hasSecretLikeEgressPayload("plain text") {
		t.Error("plain text should not match")
	}
	if hasSecretLikeEgressPayload(42) {
		t.Error("number should not match")
	}
}

// TestSameEffectSet 验证集合相等判定。
func TestSameEffectSet(t *testing.T) {
	if !sameEffectSet(withEffects("a", "b"), []string{"a", "b"}) {
		t.Error("should be equal")
	}
	if sameEffectSet(withEffects("a"), []string{"a", "b"}) {
		t.Error("size mismatch should be false")
	}
	if sameEffectSet(withEffects("c"), []string{"a"}) {
		t.Error("content mismatch should be false")
	}
	if !sameEffectSet(nil, nil) {
		t.Error("both empty should be equal")
	}
}

// TestDedupeStrings 验证去重保序。
func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
	if got := dedupeStrings(nil); len(got) != 0 {
		t.Errorf("nil -> %v", got)
	}
}

// TestKeysSubsetOf 验证键子集判定。
func TestKeysSubsetOf(t *testing.T) {
	if !keysSubsetOf(map[string]any{"a": 1}, "a", "b") {
		t.Error("subset should be true")
	}
	if keysSubsetOf(map[string]any{"c": 1}, "a", "b") {
		t.Error("extra key should be false")
	}
	if !keysSubsetOf(map[string]any{}) {
		t.Error("empty should be subset of anything")
	}
}

// TestIsExactInt 验证精确 int 判定。
func TestIsExactInt(t *testing.T) {
	if !isExactInt(42) {
		t.Error("int should be exact")
	}
	for _, v := range []any{int64(42), 42.0, "42", true, nil} {
		if isExactInt(v) {
			t.Errorf("%v (%T) should not be exact int", v, v)
		}
	}
}

// TestDomainOperation_各分类 验证操作分类。
func TestDomainOperation_各分类(t *testing.T) {
	cases := []struct {
		category string
		toolName string
		family   string
		args     map[string]any
		want     string
	}{
		{"browser", "browser_page_action", "", map[string]any{"operation": "read_text"}, "inspect"},
		{"browser", "browser_page_action", "", map[string]any{"operation": "scroll"}, "click"},
		{"browser", "", "", map[string]any{"operation": "cancel_task"}, "control_cancel"},
		{"browser", "", "", map[string]any{"operation": "route_add"}, "route_mutation"},
		{"browser", "", "", map[string]any{"operation": "upload_file"}, "upload"},
		{"browser", "", "", map[string]any{"operation": "download"}, "download"},
		{"browser", "", "", map[string]any{"operation": "screenshot"}, "inspect"},
		{"browser", "", "", map[string]any{"operation": "navigate_to"}, "navigate"},
		{"browser", "", "", map[string]any{"operation": "click_btn"}, "click"},
		{"browser", "", "", map[string]any{"operation": "type_text"}, "type"},
		{"browser", "", "", map[string]any{"operation": "post"}, "external_submit"},
		{"browser", "", "", map[string]any{"operation": "weird"}, "unknown"},
		{"cron", "", "", map[string]any{"operation": "remove_job"}, "delete"},
		{"cron", "", "", map[string]any{"operation": "enable_job"}, "toggle"},
		{"cron", "", "", map[string]any{"operation": "edit_job"}, "update"},
		{"cron", "", "", map[string]any{"operation": "add_job"}, "create"},
		{"cron", "", "", map[string]any{"operation": "run_now"}, "run_now"},
		{"cron", "", "", map[string]any{"operation": "weird"}, "unknown"},
		{"memory", "", "memory_read", map[string]any{}, "read"},
		{"memory", "", "memory_search", map[string]any{}, "search"},
		{"memory", "", "", map[string]any{"operation": "edit_memory"}, "edit"},
		{"memory", "", "", map[string]any{"operation": "remember"}, "write"},
		{"memory", "", "", map[string]any{"operation": "weird"}, "unknown"},
		{"skill", "", "skill_read", map[string]any{}, "read"},
		{"skill", "", "skill_discovery", map[string]any{}, "search"},
		{"skill", "", "", map[string]any{"operation": "install"}, "install"},
		{"skill", "", "", map[string]any{"operation": "weird"}, "unknown"},
	}
	for _, c := range cases {
		facts := autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.Category = c.category
			f.Capability.OperationFamily = c.family
			if c.toolName != "" {
				f.Capability.ToolName = c.toolName
			}
			f.UntrustedArgs = c.args
		})
		if got := domainOperation(facts); got != c.want {
			t.Errorf("(%s,%s,%v) -> %q, want %q", c.category, c.toolName, c.args, got, c.want)
		}
	}
}

// TestDomainOperation_回落到family 验证未知类别返回 operation_family。
func TestDomainOperation_回落到family(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "unknown"
		f.Capability.OperationFamily = "some_family"
	})
	if got := domainOperation(facts); got != "some_family" {
		t.Errorf("got %q", got)
	}
}

// TestClosedInternalArgsValid_internal_readonly 验证各工具契约。
func TestClosedInternalArgsValid_internal_readonly(t *testing.T) {
	network := NetworkScopeFacts{}
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		{"cron_list_jobs空", map[string]any{}, true},
		{"cron_list_jobs非空", map[string]any{"x": 1}, false},
		{"heartbeat_list_jobs空", map[string]any{}, true},
		{"heartbeat_list_jobs_current", map[string]any{"scope": "current"}, true},
		{"heartbeat_list_jobs_非法", map[string]any{"scope": "other"}, false},
		{"convert_timestamp_int", map[string]any{"timestamp": 123}, true},
		{"convert_timestamp_float", map[string]any{"timestamp": 1.5}, true},
		{"convert_timestamp_bool", map[string]any{"timestamp": true}, false},
		{"cron_preview合法", map[string]any{"job_id": "j1", "count": 3}, true},
		{"cron_preview非法count", map[string]any{"job_id": "j1", "count": true}, false},
		{"cron_preview缺job_id", map[string]any{"count": 3}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.OperationFamily = "internal_readonly"
				f.Capability.ToolName = c.name
				f.UntrustedArgs = c.args
			})
			// 用真实工具名重跑
			facts.Capability.ToolName = testToolNameFromCase(c.name)
			if got := closedInternalArgsValid(facts, network); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// testToolNameFromCase 从用例名还原工具名。
func testToolNameFromCase(name string) string {
	switch {
	case len(name) > 8 && name[:8] == "cron_lis":
		return "cron_list_jobs"
	case len(name) > 12 && name[:12] == "heartbeat_li":
		return "heartbeat_list_jobs"
	case len(name) > 13 && name[:13] == "convert_times":
		return "convert_timestamp_to_utc8_time"
	case len(name) > 4 && name[:4] == "cron":
		return "cron_preview_job"
	default:
		return name
	}
}

// TestClosedInternalArgsValid_session_status 验证会话状态契约。
func TestClosedInternalArgsValid_session_status(t *testing.T) {
	network := NetworkScopeFacts{}
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		{"session_list合法", map[string]any{"query": "x", "limit": 5}, true},
		{"session_list非法limit", map[string]any{"limit": 0}, false},
		{"session_list_offset可为0", map[string]any{"offset": 0}, true},
		{"session_list多余键", map[string]any{"other": 1}, false},
		{"session_read缺target", map[string]any{}, false},
		{"session_read合法", map[string]any{"target_session_id": "s1"}, true},
		{"session_message_list合法", map[string]any{"target_session_id": "s1"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toolName := "session_list"
			if len(c.name) > 12 && c.name[:12] == "session_read" {
				toolName = "session_read"
			} else if len(c.name) > 20 && c.name[:20] == "session_message_list" {
				toolName = "session_message_list"
			}
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.OperationFamily = "session_status"
				f.Capability.ToolName = toolName
				f.UntrustedArgs = c.args
			})
			if got := closedInternalArgsValid(facts, network); got != c.want {
				t.Errorf("%s got %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestClosedInternalArgsValid_skill_read 验证技能读取契约。
func TestClosedInternalArgsValid_skill_read(t *testing.T) {
	network := NetworkScopeFacts{}
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		{"合法", map[string]any{"skill_name": "my-skill"}, true},
		{"带SKILL.md", map[string]any{"skill_name": "my-skill", "relative_file_path": "SKILL.md"}, true},
		{"非法路径", map[string]any{"skill_name": "my-skill", "relative_file_path": "other.md"}, false},
		{"含斜杠", map[string]any{"skill_name": "a/b"}, false},
		{"含反斜杠", map[string]any{"skill_name": `a\b`}, false},
		{"空名", map[string]any{"skill_name": "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.OperationFamily = "skill_read"
				f.UntrustedArgs = c.args
			})
			if got := closedInternalArgsValid(facts, network); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestClosedInternalArgsValid_memory_read 验证记忆读取路径白名单。
func TestClosedInternalArgsValid_memory_read(t *testing.T) {
	network := NetworkScopeFacts{}
	cases := []struct {
		path string
		want bool
	}{
		{"memory/USER.md", true},
		{"memory/MEMORY.md", true},
		{"memory/2026-01-15.md", true},
		{"memory/other.md", false},
		{"other/USER.md", false},
		{"/abs/path", false},
	}
	for _, c := range cases {
		facts := autoFacts(func(f *ToolDecisionFacts) {
			f.Capability.OperationFamily = "memory_read"
			f.UntrustedArgs = map[string]any{"path": c.path}
		})
		if got := closedInternalArgsValid(facts, network); got != c.want {
			t.Errorf("path=%q got %v, want %v", c.path, got, c.want)
		}
	}
	// path 非字符串
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.OperationFamily = "memory_read"
		f.UntrustedArgs = map[string]any{"path": 42}
	})
	if closedInternalArgsValid(facts, network) {
		t.Error("non-string path should be false")
	}
}

// TestClosedInternalArgsValid_browser_recall 验证句柄形态。
func TestClosedInternalArgsValid_browser_recall(t *testing.T) {
	validHandle := "0123456789abcdef0123456789abcdef"
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.Capability.OperationFamily = "browser_session_recall"
		f.Capability.ToolName = "browser_snapshot"
		f.UntrustedArgs = map[string]any{"handle": validHandle}
	})
	if !closedInternalArgsValid(facts, NetworkScopeFacts{}) {
		t.Error("valid handle should pass")
	}
	facts.Capability.OperationFamily = "browser_session_observe"
	facts.UntrustedArgs = map[string]any{}
	if !closedInternalArgsValid(facts, NetworkScopeFacts{}) {
		t.Error("observe without uris should pass")
	}
	// observe 带 uri 应拒绝
	if closedInternalArgsValid(facts, NetworkScopeFacts{
		AbsoluteURIs: []AbsoluteURIFacts{{Scheme: "https"}},
	}) {
		t.Error("observe with uris should fail")
	}
}

// TestClosedInternalArgsValid_未知family 验证默认返回 true。
func TestClosedInternalArgsValid_未知family(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.OperationFamily = "some_other_family"
	})
	if !closedInternalArgsValid(facts, NetworkScopeFacts{}) {
		t.Error("unknown family should default true")
	}
}

// TestInternalNetworkScope_剔除不透明字段 验证 query 不参与 URI 扫描。
func TestInternalNetworkScope_剔除不透明字段(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.OperationFamily = "internal_readonly"
		f.Capability.ToolName = "tool_search"
		f.UntrustedArgs = map[string]any{
			"query":  "https://example.com",
			"cursor": "abc",
		}
	})
	got := internalNetworkScope(facts)
	// query 被剔除，不应产生 absolute_uris
	if len(got.AbsoluteURIs) != 0 {
		t.Errorf("query should be opaque, got %v", got.AbsoluteURIs)
	}
}

// TestInternalNetworkScope_subagent字段 验证 subagent 的不透明字段。
func TestInternalNetworkScope_subagent字段(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.OperationFamily = "subagent_runtime_control"
		f.Capability.ToolName = "subagent_spawn"
		f.UntrustedArgs = map[string]any{"task_description": "https://example.com"}
	})
	got := internalNetworkScope(facts)
	if len(got.AbsoluteURIs) != 0 {
		t.Errorf("task_description should be opaque, got %v", got.AbsoluteURIs)
	}
}

// TestInternalNetworkScope_无不透明字段 验证直接走 InspectNetworkScope。
func TestInternalNetworkScope_无不透明字段(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.OperationFamily = "workspace_read"
		f.UntrustedArgs = map[string]any{"url": "https://example.com"}
	})
	got := internalNetworkScope(facts)
	_ = got // 只验证不 panic 且走通用路径
}

// TestAnyURIParseFailure与FileResolution 验证两个谓词。
func TestAnyURIParseFailure与FileResolution(t *testing.T) {
	if !anyURIParseFailure([]AbsoluteURIFacts{{ParseFailureReason: "bad"}}) {
		t.Error("should detect parse failure")
	}
	if anyURIParseFailure([]AbsoluteURIFacts{{}}) {
		t.Error("empty should be false")
	}
	if !anyURIFileResolution([]AbsoluteURIFacts{{FileResolutionReason: "x"}}) {
		t.Error("should detect file resolution")
	}
	if anyURIFileResolution(nil) {
		t.Error("nil should be false")
	}
}

// TestHasSideEffect 验证副作用判定。
func TestHasSideEffect(t *testing.T) {
	if !hasSideEffect(withEffects("a"), "a") {
		t.Error("should find a")
	}
	if hasSideEffect(withEffects("a"), "b") {
		t.Error("should not find b")
	}
	if hasSideEffect(nil, "a") {
		t.Error("nil should be false")
	}
}

// TestBoundedTextList 验证列表有界判定。
func TestBoundedTextList(t *testing.T) {
	if !boundedTextList([]any{"a", "b"}, 10) {
		t.Error("valid list should pass")
	}
	if boundedTextList([]any{"a"}, 0) {
		t.Error("over limit should fail")
	}
	// Python 的 all([]) == True，空列表通过
	if !boundedTextList([]any{}, 10) {
		t.Error("empty list should pass (Python all([]) == True)")
	}
	if boundedTextList("not-a-list", 10) {
		t.Error("non-list should fail")
	}
}

// ──────────────────────────── DeterministicDomainRoute browser 分支 ────────────────────────────

// TestDeterministicDomainRoute_browser各操作 验证 browser 路由表。
func TestDeterministicDomainRoute_browser各操作(t *testing.T) {
	cases := []struct {
		op     string
		reason string
		source string
	}{
		{"upload_file", "domain_policy_browser_file_transfer", "semantic_reviewer"},
		// operation "submit_form" 不在 {post,submit,external_submit} 里，
		// 分类为 unknown → unknown_payload
		{"submit_form", "domain_policy_browser_unknown_payload", "manual_only"},
		// ⚠️ Python 的 browser_reasons 里 "credential_form" 是死键——
		// _domain_operation 没有任何分支会返回 "credential_form"，
		// 故 operation="credential_form" 仍分类为 unknown。Go 照抄保留该键。
		{"cancel", "domain_policy_browser_cancel_control_not_model_authorized", "semantic_reviewer"},
		{"route_add", "domain_policy_browser_route_mutation_not_model_authorized", "semantic_reviewer"},
		{"screenshot", "domain_policy_browser_readonly", "semantic_reviewer"},
		{"click_button", "domain_policy_browser_interactive", "semantic_reviewer"},
		{"type_text", "domain_policy_browser_interactive", "semantic_reviewer"},
		{"download_file", "domain_policy_browser_interactive", "semantic_reviewer"},
		{"weird_op", "domain_policy_browser_unknown_payload", "manual_only"},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			facts := autoFacts(func(f *ToolDecisionFacts) {
				f.Capability.Category = "browser"
				f.Capability.ToolName = "browser_action"
				f.UntrustedArgs = map[string]any{"operation": c.op}
			})
			got := DeterministicDomainRoute(facts, nil, nil, nil)
			if got == nil || got.Reason != c.reason || got.Source != c.source {
				t.Errorf("got %+v, want (%q,%q)", got, c.reason, c.source)
			}
		})
	}
}

// TestDeterministicDomainRoute_browser本地文件 验证 file scheme 的 navigate。
func TestDeterministicDomainRoute_browser本地文件(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.UntrustedArgs = map[string]any{
			"operation": "navigate",
			"url":       "file:///etc/passwd",
		}
	})
	got := DeterministicDomainRoute(facts, nil, nil, nil)
	if got == nil {
		t.Fatal("should route")
	}
	// file:///etc/passwd 可能先被绝对 URI 硬拦截，或被判 local_file_uri
	if got.Level != AutoDecisionAskLevel && got.Level != AutoDecisionDenyLevel {
		t.Errorf("level = %q", got.Level)
	}
}

// TestDeterministicDomainRoute_browser_ftp 验证 ftp scheme。
func TestDeterministicDomainRoute_browser_ftp(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.UntrustedArgs = map[string]any{"operation": "navigate", "url": "ftp://example.com/f"}
	})
	got := DeterministicDomainRoute(facts, nil, nil, nil)
	if got == nil {
		t.Fatal("should route")
	}
	if got.Level != AutoDecisionAskLevel {
		t.Errorf("level = %q, want ask", got.Level)
	}
}

// TestDeterministicDomainRoute_browser_未知scheme 验证非标准 scheme。
func TestDeterministicDomainRoute_browser_未知scheme(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.UntrustedArgs = map[string]any{"operation": "navigate", "url": "gopher://example.com/"}
	})
	got := DeterministicDomainRoute(facts, nil, nil, nil)
	if got == nil {
		t.Fatal("should route")
	}
	if got.Level != AutoDecisionAskLevel {
		t.Errorf("level = %q, want ask", got.Level)
	}
}

// ──────────────────────────── GenericMCPEgressEvidence URI 分支 ────────────────────────────

// TestGenericMCPEgressEvidence_httpURI 验证 http scheme 标签。
func TestGenericMCPEgressEvidence_httpURI(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"url": "http://example.com/a"}
	})
	got := GenericMCPEgressEvidence(facts)
	if !containsString(got, "network_scheme_not_https") {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_ftpURI 验证 ftp 标签。
func TestGenericMCPEgressEvidence_ftpURI(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"url": "ftp://example.com/a"}
	})
	got := GenericMCPEgressEvidence(facts)
	if !containsString(got, "generic_mcp_uri_provider_support_unproven") {
		t.Errorf("got %v", got)
	}
}

// TestGenericMCPEgressEvidence_fileURI 验证 file 标签。
func TestGenericMCPEgressEvidence_fileURI(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"url": "file:///etc/passwd"}
	})
	got := GenericMCPEgressEvidence(facts)
	// file URI 可能被判 hard block 或 generic_mcp_file_uri
	if len(got) == 0 {
		t.Error("file uri should produce evidence")
	}
}

// TestGenericMCPEgressEvidence_非https未知scheme 验证 gopher 标签。
func TestGenericMCPEgressEvidence_非https未知scheme(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "mcp"
		f.UntrustedArgs = map[string]any{"url": "gopher://example.com/a"}
	})
	got := GenericMCPEgressEvidence(facts)
	if len(got) == 0 {
		t.Error("unsupported scheme should produce evidence")
	}
}

// ──────────────────────────── catalogueArgsValid 边界 ────────────────────────────

// TestCatalogueArgsValid_tools列表 验证 tools 数组边界。
func TestCatalogueArgsValid_tools列表(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		{"tools非列表", map[string]any{"operation": "list", "tools": "x"}, false},
		{"tools超8个", map[string]any{"operation": "list", "tools": []any{
			"1", "2", "3", "4", "5", "6", "7", "8", "9"}}, false},
		{"tools含非串", map[string]any{"operation": "list", "tools": []any{42}}, false},
		{"tools合法", map[string]any{"operation": "list", "tools": []any{"a", "b"}}, true},
		{"query列表合法", map[string]any{"operation": "search", "query": []any{"a"}}, true},
		{"query列表超8", map[string]any{"operation": "search", "query": []any{
			"1", "2", "3", "4", "5", "6", "7", "8", "9"}}, false},
		{"call_goal超长", map[string]any{"operation": "list", "call_goal": string(make([]byte, 5000))}, false},
		{"tool超128", map[string]any{"operation": "list", "tool": string(make([]byte, 200))}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := catalogueArgsValid("tool_search", c.args); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestCatalogueArgsValid_skill_index形态 验证非 tool_search 的契约。
func TestCatalogueArgsValid_skill_index形态(t *testing.T) {
	if !catalogueArgsValid("skill_index", map[string]any{"operation": "list"}) {
		t.Error("list should pass")
	}
	if catalogueArgsValid("skill_index", map[string]any{"operation": "delete"}) {
		t.Error("delete should fail")
	}
	if !catalogueArgsValid("skill_index", map[string]any{"operation": "search", "query": "x"}) {
		t.Error("search with query should pass")
	}
	if catalogueArgsValid("skill_index", map[string]any{"operation": "search"}) {
		t.Error("search without query or cursor should fail")
	}
	if !catalogueArgsValid("skill_index", map[string]any{"operation": "search", "cursor": "c"}) {
		t.Error("search with cursor should pass")
	}
}

// ──────────────────────────── TerminalInternalRoute 更多分支 ────────────────────────────

// TestTerminalInternalRoute_观察参数非法 验证 observationArgsValid=false 直接返回 nil。
func TestTerminalInternalRoute_观察参数非法(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "get_current_goal"
		f.Capability.OperationFamily = "internal_readonly"
		f.Capability.RiskTier = "low"
		f.UntrustedArgs = map[string]any{"unexpected": 1}
	})
	if got := TerminalInternalRoute(facts, false, true); got != nil {
		t.Errorf("invalid observation args should return nil, got %+v", got)
	}
}

// TestTerminalInternalRoute_browser_session_observe 验证观察族副作用。
func TestTerminalInternalRoute_browser_session_observe(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "browser"
		f.Capability.ToolName = "browser_snapshot"
		f.Capability.OperationFamily = "browser_session_observe"
		f.Capability.RiskTier = "low"
		f.Capability.StaticSideEffects = withEffects("session_cache_write")
		f.UntrustedArgs = map[string]any{}
	})
	if got := TerminalInternalRoute(facts, false, true); got == nil {
		t.Error("browser_session_observe with correct effects should allow")
	}
	// 副作用不匹配应拒绝
	facts.Capability.StaticSideEffects = withEffects()
	if got := TerminalInternalRoute(facts, false, true); got != nil {
		t.Errorf("wrong effects should return nil, got %+v", got)
	}
}

// TestTerminalInternalRoute_内部URI阻断 验证有 absolute_uris 时返回 nil。
func TestTerminalInternalRoute_内部URI阻断(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "session_list"
		f.Capability.OperationFamily = "session_status"
		f.Capability.RiskTier = "low"
		f.UntrustedArgs = map[string]any{"query": "https://example.com"}
	})
	if got := TerminalInternalRoute(facts, false, true); got != nil {
		t.Errorf("uri in args should block, got %+v", got)
	}
}

// TestBoundedText_中文按字符数 验证长度判定用 rune 数（对齐 Python len()）。
//
// 改动前用字节数：128 的上限下中文只能放 42 个字符（3 字节/字），
// 而 Python 允许 128 个字符——属于误拒。
func TestBoundedText_中文按字符数(t *testing.T) {
	// 100 个中文字符 = 300 字节，但字符数 100 < 128
	chinese := strings.Repeat("中", 100)
	if !boundedText(chinese, 128, false) {
		t.Error("100 个中文字符应通过 128 字符上限（Python len()=100）")
	}
	// 超过上限
	tooLong := strings.Repeat("中", 129)
	if boundedText(tooLong, 128, false) {
		t.Error("129 个中文字符应被拒绝")
	}
	// 边界：正好 128
	exact := strings.Repeat("中", 128)
	if !boundedText(exact, 128, false) {
		t.Error("正好 128 个中文字符应通过")
	}
}
