// Wire types. Must match docs/API.md and internal/basket/types.go.

export type ListName = "shopping" | "wishlist";
export type Status = "open" | "bought" | "archived";

export interface Item {
  id: string;
  list: ListName;
  title: string;
  quantity: number;
  notes: string;
  target_price: string | null;
  target_price_minor: number | null;
  recurring_days: number | null;
  status: Status;
  created_at: string;
  updated_at: string;
  version: number;
  last_purchase_date: string | null;
  due_date: string | null;
  due: boolean;
}

export interface Purchase {
  id: string;
  item_id: string;
  date: string;
  price: string | null;
  price_minor: number | null;
  quantity: number;
  store: string | null;
  created_at: string;
}

export interface Snapshot {
  today: string;
  time_zone: string;
  items: Item[];
  purchases: Purchase[];
  can_undo: boolean;
}

export interface Me {
  subject: string;
  name: string | null;
  email: string | null;
}

export type AmountInput = string | null;

export interface ItemFields {
  list?: ListName;
  title?: string;
  quantity?: number;
  notes?: string;
  target_price?: AmountInput;
  recurring_days?: number | null;
}

export type Op =
  | { op_id: string; type: "add_item"; id: string; fields: ItemFields & { title: string } }
  | { op_id: string; type: "update_item"; id: string; fields: ItemFields }
  | { op_id: string; type: "remove_item"; id: string }
  | {
      op_id: string;
      type: "mark_bought";
      id: string;
      purchase_id: string;
      date?: string;
      price?: AmountInput;
      store?: string | null;
      quantity?: number;
    }
  | {
      op_id: string;
      type: "add_purchase_details";
      purchase_id: string;
      price?: AmountInput;
      store?: string | null;
    }
  | { op_id: string; type: "reopen_item"; id: string }
  | { op_id: string; type: "archive_item"; id: string }
  | { op_id: string; type: "delete_purchase"; purchase_id: string }
  | { op_id: string; type: "undo" }
  | { op_id: string; type: "set_time_zone"; time_zone: string };

export type OpType = Op["type"];

export interface OpError {
  code: "invalid" | "not_found" | "conflict";
  message: string;
}

export interface OpResult {
  op_id: string;
  ok: boolean;
  duplicate: boolean;
  error: OpError | null;
}

export interface OpsResponse {
  results: OpResult[];
  snapshot: Snapshot;
}

export interface ApiError {
  error: { code: string; message: string };
}
