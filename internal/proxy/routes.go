package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/google/uuid"
)

// Server 是代理 HTTP 层。
type Server struct {
	App *app.App
}

// New 创建代理服务。
func New(a *app.App) *Server { return &Server{App: a} }

// Routes 注册 /v1/* 端点。
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.handleChat))
	mux.HandleFunc("GET /v1/models", s.auth(s.handleModels))
}

// auth /v1/* 始终需要 api_key 认证。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || key != s.App.Cfg.APIKey {
			writeJSONError(w, http.StatusUnauthorized, "invalid api key", "authentication_error")
			return
		}
		next(w, r)
	}
}

// handleModels 返回 canonical、配置别名、provider 原生模型名及 auto 的展平列表。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	list := []model{{ID: "auto", Object: "model", OwnedBy: "clowder"}}
	for _, name := range s.App.ModelNames() {
		list = append(list, model{ID: name, Object: "model", OwnedBy: "clowder"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
}

// handleChat 核心代理入口。
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read request body: "+err.Error(), "invalid_request_error")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request_error")
		return
	}
	reqModel, _ := payload["model"].(string)
	if reqModel == "" {
		writeJSONError(w, http.StatusBadRequest, "missing model", "invalid_request_error")
		return
	}
	clientStream, _ := payload["stream"].(bool)

	var candidates []app.Candidate
	if reqModel == "auto" {
		candidates = s.App.CandidatesAuto()
	} else {
		var found bool
		candidates, found = s.resolveCandidates(reqModel)
		if !found {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("model %q not found", reqModel), "invalid_request_error")
			return
		}
	}
	if len(candidates) == 0 {
		writeJSONError(w, http.StatusServiceUnavailable, "no healthy provider for model "+reqModel, "service_unavailable")
		return
	}

	chain := &chainState{traceID: uuid.NewString()}
	s.runChain(w, r, chain, candidates, payload, clientStream, reqModel)
}

func (s *Server) resolveCandidates(reqModel string) ([]app.Candidate, bool) {
	mapping := s.App.MappingSnapshot()
	if mapping.IsCanonical(reqModel) {
		return s.App.CandidatesFor(reqModel), true
	}
	model, providerName, qualified := s.qualifiedModel(reqModel)
	if qualified {
		if mapping.IsCanonical(model) {
			return s.App.CandidatesForProvider(model, providerName)
		}
		return s.App.CandidatesForNativeProvider(model, providerName)
	}
	return s.App.CandidatesForNative(reqModel)
}

func (s *Server) qualifiedModel(reqModel string) (model, providerName string, ok bool) {
	for name := range s.App.Providers() {
		suffix := ":" + name
		if len(name) > len(providerName) && strings.HasSuffix(reqModel, suffix) {
			model = strings.TrimSuffix(reqModel, suffix)
			providerName = name
		}
	}
	return model, providerName, model != "" && providerName != ""
}
