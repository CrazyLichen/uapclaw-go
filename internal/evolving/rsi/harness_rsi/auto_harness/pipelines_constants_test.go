package autoharness

import "testing"

// TestNormalizePipelineName_别名表 验证旧名归一化与未知名原样返回
func TestNormalizePipelineName_别名表(t *testing.T) {
	cases := []struct{ in, want string }{
		{"pr_pipeline", MetaEvolvePipeline},
		{"extended_harness_pipeline", ExtendedEvolvePipeline},
		{MetaEvolvePipeline, MetaEvolvePipeline},
		{ExtendedEvolvePipeline, ExtendedEvolvePipeline},
		{"unknown_pipeline", "unknown_pipeline"},
		{"", ""},
		{"PR_PIPELINE", "PR_PIPELINE"},
	}
	for _, c := range cases {
		if got := NormalizePipelineName(c.in); got != c.want {
			t.Fatalf("NormalizePipelineName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
