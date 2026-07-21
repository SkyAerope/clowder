package app

import (
	"encoding/json"

	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

// saveCapture 按 fields 裁剪保存抓包（可选脱敏）。
func (a *App) saveCapture(act rules.Action, actx *ActionContext) {
	want := func(f string) bool {
		if len(act.Fields) == 0 {
			return true // 默认全抓
		}
		for _, x := range act.Fields {
			if x == f {
				return true
			}
		}
		return false
	}
	c := &store.Capture{
		RequestID:   actx.RequestID,
		MatchedRule: actx.Rule.Name,
		Fingerprint: actx.Fingerprint,
		Provider:    actx.Provider,
		Model:       actx.Model,
	}
	if want("request_headers") {
		c.RequestHeaders = actx.ReqHeaders
	}
	if want("request_body") {
		c.RequestBody = actx.ReqBody
	}
	if want("response_headers") {
		c.ResponseHeaders = actx.RespHeaders
	}
	if want("response_body") {
		c.ResponseBody = actx.RespBody
	}
	if a.Cfg.Capture.Redact {
		c.RequestHeaders = redactSecrets(c.RequestHeaders)
		c.ResponseHeaders = redactSecrets(c.ResponseHeaders)
		c.Redacted = true
	}
	if _, err := a.Store.InsertCapture(c, a.Cfg.Capture.MaxRecords); err != nil {
		a.Log.Warn("save capture", "error", err)
	}
}

// redactSecrets 脱敏：authorization / api-key 类 header 只留前 8 位。
func redactSecrets(headersJSON string) string {
	if headersJSON == "" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &m); err != nil {
		return headersJSON
	}
	for k, v := range m {
		lk := lower(k)
		if lk == "authorization" || lk == "x-api-key" || lk == "api-key" {
			if len(v) > 8 {
				m[k] = v[:8] + "..."
			}
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
