// Package mcp exposes Basket over the Model Context Protocol.
//
//nolint:exhaustruct_v5,goconst // SDK defaults and JSON schema keys are intentional.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zekihan/basket/internal/basket"
)

const maxAddItems = 50

const instructions = "Keep titles short and generic (for example, Ethernet cable); put requirements, units, " +
	"brand preferences and links in notes. Amounts are Turkish lira strings like \"189,90\" or \"1.299,90\"; " +
	"results also give integer kuruş in _minor fields. Dates are YYYY-MM-DD in the owner's time zone, defaulting " +
	"to today. When additions report similar items, ask whether to replace the existing item (update or remove it), " +
	"keep both (call again with allow_similar: true), or skip; never merge automatically. When remove_item or " +
	"price_history returns candidates, ask which one. Undo only reverts Claude's own last change, never the app's. " +
	"list_items defaults to open items."

// NewHandler returns a streamable HTTP handler, intended to be mounted behind bearer
// authentication.
func NewHandler(svc *basket.Service) http.Handler {
	server := NewServer(svc)

	return sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{
			Stateless:                  true,
			JSONResponse:               true,
			DisableLocalhostProtection: true,
		},
	)
}

// NewServer constructs the Basket MCP server.
func NewServer(svc *basket.Service) *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{Name: "basket", Version: "1.0.0"},
		&sdk.ServerOptions{Instructions: instructions},
	)
	addSchema := schema(
		map[string]any{
			"title": desc(typ("string"),
				"Short generic name, 1-200 characters, e.g. \"Ethernet cable\" or \"Shampoo\". "+
					"No sizes, units, brands or links here."),
			"list": desc(
				enum("shopping", "wishlist"),
				"shopping (default) for things to pick up soon; wishlist for things to buy when the price is right.",
			),
			"quantity": desc(
				typ("integer"),
				"1-999, default 1. Units such as \"2 kg\" go in notes.",
			),
			"notes": desc(
				typ("string"),
				"Requirements, units, preferred brand and links, e.g. \"15 m, Cat6 or higher\". Up to 2000 characters.",
			),
			"target_price": desc(
				typ("string"),
				"Price the owner would pay, Turkish lira, e.g. \"1.299,90\".",
			),
			"recurring_days": desc(
				typ("integer"),
				"Shopping items only: comes due again this many days (1-730) after the latest purchase.",
			),
			"allow_similar": desc(
				typ("boolean"),
				"Set true only after the owner chose to keep both items when a similar item was reported.",
			),
		},
		"title",
	)
	register(
		server,
		"add_item",
		"Add one item to the shopping list or wishlist. Keep the title short and generic and put "+
			"requirements, units, brand preferences and links in notes. If similar open items exist, nothing is "+
			"added and they are returned: ask the owner whether to replace the existing item (update or remove "+
			"it), keep both (call again with allow_similar: true) or skip. Never merge items yourself.",
		addSchema,
		svc.AddItem,
	)
	register(
		server,
		"add_items",
		"Add several items at once (1-50), with the same fields and rules as add_item. Each result reports "+
			"the added item or the similar open items; for each similar result ask the owner whether to replace, "+
			"keep both (allow_similar: true) or skip.",
		schema(
			map[string]any{
				"items": map[string]any{
					"type":     "array",
					"items":    addSchema,
					"minItems": 1,
					"maxItems": maxAddItems,
				},
			},
			"items",
		),
		svc.AddItems,
	)
	register(
		server,
		"list_items",
		"List items with due dates and last purchase. Defaults to open items on both lists, shopping first.",
		schema(
			map[string]any{
				"list":   enum("shopping", "wishlist"),
				"status": desc(enum("open", "bought", "archived", "all"), "Default open."),
				"query": desc(
					typ("string"),
					"Words matched as prefixes of title words, e.g. \"eth cab\".",
				),
			},
		),
		svc.ListItems,
	)
	idSchema := schema(map[string]any{"id": typ("string")}, "id")
	register(
		server,
		"get_item",
		"Get an item and its purchase history, newest first.",
		idSchema,
		svc.GetItem,
	)
	register(
		server,
		"update_item",
		"Change an item. Send only the fields to change; null clears notes, target_price or recurring_days. "+
			"Keep titles short and put details in notes. Moving an item to the wishlist clears recurring_days; "+
			"wishlist items cannot recur.",
		schema(
			map[string]any{
				"id":             typ("string"),
				"list":           enum("shopping", "wishlist"),
				"title":          typ("string"),
				"quantity":       typ("integer"),
				"notes":          nullable("string"),
				"target_price":   nullable("string"),
				"recurring_days": nullable("integer"),
			},
			"id",
		),
		func(ctx context.Context, in basket.UpdateItemInput) (basket.UpdateItemOutput, error) {
			return svc.UpdateItem(ctx, &in)
		},
	)
	find := schema(map[string]any{
		"id":    typ("string"),
		"query": desc(typ("string"), "Title words; use instead of id."),
	})
	register(
		server,
		"remove_item",
		"Delete an item and its purchases, by id or by query (exactly one). A query matching several items "+
			"removes nothing and returns candidates: ask the owner which one. Use undo to restore.",
		find,
		svc.RemoveItem,
	)
	register(
		server,
		"mark_bought",
		"Mark an item bought and log a purchase. Price (Turkish lira, e.g. \"189,90\"), store, date "+
			"(YYYY-MM-DD, default today) and quantity (default the item's) are optional.",
		schema(
			map[string]any{
				"id":       typ("string"),
				"price":    typ("string"),
				"store":    typ("string"),
				"date":     typ("string"),
				"quantity": typ("integer"),
			},
			"id",
		),
		func(ctx context.Context, in basket.MarkBoughtInput) (basket.MarkBoughtOutput, error) {
			return svc.MarkBought(ctx, &in)
		},
	)
	register(
		server,
		"reopen_item",
		"Put a bought or archived item back on its open list, e.g. a recurring item that is due again.",
		idSchema,
		svc.ReopenItem,
	)
	register(
		server,
		"price_history",
		"Purchase history of one item, newest first, with the average price (purchases without a price are "+
			"skipped). A query matching several items returns candidates: ask the owner which one.",
		find,
		svc.PriceHistory,
	)
	register(
		server,
		"due_items",
		"Recurring shopping items that are due again by today plus within_days.",
		schema(
			map[string]any{
				"within_days": desc(typ("integer"), "0-365, default 0 (due today or earlier)."),
			},
		),
		svc.DueItems,
	)
	register(
		server,
		"undo",
		"Revert Claude's own last change (never a change made in the app). Refuses when the item has changed "+
			"since. Undo is logged too, so a second undo re-applies the change.",
		schema(map[string]any{}),
		func(ctx context.Context, _ struct{}) (basket.UndoOutput, error) {
			return svc.Undo(ctx, basket.ActorMCP)
		},
	)

	return server
}
func typ(t string) map[string]any { return map[string]any{"type": t} }

func enum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func desc(s map[string]any, text string) map[string]any {
	s["description"] = text

	return s
}

func nullable(t string) map[string]any { return map[string]any{"type": []string{t, "null"}} }
func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}

	return s
}

func register[Input, Output any](
	server *sdk.Server,
	name, description string,
	input map[string]any,
	fn func(context.Context, Input) (Output, error),
) {
	server.AddTool(
		&sdk.Tool{Name: name, Description: description, InputSchema: input},
		func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			var in Input

			args := req.Params.Arguments
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}

			err := json.Unmarshal(args, &in)
			if err != nil {
				return failure(err), nil
			}

			out, err := fn(ctx, in)
			if err != nil {
				return failure(err), nil
			}

			summary := name + " completed"

			switch value := any(out).(type) {
			case basket.AddItemOutput:
				if value.Added != nil {
					summary = fmt.Sprintf("Added %s to %s", value.Added.Title, value.Added.List)
				} else {
					summary = fmt.Sprintf(
						"%d similar open items; ask which action to take",
						len(value.Similar),
					)
				}
			case basket.ListItemsOutput:
				summary = fmt.Sprintf("%d items", len(value.Items))
			case basket.RemoveItemOutput:
				if value.Removed != nil {
					summary = "Removed " + value.Removed.Title
				} else {
					summary = fmt.Sprintf("%d candidates; ask which item", len(value.Candidates))
				}
			case basket.PriceHistoryOutput:
				if len(value.Candidates) > 0 {
					summary = fmt.Sprintf("%d candidates; ask which item", len(value.Candidates))
				}
			case basket.UndoOutput:
				summary = value.Undone
			}

			return &sdk.CallToolResult{
				Content:           []sdk.Content{&sdk.TextContent{Text: summary}},
				StructuredContent: out,
			}, nil
		},
	)
}

func failure(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}},
	}
}
