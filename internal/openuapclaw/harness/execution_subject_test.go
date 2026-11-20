package harness

import (
	"context"
	"testing"
)

func TestExecutionSubject_字段与默认值(t *testing.T) {
	s := ExecutionSubject{SubjectID: "m1", DisplayName: "成员一", Kind: "member"}
	if s.SubjectID != "m1" || s.DisplayName != "成员一" || s.Kind != "member" {
		t.Fatal("字段赋值不符")
	}
	// 可空字段默认零值
	if s.ParentSubjectID != "" || s.SessionID != "" {
		t.Fatal("可空字段默认应为空串")
	}
}

func TestExecutionSubject_上下文绑定不泄漏(t *testing.T) {
	subject := ExecutionSubject{SubjectID: "a", DisplayName: "A", Kind: "member"}
	ctx := WithExecutionSubject(context.Background(), subject)
	got := ExecutionSubjectFromCtx(ctx)
	if got == nil || got.SubjectID != "a" {
		t.Fatal("ctx 取回失败")
	}
	if ExecutionSubjectFromCtx(context.Background()) != nil {
		t.Fatal("未绑定的 ctx 应返回 nil")
	}
}
