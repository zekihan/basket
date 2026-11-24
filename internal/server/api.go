package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/zekihan/go-commons/metrics"

	"github.com/zekihan/basket/internal/basket"
)

const maxRequestBody = 8 << 20

func (srv *Server) api(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var method string

	switch r.URL.Path {
	case "/api/snapshot", "/api/similar", "/api/export":
		method = http.MethodGet
	case "/api/ops":
		method = http.MethodPost
	default:
		writeError(w, http.StatusNotFound, "not_found", "API route not found.")

		return
	}

	if r.Method != method {
		w.Header().Set("Allow", method)
		writeError(w, http.StatusMethodNotAllowed, "invalid", "Method not allowed.")

		return
	}

	r = metrics.WithRoute(r, r.URL.Path)
	switch r.URL.Path {
	case "/api/snapshot":
		value, err := srv.deps.Service.Snapshot(r.Context())
		writeResult(w, value, err)
	case "/api/similar":
		value, err := srv.deps.Service.Similar(r.Context(), r.URL.Query().Get("title"))
		writeResult(w, struct {
			Items []basket.Item `json:"items"`
		}{value}, err)
	case "/api/export":
		snapshot, err := srv.deps.Service.Snapshot(r.Context())
		if err != nil {
			writeResult(w, nil, err)

			return
		}

		value, err := srv.deps.Service.Export(r.Context())
		if err == nil {
			w.Header().
				Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"basket-%s.json\"", snapshot.Today))
		}

		writeResult(w, value, err)
	case "/api/ops":
		srv.ops(w, r)
	}
}

func (srv *Server) ops(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ops []basket.Op `json:"ops"`
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()

	err := dec.Decode(&req)
	if err != nil || len(req.Ops) == 0 || len(req.Ops) > basket.MaxOpsPerRequest {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid",
			"Expected a valid non-empty operations batch.",
		)

		return
	}

	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid", "Expected a single JSON object.")

		return
	}

	results, err := srv.deps.Service.ApplyOps(r.Context(), basket.ActorPWA, req.Ops)
	if err != nil {
		writeResult(w, nil, err)

		return
	}

	snapshot, err := srv.deps.Service.Snapshot(r.Context())
	writeResult(w, struct {
		Results  []basket.OpResult `json:"results"`
		Snapshot basket.Snapshot   `json:"snapshot"`
	}{results, snapshot}, err)
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		slog.Error("API request failed", slog.Any("error", err))
		writeError(
			w,
			http.StatusInternalServerError,
			"internal",
			"The request could not be completed.",
		)

		return
	}

	writeJSON(w, http.StatusOK, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(value)
	if err != nil {
		slog.Error("Encode response", slog.Any("error", err))
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(
		w,
		status,
		map[string]any{"error": map[string]string{"code": code, "message": message}},
	)
}
