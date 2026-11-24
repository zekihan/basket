import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import type React from "react";
import { getStore, registerServiceWorker } from "../data/index.ts";
import { archivedItems, boughtItems, ulid } from "../data/logic.ts";
import { ListTab, QuickAdd } from "./Lists.tsx";
import { ItemSheet, PurchaseSheet } from "./ItemSheet.tsx";
import { Settings } from "./Settings.tsx";
import { LinkifiedNotes } from "./shared.tsx";
import type { Item, ListName, Snapshot } from "../types.ts";

type View = "shopping" | "wishlist" | "bought" | "settings";

const VIEWS: { id: View; label: string }[] = [
  { id: "shopping", label: "Shopping" },
  { id: "wishlist", label: "Wishlist" },
  { id: "bought", label: "Bought" },
  { id: "settings", label: "Settings" },
];

export default function App(): React.JSX.Element {
  const store = getStore();
  const state = useSyncExternalStore(store.subscribe, store.getState);
  const [view, setView] = useState<View>("shopping");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [purchaseFor, setPurchaseFor] = useState<{ purchaseId: string; title: string } | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [showUndo, setShowUndo] = useState(false);
  const undoTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastMutation = useRef<number | null>(null);

  useEffect(() => {
    registerServiceWorker();
    void store.startup();
  }, [store]);

  useEffect(() => {
    if (state.lastMutationAt !== null && state.lastMutationAt !== lastMutation.current) {
      lastMutation.current = state.lastMutationAt;
      setShowUndo(true);
      if (undoTimer.current) clearTimeout(undoTimer.current);
      undoTimer.current = setTimeout(() => setShowUndo(false), 5000);
    }
    return () => {
      if (undoTimer.current) clearTimeout(undoTimer.current);
    };
  }, [state.lastMutationAt]);

  if (state.signedOut) {
    return (
      <div className="gate">
        <div className="card gate-card">
          <h1>Basket</h1>
          <p>Sign in to view your lists.</p>
          <a className="button primary" href="/auth/login">Sign in</a>
        </div>
      </div>
    );
  }

  const snapshot = state.snapshot;
  const pending = state.queue.length;
  const canUndo = !!(snapshot && snapshot.can_undo && showUndo);

  function handleBought(item: Item): void {
    const purchaseId = ulid();
    void store.enqueue({ op_id: ulid(), type: "mark_bought", id: item.id, purchase_id: purchaseId });
    setPurchaseFor({ purchaseId, title: item.title });
  }

  function handleRemove(item: Item): void {
    void store.enqueue({ op_id: ulid(), type: "remove_item", id: item.id });
  }

  function handleReopen(item: Item): void {
    void store.enqueue({ op_id: ulid(), type: "reopen_item", id: item.id });
  }

  return (
    <div>
      <header className="app-header">
        <div className="shell header-content">
          <button type="button" className="brand" onClick={() => setView("shopping")}>
            <img src="/icon.svg" alt="" width={28} height={28} />
            Basket
          </button>
          <p data-testid="sync-status" className="hint num" role="status">
            {!state.online ? "Offline" : state.sending ? "Syncing…" : "Synced"}
            {pending > 0 ? ` · ${pending} pending` : ""}
          </p>
        </div>
      </header>
      <main className="shell workspace">
        {!snapshot && (
          <div className="sync-banner" role="status">
            <span>{state.online ? "Loading your lists…" : "Offline — showing what you have."}</span>
          </div>
        )}
        {snapshot && (
          <>
            <nav className="tabs" data-testid="tabs" role="tablist" aria-label="Lists">
              {VIEWS.map(entry => (
                <button
                  key={entry.id}
                  type="button"
                  role="tab"
                  aria-selected={view === entry.id}
                  data-testid={`tab-${entry.id}`}
                  onClick={() => setView(entry.id)}
                >
                  {entry.label}
                </button>
              ))}
            </nav>
            {(view === "shopping" || view === "wishlist") && (
              <>
                <QuickAdd list={view as ListName} />
                <ListTab
                  snapshot={snapshot}
                  list={view}
                  onOpen={item => setSelectedId(item.id)}
                  onBought={handleBought}
                  onRemove={handleRemove}
                  onReopen={handleReopen}
                />
              </>
            )}
            {view === "bought" && <BoughtView snapshot={snapshot} showArchived={showArchived} setShowArchived={setShowArchived} onOpen={item => setSelectedId(item.id)} />}
            {view === "settings" && <Settings snapshot={snapshot} onSignOut={() => setView("shopping")} />}
          </>
        )}
      </main>
      {selectedId && snapshot && <ItemSheet itemId={selectedId} snapshot={snapshot} onClose={() => setSelectedId(null)} />}
      {purchaseFor && <PurchaseSheet purchaseId={purchaseFor.purchaseId} itemTitle={purchaseFor.title} onClose={() => setPurchaseFor(null)} />}
      {canUndo && (
        <div className="toast">
          <span>Change saved.</span>
          <button
            type="button"
            className="button small quiet"
            data-testid="undo-button"
            onClick={() => {
              void store.enqueue({ op_id: ulid(), type: "undo" });
              setShowUndo(false);
            }}
          >
            Undo
          </button>
        </div>
      )}
      <div className="toasts" aria-live="polite">
        {state.toasts.map(toast => (
          <div key={toast.id} className="toast">
            <span>{toast.message}</span>
            <button type="button" className="button small quiet" onClick={() => store.dismissToast(toast.id)}>Dismiss</button>
          </div>
        ))}
      </div>
    </div>
  );
}

function BoughtView({
  snapshot,
  showArchived,
  setShowArchived,
  onOpen,
}: {
  snapshot: Snapshot;
  showArchived: boolean;
  setShowArchived: (value: boolean) => void;
  onOpen: (item: Item) => void;
}): React.JSX.Element {
  const bought = boughtItems(snapshot);
  const archived = archivedItems(snapshot);
  const visible = showArchived ? archived : bought;
  return (
    <section aria-label="Bought">
      <div className="actions">
        <label className="check">
          <input
            type="checkbox"
            data-testid="archived-filter"
            checked={showArchived}
            onChange={event => setShowArchived(event.target.checked)}
          />{" "}
          Archived filter
        </label>
      </div>
      {visible.length === 0 ? (
        <p className="empty">Nothing here.</p>
      ) : (
        <ul className="list-card">
          {visible.map(item => (
            <li key={item.id} className="item-row" data-testid="item-row" data-item-id={item.id}>
              <button type="button" className="item-row-main" onClick={() => onOpen(item)} aria-label={item.title}>
                <span>{item.title}</span>
                {item.last_purchase_date && <span className="hint num">{item.last_purchase_date}</span>}
              </button>
              {item.notes && <LinkifiedNotes text={item.notes} />}
              <span className="item-meta">
                {item.status === "archived" ? "Archived" : "Bought"}
                {item.due_date ? ` · Due ${item.due_date}` : ""}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
