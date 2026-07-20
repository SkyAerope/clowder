package server

import (
	"encoding/json"
	"net/http"

	"github.com/SkyAerope/clowder/internal/app"
)

func registerModelCheckAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, application *app.App) {
	mux.HandleFunc("POST /api/admin/providers/{name}/check-model", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
			jsonErr(w, http.StatusBadRequest, "model is required")
			return
		}
		result, err := application.CheckModel(r.Context(), r.PathValue("name"), body.Model)
		if err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonOK(w, map[string]any{
			"status": result.Status, "key_id": result.KeyID,
			"model_response": result.ModelResponse, "ttft_ms": result.TTFTMS,
			"rule_name": result.RuleName, "status_code": result.StatusCode,
			"response_body": result.ResponseBody,
		})
	}))

	mux.HandleFunc("POST /api/admin/providers/{name}/apply-model-check-rule", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model        string `json:"model"`
			KeyID        int64  `json:"key_id"`
			RuleName     string `json:"rule_name"`
			StatusCode   int    `json:"status_code"`
			ResponseBody string `json:"response_body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" || body.KeyID == 0 || body.RuleName == "" {
			jsonErr(w, http.StatusBadRequest, "model, key_id and rule_name are required")
			return
		}
		if err := application.ApplyModelCheckRule(app.ModelCheckRuleApplication{
			ProviderName: r.PathValue("name"), NativeModel: body.Model, KeyID: body.KeyID,
			RuleName: body.RuleName, StatusCode: body.StatusCode, ResponseBody: body.ResponseBody,
		}); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
}
