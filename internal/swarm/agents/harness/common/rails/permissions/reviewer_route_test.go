package permissions

import (
	"path/filepath"
	"testing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// routeFacts 构造 reviewer_route 测试用的 ToolDecisionFacts。
func routeFacts(opts func(*ToolDecisionFacts)) ToolDecisionFacts {
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

// routeCall 以默认参数调用 ReviewerRoute。
func routeCall(facts ToolDecisionFacts, policy, guard string) DecisionRoute {
	return ReviewerRoute(facts, policy, guard, "", 3, nil, nil, nil, nil)
}

// ──────────────────────────── ReviewerRoute 前置判定 ────────────────────────────

// TestReviewerRoute_policyDeny 验证策略 deny 直接硬拒绝。
func TestReviewerRoute_policyDeny(t *testing.T) {
	got := routeCall(routeFacts(nil), AutoDecisionDenyLevel, "")
	if got.Level != AutoDecisionDenyLevel || got.Reason != "policy_deny" {
		t.Errorf("got %+v", got)
	}
	if got.Source != HardBlockSource {
		t.Errorf("source = %q", got.Source)
	}
}

// TestReviewerRoute_guardDeny 验证守卫 deny 直接硬拒绝。
func TestReviewerRoute_guardDeny(t *testing.T) {
	got := routeCall(routeFacts(nil), AutoDecisionAskLevel, AutoDecisionDenyLevel)
	if got.Level != AutoDecisionDenyLevel || got.Reason != "guard_deny" {
		t.Errorf("got %+v", got)
	}
}

// TestReviewerRoute_policyNotAsk 验证策略既非 ask 也非 allow。
func TestReviewerRoute_policyNotAsk(t *testing.T) {
	got := routeCall(routeFacts(nil), "weird", "")
	if got.Level != AutoDecisionDenyLevel || got.Reason != "policy_not_ask" {
		t.Errorf("got %+v", got)
	}
}

// TestReviewerRoute_scopedCandidate豁免 验证 scoped_candidate 绕过 policy 检查。
func TestReviewerRoute_scopedCandidate豁免(t *testing.T) {
	got := routeCall(routeFacts(nil), "weird", "scoped_candidate")
	if got.Reason == "policy_not_ask" {
		t.Error("scoped_candidate should bypass policy_not_ask")
	}
}

// TestReviewerRoute_terminalManual 验证 terminal_manual 转人工。
func TestReviewerRoute_terminalManual(t *testing.T) {
	got := routeCall(routeFacts(nil), AutoDecisionAskLevel, "terminal_manual")
	if got.Level != AutoDecisionAskLevel || got.Reason != "terminal_manual" {
		t.Errorf("got %+v", got)
	}
	if got.Source != ManualReviewSource {
		t.Errorf("source = %q", got.Source)
	}
}

// TestReviewerRoute_大小写与空白 验证输入归一。
func TestReviewerRoute_大小写与空白(t *testing.T) {
	got := routeCall(routeFacts(nil), "  DENY  ", "")
	if got.Reason != "policy_deny" {
		t.Errorf("got %+v", got)
	}
}

// ──────────────────────────── 结构性人工原因 ────────────────────────────

// TestReviewerRoute_结构性人工原因 验证六类结构性问题。
func TestReviewerRoute_结构性人工原因(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ToolDecisionFacts)
		reason string
	}{
		{"参数非对象", func(f *ToolDecisionFacts) { f.ArgumentsValidObject = false }, "arguments_not_object"},
		{"path访问未知", func(f *ToolDecisionFacts) { f.AccessesKnown = false }, "core_accesses_unknown"},
		{"shell访问未知", func(f *ToolDecisionFacts) {
			f.Capability.Category = "shell"
			f.AccessesKnown = false
		}, "core_accesses_unknown"},
		{"别名冲突", func(f *ToolDecisionFacts) { f.Capability.AliasConflict = true }, "alias_conflict"},
		{"事实来源不可信", func(f *ToolDecisionFacts) { f.Capability.FactsSource = "runtime" }, "facts_source_unverified"},
		{"操作未知", func(f *ToolDecisionFacts) { f.Capability.OperationFamily = "unknown" }, "unknown_operation"},
		{"类别未知", func(f *ToolDecisionFacts) { f.Capability.Category = "unknown" }, "unknown_operation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := routeFacts(c.mutate)
			got := routeCall(facts, AutoDecisionAskLevel, "")
			if got.Reason != c.reason {
				t.Errorf("got %q, want %q", got.Reason, c.reason)
			}
		})
	}
}

// ──────────────────────────── 领域路由 ────────────────────────────

// TestReviewerRoute_领域硬拒绝 验证 domain_route 的 hard_guard。
func TestReviewerRoute_领域硬拒绝(t *testing.T) {
	domain := &DecisionRoute{Level: AutoDecisionDenyLevel, Reason: "domain_x", Source: HardBlockSource}
	got := ReviewerRoute(routeFacts(nil), AutoDecisionAskLevel, "", "", 3, nil, nil, domain, nil)
	if got.Level != AutoDecisionDenyLevel || got.Reason != "domain_x" {
		t.Errorf("got %+v", got)
	}
}

// TestReviewerRoute_领域人工 验证 domain_route 的 manual_only。
func TestReviewerRoute_领域人工(t *testing.T) {
	domain := &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_y", Source: ManualReviewSource}
	got := ReviewerRoute(routeFacts(nil), AutoDecisionAskLevel, "", "", 3, nil, nil, domain, nil)
	if got.Reason != "domain_y" || got.Source != ManualReviewSource {
		t.Errorf("got %+v", got)
	}
}

// TestReviewerRoute_领域空原因 验证空 reason 的兜底文案。
func TestReviewerRoute_领域空原因(t *testing.T) {
	domain := &DecisionRoute{Level: AutoDecisionDenyLevel, Source: HardBlockSource}
	got := ReviewerRoute(routeFacts(nil), AutoDecisionAskLevel, "", "", 3, nil, nil, domain, nil)
	if got.Reason != "domain_policy_deny" {
		t.Errorf("got %q", got.Reason)
	}
	domain2 := &DecisionRoute{Level: AutoDecisionAskLevel, Source: ManualReviewSource}
	got2 := ReviewerRoute(routeFacts(nil), AutoDecisionAskLevel, "", "", 3, nil, nil, domain2, nil)
	if got2.Reason != "domain_policy_manual" {
		t.Errorf("got %q", got2.Reason)
	}
}

// TestReviewerRoute_领域语义审查 验证 semantic_reviewer 不短路（继续往下走）。
func TestReviewerRoute_领域语义审查(t *testing.T) {
	domain := &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "sem", Source: SemanticReviewSource}
	got := ReviewerRoute(routeFacts(nil), AutoDecisionAskLevel, "", "", 3, nil, nil, domain, nil)
	// semantic 不短路，最终落到末尾的 _semantic()
	if got.Reason != "semantic_review_required" {
		t.Errorf("got %q", got.Reason)
	}
}

// ──────────────────────────── 工作区写入与用户路径 ────────────────────────────

// TestReviewerRoute_工作区写入缺root 验证 bounded_write_workspace_missing。
func TestReviewerRoute_工作区写入缺root(t *testing.T) {
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "write_file"
		f.Capability.Category = "path"
		f.Capability.RiskTier = "medium"
		f.WritePaths = []string{"/w"}
	})
	got := routeCall(facts, AutoDecisionAskLevel, "")
	if got.Reason != "bounded_write_workspace_missing" {
		t.Errorf("got %q", got.Reason)
	}
}

// TestReviewerRoute_工作区写入有root 验证转语义审查。
func TestReviewerRoute_工作区写入有root(t *testing.T) {
	dir := t.TempDir()
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "write_file"
		f.Capability.Category = "path"
		f.Capability.RiskTier = "medium"
		f.WritePaths = []string{"/w"}
	})
	got := ReviewerRoute(facts, AutoDecisionAskLevel, "", dir, 3, nil, nil, nil, nil)
	if got.Reason != "semantic_review_required" {
		t.Errorf("got %q", got.Reason)
	}
}

// TestReviewerRoute_用户路径工具带外部路径 验证转语义审查。
func TestReviewerRoute_用户路径工具带外部路径(t *testing.T) {
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "read_file"
		f.ExternalPaths = []string{"/ext"}
	})
	got := routeCall(facts, AutoDecisionAskLevel, "")
	if got.Reason != "semantic_review_required" {
		t.Errorf("got %q", got.Reason)
	}
}

// ──────────────────────────── 公开搜索 ────────────────────────────

// TestReviewerRoute_搜索缺query 验证 search_query_missing。
func TestReviewerRoute_搜索缺query(t *testing.T) {
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "network"
		f.Capability.OperationFamily = "public_search"
		f.UntrustedArgs = map[string]any{}
	})
	got := routeCall(facts, AutoDecisionAskLevel, "")
	if got.Reason != "search_query_missing" {
		t.Errorf("got %q", got.Reason)
	}
}

// TestReviewerRoute_搜索密钥query 验证 search_query_secret_like。
func TestReviewerRoute_搜索密钥query(t *testing.T) {
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "network"
		f.Capability.OperationFamily = "public_search"
		f.UntrustedArgs = map[string]any{"query": "api_key = abc123"}
	})
	got := routeCall(facts, AutoDecisionAskLevel, "")
	if got.Level != AutoDecisionDenyLevel || got.Reason != "search_query_secret_like" {
		t.Errorf("got %+v", got)
	}
}

// TestReviewerRoute_搜索正常query 验证继续往下走。
func TestReviewerRoute_搜索正常query(t *testing.T) {
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "network"
		f.Capability.OperationFamily = "public_search"
		f.UntrustedArgs = map[string]any{"query": "how to sort"}
	})
	got := routeCall(facts, AutoDecisionAskLevel, "")
	if got.Reason != "semantic_review_required" {
		t.Errorf("got %q", got.Reason)
	}
}

// ──────────────────────────── 文件投递 ────────────────────────────

// TestFileDeliveryManualReason_各原因 验证五种原因。
func TestFileDeliveryManualReason_各原因(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		facts    ToolDecisionFacts
		root     string
		maxFiles int
		want     string
	}{
		{"缺root", routeFacts(func(f *ToolDecisionFacts) {
			f.ReadPaths = []string{"/a"}
		}), "", 3, "bounded_user_file_delivery_workspace_missing"},
		{"缺路径", routeFacts(nil), dir, 3, "bounded_user_file_delivery_missing_path"},
		{"外部路径", routeFacts(func(f *ToolDecisionFacts) {
			f.ReadPaths = []string{"/a"}
			f.ExternalPaths = []string{"/ext"}
		}), dir, 3, "bounded_user_file_delivery_external_path"},
		{"文件过多", routeFacts(func(f *ToolDecisionFacts) {
			f.ReadPaths = []string{"/a", "/b", "/c", "/d"}
		}), dir, 3, "bounded_user_file_delivery_too_many_files"},
		{"排除路径", routeFacts(func(f *ToolDecisionFacts) {
			f.ReadPaths = []string{filepath.Join(dir, "go.mod")}
		}), dir, 3, "bounded_user_file_delivery_excluded_path"},
		{"正常", routeFacts(func(f *ToolDecisionFacts) {
			f.ReadPaths = []string{filepath.Join(dir, "readme.txt")}
		}), dir, 3, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FileDeliveryManualReason(c.facts, c.root, c.maxFiles, nil)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestFileDeliveryManualReason_自定义排除 验证额外排除项生效。
func TestFileDeliveryManualReason_自定义排除(t *testing.T) {
	dir := t.TempDir()
	facts := routeFacts(func(f *ToolDecisionFacts) {
		f.ReadPaths = []string{filepath.Join(dir, "secret.txt")}
	})
	got := FileDeliveryManualReason(facts, dir, 3, []string{"secret.txt"})
	if got != "bounded_user_file_delivery_excluded_path" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 用例：searchQueryIsSecretLike ───────────────────────────────

// TestSearchQueryIsSecretLike_各形态 验证密钥识别。
func TestSearchQueryIsSecretLike_各形态(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"api_key = abc123", true},
		{"API-KEY: xyz", true},
		{"apikey=foo", true},
		{"password: hunter2", true},
		// token 后的值需 20+ 字符才命中通用令牌规则（Python 实测）
		{"token is abc", false},
		{"token is abcdefghij1234567890", true},
		{"sk-abcdefghijklmnop", true},
		{"sk-proj-abcdefghijklmnop", true},
		{"ghp_abcdefghijklmnopqrst", true},
		{"github_pat_abcdefghijklmnopqrst", true},
		{"AKIA1234567890ABCDEF", true},
		{"how to sort a list", false},
		{"", false},
		{"api_key", false},
		{"just a normal query", false},
	}
	for _, c := range cases {
		if got := searchQueryIsSecretLike(c.query); got != c.want {
			t.Errorf("searchQueryIsSecretLike(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// TestSearchQueryIsSecretLike_百分号编码 验证两轮解码。
func TestSearchQueryIsSecretLike_百分号编码(t *testing.T) {
	// "api_key%3Dabc123" → 解码为 "api_key=abc123"
	if !searchQueryIsSecretLike("api_key%3Dabc123") {
		t.Error("percent-encoded secret should be detected")
	}
	// 双重编码
	if !searchQueryIsSecretLike("api_key%253Dabc123") {
		t.Error("double-encoded secret should be detected after two rounds")
	}
}

// TestSearchQueryIsSecretLike_空白归一 验证多空白合并。
func TestSearchQueryIsSecretLike_空白归一(t *testing.T) {
	if !searchQueryIsSecretLike("api_key   =   abc123") {
		t.Error("multiple spaces should be normalized")
	}
}

// ──────────────────────────── 用例：pathMatchesExclusion ───────────────────────────────

// TestPathMatchesExclusion_绝对路径 验证绝对排除项。
func TestPathMatchesExclusion_绝对路径(t *testing.T) {
	root := "/workspace"
	if !pathMatchesExclusion("/etc/secret", "/etc", root) {
		t.Error("absolute prefix should match")
	}
	if pathMatchesExclusion("/other/file", "/etc", root) {
		t.Error("different absolute should not match")
	}
}

// TestPathMatchesExclusion_相对工作区 验证相对排除项。
func TestPathMatchesExclusion_相对工作区(t *testing.T) {
	root := "/workspace"
	if !pathMatchesExclusion("/workspace/go.mod", "go.mod", root) {
		t.Error("relative to workspace should match")
	}
	if pathMatchesExclusion("/workspace/src/main.go", "go.mod", root) {
		t.Error("non-matching should not match")
	}
}

// TestPathMatchesExclusion_分段匹配 验证大小写无关的分段子序列。
func TestPathMatchesExclusion_分段匹配(t *testing.T) {
	root := "/workspace"
	// 单段：任意位置出现即可
	if !pathMatchesExclusion("/workspace/src/.git/config", ".git", root) {
		t.Error("single segment should match anywhere")
	}
	// 多段
	if !pathMatchesExclusion("/workspace/config/permissions.yaml", "config/permissions.yaml", root) {
		t.Error("multi-segment should match")
	}
	// 大小写无关
	if !pathMatchesExclusion("/workspace/CONFIG/PERMISSIONS.YAML", "config/permissions.yaml", root) {
		t.Error("should be case-insensitive")
	}
}

// TestPathMatchesExclusion_空排除项 验证空串返回 false。
func TestPathMatchesExclusion_空排除项(t *testing.T) {
	if pathMatchesExclusion("/a", "", "/root") {
		t.Error("empty exclusion should be false")
	}
	if pathMatchesExclusion("/a", "   ", "/root") {
		t.Error("whitespace exclusion should be false")
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// TestReviewerManual与Deny与Semantic 验证三个构造器。
func TestReviewerManual与Deny与Semantic(t *testing.T) {
	m := reviewerManual("r")
	if m.Level != AutoDecisionAskLevel || m.Source != ManualReviewSource || m.Reason != "r" {
		t.Errorf("manual = %+v", m)
	}
	d := reviewerDeny("r")
	if d.Level != AutoDecisionDenyLevel || d.Source != HardBlockSource || d.Reason != "r" {
		t.Errorf("deny = %+v", d)
	}
	s := reviewerSemantic()
	if s.Level != AutoDecisionAskLevel || s.Source != SemanticReviewSource {
		t.Errorf("semantic = %+v", s)
	}
}

// TestIsPublicSearch与IsPublicFetch 验证两个谓词。
func TestIsPublicSearch与IsPublicFetch(t *testing.T) {
	search := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "network"
		f.Capability.OperationFamily = "public_search"
	})
	if !isPublicSearch(search) || isPublicFetch(search) {
		t.Error("search facts should be public_search only")
	}
	fetch := routeFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "network"
		f.Capability.OperationFamily = "public_web_read"
	})
	if isPublicSearch(fetch) || !isPublicFetch(fetch) {
		t.Error("fetch facts should be public_fetch only")
	}
}

// TestIsWorkspaceWrite 验证工作区写入判定。
func TestIsWorkspaceWrite(t *testing.T) {
	base := func() ToolDecisionFacts {
		return routeFacts(func(f *ToolDecisionFacts) {
			f.Capability.ToolName = "write_file"
			f.Capability.Category = "path"
			f.Capability.RiskTier = "medium"
			f.WritePaths = []string{"/w"}
		})
	}
	if !isWorkspaceWrite(base()) {
		t.Error("should be workspace write")
	}
	// 工具名不在集合
	facts := base()
	facts.Capability.ToolName = "other_tool"
	if isWorkspaceWrite(facts) {
		t.Error("unknown tool should not match")
	}
	// 风险档不符
	facts = base()
	facts.Capability.RiskTier = "high"
	if isWorkspaceWrite(facts) {
		t.Error("high risk should not match")
	}
	// 无写路径
	facts = base()
	facts.WritePaths = nil
	if isWorkspaceWrite(facts) {
		t.Error("no write paths should not match")
	}
}

// TestIsPathWithin 验证路径包含判定。
func TestIsPathWithin(t *testing.T) {
	if !isPathWithin("/root/a/b", "/root") {
		t.Error("should be within")
	}
	if !isPathWithin("/root", "/root") {
		t.Error("same path should be within")
	}
	if isPathWithin("/other", "/root") {
		t.Error("different should not be within")
	}
	if isPathWithin("/rooted", "/root") {
		t.Error("prefix-only should not be within")
	}
}

// TestSplitPathCasefold 验证路径分段小写化。
func TestSplitPathCasefold(t *testing.T) {
	got := splitPathCasefold("/A/B/c")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
	if got := splitPathCasefold(""); len(got) != 0 {
		t.Errorf("empty -> %v", got)
	}
}

// TestEqualStringSlices 验证切片相等。
func TestEqualStringSlices(t *testing.T) {
	if !equalStringSlices([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("should be equal")
	}
	if equalStringSlices([]string{"a"}, []string{"a", "b"}) {
		t.Error("length mismatch")
	}
	if equalStringSlices([]string{"a"}, []string{"b"}) {
		t.Error("content mismatch")
	}
}

// TestPercentDecode 验证百分号解码不处理 +。
func TestPercentDecode(t *testing.T) {
	got, err := percentDecode("a%20b")
	if err != nil || got != "a b" {
		t.Errorf("got (%q,%v)", got, err)
	}
	// + 不转空格（Python unquote 同款）
	got2, _ := percentDecode("a+b")
	if got2 != "a+b" {
		t.Errorf("plus should be kept, got %q", got2)
	}
}
