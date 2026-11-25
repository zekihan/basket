//nolint:gocyclo,gocognit,maintidx,lll,exhaustruct_v5,noctx,gosec,varnamelen // Integration fixture exercises the HTTP contract.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zekihan/go-commons/oidcauth"

	"github.com/zekihan/basket/internal/basket"
	"github.com/zekihan/basket/internal/store"
)

func TestHTTP(t *testing.T) {
	st, err := store.Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	var issuer string

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(
			w,
			`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			issuer,
			issuer+"/auth",
			issuer+"/token",
			issuer+"/keys",
		)
	}))
	defer idp.Close()

	issuer = idp.URL

	a, err := oidcauth.New(
		t.Context(),
		&oidcauth.Config[oidcauth.IdentityInfo]{
			Authorize:         oidcauth.IdentityInfoAuthorize,
			AppName:           "Basket",
			SessionCookie:     "basket_session",
			TransactionCookie: "basket_oidc_tx",
			PublicURL:         issuer,
			Issuer:            issuer,
			ClientID:          "client",
			OwnerSubject:      "owner",
			MCPClientID:       "mcp",
			MCPAudience:       issuer + "/mcp",
		},
		st.DB(),
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	expires := time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000000000Z")
	hash := sha256.Sum256([]byte("test"))

	_, err = st.DB().
		ExecContext(t.Context(), `INSERT INTO sessions (token_hash, issuer, subject, created_at, renewed_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, hex.EncodeToString(hash[:]), issuer, "owner", now, now, expires)
	if err != nil {
		t.Fatal(err)
	}

	svc := basket.NewService(st, "Europe/Istanbul", time.Now)
	srv := New(
		Options{
			Auth:    a,
			Service: svc,
			MCP: http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			),
			Web: fstest.MapFS{
				"index.html":  {Data: []byte("app")},
				"sw.js":       {Data: []byte("sw")},
				"assets/a.js": {Data: []byte("asset")},
			},
			Ping: func(r *http.Request) error { return st.DB().PingContext(r.Context()) },
		},
	)
	handler := srv.MetricsRegistry().Middleware(srv.Handler())

	request := func(method, path, body string, session bool) *httptest.ResponseRecorder {
		t.Helper()

		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Basket-Request", "1")
		}

		if session {
			r.AddCookie(&http.Cookie{Name: "basket_session", Value: "test"})
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		return w
	}
	if w := request("GET", "/api/snapshot", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}

	w := request("GET", "/api/snapshot", "", true)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"items":[]`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"purchases":[]`)) {
		t.Fatalf("snapshot: %d %s", w.Code, w.Body)
	}

	op := `{"ops":[{"op_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","type":"add_item","id":"01ARZ3NDEKTSV4RRFFQ69G5FAW","fields":{"title":"Milk"}}]}`
	for _, headers := range []map[string]string{
		{"Content-Type": "text/plain", "Origin": "https://evil.example.com", "Sec-Fetch-Site": "same-site"},
		{"Content-Type": "application/json"},
		{"Content-Type": "text/plain", "X-Basket-Request": "1"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/ops", strings.NewReader(op))
		r.AddCookie(&http.Cookie{Name: "basket_session", Value: "test"})

		for key, value := range headers {
			r.Header.Set(key, value)
		}

		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Fatalf("unsafe request: %d %s", w.Code, w.Body)
		}
	}

	w = request("GET", "/api/snapshot", "", true)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("unsafe request wrote data: %s", w.Body)
	}

	for i := range 2 {
		w = request("POST", "/api/ops", op, true)
		if w.Code != 200 {
			t.Fatalf("ops: %s", w.Body)
		}

		if i == 1 && !bytes.Contains(w.Body.Bytes(), []byte(`"duplicate":true`)) {
			t.Fatal(w.Body)
		}
	}

	w = request("GET", "/api/similar?title=Milk", "", true)
	if !bytes.Contains(w.Body.Bytes(), []byte("Milk")) {
		t.Fatal(w.Body)
	}

	w = request("GET", "/api/export", "", true)
	if !strings.Contains(w.Header().Get("Content-Disposition"), "basket-") {
		t.Fatal(w.Header())
	}

	for _, body := range []string{"{", `{"ops":[]}`, `{"ops":[` + strings.Repeat(`{},`, basket.MaxOpsPerRequest) + `{}]}`} {
		if w = request("POST", "/api/ops", body, true); w.Code != 400 {
			t.Fatalf("bad ops: %d", w.Code)
		}
	}

	if w = request("GET", "/api/missing", "", true); w.Code != 404 || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("missing: %s", w.Body)
	}

	for path, cache := range map[string]string{"/route": "no-cache", "/sw.js": "no-cache", "/assets/a.js": "public, max-age=31536000, immutable"} {
		w = request("GET", path, "", false)
		if w.Code != 200 || w.Header().Get("Cache-Control") != cache {
			t.Fatalf("%s: %d %s", path, w.Code, w.Header())
		}
	}

	if w = request("GET", "/healthz", "", false); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatal(w.Body)
	}

	if w = request(
		"POST",
		"/mcp",
		"",
		false,
	); w.Code != 401 ||
		w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("mcp: %d %s", w.Code, w.Header())
	}

	if w = request(
		"GET",
		"/metrics",
		"",
		false,
	); strings.Contains(
		w.Body.String(),
		"http_requests_total",
	) {
		t.Fatal("public metrics")
	}
}
