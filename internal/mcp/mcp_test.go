//nolint:exhaustruct_v5 // SDK defaults are intentional.
package mcp

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zekihan/basket/internal/basket"
	"github.com/zekihan/basket/internal/store"
)

func TestTools(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "basket.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := basket.NewService(st, "Europe/Istanbul", time.Now)

	httpServer := httptest.NewServer(NewHandler(svc))
	defer httpServer.Close()

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)

	session, err := client.Connect(
		t.Context(),
		&sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Tools) != 11 {
		t.Fatalf("got %d tools", len(list.Tools))
	}

	for _, tool := range list.Tools {
		if tool.Description == "" {
			t.Fatalf("missing description: %s", tool.Name)
		}
	}

	call := func(name string, args map[string]any) *sdk.CallToolResult {
		t.Helper()

		result, err := session.CallTool(
			t.Context(),
			&sdk.CallToolParams{Name: name, Arguments: args},
		)
		if err != nil {
			t.Fatal(err)
		}

		return result
	}

	first := call("add_item", map[string]any{"title": "Shampoo"})
	if first.IsError || first.StructuredContent == nil {
		t.Fatalf("add: %+v", first)
	}

	similar := call("add_item", map[string]any{"title": "Shampoo"})
	if similar.IsError {
		t.Fatalf("similar: %+v", similar)
	}

	again := call("add_item", map[string]any{"title": "Shampoo", "allow_similar": true})
	if again.IsError {
		t.Fatalf("add again: %+v", again)
	}

	bad := call(
		"add_item",
		map[string]any{"title": "Milk", "list": "wishlist", "recurring_days": 3},
	)
	if !bad.IsError {
		t.Fatalf("expected error: %+v", bad)
	}
}
