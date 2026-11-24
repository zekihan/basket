import type { Item, Me, Op, OpsResponse, Snapshot } from "../types.ts";

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function handle(response: Response): Promise<Response> {
  if (response.status === 401) throw new ApiError(401, "signed out");
  if (!response.ok) {
    let message = `request failed: ${response.status}`;
    try {
      const body = (await response.json()) as { error?: { message?: string } };
      if (body.error?.message) message = body.error.message;
    } catch {
      // keep default
    }
    throw new ApiError(response.status, message);
  }
  return response;
}

export async function postOps(ops: Op[], signal?: AbortSignal): Promise<OpsResponse> {
  const response = await handle(await fetch("/api/ops", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ops }),
    credentials: "same-origin",
    signal,
  }));
  return (await response.json()) as OpsResponse;
}

export async function getSnapshot(signal?: AbortSignal): Promise<Snapshot> {
  const response = await handle(await fetch("/api/snapshot", { credentials: "same-origin", signal }));
  return (await response.json()) as Snapshot;
}

export async function getSimilar(title: string, signal?: AbortSignal): Promise<Item[]> {
  const url = `/api/similar?title=${encodeURIComponent(title)}`;
  const response = await fetch(url, { credentials: "same-origin", signal });
  if (response.status === 401) throw new ApiError(401, "signed out");
  if (!response.ok) throw new ApiError(response.status, "similar failed");
  const body = (await response.json()) as { items?: Item[] };
  return body.items ?? [];
}

export async function getMe(signal?: AbortSignal): Promise<Me> {
  const response = await handle(await fetch("/auth/me", { credentials: "same-origin", signal }));
  return (await response.json()) as Me;
}

export async function postLogout(): Promise<void> {
  await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
}

export async function downloadExport(): Promise<void> {
  const response = await handle(await fetch("/api/export", { credentials: "same-origin" }));
  const blob = await response.blob();
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const match = /filename="?([^";]+)"?/.exec(disposition);
  const fallback = `basket-${new Date().toISOString().slice(0, 10)}.json`;
  const filename = match ? match[1] : fallback;
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
