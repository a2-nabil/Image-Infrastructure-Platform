"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Download,
  ImagePlus,
  Loader2,
  MoreVertical,
  RefreshCw,
  Settings2,
  Trash2,
} from "lucide-react";
import PhotosSheet from "@/components/PhotosSheet";
import {
  formatBytes,
  uploadImage,
  waitForImageReady,
  type ImageUploadData,
  type ImageVariantData,
} from "@/lib/image-api";
import { mediaDisplayLabel, mediaFileName } from "@/lib/media-names";

const ALLOWED_IMAGE_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;
const MAX_FILE_BYTES = 10 * 1024 * 1024;
const VARIANT_OPTIONS = [
  { key: "thumbnail", label: "Thumbnail", hint: "150×150" },
  { key: "small", label: "Small", hint: "320px" },
  { key: "medium", label: "Medium", hint: "768px" },
  { key: "large", label: "Large", hint: "1280px" },
] as const;

type QueueStatus =
  | "queued"
  | "uploading"
  | "processing"
  | "ready"
  | "failed"
  | "cancelled";

type ConvertSettings = {
  quality: number;
  extraPresets: string[];
  useCustomSize: boolean;
  width: string;
  height: string;
};

type QueueItem = {
  id: string;
  file: File;
  previewURL: string;
  status: QueueStatus;
  progress: number;
  error?: string;
  result?: ImageUploadData;
  /** Per-file overrides; null/undefined = use global settings */
  overrides?: Partial<ConvertSettings> | null;
  /** Settings snapshot from the last successful conversion */
  convertedWith?: ConvertSettings | null;
};

const DEFAULT_SETTINGS: ConvertSettings = {
  quality: 80,
  extraPresets: [],
  useCustomSize: false,
  width: "",
  height: "",
};

function isAllowedImageType(type: string): boolean {
  return (ALLOWED_IMAGE_TYPES as readonly string[]).includes(type);
}

function proxyURL(url: string): string {
  try {
    const parsed = new URL(url, "http://localhost");
    if (parsed.pathname.startsWith("/api/v1/")) {
      return `${parsed.pathname}${parsed.search}`;
    }
  } catch {
    // keep
  }
  return url;
}

function downloadProxyURL(url: string): string {
  const value = proxyURL(url);
  const absolute =
    typeof window !== "undefined"
      ? new URL(value, window.location.origin).toString()
      : value;
  const parsed = new URL(absolute);
  parsed.searchParams.set("download", "1");
  return parsed.toString();
}

function statusLabel(status: QueueStatus): string {
  switch (status) {
    case "queued":
      return "Ready";
    case "uploading":
      return "Uploading…";
    case "processing":
      return "Converting…";
    case "ready":
      return "Done";
    case "failed":
      return "Failed";
    case "cancelled":
      return "Cancelled";
    default:
      return status;
  }
}

function mergeSettings(
  global: ConvertSettings,
  overrides?: Partial<ConvertSettings> | null,
): ConvertSettings {
  if (!overrides) {
    return global;
  }
  return {
    quality: overrides.quality ?? global.quality,
    extraPresets: overrides.extraPresets ?? global.extraPresets,
    useCustomSize: overrides.useCustomSize ?? global.useCustomSize,
    width: overrides.width ?? global.width,
    height: overrides.height ?? global.height,
  };
}

function settingsEqual(a: ConvertSettings, b: ConvertSettings): boolean {
  if (a.quality !== b.quality) {
    return false;
  }
  if (a.useCustomSize !== b.useCustomSize) {
    return false;
  }
  if ((a.width || "") !== (b.width || "")) {
    return false;
  }
  if ((a.height || "") !== (b.height || "")) {
    return false;
  }
  if (a.extraPresets.length !== b.extraPresets.length) {
    return false;
  }
  const left = [...a.extraPresets].sort();
  const right = [...b.extraPresets].sort();
  return left.every((preset, i) => preset === right[i]);
}

/** Convert is allowed when not yet done, or effective settings changed since last convert. */
function canConvertItem(
  item: QueueItem,
  global: ConvertSettings,
): boolean {
  if (
    item.status === "uploading" ||
    item.status === "processing"
  ) {
    return false;
  }
  if (item.status !== "ready" || !item.result) {
    return true;
  }
  if (!item.convertedWith) {
    return true;
  }
  const current = mergeSettings(global, item.overrides);
  return !settingsEqual(current, item.convertedWith);
}

function settingsToUploadOptions(settings: ConvertSettings) {
  const presets = ["original", ...settings.extraPresets];
  const width =
    settings.useCustomSize && settings.width.trim()
      ? Number.parseInt(settings.width, 10)
      : undefined;
  const height =
    settings.useCustomSize && settings.height.trim()
      ? Number.parseInt(settings.height, 10)
      : undefined;
  const hasCustom =
    settings.useCustomSize &&
    ((typeof width === "number" && width > 0) ||
      (typeof height === "number" && height > 0));

  return {
    presets,
    quality: settings.quality,
    width: hasCustom ? width : undefined,
    height: hasCustom ? height : undefined,
  };
}

function SettingsFields({
  value,
  onChange,
  compact,
}: {
  value: ConvertSettings;
  onChange: (next: ConvertSettings) => void;
  compact?: boolean;
}) {
  const togglePreset = (key: string) => {
    const next = value.extraPresets.includes(key)
      ? value.extraPresets.filter((p) => p !== key)
      : [...value.extraPresets, key];
    onChange({ ...value, extraPresets: next });
  };

  return (
    <div className={compact ? "space-y-3" : "space-y-4"}>
      <div>
        <div className="flex items-center justify-between text-sm">
          <label className="font-medium text-slate-800 dark:text-slate-100">
            Quality
          </label>
          <span className="tabular-nums text-slate-600 dark:text-slate-300">
            {value.quality}
          </span>
        </div>
        <input
          type="range"
          min={40}
          max={100}
          value={value.quality}
          onChange={(e) =>
            onChange({ ...value, quality: Number(e.target.value) })
          }
          className="mt-2 w-full accent-violet-600"
        />
      </div>

      <div>
        <p className="text-sm font-medium text-slate-800 dark:text-slate-100">
          Variants
        </p>
        <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
          Original-size WebP is always included.
        </p>
        <ul className="mt-2 grid grid-cols-2 gap-2">
          {VARIANT_OPTIONS.map((opt) => {
            const on = value.extraPresets.includes(opt.key);
            return (
              <li key={opt.key}>
                <label className="flex cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-sm hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:hover:bg-slate-800">
                  <input
                    type="checkbox"
                    checked={on}
                    onChange={() => togglePreset(opt.key)}
                    className="h-4 w-4 accent-violet-600"
                  />
                  <span>
                    <span className="block font-medium text-slate-800 dark:text-slate-100">
                      {opt.label}
                    </span>
                    <span className="block text-[11px] text-slate-500 dark:text-slate-400">
                      {opt.hint}
                    </span>
                  </span>
                </label>
              </li>
            );
          })}
        </ul>
      </div>

      <label className="flex cursor-pointer items-start gap-2.5">
        <input
          type="checkbox"
          checked={value.useCustomSize}
          onChange={(e) =>
            onChange({ ...value, useCustomSize: e.target.checked })
          }
          className="mt-1 h-4 w-4 accent-violet-600"
        />
        <span>
          <span className="block text-sm font-medium text-slate-800 dark:text-slate-100">
            Custom ratio
          </span>
          <span className="block text-xs text-slate-500 dark:text-slate-400">
            Set width × height; image fits inside and keeps aspect ratio.
          </span>
        </span>
      </label>

      {value.useCustomSize && (
        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">
              Width (px)
            </label>
            <input
              type="number"
              min={1}
              max={8192}
              placeholder="1280"
              value={value.width}
              onChange={(e) => onChange({ ...value, width: e.target.value })}
              className="mt-1 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm outline-none focus:border-violet-400 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">
              Height (px)
            </label>
            <input
              type="number"
              min={1}
              max={8192}
              placeholder="720"
              value={value.height}
              onChange={(e) => onChange({ ...value, height: e.target.value })}
              className="mt-1 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm outline-none focus:border-violet-400 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
            />
          </div>
        </div>
      )}
    </div>
  );
}

export default function ConverterPage() {
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const [items, setItems] = useState<QueueItem[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const [converting, setConverting] = useState(false);
  const [globalSettings, setGlobalSettings] =
    useState<ConvertSettings>(DEFAULT_SETTINGS);
  const [showGlobalSettings, setShowGlobalSettings] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(
    null,
  );
  const [editId, setEditId] = useState<string | null>(null);
  const [batchNote, setBatchNote] = useState<string | null>(null);

  const closeMenu = useCallback(() => {
    setMenuId(null);
    setMenuPos(null);
  }, []);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      items.forEach((item) => URL.revokeObjectURL(item.previewURL));
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!menuId) return;
    const close = () => closeMenu();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeMenu();
    };
    window.addEventListener("click", close);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [menuId, closeMenu]);

  const menuItem = menuId ? items.find((i) => i.id === menuId) : undefined;

  const addFiles = useCallback((fileList: FileList | File[]) => {
    const next: QueueItem[] = [];
    for (const file of Array.from(fileList)) {
      if (!isAllowedImageType(file.type) || file.size > MAX_FILE_BYTES) {
        continue;
      }
      next.push({
        id: crypto.randomUUID(),
        file,
        previewURL: URL.createObjectURL(file),
        status: "queued",
        progress: 0,
      });
    }
    if (next.length === 0) {
      setBatchNote("Use JPEG, PNG, or WebP under 10MB.");
      return;
    }
    setBatchNote(null);
    setItems((prev) => [...prev, ...next]);
  }, []);

  const removeItem = (id: string) => {
    setItems((prev) => {
      const target = prev.find((i) => i.id === id);
      if (target) {
        URL.revokeObjectURL(target.previewURL);
      }
      return prev.filter((i) => i.id !== id);
    });
    setMenuId(null);
    setEditId((cur) => (cur === id ? null : cur));
  };

  const clearAll = () => {
    abortRef.current?.abort();
    setConverting(false);
    setItems((prev) => {
      prev.forEach((i) => URL.revokeObjectURL(i.previewURL));
      return [];
    });
    setEditId(null);
    setMenuId(null);
    setShowGlobalSettings(false);
  };

  const updateItem = (id: string, patch: Partial<QueueItem>) => {
    setItems((prev) =>
      prev.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    );
  };

  const editingItem = items.find((i) => i.id === editId) ?? null;

  const readyItems = items.filter((i) => i.status === "ready" && i.result);
  const queuedCount = items.filter(
    (i) => i.status === "queued" || i.status === "failed",
  ).length;
  const allConverted =
    items.length > 0 && queuedCount === 0 && readyItems.length > 0;

  const startConversion = async (onlyIds?: string[]) => {
    const pending = items.filter((i) => {
      if (onlyIds && !onlyIds.includes(i.id)) {
        return false;
      }
      if (!canConvertItem(i, globalSettings)) {
        return false;
      }
      if (onlyIds) {
        return (
          i.status === "queued" ||
          i.status === "failed" ||
          i.status === "ready" ||
          i.status === "cancelled"
        );
      }
      return i.status === "queued" || i.status === "failed";
    });
    if (pending.length === 0 || converting) {
      return;
    }

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setConverting(true);
    setBatchNote(null);
    closeMenu();

    for (const item of pending) {
      updateItem(item.id, {
        status: "queued",
        progress: 0,
        error: undefined,
        result: undefined,
        convertedWith: null,
      });
    }

    const concurrency = onlyIds?.length === 1 ? 1 : 3;
    let cursor = 0;

    const runOne = async (item: QueueItem) => {
      if (controller.signal.aborted) {
        updateItem(item.id, { status: "cancelled" });
        return;
      }
      updateItem(item.id, {
        status: "uploading",
        progress: 15,
        error: undefined,
      });
      try {
        const settings = mergeSettings(globalSettings, item.overrides);
        const uploaded = await uploadImage(
          item.file,
          settingsToUploadOptions(settings),
        );
        if (controller.signal.aborted) {
          updateItem(item.id, { status: "cancelled" });
          return;
        }
        if (!uploaded.success || uploaded.mocked) {
          updateItem(item.id, {
            status: "failed",
            progress: 0,
            error: uploaded.mocked
              ? "Image API unavailable — start the Go API to convert."
              : uploaded.success
                ? "Upload failed"
                : uploaded.error?.message || "Upload failed",
          });
          return;
        }

        updateItem(item.id, {
          status: "processing",
          progress: 45,
          result: uploaded.data,
        });

        if (
          uploaded.data.deduplicated ||
          uploaded.data.status === "ready" ||
          (uploaded.data.variants?.length ?? 0) > 0
        ) {
          updateItem(item.id, {
            status: "ready",
            progress: 100,
            result: uploaded.data,
            convertedWith: settings,
          });
          return;
        }

        const ready = await waitForImageReady(uploaded.data.id, {
          signal: controller.signal,
          timeoutMs: 90000,
          onUpdate: (partial) => {
            updateItem(item.id, {
              status: "processing",
              progress: Math.min(90, 50 + (partial.variants?.length ?? 0) * 12),
              result: partial,
            });
          },
        });

        if (controller.signal.aborted) {
          updateItem(item.id, { status: "cancelled" });
          return;
        }

        if (!ready || ready.status === "failed") {
          updateItem(item.id, {
            status: "failed",
            progress: 0,
            error: "Conversion failed or timed out",
            result: ready ?? uploaded.data,
          });
          return;
        }

        updateItem(item.id, {
          status: "ready",
          progress: 100,
          result: ready,
          convertedWith: settings,
        });
      } catch (err) {
        updateItem(item.id, {
          status: "failed",
          progress: 0,
          error: err instanceof Error ? err.message : "Conversion failed",
        });
      }
    };

    const workers = Array.from({ length: concurrency }, async () => {
      while (cursor < pending.length) {
        const index = cursor;
        cursor += 1;
        const item = pending[index];
        if (!item) {
          break;
        }
        await runOne(item);
      }
    });

    await Promise.all(workers);
    if (!controller.signal.aborted) {
      setConverting(false);
    }
  };

  const downloadVariant = (
    variant: ImageVariantData,
    uploadedName: string,
    sourceWidth: number,
    sourceHeight: number,
  ) => {
    const a = document.createElement("a");
    a.href = downloadProxyURL(variant.url);
    a.download = mediaFileName({
      originalFilename: uploadedName,
      kind: "webp",
      width: variant.width,
      height: variant.height,
      sourceWidth,
      sourceHeight,
    });
    a.rel = "noreferrer";
    document.body.appendChild(a);
    a.click();
    a.remove();
  };

  const downloadAll = () => {
    for (const item of readyItems) {
      const variants = item.result?.variants ?? [];
      const preferred =
        variants.find((v) => v.preset_name === "original") || variants[0];
      if (preferred) {
        downloadVariant(
          preferred,
          item.file.name,
          item.result?.width || preferred.width,
          item.result?.height || preferred.height,
        );
      }
    }
  };

  return (
    <main className="min-h-[calc(100vh-4rem)] bg-white text-slate-900 dark:bg-[#0b1220] dark:text-slate-100">
      <section className="mx-auto max-w-5xl px-4 pb-6 pt-10 text-center sm:pt-12">
        <p className="text-xs font-semibold uppercase tracking-wider text-violet-600 dark:text-violet-400">
          Tools · Converter
        </p>
        <h1 className="mt-2 font-[family-name:var(--font-display)] text-3xl tracking-tight text-slate-900 sm:text-5xl dark:text-white">
          Any Image to WebP Converter
        </h1>
        <p className="mx-auto mt-3 max-w-2xl text-sm text-slate-500 sm:text-base dark:text-slate-400">
          Convert images to WebP in seconds — batch upload, optional variants,
          and custom size when you need them.
        </p>
        <div className="mt-6 flex flex-wrap items-center justify-center gap-3">
          <span className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-semibold text-slate-700 shadow-sm dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            Convert: <span className="text-violet-600 dark:text-violet-400">Any</span>
          </span>
          <span className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-semibold text-slate-700 shadow-sm dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            To: <span className="text-violet-600 dark:text-violet-400">WEBP</span>
          </span>
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-16">
        <input
          ref={inputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) {
              addFiles(e.target.files);
            }
            e.target.value = "";
          }}
        />

        <div className="rounded-3xl border border-slate-200 bg-white shadow-lg dark:border-slate-800 dark:bg-slate-950 dark:shadow-black/40">
          {/* Dropzone — always visible */}
          <div
            role="button"
            tabIndex={0}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                inputRef.current?.click();
              }
            }}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              if (e.dataTransfer.files?.length) {
                addFiles(e.dataTransfer.files);
              }
            }}
            className={`m-5 flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed px-6 transition ${
              items.length === 0 ? "min-h-[260px] py-12" : "min-h-[140px] py-8"
            } ${
              dragOver
                ? "border-violet-500 bg-violet-50 dark:bg-violet-950/40"
                : "border-violet-300 bg-white hover:border-violet-400 hover:bg-violet-50/40 dark:border-violet-700 dark:bg-slate-950 dark:hover:bg-violet-950/30"
            }`}
          >
            <span className="inline-flex items-center gap-2 rounded-xl bg-violet-600 px-5 py-3 text-sm font-semibold text-white shadow-sm">
              <ImagePlus className="h-4 w-4" />
              {items.length === 0 ? "Choose Files" : "Add more files"}
            </span>
            <p className="mt-3 text-sm text-slate-500 dark:text-slate-400">
              or drop your images here
            </p>
            <p className="mt-1 text-xs text-slate-400 dark:text-slate-500">
              JPEG, PNG, WebP · up to 10MB each
            </p>
          </div>

          {/* List + actions at bottom */}
          {items.length > 0 && (
            <div className="border-t border-slate-200 px-5 pb-5 pt-4 dark:border-slate-800">
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-semibold text-slate-800 dark:text-slate-100">
                  {items.length} file{items.length === 1 ? "" : "s"}
                </p>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setShowGlobalSettings(true)}
                    className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    <Settings2 className="h-3.5 w-3.5" />
                    Global settings
                  </button>
                  <button
                    type="button"
                    onClick={clearAll}
                    className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Clear
                  </button>
                </div>
              </div>

              <ul className="max-h-[28rem] space-y-2 overflow-y-auto pr-1">
                {items.map((item) => (
                  <li
                    key={item.id}
                    className="relative rounded-2xl border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
                  >
                    <div className="flex gap-3">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={item.previewURL}
                        alt=""
                        className="h-14 w-14 shrink-0 rounded-xl object-cover"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0">
                            <p className="truncate text-sm font-semibold text-slate-900 dark:text-white">
                              {item.file.name}
                            </p>
                            <p className="text-xs text-slate-500 dark:text-slate-400">
                              {formatBytes(item.file.size)} ·{" "}
                              {statusLabel(item.status)}
                              {item.overrides ? " · custom setting" : ""}
                            </p>
                          </div>
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              if (menuId === item.id) {
                                closeMenu();
                                return;
                              }
                              const rect =
                                e.currentTarget.getBoundingClientRect();
                              const menuWidth = 208;
                              const hasOriginalDl =
                                item.status === "ready" &&
                                Boolean(
                                  item.result?.variants?.some(
                                    (v) =>
                                      v.preset_name.toLowerCase() ===
                                      "original",
                                  ),
                                );
                              const menuHeight =
                                116 +
                                (item.overrides ? 32 : 0) +
                                (hasOriginalDl ? 36 : 0);
                              const left = Math.min(
                                Math.max(8, rect.right - menuWidth),
                                window.innerWidth - menuWidth - 8,
                              );
                              const below = rect.bottom + 4;
                              const top =
                                below + menuHeight > window.innerHeight - 8
                                  ? Math.max(8, rect.top - menuHeight - 4)
                                  : below;
                              setMenuPos({ top, left });
                              setMenuId(item.id);
                            }}
                            className="inline-flex shrink-0 cursor-pointer rounded-lg p-1.5 text-slate-500 hover:bg-slate-100 hover:text-slate-800 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
                            aria-label="File options"
                            aria-expanded={menuId === item.id}
                          >
                            <MoreVertical className="h-4 w-4" />
                          </button>
                        </div>

                        <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-200/80">
                          <div
                            className={`h-full rounded-full transition-all ${
                              item.status === "failed"
                                ? "bg-rose-500"
                                : item.status === "ready"
                                  ? "bg-emerald-500"
                                  : "bg-violet-500"
                            }`}
                            style={{
                              width: `${Math.max(
                                item.progress,
                                item.status === "queued" ? 3 : 0,
                              )}%`,
                            }}
                          />
                        </div>

                        {item.error && (
                          <p className="mt-1.5 text-xs text-rose-600">
                            {item.error}
                          </p>
                        )}

                        {item.status === "ready" &&
                          (item.result?.variants?.length ?? 0) > 0 && (
                            <div className="mt-2 flex flex-wrap gap-1.5">
                              {[...item.result!.variants!]
                                .sort((a, b) => {
                                  if (a.preset_name === "original") return -1;
                                  if (b.preset_name === "original") return 1;
                                  return a.preset_name.localeCompare(
                                    b.preset_name,
                                  );
                                })
                                .map((v) => {
                                const srcW = item.result?.width || v.width;
                                const srcH = item.result?.height || v.height;
                                const isOriginal =
                                  v.preset_name.toLowerCase() === "original";
                                const realName = mediaFileName({
                                  originalFilename: item.file.name,
                                  kind: "webp",
                                  width: v.width,
                                  height: v.height,
                                  sourceWidth: srcW,
                                  sourceHeight: srcH,
                                });
                                return (
                                  <button
                                    key={v.id || v.preset_name}
                                    type="button"
                                    title={realName}
                                    onClick={() =>
                                      downloadVariant(
                                        v,
                                        item.file.name,
                                        srcW,
                                        srcH,
                                      )
                                    }
                                    className={`inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold text-white ${
                                      isOriginal
                                        ? "bg-violet-600 ring-2 ring-violet-300 ring-offset-1 hover:bg-violet-500 dark:ring-violet-700 dark:ring-offset-slate-900"
                                        : "bg-slate-900 hover:bg-slate-800 dark:bg-slate-800 dark:hover:bg-slate-700"
                                    }`}
                                  >
                                    <Download className="h-3 w-3" />
                                    {mediaDisplayLabel("webp", v.preset_name)}{" "}
                                    {formatBytes(v.size_bytes)}
                                  </button>
                                );
                              })}
                            </div>
                          )}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>

              {/* Primary action — bottom of list; convert ↔ download on one button */}
              <div className="mt-4 rounded-2xl border border-violet-200 bg-violet-50/70 p-3 sm:p-4 dark:border-violet-900/60 dark:bg-violet-950/40">
                <button
                  type="button"
                  disabled={
                    converting ||
                    (!allConverted && queuedCount === 0) ||
                    (allConverted && readyItems.length === 0)
                  }
                  onClick={() => {
                    if (allConverted) {
                      downloadAll();
                    } else {
                      void startConversion();
                    }
                  }}
                  className="inline-flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-violet-600 px-4 py-3.5 text-sm font-semibold text-white shadow-sm hover:bg-violet-500 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {converting ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Converting…
                    </>
                  ) : allConverted ? (
                    <>
                      <Download className="h-4 w-4" />
                      Download All ({readyItems.length})
                    </>
                  ) : (
                    <>
                      Start Conversion
                      {queuedCount > 0 ? ` (${queuedCount})` : ""}
                    </>
                  )}
                </button>
              </div>
            </div>
          )}

          {batchNote && (
            <p className="border-t border-amber-100 bg-amber-50 px-5 py-3 text-sm text-amber-800">
              {batchNote}
            </p>
          )}
        </div>
      </section>

      {/* Fixed ⋯ menu */}
      {menuItem && menuPos && (
        <div
          className="fixed z-[60] w-52 overflow-hidden rounded-xl border border-slate-200 bg-white py-1 text-sm shadow-xl dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
          style={{ top: menuPos.top, left: menuPos.left }}
          onClick={(e) => e.stopPropagation()}
          role="menu"
        >
          <button
            type="button"
            role="menuitem"
            disabled={
              converting || !canConvertItem(menuItem, globalSettings)
            }
            title={
              menuItem.status === "ready" &&
              !canConvertItem(menuItem, globalSettings)
                ? "Already converted with current settings — change Setting to convert again"
                : undefined
            }
            className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-slate-800"
            onClick={() => {
              if (!canConvertItem(menuItem, globalSettings)) {
                return;
              }
              const id = menuItem.id;
              closeMenu();
              void startConversion([id]);
            }}
          >
            <RefreshCw className="h-3.5 w-3.5 text-violet-600" />
            Convert
          </button>
          <button
            type="button"
            role="menuitem"
            className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
            onClick={() => {
              setEditId(menuItem.id);
              closeMenu();
            }}
          >
            <Settings2 className="h-3.5 w-3.5 text-slate-500 dark:text-slate-400" />
            Setting
          </button>
          {(() => {
            const originalWebp = menuItem.result?.variants?.find(
              (v) => v.preset_name.toLowerCase() === "original",
            );
            if (!originalWebp || menuItem.status !== "ready") {
              return null;
            }
            const srcW = menuItem.result?.width || originalWebp.width;
            const srcH = menuItem.result?.height || originalWebp.height;
            return (
              <button
                type="button"
                role="menuitem"
                className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
                onClick={() => {
                  downloadVariant(
                    originalWebp,
                    menuItem.file.name,
                    srcW,
                    srcH,
                  );
                  closeMenu();
                }}
              >
                <Download className="h-3.5 w-3.5 text-violet-600" />
                Download Original WebP
              </button>
            );
          })()}
          {menuItem.overrides ? (
            <button
              type="button"
              role="menuitem"
              className="flex w-full cursor-pointer px-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
              onClick={() => {
                updateItem(menuItem.id, { overrides: null });
                closeMenu();
              }}
            >
              Use global settings
            </button>
          ) : null}
          <button
            type="button"
            role="menuitem"
            className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/40"
            onClick={() => {
              removeItem(menuItem.id);
              closeMenu();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
            Remove
          </button>
        </div>
      )}

      <PhotosSheet
        open={showGlobalSettings}
        onClose={() => setShowGlobalSettings(false)}
        title="Global settings"
        subtitle="Applies to all files unless a file has its own setting."
        labelledBy="global-settings-title"
        footer={
          <button
            type="button"
            onClick={() => setShowGlobalSettings(false)}
            className="w-full cursor-pointer rounded-xl bg-violet-600 px-3 py-2.5 text-sm font-semibold text-white hover:bg-violet-500"
          >
            Done
          </button>
        }
      >
        <SettingsFields value={globalSettings} onChange={setGlobalSettings} />
      </PhotosSheet>

      <PhotosSheet
        open={Boolean(editingItem)}
        onClose={() => setEditId(null)}
        title="Setting"
        subtitle={editingItem?.file.name}
        labelledBy="file-setting-title"
        footer={
          editingItem ? (
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => {
                  updateItem(editingItem.id, { overrides: null });
                  setEditId(null);
                }}
                className="flex-1 cursor-pointer rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                Reset to global
              </button>
              <button
                type="button"
                onClick={() => setEditId(null)}
                className="flex-1 cursor-pointer rounded-xl bg-violet-600 px-3 py-2.5 text-sm font-semibold text-white hover:bg-violet-500"
              >
                Done
              </button>
            </div>
          ) : null
        }
      >
        {editingItem ? (
          <SettingsFields
            compact
            value={mergeSettings(globalSettings, editingItem.overrides ?? {})}
            onChange={(next) =>
              updateItem(editingItem.id, { overrides: next })
            }
          />
        ) : null}
      </PhotosSheet>
    </main>
  );
}
