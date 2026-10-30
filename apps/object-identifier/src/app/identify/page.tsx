"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  AlertTriangle,
  ImagePlus,
  Info,
  LayoutDashboard,
  Loader2,
  MoreVertical,
  ScanSearch,
  Trash2,
} from "lucide-react";
import type { IdentifyResult } from "@/components/ResultDisplay";
import {
  formatBytes,
  uploadImage,
  type ImageUploadData,
} from "@/lib/image-api";
import { IdentifyError, identifyImage } from "@/lib/identify";
import {
  IDENTIFY_MAX_FILE_BYTES,
  IDENTIFY_MAX_QUEUE_FILES,
  IDENTIFY_RATE_LIMIT,
  IDENTIFY_RATE_WINDOW_SECONDS,
  formatIdentifyLimitLabel,
  isIdentifyAllowedType,
} from "@/lib/identify-limits";

type QueueStatus =
  | "queued"
  | "uploading"
  | "analyzing"
  | "ready"
  | "failed"
  | "cancelled";

type QueueItem = {
  id: string;
  file: File;
  previewURL: string;
  status: QueueStatus;
  progress: number;
  error?: string;
  result?: IdentifyResult;
  upload?: ImageUploadData;
  librarySaved?: boolean;
  deduplicated?: boolean;
  libraryNote?: string;
};

type LimitNotice = {
  kind: "rate" | "quota" | "upload" | "info";
  message: string;
  retryAfterSeconds?: number | null;
};

function statusLabel(status: QueueStatus): string {
  switch (status) {
    case "queued":
      return "Ready";
    case "uploading":
      return "Saving to library…";
    case "analyzing":
      return "Analyzing…";
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

function maxFileLabel(): string {
  return `${IDENTIFY_MAX_FILE_BYTES / (1024 * 1024)}MB`;
}

export default function IdentifyPage() {
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const itemsRef = useRef<QueueItem[]>([]);
  const [items, setItems] = useState<QueueItem[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(
    null,
  );
  const [batchNote, setBatchNote] = useState<string | null>(null);
  const [limitNotice, setLimitNotice] = useState<LimitNotice | null>(null);
  const [retryLeft, setRetryLeft] = useState<number | null>(null);

  useEffect(() => {
    itemsRef.current = items;
  }, [items]);

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

  useEffect(() => {
    if (
      !limitNotice ||
      (limitNotice.kind !== "rate" && limitNotice.kind !== "quota") ||
      !limitNotice.retryAfterSeconds ||
      limitNotice.retryAfterSeconds <= 0
    ) {
      const clear = window.setTimeout(() => setRetryLeft(null), 0);
      return () => window.clearTimeout(clear);
    }
    const total = limitNotice.retryAfterSeconds;
    const started = Date.now();
    const kickoff = window.setTimeout(() => setRetryLeft(total), 0);
    const timer = window.setInterval(() => {
      const left = Math.max(
        0,
        total - Math.floor((Date.now() - started) / 1000),
      );
      setRetryLeft(left);
      if (left <= 0) {
        window.clearInterval(timer);
        setLimitNotice(null);
      }
    }, 250);
    return () => {
      window.clearTimeout(kickoff);
      window.clearInterval(timer);
    };
  }, [limitNotice]);

  const menuItem = menuId ? items.find((i) => i.id === menuId) : undefined;

  const addFiles = useCallback((fileList: FileList | File[]) => {
    const incoming = Array.from(fileList);
    const prev = itemsRef.current;
    const room = Math.max(0, IDENTIFY_MAX_QUEUE_FILES - prev.length);
    let skippedType = 0;
    let skippedSize = 0;
    let skippedCap = 0;
    const next: QueueItem[] = [];

    for (const file of incoming) {
      if (!isIdentifyAllowedType(file.type)) {
        skippedType += 1;
        continue;
      }
      if (file.size > IDENTIFY_MAX_FILE_BYTES) {
        skippedSize += 1;
        continue;
      }
      if (next.length >= room) {
        skippedCap += 1;
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

    setBatchNote(null);
    if (next.length === 0) {
      const parts: string[] = [];
      if (skippedSize > 0) {
        parts.push(`${skippedSize} over the ${maxFileLabel()} max upload size`);
      }
      if (skippedType > 0) {
        parts.push(`${skippedType} unsupported type`);
      }
      if (skippedCap > 0 || room === 0) {
        parts.push(`queue is full (max ${IDENTIFY_MAX_QUEUE_FILES} files)`);
      }
      setLimitNotice({
        kind: "upload",
        message:
          parts.length > 0
            ? `Could not add files: ${parts.join("; ")}.`
            : `Use JPEG, PNG, or WebP under ${maxFileLabel()}.`,
      });
      return;
    }

    const notes: string[] = [];
    if (skippedSize > 0) {
      notes.push(`${skippedSize} skipped (>${maxFileLabel()})`);
    }
    if (skippedType > 0) {
      notes.push(`${skippedType} skipped (type)`);
    }
    if (skippedCap > 0) {
      notes.push(
        `${skippedCap} skipped (max ${IDENTIFY_MAX_QUEUE_FILES} files)`,
      );
    }
    setLimitNotice(
      notes.length > 0
        ? {
            kind: "upload",
            message: `Added ${next.length}. ${notes.join(" · ")}.`,
          }
        : null,
    );
    setItems([...prev, ...next]);
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
  };

  const clearAll = () => {
    abortRef.current?.abort();
    setAnalyzing(false);
    setItems((prev) => {
      prev.forEach((i) => URL.revokeObjectURL(i.previewURL));
      return [];
    });
    setMenuId(null);
    setBatchNote(null);
    setLimitNotice(null);
  };

  const updateItem = (id: string, patch: Partial<QueueItem>) => {
    setItems((prev) =>
      prev.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    );
  };

  const readyItems = items.filter((i) => i.status === "ready" && i.result);
  const queuedCount = items.filter(
    (i) => i.status === "queued" || i.status === "failed",
  ).length;
  const allDone =
    items.length > 0 && queuedCount === 0 && readyItems.length > 0;

  const runIdentification = async (targets: QueueItem[]) => {
    if (targets.length === 0) {
      return;
    }

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setAnalyzing(true);
    setBatchNote(null);
    setLimitNotice(null);

    for (let index = 0; index < targets.length; index += 1) {
      const item = targets[index]!;
      if (controller.signal.aborted) {
        break;
      }
      updateItem(item.id, {
        status: "uploading",
        progress: 12,
        error: undefined,
        result: undefined,
        upload: undefined,
        librarySaved: false,
        deduplicated: false,
        libraryNote: undefined,
      });
      let progressTimer: number | undefined;
      try {
        progressTimer = window.setInterval(() => {
          setItems((prev) =>
            prev.map((row) => {
              if (
                row.id !== item.id ||
                (row.status !== "analyzing" && row.status !== "uploading")
              ) {
                return row;
              }
              return {
                ...row,
                progress: Math.min(88, row.progress + 6),
              };
            }),
          );
        }, 280);

        // Persist to Image API first so the gallery + fingerprint dedupe work.
        const uploaded = await uploadImage(item.file, {
          presets: ["original"],
        });
        if (controller.signal.aborted) {
          if (progressTimer !== undefined) {
            window.clearInterval(progressTimer);
          }
          updateItem(item.id, { status: "cancelled", progress: 0 });
          break;
        }

        let librarySaved = false;
        let deduplicated = false;
        let libraryNote: string | undefined;
        let imageId: string | undefined;
        let fingerprint: string | undefined;

        if (uploaded.success && !uploaded.mocked) {
          librarySaved = true;
          deduplicated = Boolean(uploaded.data.deduplicated);
          imageId = uploaded.data.id;
          fingerprint = uploaded.data.fingerprint;
          updateItem(item.id, {
            status: "analyzing",
            progress: Math.max(36, item.progress),
            upload: uploaded.data,
            librarySaved: true,
            deduplicated,
            libraryNote: deduplicated
              ? "Deduplication hit — existing library image reused"
              : "Saved to your gallery",
          });
        } else {
          libraryNote =
            "Image API unavailable — identified locally, not saved to gallery";
          updateItem(item.id, {
            status: "analyzing",
            progress: 36,
            librarySaved: false,
            libraryNote,
          });
          setLimitNotice({
            kind: "info",
            message:
              "Could not reach the Image API. AI identify still runs, but photos will not appear in the dashboard until the API is up.",
          });
        }

        const result = await identifyImage(item.file, {
          imageId,
          fingerprint,
        });
        if (progressTimer !== undefined) {
          window.clearInterval(progressTimer);
        }

        if (controller.signal.aborted) {
          updateItem(item.id, { status: "cancelled", progress: 0 });
          break;
        }
        updateItem(item.id, {
          status: "ready",
          progress: 100,
          result,
          librarySaved,
          deduplicated,
          libraryNote,
          upload:
            uploaded.success && !uploaded.mocked ? uploaded.data : undefined,
        });
      } catch (err) {
        if (progressTimer !== undefined) {
          window.clearInterval(progressTimer);
        }
        if (controller.signal.aborted) {
          updateItem(item.id, { status: "cancelled", progress: 0 });
          break;
        }
        const identifyErr =
          err instanceof IdentifyError
            ? err
            : new IdentifyError({
                message:
                  err instanceof Error
                    ? err.message
                    : "Failed to identify object",
              });

        updateItem(item.id, {
          status: "failed",
          progress: 100,
          error: identifyErr.message,
        });

        if (identifyErr.isRateLimited) {
          const retry =
            identifyErr.retryAfterSeconds ?? IDENTIFY_RATE_WINDOW_SECONDS;
          setLimitNotice({
            kind:
              identifyErr.code === "QUOTA_EXCEEDED" ? "quota" : "rate",
            message:
              identifyErr.code === "QUOTA_EXCEEDED"
                ? "AI quota or provider rate limit reached. Remaining files were left queued."
                : `Rate limit hit (${IDENTIFY_RATE_LIMIT}/${IDENTIFY_RATE_WINDOW_SECONDS}s). Remaining files were left queued.`,
            retryAfterSeconds: retry,
          });
          // Leave the rest of this batch queued for a later retry.
          break;
        }

        if (
          identifyErr.code === "FILE_TOO_LARGE" ||
          identifyErr.code === "INVALID_TYPE"
        ) {
          setLimitNotice({
            kind: "upload",
            message: identifyErr.message,
          });
        }
      }
    }

    setAnalyzing(false);
  };

  const startIdentification = (onlyIds?: string[]) => {
    const pending = items.filter((i) => {
      if (onlyIds && !onlyIds.includes(i.id)) {
        return false;
      }
      return i.status === "queued" || i.status === "failed";
    });
    void runIdentification(pending);
  };

  const identifyAgain = () => {
    const reset = items.map((row) => ({
      ...row,
      status: "queued" as const,
      progress: 0,
      error: undefined,
      result: undefined,
      upload: undefined,
      librarySaved: false,
      deduplicated: false,
      libraryNote: undefined,
    }));
    setItems(reset);
    void runIdentification(reset);
  };

  return (
    <main className="min-h-[calc(100vh-4rem)] bg-white text-slate-900 dark:bg-[#0b1220] dark:text-slate-100">
      <section className="mx-auto max-w-5xl px-4 pb-6 pt-10 text-center sm:pt-12">
        <p className="text-xs font-semibold uppercase tracking-wider text-violet-600 dark:text-violet-400">
          Tools · Object Identifier
        </p>
        <h1 className="mt-2 font-[family-name:var(--font-display)] text-3xl tracking-tight text-slate-900 sm:text-5xl dark:text-white">
          Object Identifier
        </h1>
        <p className="mx-auto mt-3 max-w-2xl text-sm text-slate-500 sm:text-base dark:text-slate-400">
          Upload one or many photos — AI names the subject, confidence, and
          useful next steps for each image.
        </p>
        <div className="mx-auto mt-5 flex max-w-2xl items-start gap-2 rounded-2xl border border-slate-200 bg-slate-50 px-3.5 py-3 text-left text-xs leading-relaxed text-slate-600 dark:border-slate-700 dark:bg-slate-900/70 dark:text-slate-300">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-violet-600 dark:text-violet-400" />
          <p>
            Each run <strong>saves to your gallery</strong> (with fingerprint
            dedupe), then runs AI identify. Limits: max{" "}
            <strong>{maxFileLabel()}</strong> per image,{" "}
            <strong>{IDENTIFY_MAX_QUEUE_FILES}</strong> files,{" "}
            <strong>
              {IDENTIFY_RATE_LIMIT} identifies / {IDENTIFY_RATE_WINDOW_SECONDS}s
            </strong>
            . Sign in to keep guest uploads after claim.
          </p>
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-16">
        {limitNotice ? (
          <div
            className={`mb-4 flex items-start gap-3 rounded-2xl border px-4 py-3 text-sm ${
              limitNotice.kind === "rate" ||
              limitNotice.kind === "quota" ||
              limitNotice.kind === "info"
                ? "border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-100"
                : "border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100"
            }`}
            role="status"
          >
            {limitNotice.kind === "info" ? (
              <Info className="mt-0.5 h-4 w-4 shrink-0" />
            ) : (
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            )}
            <div className="min-w-0 flex-1">
              <p className="font-semibold">
                {limitNotice.kind === "rate"
                  ? "Rate limit"
                  : limitNotice.kind === "quota"
                    ? "AI quota"
                    : limitNotice.kind === "info"
                      ? "Library"
                      : "Upload limit"}
              </p>
              <p className="mt-0.5 text-sm opacity-90">{limitNotice.message}</p>
              {retryLeft !== null && retryLeft > 0 ? (
                <p className="mt-1 text-xs font-medium tabular-nums opacity-80">
                  Retry available in {retryLeft}s
                </p>
              ) : null}
            </div>
            <button
              type="button"
              onClick={() => setLimitNotice(null)}
              className="cursor-pointer rounded-lg px-2 py-1 text-xs font-semibold opacity-70 hover:bg-black/5 hover:opacity-100 dark:hover:bg-white/10"
            >
              Dismiss
            </button>
          </div>
        ) : null}

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
          <div
            role="button"
            tabIndex={0}
            onClick={() => {
              if (items.length >= IDENTIFY_MAX_QUEUE_FILES) {
                setLimitNotice({
                  kind: "upload",
                  message: `Queue is full. Max ${IDENTIFY_MAX_QUEUE_FILES} files — remove some to add more.`,
                });
                return;
              }
              inputRef.current?.click();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                if (items.length >= IDENTIFY_MAX_QUEUE_FILES) {
                  setLimitNotice({
                    kind: "upload",
                    message: `Queue is full. Max ${IDENTIFY_MAX_QUEUE_FILES} files — remove some to add more.`,
                  });
                  return;
                }
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
            <p className="mt-1 max-w-md text-center text-xs text-slate-400 dark:text-slate-500">
              {formatIdentifyLimitLabel()}
            </p>
          </div>

          {items.length > 0 && (
            <div className="border-t border-slate-200 px-5 pb-5 pt-4 dark:border-slate-800">
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-semibold text-slate-800 dark:text-slate-100">
                  {items.length} file{items.length === 1 ? "" : "s"}
                </p>
                <button
                  type="button"
                  onClick={clearAll}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Clear
                </button>
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
                              {item.result
                                ? ` · ${item.result.object_name}`
                                : ""}
                            </p>
                            {item.libraryNote ? (
                              <p
                                className={`mt-1 text-[11px] font-medium ${
                                  item.deduplicated
                                    ? "text-emerald-700 dark:text-emerald-300"
                                    : item.librarySaved
                                      ? "text-violet-700 dark:text-violet-300"
                                      : "text-amber-700 dark:text-amber-300"
                                }`}
                              >
                                {item.libraryNote}
                              </p>
                            ) : null}
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
                              const menuHeight = 88;
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

                        <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-200/80 dark:bg-slate-800">
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
                          <p className="mt-1.5 text-xs text-rose-600 dark:text-rose-400">
                            {item.error}
                          </p>
                        )}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>

              <div className="mt-4 rounded-2xl border border-violet-200 bg-violet-50/70 p-3 sm:p-4 dark:border-violet-900/60 dark:bg-violet-950/40">
                <button
                  type="button"
                  disabled={
                    analyzing ||
                    (queuedCount === 0 && !allDone) ||
                    (retryLeft !== null && retryLeft > 0)
                  }
                  onClick={() => {
                    if (allDone && queuedCount === 0) {
                      identifyAgain();
                      return;
                    }
                    startIdentification();
                  }}
                  className="inline-flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-violet-600 px-4 py-3.5 text-sm font-semibold text-white shadow-sm hover:bg-violet-500 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {analyzing ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Saving & identifying…
                    </>
                  ) : retryLeft !== null && retryLeft > 0 ? (
                    <>
                      <AlertTriangle className="h-4 w-4" />
                      Wait {retryLeft}s (rate limited)
                    </>
                  ) : (
                    <>
                      <ScanSearch className="h-4 w-4" />
                      {allDone && queuedCount === 0
                        ? "Identify again"
                        : `Start Identification${
                            queuedCount > 0 ? ` (${queuedCount})` : ""
                          }`}
                    </>
                  )}
                </button>
                {batchNote && (
                  <p className="mt-2 text-center text-xs text-amber-800 dark:text-amber-200">
                    {batchNote}
                  </p>
                )}
              </div>
            </div>
          )}
        </div>

        {readyItems.length > 0 && (
          <div className="mt-10">
            <div className="mb-4 flex flex-wrap items-end justify-between gap-2">
              <div>
                <p className="text-xs font-semibold uppercase tracking-wider text-violet-600 dark:text-violet-400">
                  Results
                </p>
                <h2 className="mt-1 font-[family-name:var(--font-display)] text-2xl tracking-tight text-slate-900 dark:text-white">
                  {readyItems.length} identified
                </h2>
                <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                  {
                    readyItems.filter((i) => i.librarySaved).length
                  }{" "}
                  saved to gallery
                  {readyItems.some((i) => i.deduplicated)
                    ? ` · ${readyItems.filter((i) => i.deduplicated).length} dedupe hit${
                        readyItems.filter((i) => i.deduplicated).length === 1
                          ? ""
                          : "s"
                      }`
                    : ""}
                </p>
              </div>
              <Link
                href="/dashboard"
                className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-semibold text-slate-800 shadow-sm hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800"
              >
                <LayoutDashboard className="h-3.5 w-3.5" />
                Open gallery
              </Link>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              {readyItems.map((item) => {
                const result = item.result!;
                const confidence = Math.round(
                  Math.min(Math.max(result.confidence_score, 0), 1) * 100,
                );
                return (
                  <article
                    key={item.id}
                    className="overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-950"
                  >
                    <div className="relative aspect-[16/10] bg-slate-100 dark:bg-slate-900">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={item.previewURL}
                        alt=""
                        className="h-full w-full object-cover"
                      />
                      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 via-black/30 to-transparent px-4 pb-3 pt-10">
                        <p className="truncate text-lg font-semibold text-white">
                          {result.object_name}
                        </p>
                        <p className="text-xs font-medium text-white/80">
                          {confidence}% confidence
                          {result.cache_hit
                            ? ` · cache ${result.cache_source || "hit"}`
                            : result.cache_source === "live"
                              ? " · live Gemini"
                              : ""}
                        </p>
                      </div>
                    </div>
                    <div className="space-y-4 p-4 sm:p-5">
                      <div className="flex flex-wrap gap-1.5">
                        {item.deduplicated ? (
                          <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[11px] font-semibold text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-200">
                            Deduplication hit
                          </span>
                        ) : null}
                        {item.librarySaved ? (
                          <span className="rounded-full border border-violet-200 bg-violet-50 px-2.5 py-1 text-[11px] font-semibold text-violet-800 dark:border-violet-800 dark:bg-violet-950/50 dark:text-violet-200">
                            In gallery
                          </span>
                        ) : (
                          <span className="rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 text-[11px] font-semibold text-amber-900 dark:border-amber-800 dark:bg-amber-950/50 dark:text-amber-100">
                            Not saved
                          </span>
                        )}
                        {result.cache_hit ? (
                          <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 text-[11px] font-semibold text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
                            AI cache
                          </span>
                        ) : null}
                      </div>
                      <p className="text-sm leading-relaxed text-slate-600 dark:text-slate-300">
                        {result.summary}
                      </p>
                      {result.key_details?.length > 0 && (
                        <div>
                          <p className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
                            Key details
                          </p>
                          <div className="flex flex-wrap gap-1.5">
                            {result.key_details.map((detail) => (
                              <span
                                key={detail}
                                className="rounded-full border border-violet-200 bg-violet-50 px-2.5 py-1 text-xs font-medium text-violet-800 dark:border-violet-800 dark:bg-violet-950/50 dark:text-violet-200"
                              >
                                {detail}
                              </span>
                            ))}
                          </div>
                        </div>
                      )}
                      {result.suggested_actions?.length > 0 && (
                        <div>
                          <p className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
                            Suggested actions
                          </p>
                          <ul className="space-y-1.5 text-sm text-slate-700 dark:text-slate-200">
                            {result.suggested_actions.map((action) => (
                              <li
                                key={action}
                                className="flex gap-2 leading-snug"
                              >
                                <span className="mt-2 h-1 w-1 shrink-0 rounded-full bg-violet-500" />
                                <span>{action}</span>
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                      <p className="truncate text-[11px] text-slate-400 dark:text-slate-500">
                        {item.file.name}
                      </p>
                    </div>
                  </article>
                );
              })}
            </div>
          </div>
        )}
      </section>

      {menuItem && menuPos ? (
        <div
          className="fixed z-50 w-52 overflow-hidden rounded-xl border border-slate-200 bg-white py-1 shadow-xl dark:border-slate-700 dark:bg-slate-900"
          style={{ top: menuPos.top, left: menuPos.left }}
          onClick={(e) => e.stopPropagation()}
        >
          {(menuItem.status === "queued" || menuItem.status === "failed") && (
            <button
              type="button"
              onClick={() => {
                closeMenu();
                startIdentification([menuItem.id]);
              }}
              className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-sm font-medium text-slate-800 hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
            >
              <ScanSearch className="h-3.5 w-3.5" />
              Identify
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              removeItem(menuItem.id);
              closeMenu();
            }}
            className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-sm font-medium text-rose-700 hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-950/40"
          >
            <Trash2 className="h-3.5 w-3.5" />
            Remove
          </button>
        </div>
      ) : null}
    </main>
  );
}
