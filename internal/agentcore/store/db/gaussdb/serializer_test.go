package gaussdb

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm/schema"
)

// ──────────────────────────── gaussStringSerializer 测试 ────────────────────────────

// TestGaussStringSerializer_Value_String 验证 string 值直接返回。
func TestGaussStringSerializer_Value_String(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("got %v, want %q", val, "hello")
	}
}

// TestGaussStringSerializer_Value_Time 验证 time.Time 值转换为指定格式字符串。
func TestGaussStringSerializer_Value_Time(t *testing.T) {
	s := gaussStringSerializer{}
	ts := time.Date(2025, 7, 30, 15, 4, 5, 123456000, time.UTC)
	val, err := s.Value(context.Background(), nil, reflect.Value{}, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "2025-07-30 15:04:05.123456"
	if val != want {
		t.Errorf("got %q, want %q", val, want)
	}
}

// TestGaussStringSerializer_Value_Nil 验证 nil 值返回 nil。
func TestGaussStringSerializer_Value_Nil(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Errorf("got %v, want nil", val)
	}
}

// TestGaussStringSerializer_Value_Int 验证 int 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Int(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "42" {
		t.Errorf("got %v, want %q", val, "42")
	}
}

// TestGaussStringSerializer_Value_Float 验证 float64 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Float(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, 3.14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "3.14" {
		t.Errorf("got %v, want %q", val, "3.14")
	}
}

// TestGaussStringSerializer_Value_Bool 验证 bool 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Bool(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "true" {
		t.Errorf("got %v, want %q", val, "true")
	}
}

// TestGaussStringSerializer_Scan_String 验证从数据库扫描 string 值。
func TestGaussStringSerializer_Scan_String(t *testing.T) {
	s := gaussStringSerializer{}
	var captured string
	field := &schema.Field{}
	field.Set = func(_ context.Context, _ reflect.Value, v interface{}) error {
		captured = v.(string)
		return nil
	}

	err := s.Scan(context.Background(), field, reflect.Value{}, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "hello" {
		t.Errorf("got %q, want %q", captured, "hello")
	}
}

// TestGaussStringSerializer_Scan_Bytes 验证从数据库扫描 []byte 值转为 string。
func TestGaussStringSerializer_Scan_Bytes(t *testing.T) {
	s := gaussStringSerializer{}
	var captured string
	field := &schema.Field{}
	field.Set = func(_ context.Context, _ reflect.Value, v interface{}) error {
		captured = v.(string)
		return nil
	}

	err := s.Scan(context.Background(), field, reflect.Value{}, []byte("world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "world" {
		t.Errorf("got %q, want %q", captured, "world")
	}
}

// TestGaussStringSerializer_Scan_Nil 验证从数据库扫描 nil 值不报错且不调用 Set。
func TestGaussStringSerializer_Scan_Nil(t *testing.T) {
	s := gaussStringSerializer{}
	setCalled := false
	field := &schema.Field{}
	field.Set = func(_ context.Context, _ reflect.Value, _ interface{}) error {
		setCalled = true
		return nil
	}

	err := s.Scan(context.Background(), field, reflect.Value{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setCalled {
		t.Error("Scan(nil) 不应该调用 field.Set")
	}
}

// TestGaussStringSerializer_Scan_Int 验证从数据库扫描非 string/[]byte 值通过 fmt.Sprintf 转换。
func TestGaussStringSerializer_Scan_Int(t *testing.T) {
	s := gaussStringSerializer{}
	var captured string
	field := &schema.Field{}
	field.Set = func(_ context.Context, _ reflect.Value, v interface{}) error {
		captured = v.(string)
		return nil
	}

	err := s.Scan(context.Background(), field, reflect.Value{}, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "42" {
		t.Errorf("got %q, want %q", captured, "42")
	}
}
