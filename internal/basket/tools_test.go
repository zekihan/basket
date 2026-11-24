//nolint:exhaustruct_v5 // Fixtures specify only fields relevant to each case.
package basket

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSimilarTitlesAndQuery(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	first := toolAdd(t, fx.svc, "ŞAMPUAN")

	second, err := fx.svc.AddItem(t.Context(), AddItemInput{
		Title: "IŞIK Şampuan", List: new(ListWishlist), AllowSimilar: true,
	})
	if err != nil || second.Added == nil {
		t.Fatalf("wishlist add = %+v, %v", second, err)
	}

	cases := map[string]int{
		"  sampuan!! ":   2,
		"İŞİK   ŞAMPUAN": 2,
		"İşık":          1,
		"Clear Şampuan":  1,
		"Clear Men":      0,
		"Şamp":           0,
		"Işık Men":       0,
		"  ÇİĞ  ÖZÜN?  ": 0,
	}

	for title, want := range cases {
		matches, similarErr := fx.svc.Similar(t.Context(), title)
		if similarErr != nil || len(matches) != want {
			t.Fatalf("Similar(%q) = %+v, %v, want %d", title, matches, similarErr, want)
		}
	}

	blocked, err := fx.svc.AddItem(t.Context(), AddItemInput{Title: "Sampuan"})
	if err != nil || blocked.Added != nil || len(blocked.Similar) != 2 {
		t.Fatalf("similar add = %+v, %v", blocked, err)
	}

	list, err := fx.svc.ListItems(t.Context(), ListItemsInput{Query: "Şa"})
	if err != nil || len(list.Items) != 2 || list.Items[0].ID != first.ID {
		t.Fatalf("grouped list = %+v, %v", list, err)
	}
}

func TestQueryResolutionAcrossStatuses(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	first := toolAdd(t, fx.svc, "Şampuan")
	second := toolAdd(t, fx.svc, "Işık Şampuan")

	_, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: first.ID})
	if err != nil {
		t.Fatal(err)
	}

	matches, err := fx.svc.Similar(t.Context(), "şampuan")
	if err != nil || len(matches) != 1 || matches[0].ID != second.ID {
		t.Fatalf("similar open items = %+v, %v", matches, err)
	}

	verifyQueryResolution(t, fx.svc, first.ID, second.ID)
}

func verifyQueryResolution(t *testing.T, svc *Service, firstID, secondID string) {
	t.Helper()

	query := "şa"

	remove, err := svc.RemoveItem(t.Context(), FindItemInput{Query: &query})
	if err != nil || remove.Removed != nil || len(remove.Candidates) != 2 {
		t.Fatalf("ambiguous remove = %+v, %v", remove, err)
	}

	history, err := svc.PriceHistory(t.Context(), FindItemInput{Query: &query})
	if err != nil || history.ItemPriceHistory != nil || len(history.Candidates) != 2 {
		t.Fatalf("ambiguous history = %+v, %v", history, err)
	}

	query = "şam ışı"

	remove, err = svc.RemoveItem(t.Context(), FindItemInput{Query: &query})
	if err != nil || remove.Removed == nil || remove.Removed.ID != secondID {
		t.Fatalf("unique query remove = %+v, %v", remove, err)
	}

	_, err = svc.RemoveItem(t.Context(), FindItemInput{Query: &query})
	requireError(t, err, CodeNotFound, "query")

	_, err = svc.RemoveItem(t.Context(), FindItemInput{ID: &firstID, Query: &query})
	requireError(t, err, CodeInvalid, "id or query")
}

func TestNormalizedTokenRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		left  string
		right string
		match bool
	}{
		{"  ÇİĞ   ÖZÜN? ", "cig ozun", true},
		{"IŞIK", "ısık", true},
		{"mal, mal", "mal", true},
		{"a b", "a b", true},
		{"ab", "ab cd", false},
		{"kablo", "kablo usb", true},
		{"şarj kablo", "usb kablo", false},
		{"usb-c", "usbc", true},
		{"İzmir", "izmir", true},
	}

	for _, tc := range cases {
		if result := similarTitles(
			normalizeTitle(tc.left),
			normalizeTitle(tc.right),
		); result != tc.match {
			t.Errorf("Similar(%q, %q) = %t", tc.left, tc.right, result)
		}
	}
}

func TestBatchAndExplicitNulls(t *testing.T) {
	t.Parallel()

	fx := setup(t)

	batch, err := fx.svc.AddItems(t.Context(), AddItemsInput{Items: []AddItemInput{
		{Title: "Milk", Notes: new("notes"), TargetPrice: new("1.299,90"), RecurringDays: new(4)},
		{Title: "Milk"},
		{Title: ""},
	}})
	if err != nil || len(batch.Results) != 3 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}

	first, second, third := batch.Results[0], batch.Results[1], batch.Results[2]
	if first.Added == nil || len(first.Similar) != 0 || second.Added != nil ||
		len(second.Similar) != 1 || third.Error == nil || !strings.Contains(*third.Error, "title") {
		t.Fatalf("batch results = %+v", batch.Results)
	}

	_, err = fx.svc.AddItems(t.Context(), AddItemsInput{Items: []AddItemInput{}})
	requireError(t, err, CodeInvalid, "items")
}

func TestUpdateExplicitNulls(t *testing.T) {
	t.Parallel()

	fx := setup(t)

	result, err := fx.svc.AddItem(t.Context(), AddItemInput{
		Title: "Milk", Notes: new("notes"), TargetPrice: new("1.299,90"), RecurringDays: new(4),
	})
	if err != nil || result.Added == nil {
		t.Fatalf("add fixture = %+v, %v", result, err)
	}

	body := `{"notes":null,"target_price":null,"recurring_days":null,"quantity":3}`

	var input UpdateItemInput

	err = json.Unmarshal([]byte(body), &input)
	if err != nil {
		t.Fatal(err)
	}

	input.ID = result.Added.ID
	updated, err := fx.svc.UpdateItem(t.Context(), &input)

	if err != nil || updated.Item.Notes != "" || updated.Item.TargetPrice != nil ||
		updated.Item.RecurringDays != nil || updated.Item.Title != "Milk" || updated.Item.Quantity != 3 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}

	encoded, err := json.Marshal(input)
	if err != nil || strings.Contains(string(encoded), `"title"`) ||
		!strings.Contains(string(encoded), `"notes":null`) {
		t.Fatalf("encoded update = %s, %v", encoded, err)
	}
}

func TestPurchaseValidation(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Cookies")
	cases := []struct {
		body  string
		field string
	}{
		{`{"date":"2026-02-30"}`, "date"},
		{`{"date":"2026-11-2"}`, "date"},
		{`{"date":null}`, "date"},
		{`{"quantity":1000}`, "quantity"},
		{`{"price":"-1"}`, "price"},
		{fmt.Sprintf(`{"store":%q}`, strings.Repeat("ş", 101)), "store"},
	}

	for _, tc := range cases {
		var input MarkBoughtInput

		err := json.Unmarshal([]byte(tc.body), &input)
		if err != nil {
			t.Fatal(err)
		}

		input.ID = item.ID

		_, err = fx.svc.MarkBought(t.Context(), &input)
		requireError(t, err, CodeInvalid, tc.field)
	}

	before := toolGet(t, fx.svc, item.ID)
	if len(before.Purchases) != 0 || before.Item.Version != item.Version {
		t.Fatalf("failed purchases modified item: %+v", before)
	}
}

func TestPurchaseDetailsAndPriceHistory(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Cookies")

	first, err := fx.svc.MarkBought(t.Context(), &MarkBoughtInput{
		ID: item.ID, Price: optional(new("0,01")), Store: optional(new(strings.Repeat("ş", 100))),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = fx.svc.MarkBought(
		t.Context(),
		&MarkBoughtInput{ID: item.ID, Price: optional(new("0,02"))},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fx.svc.MarkBought(t.Context(), &MarkBoughtInput{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}

	history, err := fx.svc.PriceHistory(t.Context(), FindItemInput{ID: &item.ID})
	if err != nil || history.ItemPriceHistory == nil || len(history.Purchases) != 3 ||
		history.AveragePriceMinor == nil || *history.AveragePriceMinor != 2 ||
		history.AveragePrice == nil || *history.AveragePrice != "0,02" {
		t.Fatalf("history = %+v, %v", history, err)
	}

	verifyPurchaseEdits(t, fx.svc, item.ID, first.Purchase.ID)
}

func verifyPurchaseEdits(t *testing.T, svc *Service, itemID, purchaseID string) {
	t.Helper()

	mustApply(
		t,
		svc,
		opFromJSON(
			t,
			fmt.Sprintf(
				`{"type":"add_purchase_details","purchase_id":%q,"store":null}`,
				purchaseID,
			),
		),
		opFromJSON(t, fmt.Sprintf(`{"type":"archive_item","id":%q}`, itemID)),
	)

	got := toolGet(t, svc, itemID)
	if got.Item.Status != StatusArchived {
		t.Fatalf("archive = %+v", got.Item)
	}

	for _, purchase := range got.Purchases {
		if purchase.ID == purchaseID && (purchase.Store != nil || purchase.PriceMinor == nil ||
			*purchase.PriceMinor != 1) {
			t.Fatalf("partial purchase details = %+v", purchase)
		}
	}

	mustApply(
		t,
		svc,
		opFromJSON(t, fmt.Sprintf(`{"type":"delete_purchase","purchase_id":%q}`, purchaseID)),
	)

	export, err := svc.Export(t.Context())
	if err != nil || len(export.Items) != 1 || len(export.Purchases) != 2 ||
		export.ExportedAt != "2026-11-23T22:30:00Z" || export.TimeZone != testZone {
		t.Fatalf("export = %+v, %v", export, err)
	}

	mustApply(t, svc, opFromJSON(t, `{"type":"undo"}`))
	got = toolGet(t, svc, itemID)

	if len(got.Purchases) != 3 {
		t.Fatalf("undo delete_purchase = %+v", got)
	}
}

func TestPriceHistoryJSON(t *testing.T) {
	t.Parallel()

	fx := setup(t)
	item := toolAdd(t, fx.svc, "Raisins")

	output, err := fx.svc.PriceHistory(t.Context(), FindItemInput{ID: &item.ID})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(output)
	if err != nil || !strings.Contains(string(body), `"purchases":[]`) ||
		!strings.Contains(
			string(body),
			`"average_price":null`,
		) || strings.Contains(string(body), "candidates") {
		t.Fatalf("resolved history JSON = %s, %v", body, err)
	}

	body, err = json.Marshal(PriceHistoryOutput{Candidates: []Item{item}})
	if err != nil || !strings.HasPrefix(string(body), `{"candidates":[`) ||
		strings.Contains(string(body), "average_price") {
		t.Fatalf("ambiguous history JSON = %s, %v", body, err)
	}
}
