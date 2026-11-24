//nolint:exhaustruct_v5 // Omitted fields represent defaults or absent values.
package basket

import (
	"context"
	"fmt"
	"time"

	"github.com/zekihan/basket/internal/store"
)

func (current state) addItem(ctx context.Context, op *Op) (changeBody, error) {
	err := validateID("id", op.ID)
	if err != nil {
		return changeBody{}, err
	}

	existing, err := current.tx.Item(ctx, op.ID)
	if err != nil {
		return changeBody{}, fmt.Errorf("read item: %w", err)
	}

	if existing != nil {
		return changeBody{}, invalid("id", "already exists")
	}

	item := &store.Item{
		ID: op.ID, List: ListShopping, Quantity: 1, Status: StatusOpen,
		CreatedAt: timestamp(current.now),
	}

	err = applyFields(item, &op.Fields)
	if err != nil {
		return changeBody{}, err
	}

	return changeBody{Item: itemChange{After: item}}, nil
}

func (current state) item(ctx context.Context, id string) (*store.Item, error) {
	err := validateID("id", id)
	if err != nil {
		return nil, err
	}

	item, err := current.tx.Item(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read item: %w", err)
	}

	if item == nil {
		return nil, notFound("id")
	}

	return item, nil
}

func (current state) changeItem(ctx context.Context, op *Op) (changeBody, error) {
	before, err := current.item(ctx, op.ID)
	if err != nil {
		return changeBody{}, err
	}

	after := *before
	change := changeBody{Item: itemChange{Before: before, After: &after}}

	switch op.Type {
	case OpUpdateItem:
		err = applyFields(&after, &op.Fields)
	case OpReopenItem:
		if before.Status == StatusOpen {
			return changeBody{}, invalid("status", "must be bought or archived to reopen")
		}

		after.Status = StatusOpen
	case OpArchiveItem:
		after.Status = StatusArchived
	case OpRemoveItem:
		change.Item.After = nil
		change.Purchases, err = current.deletedPurchases(ctx, op.ID)
	case OpMarkBought:
		after.Status = StatusBought

		var purchase *store.Purchase

		purchase, err = current.newPurchase(ctx, before, op)
		change.Purchases = []purchaseChange{{After: purchase}}
	}

	return change, err
}

func (current state) deletedPurchases(ctx context.Context, id string) ([]purchaseChange, error) {
	rows, err := current.tx.ItemPurchases(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read item purchases: %w", err)
	}

	deleted := make([]purchaseChange, 0, len(rows))

	for index := range rows {
		deleted = append(deleted, purchaseChange{Before: &rows[index]})
	}

	return deleted, nil
}

func (current state) newPurchase(
	ctx context.Context,
	item *store.Item,
	op *Op,
) (*store.Purchase, error) {
	err := validateID("purchase_id", op.PurchaseID)
	if err != nil {
		return nil, err
	}

	existing, err := current.tx.Purchase(ctx, op.PurchaseID)
	if err != nil {
		return nil, fmt.Errorf("read purchase: %w", err)
	}

	if existing != nil {
		return nil, invalid("purchase_id", "already exists")
	}

	purchase := &store.Purchase{
		ID: op.PurchaseID, ItemID: item.ID, Date: current.today, Quantity: item.Quantity,
		CreatedAt: timestamp(current.now),
	}
	if op.Date.Set {
		_, dateErr := time.Parse(time.DateOnly, op.Date.Value)
		if op.Date.Null || dateErr != nil {
			return nil, invalid("date", "must be YYYY-MM-DD")
		}

		purchase.Date = op.Date.Value
	}

	if op.Quantity.Set {
		purchase.Quantity, err = readQuantity(op.Quantity)
		if err != nil {
			return nil, err
		}
	}

	err = purchaseDetails(purchase, op)
	if err != nil {
		return nil, err
	}

	return purchase, nil
}

func (current state) changePurchase(ctx context.Context, op *Op) (changeBody, error) {
	err := validateID("purchase_id", op.PurchaseID)
	if err != nil {
		return changeBody{}, err
	}

	before, err := current.tx.Purchase(ctx, op.PurchaseID)
	if err != nil {
		return changeBody{}, fmt.Errorf("read purchase: %w", err)
	}

	if before == nil {
		return changeBody{}, notFound("purchase_id")
	}

	item, err := current.item(ctx, before.ItemID)
	if err != nil {
		return changeBody{}, err
	}

	updated := *item
	change := changeBody{
		Item:      itemChange{Before: item, After: &updated},
		Purchases: []purchaseChange{{Before: before}},
	}

	if op.Type == OpAddPurchaseDetails {
		after := *before

		err = purchaseDetails(&after, op)
		if err != nil {
			return changeBody{}, err
		}

		change.Purchases[0].After = &after
	}

	return change, nil
}

func (current state) lastPurchase(
	ctx context.Context,
	id string,
	changes []purchaseChange,
) (string, error) {
	excluded := make(map[string]bool, len(changes))

	var latest string

	for _, change := range changes {
		if change.Before != nil {
			excluded[change.Before.ID] = true
		}

		if change.After != nil {
			latest = max(latest, change.After.Date)
		}
	}

	rows, err := current.tx.ItemPurchases(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read item purchases: %w", err)
	}

	for _, row := range rows {
		if !excluded[row.ID] {
			return max(latest, row.Date), nil
		}
	}

	return latest, nil
}

func (current state) resultBeforeRemoval(
	ctx context.Context,
	item *store.Item,
) (mutationResult, error) {
	latest, err := current.lastPurchase(ctx, item.ID, nil)
	if err != nil {
		return mutationResult{}, err
	}

	view, err := current.itemView(item, latest)

	return mutationResult{item: view}, err
}

// Status changes from mark_bought are explicit. For other writes, retain an
// existing derived archive even when an update also enables recurrence.
func (current state) materializeArchive(op string, change itemChange, latest string) error {
	if op == OpUndo || op == OpMarkBought || change.After.Status != StatusBought || latest == "" {
		return nil
	}

	recurs := change.After.RecurringDays != nil
	if recurs && (change.Before == nil || change.Before.RecurringDays != nil) {
		return nil
	}

	cutoff, err := addDays(current.today, -archiveDays)
	if err != nil {
		return err
	}

	if latest < cutoff {
		change.After.Status = StatusArchived
	}

	return nil
}
