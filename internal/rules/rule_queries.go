package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TriggerKey 返回规则触发条件（when + if）的归一化签名。
// 两条规则签名相同 = 触发条件完全一致，会对同一批事件竞争匹配。
func (r *Rule) TriggerKey() string {
	w := &r.When
	var b strings.Builder
	fmt.Fprintf(&b, "t=%s;", w.Type())
	fmt.Fprintf(&b, "prov=%s;model=%s;sc=%s;bc=%s;br=%s;tr=%s;ttft=%d;err=%t;",
		w.Provider, w.Model, w.StatusCode, w.BodyContains, w.bodyRegexSrc,
		w.Transport, w.TTFTOverMs, w.ErrorAny)
	fmt.Fprintf(&b, "check=%s;cron=%s;scope=%s;", w.CheckStatus, w.Cron, w.ScopeProvider)
	// if 条件排序后拼接（顺序无关）
	conds := make([]string, 0, len(r.If))
	for _, c := range r.If {
		conds = append(conds, fmt.Sprintf("%s%s%d", c.Field, c.Op, c.Value))
	}
	sort.Strings(conds)
	b.WriteString("if=" + strings.Join(conds, ","))
	return b.String()
}

// CanMatchSuccess 该规则是否可能命中成功响应
// （无 error / transport / ttft_over_ms / 4xx / 5xx / 具体错误码条件 → 可能命中成功响应）。
func (r *Rule) CanMatchSuccess() bool {
	if r.When.Type() != "upstream" {
		return false
	}
	w := &r.When
	if w.ErrorAny || w.Transport != "" || w.TTFTOverMs > 0 {
		return false
	}
	if w.StatusCode != "" && w.StatusCode != "2xx" {
		// 4xx/5xx/具体非 2xx 错误码都不会命中成功响应
		if w.StatusCode == "4xx" || w.StatusCode == "5xx" {
			return false
		}
		if lo, hi, ok := parseStatusRange(w.StatusCode); ok {
			// 区间完全落在 2xx 之外 → 不会命中成功响应
			if lo > 299 || hi < 200 {
				return false
			}
		} else if n, err := strconv.Atoi(w.StatusCode); err == nil && (n < 200 || n >= 300) {
			return false
		}
	}
	return true
}

// NeedsBody 是否需要响应体参与匹配（决定 tee 旁路）。
func (r *Rule) NeedsBody() bool {
	return r.When.BodyContains != "" || r.When.BodyRegex != nil
}

// CapturesField 报告规则是否有 capture 动作会保存指定字段。
func (r *Rule) CapturesField(field string) bool {
	for _, action := range r.Do {
		if action.Kind != "capture" {
			continue
		}
		if len(action.Fields) == 0 {
			return true
		}
		for _, captured := range action.Fields {
			if captured == field {
				return true
			}
		}
	}
	return false
}

func validStatusPattern(s string) bool {
	if s == "4xx" || s == "5xx" || s == "2xx" || s == "3xx" || s == "1xx" {
		return true
	}
	if lo, hi, ok := parseStatusRange(s); ok {
		return lo >= 100 && hi <= 599
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 100 && n <= 599
}

func validTransport(s string) bool {
	switch s {
	case TransportDNSFailure, TransportConnRefused, TransportTLSError,
		TransportConnectTimeout, TransportStreamCut, TransportMalformed:
		return true
	}
	return false
}

func validRoute(s string) bool {
	switch s {
	case RouteNextKey, RouteNextProvider, RouteRetrySame, RoutePassthrough, RouteWait:
		return true
	}
	return false
}
