//nolint:exhaustruct_v5 // Omitted fields represent defaults or absent values.
package basket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zekihan/basket/internal/store"
)

func (svc *Service) ApplyOps(ctx context.Context, actor string, ops []Op) ([]OpResult, error) {
	if len(ops) > MaxOpsPerRequest {
		return nil, invalid("ops", "must have at most 200 entries")
	}

	results := make([]OpResult, 0, len(ops))

	for index := range ops {
		result, err := svc.applyOp(ctx, actor, &ops[index])
		if err != nil {
			return nil, err
		}

		results = append(results, result)
	}

	return results, nil
}

func (svc *Service) applyOp(ctx context.Context, actor string, op *Op) (OpResult, error) {
	result := OpResult{OpID: op.OpID}

	err := svc.write(ctx, func(ctx context.Context, current state) error {
		stored, err := current.tx.AppliedOp(ctx, op.OpID, current.now)
		if err != nil {
			return fmt.Errorf("read applied operation: %w", err)
		}

		if stored != nil {
			err = json.Unmarshal([]byte(*stored), &result)
			if err != nil {
				return fmt.Errorf("decode op result: %w", err)
			}

			result.Duplicate = true

			return nil
		}

		if validID(op.OpID) {
			_, err = current.apply(ctx, actor, op)
			if err != nil && !errors.As(err, &result.Error) {
				return err
			}
		} else {
			result.Error = invalid("op_id", "must be an uppercase ULID")
		}

		result.OK = result.Error == nil

		encoded, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode op result: %w", err)
		}

		return current.tx.SaveAppliedOp(ctx, op.OpID, string(encoded), current.now)
	})

	return result, err
}

func (svc *Service) write(ctx context.Context, run func(context.Context, state) error) error {
	err := svc.store.Write(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := svc.state(ctx, tx, svc.clock())
		if err != nil {
			return err
		}

		err = tx.Prune(ctx, current.now)
		if err != nil {
			return fmt.Errorf("prune history: %w", err)
		}

		return run(ctx, current)
	})
	if err != nil {
		return fmt.Errorf("write basket: %w", err)
	}

	return nil
}

type mutationResult struct {
	item     Item
	purchase Purchase
	undone   string
}

func (current state) apply(ctx context.Context, actor string, op *Op) (mutationResult, error) {
	switch op.Type {
	case OpSetTimeZone:
		_, err := loadZone(op.TimeZone)
		if err != nil {
			return mutationResult{}, err
		}

		err = current.tx.SetTimeZone(ctx, op.TimeZone)
		if err != nil {
			return mutationResult{}, fmt.Errorf("save time zone: %w", err)
		}

		return mutationResult{}, nil
	case OpUndo:
		return current.undo(ctx, actor)
	default:
		change, err := current.prepare(ctx, op)
		if err != nil {
			return mutationResult{}, err
		}

		return current.commit(ctx, actor, op.Type, change)
	}
}

func (current state) prepare(ctx context.Context, op *Op) (changeBody, error) {
	switch op.Type {
	case OpAddItem:
		return current.addItem(ctx, op)
	case OpUpdateItem, OpReopenItem, OpArchiveItem, OpRemoveItem, OpMarkBought:
		return current.changeItem(ctx, op)
	case OpAddPurchaseDetails, OpDeletePurchase:
		return current.changePurchase(ctx, op)
	default:
		return changeBody{}, invalid("type", "is not a recognized operation")
	}
}
