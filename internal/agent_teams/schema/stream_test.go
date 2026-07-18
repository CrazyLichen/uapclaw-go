package schema

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// 编译期断言：*TeamOutputSchema 必须实现 stream.Schema 接口
var _ stream.Schema = (*TeamOutputSchema)(nil)

// TestNewTeamOutputSchema_返回指针 测试 NewTeamOutputSchema 返回指针类型。
func TestNewTeamOutputSchema_返回指针(t *testing.T) {
	member := "leader"
	role := TeamRoleLeader
	base := stream.OutputSchema{Type: "message", Index: 0}

	result := NewTeamOutputSchema(base, &member, &role)

	if result == nil {
		t.Fatal("NewTeamOutputSchema 返回 nil")
	}
	if result.SourceMember == nil || *result.SourceMember != "leader" {
		t.Errorf("SourceMember = %v, 期望 leader", result.SourceMember)
	}
	if result.Role == nil || *result.Role != TeamRoleLeader {
		t.Errorf("Role = %v, 期望 leader", result.Role)
	}
	if result.Type != "message" {
		t.Errorf("Type = %v, 期望 message", result.Type)
	}
}

// TestTeamOutputSchema_SchemaType 测试 SchemaType 方法。
func TestTeamOutputSchema_SchemaType(t *testing.T) {
	s := &TeamOutputSchema{
		OutputSchema: stream.OutputSchema{Type: "message"},
	}
	if got := s.SchemaType(); got != "message" {
		t.Errorf("SchemaType() = %v, 期望 message", got)
	}
}

// TestTeamOutputSchema_Validate 测试 Validate 方法。
func TestTeamOutputSchema_Validate(t *testing.T) {
	tests := []struct {
		name    string
		schema  *TeamOutputSchema
		wantErr bool
	}{
		{
			name:    "合法 Schema",
			schema:  &TeamOutputSchema{OutputSchema: stream.OutputSchema{Type: "message", Index: 0}},
			wantErr: false,
		},
		{
			name:    "type 为空",
			schema:  &TeamOutputSchema{OutputSchema: stream.OutputSchema{Type: "", Index: 0}},
			wantErr: true,
		},
		{
			name:    "index 为负数",
			schema:  &TeamOutputSchema{OutputSchema: stream.OutputSchema{Type: "message", Index: -1}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() 错误 = %v, 期望错误 %v", err, tt.wantErr)
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
