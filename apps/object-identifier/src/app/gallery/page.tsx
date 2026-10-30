"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type TouchEvent as ReactTouchEvent,
} from "react";
import Link from "next/link";
import { GoogleLogin } from "@react-oauth/google";
import { useRouter } from "next/navigation";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  Download,
  ExternalLink,
  FolderPlus,
  ImageIcon,
  Info,
  Layers,
  MapPin,
  MoreVertical,
  RotateCcw,
  Settings,
  Sparkles,
  Trash2,
  Upload,
  Video,
  X,
} from "lucide-react";
import { useAuth } from "@/components/AuthProvider";
import { usePhotosAlert } from "@/components/PhotosAlert";
import PhotosSheet from "@/components/PhotosSheet";
import ResultDisplay from "@/components/ResultDisplay";
import {
  GALLERY_SUPER_ADMIN_EMAIL,
  userHasGalleryAccess,
} from "@/lib/auth";
import {
  addAlbumItems,
  allowGalleryEmail,
  createAlbum,
  deleteAlbum,
  GALLERY_ACCEPT,
  listAlbums,
  listGalleryAccess,
  listGalleryMedia,
  revokeGalleryEmail,
  type Album,
  type GalleryAccessEntry,
} from "@/lib/gallery-api";
import {
  invalidateGalleryCache,
  patchGalleryCacheItems,
  mergeSoftRevalidatePage,
  readGalleryCache,
  writeGalleryCache,
} from "@/lib/gallery-cache";
import {
  filterGalleryUploadFiles,
  galleryUploadQueue,
  type LibraryRefreshDetail,
} from "@/lib/gallery-upload-queue";
import { formatBytes, getGuestSessionID } from "@/lib/image-api";
import {
  mediaDisplayLabel,
  mediaFileName,
  type MediaNameInput,
} from "@/lib/media-names";
import {
  claimGuestMedia,
  deleteMyVariant,
  purgeMyImages,
  restoreMyImages,
  sameOriginProxyURL,
  trashMyImage,
  trashMyImages,
  type UserImageDetail,
} from "@/lib/user-api";

const PAGE_SIZE = 36;
const CACHE_KIND = "gallery" as const;
const PRESET_ORDER = ["original", "large", "medium", "small", "thumbnail", "custom"];

type ContextMenuState = {
  x: number;
  y: number;
  index: number;
};

type DisplayOption = {
  key: string;
  label: string;
  downloadName: string;
  url: string;
  width: number;
  height: number;
  sizeBytes: number;
  mime: string;
  kind: "source" | "webp";
  /** WebP preset name when kind is webp */
  preset?: string;
};

function nameInputFor(
  item: UserImageDetail,
  kind: "source" | "webp",
  width: number,
  height: number,
): MediaNameInput {
  return {
    originalFilename: item.original_filename,
    kind,
    width,
    height,
    sourceWidth: item.width,
    sourceHeight: item.height,
  };
}

function statusTone(status: string): string {
  switch (status) {
    case "ready":
      return "bg-emerald-50 text-emerald-800 border-emerald-200";
    case "processing":
      return "bg-sky-50 text-sky-800 border-sky-200";
    case "failed":
      return "bg-rose-50 text-rose-800 border-rose-200";
    default:
      return "bg-slate-50 text-slate-700 border-slate-200";
  }
}

function isVideoItem(item: UserImageDetail): boolean {
  return (
    item.media_kind === "video" ||
    (item.mime_type || "").startsWith("video/")
  );
}

function thumbURL(item: UserImageDetail): string {
  if (isVideoItem(item)) {
    return sameOriginProxyURL(item.original_url);
  }
  const thumb =
    item.variants.find((v) => v.preset_name === "thumbnail") ||
    item.variants.find((v) => v.preset_name === "small") ||
    item.variants[0];
  return sameOriginProxyURL(thumb?.url || item.original_url);
}

function buildDisplayOptions(item: UserImageDetail): DisplayOption[] {
  const sourceName = nameInputFor(item, "source", item.width, item.height);
  const source: DisplayOption = {
    key: "source",
    label: mediaDisplayLabel("source"),
    downloadName: mediaFileName(sourceName),
    url: sameOriginProxyURL(item.original_url),
    width: item.width,
    height: item.height,
    sizeBytes: item.file_size_bytes,
    mime: item.mime_type || item.format || "image",
    kind: "source",
  };

  const sorted = [...item.variants].sort((a, b) => {
    const ai = PRESET_ORDER.indexOf(a.preset_name);
    const bi = PRESET_ORDER.indexOf(b.preset_name);
    const aRank = ai === -1 ? 99 : ai;
    const bRank = bi === -1 ? 99 : bi;
    return aRank - bRank;
  });

  const variants: DisplayOption[] = sorted.map((v) => {
    const named = nameInputFor(item, "webp", v.width, v.height);
    return {
      key: `webp:${v.preset_name}`,
      label: mediaDisplayLabel("webp", v.preset_name),
      downloadName: mediaFileName(named),
      url: sameOriginProxyURL(v.url),
      width: v.width,
      height: v.height,
      sizeBytes: v.size_bytes,
      mime: v.mime_type || "image/webp",
      kind: "webp" as const,
      preset: v.preset_name,
    };
  });

  return [source, ...variants];
}

function defaultDisplayKey(item: UserImageDetail): string {
  const options = buildDisplayOptions(item);
  const preferred =
    options.find((o) => o.key === "webp:original") ||
    options.find((o) => o.key === "webp:medium") ||
    options.find((o) => o.kind === "webp") ||
    options[0];
  return preferred?.key ?? "source";
}

function resolveDisplayOption(
  item: UserImageDetail,
  key: string,
): DisplayOption {
  const options = buildDisplayOptions(item);
  return options.find((o) => o.key === key) ?? options[0];
}

function proxyAbsolute(url: string): string {
  const value = sameOriginProxyURL(url);
  if (typeof window === "undefined") {
    return value;
  }
  return new URL(value, window.location.origin).toString();
}

function downloadProxyURL(url: string): string {
  const absolute = proxyAbsolute(url);
  const parsed = new URL(absolute);
  parsed.searchParams.set("download", "1");
  return parsed.toString();
}

function totalWebpBytes(item: UserImageDetail): number {
  return item.variants.reduce((sum, v) => sum + (v.size_bytes || 0), 0);
}

function copyText(value: string) {
  void navigator.clipboard?.writeText(value);
}

function DetailRow({
  label,
  value,
  copyable,
  light,
}: {
  label: string;
  value: string;
  copyable?: boolean;
  light?: boolean;
}) {
  return (
    <div
      className={`grid grid-cols-[7.5rem_1fr] gap-2 border-b pb-2.5 text-sm ${
        light ? "border-white/10" : "border-slate-100 dark:border-slate-800"
      }`}
    >
      <dt
        className={
          light ? "text-white/70" : "text-slate-600 dark:text-slate-300"
        }
      >
        {label}
      </dt>
      <dd
        className={`break-all font-medium ${light ? "text-white" : "text-slate-900 dark:text-slate-100"}`}
      >
        <span className="inline-flex max-w-full items-start gap-2">
          <span className="min-w-0">{value}</span>
          {copyable && value !== "—" && (
            <button
              type="button"
              className={`shrink-0 cursor-pointer ${light ? "text-white/40 hover:text-white" : "text-slate-400 hover:text-slate-700"}`}
              onClick={() => copyText(value)}
              aria-label={`Copy ${label}`}
            >
              <Copy className="h-3.5 w-3.5" />
            </button>
          )}
        </span>
      </dd>
    </div>
  );
}

function InfoContent({
  item,
  light = false,
  displayKey,
  onSelectDisplay,
  onDeleteOption,
  deletingKey,
}: {
  item: UserImageDetail;
  light?: boolean;
  displayKey: string;
  onSelectDisplay: (key: string) => void;
  onDeleteOption?: (opt: DisplayOption) => void;
  deletingKey?: string | null;
}) {
  const options = buildDisplayOptions(item);
  const active = resolveDisplayOption(item, displayKey);
  const webpTotal = totalWebpBytes(item);

  return (
    <div className="space-y-5">
      <div>
        <p
          className={`text-xs font-semibold uppercase tracking-[0.14em] ${light ? "text-sky-300" : "text-sky-700 dark:text-sky-300"}`}
        >
          Info
        </p>
        <h2
          className={`mt-1 text-xl font-semibold tracking-tight ${light ? "text-white" : "text-slate-900 dark:text-white"}`}
        >
          {item.original_filename}
        </h2>
        <span
          className={`mt-2 inline-flex rounded-full border px-2.5 py-0.5 text-[11px] font-semibold ${statusTone(item.status)}`}
        >
          {item.status}
        </span>
      </div>

      <div>
        <p
          className={`mb-2 text-sm font-semibold ${light ? "text-white" : "text-slate-900 dark:text-white"}`}
        >
          Display &amp; download
        </p>
        <ul className="space-y-2">
          {options.map((opt) => {
            const selected = opt.key === active.key;
            const deleting = deletingKey === opt.key;
            return (
              <li
                key={opt.key}
                className={`rounded-xl border px-3 py-2.5 ${
                  selected
                    ? light
                      ? "border-sky-400/60 bg-sky-500/15"
                      : "border-sky-300 bg-sky-50 dark:border-sky-500/50 dark:bg-sky-950/40"
                    : light
                      ? "border-white/10 bg-white/5"
                      : "border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900"
                }`}
              >
                <div className="flex items-start justify-between gap-2">
                  <button
                    type="button"
                    onClick={() => onSelectDisplay(opt.key)}
                    className={`min-w-0 flex-1 cursor-pointer text-left ${light ? "text-white" : "text-slate-900 dark:text-white"}`}
                  >
                    <p className="truncate text-sm font-semibold">
                      {opt.label}
                      {selected ? " · viewing" : ""}
                      {opt.kind === "source" ? " · original" : ""}
                    </p>
                    <p
                      className={`mt-0.5 text-xs ${light ? "text-white/70" : "text-slate-600 dark:text-slate-300"}`}
                    >
                      {opt.width}×{opt.height} · {formatBytes(opt.sizeBytes)} ·{" "}
                      {opt.mime}
                    </p>
                  </button>
                  {selected ? (
                    <Check
                      className={`mt-0.5 h-4 w-4 shrink-0 ${light ? "text-sky-300" : "text-sky-600 dark:text-sky-300"}`}
                    />
                  ) : null}
                </div>
                <div className="mt-2 flex flex-wrap gap-2">
                  <button
                    type="button"
                    onClick={() => onSelectDisplay(opt.key)}
                    className={`inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold ${
                      light
                        ? "bg-white/10 text-white hover:bg-white/15"
                        : "bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
                    }`}
                  >
                    <Layers className="h-3 w-3" />
                    View
                  </button>
                  <a
                    href={downloadProxyURL(opt.url)}
                    download={opt.downloadName}
                    className={`inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold ${
                      light
                        ? "bg-white text-slate-900"
                        : "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                    }`}
                  >
                    <Download className="h-3 w-3" />
                    Download
                  </a>
                  <a
                    href={sameOriginProxyURL(opt.url)}
                    target="_blank"
                    rel="noreferrer"
                    className={`inline-flex cursor-pointer items-center gap-1 rounded-full border px-2.5 py-1 text-[11px] font-semibold ${
                      light
                        ? "border-white/25 text-white"
                        : "border-slate-200 text-slate-700 dark:border-slate-600 dark:text-slate-200"
                    }`}
                  >
                    <ExternalLink className="h-3 w-3" />
                    Open
                  </a>
                  {onDeleteOption ? (
                    <button
                      type="button"
                      disabled={deleting}
                      onClick={() => onDeleteOption(opt)}
                      className={`inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold disabled:cursor-not-allowed disabled:opacity-60 ${
                        light
                          ? "bg-rose-500/20 text-rose-200 hover:bg-rose-500/30"
                          : "bg-rose-50 text-rose-700 hover:bg-rose-100 dark:bg-rose-950/50 dark:text-rose-300 dark:hover:bg-rose-900/50"
                      }`}
                    >
                      <Trash2 className="h-3 w-3" />
                      {deleting
                        ? "Working…"
                        : opt.kind === "source"
                          ? "Move to trash"
                          : "Delete forever"}
                    </button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
        <p
          className={`mt-2 text-xs ${light ? "text-white/70" : "text-slate-600 dark:text-slate-300"}`}
        >
          Source {formatBytes(item.file_size_bytes)}
          {item.variants.length
            ? ` · outputs ${formatBytes(webpTotal)}`
            : ""}
          {" · "}
          Move to trash applies to the whole photo (original + all variants).
          Variant “Delete forever” removes only that WebP from DB and S3.
        </p>
      </div>

      <dl className="space-y-2.5">
        <DetailRow label="Image ID" value={item.id} copyable light={light} />
        <DetailRow
          label="Fingerprint"
          value={item.fingerprint}
          copyable
          light={light}
        />
        <DetailRow label="MIME" value={item.mime_type || "—"} light={light} />
        <DetailRow label="Format" value={item.format || "—"} light={light} />
        <DetailRow
          label="Dimensions"
          value={`${item.width} × ${item.height}`}
          light={light}
        />
        <DetailRow
          label="Created"
          value={new Date(item.created_at).toLocaleString()}
          light={light}
        />
        <DetailRow
          label="Updated"
          value={new Date(item.updated_at).toLocaleString()}
          light={light}
        />
        <DetailRow
          label="Expires"
          value={
            item.expires_at
              ? new Date(item.expires_at).toLocaleString()
              : "Never"
          }
          light={light}
        />
        <DetailRow
          label="Proxy URL"
          value={proxyAbsolute(active.url)}
          copyable
          light={light}
        />
      </dl>

      {item.location ? (
        <div
          className={`rounded-2xl border p-3 ${light ? "border-white/10 bg-white/5" : "border-slate-200 bg-slate-50"}`}
        >
          <p
            className={`inline-flex items-center gap-2 text-sm font-semibold ${light ? "text-white" : "text-slate-800"}`}
          >
            <MapPin className="h-4 w-4 text-sky-400" />
            Location
          </p>
          <dl className="mt-2 space-y-1.5 text-sm">
            <div className="flex justify-between gap-3">
              <dt className={light ? "text-white/55" : "text-slate-500"}>
                Lat
              </dt>
              <dd className={light ? "text-white/90" : "font-medium"}>
                {item.location.latitude}
              </dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className={light ? "text-white/55" : "text-slate-500"}>
                Lng
              </dt>
              <dd className={light ? "text-white/90" : "font-medium"}>
                {item.location.longitude}
              </dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className={light ? "text-white/55" : "text-slate-500"}>
                Source
              </dt>
              <dd className={light ? "text-white/90" : "font-medium"}>
                {item.location.source}
              </dd>
            </div>
          </dl>
        </div>
      ) : null}

      {item.identification ? (
        <div className={light ? "rounded-2xl bg-white p-1" : ""}>
          <ResultDisplay
            result={{
              object_name: item.identification.object_name,
              confidence_score: item.identification.confidence_score,
              summary: item.identification.summary,
              key_details: item.identification.key_details || [],
              suggested_actions: item.identification.suggested_actions || [],
              cache_hit: item.identification.cache_source !== "live",
              cache_source: item.identification.cache_source,
            }}
          />
        </div>
      ) : (
        <p className={`text-sm ${light ? "text-white/50" : "text-slate-500"}`}>
          No AI identification stored yet.
        </p>
      )}
    </div>
  );
}

export default function GalleryPage() {
  const router = useRouter();
  const { user, loading, signInWithGoogleCredential, claimedCount } = useAuth();
  const { confirm } = usePhotosAlert();
  const googleConfigured = Boolean(process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID);
  const galleryAllowed = userHasGalleryAccess(user);
  const hiddenGoogleRef = useRef<HTMLDivElement>(null);
  const autoPrompted = useRef(false);
  const claimTried = useRef(false);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const touchStartX = useRef<number | null>(null);

  const [items, setItems] = useState<UserImageDetail[]>([]);
  const [total, setTotal] = useState(0);
  const [nextOffset, setNextOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [signInBusy, setSignInBusy] = useState(false);
  const [menu, setMenu] = useState<ContextMenuState | null>(null);
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);
  const [showInfo, setShowInfo] = useState(false);
  const [showQuality, setShowQuality] = useState(false);
  const [showDownloadMenu, setShowDownloadMenu] = useState(false);
  /** lg+ uses the viewer side panel; PhotosSheet portals to body so CSS hide won't work. */
  const [infoAsSheet, setInfoAsSheet] = useState(false);
  const [displayKeyByImage, setDisplayKeyByImage] = useState<
    Record<string, string>
  >({});
  const [copied, setCopied] = useState(false);
  const [deletingKey, setDeletingKey] = useState<string | null>(null);
  /** Google Photos-style top sections */
  const [gallerySection, setGallerySection] = useState<
    "photos" | "albums" | "trash"
  >("photos");
  const libraryView = gallerySection === "trash" ? "trash" : "photos";
  const [albums, setAlbums] = useState<Album[]>([]);
  const [activeAlbumId, setActiveAlbumId] = useState<string | null>(null);
  const [mediaFilter, setMediaFilter] = useState<"" | "image" | "video">("");
  const [showCreateAlbum, setShowCreateAlbum] = useState(false);
  const [showAddToAlbum, setShowAddToAlbum] = useState(false);
  const [showAccessSettings, setShowAccessSettings] = useState(false);
  const [accessEntries, setAccessEntries] = useState<GalleryAccessEntry[]>([]);
  const [accessEmail, setAccessEmail] = useState("");
  const [accessBusy, setAccessBusy] = useState(false);
  const [albumTitle, setAlbumTitle] = useState("");
  const [uploading, setUploading] = useState(
    () => galleryUploadQueue.snapshot().uploading,
  );
  const [fileDragOver, setFileDragOver] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const actionsMenuRef = useRef<HTMLDivElement>(null);
  const uploadInputRef = useRef<HTMLInputElement>(null);
  const fileDragDepth = useRef(0);
  const gallerySectionRef = useRef(gallerySection);
  const activeAlbumIdRef = useRef(activeAlbumId);
  const mediaFilterRef = useRef(mediaFilter);
  const applyLibraryRefreshRef = useRef<
    ((payload: LibraryRefreshDetail) => void) | null
  >(null);
  const lastRefreshGenerationRef = useRef(0);
  const [selectMode, setSelectMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [actionBusy, setActionBusy] = useState(false);
  const [marquee, setMarquee] = useState<{
    x: number;
    y: number;
    w: number;
    h: number;
  } | null>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    originSelected: Set<string>;
    additive: boolean;
    active: boolean;
    moved: boolean;
    photoId: string | null;
  } | null>(null);
  const suppressClickRef = useRef(false);

  const viewerItem = useMemo(
    () => (viewerIndex !== null ? items[viewerIndex] ?? null : null),
    [items, viewerIndex],
  );

  const displayKey = viewerItem
    ? displayKeyByImage[viewerItem.id] ?? defaultDisplayKey(viewerItem)
    : "original";

  const setDisplayKey = useCallback(
    (key: string) => {
      if (!viewerItem) {
        return;
      }
      setDisplayKeyByImage((prev) => ({ ...prev, [viewerItem.id]: key }));
    },
    [viewerItem],
  );

  const displayOptions = useMemo(
    () => (viewerItem ? buildDisplayOptions(viewerItem) : []),
    [viewerItem],
  );

  const activeDisplay = useMemo(
    () =>
      viewerItem ? resolveDisplayOption(viewerItem, displayKey) : null,
    [viewerItem, displayKey],
  );

  const loadPage = useCallback(
    async (offset: number, replace: boolean, opts?: { soft?: boolean }) => {
      if (!user) {
        return;
      }
      const trash = libraryView === "trash";
      if (replace) {
        // Soft refresh keeps current grid visible while revalidating.
        if (!opts?.soft) {
          setBusy(true);
        }
      } else {
        setLoadingMore(true);
      }
      setError(null);
      try {
        const payload = await listGalleryMedia({
          limit: PAGE_SIZE,
          offset,
          trash,
          albumId: trash ? null : activeAlbumId,
          mediaKind: trash ? "" : mediaFilter,
        });
        setItems((prev) => {
          let next: UserImageDetail[];
          if (replace) {
            next =
              opts?.soft && offset === 0
                ? mergeSoftRevalidatePage(
                    prev,
                    payload.items,
                    payload.pagination.total,
                  )
                : payload.items;
          } else {
            next = [...prev, ...payload.items];
          }
          // Only cache unfiltered library/trash first pages.
          if (!activeAlbumId && !mediaFilter) {
            writeGalleryCache({
              userId: user.id,
              trash,
              kind: CACHE_KIND,
              items: next,
              total: payload.pagination.total,
              nextOffset:
                replace && opts?.soft
                  ? Math.min(
                      Math.max(prev.length, payload.pagination.next_offset),
                      payload.pagination.total,
                    )
                  : payload.pagination.next_offset,
              hasMore: payload.pagination.has_more,
              savedAt: Date.now(),
            });
          }
          return next;
        });
        setTotal(payload.pagination.total);
        if (!(replace && opts?.soft)) {
          setNextOffset(payload.pagination.next_offset);
          setHasMore(payload.pagination.has_more);
        } else {
          setNextOffset((prev) =>
            Math.min(
              Math.max(prev, payload.pagination.next_offset),
              payload.pagination.total,
            ),
          );
          setHasMore(payload.pagination.has_more);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load gallery");
      } finally {
        setBusy(false);
        setLoadingMore(false);
      }
    },
    [user, libraryView, activeAlbumId, mediaFilter],
  );

  useEffect(() => {
    if (loading) {
      return;
    }
    if (!user) {
      return;
    }
    if (!galleryAllowed) {
      router.replace("/dashboard");
    }
  }, [loading, user, galleryAllowed, router]);

  useEffect(() => {
    if (!user || !galleryAllowed) {
      claimTried.current = false;
      const clear = window.setTimeout(() => {
        setItems([]);
        setTotal(0);
        setNextOffset(0);
        setHasMore(false);
      }, 0);
      return () => window.clearTimeout(clear);
    }
    if (gallerySection === "albums") {
      return;
    }
    let alive = true;
    const trash = libraryView === "trash";
    // Don't blank the grid while uploads are running on Photos — prevents
    // "missing file until manual refresh" races with the upload queue.
    const uploadingNow = galleryUploadQueue.snapshot().uploading;
    let reset: number | undefined;
    if (!(gallerySection === "photos" && uploadingNow && !trash)) {
      reset = window.setTimeout(() => {
        if (!alive) return;
        setItems([]);
        setTotal(0);
        setNextOffset(0);
        setHasMore(false);
      }, 0);
    }
    void (async () => {
      if (!claimTried.current) {
        claimTried.current = true;
        const guest = getGuestSessionID();
        if (guest) {
          try {
            const claim = await claimGuestMedia(guest);
            if ((claim.claimed_count ?? 0) > 0 || (claim.merged_count ?? 0) > 0) {
              invalidateGalleryCache(user.id, "all", CACHE_KIND);
            }
          } catch {
            // best-effort
          }
        }
      }
      if (!alive) {
        return;
      }
      setSelectedIds(new Set());
      setSelectMode(false);
      setViewerIndex(null);

      const canUseCache = !activeAlbumId && !mediaFilter;
      const cached = canUseCache
        ? readGalleryCache(user.id, trash, CACHE_KIND)
        : null;
      if (cached && cached.items.length > 0 && cached.userId === user.id) {
        setItems(cached.items);
        setTotal(cached.total);
        setNextOffset(cached.nextOffset);
        setHasMore(cached.hasMore);
        setBusy(false);
        // Stale-while-revalidate first page.
        await loadPage(0, true, { soft: true });
      } else {
        await loadPage(0, true);
      }
    })();
    return () => {
      alive = false;
      if (reset !== undefined) {
        window.clearTimeout(reset);
      }
    };
  }, [
    user,
    galleryAllowed,
    loadPage,
    libraryView,
    activeAlbumId,
    mediaFilter,
    gallerySection,
  ]);

  const refreshAlbums = useCallback(async () => {
    if (!user || !galleryAllowed) {
      setAlbums([]);
      return;
    }
    try {
      setAlbums(await listAlbums());
    } catch {
      // keep prior albums on transient failure
    }
  }, [user, galleryAllowed]);

  useEffect(() => {
    const t = window.setTimeout(() => {
      void refreshAlbums();
    }, 0);
    return () => window.clearTimeout(t);
  }, [refreshAlbums]);

  // Re-fetch albums whenever the Albums tab is opened (counts / covers).
  useEffect(() => {
    if (!user || !galleryAllowed || gallerySection !== "albums") {
      return;
    }
    const t = window.setTimeout(() => {
      void refreshAlbums();
    }, 0);
    return () => window.clearTimeout(t);
  }, [user, galleryAllowed, gallerySection, refreshAlbums]);

  useEffect(() => {
    if (!actionsOpen) {
      return;
    }
    const onPointerDown = (e: MouseEvent | PointerEvent) => {
      const root = actionsMenuRef.current;
      if (root && !root.contains(e.target as Node)) {
        setActionsOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setActionsOpen(false);
      }
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [actionsOpen]);

  useEffect(() => {
    if (!user || !hasMore || busy || loadingMore) {
      return;
    }
    const node = sentinelRef.current;
    if (!node) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          void loadPage(nextOffset, false);
        }
      },
      { rootMargin: "320px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [user, hasMore, busy, loadingMore, nextOffset, loadPage]);

  useEffect(() => {
    if (loading || user || !googleConfigured || autoPrompted.current) {
      return;
    }
    autoPrompted.current = true;
    const timer = window.setTimeout(() => {
      const btn = hiddenGoogleRef.current?.querySelector(
        'div[role="button"]',
      ) as HTMLElement | null;
      btn?.click();
    }, 400);
    return () => window.clearTimeout(timer);
  }, [loading, user, googleConfigured]);

  useEffect(() => {
    const close = () => setMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("scroll", close, true);
    };
  }, []);

  useEffect(() => {
    const mq = window.matchMedia("(max-width: 1023px)");
    const sync = () => setInfoAsSheet(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);

  const closeViewer = useCallback(() => {
    setViewerIndex(null);
    setShowInfo(false);
    setShowQuality(false);
    setShowDownloadMenu(false);
  }, []);

  const removeItemsFromGrid = useCallback(
    (ids: string[]) => {
      const idSet = new Set(ids);
      setItems((prev) => {
        const next = prev.filter((i) => !idSet.has(i.id));
        setViewerIndex((idx) => {
          if (idx === null) {
            return idx;
          }
          if (next.length === 0) {
            return null;
          }
          return Math.min(idx, next.length - 1);
        });
        return next;
      });
      setTotal((t) => Math.max(0, t - ids.length));
      setSelectedIds((prev) => {
        const next = new Set(prev);
        for (const id of ids) {
          next.delete(id);
        }
        return next;
      });
      if (user) {
        // Trash/restore moves between views — drop both caches.
        invalidateGalleryCache(user.id, "all", CACHE_KIND);
      }
    },
    [user],
  );

  const handleDeleteOption = useCallback(
    async (image: UserImageDetail, opt: DisplayOption) => {
      const isSource = opt.kind === "source";
      const ok = await confirm({
        title: isSource ? "Move to Trash?" : "Delete forever?",
        message: isSource ? (
          <>
            Move <span className="font-semibold">{image.original_filename}</span>{" "}
            and all of its WebP variants to Trash? You can restore it later.
          </>
        ) : (
          <>
            Permanently delete the{" "}
            <span className="font-semibold">{opt.label}</span> variant for{" "}
            <span className="font-semibold">{image.original_filename}</span> from
            storage? This cannot be undone.
          </>
        ),
        confirmLabel: isSource ? "Move to Trash" : "Delete forever",
        tone: "danger",
        dark: true,
      });
      if (!ok) {
        return;
      }

      setDeletingKey(opt.key);
      setError(null);
      try {
        if (opt.kind === "source") {
          await trashMyImage(image.id);
          removeItemsFromGrid([image.id]);
          setShowInfo(false);
          setShowQuality(false);
          setShowDownloadMenu(false);
          setMenu(null);
        } else if (opt.preset) {
          await deleteMyVariant(image.id, opt.preset);
          setItems((prev) => {
            const next = prev.map((i) =>
              i.id === image.id
                ? {
                    ...i,
                    variants: i.variants.filter(
                      (v) => v.preset_name !== opt.preset,
                    ),
                  }
                : i,
            );
            if (user) {
              patchGalleryCacheItems(
                user.id,
                libraryView === "trash",
                (cached) =>
                  cached.map((i) => {
                    const updated = next.find((n) => n.id === i.id);
                    return updated ?? i;
                  }),
                CACHE_KIND,
              );
            }
            return next;
          });
          setDisplayKeyByImage((prev) => {
            const current = prev[image.id];
            if (current === opt.key) {
              const nextKey = defaultDisplayKey({
                ...image,
                variants: image.variants.filter(
                  (v) => v.preset_name !== opt.preset,
                ),
              });
              return { ...prev, [image.id]: nextKey };
            }
            return prev;
          });
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Delete failed");
      } finally {
        setDeletingKey(null);
      }
    },
    [confirm, removeItemsFromGrid, user, libraryView],
  );

  const toggleSelected = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set());
    setSelectMode(false);
  }, []);

  const selectAllVisible = useCallback(() => {
    setSelectMode(true);
    setSelectedIds(new Set(items.map((i) => i.id)));
  }, [items]);

  const idsInMarquee = useCallback((rect: DOMRect) => {
    const root = gridRef.current ?? stageRef.current;
    if (!root) {
      return [] as string[];
    }
    const hit: string[] = [];
    root.querySelectorAll<HTMLElement>("[data-photo-id]").forEach((el) => {
      const box = el.getBoundingClientRect();
      const overlaps =
        box.left < rect.right &&
        box.right > rect.left &&
        box.top < rect.bottom &&
        box.bottom > rect.top;
      if (overlaps) {
        const id = el.dataset.photoId;
        if (id) {
          hit.push(id);
        }
      }
    });
    return hit;
  }, []);

  const onStagePointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (e.button !== 0 || e.pointerType === "touch") {
        return;
      }
      const target = e.target as HTMLElement | null;
      if (!target) {
        return;
      }
      // Toolbar / links / menus — don't steal these clicks.
      if (
        target.closest(
          "[data-no-marquee], a, input, textarea, select, [role='menu'], [role='dialog']",
        )
      ) {
        return;
      }

      const additive = e.shiftKey || e.ctrlKey || e.metaKey;
      const startX = e.clientX;
      const startY = e.clientY;
      const pointerId = e.pointerId;

      // Marquee can start on empty stage, gaps, or a photo tile.
      // Plain left-click select is handled by each photo's onClick.
      dragRef.current = {
        pointerId,
        startX,
        startY,
        originSelected: additive ? new Set(selectedIds) : new Set(),
        additive,
        active: false,
        moved: false,
        photoId: null,
      };

      const onMove = (ev: PointerEvent) => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== pointerId) {
          return;
        }
        const dist = Math.hypot(ev.clientX - drag.startX, ev.clientY - drag.startY);
        if (!drag.active && dist < 6) {
          return;
        }
        if (!drag.active) {
          drag.active = true;
          drag.moved = true;
          suppressClickRef.current = true;
          setSelectMode(true);
        }

        const left = Math.min(drag.startX, ev.clientX);
        const top = Math.min(drag.startY, ev.clientY);
        const right = Math.max(drag.startX, ev.clientX);
        const bottom = Math.max(drag.startY, ev.clientY);
        setMarquee({
          x: left,
          y: top,
          w: right - left,
          h: bottom - top,
        });

        const hit = idsInMarquee(
          new DOMRect(left, top, right - left, bottom - top),
        );
        const next = new Set(drag.originSelected);
        for (const id of hit) {
          next.add(id);
        }
        setSelectedIds(next);
      };

      const onUp = () => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);

        const drag = dragRef.current;
        dragRef.current = null;
        setMarquee(null);
        if (!drag || drag.pointerId !== pointerId) {
          return;
        }

        if (drag.moved) {
          // Suppress the trailing click on the photo under the pointer.
          window.setTimeout(() => {
            suppressClickRef.current = false;
          }, 0);
        }
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
    },
    [selectedIds, idsInMarquee],
  );

  const runSelectionAction = useCallback(
    async (action: "trash" | "restore" | "purge") => {
      const ids = Array.from(selectedIds);
      if (ids.length === 0) {
        return;
      }
      const countLabel = `${ids.length} photo${ids.length === 1 ? "" : "s"}`;
      const ok = await confirm(
        action === "trash"
          ? {
              title: "Move to Trash?",
              message: (
                <>
                  Move <span className="font-semibold">{countLabel}</span>{" "}
                  (including all WebP variants) to Trash? You can restore them
                  later.
                </>
              ),
              confirmLabel: "Move to Trash",
              tone: "danger",
            }
          : action === "restore"
            ? {
                title: "Restore photos?",
                message: (
                  <>
                    Restore <span className="font-semibold">{countLabel}</span>{" "}
                    and all variants back to your library?
                  </>
                ),
                confirmLabel: "Restore",
                tone: "default",
              }
            : {
                title: "Delete forever?",
                message: (
                  <>
                    Permanently delete{" "}
                    <span className="font-semibold">{countLabel}</span> and all
                    variants from the database and S3? This cannot be undone.
                  </>
                ),
                confirmLabel: "Delete forever",
                tone: "danger",
              },
      );
      if (!ok) {
        return;
      }
      setActionBusy(true);
      setError(null);
      try {
        if (action === "trash") {
          await trashMyImages(ids);
        } else if (action === "restore") {
          await restoreMyImages(ids);
        } else {
          await purgeMyImages(ids);
        }
        removeItemsFromGrid(ids);
        clearSelection();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Action failed");
      } finally {
        setActionBusy(false);
      }
    },
    [confirm, selectedIds, removeItemsFromGrid, clearSelection],
  );

  const goPrev = useCallback(() => {
    setViewerIndex((idx) => {
      if (idx === null || idx <= 0) {
        return idx;
      }
      return idx - 1;
    });
  }, []);

  const goNext = useCallback(() => {
    setViewerIndex((idx) => {
      if (idx === null) {
        return idx;
      }
      if (idx >= items.length - 1) {
        if (hasMore && !loadingMore) {
          void loadPage(nextOffset, false);
        }
        return idx;
      }
      return idx + 1;
    });
  }, [items.length, hasMore, loadingMore, loadPage, nextOffset]);

  // After loading more while at the end, advance once.
  useEffect(() => {
    if (viewerIndex === null) {
      return;
    }
    if (viewerIndex === items.length - 2 && items.length > 1) {
      // no-op; keep index stable
    }
  }, [items.length, viewerIndex]);

  useEffect(() => {
    if (viewerIndex === null) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (showInfo) {
          setShowInfo(false);
        } else {
          closeViewer();
        }
      } else if (e.key === "ArrowLeft") {
        goPrev();
      } else if (e.key === "ArrowRight") {
        goNext();
      } else if (e.key === "i" || e.key === "I") {
        setShowInfo((v) => !v);
        setShowQuality(false);
        setShowDownloadMenu(false);
      } else if (e.key === "q" || e.key === "Q") {
        setShowQuality((v) => !v);
        setShowDownloadMenu(false);
      }
    };
    window.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [viewerIndex, showInfo, closeViewer, goPrev, goNext]);

  const openViewer = (index: number, info = false) => {
    setViewerIndex(index);
    setShowInfo(info);
    setShowQuality(false);
    setShowDownloadMenu(false);
    setMenu(null);
  };

  const applyLibraryRefresh = useCallback(
    (payload: LibraryRefreshDetail) => {
      // Ignore out-of-order refresh payloads from parallel uploads.
      if (
        typeof payload.generation === "number" &&
        payload.generation < lastRefreshGenerationRef.current
      ) {
        return;
      }
      if (typeof payload.generation === "number") {
        lastRefreshGenerationRef.current = payload.generation;
      }

      if (payload.albums) {
        setAlbums(payload.albums);
      } else {
        void refreshAlbums();
      }

      const section = gallerySectionRef.current;
      if (section === "albums") {
        return;
      }
      if (section !== "photos") {
        return;
      }

      const viewingAlbumId = activeAlbumIdRef.current;
      if (viewingAlbumId) {
        const albumPayload =
          payload.albumId === viewingAlbumId ? payload.albumPayload : null;
        if (albumPayload?.items) {
          setItems(albumPayload.items);
          setTotal(albumPayload.pagination.total);
          setNextOffset(albumPayload.pagination.next_offset);
          setHasMore(albumPayload.pagination.has_more);
          setBusy(false);
          return;
        }
        void loadPage(0, true);
        return;
      }

      if (mediaFilterRef.current) {
        void loadPage(0, true);
        return;
      }

      // Always replace with the latest server list so each finished upload appears.
      setItems(payload.items);
      setTotal(payload.pagination.total);
      setNextOffset(payload.pagination.next_offset);
      setHasMore(payload.pagination.has_more);
      setBusy(false);
    },
    [loadPage, refreshAlbums],
  );

  useEffect(() => {
    gallerySectionRef.current = gallerySection;
    activeAlbumIdRef.current = activeAlbumId;
    mediaFilterRef.current = mediaFilter;
  }, [gallerySection, activeAlbumId, mediaFilter]);

  useEffect(() => {
    applyLibraryRefreshRef.current = applyLibraryRefresh;
  }, [applyLibraryRefresh]);

  useEffect(() => {
    const handler = (payload: LibraryRefreshDetail) => {
      applyLibraryRefreshRef.current?.(payload);
    };
    galleryUploadQueue.configure({
      userId: user?.id ?? null,
      onLibraryRefresh: handler,
    });
    return () => {
      galleryUploadQueue.configure({
        userId: user?.id ?? null,
        onLibraryRefresh: null,
      });
    };
  }, [user?.id]);

  useEffect(() => {
    const sync = () => {
      setUploading(galleryUploadQueue.snapshot().uploading);
    };
    sync();
    const onErr = (event: Event) => {
      const detail = (event as CustomEvent<{ message?: string }>).detail;
      if (detail?.message) {
        setError(detail.message);
      }
    };
    const onRefresh = (event: Event) => {
      const payload = (event as CustomEvent<LibraryRefreshDetail>).detail;
      if (payload?.items) {
        applyLibraryRefreshRef.current?.(payload);
      }
    };
    const onAlbums = (event: Event) => {
      const next = (event as CustomEvent<Album[]>).detail;
      if (Array.isArray(next)) {
        setAlbums(next);
      }
    };
    galleryUploadQueue.addEventListener("change", sync);
    galleryUploadQueue.addEventListener("fileerror", onErr);
    galleryUploadQueue.addEventListener("libraryrefresh", onRefresh);
    galleryUploadQueue.addEventListener("albumsrefresh", onAlbums);
    return () => {
      galleryUploadQueue.removeEventListener("change", sync);
      galleryUploadQueue.removeEventListener("fileerror", onErr);
      galleryUploadQueue.removeEventListener("libraryrefresh", onRefresh);
      galleryUploadQueue.removeEventListener("albumsrefresh", onAlbums);
    };
  }, []);

  const uploadGalleryFiles = useCallback(
    (fileList: File[]) => {
      if (!user || gallerySection === "trash" || fileList.length === 0) {
        return;
      }
      const files = filterGalleryUploadFiles(fileList);
      if (files.length === 0) {
        setError(
          "Drop photos or videos only (JPEG, PNG, WebP, HEIC, MP4, MOV…).",
        );
        return;
      }
      setError(null);
      if (gallerySection !== "photos") {
        setGallerySection("photos");
      }
      galleryUploadQueue.configure({
        userId: user.id,
        onLibraryRefresh: (payload) => {
          applyLibraryRefreshRef.current?.(payload);
        },
      });
      galleryUploadQueue.enqueue(files, activeAlbumId);
    },
    [user, gallerySection, activeAlbumId],
  );

  const onContextMenu = (
    event: ReactMouseEvent,
    index: number,
  ) => {
    event.preventDefault();
    const pad = 8;
    const menuW = 240;
    const menuH = 260;
    const x = Math.min(event.clientX, window.innerWidth - menuW - pad);
    const y = Math.min(event.clientY, window.innerHeight - menuH - pad);
    setMenu({ x, y, index });
  };

  const onTouchStart = (e: ReactTouchEvent) => {
    touchStartX.current = e.changedTouches[0]?.clientX ?? null;
  };

  const onTouchEnd = (e: ReactTouchEvent) => {
    const start = touchStartX.current;
    touchStartX.current = null;
    if (start === null) {
      return;
    }
    const end = e.changedTouches[0]?.clientX ?? start;
    const delta = end - start;
    if (Math.abs(delta) < 56) {
      return;
    }
    if (delta > 0) {
      goPrev();
    } else {
      goNext();
    }
  };

  const copyProxyURL = async (url: string) => {
    await navigator.clipboard?.writeText(proxyAbsolute(url));
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  if (loading || signInBusy) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-16 text-slate-600 dark:text-slate-300">
        {signInBusy ? "Signing in with Google…" : "Loading gallery…"}
      </main>
    );
  }

  if (user && !galleryAllowed) {
    return (
      <main className="mx-auto max-w-lg px-4 py-16 text-center">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-white">
          Gallery unavailable
        </h1>
        <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
          This personal gallery is private. You still have Converter, Object
          Identifier, and Dashboard.
        </p>
        <Link
          href="/dashboard"
          className="mt-6 inline-flex cursor-pointer text-sm font-medium text-sky-700 hover:underline dark:text-sky-300"
        >
          Go to Dashboard
        </Link>
      </main>
    );
  }

  if (!user) {
    return (
      <main className="relative mx-auto flex min-h-[70vh] max-w-3xl flex-col items-center justify-center px-4 py-16 text-center">
        <h1 className="font-[family-name:var(--font-display)] text-4xl tracking-tight text-slate-900 dark:text-white">
          Photos
        </h1>
        <p className="mt-3 max-w-md text-slate-700 dark:text-slate-200">
          {googleConfigured
            ? "Opening Google sign-in…"
            : "Add NEXT_PUBLIC_GOOGLE_CLIENT_ID to enable Google sign-in."}
        </p>
        {googleConfigured ? (
          <div
            ref={hiddenGoogleRef}
            className="pointer-events-none absolute h-0 w-0 overflow-hidden opacity-0"
            aria-hidden="true"
          >
            <GoogleLogin
              onSuccess={(res) => {
                if (!res.credential) {
                  return;
                }
                setSignInBusy(true);
                void signInWithGoogleCredential(res.credential).finally(() => {
                  setSignInBusy(false);
                });
              }}
              onError={() => setError("Google sign-in was cancelled or failed")}
              useOneTap={false}
              theme="filled_black"
              size="large"
              shape="pill"
              text="signin_with"
            />
          </div>
        ) : null}
        {error && (
          <p className="mt-4 text-sm text-rose-700">{error}</p>
        )}
        <Link
          href="/converter"
          className="mt-6 cursor-pointer text-sm font-medium text-sky-700 hover:underline"
        >
          Back home
        </Link>
      </main>
    );
  }

  const activeAlbum = activeAlbumId
    ? albums.find((a) => a.id === activeAlbumId) ?? null
    : null;
  const menuItem = menu ? items[menu.index] : null;
  const canPrev = viewerIndex !== null && viewerIndex > 0;
  const canNext =
    viewerIndex !== null &&
    (viewerIndex < items.length - 1 || hasMore);

  const isSuperAdmin =
    user?.is_super_admin ||
    user?.email?.toLowerCase() === GALLERY_SUPER_ADMIN_EMAIL;
  return (
    <main
      className="relative mx-auto min-h-[calc(100vh-4rem)] max-w-[1600px] px-2 pb-10 pt-4 text-slate-900 sm:px-4 sm:pt-6 dark:text-slate-100"
      onDragEnter={(e) => {
        if (gallerySection === "trash") {
          return;
        }
        if (![...e.dataTransfer.types].includes("Files")) {
          return;
        }
        e.preventDefault();
        fileDragDepth.current += 1;
        setFileDragOver(true);
      }}
      onDragOver={(e) => {
        if (gallerySection === "trash") {
          return;
        }
        if (![...e.dataTransfer.types].includes("Files")) {
          return;
        }
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      }}
      onDragLeave={() => {
        fileDragDepth.current = Math.max(0, fileDragDepth.current - 1);
        if (fileDragDepth.current === 0) {
          setFileDragOver(false);
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        fileDragDepth.current = 0;
        setFileDragOver(false);
        if (gallerySection === "trash") {
          return;
        }
        const files = Array.from(e.dataTransfer.files || []);
        uploadGalleryFiles(files);
      }}
    >
      {fileDragOver && gallerySection !== "trash" ? (
        <div className="pointer-events-none fixed inset-0 z-[60] flex items-center justify-center bg-[#1a73e8]/12 backdrop-blur-[1px]">
          <div className="rounded-3xl border-2 border-dashed border-[#1a73e8] bg-white/95 px-10 py-12 text-center shadow-2xl dark:bg-slate-900/95">
            <Upload className="mx-auto h-10 w-10 text-[#1a73e8]" />
            <p className="mt-3 text-lg font-medium text-slate-900 dark:text-white">
              Drop files to upload
            </p>
            <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
              Photos and videos · originals only
            </p>
          </div>
        </div>
      ) : null}

      <input
        ref={uploadInputRef}
        type="file"
        accept={GALLERY_ACCEPT}
        multiple
        className="hidden"
        onChange={(e) => {
          const files = Array.from(e.target.files || []);
          e.target.value = "";
          void uploadGalleryFiles(files);
        }}
      />

      {/* Google Photos-style library header */}
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3 px-1 sm:mb-5">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 sm:text-[1.75rem] dark:text-white">
            {gallerySection === "trash"
              ? "Trash"
              : gallerySection === "albums"
                ? "Albums"
                : activeAlbum
                  ? activeAlbum.title
                  : "Photos"}
          </h1>
          <p className="mt-0.5 text-sm font-medium text-slate-700 dark:text-slate-200">
            {gallerySection === "albums"
              ? `${albums.length} album${albums.length === 1 ? "" : "s"}`
              : busy && items.length === 0
                ? "Loading…"
                : `${total.toLocaleString()} item${total === 1 ? "" : "s"}`}
            {gallerySection === "trash"
              ? " · removed forever after 30 days"
              : gallerySection === "photos"
                ? " · personal gallery · drop anywhere to upload"
                : ""}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2" data-no-marquee>
          <div className="inline-flex rounded-full border border-slate-200 bg-white p-0.5 dark:border-slate-700 dark:bg-slate-900">
            {(
              [
                ["photos", "Photos"],
                ["albums", "Albums"],
              ] as const
            ).map(([key, label]) => (
              <button
                key={key}
                type="button"
                onClick={() => {
                  setGallerySection(key);
                  if (key !== "photos") {
                    setActiveAlbumId(null);
                    setMediaFilter("");
                    clearSelection();
                  }
                }}
                className={`cursor-pointer rounded-full px-3 py-1.5 text-xs font-semibold ${
                  gallerySection === key
                    ? "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                    : "text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-white"
                }`}
              >
                {label}
              </button>
            ))}
          </div>

          <div ref={actionsMenuRef} className="relative">
            <button
              type="button"
              onClick={() => setActionsOpen((o) => !o)}
              className="inline-flex h-9 w-9 cursor-pointer items-center justify-center rounded-full border border-slate-200 bg-white text-slate-700 transition hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
              aria-label="More actions"
              aria-expanded={actionsOpen}
              title="More"
            >
              <MoreVertical className="h-4 w-4" />
            </button>
            {actionsOpen ? (
              <div
                role="menu"
                className="absolute right-0 top-[calc(100%+0.35rem)] z-40 w-56 overflow-hidden rounded-xl border border-slate-200 bg-white py-1 shadow-xl dark:border-slate-700 dark:bg-slate-900"
              >
                {gallerySection === "photos" ? (
                  <button
                    type="button"
                    role="menuitem"
                    disabled={uploading}
                    onClick={() => {
                      setActionsOpen(false);
                      uploadInputRef.current?.click();
                    }}
                    className="flex w-full cursor-pointer items-center gap-2.5 px-3 py-2.5 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 disabled:opacity-50 dark:text-slate-100 dark:hover:bg-slate-800"
                  >
                    <Upload className="h-4 w-4" />
                    Upload
                  </button>
                ) : null}
                {gallerySection !== "trash" ? (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setActionsOpen(false);
                      setAlbumTitle("");
                      setShowCreateAlbum(true);
                    }}
                    className="flex w-full cursor-pointer items-center gap-2.5 px-3 py-2.5 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                  >
                    <FolderPlus className="h-4 w-4" />
                    New album
                  </button>
                ) : null}
                {gallerySection !== "albums" ? (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setActionsOpen(false);
                      if (selectMode) {
                        clearSelection();
                      } else {
                        setSelectMode(true);
                      }
                    }}
                    className="flex w-full cursor-pointer items-center gap-2.5 px-3 py-2.5 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                  >
                    <Check className="h-4 w-4" />
                    {selectMode ? "Cancel select" : "Select"}
                  </button>
                ) : null}
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    setActionsOpen(false);
                    setGallerySection("trash");
                    setActiveAlbumId(null);
                    setMediaFilter("");
                    clearSelection();
                  }}
                  className="flex w-full cursor-pointer items-center gap-2.5 px-3 py-2.5 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                >
                  <Trash2 className="h-4 w-4" />
                  Trash
                </button>
                {isSuperAdmin ? (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setActionsOpen(false);
                      setShowAccessSettings(true);
                      void listGalleryAccess()
                        .then((data) => setAccessEntries(data.entries || []))
                        .catch((err) =>
                          setError(
                            err instanceof Error
                              ? err.message
                              : "Failed to load access list",
                          ),
                        );
                    }}
                    className="flex w-full cursor-pointer items-center gap-2.5 px-3 py-2.5 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                  >
                    <Settings className="h-4 w-4" />
                    Access
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>
        </div>
      </div>

      {activeAlbum && gallerySection === "photos" ? (
        <div data-no-marquee className="mb-3 px-1">
          <button
            type="button"
            onClick={() => {
              setActiveAlbumId(null);
              setGallerySection("albums");
            }}
            className="inline-flex cursor-pointer items-center gap-1 text-sm font-medium text-sky-700 hover:underline dark:text-sky-300"
          >
            <ChevronLeft className="h-4 w-4" />
            All albums
          </button>
        </div>
      ) : null}

      {gallerySection === "photos" && !activeAlbumId ? (
        <div
          data-no-marquee
          className="mb-3 flex flex-wrap items-center gap-2 px-1"
        >
          <button
            type="button"
            onClick={() => setMediaFilter("")}
            className={`cursor-pointer rounded-full px-3 py-1 text-xs font-semibold ${
              !mediaFilter
                ? "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                : "border border-slate-200 bg-white text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
            }`}
          >
            All
          </button>
          <button
            type="button"
            onClick={() => setMediaFilter("image")}
            className={`cursor-pointer rounded-full px-3 py-1 text-xs font-semibold ${
              mediaFilter === "image"
                ? "bg-sky-600 text-white"
                : "border border-slate-200 bg-white text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
            }`}
          >
            Photos
          </button>
          <button
            type="button"
            onClick={() => setMediaFilter("video")}
            className={`cursor-pointer rounded-full px-3 py-1 text-xs font-semibold ${
              mediaFilter === "video"
                ? "bg-violet-600 text-white"
                : "border border-slate-200 bg-white text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
            }`}
          >
            Videos
          </button>
        </div>
      ) : null}

      {selectMode ? (
        <div
          data-no-marquee
          className="sticky top-[4.25rem] z-30 mb-3 flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-slate-200 bg-white/95 px-3 py-2.5 shadow-lg backdrop-blur dark:border-slate-700 dark:bg-slate-950/95"
        >
          <div>
            <p className="text-sm font-semibold text-slate-900 dark:text-white">
              {selectedIds.size} selected
              {items.length > 0 ? ` of ${items.length}` : ""}
            </p>
            <p className="text-[11px] text-slate-600 dark:text-slate-300">
              Drag across photos to multi-select
              {libraryView === "trash"
                ? " · Delete forever removes original + all variants from DB and S3."
                : " · Trash hides the photo and all variants together."}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            {items.length > 0 ? (
              <button
                type="button"
                onClick={() => {
                  if (selectedIds.size === items.length) {
                    setSelectedIds(new Set());
                  } else {
                    selectAllVisible();
                  }
                }}
                className="inline-flex cursor-pointer items-center rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-800 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
              >
                {selectedIds.size === items.length ? "Deselect all" : "Select all"}
              </button>
            ) : null}
            {libraryView === "photos" ? (
              <>
                <button
                  type="button"
                  disabled={actionBusy || selectedIds.size === 0 || albums.length === 0}
                  onClick={() => setShowAddToAlbum(true)}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-800 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                >
                  <FolderPlus className="h-3.5 w-3.5" />
                  Add to album
                </button>
                <button
                  type="button"
                  disabled={actionBusy || selectedIds.size === 0}
                  onClick={() => void runSelectionAction("trash")}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-full bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-white dark:text-slate-900"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Move to trash
                </button>
              </>
            ) : (
              <>
                <button
                  type="button"
                  disabled={actionBusy || selectedIds.size === 0}
                  onClick={() => void runSelectionAction("restore")}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-800 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  Restore
                </button>
                <button
                  type="button"
                  disabled={actionBusy || selectedIds.size === 0}
                  onClick={() => void runSelectionAction("purge")}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-full bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Delete forever
                </button>
              </>
            )}
          </div>
        </div>
      ) : null}

      {claimedCount !== null && claimedCount > 0 && (
        <p className="mb-3 rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-900">
          Claimed {claimedCount} guest upload{claimedCount === 1 ? "" : "s"}.
        </p>
      )}
      {error && (
        <p className="mb-3 rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800">
          {error}
        </p>
      )}
      {copied && (
        <p className="mb-2 text-sm text-emerald-700">Copied proxy URL</p>
      )}

      {gallerySection === "albums" ? (
        <div data-no-marquee className="px-1">
          {albums.length === 0 ? (
            <div className="mt-8 rounded-3xl border border-dashed border-slate-300 bg-white/80 px-6 py-16 text-center dark:border-slate-600 dark:bg-slate-900/60">
              <FolderPlus className="mx-auto h-10 w-10 text-slate-400 dark:text-slate-300" />
              <p className="mt-4 font-medium text-slate-800 dark:text-slate-100">
                No albums yet
              </p>
              <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
                Create an album, then add photos from your library.
              </p>
              <button
                type="button"
                onClick={() => {
                  setAlbumTitle("");
                  setShowCreateAlbum(true);
                }}
                className="mt-4 inline-flex cursor-pointer text-sm font-medium text-sky-700 hover:underline dark:text-sky-300"
              >
                Create album
              </button>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {albums.map((album) => (
                <button
                  key={album.id}
                  type="button"
                  onClick={() => {
                    setActiveAlbumId(album.id);
                    setGallerySection("photos");
                  }}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    void (async () => {
                      const ok = await confirm({
                        title: "Delete album?",
                        message: (
                          <>
                            Remove album{" "}
                            <span className="font-semibold">{album.title}</span>?
                            Photos stay in your library.
                          </>
                        ),
                        confirmLabel: "Delete album",
                        tone: "danger",
                      });
                      if (!ok) {
                        return;
                      }
                      try {
                        await deleteAlbum(album.id);
                        setAlbums((prev) =>
                          prev.filter((a) => a.id !== album.id),
                        );
                      } catch (err) {
                        setError(
                          err instanceof Error
                            ? err.message
                            : "Delete album failed",
                        );
                      }
                    })();
                  }}
                  className="group cursor-pointer overflow-hidden rounded-2xl border border-slate-200 bg-white text-left shadow-sm transition hover:border-slate-300 hover:shadow-md dark:border-slate-700 dark:bg-slate-900 dark:hover:border-slate-600"
                  title="Open album · right-click to delete"
                >
                  <div className="flex aspect-square items-center justify-center bg-gradient-to-br from-slate-100 to-slate-200 dark:from-slate-800 dark:to-slate-950">
                    <ImageIcon className="h-10 w-10 text-slate-400 transition group-hover:scale-105 dark:text-slate-500" />
                  </div>
                  <div className="px-3 py-2.5">
                    <p className="truncate text-sm font-semibold text-slate-900 dark:text-white">
                      {album.title}
                    </p>
                    <p className="text-xs text-slate-600 dark:text-slate-300">
                      {album.item_count ?? 0} item
                      {(album.item_count ?? 0) === 1 ? "" : "s"}
                    </p>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      ) : null}

      {gallerySection !== "albums" ? (
      <>
      <div
        ref={stageRef}
        className="relative min-h-[calc(100vh-11rem)] touch-pan-y select-none"
        onPointerDown={onStagePointerDown}
      >
        {items.length === 0 && !busy ? (
          <div
            data-no-marquee
            className="mx-2 mt-8 rounded-3xl border border-dashed border-slate-300 bg-white/80 px-6 py-16 text-center dark:border-slate-600 dark:bg-slate-900/60"
          >
            <ImageIcon className="mx-auto h-10 w-10 text-slate-400 dark:text-slate-300" />
            <p className="mt-4 font-medium text-slate-800 dark:text-slate-100">
              {libraryView === "trash" ? "Trash is empty" : "No media yet"}
            </p>
            <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
              Drag and drop files here, or use Upload
            </p>
            {libraryView === "photos" ? (
              <button
                type="button"
                onClick={() => uploadInputRef.current?.click()}
                className="mt-4 inline-flex cursor-pointer items-center gap-1.5 text-sm font-medium text-sky-700 hover:underline dark:text-sky-300"
              >
                <Upload className="h-4 w-4" />
                Upload photos or videos
              </button>
            ) : null}
          </div>
        ) : (
          <div
            ref={gridRef}
            className="grid grid-cols-3 gap-0.5 sm:grid-cols-4 sm:gap-1 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7"
          >
            {items.map((item, index) => {
              const selected = selectedIds.has(item.id);
              return (
                <button
                  key={item.id}
                  type="button"
                  data-photo-id={item.id}
                  onClick={() => {
                    if (suppressClickRef.current) {
                      return;
                    }
                    if (selectMode || libraryView === "trash") {
                      if (!selectMode) {
                        setSelectMode(true);
                      }
                      toggleSelected(item.id);
                      return;
                    }
                    openViewer(index, false);
                  }}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    onContextMenu(e, index);
                  }}
                  className={`group relative aspect-square cursor-pointer overflow-hidden bg-slate-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 focus-visible:ring-offset-1 ${
                    selected ? "ring-2 ring-sky-500 ring-offset-1" : ""
                  }`}
                >
                  {isVideoItem(item) ? (
                    <video
                      src={thumbURL(item)}
                      muted
                      preload="metadata"
                      draggable={false}
                      className="pointer-events-none h-full w-full object-cover transition duration-200 group-hover:scale-[1.03]"
                    />
                  ) : (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={thumbURL(item)}
                      alt={item.original_filename}
                      draggable={false}
                      className="pointer-events-none h-full w-full object-cover transition duration-200 group-hover:scale-[1.03]"
                      loading="lazy"
                    />
                  )}
                  <span className="pointer-events-none absolute inset-0 bg-black/0 transition group-hover:bg-black/10" />
                  {isVideoItem(item) ? (
                    <span className="pointer-events-none absolute bottom-2 left-2 inline-flex items-center gap-1 rounded-full bg-black/55 px-2 py-0.5 text-[10px] font-semibold text-white">
                      <Video className="h-3 w-3" />
                      Video
                    </span>
                  ) : null}
                  {selectMode || selected ? (
                    <span
                      className={`pointer-events-none absolute left-2 top-2 flex h-6 w-6 items-center justify-center rounded-full border-2 ${
                        selected
                          ? "border-sky-500 bg-sky-500 text-white"
                          : "border-white/90 bg-black/25 text-transparent"
                      }`}
                    >
                      <Check className="h-3.5 w-3.5" />
                    </span>
                  ) : null}
                </button>
              );
            })}
          </div>
        )}

        <div ref={sentinelRef} className="h-12 w-full" aria-hidden="true" />
        {loadingMore && (
          <p className="py-3 text-center text-sm text-slate-600 dark:text-slate-300">
            Loading more…
          </p>
        )}

        {marquee ? (
          <div
            className="pointer-events-none fixed z-40 border border-sky-400 bg-sky-400/20"
            style={{
              left: marquee.x,
              top: marquee.y,
              width: marquee.w,
              height: marquee.h,
            }}
          />
        ) : null}
      </div>

      {/* Context menu */}
      {menu && menuItem && (
        <div
          className="fixed z-[60] max-h-[70vh] min-w-[12rem] overflow-y-auto rounded-xl border border-slate-200 bg-white py-1 text-sm shadow-xl dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
          style={{ left: menu.x, top: menu.y }}
          onClick={(e) => e.stopPropagation()}
          role="menu"
        >
          {libraryView === "trash" ? (
            <>
              <button
                type="button"
                role="menuitem"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                onClick={() => {
                  const id = menuItem.id;
                  setMenu(null);
                  void (async () => {
                    try {
                      await restoreMyImages([id]);
                      removeItemsFromGrid([id]);
                    } catch (err) {
                      setError(
                        err instanceof Error ? err.message : "Restore failed",
                      );
                    }
                  })();
                }}
              >
                <RotateCcw className="h-3.5 w-3.5 shrink-0" />
                Restore
              </button>
              <button
                type="button"
                role="menuitem"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-rose-700 hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-950/40"
                onClick={() => {
                  const id = menuItem.id;
                  setMenu(null);
                  void (async () => {
                    const ok = await confirm({
                      title: "Delete forever?",
                      message:
                        "Permanently delete this item from storage? This cannot be undone.",
                      confirmLabel: "Delete forever",
                      tone: "danger",
                    });
                    if (!ok) {
                      return;
                    }
                    try {
                      await purgeMyImages([id]);
                      removeItemsFromGrid([id]);
                    } catch (err) {
                      setError(
                        err instanceof Error ? err.message : "Delete failed",
                      );
                    }
                  })();
                }}
              >
                <Trash2 className="h-3.5 w-3.5 shrink-0" />
                Delete forever
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                onClick={() => openViewer(menu.index, true)}
              >
                <Info className="h-3.5 w-3.5 text-slate-500 dark:text-slate-300" />
                Properties
              </button>
              <button
                type="button"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                onClick={() => openViewer(menu.index, false)}
              >
                <Sparkles className="h-3.5 w-3.5 text-slate-500 dark:text-slate-300" />
                Open
              </button>
              {buildDisplayOptions(menuItem).map((opt) => (
                <a
                  key={opt.key}
                  href={downloadProxyURL(opt.url)}
                  download={opt.downloadName}
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                  onClick={() => setMenu(null)}
                >
                  <Download className="h-3.5 w-3.5 shrink-0 text-slate-500 dark:text-slate-300" />
                  <span className="min-w-0 truncate">
                    {opt.label}
                    <span className="text-slate-500 dark:text-slate-400">
                      {" "}
                      ({formatBytes(opt.sizeBytes)})
                    </span>
                  </span>
                </a>
              ))}
              <button
                type="button"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                onClick={() => {
                  void copyProxyURL(menuItem.original_url);
                  setMenu(null);
                }}
              >
                <Copy className="h-3.5 w-3.5 text-slate-500 dark:text-slate-300" />
                Copy proxy URL
              </button>
              <div className="my-1 border-t border-slate-100 dark:border-slate-700" />
              <button
                type="button"
                role="menuitem"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-rose-700 hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-950/40"
                onClick={() => {
                  setMenu(null);
                  void handleDeleteOption(menuItem, {
                    key: "source",
                    label: "Original",
                    downloadName: menuItem.original_filename,
                    url: menuItem.original_url,
                    width: menuItem.width,
                    height: menuItem.height,
                    sizeBytes: menuItem.file_size_bytes,
                    mime: menuItem.mime_type,
                    kind: "source",
                  });
                }}
              >
                <Trash2 className="h-3.5 w-3.5 shrink-0" />
                Move to trash
              </button>
              {buildDisplayOptions(menuItem)
                .filter((opt) => opt.kind === "webp")
                .map((opt) => (
                  <button
                    key={`del-${opt.key}`}
                    type="button"
                    role="menuitem"
                    disabled={deletingKey === opt.key}
                    className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-rose-700 hover:bg-rose-50 disabled:opacity-50 dark:text-rose-300 dark:hover:bg-rose-950/40"
                    onClick={() => {
                      setMenu(null);
                      void handleDeleteOption(menuItem, opt);
                    }}
                  >
                    <Trash2 className="h-3.5 w-3.5 shrink-0" />
                    <span className="min-w-0 truncate">
                      Delete {opt.label} forever
                    </span>
                  </button>
                ))}
            </>
          )}
        </div>
      )}

      {/* Google Photos-style viewer */}
      {viewerItem && viewerIndex !== null && (
        <div
          className="fixed inset-0 z-50 flex flex-col bg-black"
          role="dialog"
          aria-modal="true"
          aria-label="Photo viewer"
          onTouchStart={onTouchStart}
          onTouchEnd={onTouchEnd}
        >
          {/* Top chrome — app bar on mobile, toolbar on desktop */}
          <div className="absolute inset-x-0 top-0 z-20 flex items-center justify-between gap-2 bg-gradient-to-b from-black/70 to-transparent px-2 py-2 sm:px-4 sm:py-3">
            <button
              type="button"
              onClick={closeViewer}
              className="inline-flex h-10 w-10 cursor-pointer items-center justify-center rounded-full text-white hover:bg-white/15"
              aria-label="Back"
            >
              <X className="h-5 w-5 sm:hidden" />
              <ChevronLeft className="hidden h-6 w-6 sm:block" />
            </button>
            <p className="min-w-0 flex-1 truncate text-center text-sm text-white/90 sm:text-left">
              <span className="hidden sm:inline">
                {viewerItem.original_filename}
              </span>
              <span className="sm:hidden">
                {viewerIndex + 1} / {total || items.length}
              </span>
            </p>
            <div className="relative flex items-center gap-1">
              <button
                type="button"
                onClick={() => {
                  setShowQuality((v) => !v);
                  setShowDownloadMenu(false);
                }}
                className={`inline-flex h-10 items-center gap-1.5 rounded-full px-2.5 text-white hover:bg-white/15 sm:px-3 ${showQuality ? "bg-white/20" : ""}`}
                aria-label="Choose display version"
                aria-expanded={showQuality}
              >
                <Layers className="h-5 w-5" />
                <span className="hidden max-w-[10rem] truncate text-xs font-medium sm:inline">
                  {activeDisplay?.label || "Version"}
                </span>
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowDownloadMenu((v) => !v);
                  setShowQuality(false);
                }}
                className={`inline-flex h-10 w-10 cursor-pointer items-center justify-center rounded-full text-white hover:bg-white/15 ${showDownloadMenu ? "bg-white/20" : ""}`}
                aria-label="Download options"
                aria-expanded={showDownloadMenu}
              >
                <Download className="h-5 w-5" />
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowInfo((v) => !v);
                  setShowQuality(false);
                  setShowDownloadMenu(false);
                }}
                className={`inline-flex h-10 w-10 cursor-pointer items-center justify-center rounded-full text-white hover:bg-white/15 ${showInfo ? "bg-white/20" : ""}`}
                aria-label="Info"
                aria-pressed={showInfo}
              >
                <Info className="h-5 w-5" />
              </button>

              {/* Desktop/mobile: version chooser popover */}
              {showQuality && (
                <div className="absolute right-0 top-12 z-40 w-[min(20rem,calc(100vw-1.5rem))] overflow-hidden rounded-2xl border border-white/10 bg-[#1e1e1e] py-1 shadow-2xl">
                  <p className="px-3 py-2 text-[11px] font-semibold uppercase tracking-wide text-white/45">
                    Display version
                  </p>
                  {displayOptions.map((opt) => {
                    const selected = opt.key === activeDisplay?.key;
                    return (
                      <button
                        key={opt.key}
                        type="button"
                        onClick={() => {
                          setDisplayKey(opt.key);
                          setShowQuality(false);
                        }}
                        className={`flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm text-white hover:bg-white/10 ${selected ? "bg-white/10" : ""}`}
                      >
                        <span className="min-w-0 flex-1">
                          <span className="block truncate font-medium">
                            {opt.label}
                          </span>
                          <span className="block text-xs text-white/50">
                            {opt.width}×{opt.height} ·{" "}
                            {formatBytes(opt.sizeBytes)}
                          </span>
                        </span>
                        {selected ? (
                          <Check className="h-4 w-4 shrink-0 text-sky-300" />
                        ) : null}
                      </button>
                    );
                  })}
                </div>
              )}

              {/* Download chooser: source + each WebP */}
              {showDownloadMenu && (
                <div className="absolute right-0 top-12 z-40 w-[min(20rem,calc(100vw-1.5rem))] overflow-hidden rounded-2xl border border-white/10 bg-[#1e1e1e] py-1 shadow-2xl">
                  <p className="px-3 py-2 text-[11px] font-semibold uppercase tracking-wide text-white/45">
                    Download
                  </p>
                  {displayOptions.map((opt) => (
                    <a
                      key={`dl-${opt.key}`}
                      href={downloadProxyURL(opt.url)}
                      download={opt.downloadName}
                      className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm text-white hover:bg-white/10"
                      onClick={() => setShowDownloadMenu(false)}
                    >
                      <Download className="h-3.5 w-3.5 shrink-0 text-white/50" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">
                          {opt.label}
                        </span>
                        <span className="block text-xs text-white/50">
                          {formatBytes(opt.sizeBytes)} · {opt.mime}
                        </span>
                      </span>
                    </a>
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="relative flex min-h-0 flex-1">
            {/* Image stage */}
            <div
              className={`relative flex min-w-0 flex-1 items-center justify-center ${showInfo ? "lg:pr-0" : ""}`}
              onClick={() => {
                // Mobile: tap empty area toggles chrome feel — close info if open
                if (showInfo && window.innerWidth < 1024) {
                  setShowInfo(false);
                }
              }}
            >
              {/* Desktop prev/next */}
              <button
                type="button"
                disabled={!canPrev}
                onClick={(e) => {
                  e.stopPropagation();
                  goPrev();
                }}
                className="absolute left-2 top-1/2 z-10 hidden h-12 w-12 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full bg-black/40 text-white backdrop-blur-sm transition hover:bg-black/60 disabled:opacity-20 sm:flex"
                aria-label="Previous photo"
              >
                <ChevronLeft className="h-7 w-7" />
              </button>
              <button
                type="button"
                disabled={!canNext}
                onClick={(e) => {
                  e.stopPropagation();
                  goNext();
                }}
                className={`absolute top-1/2 z-10 hidden h-12 w-12 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full bg-black/40 text-white backdrop-blur-sm transition hover:bg-black/60 disabled:opacity-20 sm:flex ${showInfo ? "right-2 lg:right-[23rem]" : "right-2"}`}
                aria-label="Next photo"
              >
                <ChevronRight className="h-7 w-7" />
              </button>

              {isVideoItem(viewerItem) ? (
                <video
                  key={viewerItem.id}
                  src={sameOriginProxyURL(viewerItem.original_url)}
                  controls
                  playsInline
                  className={`max-h-[100dvh] max-w-full object-contain ${showInfo ? "lg:max-w-[calc(100%-22rem)]" : ""}`}
                />
              ) : (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  key={`${viewerItem.id}-${activeDisplay?.key || "source"}`}
                  src={
                    activeDisplay?.url ||
                    sameOriginProxyURL(viewerItem.original_url)
                  }
                  alt={viewerItem.original_filename}
                  className={`max-h-[100dvh] max-w-full object-contain transition-opacity duration-200 ${showInfo ? "lg:max-w-[calc(100%-22rem)]" : ""}`}
                  draggable={false}
                />
              )}

              {/* Mobile / desktop: quick version chips — images only */}
              {!isVideoItem(viewerItem) ? (
                <div className="absolute inset-x-0 bottom-3 z-10 flex justify-center px-2 sm:bottom-5">
                  <div className="flex max-w-full items-center gap-1 overflow-x-auto rounded-full bg-black/55 p-1 backdrop-blur-md">
                    {displayOptions.map((opt) => {
                      const selected = opt.key === activeDisplay?.key;
                      return (
                        <button
                          key={`chip-${opt.key}`}
                          type="button"
                          title={`${opt.downloadName} · ${formatBytes(opt.sizeBytes)}`}
                          onClick={(e) => {
                            e.stopPropagation();
                            setDisplayKey(opt.key);
                          }}
                          className={`shrink-0 cursor-pointer rounded-full px-2.5 py-1.5 text-[11px] font-semibold transition sm:px-3 sm:text-xs ${
                            selected
                              ? "bg-white text-slate-900"
                              : "text-white/80 hover:bg-white/10 hover:text-white"
                          }`}
                        >
                          {opt.label}
                          <span className="ml-1 opacity-70">
                            {formatBytes(opt.sizeBytes)}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              ) : null}
            </div>

            {/* Desktop: Google Photos info side panel */}
            {showInfo && (
              <aside className="hidden w-[22rem] shrink-0 overflow-y-auto border-l border-white/10 bg-[#1e1e1e] p-5 lg:block">
                <div className="mb-4 flex items-center justify-between">
                  <h3 className="text-base font-medium text-white">Info</h3>
                  <button
                    type="button"
                    onClick={() => setShowInfo(false)}
                    className="inline-flex cursor-pointer rounded-full p-1.5 text-white/70 hover:bg-white/10 hover:text-white"
                    aria-label="Close info"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </div>
                <InfoContent
                  item={viewerItem}
                  light
                  displayKey={displayKey}
                  deletingKey={deletingKey}
                  onSelectDisplay={(key) => {
                    setDisplayKey(key);
                    setShowQuality(false);
                  }}
                  onDeleteOption={(opt) =>
                    void handleDeleteOption(viewerItem, opt)
                  }
                />
              </aside>
            )}
          </div>

          {/* Mobile only: bottom sheet (portaled — must not mount on desktop). */}
          {infoAsSheet && (
            <PhotosSheet
              open={showInfo}
              onClose={() => setShowInfo(false)}
              title="Info"
              dark
              labelledBy="viewer-info-title"
            >
              <InfoContent
                item={viewerItem}
                light
                displayKey={displayKey}
                deletingKey={deletingKey}
                onSelectDisplay={(key) => {
                  setDisplayKey(key);
                  setShowQuality(false);
                }}
                onDeleteOption={(opt) =>
                  void handleDeleteOption(viewerItem, opt)
                }
              />
            </PhotosSheet>
          )}
        </div>
      )}
      </>
      ) : null}

      <PhotosSheet
        open={showCreateAlbum}
        onClose={() => setShowCreateAlbum(false)}
        title="New album"
        labelledBy="create-album-title"
        footer={
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <button
              type="button"
              className="inline-flex cursor-pointer items-center justify-center rounded-full border border-slate-200 bg-white px-4 py-2.5 text-sm font-semibold text-slate-800 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
              onClick={() => setShowCreateAlbum(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="inline-flex cursor-pointer items-center justify-center rounded-full bg-slate-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900"
              onClick={() => {
                void (async () => {
                  const title = albumTitle.trim();
                  if (!title) {
                    return;
                  }
                  try {
                    const album = await createAlbum(title);
                    if (selectedIds.size > 0) {
                      await addAlbumItems(
                        album.id,
                        Array.from(selectedIds),
                      );
                      clearSelection();
                    }
                    await refreshAlbums();
                    setShowCreateAlbum(false);
                    setAlbumTitle("");
                    setActiveAlbumId(album.id);
                    setGallerySection("photos");
                    // Open album grid immediately (don't wait only on effect).
                    activeAlbumIdRef.current = album.id;
                    gallerySectionRef.current = "photos";
                    const payload = await listGalleryMedia({
                      limit: PAGE_SIZE,
                      offset: 0,
                      trash: false,
                      albumId: album.id,
                    });
                    setItems(payload.items);
                    setTotal(payload.pagination.total);
                    setNextOffset(payload.pagination.next_offset);
                    setHasMore(payload.pagination.has_more);
                    setBusy(false);
                  } catch (err) {
                    setError(
                      err instanceof Error ? err.message : "Create album failed",
                    );
                  }
                })();
              }}
            >
              Create
            </button>
          </div>
        }
      >
        <label className="block text-sm font-medium text-slate-800 dark:text-slate-100">
          Title
          <input
            value={albumTitle}
            onChange={(e) => setAlbumTitle(e.target.value)}
            maxLength={120}
            placeholder="Trip, Family, Work…"
            className="mt-1.5 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-900 outline-none ring-sky-400 focus:ring-2 dark:border-slate-600 dark:bg-slate-950 dark:text-white"
          />
        </label>
        <p className="mt-3 text-sm text-slate-600 dark:text-slate-300">
          After creating, open the album and upload or add items from Photos
          with Select → Add to album. Right-click an album card to delete it
          (media stays in your library).
        </p>
      </PhotosSheet>

      <PhotosSheet
        open={showAccessSettings}
        onClose={() => setShowAccessSettings(false)}
        title="Gallery access"
        subtitle="Super admin controls who can open /gallery and gallery APIs"
        labelledBy="gallery-access-title"
      >
        <p className="mb-3 text-sm text-slate-600 dark:text-slate-300">
          Super admin{" "}
          <span className="font-semibold text-slate-900 dark:text-white">
            a2nabil000@gmail.com
          </span>{" "}
          always has access. Add other emails below.
        </p>
        <form
          className="flex flex-col gap-2 sm:flex-row"
          onSubmit={(e) => {
            e.preventDefault();
            const email = accessEmail.trim();
            if (!email) {
              return;
            }
            void (async () => {
              setAccessBusy(true);
              setError(null);
              try {
                await allowGalleryEmail(email);
                const data = await listGalleryAccess();
                setAccessEntries(data.entries || []);
                setAccessEmail("");
              } catch (err) {
                setError(
                  err instanceof Error ? err.message : "Failed to allow email",
                );
              } finally {
                setAccessBusy(false);
              }
            })();
          }}
        >
          <input
            type="email"
            required
            value={accessEmail}
            onChange={(e) => setAccessEmail(e.target.value)}
            placeholder="name@example.com"
            className="min-w-0 flex-1 rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-900 outline-none ring-sky-400 focus:ring-2 dark:border-slate-600 dark:bg-slate-950 dark:text-white"
          />
          <button
            type="submit"
            disabled={accessBusy}
            className="inline-flex cursor-pointer items-center justify-center rounded-full bg-slate-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-white dark:text-slate-900"
          >
            Allow
          </button>
        </form>
        <ul className="mt-4 space-y-1">
          {accessEntries.length === 0 ? (
            <li className="text-sm text-slate-500 dark:text-slate-400">
              No extra emails yet.
            </li>
          ) : (
            accessEntries.map((entry) => (
              <li
                key={entry.email}
                className="flex items-center justify-between gap-2 rounded-xl px-2 py-2 hover:bg-slate-50 dark:hover:bg-slate-800/80"
              >
                <span className="truncate text-sm font-medium text-slate-900 dark:text-white">
                  {entry.email}
                </span>
                <button
                  type="button"
                  disabled={accessBusy}
                  className="shrink-0 cursor-pointer text-xs font-semibold text-rose-600 hover:underline disabled:opacity-50 dark:text-rose-300"
                  onClick={() => {
                    void (async () => {
                      setAccessBusy(true);
                      try {
                        await revokeGalleryEmail(entry.email);
                        setAccessEntries((prev) =>
                          prev.filter((e) => e.email !== entry.email),
                        );
                      } catch (err) {
                        setError(
                          err instanceof Error
                            ? err.message
                            : "Failed to revoke",
                        );
                      } finally {
                        setAccessBusy(false);
                      }
                    })();
                  }}
                >
                  Revoke
                </button>
              </li>
            ))
          )}
        </ul>
      </PhotosSheet>

      <PhotosSheet
        open={showAddToAlbum}
        onClose={() => setShowAddToAlbum(false)}
        title="Add to album"
        subtitle={`${selectedIds.size} selected`}
        labelledBy="add-to-album-title"
      >
        {albums.length === 0 ? (
          <p className="text-sm text-slate-600 dark:text-slate-300">
            Create an album first.
          </p>
        ) : (
          <div className="space-y-1">
            {albums.map((album) => (
              <button
                key={album.id}
                type="button"
                className="flex w-full cursor-pointer items-center justify-between rounded-xl px-3 py-2.5 text-left text-sm font-medium text-slate-900 hover:bg-slate-100 dark:text-white dark:hover:bg-slate-800"
                onClick={() => {
                  void (async () => {
                    try {
                      await addAlbumItems(album.id, Array.from(selectedIds));
                      await refreshAlbums();
                      setShowAddToAlbum(false);
                      clearSelection();
                      if (activeAlbumIdRef.current === album.id) {
                        void loadPage(0, true);
                      }
                    } catch (err) {
                      setError(
                        err instanceof Error
                          ? err.message
                          : "Add to album failed",
                      );
                    }
                  })();
                }}
              >
                <span>{album.title}</span>
                <span className="text-xs text-slate-500 dark:text-slate-400">
                  {album.item_count ?? 0}
                </span>
              </button>
            ))}
          </div>
        )}
      </PhotosSheet>
    </main>
  );
}
