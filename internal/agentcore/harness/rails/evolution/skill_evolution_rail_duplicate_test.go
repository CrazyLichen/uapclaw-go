package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// makeRecord 构造一条用于去重提示词/解析测试的演进记录。
func makeRecord(id, target, section, content string) checkpointing.EvolutionRecord {
	return checkpointing.EvolutionRecord{
		ID: id,
		Change: checkpointing.EvolutionPatch{
			Target:  signal.EvolutionTarget(target),
			Section: section,
			Content: content,
		},
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestBuildDuplicateCheckPrompt 验证去重提示词的语言分支与内容裁剪。
func TestBuildDuplicateCheckPrompt(t *testing.T) {
	shared := []checkpointing.EvolutionRecord{makeRecord("ev_s1", "SKILL.md", "Tips", "shared-body")}
	existing := []checkpointing.EvolutionRecord{makeRecord("ev_e1", "SKILL.md", "Rules", "existing-body")}

	t.Run("中文提示词含两段经验", func(t *testing.T) {
		r := newConstructedRail(t)
		got := r.buildDuplicateCheckPrompt("skill-a", shared, existing)
		assert.Contains(t, got, "本地已有经验")
		assert.Contains(t, got, "下载的共享经验")
		assert.Contains(t, got, "shared-body")
		assert.Contains(t, got, "existing-body")
	})

	t.Run("英文语言输出英文提示词", func(t *testing.T) {
		// language 是构造参数而非 Option，用 -en 变体构造
		r := NewSkillEvolutionRail([]string{t.TempDir()}, newTestLLMModel(t), "test-model", "en")
		got := r.buildDuplicateCheckPrompt("skill-a", shared, existing)
		assert.Contains(t, got, "You are an experience deduplication expert")
		assert.NotContains(t, got, "本地已有经验")
	})

	t.Run("超长内容被截断到 500 字节", func(t *testing.T) {
		long := strings.Repeat("x", 800)
		rec := makeRecord("ev_long", "t", "s", long)
		r := newConstructedRail(t)
		got := r.buildDuplicateCheckPrompt("skill-a", []checkpointing.EvolutionRecord{rec}, nil)
		// 截断后不应出现 800 字节的完整原文
		assert.NotContains(t, got, long)
		assert.Contains(t, got, strings.Repeat("x", 500))
	})

	t.Run("空记录列表也能生成提示词", func(t *testing.T) {
		r := newConstructedRail(t)
		got := r.buildDuplicateCheckPrompt("skill-a", nil, nil)
		assert.NotEmpty(t, got)
	})
}

// TestParseDuplicateCheckResponse 验证去重结果的解析与过滤。
func TestParseDuplicateCheckResponse(t *testing.T) {
	shared := []checkpointing.EvolutionRecord{
		makeRecord("ev_a", "t", "s", "a"),
		makeRecord("ev_b", "t", "s", "b"),
	}

	t.Run("无 JSON 数组时返回全部", func(t *testing.T) {
		got := parseDuplicateCheckResponse(shared, "no json here")
		assert.Len(t, got, 2)
	})

	t.Run("JSON 非法时返回全部", func(t *testing.T) {
		got := parseDuplicateCheckResponse(shared, `[{"record_id": bad}]`)
		assert.Len(t, got, 2)
	})

	t.Run("标记 duplicate 的记录被剔除", func(t *testing.T) {
		raw := `[{"record_id":"ev_a","decision":"duplicate"},{"record_id":"ev_b","decision":"keep"}]`
		got := parseDuplicateCheckResponse(shared, raw)
		assert.Len(t, got, 1)
		assert.Equal(t, "ev_b", got[0].ID)
	})

	t.Run("全部 keep 时全部保留", func(t *testing.T) {
		raw := `[{"record_id":"ev_a","decision":"keep"},{"record_id":"ev_b","decision":"keep"}]`
		got := parseDuplicateCheckResponse(shared, raw)
		assert.Len(t, got, 2)
	})

	t.Run("未出现在决策中的记录被保留", func(t *testing.T) {
		raw := `[{"record_id":"ev_a","decision":"duplicate"}]`
		got := parseDuplicateCheckResponse(shared, raw)
		assert.Len(t, got, 1)
		assert.Equal(t, "ev_b", got[0].ID)
	})

	t.Run("JSON 前后有噪声文本也能提取", func(t *testing.T) {
		raw := "思考过程如下：\n" + `[{"record_id":"ev_a","decision":"duplicate"}]` + "\n以上。"
		got := parseDuplicateCheckResponse(shared, raw)
		assert.Len(t, got, 1)
		assert.Equal(t, "ev_b", got[0].ID)
	})
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSkillRail_OnAfterToolCall_非工具输入 验证输入类型不符时直接返回 nil。
func TestSkillRail_OnAfterToolCall_非工具输入(t *testing.T) {
	r := newConstructedRail(t)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	if err := r.OnAfterToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("非工具输入应返回 nil，实际 %v", err)
	}
}

// TestSkillRail_RequestUserEvolution_无信号 验证无轨迹且无用户意图时返回空结果。
func TestSkillRail_RequestUserEvolution_无信号(t *testing.T) {
	r := newConstructedRail(t)
	res, err := r.RequestUserEvolution(context.Background(), "skill-x", "", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res == nil {
		t.Fatal("应返回非 nil 结果")
	}
	if res.SkillName != "skill-x" {
		t.Fatalf("SkillName = %q, 期望 skill-x", res.SkillName)
	}
	if res.RequestID != nil {
		t.Fatalf("无信号时不应产生 RequestID，实际 %v", *res.RequestID)
	}
}

// TestSkillRail_RequestSimplify_无治理快照 验证精简请求在无待审治理时返回仅含技能名。
func TestSkillRail_RequestSimplify_无治理快照(t *testing.T) {
	r := newConstructedRail(t)
	res, err := r.RequestSimplify(context.Background(), "no-such-skill", nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res == nil {
		t.Fatal("应返回非 nil 结果")
	}
	if res.SkillName != "no-such-skill" {
		t.Fatalf("SkillName = %q", res.SkillName)
	}
}
