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
			"title":          typ("string"),
			"list":           typ("string"),
			"quantity":       typ("integer"),
			"notes":          typ("string"),
			"target_price":   typ("string"),
			"recurring_days": typ("integer"),
			"allow_similar":  typ("boolean"),
		},
		"title",
	)
	register(
		server,
		"add_item",
		"Add an item; ask about similar items before deciding to replace, keep both, or skip.",
		addSchema,
		svc.AddItem,
	)
	register(
		server,
		"add_items",
		"Add multiple items; ask about each similar item before deciding.",
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
		"List items; defaults to open items, optionally filter by list, status or query.",
		schema(
			map[string]any{"list": typ("string"), "status": typ("string"), "query": typ("string")},
		),
		svc.ListItems,
	)
	idSchema := schema(map[string]any{"id": typ("string")}, "id")
	register(server, "get_item", "Get an item and its purchases.", idSchema, svc.GetItem)
	register(
		server,
		"update_item",
		"Update an item; null clears notes, target_price or recurring_days.",
		schema(
			map[string]any{
				"id":             typ("string"),
				"list":           typ("string"),
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
	find := schema(map[string]any{"id": typ("string"), "query": typ("string")})
	register(
		server,
		"remove_item",
		"Remove by id or query; ask which item if candidates are returned.",
		find,
		svc.RemoveItem,
	)
	register(
		server,
		"mark_bought",
		"Mark an item bought, optionally recording price, store, date and quantity.",
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
	register(server, "reopen_item", "Reopen a bought item.", idSchema, svc.ReopenItem)
	register(
		server,
		"price_history",
		"Get purchase history and average price; ask which item if candidates are returned.",
		find,
		svc.PriceHistory,
	)
	register(
		server,
		"due_items",
		"List recurring items due within the given number of days.",
		schema(map[string]any{"within_days": typ("integer")}),
		svc.DueItems,
	)
	register(
		server,
		"undo",
		"Undo Claude's own last change, never the app's; a second undo re-applies it.",
		schema(map[string]any{}),
		func(ctx context.Context, _ struct{}) (basket.UndoOutput, error) {
			return svc.Undo(ctx, basket.ActorMCP)
		},
	)

	return server
}
func typ(t string) map[string]any      { return map[string]any{"type": t} }
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

			err := json.Unmarshal(req.Params.Arguments, &in)
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
