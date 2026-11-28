package kbadmin

import (
	"context"
	"errors"
	"testing"
)

// TestRunPipeline_按序执行 步骤按声明序执行
func TestRunPipeline_按序执行(t *testing.T) {
	var order []string
	steps := []stageStep{
		{name: "parsing", run: func(context.Context, *ParseJob, *pipelineEnv) error { order = append(order, "parsing"); return nil }},
		{name: "chunking", run: func(context.Context, *ParseJob, *pipelineEnv) error { order = append(order, "chunking"); return nil }},
	}
	env := &pipelineEnv{}
	err := runPipeline(context.Background(), &ParseJob{TaskID: "t1"}, steps, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "parsing" || order[1] != "chunking" {
		t.Fatalf("步骤序不符: %v", order)
	}
}

// TestRunPipeline_ctx取消中断 ctx 取消后停止后续步骤
func TestRunPipeline_ctx取消中断(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ran := false
	steps := []stageStep{
		{name: "parsing", run: func(context.Context, *ParseJob, *pipelineEnv) error { cancel(); return nil }},
		{name: "chunking", run: func(context.Context, *ParseJob, *pipelineEnv) error { ran = true; return nil }},
	}
	err := runPipeline(ctx, &ParseJob{TaskID: "t1"}, steps, &pipelineEnv{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 ctx.Canceled, got %v", err)
	}
	if ran {
		t.Fatal("取消后后续步骤不得执行")
	}
}

// TestRunPipeline_失败即停 步骤错误透传且后续不执行
func TestRunPipeline_失败即停(t *testing.T) {
	want := errors.New("boom")
	steps := []stageStep{
		{name: "parsing", run: func(context.Context, *ParseJob, *pipelineEnv) error { return want }},
		{name: "chunking", run: func(context.Context, *ParseJob, *pipelineEnv) error { t.Fatal("不应执行"); return nil }},
	}
	if err := runPipeline(context.Background(), &ParseJob{}, steps, &pipelineEnv{}); !errors.Is(err, want) {
		t.Fatalf("错误应透传, got %v", err)
	}
}

// TestPipelinePlans_步骤计划声明 断言两类型计划表内容（新增类型=加一行声明）
func TestPipelinePlans_步骤计划声明(t *testing.T) {
	docSteps := pipelinePlans["document"]
	if len(docSteps) != 3 {
		t.Fatalf("document 计划应 3 步, got %d", len(docSteps))
	}
	for i, want := range []string{"parsing", "chunking", "embedding"} {
		if docSteps[i].name != want {
			t.Fatalf("document 第 %d 步应 %s, got %s", i, want, docSteps[i].name)
		}
	}
	graphSteps := pipelinePlans["graph"]
	if len(graphSteps) != 4 || graphSteps[3].name != "triple" {
		t.Fatalf("graph 计划应 4 步且末步 triple: %+v", graphSteps)
	}
	if _, ok := pipelinePlans["multi"]; ok {
		t.Fatal("multi 聚合库无文档级操作，不得入计划表")
	}
}
