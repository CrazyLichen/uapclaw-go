package tool_call

import "testing"

// TestDefaultConfigEg_完整 测试 Example Stage 配置完整性
func TestDefaultConfigEg_完整(t *testing.T) {
	requiredKeys := []string{
		"gen_model_id", "eval_model_id", "verbose",
		"num_init_loop", "num_refine_steps", "num_feedback_steps",
		"score_eval_weight", "beam_width", "expand_num",
		"max_depth", "num_workers", "top_k",
	}
	for _, key := range requiredKeys {
		if _, ok := DefaultConfigEg[key]; !ok {
			t.Errorf("DefaultConfigEg missing key: %s", key)
		}
	}
}

// TestDefaultConfigDesc_完整 测试 Description Stage 配置完整性
func TestDefaultConfigDesc_完整(t *testing.T) {
	requiredKeys := []string{
		"gen_model_id", "eval_model_id", "verbose",
		"num_init_loop", "num_feedback_steps",
		"score_eval_weight", "num_examples_for_desc",
		"beam_width", "expand_num", "max_depth",
		"num_workers", "top_k",
	}
	for _, key := range requiredKeys {
		if _, ok := DefaultConfigDesc[key]; !ok {
			t.Errorf("DefaultConfigDesc missing key: %s", key)
		}
	}
}

// TestDefaultConfigEg_Desc差异 测试两个配置的差异键
func TestDefaultConfigEg_Desc差异(t *testing.T) {
	// eg 有 num_refine_steps，desc 没有
	if _, ok := DefaultConfigEg["num_refine_steps"]; !ok {
		t.Error("DefaultConfigEg should have num_refine_steps")
	}
	if _, ok := DefaultConfigDesc["num_refine_steps"]; ok {
		t.Error("DefaultConfigDesc should not have num_refine_steps")
	}
	// desc 有 num_examples_for_desc，eg 没有
	if _, ok := DefaultConfigDesc["num_examples_for_desc"]; !ok {
		t.Error("DefaultConfigDesc should have num_examples_for_desc")
	}
	if _, ok := DefaultConfigEg["num_examples_for_desc"]; ok {
		t.Error("DefaultConfigEg should not have num_examples_for_desc")
	}
}
