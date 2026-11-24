// Package basket is the service layer: validation, similar-title checks,
// recurrence, derived auto-archive and per-actor undo. Wire shapes follow
// docs/API.md and must match web/src/types.ts.
package basket

import "encoding/json"

// List names.
const (
	ListShopping = "shopping"
	ListWishlist = "wishlist"
)

// Item statuses.
const (
	StatusOpen     = "open"
	StatusBought   = "bought"
	StatusArchived = "archived"
)

// Actors recorded in the change log.
const (
	ActorPWA = "pwa"
	ActorMCP = "mcp"
)

// Op types accepted by POST /api/ops.
const (
	OpAddItem            = "add_item"
	OpUpdateItem         = "update_item"
	OpRemoveItem         = "remove_item"
	OpMarkBought         = "mark_bought"
	OpAddPurchaseDetails = "add_purchase_details"
	OpReopenItem         = "reopen_item"
	OpArchiveItem        = "archive_item"
	OpDeletePurchase     = "delete_purchase"
	OpUndo               = "undo"
	OpSetTimeZone        = "set_time_zone"
)

// Error codes.
const (
	CodeInvalid  = "invalid"
	CodeNotFound = "not_found"
	CodeConflict = "conflict"
)

// MaxOpsPerRequest bounds one POST /api/ops batch.
const MaxOpsPerRequest = 200

// Item is the wire form of an item with derived fields.
type Item struct {
	ID               string  `json:"id"`
	List             string  `json:"list"`
	Title            string  `json:"title"`
	Quantity         int     `json:"quantity"`
	Notes            string  `json:"notes"`
	TargetPrice      *string `json:"target_price"`
	TargetPriceMinor *int64  `json:"target_price_minor"`
	RecurringDays    *int    `json:"recurring_days"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	Version          int64   `json:"version"`
	LastPurchaseDate *string `json:"last_purchase_date"`
	DueDate          *string `json:"due_date"`
	Due              bool    `json:"due"`
}

// Purchase is the wire form of a purchase.
type Purchase struct {
	ID         string  `json:"id"`
	ItemID     string  `json:"item_id"`
	Date       string  `json:"date"`
	Price      *string `json:"price"`
	PriceMinor *int64  `json:"price_minor"`
	Quantity   int     `json:"quantity"`
	Store      *string `json:"store"`
	CreatedAt  string  `json:"created_at"`
}

// Snapshot is the full client state.
type Snapshot struct {
	Today     string     `json:"today"`
	TimeZone  string     `json:"time_zone"`
	Items     []Item     `json:"items"`
	Purchases []Purchase `json:"purchases"`
	CanUndo   bool       `json:"can_undo"`
}

// Export is the JSON export body.
type Export struct {
	ExportedAt string     `json:"exported_at"`
	TimeZone   string     `json:"time_zone"`
	Items      []Item     `json:"items"`
	Purchases  []Purchase `json:"purchases"`
}

// Optional distinguishes an absent JSON field from an explicit null.
// Set is true when the field was present; Null is true when it was null.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON records presence and null.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Null = true

		return nil
	}

	return json.Unmarshal(data, &o.Value)
}

// ItemFields are the writable item fields. Absent fields are untouched.
// For TargetPrice and RecurringDays, null clears; for Notes, null clears to "".
type ItemFields struct {
	List          Optional[string] `json:"list"`
	Title         Optional[string] `json:"title"`
	Quantity      Optional[int]    `json:"quantity"`
	Notes         Optional[string] `json:"notes"`
	TargetPrice   Optional[string] `json:"target_price"`
	RecurringDays Optional[int]    `json:"recurring_days"`
}

// Op is one client operation. Only the fields of its type are read.
type Op struct {
	OpID       string           `json:"op_id"`
	Type       string           `json:"type"`
	ID         string           `json:"id,omitempty"`
	Fields     ItemFields       `json:"fields"`
	PurchaseID string           `json:"purchase_id,omitempty"`
	Date       Optional[string] `json:"date"`
	Price      Optional[string] `json:"price"`
	Store      Optional[string] `json:"store"`
	Quantity   Optional[int]    `json:"quantity"`
	TimeZone   string           `json:"time_zone,omitempty"`
}

// OpsRequest is the POST /api/ops body.
type OpsRequest struct {
	Ops []Op `json:"ops"`
}

// OpError is a failed op's reason.
type OpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OpResult is the outcome of one op.
type OpResult struct {
	OpID      string   `json:"op_id"`
	OK        bool     `json:"ok"`
	Duplicate bool     `json:"duplicate"`
	Error     *OpError `json:"error"`
}

// OpsResponse is the POST /api/ops response.
type OpsResponse struct {
	Results  []OpResult `json:"results"`
	Snapshot Snapshot   `json:"snapshot"`
}
