//nolint:exhaustruct_v5 // Omitted fields represent defaults or absent values.
package basket

import (
	"context"

	"github.com/oklog/ulid/v2"
)

//nolint:govet,tagliatelle // Keep the MCP fields in contract order.
type AddItemInput struct {
	Title         string  `json:"title"`
	List          *string `json:"list,omitempty"`
	Quantity      *int    `json:"quantity,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	TargetPrice   *string `json:"target_price,omitempty"`
	RecurringDays *int    `json:"recurring_days,omitempty"`
	AllowSimilar  bool    `json:"allow_similar,omitempty"`
}

type AddItemOutput struct {
	Added   *Item  `json:"added"`
	Similar []Item `json:"similar"`
}

type AddItemsInput struct {
	Items []AddItemInput `json:"items"`
}

type AddItemsOutput struct {
	Results []AddItemsResult `json:"results"`
}

//nolint:govet // Group the base add result fields together.
type AddItemsResult struct {
	AddItemOutput

	Error *string `json:"error"`
	Title string  `json:"title"`
}

type ListItemsInput struct {
	List   string `json:"list,omitempty"`
	Status string `json:"status,omitempty"`
	Query  string `json:"query,omitempty"`
}

type ListItemsOutput struct {
	Items []Item `json:"items"`
}

type GetItemInput struct {
	ID string `json:"id"`
}

type GetItemOutput struct {
	Item      Item       `json:"item"`
	Purchases []Purchase `json:"purchases"`
}

//nolint:govet // Keep the embedded item fields together.
type UpdateItemInput struct {
	ItemFields

	ID string `json:"id"`
}

type UpdateItemOutput struct {
	Item Item `json:"item"`
}

type FindItemInput struct {
	ID    *string `json:"id,omitempty"`
	Query *string `json:"query,omitempty"`
}

type RemoveItemOutput struct {
	Removed    *Item  `json:"removed"`
	Candidates []Item `json:"candidates"`
}

type MarkBoughtInput struct {
	ID       string           `json:"id"`
	Price    Optional[string] `json:"price,omitzero"`
	Store    Optional[string] `json:"store,omitzero"`
	Date     Optional[string] `json:"date,omitzero"`
	Quantity Optional[int]    `json:"quantity,omitzero"`
}

type MarkBoughtOutput struct {
	Item     Item     `json:"item"`
	Purchase Purchase `json:"purchase"`
}

type PriceHistoryOutput struct {
	*ItemPriceHistory

	Candidates []Item `json:"candidates,omitempty"`
}

//nolint:govet,tagliatelle // Group the base item and purchase fields together.
type ItemPriceHistory struct {
	GetItemOutput

	AveragePrice      *string `json:"average_price"`
	AveragePriceMinor *int64  `json:"average_price_minor"`
}

//nolint:tagliatelle // The MCP contract uses snake_case.
type DueItemsInput struct {
	WithinDays int `json:"within_days,omitempty"`
}

type UndoOutput struct {
	Undone string `json:"undone"`
}

func (svc *Service) AddItem(ctx context.Context, input AddItemInput) (AddItemOutput, error) {
	output := AddItemOutput{Added: nil, Similar: []Item{}}

	err := svc.write(ctx, func(ctx context.Context, current state) error {
		op := Op{Type: OpAddItem, ID: ulid.Make().String(), Fields: input.fields()}

		// Validate before returning a similarity hint, and hold the write
		// transaction to prevent concurrent additions between check and insert.
		change, err := current.addItem(ctx, &op)
		if err != nil {
			return err
		}

		if !input.AllowSimilar {
			output.Similar, err = current.similar(ctx, change.Item.After.Title)
			if err != nil || len(output.Similar) != 0 {
				return err
			}
		}

		result, err := current.commit(ctx, ActorMCP, op.Type, change)
		if err != nil {
			return err
		}

		output.Added = &result.item

		return nil
	})

	return output, err
}

func (input AddItemInput) fields() ItemFields {
	return ItemFields{
		Title: Optional[string]{Set: true, Value: input.Title},
		List:  optional(input.List), Quantity: optional(input.Quantity),
		Notes: optional(input.Notes), TargetPrice: optional(input.TargetPrice),
		RecurringDays: optional(input.RecurringDays),
	}
}

func optional[T any](value *T) Optional[T] {
	if value == nil {
		return Optional[T]{}
	}

	return Optional[T]{Set: true, Value: *value}
}

func (svc *Service) AddItems(ctx context.Context, input AddItemsInput) (AddItemsOutput, error) {
	if len(input.Items) < 1 || len(input.Items) > 50 {
		return AddItemsOutput{}, invalid("items", "must have 1 to 50 entries")
	}

	results := make([]AddItemsResult, 0, len(input.Items))
	for _, entry := range input.Items {
		output, err := svc.AddItem(ctx, entry)
		result := AddItemsResult{Title: entry.Title, AddItemOutput: output}

		if err != nil {
			message := err.Error()
			result.Error = &message
		}

		results = append(results, result)
	}

	return AddItemsOutput{Results: results}, nil
}

func (svc *Service) UpdateItem(
	ctx context.Context,
	input *UpdateItemInput,
) (UpdateItemOutput, error) {
	result, err := svc.toolOp(ctx, ActorMCP, &Op{
		Type: OpUpdateItem, ID: input.ID, Fields: input.ItemFields,
	})

	return UpdateItemOutput{Item: result.item}, err
}

func (svc *Service) RemoveItem(ctx context.Context, input FindItemInput) (RemoveItemOutput, error) {
	output := RemoveItemOutput{Removed: nil, Candidates: []Item{}}

	err := svc.write(ctx, func(ctx context.Context, current state) error {
		id, candidates, err := current.resolveItem(ctx, input)
		if err != nil {
			return err
		}

		if len(candidates) > 0 {
			output.Candidates = candidates

			return nil
		}

		result, err := current.apply(ctx, ActorMCP, &Op{Type: OpRemoveItem, ID: id})
		if err != nil {
			return err
		}

		output.Removed = &result.item

		return nil
	})

	return output, err
}

func (svc *Service) MarkBought(
	ctx context.Context,
	input *MarkBoughtInput,
) (MarkBoughtOutput, error) {
	result, err := svc.toolOp(ctx, ActorMCP, &Op{
		Type: OpMarkBought, ID: input.ID, PurchaseID: ulid.Make().String(),
		Price: input.Price, Store: input.Store, Quantity: input.Quantity, Date: input.Date,
	})

	return MarkBoughtOutput{Item: result.item, Purchase: result.purchase}, err
}

func (svc *Service) ReopenItem(ctx context.Context, input GetItemInput) (UpdateItemOutput, error) {
	result, err := svc.toolOp(ctx, ActorMCP, &Op{Type: OpReopenItem, ID: input.ID})

	return UpdateItemOutput{Item: result.item}, err
}

func (svc *Service) Undo(ctx context.Context, actor string) (UndoOutput, error) {
	result, err := svc.toolOp(ctx, actor, &Op{Type: OpUndo})

	return UndoOutput{Undone: result.undone}, err
}

func (svc *Service) toolOp(ctx context.Context, actor string, op *Op) (mutationResult, error) {
	var result mutationResult

	err := svc.write(ctx, func(ctx context.Context, current state) error {
		var err error

		result, err = current.apply(ctx, actor, op)

		return err
	})

	return result, err
}
