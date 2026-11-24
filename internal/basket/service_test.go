//nolint:exhaustruct_v5 // Fixtures specify only fields relevant to each case.
package basket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zekihan/basket/internal/store"
)

const testZone = "Europe/Istanbul"

type fixture struct {
	svc *Service
	st  *store.Store
	now *time.Time
}

func setup(t *testing.T) fixture {
	t.Helper()

	st, err := store.Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	now := time.Date(2026, time.November, 23, 22, 30, 0, 0, time.UTC)

	return fixture{
		svc: NewService(st, testZone, func() time.Time { return now }),
		st:  st,
		now: &now,
	}
}

func opFromJSON(t *testing.T, body string) Op {
	t.Helper()

	var op Op

	err := json.Unmarshal([]byte(body), &op)
	if err != nil {
		t.Fatal(err)
	}

	op.OpID = ulid.Make().String()

	return op
}

func apply(t *testing.T, svc *Service, actor string, ops ...Op) []OpResult {
	t.Helper()

	results, err := svc.ApplyOps(t.Context(), actor, ops)
	if err != nil || len(results) != len(ops) {
		t.Fatalf("ApplyOps = %+v, %v", results, err)
	}

	return results
}

func mustApply(t *testing.T, svc *Service, ops ...Op) {
	t.Helper()

	results := apply(t, svc, ActorPWA, ops...)
	for _, result := range results {
		if !result.OK || result.Duplicate {
			t.Fatalf("unexpected result: %+v %v", result, result.Error)
		}
	}
}

func requireError(t *testing.T, err error, code, message string) {
	t.Helper()

	var domain *OpError

	if !errors.As(err, &domain) || domain == nil || domain.Code != code ||
		!strings.Contains(domain.Message, message) {
		t.Fatalf("error = %v, want %s mentioning %q", err, code, message)
	}
}

func toolAdd(t *testing.T, svc *Service, title string) Item {
	t.Helper()

	out, err := svc.AddItem(t.Context(), AddItemInput{Title: title, AllowSimilar: true})
	if err != nil || out.Added == nil {
		t.Fatalf("AddItem = %+v, %v", out, err)
	}

	return *out.Added
}

func toolGet(t *testing.T, svc *Service, id string) GetItemOutput {
	t.Helper()

	out, err := svc.GetItem(t.Context(), GetItemInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestApplyOpsIdempotencyAndValidation(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	id := ulid.Make().String()

	invalidOp := opFromJSON(
		t,
		fmt.Sprintf(`{"type":"update_item","id":%q,"fields":{"notes":"ignored"}}`, id),
	)
	add := opFromJSON(
		t,
		fmt.Sprintf(`{"type":"add_item","id":%q,"fields":{"title":" Şampuan ","quantity":2}}`, id),
	)
	repeated := add
	repeated.Type = OpRemoveItem

	results := apply(t, fx.svc, ActorPWA, invalidOp, add, invalidOp, repeated)
	requireError(t, results[0].Error, CodeNotFound, "id")

	if !results[1].OK || !results[2].Duplicate || !results[3].OK || !results[3].Duplicate {
		t.Fatalf("unexpected operation results: %+v", results)
	}

	requireError(t, results[2].Error, CodeNotFound, "id")

	got := toolGet(t, fx.svc, id)
	if got.Item.Title != "Şampuan" || got.Item.Quantity != 2 || got.Item.Version != 1 {
		t.Fatalf("unexpected item: %+v", got.Item)
	}

	cases := map[string]string{
		"duplicate id": `{"type":"add_item","id":%q,"fields":{"title":"again"}}`,
		"title":        `{"type":"update_item","id":%q,"fields":{"title":"  "}}`,
		"quantity":     `{"type":"update_item","id":%q,"fields":{"quantity":0}}`,
		"target_price": `{"type":"update_item","id":%q,"fields":{"target_price":"-1"}}`,
		"recurring_days": `{"type":"update_item","id":%q,
			"fields":{"list":"wishlist","recurring_days":7}}`,
		"list": `{"type":"update_item","id":%q,"fields":{"list":null}}`,
		"type": `{"type":"unknown","id":%q}`,
	}

	for field, body := range cases {
		name := field
		if name == "duplicate id" {
			name = "id"
		}

		result := apply(t, fx.svc, ActorPWA, opFromJSON(t, fmt.Sprintf(body, id)))[0]
		requireError(t, result.Error, CodeInvalid, name)
	}

	badID := Op{OpID: strings.ToLower(ulid.Make().String()), Type: OpUndo}
	results = apply(t, fx.svc, ActorPWA, badID, badID)
	requireError(t, results[0].Error, CodeInvalid, "op_id")

	if !results[1].Duplicate {
		t.Fatal("malformed op failure was not recorded")
	}

	_, err := fx.svc.ApplyOps(t.Context(), ActorPWA, make([]Op, MaxOpsPerRequest+1))
	requireError(t, err, CodeInvalid, "ops")
}

func TestItemFieldLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		field string
		json  string
		valid bool
	}{
		{"title", fmt.Sprintf(`{"title":%q}`, " "+strings.Repeat("ş", 200)+" "), true},
		{"title", fmt.Sprintf(`{"title":%q}`, strings.Repeat("x", 201)), false},
		{"quantity", `{"quantity":999}`, true},
		{"quantity", `{"quantity":1000}`, false},
		{"notes", fmt.Sprintf(`{"notes":%q}`, strings.Repeat("ş", 2000)), true},
		{"notes", fmt.Sprintf(`{"notes":%q}`, strings.Repeat("x", 2001)), false},
		{"recurring_days", `{"recurring_days":1}`, true},
		{"recurring_days", `{"recurring_days":730}`, true},
		{"recurring_days", `{"recurring_days":0}`, false},
		{"recurring_days", `{"recurring_days":731}`, false},
		{"target_price", `{"target_price":"0"}`, true},
		{"target_price", `{"target_price":"NaN"}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()

			fx := setup(t)
			item := toolAdd(t, fx.svc, "Title")

			var input UpdateItemInput

			err := json.Unmarshal([]byte(tc.json), &input)
			if err != nil {
				t.Fatal(err)
			}

			input.ID = item.ID
			got, err := fx.svc.UpdateItem(t.Context(), &input)

			if tc.valid {
				if err != nil || got.Item.Version != item.Version+1 {
					t.Fatalf("valid update = %+v, %v", got, err)
				}

				return
			}

			requireError(t, err, CodeInvalid, tc.field)

			if toolGet(t, fx.svc, item.ID).Item.Version != item.Version {
				t.Fatal("failed update changed item version")
			}
		})
	}
}

func TestCalendarAndStoredArchival(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Soap")

	bought, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: item.ID})
	if err != nil || bought.Purchase.Date != "2026-11-24" {
		t.Fatalf("purchase = %+v, %v", bought, err)
	}

	*fx.now = fx.now.AddDate(0, 0, 30)

	if status := toolGet(t, fx.svc, item.ID).Item.Status; status != StatusBought {
		t.Fatalf("at 30 days got %s", status)
	}

	*fx.now = fx.now.AddDate(0, 0, 1)

	if status := toolGet(t, fx.svc, item.ID).Item.Status; status != StatusArchived {
		t.Fatalf("at 31 days got %s", status)
	}

	assertStatus := func(want string) {
		t.Helper()

		var stored string

		queryErr := fx.st.DB().
			GetContext(t.Context(), &stored, "SELECT status FROM items WHERE id = ?", item.ID)
		if queryErr != nil || stored != want {
			t.Fatalf("stored status = %q, %v, want %q", stored, queryErr, want)
		}
	}

	assertStatus(StatusBought)
	mustApply(
		t,
		fx.svc,
		opFromJSON(
			t,
			fmt.Sprintf(`{"type":"update_item","id":%q,"fields":{"notes":"abc"}}`, item.ID),
		),
	)
	assertStatus(StatusArchived)

	reopened, err := fx.svc.ReopenItem(t.Context(), GetItemInput{ID: item.ID})
	if err != nil || reopened.Item.Status != StatusOpen || reopened.Item.LastPurchaseDate == nil {
		t.Fatalf("reopen = %+v, %v", reopened, err)
	}

	assertStatus(StatusOpen)
}

func TestRecurrenceOnDerivedArchiveComesDue(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Milk")

	_, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}

	*fx.now = fx.now.AddDate(0, 0, 40)

	mustApply(
		t,
		fx.svc,
		opFromJSON(
			t,
			fmt.Sprintf(`{"type":"update_item","id":%q,"fields":{"recurring_days":7}}`, item.ID),
		),
	)

	got := toolGet(t, fx.svc, item.ID).Item
	if got.Status != StatusBought || !got.Due || got.DueDate == nil ||
		*got.DueDate != "2026-12-01" {
		t.Fatalf("item = %+v", got)
	}
}

func TestRecurrenceAndTimeZone(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	id := ulid.Make().String()

	mustApply(
		t,
		fx.svc,
		opFromJSON(
			t,
			fmt.Sprintf(
				`{"type":"add_item","id":%q,"fields":{"title":"Milk","recurring_days":2}}`,
				id,
			),
		),
		opFromJSON(
			t,
			fmt.Sprintf(
				`{"type":"mark_bought","id":%q,"purchase_id":%q,"date":"2026-11-22"}`,
				id,
				ulid.Make(),
			),
		),
		opFromJSON(
			t,
			fmt.Sprintf(
				`{"type":"mark_bought","id":%q,"purchase_id":%q,"date":"2026-11-20"}`,
				id,
				ulid.Make(),
			),
		),
	)

	got := toolGet(t, fx.svc, id)
	if !got.Item.Due || got.Item.DueDate == nil || *got.Item.DueDate != "2026-11-24" ||
		len(got.Purchases) != 2 {
		t.Fatalf("recurrence = %+v", got)
	}

	mustApply(t, fx.svc, opFromJSON(t, `{"type":"set_time_zone","time_zone":"UTC"}`))

	got = toolGet(t, fx.svc, id)
	if got.Item.Due {
		t.Fatal("item due early in UTC")
	}

	for _, days := range []int{0, 1, 365} {
		due, err := fx.svc.DueItems(t.Context(), DueItemsInput{WithinDays: days})
		if err != nil || len(due.Items) != min(days, 1) {
			t.Fatalf("due within %d days = %+v, %v", days, due, err)
		}
	}

	_, err := fx.svc.DueItems(t.Context(), DueItemsInput{WithinDays: 366})
	requireError(t, err, CodeInvalid, "within_days")
	badZone := apply(t, fx.svc, ActorPWA, opFromJSON(t, `{"type":"set_time_zone","time_zone":"Local"}`))[0]
	requireError(t, badZone.Error, CodeInvalid, "time_zone")

	// Undo still targets the second purchase, despite the intervening zone change.
	mustApply(t, fx.svc, opFromJSON(t, `{"type":"undo"}`))
	got = toolGet(t, fx.svc, id)

	if len(got.Purchases) != 1 {
		t.Fatalf("undo after zone change: %+v", got)
	}

	mustApply(
		t,
		fx.svc,
		opFromJSON(
			t,
			fmt.Sprintf(`{"type":"update_item","id":%q,"fields":{"list":"wishlist"}}`, id),
		),
	)
	got = toolGet(t, fx.svc, id)

	if got.Item.List != ListWishlist || got.Item.RecurringDays != nil || got.Item.DueDate != nil {
		t.Fatalf("wishlist recurrence = %+v", got.Item)
	}
}

func TestUndoChecksPurchasesAndVersion(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Coffee")

	purchase, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}

	mustApply(t, fx.svc, opFromJSON(t, fmt.Sprintf(`{"type":"remove_item","id":%q}`, item.ID)))

	_, err = fx.svc.GetItem(t.Context(), GetItemInput{ID: item.ID})
	requireError(t, err, CodeNotFound, "id")

	for _, op := range []Op{
		{OpID: ulid.Make().String(), Type: OpUpdateItem, ID: item.ID},
		{OpID: ulid.Make().String(), Type: OpAddPurchaseDetails, PurchaseID: purchase.Purchase.ID},
		{OpID: ulid.Make().String(), Type: OpDeletePurchase, PurchaseID: purchase.Purchase.ID},
	} {
		got := apply(t, fx.svc, ActorPWA, op)[0]
		requireError(t, got.Error, CodeNotFound, "id")
	}

	_, err = fx.svc.Undo(t.Context(), ActorMCP)
	requireError(t, err, CodeConflict, "Coffee")

	undoOp := opFromJSON(t, `{"type":"undo"}`)
	mustApply(t, fx.svc, undoOp)
	restored := toolGet(t, fx.svc, item.ID)

	if len(restored.Purchases) != 1 || restored.Purchases[0].ID != purchase.Purchase.ID ||
		restored.Item.Version <= purchase.Item.Version {
		t.Fatalf("restoration = %+v", restored)
	}

	retry := apply(t, fx.svc, ActorPWA, undoOp)[0]
	if !retry.Duplicate || !retry.OK {
		t.Fatalf("undo retry = %+v", retry)
	}

	mustApply(t, fx.svc, opFromJSON(t, `{"type":"undo"}`))

	_, err = fx.svc.GetItem(t.Context(), GetItemInput{ID: item.ID})
	requireError(t, err, CodeNotFound, "id")

	mustApply(t, fx.svc, opFromJSON(t, `{"type":"undo"}`))

	if got := toolGet(t, fx.svc, item.ID); got.Item.Version <= restored.Item.Version {
		t.Fatalf("version did not increase: %+v", got)
	}

	// A second item can take over a deleted purchase ID. Undo must detect it
	// even though the removed item's tombstone is unchanged.
	mustApply(t, fx.svc, opFromJSON(t, fmt.Sprintf(`{"type":"remove_item","id":%q}`, item.ID)))
	other := toolAdd(t, fx.svc, "Tea")
	buy := Op{
		OpID:       ulid.Make().String(),
		ID:         other.ID,
		Type:       OpMarkBought,
		PurchaseID: purchase.Purchase.ID,
	}

	results := apply(t, fx.svc, ActorMCP, buy)
	if !results[0].OK {
		t.Fatalf("reused purchase ID: %v", results[0].Error)
	}

	_, err = fx.svc.Undo(t.Context(), ActorPWA)
	requireError(t, err, CodeConflict, "Coffee")
}

func TestPruningAndSnapshotActor(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	toolAdd(t, fx.svc, "MCP only")

	snapshot, err := fx.svc.Snapshot(t.Context())
	if err != nil || snapshot.CanUndo {
		t.Fatalf("MCP-only snapshot = %+v, %v", snapshot, err)
	}

	op := opFromJSON(
		t,
		fmt.Sprintf(`{"type":"add_item","id":%q,"fields":{"title":"PWA"}}`, ulid.Make()),
	)

	mustApply(t, fx.svc, op)

	snapshot, err = fx.svc.Snapshot(t.Context())
	if err != nil || !snapshot.CanUndo || snapshot.Today != "2026-11-24" ||
		snapshot.TimeZone != testZone {
		t.Fatalf("PWA snapshot = %+v, %v", snapshot, err)
	}

	*fx.now = fx.now.Add(store.Retention)

	if result := apply(t, fx.svc, ActorPWA, op)[0]; !result.Duplicate || !result.OK {
		t.Fatalf("exactly 30 days: %+v", result)
	}

	*fx.now = fx.now.Add(time.Second)

	err = fx.svc.Prune(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err = fx.svc.Snapshot(t.Context())
	if err != nil || snapshot.CanUndo {
		t.Fatalf("snapshot after prune = %+v, %v", snapshot, err)
	}

	_, err = fx.svc.Undo(t.Context(), ActorPWA)
	requireError(t, err, CodeNotFound, "change")

	result := apply(t, fx.svc, ActorPWA, op)[0]
	requireError(t, result.Error, CodeInvalid, "id")

	if result.Duplicate {
		t.Fatal("expired result was replayed")
	}
}

func TestConcurrentDuplicate(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Milk")
	op := opFromJSON(
		t,
		fmt.Sprintf(`{"type":"mark_bought","id":%q,"purchase_id":%q}`, item.ID, ulid.Make()),
	)
	results := make(chan bool, 2)

	for range cap(results) {
		go func() {
			result, err := fx.svc.ApplyOps(t.Context(), ActorPWA, []Op{op})
			if err != nil || !result[0].OK {
				t.Errorf("concurrent operation = %+v, %v", result, err)

				results <- false

				return
			}

			results <- result[0].Duplicate
		}()
	}

	if first, second := <-results, <-results; first == second {
		t.Fatal("expected exactly one duplicate")
	}

	got := toolGet(t, fx.svc, item.ID)
	if len(got.Purchases) != 1 || got.Item.Version != item.Version+1 {
		t.Fatalf("concurrent purchase = %+v", got)
	}
}

func TestUndoRejectsItemIDReuse(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Original")
	remove := Op{OpID: ulid.Make().String(), Type: OpRemoveItem, ID: item.ID}

	mustApply(t, fx.svc, remove)

	reused := opFromJSON(
		t,
		fmt.Sprintf(`{"type":"add_item","id":%q,"fields":{"title":"Replacement"}}`, item.ID),
	)

	results := apply(t, fx.svc, ActorMCP, reused)
	if !results[0].OK {
		t.Fatalf("reuse = %v", results[0].Error)
	}

	get := toolGet(t, fx.svc, item.ID)
	if get.Item.Version <= item.Version {
		t.Fatalf("reused ID version = %d", get.Item.Version)
	}

	_, err := fx.svc.Undo(t.Context(), ActorMCP)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fx.svc.Undo(t.Context(), ActorPWA)
	requireError(t, err, CodeConflict, "Original")
}

func TestUndoRejectsChangedPurchase(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Rice")

	bought, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}

	err = fx.st.Write(t.Context(), func(ctx context.Context, tx *store.Tx) error {
		row, readErr := tx.Purchase(ctx, bought.Purchase.ID)
		if readErr != nil {
			return fmt.Errorf("read fixture purchase: %w", readErr)
		}

		storeName := "Somewhere"
		row.Store = &storeName

		return tx.SavePurchase(ctx, row)
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = fx.svc.Undo(t.Context(), ActorMCP)
	requireError(t, err, CodeConflict, "Rice")
}
