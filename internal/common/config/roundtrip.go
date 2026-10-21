// Package config 的 round-trip 写回实现见各函数。
// 包注释见 doc.go（多文件包注释只在 doc.go 一处）。
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetRoundTrip 更新指定路径的值并 round-trip 写回文件（保留注释与格式）。
//
// 对齐 Python ruamel round-trip（common/config.py L126-143）：
//   - _load_yaml_round_trip → 定位嵌套节点 → 修改 value → _dump_yaml_round_trip
//   - preserve_quotes=True：保留原引号风格
//   - 非 ruamel mapping/list 结构（中间节点非 map）时覆盖为 map
//   - 文件不存在时自动创建（T-11 延续：Python ruamel 抛 FileNotFoundError，
//     Go 行为更健壮，有意增强）
//
// 与 Set 的区别：Set 走 yaml.Marshal 全量重写，丢注释与格式；SetRoundTrip
// 走 yaml.v3 Node 模式原样写回。
//
// key 支持点分隔路径，如 "modes.agent.code.memory.enabled"。
func (c *Config) SetRoundTrip(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 读取原始文件内容（未解析环境变量，round-trip 必须基于原始文本）
	content, err := os.ReadFile(c.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("读取配置文件失败: %w", err)
		}
		// T-11: 文件不存在时自动创建（Python ruamel 抛 FileNotFoundError）
		content = nil
	}

	// 解析为 Node 树（保留注释/引号/格式）
	var root yaml.Node
	if len(content) > 0 {
		if err := yaml.Unmarshal(content, &root); err != nil {
			return fmt.Errorf("解析配置文件失败: %w", err)
		}
	}

	// 空文件（或新建）：Unmarshal 后 root.Kind == 0，初始化为 document + mapping
	doc := &root
	if doc.Kind == yaml.DocumentNode {
		doc = doc.Content[0]
	}
	if doc.Kind == 0 || doc.Kind == yaml.ScalarNode && doc.Tag == "!!null" {
		// 空文档：构造 mapping 根
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}}}
		doc = root.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("配置文件根节点不是 mapping: %v", doc.Kind)
	}

	parts := strings.Split(key, ".")
	if err := setNodeValue(doc, parts, value); err != nil {
		return err
	}

	// 序列化 Node 树原样写回（yaml.v3 Node Encoder 保留 HeadComment/LineComment 等）。
	// SetIndent(2) 对齐 Python ruamel indent(mapping=2) 的 2 空格缩进。
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("关闭编码器失败: %w", err)
	}
	out := buf.Bytes()

	// 确保目录存在（对齐 saveLocked）
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	if err := os.WriteFile(c.path, out, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	// 同步内存缓存（data/raw），保持 Get 读到最新值
	if err := c.reloadAfterRoundTrip(); err != nil {
		return err
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reloadAfterRoundTrip round-trip 写回后同步内存缓存（调用方必须持有写锁）。
func (c *Config) reloadAfterRoundTrip() error {
	content, err := os.ReadFile(c.path)
	if err != nil {
		return nil // 写回已成功，缓存同步失败不回滚（下次 Load 会重读）
	}
	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil // 同上，容忍缓存同步失败
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	c.raw = raw
	c.data = ResolveEnvVars(c.raw, c.decryptFn).(map[string]any)
	if c.normFn != nil {
		c.normFn(c.data)
	}
	return nil
}

// setNodeValue 在 Node 树中设置指定路径的值（中间节点不存在时自动创建 mapping）。
// 对齐 setNestedValue 语义，但操作对象是 yaml.Node。
func setNodeValue(m *yaml.Node, parts []string, value any) error {
	current := m
	for i, part := range parts {
		// 在当前 mapping 中查找 key 节点
		var valNode *yaml.Node
		found := false
		for j := 0; j+1 < len(current.Content); j += 2 {
			if current.Content[j].Value == part {
				valNode = current.Content[j+1]
				found = true
				break
			}
		}

		if i == len(parts)-1 {
			// 最后一个节点：设置/覆盖值
			valueNode, err := valueToNode(value)
			if err != nil {
				return err
			}
			if found {
				// 保留 key 节点与 value 节点上的注释（LineComment 挂在 value 节点上），
				// 只替换值本身：Kind/Tag/Value/Style/Content
				lineComment := valNode.LineComment
				headComment := valNode.HeadComment
				footComment := valNode.FootComment
				*valNode = *valueNode
				valNode.LineComment = lineComment
				valNode.HeadComment = headComment
				valNode.FootComment = footComment
			} else {
				current.Content = append(current.Content, &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: part,
				}, valueNode)
			}
			return nil
		}

		// 中间节点：确保是 mapping
		if !found {
			// 不存在，创建新的 mapping 节点
			newMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			current.Content = append(current.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: part,
			}, newMap)
			current = newMap
			continue
		}
		if valNode.Kind == yaml.MappingNode {
			current = valNode
			continue
		}
		// 已存在但不是 mapping（如标量），覆盖为新的 mapping（保留其注释）
		newMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		lineComment := valNode.LineComment
		headComment := valNode.HeadComment
		footComment := valNode.FootComment
		*valNode = *newMap
		valNode.LineComment = lineComment
		valNode.HeadComment = headComment
		valNode.FootComment = footComment
		current = valNode
	}
	return nil
}

// valueToNode 将 Go 值转换为 yaml.Node。
// bool → !!bool 标量；string → !!str 标量；map/slice → 递归构建。
func valueToNode(value any) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, fmt.Errorf("编码值失败: %w", err)
	}
	return &node, nil
}
