// Package models 实现 canonical model 映射（models.yaml）：
// 精确匹配 → 正则匹配，不做模糊匹配。
package models

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Mapping 是加载后的映射表。
type Mapping struct {
	// alias（provider 原生模型名）→ canonical
	aliasToCanonical map[string]string
	// canonical → 别名列表（保持文件顺序）
	canonicalAliases map[string][]string
	canonicalOrder   []string
	patterns         []pattern
}

type pattern struct {
	re        *regexp.Regexp
	canonical string
}

// rawFile 对应 models.yaml 结构：顶层 key = canonical name（patterns 除外）。
type rawPattern struct {
	Match     string `yaml:"match"`
	Canonical string `yaml:"canonical"`
}

// Load 解析 models.yaml。文件不存在返回空映射（不报错）。
func Load(path string) (*Mapping, error) {
	m := &Mapping{
		aliasToCanonical: map[string]string{},
		canonicalAliases: map[string][]string{},
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse models.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return m, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("models.yaml: top level must be a mapping")
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		if key == "patterns" {
			var raws []rawPattern
			if err := val.Decode(&raws); err != nil {
				return nil, fmt.Errorf("models.yaml patterns: %w", err)
			}
			for _, rp := range raws {
				re, err := regexp.Compile(rp.Match)
				if err != nil {
					return nil, fmt.Errorf("models.yaml pattern %q: %w", rp.Match, err)
				}
				m.patterns = append(m.patterns, pattern{re: re, canonical: rp.Canonical})
			}
			continue
		}
		var aliases []string
		if err := val.Decode(&aliases); err != nil {
			return nil, fmt.Errorf("models.yaml %s: %w", key, err)
		}
		m.canonicalAliases[key] = aliases
		m.canonicalOrder = append(m.canonicalOrder, key)
		for _, a := range aliases {
			m.aliasToCanonical[a] = key
		}
	}
	return m, nil
}

// CanonicalOf 将 provider 原生模型名归到 canonical name。
// 顺序：精确别名 → 正则 pattern；都不命中返回 ("", false)。
func (m *Mapping) CanonicalOf(native string) (string, bool) {
	if c, ok := m.aliasToCanonical[native]; ok {
		return c, true
	}
	for _, p := range m.patterns {
		if p.re.MatchString(native) {
			return p.canonical, true
		}
	}
	return "", false
}

// IsCanonical 报告 name 是否为精确 canonical 名。
func (m *Mapping) IsCanonical(name string) bool {
	_, ok := m.canonicalAliases[name]
	return ok
}

// Canonicals 返回全部 canonical name（文件顺序）。
func (m *Mapping) Canonicals() []string {
	return append([]string(nil), m.canonicalOrder...)
}

// Aliases 返回某 canonical 的别名列表。
func (m *Mapping) Aliases(canonical string) []string {
	return m.canonicalAliases[canonical]
}
