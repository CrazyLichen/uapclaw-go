package utils

import (
	"testing"
)

// ──────────────────────────── CreateNestedDict ────────────────────────────

func TestCreateNestedDict(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		value     any
		separator string
		want      map[string]any
	}{
		{
			name:  "simple two-level",
			path:  "a.b",
			value: 1,
			want:  map[string]any{"a": map[string]any{"b": 1}},
		},
		{
			name:  "three-level",
			path:  "a.b.c",
			value: "hello",
			want:  map[string]any{"a": map[string]any{"b": map[string]any{"c": "hello"}}},
		},
		{
			name:  "single key",
			path:  "key",
			value: 42,
			want:  map[string]any{"key": 42},
		},
		{
			name:  "empty path",
			path:  "",
			value: "x",
			want:  nil,
		},
		{
			name:      "custom separator",
			path:      "a/b/c",
			value:     true,
			separator: "/",
			want:      map[string]any{"a": map[string]any{"b": map[string]any{"c": true}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			if tt.separator != "" {
				got = CreateNestedDict(tt.path, tt.value, tt.separator)
			} else {
				got = CreateNestedDict(tt.path, tt.value)
			}
			if !mapsEqual(got, tt.want) {
				t.Fatalf("CreateNestedDict(%q, %v) = %v, want %v", tt.path, tt.value, got, tt.want)
			}
		})
	}
}

// ──────────────────────────── FlattenDict / ExtractLeafNodes ────────────────────────────

func TestExtractLeafNodes(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want []LeafNode
	}{
		{
			name: "flat dict",
			data: map[string]any{"a": 1, "b": "hello"},
			want: []LeafNode{
				{Path: []string{"a"}, Value: 1},
				{Path: []string{"b"}, Value: "hello"},
			},
		},
		{
			name: "nested dict",
			data: map[string]any{"a": map[string]any{"b": 2}},
			want: []LeafNode{
				{Path: []string{"a", "b"}, Value: 2},
			},
		},
		{
			name: "dict with array",
			data: map[string]any{"a": []any{1, map[string]any{"b": 2}}},
			want: []LeafNode{
				{Path: []string{"a", "[0]"}, Value: 1},
				{Path: []string{"a", "[1]", "b"}, Value: 2},
			},
		},
		{
			name: "nil input",
			data: nil,
			want: nil,
		},
		{
			name: "empty dict",
			data: map[string]any{},
			want: []LeafNode(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLeafNodes(tt.data)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractLeafNodes() got %d nodes, want %d", len(got), len(tt.want))
			}
			// 转为 map 方便比较（不依赖顺序）
			gotMap := leafNodesToMap(got)
			wantMap := leafNodesToMap(tt.want)
			for k, v := range wantMap {
				if gotMap[k] != v {
					t.Fatalf("leaf node at path %q: got %v, want %v", k, gotMap[k], v)
				}
			}
		})
	}
}

func TestFlattenDict(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": 1,
			"c": "hello",
		},
		"d": []any{10, 20},
	}

	got := FlattenDict(data)

	// 验证关键路径存在
	if got["a.b"] != 1 {
		t.Fatalf("FlattenDict()[\"a.b\"] = %v, want 1", got["a.b"])
	}
	if got["a.c"] != "hello" {
		t.Fatalf("FlattenDict()[\"a.c\"] = %v, want \"hello\"", got["a.c"])
	}
	if got["d[0]"] != 10 {
		t.Fatalf("FlattenDict()[\"d[0]\"] = %v, want 10", got["d[0]"])
	}
	if got["d[1]"] != 20 {
		t.Fatalf("FlattenDict()[\"d[1]\"] = %v, want 20", got["d[1]"])
	}
}

// ──────────────────────────── RebuildDict ────────────────────────────

func TestRebuildDict(t *testing.T) {
	pairs := []LeafNode{
		{Path: []string{"a", "b"}, Value: 1},
		{Path: []string{"a", "c"}, Value: "hello"},
		{Path: []string{"d"}, Value: true},
	}

	got := RebuildDict(pairs)

	a, ok := got["a"].(map[string]any)
	if !ok {
		t.Fatal("got[\"a\"] is not a map")
	}
	if a["b"] != 1 {
		t.Fatalf("got[\"a\"][\"b\"] = %v, want 1", a["b"])
	}
	if a["c"] != "hello" {
		t.Fatalf("got[\"a\"][\"c\"] = %v, want \"hello\"", a["c"])
	}
	if got["d"] != true {
		t.Fatalf("got[\"d\"] = %v, want true", got["d"])
	}
}

// ──────────────────────────── RemoveZeroValues ────────────────────────────

func TestRemoveZeroValues(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want map[string]any
	}{
		{
			name: "removes nil and empty string",
			data: map[string]any{"a": nil, "b": "", "c": "hello"},
			want: map[string]any{"c": "hello"},
		},
		{
			name: "removes zero numbers",
			data: map[string]any{"a": 0, "b": 0.0, "c": 42},
			want: map[string]any{"c": 42},
		},
		{
			name: "removes false boolean",
			data: map[string]any{"a": false, "b": true},
			want: map[string]any{"b": true},
		},
		{
			name: "removes empty collections",
			data: map[string]any{"a": []any{}, "b": map[string]any{}, "c": []any{1}},
			want: map[string]any{"c": []any{1}},
		},
		{
			name: "recursive map cleaning",
			data: map[string]any{
				"a": map[string]any{"x": nil, "y": 1},
				"b": map[string]any{"z": ""}, // 整个 b 清理后为空，应移除
			},
			want: map[string]any{"a": map[string]any{"y": 1}},
		},
		{
			name: "nil input",
			data: nil,
			want: nil,
		},
		{
			name: "all zero values",
			data: map[string]any{"a": nil, "b": "", "c": 0},
			want: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveZeroValues(tt.data)
			if !mapsEqual(got, tt.want) {
				t.Fatalf("RemoveZeroValues() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── ValidateArgs ────────────────────────────

func TestValidateArgs(t *testing.T) {
	tests := []struct {
		name    string
		schema  map[string]any
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "empty schema accepts anything",
			schema:  map[string]any{},
			args:    map[string]any{"anything": "goes"},
			wantErr: false,
		},
		{
			name: "required field present",
			schema: map[string]any{
				"required": []any{"name"},
			},
			args:    map[string]any{"name": "test"},
			wantErr: false,
		},
		{
			name: "required field missing",
			schema: map[string]any{
				"required": []any{"name"},
			},
			args:    map[string]any{"age": 10},
			wantErr: true,
		},
		{
			name: "type check string",
			schema: map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			args:    map[string]any{"name": "hello"},
			wantErr: false,
		},
		{
			name: "type check string fails",
			schema: map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			args:    map[string]any{"name": 42},
			wantErr: true,
		},
		{
			name: "type check integer",
			schema: map[string]any{
				"properties": map[string]any{
					"age": map[string]any{"type": "integer"},
				},
			},
			args:    map[string]any{"age": float64(25)},
			wantErr: false,
		},
		{
			name: "type check integer fails with fraction",
			schema: map[string]any{
				"properties": map[string]any{
					"age": map[string]any{"type": "integer"},
				},
			},
			args:    map[string]any{"age": 25.5},
			wantErr: true,
		},
		{
			name: "type check boolean",
			schema: map[string]any{
				"properties": map[string]any{
					"active": map[string]any{"type": "boolean"},
				},
			},
			args:    map[string]any{"active": true},
			wantErr: false,
		},
		{
			name: "enum check passes",
			schema: map[string]any{
				"properties": map[string]any{
					"color": map[string]any{
						"type": "string",
						"enum": []any{"red", "green", "blue"},
					},
				},
			},
			args:    map[string]any{"color": "red"},
			wantErr: false,
		},
		{
			name: "enum check fails",
			schema: map[string]any{
				"properties": map[string]any{
					"color": map[string]any{
						"type": "string",
						"enum": []any{"red", "green", "blue"},
					},
				},
			},
			args:    map[string]any{"color": "yellow"},
			wantErr: true,
		},
		{
			name: "additional properties rejected by default",
			schema: map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			args:    map[string]any{"name": "test", "extra": "field"},
			wantErr: true,
		},
		{
			name: "additional properties allowed when set",
			schema: map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"additionalProperties": true,
			},
			args:    map[string]any{"name": "test", "extra": "field"},
			wantErr: false,
		},
		{
			name: "nested object validation",
			schema: map[string]any{
				"properties": map[string]any{
					"config": map[string]any{
						"type": "object",
						"required": []any{"port"},
						"properties": map[string]any{
							"port": map[string]any{"type": "integer"},
						},
					},
				},
			},
			args:    map[string]any{"config": map[string]any{"port": float64(8080)}},
			wantErr: false,
		},
		{
			name: "nested object validation fails",
			schema: map[string]any{
				"properties": map[string]any{
					"config": map[string]any{
						"type": "object",
						"required": []any{"port"},
						"properties": map[string]any{
							"port": map[string]any{"type": "integer"},
						},
					},
				},
			},
			args:    map[string]any{"config": map[string]any{}},
			wantErr: true,
		},
		{
			name: "array validation",
			schema: map[string]any{
				"properties": map[string]any{
					"tags": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			},
			args:    map[string]any{"tags": []any{"a", "b"}},
			wantErr: false,
		},
		{
			name: "array element validation fails",
			schema: map[string]any{
				"properties": map[string]any{
					"tags": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			},
			args:    map[string]any{"tags": []any{"a", 42}},
			wantErr: true,
		},
		{
			name:    "nil args treated as empty",
			schema:  map[string]any{"required": []any{"name"}},
			args:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateArgs(tt.schema, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// mapsEqual 深度比较两个 map[string]any
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		switch vaTyped := va.(type) {
		case map[string]any:
			vbMap, ok := vb.(map[string]any)
			if !ok || !mapsEqual(vaTyped, vbMap) {
				return false
			}
		case []any:
			vbSlice, ok := vb.([]any)
			if !ok || len(vaTyped) != len(vbSlice) {
				return false
			}
			for i := range vaTyped {
				if vaTyped[i] != vbSlice[i] {
					return false
				}
			}
		default:
			if va != vb {
				return false
			}
		}
	}
	return true
}

// leafNodesToMap 将叶子节点列表转为路径字符串→值的映射
func leafNodesToMap(nodes []LeafNode) map[string]any {
	m := make(map[string]any, len(nodes))
	for _, n := range nodes {
		m[formatPath(n.Path)] = n.Value
	}
	return m
}
