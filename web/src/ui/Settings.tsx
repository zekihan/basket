import { useEffect, useState } from "react";
import type React from "react";
import { getStore, getMe } from "../data/index.ts";
import { ulid } from "../data/logic.ts";
import type { Me, Snapshot } from "../types.ts";

function timeZones(): string[] {
  const supported = (Intl as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf;
  if (typeof supported === "function") {
    try {
      return supported.call(Intl, "timeZone");
    } catch {
      return [];
    }
  }
  return [];
}

export function Settings({ snapshot, onSignOut }: { snapshot: Snapshot; onSignOut: () => void }): React.JSX.Element {
  const store = getStore();
  const [me, setMe] = useState<Me | null>(null);
  const [busy, setBusy] = useState(false);
  const zones = timeZones();
  const current = snapshot.time_zone;
  const options = zones.includes(current) ? zones : [current, ...zones];

  useEffect(() => {
    void getMe().then(setMe).catch(() => setMe(null));
  }, []);

  async function signOut(): Promise<void> {
    setBusy(true);
    await store.signOut();
    onSignOut();
  }

  async function exportJSON(): Promise<void> {
    setBusy(true);
    try {
      await store.exportJSON();
    } finally {
      setBusy(false);
    }
  }

  async function changeZone(zone: string): Promise<void> {
    await store.enqueue({ op_id: ulid(), type: "set_time_zone", time_zone: zone });
  }

  return (
    <section className="card settings-card" aria-label="Settings">
      <h2>Settings</h2>
      {me ? (
        <p>
          Signed in as {me.name ?? me.email ?? me.subject}
          {me.email && me.name ? ` · ${me.email}` : ""}
        </p>
      ) : (
        <p className="hint">Identity unavailable offline.</p>
      )}
      <div className="actions">
        <button type="button" className="button secondary" data-testid="export" disabled={busy} onClick={() => void exportJSON()}>
          Export JSON
        </button>
        <button type="button" className="button secondary" data-testid="sign-out" disabled={busy} onClick={() => void signOut()}>
          Sign out
        </button>
      </div>
      <label className="field" style={{ width: "100%" }}>
        Time zone
        <select value={current} onChange={event => void changeZone(event.target.value)} aria-label="Time zone">
          {options.map(zone => (
            <option key={zone} value={zone}>{zone}</option>
          ))}
        </select>
      </label>
    </section>
  );
}
