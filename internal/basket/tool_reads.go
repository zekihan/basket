package basket

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
)

const statusAll = "all"

func (svc *Service) ListItems(ctx context.Context, input ListItemsInput) (ListItemsOutput, error) {
	if input.List != "" && !validList(input.List) {
		return ListItemsOutput{}, invalid("list", "must be shopping or wishlist")
	}

	switch input.Status {
	case "":
		input.Status = StatusOpen
	case StatusOpen, StatusBought, StatusArchived, statusAll:
	default:
		return ListItemsOutput{}, invalid("status", "must be open, bought, archived or all")
	}

	output := ListItemsOutput{Items: []Item{}}

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		items, _, err := current.inventory(ctx)
		if err != nil {
			return err
		}

		tokens := strings.Fields(normalizeTitle(input.Query))

		for index := range items {
			item := &items[index]
			if includeItem(item, input, tokens) {
				output.Items = append(output.Items, *item)
			}
		}

		slices.SortStableFunc(output.Items, func(first, second Item) int {
			return cmp.Compare(first.List, second.List)
		})

		return nil
	})

	return output, err
}

func includeItem(item *Item, input ListItemsInput, tokens []string) bool {
	return (input.List == "" || input.List == item.List) &&
		(input.Status == statusAll || input.Status == item.Status) &&
		matchesQuery(item.Title, tokens)
}

func (svc *Service) GetItem(ctx context.Context, input GetItemInput) (GetItemOutput, error) {
	var output GetItemOutput

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		var err error

		output, err = current.getItem(ctx, input.ID)

		return err
	})

	return output, err
}

func (current state) getItem(ctx context.Context, id string) (GetItemOutput, error) {
	row, err := current.item(ctx, id)
	if err != nil {
		return GetItemOutput{}, err
	}

	rows, err := current.tx.ItemPurchases(ctx, id)
	if err != nil {
		return GetItemOutput{}, fmt.Errorf("read item purchases: %w", err)
	}

	var latest string
	if len(rows) > 0 {
		latest = rows[0].Date
	}

	item, err := current.itemView(row, latest)

	return GetItemOutput{Item: item, Purchases: purchaseViews(rows)}, err
}

func (current state) resolveItem(ctx context.Context, input FindItemInput) (string, []Item, error) {
	if (input.ID == nil) == (input.Query == nil) {
		return "", nil, invalid("id or query", "must supply exactly one")
	}

	if input.ID != nil {
		err := validateID("id", *input.ID)

		return *input.ID, nil, err
	}

	tokens := strings.Fields(normalizeTitle(*input.Query))
	if len(tokens) == 0 {
		return "", nil, invalid("query", "must contain a search token")
	}

	items, _, err := current.inventory(ctx)
	if err != nil {
		return "", nil, err
	}

	var matches []Item

	for index := range items {
		item := &items[index]
		if matchesQuery(item.Title, tokens) {
			matches = append(matches, *item)
		}
	}

	switch len(matches) {
	case 0:
		return "", nil, notFound("query")
	case 1:
		return matches[0].ID, nil, nil
	default:
		return "", matches, nil
	}
}

func (svc *Service) PriceHistory(
	ctx context.Context,
	input FindItemInput,
) (PriceHistoryOutput, error) {
	var output PriceHistoryOutput

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		id, candidates, err := current.resolveItem(ctx, input)
		if err != nil {
			return err
		}

		if len(candidates) > 0 {
			output.Candidates = candidates

			return nil
		}

		item, err := current.getItem(ctx, id)
		if err != nil {
			return err
		}

		average := averagePrice(item.Purchases)
		output.ItemPriceHistory = &ItemPriceHistory{
			GetItemOutput: item, AveragePrice: amountString(average), AveragePriceMinor: average,
		}

		return nil
	})

	return output, err
}

func averagePrice(purchases []Purchase) *int64 {
	var (
		sum, value big.Int
		count      int64
	)

	for _, purchase := range purchases {
		if purchase.PriceMinor != nil {
			sum.Add(&sum, value.SetInt64(*purchase.PriceMinor))

			count++
		}
	}

	if count == 0 {
		return nil
	}

	// Adding floor(count/2) before dividing rounds nonnegative values half up.
	const half = 2

	halfCount := count / half
	sum.Add(&sum, value.SetInt64(halfCount))
	sum.Quo(&sum, value.SetInt64(count))
	result := sum.Int64()

	return &result
}

func (svc *Service) DueItems(ctx context.Context, input DueItemsInput) (ListItemsOutput, error) {
	if input.WithinDays < 0 || input.WithinDays > 365 {
		return ListItemsOutput{}, invalid("within_days", "must be between 0 and 365")
	}

	output := ListItemsOutput{Items: []Item{}}

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		limit, err := addDays(current.today, input.WithinDays)
		if err != nil {
			return err
		}

		items, _, err := current.inventory(ctx)
		if err != nil {
			return err
		}

		for index := range items {
			item := &items[index]
			if item.Status == StatusBought && item.DueDate != nil && *item.DueDate <= limit {
				output.Items = append(output.Items, *item)
			}
		}

		return nil
	})

	return output, err
}
