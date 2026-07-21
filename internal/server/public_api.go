package server

import (
	"net/http"
	"strings"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/health"
)

func registerPublicAPI(mux *http.ServeMux, public func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
	// 公开看板数据（绝不返回 key/email/password）
	mux.HandleFunc("GET /api/providers", public(func(w http.ResponseWriter, r *http.Request) {
		type pv struct {
			Name          string `json:"name"`
			DisplayName   string `json:"display_name"`
			Status        string `json:"status"`
			StatusReason  string `json:"status_reason,omitempty"`
			LastSlug      string `json:"last_slug,omitempty"`
			LastErrorAt   string `json:"last_error_at,omitempty"`
			LastSuccessAt string `json:"last_success_at,omitempty"`
			ActiveKeys    int    `json:"active_keys"`
			TotalKeys     int    `json:"total_keys"`
			AvgTTFTMs     int64  `json:"avg_ttft_ms"`
			Priority      int    `json:"priority"`
			ModelCount    int    `json:"model_count"`
		}
		var out []pv
		providerHealth := map[string]health.Snapshot{}
		for _, snapshot := range a.Health.Snapshots() {
			if snapshot.Model == "" {
				providerHealth[snapshot.Provider] = snapshot
			}
		}
		includeStatusReason := a.Cfg.Dashboard.Admin && validSession(r)
		for name, ps := range a.ConfiguredProviders() {
			usable, total := a.Pool.UsableCount(name)
			var ttftSum, ttftN int64
			seenModels := map[string]bool{}
			for _, native := range ps.Models {
				if seenModels[native] {
					continue
				}
				seenModels[native] = true
				if t, ok := a.Health.AvgTTFT(name, native); ok {
					ttftSum += t
					ttftN++
				}
			}
			var avg int64
			if ttftN > 0 {
				avg = ttftSum / ttftN
			}
			status, statusReason := "disabled", ""
			if ps.Manifest.Enabled {
				status = a.Health.Status(name, "")
				var reasons []string
				if usable == 0 {
					reasons = append(reasons, "无可用 Key")
				}
				if len(ps.Models) == 0 {
					reasons = append(reasons, "无可用模型")
				}
				if len(reasons) > 0 {
					status = health.Unhealthy
					statusReason = strings.Join(reasons, "；")
				} else if status == health.Unhealthy {
					statusReason = "健康检查失败"
				}
			}
			if !includeStatusReason {
				statusReason = ""
			}
			snapshot := providerHealth[name]
			out = append(out, pv{
				Name: name, DisplayName: ps.Manifest.DisplayName,
				Status: status, StatusReason: statusReason,
				LastSlug: snapshot.LastSlug, LastErrorAt: snapshot.LastError,
				LastSuccessAt: snapshot.LastSuccess,
				ActiveKeys:    usable, TotalKeys: total,
				AvgTTFTMs: avg, Priority: ps.Manifest.Routing.Priority,
				ModelCount: len(ps.Models),
			})
		}
		jsonOK(w, out)
	}))

	mux.HandleFunc("GET /api/models", public(func(w http.ResponseWriter, r *http.Request) {
		// 模型路由图：canonical → 各 provider 的 native 名；空 canonical 表示未映射。
		type edge struct {
			Provider            string `json:"provider"`
			ProviderDisplayName string `json:"provider_display_name"`
			Native              string `json:"native"`
			TTFTMs              int64  `json:"ttft_ms"`
			HasTTFT             bool   `json:"has_ttft"`
			Healthy             bool   `json:"healthy"`
			Best                bool   `json:"best"`
		}
		out := map[string][]edge{}
		providers := a.Providers()
		mapping := a.MappingSnapshot()
		for _, canonical := range mapping.Canonicals() {
			cands := a.CandidatesFor(canonical)
			bestProvider, bestNative := "", ""
			if len(cands) > 0 {
				bestProvider, bestNative = cands[0].Provider, cands[0].Native
			}
			var edges []edge
			for name, ps := range providers {
				for _, native := range ps.NativesByCanonical[canonical] {
					t, has := a.Health.AvgTTFT(name, native)
					edges = append(edges, edge{
						Provider: name, ProviderDisplayName: ps.Manifest.DisplayName,
						Native: native, TTFTMs: t, HasTTFT: has,
						Healthy: a.ModelRoutable(name, native),
						Best:    name == bestProvider && native == bestNative,
					})
				}
			}
			if len(edges) > 0 {
				out[canonical] = edges
			}
		}
		for name, ps := range providers {
			seenModels := map[string]bool{}
			for _, native := range ps.Models {
				if seenModels[native] || len(mapping.CanonicalsOf(native)) > 0 {
					continue
				}
				seenModels[native] = true
				t, has := a.Health.AvgTTFT(name, native)
				out[""] = append(out[""], edge{
					Provider:            name,
					ProviderDisplayName: ps.Manifest.DisplayName,
					Native:              native,
					TTFTMs:              t,
					HasTTFT:             has,
					Healthy:             a.ModelRoutable(name, native),
				})
			}
		}
		jsonOK(w, out)
	}))

	mux.HandleFunc("GET /api/auto", public(func(w http.ResponseWriter, r *http.Request) {
		cands := a.CandidatesAuto()
		if len(cands) > 3 {
			cands = cands[:3]
		}
		jsonOK(w, cands)
	}))

	mux.HandleFunc("GET /api/health", public(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, a.Health.Snapshots())
	}))
}
