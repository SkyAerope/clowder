package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// Fingerprint 计算错误指纹：响应体去掉 UUID / 时间戳 / 数字后归一化哈希，
// 结合 provider + status code 作为分组键。
func Fingerprint(provider string, statusCode int, transport, body string) string {
	norm := body
	if len(norm) > 4096 {
		norm = norm[:4096] // 长 body 截断后归一化，足够区分错误种类
	}
	norm = reISOTs.ReplaceAllString(norm, "<ts>")
	norm = reUUID.ReplaceAllString(norm, "<uuid>")
	norm = reNumber.ReplaceAllString(norm, "<n>")
	norm = reSpaces.ReplaceAllString(norm, " ")
	h := sha256.Sum256([]byte(provider + "|" + strconv.Itoa(statusCode) + "|" + transport + "|" + norm))
	return hex.EncodeToString(h[:8]) // 64 bit 足够
}

// FingerprintOf 从事件计算指纹（仅错误事件有意义）。
func FingerprintOf(ev *Event) string {
	if !ev.IsError {
		return ""
	}
	return Fingerprint(ev.Provider, ev.StatusCode, ev.Transport, ev.Body)
}

// ValidationWarning 规则校验警告。
type ValidationWarning struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// ValidationContext 校验所需的外部信息。
type ValidationContext struct {
	// Providers: name → 是否定义了 check 脚本
	Providers map[string]bool
	// ProviderModels: provider → 当前生效的模型集合（canonical 不在此校验）
	ProviderModels map[string]map[string]bool
}

// Validate 对规则全集做持续校验（保存时 + 规则页展示）。
func Validate(all []*Rule, ctx *ValidationContext) []ValidationWarning {
	// 停用规则不参与校验（用户已主动关闭）。
	enabled := make([]*Rule, 0, len(all))
	for _, r := range all {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	all = enabled
	var warns []ValidationWarning
	add := func(rule, format string, args ...any) {
		warns = append(warns, ValidationWarning{Rule: rule, Message: fmt.Sprintf(format, args...)})
	}

	// 1. 触发条件（when + if）完全相同且 priority 也相同的规则 → 匹配结果取决于来源/定义顺序
	type sig struct {
		trigger string
		prio    int
	}
	seen := map[sig][]string{}
	for _, r := range all {
		k := sig{r.TriggerKey(), r.Priority}
		seen[k] = append(seen[k], r.Name)
	}
	for k, names := range seen {
		if len(names) > 1 {
			for _, n := range names {
				add(n, "与规则 %v 触发条件(when+if)与优先级(%d)均相同，匹配顺序取决于来源/定义顺序", names, k.prio)
			}
		}
	}

	for _, r := range all {
		// 2. check_key/check_keys 但作用域 provider 无 check 脚本
		for _, a := range r.Do {
			if a.Kind == "check_key" || a.Kind == "check_keys" {
				scope := r.When.Provider
				if scope == "" {
					scope = r.When.ScopeProvider
				}
				if ctx != nil {
					if scope != "" {
						if hasCheck, ok := ctx.Providers[scope]; ok && !hasCheck {
							add(r.Name, "provider %s 未定义 check 脚本，该规则的 %s 不会执行", scope, a.Kind)
						}
					} else {
						for p, hasCheck := range ctx.Providers {
							if !hasCheck {
								add(r.Name, "provider %s 未定义 check 脚本，该规则的 %s 对其不会执行", p, a.Kind)
							}
						}
					}
				}
			}
			// 3. check/schedule 触发器下 capture 无报文可抓
			if a.Kind == "capture" && r.When.Type() != "upstream" {
				add(r.Name, "%s 触发器下 capture 无报文可抓（no-op）", r.When.Type())
			}
		}
		// 4. 引用的 provider / model 不存在
		if ctx != nil {
			if p := r.When.Provider; p != "" {
				if _, ok := ctx.Providers[p]; !ok {
					add(r.Name, "引用的 provider %q 不存在", p)
				} else if m := r.When.Model; m != "" {
					if models, ok := ctx.ProviderModels[p]; ok && len(models) > 0 && !models[m] {
						add(r.Name, "provider %q 当前模型列表中没有 %q（已删除或被黑白名单排除）", p, m)
					}
				}
			}
			if sp := r.When.ScopeProvider; sp != "" {
				if _, ok := ctx.Providers[sp]; !ok {
					add(r.Name, "scope 引用的 provider %q 不存在", sp)
				}
			}
		}
	}
	return warns
}
