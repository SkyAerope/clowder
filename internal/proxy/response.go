package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/rules"
)

// writeErrorToClient passthrough：透传上游错误；超时类无响应可透 → 合成 504。
func (s *Server) writeErrorToClient(w http.ResponseWriter, o *attemptOutcome, cand app.Candidate) {
	if o.event.TTFTTimeout || o.event.Transport == rules.TransportConnectTimeout {
		writeJSONError(w, http.StatusGatewayTimeout,
			fmt.Sprintf("upstream timeout (provider=%s, model=%s)", o.event.Provider, o.event.Model),
			"timeout")
		return
	}
	if o.event.StatusCode != 0 {
		for k, vs := range o.respHeaderMap {
			if skipHopHeader(k) {
				continue
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(o.respBody)))
		w.WriteHeader(o.event.StatusCode)
		w.Write(o.respBody)
		return
	}
	writeJSONError(w, http.StatusBadGateway,
		fmt.Sprintf("upstream transport error: %s (provider=%s)", o.event.Transport, o.event.Provider),
		"upstream_error")
}

func rewriteModelField(body []byte, reqModel string) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	m["model"] = reqModel
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func headersJSON(h http.Header) string {
	m := make(map[string]string, len(h))
	for k, vs := range h {
		m[k] = strings.Join(vs, ", ")
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func skipHopHeader(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "keep-alive", "transfer-encoding", "content-length", "te", "trailer", "upgrade":
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, message, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(errorJSON(message, typ))
}
