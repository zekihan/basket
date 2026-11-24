//nolint:exhaustruct_v5,gocyclo,noinlineerr,varnamelen,wsl_v5 // Tests and fixtures are intentional.
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zekihan/basket/internal/basket"
	"github.com/zekihan/basket/internal/store"
)

type harness struct {
	svc     *basket.Service
	session *sdk.ClientSession
	now     *time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	st, err := store.Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Date(2026, time.November, 23, 22, 30, 0, 0, time.UTC)
	svc := basket.NewService(st, "Europe/Istanbul", func() time.Time { return now })
	h := &harness{svc: svc, now: &now}

	clientTransport, serverTransport := sdk.NewInMemoryTransports()

	serverSession, err := NewServer(svc).Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)

	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	h.session = session

	return h
}

func (h *harness) call(t *testing.T, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()

	var arguments any
	if args != nil {
		arguments = args
	}

	result, err := h.session.CallTool(
		t.Context(),
		&sdk.CallToolParams{Name: name, Arguments: arguments},
	)
	if err != nil {
		t.Fatal(err)
	}

	return result
}

func decode[T any](t *testing.T, result *sdk.CallToolResult) T {
	t.Helper()

	if result.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(result))
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}

	var out T

	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func textOf(result *sdk.CallToolResult) string {
	var parts []string

	for _, content := range result.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}

	return strings.Join(parts, " ")
}

func (h *harness) add(t *testing.T, args map[string]any) basket.AddItemOutput {
	t.Helper()

	return decode[basket.AddItemOutput](t, h.call(t, "add_item", args))
}

func (h *harness) mustAdd(t *testing.T, title string) basket.Item {
	t.Helper()

	out := h.add(t, map[string]any{"title": title})
	if out.Added == nil {
		t.Fatalf("add %q: %+v", title, out)
	}

	return *out.Added
}

func (h *harness) list(
	t *testing.T,
	args map[string]any,
) basket.ListItemsOutput {
	t.Helper()

	return decode[basket.ListItemsOutput](t, h.call(t, "list_items", args))
}

func (h *harness) get(t *testing.T, id string) basket.GetItemOutput {
	t.Helper()

	return decode[basket.GetItemOutput](
		t,
		h.call(t, "get_item", map[string]any{"id": id}),
	)
}

func (h *harness) pwa(t *testing.T, body string) {
	t.Helper()

	var op basket.Op
	if err := json.Unmarshal([]byte(body), &op); err != nil {
		t.Fatal(err)
	}

	op.OpID = ulid.Make().String()

	results, err := h.svc.ApplyOps(t.Context(), basket.ActorPWA, []basket.Op{op})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || !results[0].OK {
		t.Fatalf("PWA op = %+v", results)
	}
}

func titles(items []basket.Item) []string {
	out := make([]string, 0, len(items))
	for index := range items {
		out = append(out, items[index].Title)
	}

	return out
}

func TestAddItemSimilar(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	shampoo := h.mustAdd(t, "Shampoo")

	out := h.add(t, map[string]any{"title": "Clear men shampoo"})
	if out.Added != nil || len(out.Similar) != 1 || out.Similar[0].ID != shampoo.ID {
		t.Fatalf("similar = %+v", out)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 1 {
		t.Fatalf("items after blocked add = %+v", got.Items)
	}

	added := h.add(t, map[string]any{"title": "Clear men shampoo", "allow_similar": true})
	if added.Added == nil || added.Added.Title != "Clear men shampoo" {
		t.Fatalf("allow_similar = %+v", added)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 2 {
		t.Fatalf("items after allow_similar = %+v", got.Items)
	}
}

func TestAddItemsMixed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.mustAdd(t, "Shampoo")

	result := h.call(t, "add_items", map[string]any{"items": []map[string]any{
		{"title": "Bread"},
		{"title": "Clear men shampoo"},
		{"title": ""},
	}})
	out := decode[basket.AddItemsOutput](t, result)

	if len(out.Results) != 3 {
		t.Fatalf("results = %+v", out.Results)
	}

	if out.Results[0].Added == nil || out.Results[0].Error != nil {
		t.Fatalf("new entry = %+v", out.Results[0])
	}

	if out.Results[1].Added != nil || len(out.Results[1].Similar) != 1 ||
		out.Results[1].Similar[0].Title != "Shampoo" || out.Results[1].Error != nil {
		t.Fatalf("similar entry = %+v", out.Results[1])
	}

	if out.Results[2].Added != nil || out.Results[2].Error == nil ||
		*out.Results[2].Error == "" {
		t.Fatalf("invalid entry = %+v", out.Results[2])
	}

	got := titles(h.list(t, map[string]any{"status": "all"}).Items)
	if len(got) != 2 || !strings.Contains(strings.Join(got, ","), "Bread") {
		t.Fatalf("items = %v", got)
	}
}

func TestListItemsFilters(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ethernet := h.add(t, map[string]any{"title": "Ethernet cable"})
	wish := h.add(t, map[string]any{"title": "Monitor", "list": "wishlist"})
	soap := h.mustAdd(t, "Soap")

	if ethernet.Added == nil || wish.Added == nil {
		t.Fatalf("setup adds: %+v %+v", ethernet, wish)
	}

	if got := h.list(t, map[string]any{}); len(got.Items) != 3 {
		t.Fatalf("default open = %+v", got.Items)
	}

	if got := h.list(t, map[string]any{"list": "wishlist"}); len(got.Items) != 1 ||
		got.Items[0].Title != "Monitor" {
		t.Fatalf("wishlist = %+v", got.Items)
	}

	if got := h.list(t, map[string]any{"list": "shopping"}); len(got.Items) != 2 {
		t.Fatalf("shopping = %+v", got.Items)
	}

	h.call(t, "mark_bought", map[string]any{"id": soap.ID})

	if got := h.list(t, map[string]any{}); len(got.Items) != 2 {
		t.Fatalf("open after buy = %+v", got.Items)
	}

	if got := h.list(t, map[string]any{"status": "bought"}); len(got.Items) != 1 ||
		got.Items[0].ID != soap.ID {
		t.Fatalf("bought = %+v", got.Items)
	}

	*h.now = h.now.AddDate(0, 0, 31)

	if got := h.list(t, map[string]any{"status": "archived"}); len(got.Items) != 1 ||
		got.Items[0].ID != soap.ID {
		t.Fatalf("archived = %+v", got.Items)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 3 {
		t.Fatalf("all = %+v", got.Items)
	}

	got := h.list(t, map[string]any{"status": "all", "query": "eth cab"})
	if len(got.Items) != 1 || got.Items[0].Title != "Ethernet cable" {
		t.Fatalf("query = %+v", got.Items)
	}
}

func TestGetItemPurchasesNewestFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.mustAdd(t, "Milk")
	h.call(t, "mark_bought", map[string]any{"id": item.ID, "date": "2026-11-20"})
	h.call(t, "mark_bought", map[string]any{"id": item.ID, "date": "2026-11-24"})

	got := h.get(t, item.ID)
	if len(got.Purchases) != 2 || got.Purchases[0].Date != "2026-11-24" ||
		got.Purchases[1].Date != "2026-11-20" {
		t.Fatalf("purchases = %+v", got.Purchases)
	}
}

func TestUpdateItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.add(t, map[string]any{
		"title": "Ethernet cable", "target_price": "500,00", "recurring_days": 7,
	})
	if item.Added == nil {
		t.Fatalf("add = %+v", item)
	}

	id := item.Added.ID
	updated := decode[basket.UpdateItemOutput](t, h.call(t, "update_item",
		map[string]any{"id": id, "notes": "Cat6"}))
	if updated.Item.Notes != "Cat6" {
		t.Fatalf("notes = %+v", updated.Item)
	}

	cleared := decode[basket.UpdateItemOutput](t, h.call(t, "update_item",
		map[string]any{"id": id, "notes": nil}))
	if cleared.Item.Notes != "" || cleared.Item.TargetPriceMinor == nil ||
		*cleared.Item.TargetPriceMinor != 50000 || cleared.Item.RecurringDays == nil {
		t.Fatalf("clear notes = %+v", cleared.Item)
	}

	cleared = decode[basket.UpdateItemOutput](t, h.call(t, "update_item",
		map[string]any{"id": id, "target_price": nil}))
	if cleared.Item.TargetPrice != nil || cleared.Item.TargetPriceMinor != nil ||
		cleared.Item.RecurringDays == nil || *cleared.Item.RecurringDays != 7 {
		t.Fatalf("clear target_price = %+v", cleared.Item)
	}

	wish := h.add(t, map[string]any{"title": "Keyboard", "list": "wishlist"})
	if wish.Added == nil {
		t.Fatalf("wishlist add = %+v", wish)
	}

	bad := h.call(t, "update_item",
		map[string]any{"id": wish.Added.ID, "recurring_days": 7})
	if !bad.IsError || !strings.Contains(textOf(bad), "recurring_days") {
		t.Fatalf("wishlist recurring = %v %q", bad.IsError, textOf(bad))
	}

	moved := decode[basket.UpdateItemOutput](t, h.call(t, "update_item",
		map[string]any{"id": id, "list": "wishlist"}))
	if moved.Item.List != "wishlist" || moved.Item.RecurringDays != nil {
		t.Fatalf("moved = %+v", moved.Item)
	}
}

func TestRemoveItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first := h.mustAdd(t, "Ethernet cable")
	h.mustAdd(t, "Ethernet switch")

	byID := decode[basket.RemoveItemOutput](t, h.call(t, "remove_item",
		map[string]any{"id": first.ID}))
	if byID.Removed == nil || byID.Removed.ID != first.ID {
		t.Fatalf("by id = %+v", byID)
	}

	unique := decode[basket.RemoveItemOutput](t, h.call(t, "remove_item",
		map[string]any{"query": "eth swi"}))
	if unique.Removed == nil || unique.Removed.Title != "Ethernet switch" {
		t.Fatalf("unique query = %+v", unique)
	}

	h.mustAdd(t, "USB cable")
	h.mustAdd(t, "USB hub")

	ambiguous := decode[basket.RemoveItemOutput](t, h.call(t, "remove_item",
		map[string]any{"query": "usb"}))
	if ambiguous.Removed != nil || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous = %+v", ambiguous)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 2 {
		t.Fatalf("after ambiguous = %+v", got.Items)
	}

	zero := h.call(t, "remove_item", map[string]any{"query": "nonexistentthing"})
	if !zero.IsError {
		t.Fatalf("zero matches = %+v", zero)
	}
}

func TestMarkBought(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.mustAdd(t, "Shampoo")

	out := decode[basket.MarkBoughtOutput](t, h.call(t, "mark_bought",
		map[string]any{"id": item.ID, "price": "189,90", "store": "Trendyol"}))
	if out.Purchase.PriceMinor == nil || *out.Purchase.PriceMinor != 18990 ||
		out.Purchase.Price == nil || *out.Purchase.Price != "189,90" ||
		out.Purchase.Store == nil || *out.Purchase.Store != "Trendyol" ||
		out.Item.Status != "bought" {
		t.Fatalf("mark_bought = %+v", out)
	}

	again := decode[basket.MarkBoughtOutput](t, h.call(t, "mark_bought",
		map[string]any{"id": item.ID}))
	if again.Item.Status != "bought" {
		t.Fatalf("second mark = %+v", again.Item)
	}

	if got := h.get(t, item.ID); len(got.Purchases) != 2 {
		t.Fatalf("purchases = %+v", got.Purchases)
	}
}

func TestReopenItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.mustAdd(t, "Soap")
	h.call(t, "mark_bought", map[string]any{"id": item.ID})

	out := decode[basket.UpdateItemOutput](t, h.call(t, "reopen_item",
		map[string]any{"id": item.ID}))
	if out.Item.Status != "open" {
		t.Fatalf("reopen = %+v", out.Item)
	}
}

func TestPriceHistory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.mustAdd(t, "Coffee")
	h.call(t, "mark_bought", map[string]any{"id": item.ID, "price": "100,00"})
	h.call(t, "mark_bought", map[string]any{"id": item.ID, "price": "100,01"})
	h.call(t, "mark_bought", map[string]any{"id": item.ID})

	out := decode[basket.PriceHistoryOutput](t, h.call(t, "price_history",
		map[string]any{"id": item.ID}))
	if out.ItemPriceHistory == nil || out.AveragePriceMinor == nil ||
		*out.AveragePriceMinor != 10001 || out.AveragePrice == nil ||
		*out.AveragePrice != "100,01" || len(out.Purchases) != 3 {
		t.Fatalf("history = %+v", out)
	}

	h.mustAdd(t, "USB cable")
	h.mustAdd(t, "USB hub")

	ambiguous := decode[basket.PriceHistoryOutput](t, h.call(t, "price_history",
		map[string]any{"query": "usb"}))
	if len(ambiguous.Candidates) != 2 || ambiguous.ItemPriceHistory != nil {
		t.Fatalf("ambiguous history = %+v", ambiguous)
	}
}

func TestDueItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.add(t, map[string]any{"title": "Milk", "recurring_days": 7})
	if item.Added == nil {
		t.Fatalf("add = %+v", item)
	}

	h.call(t, "mark_bought", map[string]any{"id": item.Added.ID})

	if got := decode[basket.ListItemsOutput](t, h.call(t, "due_items",
		map[string]any{"within_days": 0})); len(got.Items) != 0 {
		t.Fatalf("due today = %+v", got.Items)
	}

	if got := decode[basket.ListItemsOutput](t, h.call(t, "due_items",
		map[string]any{"within_days": 7})); len(got.Items) != 1 {
		t.Fatalf("due within 7 = %+v", got.Items)
	}

	*h.now = h.now.AddDate(0, 0, 7)

	if got := decode[basket.ListItemsOutput](t, h.call(t, "due_items",
		map[string]any{"within_days": 0})); len(got.Items) != 1 {
		t.Fatalf("due after 7 days = %+v", got.Items)
	}
}

func TestUndoRevertsAndReapplies(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.mustAdd(t, "Shampoo")

	undone := decode[basket.UndoOutput](t, h.call(t, "undo", map[string]any{}))
	if undone.Undone == "" {
		t.Fatalf("undo = %+v", undone)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 0 {
		t.Fatalf("after undo = %+v", got.Items)
	}

	decode[basket.UndoOutput](t, h.call(t, "undo", map[string]any{}))

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 1 {
		t.Fatalf("after second undo = %+v", got.Items)
	}
}

func TestUndoConflictAfterPWAUpdate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	item := h.mustAdd(t, "Shampoo")
	decode[basket.UpdateItemOutput](t, h.call(t, "update_item",
		map[string]any{"id": item.ID, "notes": "mcp"}))
	h.pwa(t, fmt.Sprintf(`{"type":"update_item","id":%q,"fields":{"notes":"pwa"}}`, item.ID))

	result := h.call(t, "undo", map[string]any{})
	if !result.IsError || !strings.Contains(textOf(result), "Shampoo") {
		t.Fatalf("undo = %v %q", result.IsError, textOf(result))
	}
}

func TestUndoNeverRevertsPWA(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := ulid.Make().String()
	h.pwa(t, fmt.Sprintf(`{"type":"add_item","id":%q,"fields":{"title":"PWA item"}}`, id))

	result := h.call(t, "undo", map[string]any{})
	if !result.IsError {
		t.Fatalf("undo = %+v", result)
	}

	if got := h.get(t, id); got.Item.Title != "PWA item" {
		t.Fatalf("item after undo = %+v", got.Item)
	}
}

func TestNoArguments(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if got := decode[basket.ListItemsOutput](t, h.call(t, "list_items", nil)); len(got.Items) != 0 {
		t.Fatalf("list no args = %+v", got.Items)
	}

	h.mustAdd(t, "Shampoo")

	if got := decode[basket.UndoOutput](t, h.call(t, "undo", nil)); got.Undone == "" {
		t.Fatalf("undo no args = %+v", got)
	}

	if got := h.list(t, map[string]any{"status": "all"}); len(got.Items) != 0 {
		t.Fatalf("after no-arg undo = %+v", got.Items)
	}
}
