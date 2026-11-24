package basket

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/zekihan/basket/internal/store"
)

const (
	maxNotesLength = 2000
	maxStoreLength = 100
)

func (err *OpError) Error() string { return err.Message }

func invalid(field, rule string) *OpError {
	return &OpError{Code: CodeInvalid, Message: field + " " + rule}
}

func notFound(field string) *OpError {
	return &OpError{Code: CodeNotFound, Message: field + " not found"}
}

func validID(id string) bool {
	if len(id) != ulid.EncodedSize {
		return false
	}

	parsed, err := ulid.ParseStrict(id)

	return err == nil && parsed.String() == id
}

func validateID(field, id string) error {
	if !validID(id) {
		return invalid(field, "must be an uppercase ULID")
	}

	return nil
}

func applyFields(item *store.Item, fields *ItemFields) error {
	err := applyItemText(item, fields)
	if err != nil {
		return err
	}

	err = applyItemNumbers(item, fields)
	if err != nil {
		return err
	}

	return applyRecurrence(item, fields)
}

func applyItemText(item *store.Item, fields *ItemFields) error {
	if fields.List.Set {
		if fields.List.Null || !validList(fields.List.Value) {
			return invalid("list", "must be shopping or wishlist")
		}

		item.List = fields.List.Value
	}

	if fields.Title.Set {
		item.Title = strings.TrimSpace(fields.Title.Value)
		if fields.Title.Null {
			item.Title = ""
		}
	}

	if length := utf8.RuneCountInString(item.Title); length < 1 || length > 200 {
		return invalid("title", "must have 1 to 200 characters after trimming")
	}

	if fields.Notes.Set {
		item.Notes = fields.Notes.Value
		if fields.Notes.Null {
			item.Notes = ""
		}

		if utf8.RuneCountInString(item.Notes) > maxNotesLength {
			return invalid("notes", "must have at most 2000 characters")
		}
	}

	return nil
}

func applyItemNumbers(item *store.Item, fields *ItemFields) error {
	if fields.Quantity.Set {
		quantity, err := readQuantity(fields.Quantity)
		if err != nil {
			return err
		}

		item.Quantity = quantity
	}

	if fields.TargetPrice.Set {
		amount, err := readAmount("target_price", fields.TargetPrice)
		if err != nil {
			return err
		}

		item.TargetPriceMinor = amount
	}

	return nil
}

func applyRecurrence(item *store.Item, fields *ItemFields) error {
	if item.List == ListWishlist || fields.RecurringDays.Set && fields.RecurringDays.Null {
		item.RecurringDays = nil
	}

	if !fields.RecurringDays.Set || fields.RecurringDays.Null {
		return nil
	}

	days := fields.RecurringDays.Value
	if days < 1 || days > 730 {
		return invalid("recurring_days", "must be between 1 and 730")
	}

	if item.List != ListShopping {
		return invalid("recurring_days", "is only allowed on shopping items")
	}

	item.RecurringDays = &days

	return nil
}

func validList(list string) bool {
	return list == ListShopping || list == ListWishlist
}

func readQuantity(quantity Optional[int]) (int, error) {
	if quantity.Null || quantity.Value < 1 || quantity.Value > 999 {
		return 0, invalid("quantity", "must be between 1 and 999")
	}

	return quantity.Value, nil
}

func readAmount(field string, input Optional[string]) (*int64, error) {
	if input.Null {
		return nil, nil //nolint:nilnil // Null clears the amount.
	}

	amount, err := parseAmount(input.Value)
	if err != nil {
		return nil, invalid(field, "must be a valid nonnegative amount")
	}

	return &amount, nil
}

func purchaseDetails(purchase *store.Purchase, op *Op) error {
	if op.Price.Set {
		price, err := readAmount("price", op.Price)
		if err != nil {
			return err
		}

		purchase.PriceMinor = price
	}

	if op.Store.Set {
		purchase.Store = nil

		if !op.Store.Null {
			if utf8.RuneCountInString(op.Store.Value) > maxStoreLength {
				return invalid("store", "must have at most 100 characters")
			}

			purchase.Store = &op.Store.Value
		}
	}

	return nil
}

func loadZone(zone string) (*time.Location, error) {
	if zone != "" && zone != "Local" {
		location, err := time.LoadLocation(zone)
		if err == nil {
			return location, nil
		}
	}

	return nil, invalid("time_zone", "must be an IANA time zone")
}

func addDays(date string, days int) (string, error) {
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return "", fmt.Errorf("parse calendar date: %w", err)
	}

	return parsed.AddDate(0, 0, days).Format(time.DateOnly), nil
}
