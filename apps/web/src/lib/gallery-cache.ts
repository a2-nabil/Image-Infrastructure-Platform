import type { UserImageDetail } from "@/lib/user-api";

const STORAGE_PREFIX = "a2n-gallery-cache:v1:";
/** Soft TTL — still shown, then refreshed in background. */
export const GALLERY_CACHE_TTL_MS = 5 * 60 * 1000;

/** tools = Dashboard library; gallery = personal Photos app. */
export type LibraryCacheKind = "tools" | "gallery";

export type GalleryCacheSnapshot = {
  userId: string;
  trash: boolean;
  kind: LibraryCacheKind;
  items: UserImageDetail[];
  total: number;
  nextOffset: number;
  hasMore: boolean;
  savedAt: number;
};

function storageKey(
  userId: string,
  trash: boolean,
  kind: LibraryCacheKind = "tools",
): string {
  return `${STORAGE_PREFIX}${kind}:${userId}:${trash ? "trash" : "library"}`;
}

function canUseStorage(): boolean {
  return typeof window !== "undefined";
}

export function readGalleryCache(
  userId: string,
  trash: boolean,
  kind: LibraryCacheKind = "tools",
): GalleryCacheSnapshot | null {
  if (!canUseStorage() || !userId) {
    return null;
  }
  try {
    const raw = window.sessionStorage.getItem(storageKey(userId, trash, kind));
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as GalleryCacheSnapshot;
    if (
      !parsed ||
      parsed.userId !== userId ||
      parsed.trash !== trash ||
      (parsed.kind && parsed.kind !== kind) ||
      !Array.isArray(parsed.items)
    ) {
      return null;
    }
    return { ...parsed, kind };
  } catch {
    return null;
  }
}

export function writeGalleryCache(snapshot: GalleryCacheSnapshot): void {
  if (!canUseStorage() || !snapshot.userId) {
    return;
  }
  const kind = snapshot.kind || "tools";
  try {
    window.sessionStorage.setItem(
      storageKey(snapshot.userId, snapshot.trash, kind),
      JSON.stringify({
        ...snapshot,
        kind,
        savedAt: Date.now(),
      }),
    );
  } catch {
    // quota / private mode
  }
}

export function isGalleryCacheFresh(snapshot: GalleryCacheSnapshot): boolean {
  return Date.now() - snapshot.savedAt < GALLERY_CACHE_TTL_MS;
}

/** Drop library and/or trash caches for a user (call after mutations). */
export function invalidateGalleryCache(
  userId: string,
  scope: "library" | "trash" | "all" = "all",
  kind: LibraryCacheKind = "tools",
): void {
  if (!canUseStorage() || !userId) {
    return;
  }
  try {
    if (scope === "library" || scope === "all") {
      window.sessionStorage.removeItem(storageKey(userId, false, kind));
    }
    if (scope === "trash" || scope === "all") {
      window.sessionStorage.removeItem(storageKey(userId, true, kind));
    }
  } catch {
    // ignore
  }
}

/** Wipe every gallery cache entry (call on logout / account switch). */
export function clearAllGalleryCaches(): void {
  if (!canUseStorage()) {
    return;
  }
  try {
    const keys: string[] = [];
    for (let i = 0; i < window.sessionStorage.length; i += 1) {
      const key = window.sessionStorage.key(i);
      if (key && key.startsWith(STORAGE_PREFIX)) {
        keys.push(key);
      }
    }
    for (const key of keys) {
      window.sessionStorage.removeItem(key);
    }
  } catch {
    // ignore
  }
}

/**
 * Soft revalidate first page while keeping infinitely-scrolled pages.
 * Never keep more rows than the server `total` (prevents cross-account UI leaks
 * when switching users: empty library must not retain the previous user's grid).
 */
export function mergeSoftRevalidatePage<T extends { id: string }>(
  prev: T[],
  pageItems: T[],
  total: number,
): T[] {
  const safeTotal = Math.max(0, total);
  if (safeTotal <= pageItems.length) {
    return pageItems;
  }
  if (prev.length <= pageItems.length) {
    return pageItems;
  }
  const firstIds = new Set(pageItems.map((i) => i.id));
  const rest = prev
    .slice(pageItems.length)
    .filter((i) => !firstIds.has(i.id));
  return [...pageItems, ...rest].slice(0, safeTotal);
}

/** Patch cached first-page items after local grid edits (trash/restore/purge). */
export function patchGalleryCacheItems(
  userId: string,
  trash: boolean,
  updater: (items: UserImageDetail[]) => UserImageDetail[],
  kind: LibraryCacheKind = "tools",
): void {
  const existing = readGalleryCache(userId, trash, kind);
  if (!existing) {
    return;
  }
  const nextItems = updater(existing.items);
  writeGalleryCache({
    ...existing,
    kind,
    items: nextItems,
    total: Math.max(0, existing.total - (existing.items.length - nextItems.length)),
    nextOffset: nextItems.length,
    hasMore: existing.hasMore,
    savedAt: Date.now(),
  });
}
