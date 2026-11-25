package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// newReviewTestKit 建真实 store/runtime/服务与五工具。
func newReviewTestKit(t *testing.T) ([]tool.Tool, *review.EvolutionReviewRuntime, *checkpointing.EvolutionStore) {
	t.Helper()
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)
	rt := review.NewEvolutionReviewRuntime(60)
	tools, err := CreateEvolutionReviewTools(EvolutionReviewToolsDeps{
		Runtime: rt, Store: store, Language: "cn", AgentID: "agt1",
	})
	require.NoError(t, err)
	require.Len(t, tools, 5)
	return tools, rt, store
}

func reviewRefArgs(ref string) map[string]any {
	return map[string]any{"evolution_review_ref": ref, "conversation_id": "sess1"}
}

func submitReviewPayload() map[string]any {
	return map[string]any{
		"subject":       map[string]any{"kind": "skill", "name": "demo_skill"},
		"outcome":       "recommend_evolve",
		"evidence_refs": []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience": map[string]any{
				"summary": "部署失败先查端口", "content": "deploy 前先检查端口占用",
				"target": "body", "section": "Troubleshooting",
			},
			"reason":        "反复失败",
			"evidence_refs": []any{},
		}},
		"summary": "审查完成",
	}
}

// TestSubmitEvolutionReview 测试提交结论返回 proposal 选择载体。
// Python review.py:351-379。
func TestSubmitEvolutionReview(t *testing.T) {
	tools, rt, _ := newReviewTestKit(t)
	ctx := context.Background()
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	payload := submitReviewPayload()
	payload["evolution_review_ref"] = launch.EvolutionReviewRef
	payload["conversation_id"] = "sess1"
	result, err := tools[0].Invoke(ctx, payload)
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	data := result["data"].(map[string]any)
	assert.Equal(t, "review_completed", data["status"])
	proposalIDs := data["proposal_ids"].([]string)
	assert.Equal(t, []string{"p1"}, proposalIDs)
	selection := data["proposal_selection_for_submission"].(map[string]any)
	assert.Equal(t, launch.EvolutionReviewRef, selection["evolution_review_ref"])
	assert.Equal(t, []string{"p1"}, selection["selected_proposal_ids"])
}

// TestReviewListReadExperiences 测试审查侧索引与读取记账。
// Python review.py:403-422/474-494。
func TestReviewListReadExperiences(t *testing.T) {
	tools, rt, store := newReviewTestKit(t)
	ctx := context.Background()
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	// 预置经验
	log := checkpointing.EmptyEvolutionLog("demo_skill")
	log.Entries = append(log.Entries, checkpointing.EvolutionRecord{
		ID: "ev_r1", Source: "t", Timestamp: "2026-01-01T00:00:00Z", Score: 0.9,
		Change: checkpointing.EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "内容"},
	})
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", log, ""))

	// list 默认 limit 50（与主 agent 20 区分）
	result, err := tools[1].Invoke(ctx, reviewRefArgs(launch.EvolutionReviewRef))
	require.NoError(t, err)
	data := result["data"].(map[string]any)
	assert.Equal(t, 1, data["total_count"])

	// read + evidence 记账
	result, err = tools[2].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"record_ids":           []any{"ev_r1"},
	})
	require.NoError(t, err)
	scope, err := rt.ResolveScope(launch.EvolutionReviewRef, "sess1")
	require.NoError(t, err)
	assert.True(t, scope.ReadTrace["ev_r1"])
}

// TestReviewTrajectoryListFilter 测试四过滤与分页上限。
// Python review.py:738-780。
func TestReviewTrajectoryListFilter(t *testing.T) {
	tools, rt, _ := newReviewTestKit(t)
	ctx := context.Background()
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	// 最小 OTLP 载荷：两条 llm span + 一条错误 span（对齐 model_spans_team_test 构造）
	trajPayload := map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{
				map[string]any{"key": "openjiuwen.trajectory_id", "value": map[string]any{"stringValue": "traj_1"}},
			}},
			"scopeSpans": []any{map[string]any{"spans": []any{
				map[string]any{
					"name": "llm.call", "traceId": "t1", "spanId": "s1",
					"startTimeUnixNano": "100", "endTimeUnixNano": "200",
				},
				map[string]any{
					"name": "llm.call", "traceId": "t1", "spanId": "s2",
					"startTimeUnixNano": "150", "endTimeUnixNano": "250",
					"status": map[string]any{"code": 2, "message": "boom"},
				},
			}}},
		}}}
	traj, err := trajectory.NewTrajectory(trajPayload)
	require.NoError(t, err)
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", traj)
	require.NoError(t, err)

	args := reviewRefArgs(launch.EvolutionReviewRef)
	result, err := tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	data := result["data"].(map[string]any)
	assert.NotNil(t, data["items"])

	// kind 过滤
	args["kind"] = "llm"
	result, err = tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	data = result["data"].(map[string]any)
	items := data["items"].([]map[string]any)
	for _, item := range items {
		assert.Equal(t, "llm", item["kind"])
	}

	// limit 越界回落默认
	args["kind"] = ""
	args["limit"] = 999
	result, err = tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	_ = result

	// has_error 过滤
	delete(args, "limit")
	args["has_error"] = true
	result, err = tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	_ = result
}

// TestReviewTrajectoryReadLimits 测试 refs 上限与 unknown。
// Python review.py:806-826。
func TestReviewTrajectoryReadLimits(t *testing.T) {
	tools, rt, _ := newReviewTestKit(t)
	ctx := context.Background()
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	// >8 refs
	refs := make([]any, 9)
	for i := range refs {
		refs[i] = "span:a:b"
	}
	result, err := tools[4].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"refs":                 refs,
	})
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["error"], "read at most 8 trajectory refs per call")

	// unknown refs
	result, err = tools[4].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"refs":                 []any{"span:x:y"},
	})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "unknown trajectory refs")
}

// TestReviewSafe 测试图片脱敏与键剔除。
// Python review.py:518-543。
func TestReviewSafe(t *testing.T) {
	// data:image 字符串
	assert.Equal(t, "<omitted:image_content>", reviewSafe("data:image/png;base64,xxx"))
	// image 类型 map
	assert.Equal(t, map[string]any{"type": "image", "omitted": "image_content"}, reviewSafe(map[string]any{"type": "image"}))
	assert.Equal(t, map[string]any{"type": "input_image", "omitted": "image_content"}, reviewSafe(map[string]any{"type": "input_image"}))
	// base64/image_url/bytes 键剔除
	// Python 仅剔除 key 含 "base64" 或 ∈ {image_url, bytes}；b64_json 保留（L532）
	got := reviewSafe(map[string]any{"b64_json": "x", "image_url": "y", "bytes": "z", "keep": 1})
	assert.Equal(t, map[string]any{"b64_json": "x", "keep": 1}, got)
	// 嵌套递归
	got = reviewSafe(map[string]any{"outer": []any{"data:image/png;base64,aaa"}})
	assert.Equal(t, map[string]any{"outer": []any{"<omitted:image_content>"}}, got)
	// 原始标量透传
	assert.Equal(t, "plain", reviewSafe("plain"))
	assert.Equal(t, 1.5, reviewSafe(1.5))
}

// TestBoundedValueAndMessages 测试截断包装。
// Python review.py:546-581。
func TestBoundedValueAndMessages(t *testing.T) {
	// 未截断
	got := boundedValue("short", 100)
	assert.Equal(t, "short", got["value"])
	assert.Equal(t, false, got["truncated"])
	// 截断 + original_chars
	long := make([]byte, 50)
	for i := range long {
		long[i] = 'a'
	}
	got = boundedValue(string(long), 10)
	assert.Equal(t, true, got["truncated"])
	assert.Equal(t, 50, got["original_chars"])

	// 消息保留末尾 3 条
	msgs := make([]map[string]any, 5)
	for i := range msgs {
		msgs[i] = map[string]any{"role": "user", "content": "m"}
	}
	wrapped := boundedMessages(msgs)
	assert.Equal(t, true, wrapped["truncated"])
	assert.Equal(t, 5, wrapped["original_count"])
	items := wrapped["items"].([]map[string]any)
	assert.Len(t, items, 3)
}

// TestSpanDetailBranches 测试 llm/tool/context 三分支。
// Python review.py:644-671。
func TestSpanDetailBranches(t *testing.T) {
	// llm 分支
	llmSpan := map[string]any{
		"name": "llm.call", "traceId": "t1", "spanId": "s1",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": map[string]any{
			"gen_ai.response.model":   "gpt-4",
			"gen_ai.prompt.0.content": "hi",
		},
	}
	detail := spanDetail(llmSpan)
	assert.Equal(t, "llm", detail["kind"])
	assert.Equal(t, "span:t1:s1", detail["ref"])
	llm := detail["llm"].(map[string]any)
	assert.Equal(t, "gpt-4", llm["model"])
	assert.NotNil(t, llm["input_messages"])

	// tool 分支
	toolSpan := map[string]any{
		"name": "tool.run", "traceId": "t1", "spanId": "s2",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": map[string]any{"gen_ai.tool.name": "shell"},
	}
	detail = spanDetail(toolSpan)
	assert.Equal(t, "tool", detail["kind"])
	toolDetail := detail["tool"].(map[string]any)
	assert.Equal(t, "shell", toolDetail["name"])

	// context 分支（event 类别）
	eventSpan := map[string]any{
		"name": "event.team_event", "traceId": "t1", "spanId": "s3",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": map[string]any{"agentteam.task.id": "task_9"},
	}
	detail = spanDetail(eventSpan)
	assert.Equal(t, "event", detail["kind"])
	context := detail["context"].(map[string]any)
	assert.Equal(t, "task_9", context["agentteam.task.id"])
}

// TestReviewToolsStoreFallback 测试 query_service 缺省回落 store。
// Python review.py:497-500 + create 参数回退（review.py:112）。
func TestReviewToolsStoreFallback(t *testing.T) {
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	_, err := queryServiceFromStore(store)
	assert.NoError(t, err)
	_, err = queryServiceFromStore(nil)
	assert.EqualError(t, err, "query_service or store is required")

	// runtimeForRef：runtime nil 报 unknown
	tool := &evolutionReviewTool{}
	_, err = tool.runtimeForRef("ref_x")
	assert.Error(t, err)
}

// TestReviewToolsErrorPaths 五工具错误分支表驱动。
// Python 各 invoke 的 except 分支（runtime/查询失败透传 failure）。
func TestReviewToolsErrorPaths(t *testing.T) {
	tools, rt, store := newReviewTestKit(t)
	ctx := context.Background()

	// runtime nil → runtimeForRef 报错
	noRT, err := agenttoolsCreateReview(nil, store)
	require.NoError(t, err)
	result, err := noRT[0].Invoke(ctx, map[string]any{"evolution_review_ref": "ref"})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "unknown evolution review ref")

	// ref 键缺失（Python args.pop KeyError）
	result, err = tools[0].Invoke(ctx, map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "evolution_review_ref is required")

	// unknown ref 全工具
	for i, tt := range tools {
		result, err = tt.Invoke(ctx, map[string]any{
			"evolution_review_ref": "evrr_missing",
			"conversation_id":      "sess1",
			"record_ids":           []any{"x"},
			"refs":                 []any{"span:a:b"},
		})
		require.NoError(t, err, "tool=%d", i)
		assert.Equal(t, false, result["success"], "tool=%d", i)
	}

	// subject 缺失触发 ResolveSelectedProposals 前？submit 缺 outcome（NormalizeReviewResult 失败）
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)
	result, err = tools[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
	})
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
}

// agenttoolsCreateReview 测试辅助：nil runtime 构造。
func agenttoolsCreateReview(rt *review.EvolutionReviewRuntime, store *checkpointing.EvolutionStore) ([]tool.Tool, error) {
	return CreateEvolutionReviewTools(EvolutionReviewToolsDeps{
		Runtime: rt, Store: store, Language: "cn", AgentID: "x",
	})
}

// TestReviewQueryServiceFallback 集成替身：query_service 缺省走 store 构造。
// Python review.py:112（_query_service_from_store 回退）+ queryServiceForRef 分支。
func TestReviewQueryServiceFallback(t *testing.T) {
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)
	rt := review.NewEvolutionReviewRuntime(60)
	tools, err := CreateEvolutionReviewTools(EvolutionReviewToolsDeps{
		Runtime: rt, Store: store, Language: "cn", AgentID: "",
	})
	require.NoError(t, err)
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	// list 经 store 回落的 query service 成功
	result, err := tools[1].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])

	// name_contains / tool_name 过滤命中空集
	result, err = tools[3].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"name_contains":        "nomatch",
	})
	require.NoError(t, err)
	data := result["data"].(map[string]any)
	assert.Equal(t, 0, data["total"])

	result, err = tools[3].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"tool_name":            "nomatch",
	})
	require.NoError(t, err)
	data = result["data"].(map[string]any)
	assert.Equal(t, 0, data["total"])

	// store 缺失 → queryServiceFromStore 报错（L497-500）
	_, err = CreateEvolutionReviewTools(EvolutionReviewToolsDeps{Runtime: rt, Language: "cn"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query_service or store is required")
}
