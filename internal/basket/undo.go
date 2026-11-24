//nolint:exhaustruct_v5 // Omitted fields represent defaults or absent values.
package basket

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/zekihan/basket/internal/store"
)

type changeBody struct {
	Item      itemChange       `json:"item"`
	Purchases []purchaseChange `json:"purchases"`
}

type itemChange struct {
	Before  *store.Item `json:"before"`
	After   *store.Item `json:"after"`
	Version int64       `json:"version"`
}

func (change itemChange) item() *store.Item {
	if change.After != nil {
		return change.After
	}

	return change.Before
}

type purchaseChange struct {
	Before *store.Purchase `json:"before"`
	After  *store.Purchase `json:"after"`
}

func (current state) commit(
	ctx context.Context,
	actor, op string,
	change changeBody,
) (mutationResult, error) {
	item := change.Item.item()

	version, err := current.tx.ItemVersion(ctx, item.ID)
	if err != nil {
		return mutationResult{}, fmt.Errorf("read item version: %w", err)
	}

	change.Item.Version = version + 1

	result, err := current.saveChange(ctx, op, item.ID, &change)
	if err != nil {
		return mutationResult{}, err
	}

	err = current.tx.SaveItemVersion(ctx, item.ID, change.Item.Version)
	if err != nil {
		return mutationResult{}, fmt.Errorf("save item version: %w", err)
	}

	body, err := json.Marshal(change)
	if err != nil {
		return mutationResult{}, fmt.Errorf("encode change log: %w", err)
	}

	err = current.tx.AddChange(ctx, actor, op, string(body), current.now)
	if err != nil {
		return result, fmt.Errorf("append change: %w", err)
	}

	return result, nil
}

func (current state) saveChange(
	ctx context.Context,
	op, id string,
	change *changeBody,
) (mutationResult, error) {
	after := change.Item.After
	if after == nil {
		result, err := current.resultBeforeRemoval(ctx, change.Item.Before)
		if err != nil {
			return mutationResult{}, err
		}

		err = current.tx.DeleteItem(ctx, id)
		if err != nil {
			return result, fmt.Errorf("delete item: %w", err)
		}

		return result, nil
	}

	after.Version = change.Item.Version
	after.UpdatedAt = timestamp(current.now)

	latest, err := current.lastPurchase(ctx, id, change.Purchases)
	if err != nil {
		return mutationResult{}, err
	}

	err = current.materializeArchive(op, change.Item, latest)
	if err != nil {
		return mutationResult{}, err
	}

	item, err := current.itemView(after, latest)
	if err != nil {
		return mutationResult{}, err
	}

	err = current.tx.SaveItem(ctx, after)
	if err != nil {
		return mutationResult{}, fmt.Errorf("write item: %w", err)
	}

	purchase, err := current.savePurchases(ctx, change.Purchases)

	return mutationResult{item: item, purchase: purchase}, err
}

func (current state) savePurchases(
	ctx context.Context,
	changes []purchaseChange,
) (Purchase, error) {
	var result Purchase

	for _, change := range changes {
		if change.After == nil {
			err := current.tx.DeletePurchase(ctx, change.Before.ID)
			if err != nil {
				return Purchase{}, fmt.Errorf("remove purchase: %w", err)
			}

			continue
		}

		err := current.tx.SavePurchase(ctx, change.After)
		if err != nil {
			return Purchase{}, fmt.Errorf("write purchase: %w", err)
		}

		result = purchaseView(change.After)
	}

	return result, nil
}

func (current state) undo(ctx context.Context, actor string) (mutationResult, error) {
	previous, err := current.tx.LatestChange(ctx, actor, current.now)
	if err != nil {
		return mutationResult{}, fmt.Errorf("read latest change: %w", err)
	}

	if previous == nil {
		return mutationResult{}, notFound("change to undo")
	}

	var change changeBody

	err = json.Unmarshal([]byte(previous.Body), &change)
	if err != nil {
		return mutationResult{}, fmt.Errorf("decode change log: %w", err)
	}

	actual, err := current.checkUndo(ctx, change)
	if err != nil {
		return mutationResult{}, err
	}

	reverse := changeBody{
		Item:      itemChange{Before: actual, After: change.Item.Before},
		Purchases: make([]purchaseChange, 0, len(change.Purchases)),
	}
	for _, purchase := range change.Purchases {
		reverse.Purchases = append(reverse.Purchases, purchaseChange{
			Before: purchase.After, After: purchase.Before,
		})
	}

	// Keep the original stored status when reversing a change. Reads derive
	// archival against today's date even when the before-state was bought.
	result, err := current.commit(ctx, actor, OpUndo, reverse)
	result.undone = previous.Op + ": " + change.Item.item().Title

	return result, err
}

func (current state) checkUndo(ctx context.Context, change changeBody) (*store.Item, error) {
	target := change.Item.item()
	conflict := &OpError{
		Code:    CodeConflict,
		Message: fmt.Sprintf("item %q has changed since this operation", target.Title),
	}

	actual, err := current.tx.Item(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read item: %w", err)
	}

	version, err := current.tx.ItemVersion(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read item version: %w", err)
	}

	if version != change.Item.Version || !sameVersion(actual, change.Item.After) {
		return nil, conflict
	}

	for _, purchase := range change.Purchases {
		expected := purchase.After
		id := purchaseID(purchase)

		actual, err := current.tx.Purchase(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read purchase: %w", err)
		}

		if !reflect.DeepEqual(actual, expected) {
			return nil, conflict
		}
	}

	return actual, nil
}

func sameVersion(actual, expected *store.Item) bool {
	if expected == nil {
		return actual == nil
	}

	return actual != nil && actual.Version == expected.Version
}

func purchaseID(change purchaseChange) string {
	if change.After != nil {
		return change.After.ID
	}

	return change.Before.ID
}
