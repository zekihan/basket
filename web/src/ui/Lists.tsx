import { useEffect, useRef, useState } from "react";
import type React from "react";
import { dueAgainItems, openItems, splitQuickAdd, ulid } from "../data/logic.ts";
import { getStore } from "../data/index.ts";
import { LinkifiedNotes } from "./shared.tsx";
import type { Item, ListName, Snapshot } from "../types.ts";

export function QuickAdd({ list }: { list: ListName }): React.JSX.Element {
  const store = getStore();
  const [value, setValue] = useState("");
  const [similar, setSimilar] = useState<Item[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (timer.current) clearTimeout(timer.current);
    const parsed = splitQuickAdd(value);
    if (!parsed || !store.getState().online) {
      setSimilar([]);
      return;
    }
    timer.current = setTimeout(() => {
      void store.fetchSimilar(parsed.title).then(setSimilar);
    }, 300);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [value, store]);

  function submit(event: React.FormEvent): void {
    event.preventDefault();
    const parsed = splitQuickAdd(value);
    if (!parsed) return;
    void store.enqueue({
      op_id: ulid(),
      type: "add_item",
      id: ulid(),
      fields: { title: parsed.title, list, ...(parsed.notes ? { notes: parsed.notes } : {}) },
    });
    setValue("");
    setSimilar([]);
  }

  return (
    <div className="quick-add">
      <form onSubmit={submit} role="search" aria-label="Quick add">
        <label className="field">
          <span className="hint">Add, e.g. “charger cable, usb-c 60W”</span>
          <input
            data-testid="quick-add"
            value={value}
            onChange={event => setValue(event.target.value)}
            placeholder={`Add to ${list}…`}
            autoComplete="off"
            enterKeyHint="send"
          />
        </label>
      </form>
      {similar.length > 0 && (
        <p className="similar-hint" role="status">
          Similar open: {similar.map(item => item.title).join(", ")}
        </p>
      )}
    </div>
  );
}

export function ItemRow({
  item,
  list,
  onOpen,
  onBought,
  onRemove,
}: {
  item: Item;
  list: ListName;
  onOpen: (item: Item) => void;
  onBought: (item: Item) => void;
  onRemove: (item: Item) => void;
}): React.JSX.Element {
  const [offsetX, setOffsetX] = useState(0);
  const startX = useRef<number | null>(null);
  const moved = useRef(false);

  function onTouchStart(event: React.TouchEvent): void {
    startX.current = event.touches[0]?.clientX ?? null;
    moved.current = false;
  }

  function onTouchMove(event: React.TouchEvent): void {
    if (startX.current === null) return;
    const x = event.touches[0]?.clientX ?? startX.current;
    const dx = x - startX.current;
    if (Math.abs(dx) > 8) moved.current = true;
    setOffsetX(dx);
  }

  function onTouchEnd(): void {
    if (startX.current === null) return;
    const dx = offsetX;
    startX.current = null;
    setOffsetX(0);
    if (dx > 72) onBought(item);
    else if (dx < -72) onRemove(item);
  }

  return (
    <li
      className="item-row"
      data-testid="item-row"
      data-item-id={item.id}
      style={offsetX !== 0 ? { transform: `translateX(${offsetX}px)` } : undefined}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
    >
      <button
        type="button"
        className="item-row-main"
        onClick={() => {
          if (moved.current) return;
          onOpen(item);
        }}
        aria-label={item.title}
      >
        <span>{item.title}</span>
        {item.quantity > 1 && <span className="hint num">×{item.quantity}</span>}
      </button>
      {item.notes && <LinkifiedNotes text={item.notes} />}
      <span className="item-meta">
        {list === "wishlist" && item.target_price && <span className="num">Target {item.target_price}</span>}
        {item.list === "shopping" && item.recurring_days && <span>Every {item.recurring_days}d</span>}
      </span>
      <span className="item-actions">
        <button
          type="button"
          className="button small secondary"
          data-testid="bought-button"
          onClick={() => onBought(item)}
        >
          Bought
        </button>
        <button
          type="button"
          className="button small quiet"
          data-testid="delete-button"
          onClick={() => onRemove(item)}
        >
          Remove
        </button>
      </span>
    </li>
  );
}

export function DueAgain({ snapshot, onReopen }: { snapshot: Snapshot; onReopen: (item: Item) => void }): React.JSX.Element {
  const due = dueAgainItems(snapshot);
  if (due.length === 0) return <></>;
  return (
    <section className="due-again" data-testid="due-again" aria-label="Due again">
      <h2>Due again</h2>
      <ul className="list-card">
        {due.map(item => (
          <li key={item.id} className="item-row" data-testid="item-row" data-item-id={item.id}>
            <span className="item-row-main">{item.title}</span>
            <span className="item-meta">Due {item.due_date ?? ""}</span>
            <span className="item-actions">
              <button
                type="button"
                className="button small secondary"
                data-testid="reopen-button"
                onClick={() => onReopen(item)}
              >
                Reopen
              </button>
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function ListTab({
  snapshot,
  list,
  onOpen,
  onBought,
  onRemove,
  onReopen,
}: {
  snapshot: Snapshot;
  list: ListName;
  onOpen: (item: Item) => void;
  onBought: (item: Item) => void;
  onRemove: (item: Item) => void;
  onReopen: (item: Item) => void;
}): React.JSX.Element {
  const items = openItems(snapshot, list);
  return (
    <section aria-label={list === "shopping" ? "Shopping" : "Wishlist"}>
      {list === "shopping" && <DueAgain snapshot={snapshot} onReopen={onReopen} />}
      {items.length === 0 ? (
        <p className="empty">Nothing open here yet.</p>
      ) : (
        <ul className="list-card">
          {items.map(item => (
            <ItemRow key={item.id} item={item} list={list} onOpen={onOpen} onBought={onBought} onRemove={onRemove} />
          ))}
        </ul>
      )}
    </section>
  );
}
