package rules

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// rawRule 是 rules.yaml / manifest rules 的原始结构。
type rawRule struct {
	Slug     string
	Name     string           `yaml:"name"`
	Priority *int             `yaml:"priority"`
	Enabled  *bool            `yaml:"enabled"`
	When     map[string]any   `yaml:"when"`
	If       map[string]any   `yaml:"if"`
	Do       []map[string]any `yaml:"do"`
	Route    any              `yaml:"route"`
}

// ParseRules 从 YAML 字节解析规则列表（rules.yaml 顶层为 {rules: [...]}）。
func ParseRules(data []byte, source int) ([]*Rule, error) {
	var doc struct {
		Rules []map[string]rawRule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	raws, err := keyedRules(doc.Rules)
	if err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	return buildRules(raws, source, "")
}

// ParseManifestRules 解析 manifest 内嵌规则（自动附加 provider 作用域）。
func ParseManifestRules(raw []map[string]any, providerName string) ([]*Rule, error) {
	rrs := make([]rawRule, 0, len(raw))
	for _, m := range raw {
		if len(m) != 1 {
			return nil, fmt.Errorf("rule must be a mapping with one slug key")
		}
		var slug string
		var value any
		for key, entry := range m {
			slug, value = key, entry
		}
		b, err := yaml.Marshal(value)
		if err != nil {
			return nil, err
		}
		var rr rawRule
		if err := yaml.Unmarshal(b, &rr); err != nil {
			return nil, err
		}
		rr.Slug = slug
		rrs = append(rrs, rr)
	}
	return buildRules(rrs, SourceManifest, providerName)
}

func keyedRules(entries []map[string]rawRule) ([]rawRule, error) {
	raws := make([]rawRule, 0, len(entries))
	for _, entry := range entries {
		if len(entry) != 1 {
			return nil, fmt.Errorf("rule must be a mapping with one slug key")
		}
		for slug, rr := range entry {
			rr.Slug = slug
			raws = append(raws, rr)
		}
	}
	return raws, nil
}

func buildRules(raws []rawRule, source int, forceProvider string) ([]*Rule, error) {
	var out []*Rule
	for i, rr := range raws {
		r, err := buildRule(rr, source)
		if err != nil {
			slug := rr.Slug
			if slug == "" {
				slug = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("rule %s: %w", slug, err)
		}
		if forceProvider != "" && r.When.Type() == "upstream" {
			r.When.Provider = forceProvider // manifest 规则强制限定本 provider
		}
		out = append(out, r)
	}
	return out, nil
}

func buildRule(rr rawRule, source int) (*Rule, error) {
	if rr.Slug == "" {
		return nil, fmt.Errorf("missing slug")
	}
	r := &Rule{
		Slug:     rr.Slug,
		Name:     rr.Name,
		Priority: 100,
		Source:   source,
		Enabled:  rr.Enabled == nil || *rr.Enabled,
	}
	if rr.Priority != nil {
		r.Priority = *rr.Priority
	}
	if err := parseWhen(rr.When, &r.When); err != nil {
		return nil, err
	}
	conds, err := parseIf(rr.If)
	if err != nil {
		return nil, err
	}
	r.If = conds
	actionKinds := make(map[string]struct{}, len(rr.Do))
	for _, dm := range rr.Do {
		a, err := parseAction(dm)
		if err != nil {
			return nil, err
		}
		if _, exists := actionKinds[a.Kind]; exists {
			return nil, fmt.Errorf("duplicate action %q", a.Kind)
		}
		actionKinds[a.Kind] = struct{}{}
		r.Do = append(r.Do, a)
	}
	rt, err := parseRoute(rr.Route)
	if err != nil {
		return nil, err
	}
	r.Route = rt
	if r.Route.IsEmpty() && r.When.Type() == "upstream" {
		r.Route = RouteSpec{Single: RoutePassthrough} // 新建规则默认透传
	}
	return r, nil
}
