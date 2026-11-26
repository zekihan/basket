import type { Item, ListName, Op, Purchase, Snapshot } from "../types.ts";

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;

function encodeUlidTime(time: number): string {
  let out = "";
  let value = time;
  for (let i = 0; i < 10; i++) {
    out = CROCKFORD[value & 31] + out;
    value = Math.floor(value / 32);
  }
  return out;
}

function randomBytes(n: number): Uint8Array {
  const g = globalThis as { crypto?: Crypto };
  if (g.crypto?.getRandomValues) {
    const buf = new Uint8Array(n);
    g.crypto.getRandomValues(buf);
    return buf;
  }
  const out = new Uint8Array(n);
  for (let i = 0; i < n; i++) out[i] = Math.floor(Math.random() * 256);
  return out;
}

function encodeRandom(bytes: Uint8Array): string {
  let out = "";
  let current = 0;
  let bits = 0;
  for (const byte of bytes) {
    current = (current << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      out += CROCKFORD[(current >> bits) & 31];
    }
  }
  if (bits > 0) out += CROCKFORD[(current << (5 - bits)) & 31];
  return out.slice(0, 16);
}

function decodeRandom(value: string): number[] {
  const out: number[] = [];
  let current = 0;
  let bits = 0;
  for (const ch of value) {
    const idx = CROCKFORD.indexOf(ch);
    if (idx < 0) continue;
    current = (current << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      out.push((current >> bits) & 255);
    }
  }
  return out;
}

function incrementRandom(state: string): string {
  const bytes = decodeRandom(state);
  while (bytes.length < 10) bytes.unshift(0);
  for (let i = bytes.length - 1; i >= 0; i--) {
    bytes[i]++;
    if (bytes[i] < 256) break;
    bytes[i] = 0;
  }
  return encodeRandom(new Uint8Array(bytes));
}

let lastTimestamp = -1;
let lastRandom = "";

export function ulid(now?: number): string {
  const time = now ?? Date.now();
  let timestamp: number;
  let randomPart: string;
  if (time > lastTimestamp) {
    timestamp = time;
    randomPart = encodeRandom(randomBytes(10));
  } else {
    timestamp = lastTimestamp;
    randomPart = incrementRandom(lastRandom);
  }
  lastTimestamp = timestamp;
  lastRandom = randomPart;
  return encodeUlidTime(timestamp) + randomPart;
}

export function isUlid(value: string): boolean {
  return ULID_RE.test(value);
}

export function splitQuickAdd(input: string): { title: string; notes: string } | null {
  const trimmed = input.trim();
  if (trimmed.length === 0) return null;
  const idx = trimmed.indexOf(",");
  if (idx < 0) {
    const title = trimmed;
    if (title.length === 0) return null;
    return { title, notes: "" };
  }
  const title = trimmed.slice(0, idx).trim();
  const notes = trimmed.slice(idx + 1).trim();
  if (title.length === 0) return null;
  return { title, notes };
}

export interface TextSegment {
  text: string;
  href?: string;
}

const URL_RE = /https?:\/\/[^\s<>"']+/gi;

export function linkify(text: string): TextSegment[] {
  if (text.length === 0) return [];
  const segments: TextSegment[] = [];
  let last = 0;
  let match: RegExpExecArray | null;
  URL_RE.lastIndex = 0;
  while ((match = URL_RE.exec(text)) !== null) {
    let href = match[0];
    const cut = cutTrailingPunctuation(href);
    href = href.slice(0, href.length - cut);
    if (href.length === 0) continue;
    if (match.index > last) segments.push({ text: text.slice(last, match.index) });
    segments.push({ text: href, href });
    last = match.index + href.length;
    URL_RE.lastIndex = last;
  }
  if (last < text.length) segments.push({ text: text.slice(last) });
  return segments.length === 0 ? [{ text }] : segments;
}

function cutTrailingPunctuation(value: string): number {
  let count = 0;
  while (value.length > count) {
    const ch = value[value.length - 1 - count];
    if (ch === "." || ch === "," || ch === ";" || ch === ":" || ch === "!" || ch === "?" || ch === ")" || ch === "]") {
      count++;
    } else break;
  }
  return count;
}

export function parseTRYMinor(input: string): number | null {
  const s = input.trim().replace(/\s+/g, "");
  if (s.length === 0) return null;
  if (!/^[0-9.,]+$/.test(s)) return null;
  const hasComma = s.includes(",");
  const hasDot = s.includes(".");
  let minor: string;
  if (hasComma && hasDot) {
    if (s.lastIndexOf(",") > s.lastIndexOf(".")) {
      const normalized = s.replace(/\./g, "").replace(",", ".");
      const num = Number(normalized);
      if (!Number.isFinite(num) || num < 0) return null;
      return Math.round(num * 100);
    }
    minor = s.replace(/\./g, "");
  } else if (hasComma) {
    minor = s.replace(",", ".");
  } else {
    minor = s;
  }
  const num = Number(minor);
  if (!Number.isFinite(num) || num < 0) return null;
  return Math.round(num * 100);
}

export function formatTRYMinor(minor: number | null): string | null {
  if (minor === null) return null;
  const sign = minor < 0 ? "-" : "";
  const abs = Math.abs(minor);
  const whole = Math.floor(abs / 100);
  const frac = abs % 100;
  const grouped = String(whole).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  return `${sign}${grouped},${String(frac).padStart(2, "0")}`;
}

function sortItemsByCreated(items: Item[]): Item[] {
  return [...items].sort((a, b) => {
    if (a.created_at !== b.created_at) return b.created_at < a.created_at ? -1 : 1;
    return b.id < a.id ? -1 : 1;
  });
}

export function openItems(snapshot: Snapshot, list: ListName): Item[] {
  return sortItemsByCreated(snapshot.items.filter(item => item.status === "open" && item.list === list));
}

export function dueAgainItems(snapshot: Snapshot): Item[] {
  return sortItemsByCreated(snapshot.items.filter(item => item.due === true));
}

export function boughtItems(snapshot: Snapshot): Item[] {
  return [...snapshot.items]
    .filter(item => item.status === "bought")
    .sort((a, b) => {
      const da = a.last_purchase_date ?? "";
      const db = b.last_purchase_date ?? "";
      if (da !== db) return db < da ? -1 : 1;
      return sortItemsByCreated([a, b])[0] === a ? -1 : 1;
    });
}

export function archivedItems(snapshot: Snapshot): Item[] {
  return sortItemsByCreated(snapshot.items.filter(item => item.status === "archived"));
}

export function itemsForList(snapshot: Snapshot, list: ListName): Item[] {
  return openItems(snapshot, list);
}

function nowIso(): string {
  return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

function baseItem(id: string, list: ListName, title: string, partial?: Partial<Item>): Item {
  const now = nowIso();
  return {
    id,
    list,
    title,
    quantity: 1,
    notes: "",
    target_price: null,
    target_price_minor: null,
    recurring_days: null,
    status: "open",
    created_at: now,
    updated_at: now,
    version: 1,
    last_purchase_date: null,
    due_date: null,
    due: false,
    ...partial,
  };
}

function cloneSnapshot(snapshot: Snapshot): Snapshot {
  return {
    ...snapshot,
    items: snapshot.items.map(item => ({ ...item })),
    purchases: snapshot.purchases.map(p => ({ ...p })),
  };
}

function updateRecurrence(item: Item, today: string): void {
  const { last_purchase_date: last, recurring_days: days } = item;
  item.due_date = last && days !== null && Number.isInteger(days) && days >= 1 && days <= 730
    ? addDays(last, days)
    : null;
  item.due = item.status === "bought" && item.due_date !== null && item.due_date <= today;
}

function updatePurchaseHistory(item: Item, snapshot: Snapshot): void {
  let latest = "";
  for (const purchase of snapshot.purchases) {
    if (purchase.item_id === item.id && purchase.date > latest) latest = purchase.date;
  }
  item.last_purchase_date = latest || null;
  updateRecurrence(item, snapshot.today);
}

export function applyOp(snapshot: Snapshot, op: Op): Snapshot {
  const next = cloneSnapshot(snapshot);
  switch (op.type) {
    case "add_item": {
      if (next.items.some(item => item.id === op.id)) return next;
      const fields = op.fields;
      const targetPrice = fields.target_price ?? null;
      const targetMinor = targetPrice === null ? null : parseTRYMinor(targetPrice);
      const item = baseItem(op.id, fields.list ?? "shopping", fields.title, {
        quantity: fields.quantity ?? 1,
        notes: fields.notes ?? "",
        target_price: targetPrice,
        target_price_minor: targetMinor,
        recurring_days: fields.recurring_days ?? null,
      });
      next.items = sortItemsByCreated([item, ...next.items]);
      return next;
    }
    case "update_item": {
      const item = next.items.find(it => it.id === op.id);
      if (!item) return next;
      const fields = op.fields;
      if (fields.list !== undefined) item.list = fields.list;
      if (fields.title !== undefined) item.title = fields.title;
      if (fields.quantity !== undefined) item.quantity = fields.quantity;
      if (fields.notes !== undefined) item.notes = fields.notes;
      if (fields.target_price !== undefined) {
        item.target_price = fields.target_price;
        item.target_price_minor = fields.target_price === null ? null : parseTRYMinor(fields.target_price);
      }
      if (fields.recurring_days !== undefined) item.recurring_days = fields.recurring_days;
      if (item.list === "wishlist") item.recurring_days = null;
      updateRecurrence(item, next.today);
      item.version += 1;
      item.updated_at = nowIso();
      return next;
    }
    case "remove_item": {
      next.items = next.items.filter(it => it.id !== op.id);
      next.purchases = next.purchases.filter(p => p.item_id !== op.id);
      return next;
    }
    case "mark_bought": {
      const item = next.items.find(it => it.id === op.id);
      if (!item) return next;
      item.status = "bought";
      item.version += 1;
      item.updated_at = nowIso();
      const date = op.date ?? next.today;
      const price = op.price ?? null;
      const purchase: Purchase = {
        id: op.purchase_id,
        item_id: op.id,
        date,
        price,
        price_minor: price === null ? null : parseTRYMinor(price),
        quantity: op.quantity ?? item.quantity,
        store: op.store ?? null,
        created_at: nowIso(),
      };
      next.purchases = [purchase, ...next.purchases];
      updatePurchaseHistory(item, next);
      return next;
    }
    case "add_purchase_details": {
      const purchase = next.purchases.find(p => p.id === op.purchase_id);
      if (!purchase) return next;
      if (op.price !== undefined) {
        purchase.price = op.price;
        purchase.price_minor = op.price === null ? null : parseTRYMinor(op.price);
      }
      if (op.store !== undefined) purchase.store = op.store;
      return next;
    }
    case "reopen_item": {
      const item = next.items.find(it => it.id === op.id);
      if (!item) return next;
      item.status = "open";
      updateRecurrence(item, next.today);
      item.version += 1;
      item.updated_at = nowIso();
      return next;
    }
    case "archive_item": {
      const item = next.items.find(it => it.id === op.id);
      if (!item) return next;
      item.status = "archived";
      updateRecurrence(item, next.today);
      item.version += 1;
      item.updated_at = nowIso();
      return next;
    }
    case "delete_purchase": {
      const purchase = next.purchases.find(p => p.id === op.purchase_id);
      if (!purchase) return next;
      next.purchases = next.purchases.filter(p => p.id !== op.purchase_id);
      const item = next.items.find(it => it.id === purchase.item_id);
      if (item) updatePurchaseHistory(item, next);
      return next;
    }
    case "undo": {
      return next;
    }
    case "set_time_zone": {
      next.time_zone = op.time_zone;
      return next;
    }
  }
}

function addDays(date: string, days: number): string {
  let [year, month, day] = date.split("-").map(Number);
  day += days;
  while (true) {
    const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
    const monthLength = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1];
    if (!(day > monthLength)) break;
    day -= monthLength;
    month++;
    if (month === 13) {
      year++;
      month = 1;
    }
  }
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

export function batchOps(ops: Op[], max = 200): Op[][] {
  const batches: Op[][] = [];
  for (let i = 0; i < ops.length; i += max) batches.push(ops.slice(i, i + max));
  return batches;
}

export function filterAckedOps(queued: Op[], ackedIds: string[]): Op[] {
  const acked = new Set(ackedIds);
  return queued.filter(op => !acked.has(op.op_id));
}

export function rebaseSnapshot(serverSnapshot: Snapshot, remaining: Op[]): Snapshot {
  let current = serverSnapshot;
  for (const op of remaining) {
    current = applyOp(current, op);
    if (op.type !== "undo" && op.type !== "set_time_zone") current.can_undo = true;
  }
  return current;
}

export function emptySnapshot(): Snapshot {
  return { today: "", time_zone: "Europe/Istanbul", items: [], purchases: [], can_undo: false };
}
