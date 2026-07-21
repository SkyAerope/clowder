package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/store"
)

func registerActivityAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
	mux.HandleFunc("GET /api/admin/requests", admin(func(w http.ResponseWriter, r *http.Request) {
		limit, offset := queryInt(r, "limit", 50), queryInt(r, "offset", 0)
		filters, err := parseRequestFilters(r)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		logs, err := a.Store.ListRequests(filters, limit, offset)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		views, err := buildRequestLogViews(a.Store, logs)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, views)
	}))
	mux.HandleFunc("GET /api/admin/errors", admin(func(w http.ResponseWriter, r *http.Request) {
		groups, err := a.Store.AggregateErrors(r.URL.Query().Get("rule"), queryInt(r, "limit", 100))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, groups)
	}))
	mux.HandleFunc("GET /api/admin/captures", admin(func(w http.ResponseWriter, r *http.Request) {
		caps, err := a.Store.ListCaptures(r.URL.Query().Get("rule"), r.URL.Query().Get("fingerprint"), queryInt(r, "limit", 20))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, caps)
	}))
}

// requestLogView 是请求日志列表的一行：随列表内联返回完整重试链与抓包，前端展开无需二次请求。
type requestLogView struct {
	*store.RequestLog
	Trace   []*store.RequestLog `json:"trace"`
	Capture *store.Capture      `json:"capture"`
}

func buildRequestLogViews(st *store.Store, logs []*store.RequestLog) ([]requestLogView, error) {
	ids := make([]int64, len(logs))
	traceIDSet := make(map[string]struct{}, len(logs))
	for i, log := range logs {
		ids[i] = log.ID
		traceIDSet[log.TraceID] = struct{}{}
	}
	captures, err := st.CapturesByRequest(ids)
	if err != nil {
		return nil, err
	}
	traces := make(map[string][]*store.RequestLog, len(traceIDSet))
	for traceID := range traceIDSet {
		trace, err := st.ListTrace(traceID)
		if err != nil {
			return nil, err
		}
		traces[traceID] = trace
	}
	views := make([]requestLogView, len(logs))
	for i, log := range logs {
		views[i] = requestLogView{RequestLog: log, Trace: traces[log.TraceID], Capture: captures[log.ID]}
	}
	return views, nil
}

func parseRequestFilters(r *http.Request) (store.RequestFilters, error) {
	query := r.URL.Query()
	filters := store.RequestFilters{
		Provider:    query.Get("provider"),
		Model:       query.Get("model"),
		MatchedSlug: query.Get("rule"),
		Fingerprint: strings.ToLower(query.Get("fingerprint")),
	}
	from, err := parseRequestTime(query.Get("from"), "from")
	if err != nil {
		return store.RequestFilters{}, err
	}
	to, err := parseRequestTime(query.Get("to"), "to")
	if err != nil {
		return store.RequestFilters{}, err
	}
	if from != nil && to != nil && from.After(*to) {
		return store.RequestFilters{}, fmt.Errorf("from must not be after to")
	}
	filters.From, filters.To = from, to

	if value := query.Get("status"); value != "" {
		status, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return store.RequestFilters{}, fmt.Errorf("invalid status")
		}
		if status < 100 || status > 599 {
			return store.RequestFilters{}, fmt.Errorf("status must be between 100 and 599")
		}
		filters.StatusCode = &status
	}

	if value := query.Get("error"); value != "" {
		var isError bool
		switch value {
		case "true":
			isError = true
		case "false":
			isError = false
		default:
			return store.RequestFilters{}, fmt.Errorf("invalid error")
		}
		filters.Error = &isError
	}

	if value := query.Get("captured"); value != "" {
		var captured bool
		switch value {
		case "true":
			captured = true
		case "false":
			captured = false
		default:
			return store.RequestFilters{}, fmt.Errorf("invalid captured")
		}
		filters.Captured = &captured
	}
	return filters, nil
}

func parseRequestTime(value, name string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s", name)
	}
	utc := parsed.UTC()
	return &utc, nil
}
