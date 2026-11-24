//nolint:exhaustruct_v5 // Omitted fields represent defaults or absent values.
package basket

import (
	"context"
	"fmt"
	"time"

	"github.com/zekihan/basket/internal/store"
)

const archiveDays = 30

type Service struct {
	store       *store.Store
	clock       func() time.Time
	defaultZone string
}

// NewService uses time.Now when clock is nil.
func NewService(st *store.Store, defaultZone string, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}

	return &Service{store: st, defaultZone: defaultZone, clock: clock}
}

type state struct {
	tx    *store.Tx
	now   time.Time
	zone  string
	today string
}

func (svc *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		items, purchases, err := current.inventory(ctx)
		if err != nil {
			return err
		}

		change, err := current.tx.LatestChange(ctx, ActorPWA, current.now)
		if err != nil {
			return fmt.Errorf("read latest change: %w", err)
		}

		snapshot = Snapshot{
			Today: current.today, TimeZone: current.zone, Items: items,
			Purchases: purchases, CanUndo: change != nil,
		}

		return nil
	})

	return snapshot, err
}

func (svc *Service) Export(ctx context.Context) (Export, error) {
	var export Export

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		items, purchases, err := current.inventory(ctx)
		if err != nil {
			return err
		}

		export = Export{
			ExportedAt: timestamp(current.now), TimeZone: current.zone,
			Items: items, Purchases: purchases,
		}

		return nil
	})

	return export, err
}

func (svc *Service) Similar(ctx context.Context, title string) ([]Item, error) {
	var matches []Item

	err := svc.read(ctx, func(ctx context.Context, current state) error {
		var err error

		matches, err = current.similar(ctx, title)

		return err
	})

	return matches, err
}

// Prune removes history and operation results outside the retention window.
func (svc *Service) Prune(ctx context.Context) error {
	err := svc.store.Write(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.Prune(ctx, svc.clock())
	})
	if err != nil {
		return fmt.Errorf("write basket: %w", err)
	}

	return nil
}

func (svc *Service) read(ctx context.Context, run func(context.Context, state) error) error {
	err := svc.store.Read(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := svc.state(ctx, tx, svc.clock())
		if err != nil {
			return err
		}

		return run(ctx, current)
	})
	if err != nil {
		return fmt.Errorf("read basket: %w", err)
	}

	return nil
}

func (svc *Service) state(ctx context.Context, tx *store.Tx, now time.Time) (state, error) {
	zone, err := tx.TimeZone(ctx)
	if err != nil {
		return state{}, fmt.Errorf("read time zone: %w", err)
	}

	if zone == "" {
		zone = svc.defaultZone
	}

	location, err := loadZone(zone)
	if err != nil {
		return state{}, fmt.Errorf("load configured time zone: %w", err)
	}

	return state{tx: tx, now: now, zone: zone, today: now.In(location).Format(time.DateOnly)}, nil
}

func (current state) similar(ctx context.Context, title string) ([]Item, error) {
	items, _, err := current.inventory(ctx)
	if err != nil {
		return nil, err
	}

	normalized := normalizeTitle(title)
	found := make([]Item, 0)

	for index := range items {
		item := &items[index]
		if item.Status == StatusOpen && similarTitles(normalized, normalizeTitle(item.Title)) {
			found = append(found, *item)
		}
	}

	return found, nil
}

func (current state) inventory(ctx context.Context) ([]Item, []Purchase, error) {
	rows, err := current.tx.Items(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read items: %w", err)
	}

	purchases, err := current.tx.Purchases(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read purchases: %w", err)
	}

	latest := make(map[string]string)
	for _, purchase := range purchases {
		if _, exists := latest[purchase.ItemID]; !exists {
			latest[purchase.ItemID] = purchase.Date
		}
	}

	items := make([]Item, 0, len(rows))
	for index := range rows {
		row := &rows[index]

		item, err := current.itemView(row, latest[row.ID])
		if err != nil {
			return nil, nil, err
		}

		items = append(items, item)
	}

	return items, purchaseViews(purchases), nil
}

func (current state) itemView(row *store.Item, last string) (Item, error) {
	item := Item{
		ID:               row.ID,
		List:             row.List,
		Title:            row.Title,
		Quantity:         row.Quantity,
		Notes:            row.Notes,
		TargetPrice:      amountString(row.TargetPriceMinor),
		TargetPriceMinor: row.TargetPriceMinor,
		RecurringDays:    row.RecurringDays,
		Status:           row.Status,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		Version:          row.Version,
	}
	if last == "" {
		return item, nil
	}

	item.LastPurchaseDate = &last

	if row.RecurringDays != nil {
		due, err := addDays(last, *row.RecurringDays)
		if err != nil {
			return Item{}, err
		}

		item.DueDate = &due
		item.Due = item.Status == StatusBought && due <= current.today
	} else if row.Status == StatusBought {
		cutoff, err := addDays(current.today, -archiveDays)
		if err != nil {
			return Item{}, err
		}

		if last < cutoff {
			item.Status = StatusArchived
		}
	}

	return item, nil
}

func purchaseViews(rows []store.Purchase) []Purchase {
	purchases := make([]Purchase, 0, len(rows))

	for index := range rows {
		purchases = append(purchases, purchaseView(&rows[index]))
	}

	return purchases
}

func purchaseView(row *store.Purchase) Purchase {
	return Purchase{
		ID:         row.ID,
		ItemID:     row.ItemID,
		Date:       row.Date,
		Price:      amountString(row.PriceMinor),
		PriceMinor: row.PriceMinor,
		Quantity:   row.Quantity,
		Store:      row.Store,
		CreatedAt:  row.CreatedAt,
	}
}

func amountString(amount *int64) *string {
	if amount == nil {
		return nil
	}

	value := formatAmount(*amount)

	return &value
}

func timestamp(now time.Time) string { return now.UTC().Format(time.RFC3339) }
