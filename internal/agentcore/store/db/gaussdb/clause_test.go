package gaussdb

import (
	"bytes"
	"testing"

	"gorm.io/gorm/clause"
)

// ──────────────────────────── gaussLockingClauseBuilder 测试 ────────────────────────────

// TestGaussLockingClauseBuilder_ForUpdate 验证 FOR UPDATE 不带选项时输出正确。
func TestGaussLockingClauseBuilder_ForUpdate(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateNowait 验证 FOR UPDATE NOWAIT 忽略 NOWAIT 选项。
func TestGaussLockingClauseBuilder_ForUpdateNowait(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Options:  "NOWAIT",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateSkipLocked 验证 FOR UPDATE SKIP LOCKED 忽略 SKIP LOCKED 选项。
func TestGaussLockingClauseBuilder_ForUpdateSkipLocked(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForShare 验证 FOR SHARE 输出正确。
func TestGaussLockingClauseBuilder_ForShare(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "SHARE",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR SHARE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateOfTable 验证 FOR UPDATE OF table 忽略 OF table 子句。
func TestGaussLockingClauseBuilder_ForUpdateOfTable(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Table:    clause.Table{Name: "users"},
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_NonLockingExpression 验证非 Locking 表达式时回退到默认 Build。
func TestGaussLockingClauseBuilder_NonLockingExpression(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name:       "FOR",
		Expression: clause.Expr{SQL: "SOMETHING"},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	// 非 Locking 表达式，回退到 Clause.Build()，输出 Name + Expression
	if got == "" {
		t.Error("期望非空输出，但得到空字符串")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// testBuilder 实现 clause.Builder 接口，用于捕获 SQL 输出。
type testBuilder struct {
	writer *bytes.Buffer
}

func (b *testBuilder) WriteByte(c byte) error {
	return b.writer.WriteByte(c)
}

func (b *testBuilder) WriteString(s string) (int, error) {
	return b.writer.WriteString(s)
}

func (b *testBuilder) WriteQuoted(field interface{}) {
	b.writer.WriteString(`"`)
	b.writer.WriteString(field.(string))
	b.writer.WriteString(`"`)
}

func (b *testBuilder) AddVar(writer clause.Writer, vars ...interface{}) {
	for i, v := range vars {
		if i > 0 {
			writer.WriteByte(',')
		}
		writer.WriteByte('$')
		switch val := v.(type) {
		case int:
			writer.WriteString(string(rune('0' + val)))
		case string:
			writer.WriteString(val)
		}
	}
}

func (b *testBuilder) AddError(err error) error {
	return err
}
