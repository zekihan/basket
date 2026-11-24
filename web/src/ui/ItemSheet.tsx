import { useEffect, useState } from "react";
import type React from "react";
import { ulid } from "../data/logic.ts";
import { getStore } from "../data/index.ts";
import { LinkifiedNotes, PrimaryButton, SecondaryButton, Sheet } from "./shared.tsx";
import type { ItemFields, Snapshot } from "../types.ts";

export function PurchaseSheet({ purchaseId, itemTitle, onClose }: { purchaseId: string; itemTitle: string; onClose: () => void }): React.JSX.Element {
  const store = getStore();
  const [price, setPrice] = useState("");
  const [storeName, setStoreName] = useState("");
  const [busy, setBusy] = useState(false);

  async function save(): Promise<void> {
    setBusy(true);
    const fields: { price?: string | null; store?: string | null } = {};
    if (price.trim()) fields.price = price.trim();
    if (storeName.trim()) fields.store = storeName.trim();
    if (Object.keys(fields).length > 0) {
      await store.enqueue({ op_id: ulid(), type: "add_purchase_details", purchase_id: purchaseId, ...fields });
    }
    setBusy(false);
    onClose();
  }

  return (
    <Sheet title={`Bought: ${itemTitle}`} onClose={onClose}>
      <div className="sheet-form">
        <div className="form-fields shell">
          <p className="hint">Add price and store if you have them. Ignoring this keeps the bare purchase.</p>
          <label className="field">
            Price
            <input
              data-testid="purchase-price"
              inputMode="decimal"
              value={price}
              onChange={event => setPrice(event.target.value)}
              placeholder="189,90"
            />
          </label>
          <label className="field">
            Store
            <input
              data-testid="purchase-store"
              value={storeName}
              onChange={event => setStoreName(event.target.value)}
              placeholder="Trendyol"
            />
          </label>
        </div>
        <div className="sheet-footer shell">
          <PrimaryButton onClick={() => void save()} disabled={busy} data-testid="purchase-save">
            Save details
          </PrimaryButton>
          <SecondaryButton onClick={onClose}>Skip</SecondaryButton>
        </div>
      </div>
    </Sheet>
  );
}

export function ItemSheet({ itemId, snapshot, onClose }: { itemId: string; snapshot: Snapshot; onClose: () => void }): React.JSX.Element {
  const store = getStore();
  const item = snapshot.items.find(entry => entry.id === itemId);
  const [title, setTitle] = useState(item?.title ?? "");
  const [quantity, setQuantity] = useState(String(item?.quantity ?? 1));
  const [notes, setNotes] = useState(item?.notes ?? "");
  const [targetPrice, setTargetPrice] = useState(item?.target_price ?? "");
  const [recurring, setRecurring] = useState(item?.recurring_days !== null && item?.recurring_days !== undefined ? String(item.recurring_days) : "");
  const [list, setList] = useState(item?.list ?? "shopping");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (item) {
      setTitle(item.title);
      setQuantity(String(item.quantity));
      setNotes(item.notes);
      setTargetPrice(item.target_price ?? "");
      setRecurring(item.recurring_days !== null && item.recurring_days !== undefined ? String(item.recurring_days) : "");
      setList(item.list);
    }
  }, [item]);

  if (!item) return <></>;
  const current = item;
  const purchases = snapshot.purchases.filter(p => p.item_id === current.id);
  const isShopping = list === "shopping";

  async function save(): Promise<void> {
    setBusy(true);
    const fields: ItemFields = {};
    if (title.trim() && title.trim() !== current.title) fields.title = title.trim();
    const qty = Number.parseInt(quantity, 10);
    if (Number.isFinite(qty) && qty >= 1 && qty <= 999 && qty !== current.quantity) fields.quantity = qty;
    if (notes !== current.notes) fields.notes = notes;
    const priceNow = targetPrice.trim() ? targetPrice.trim() : null;
    if (priceNow !== current.target_price) fields.target_price = priceNow;
    if (list !== current.list) fields.list = list;
    if (list === "wishlist") {
      if (current.recurring_days !== null) fields.recurring_days = null;
    } else if (recurring.trim()) {
      const days = Number.parseInt(recurring, 10);
      if (Number.isFinite(days) && days >= 1 && days <= 730 && days !== current.recurring_days) fields.recurring_days = days;
    } else if (current.recurring_days !== null) {
      fields.recurring_days = null;
    }
    if (Object.keys(fields).length > 0) {
      await store.enqueue({ op_id: ulid(), type: "update_item", id: current.id, fields });
    }
    setBusy(false);
    onClose();
  }

  return (
    <Sheet title={item.title} onClose={onClose}>
      <div className="sheet-form">
        <div className="form-fields shell">
          <label className="field">
            Title
            <input value={title} onChange={event => setTitle(event.target.value)} />
          </label>
          <label className="field">
            Quantity
            <input inputMode="numeric" value={quantity} onChange={event => setQuantity(event.target.value)} />
          </label>
          <label className="field">
            Notes
            <textarea value={notes} onChange={event => setNotes(event.target.value)} />
          </label>
          {notes && (
            <div className="field">
              <span className="hint">Preview</span>
              <LinkifiedNotes text={notes} />
            </div>
          )}
          <label className="field">
            Target price
            <input inputMode="decimal" value={targetPrice} onChange={event => setTargetPrice(event.target.value)} placeholder="1.299,90" />
          </label>
          {isShopping && (
            <label className="field">
              Recurring days
              <input inputMode="numeric" value={recurring} onChange={event => setRecurring(event.target.value)} placeholder="60" />
            </label>
          )}
          <fieldset>
            <legend>List</legend>
            <div className="segmented" role="group" aria-label="List">
              <button type="button" className="choice" aria-pressed={list === "shopping"} onClick={() => setList("shopping")}>Shopping</button>
              <button type="button" className="choice" aria-pressed={list === "wishlist"} onClick={() => setList("wishlist")}>Wishlist</button>
            </div>
          </fieldset>
          <fieldset>
            <legend>Purchase history</legend>
            {purchases.length === 0 ? (
              <p className="hint">No purchases yet.</p>
            ) : (
              purchases.map(purchase => (
                <div key={purchase.id} className="purchase-row">
                  <span className="num">{purchase.date} · {purchase.price ?? "—"}{purchase.store ? ` · ${purchase.store}` : ""}</span>
                  <button
                    type="button"
                    className="button small quiet"
                    data-testid="delete-purchase"
                    onClick={() => void store.enqueue({ op_id: ulid(), type: "delete_purchase", purchase_id: purchase.id })}
                  >
                    Delete
                  </button>
                </div>
              ))
            )}
          </fieldset>
        </div>
        <div className="sheet-footer shell">
          <PrimaryButton onClick={() => void save()} disabled={busy}>Save</PrimaryButton>
          <div className="actions">
            {item.status !== "open" && (
              <SecondaryButton onClick={() => { void store.enqueue({ op_id: ulid(), type: "reopen_item", id: item.id }); onClose(); }}>
                Reopen
              </SecondaryButton>
            )}
            <SecondaryButton onClick={() => { void store.enqueue({ op_id: ulid(), type: "archive_item", id: item.id }); onClose(); }}>
              Archive
            </SecondaryButton>
            <SecondaryButton onClick={() => { void store.enqueue({ op_id: ulid(), type: "remove_item", id: item.id }); onClose(); }}>
              Delete
            </SecondaryButton>
          </div>
        </div>
      </div>
    </Sheet>
  );
}
