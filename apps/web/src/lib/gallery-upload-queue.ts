import {
  listAlbums,
  listGalleryMedia,
  uploadGalleryFile,
  type Album,
  type ChunkedUploadProgress,
  type UploadCompleteResult,
} from "@/lib/gallery-api";
import {
  invalidateGalleryCache,
  writeGalleryCache,
  type LibraryCacheKind,
} from "@/lib/gallery-cache";
import type { UserImagesPayload } from "@/lib/user-api";

export type LibraryRefreshDetail = UserImagesPayload & {
  albumId?: string | null;
  albumPayload?: UserImagesPayload | null;
  albums?: Album[];
  /** Monotonic id so the UI can ignore out-of-order refresh responses. */
  generation: number;
};

const FILE_CONCURRENCY = 3;
const CACHE_KIND: LibraryCacheKind = "gallery";
const PAGE_SIZE = 36;

export type GalleryUploadJob = {
  id: string;
  name: string;
  size: number;
  percent: number;
  bytesLoaded: number;
  bytesTotal: number;
  status: "queued" | "uploading" | "done" | "error";
  error?: string;
  albumId?: string | null;
  resultId?: string;
};

type QueueSnapshot = {
  jobs: GalleryUploadJob[];
  activeCount: number;
  uploading: boolean;
};

type RefreshHandler = (payload: LibraryRefreshDetail) => void;

/**
 * Process-wide gallery upload queue. Survives Photos/Albums/Trash tab switches
 * and /dashboard navigations so in-flight chunked uploads keep running.
 */
class GalleryUploadQueue extends EventTarget {
  private jobs: GalleryUploadJob[] = [];
  private pending: {
    jobId: string;
    file: File;
    albumId?: string | null;
  }[] = [];
  private running = 0;
  private userId: string | null = null;
  private onLibraryRefresh: RefreshHandler | null = null;
  private controllers = new Map<string, AbortController>();
  /** Drop stale list responses when parallel uploads refresh concurrently. */
  private refreshGeneration = 0;
  private lastAlbumId: string | null | undefined = undefined;
  private trailingRefreshTimer: ReturnType<typeof setTimeout> | null = null;

  configure(opts: {
    userId: string | null;
    onLibraryRefresh?: RefreshHandler | null;
  }) {
    this.userId = opts.userId;
    if (opts.onLibraryRefresh !== undefined) {
      this.onLibraryRefresh = opts.onLibraryRefresh;
    }
  }

  snapshot(): QueueSnapshot {
    const activeCount = this.jobs.filter(
      (j) => j.status === "queued" || j.status === "uploading",
    ).length;
    return {
      jobs: this.jobs.map((j) => ({ ...j })),
      activeCount,
      uploading: activeCount > 0,
    };
  }

  enqueue(files: File[], albumId?: string | null): string[] {
    const ids: string[] = [];
    for (const file of files) {
      const id = `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2, 9)}`;
      ids.push(id);
      const job: GalleryUploadJob = {
        id,
        name: file.name,
        size: file.size,
        percent: 0,
        bytesLoaded: 0,
        bytesTotal: file.size,
        status: "queued",
        albumId: albumId ?? null,
      };
      this.jobs = [job, ...this.jobs].slice(0, 60);
      this.pending.push({ jobId: id, file, albumId });
    }
    this.emitChange();
    this.pump();
    return ids;
  }

  private pump() {
    while (this.running < FILE_CONCURRENCY && this.pending.length > 0) {
      const next = this.pending.shift();
      if (!next) {
        break;
      }
      this.running += 1;
      void this.runJob(next).finally(() => {
        this.running -= 1;
        this.pump();
        this.emitChange();
        if (this.running === 0 && this.pending.length === 0) {
          // Final authoritative refresh after the whole batch settles.
          this.queueRefresh(this.lastAlbumId, true);
        }
      });
    }
  }

  private patchJob(id: string, patch: Partial<GalleryUploadJob>) {
    this.jobs = this.jobs.map((j) => (j.id === id ? { ...j, ...patch } : j));
    this.emitChange();
  }

  private emitChange() {
    this.dispatchEvent(new Event("change"));
  }

  private async runJob(entry: {
    jobId: string;
    file: File;
    albumId?: string | null;
  }) {
    const controller = new AbortController();
    this.controllers.set(entry.jobId, controller);
    this.patchJob(entry.jobId, {
      status: "uploading",
      percent: 0,
      bytesLoaded: 0,
    });

    try {
      const result = await uploadGalleryFile(entry.file, {
        albumId: entry.albumId,
        signal: controller.signal,
        onProgress: (p: ChunkedUploadProgress) => {
          this.patchJob(entry.jobId, {
            status: "uploading",
            percent: p.percent,
            bytesLoaded: p.bytesLoaded,
            bytesTotal: p.bytesTotal,
          });
        },
      });
      this.patchJob(entry.jobId, {
        status: "done",
        percent: 100,
        bytesLoaded: entry.file.size,
        bytesTotal: entry.file.size,
        resultId: result.id,
      });
      this.dispatchEvent(
        new CustomEvent<UploadCompleteResult>("filecomplete", {
          detail: result,
        }),
      );
      this.queueRefresh(entry.albumId, false);
    } catch (err) {
      this.patchJob(entry.jobId, {
        status: "error",
        error: err instanceof Error ? err.message : "Upload failed",
      });
      this.dispatchEvent(
        new CustomEvent("fileerror", {
          detail: {
            jobId: entry.jobId,
            message: err instanceof Error ? err.message : "Upload failed",
          },
        }),
      );
    } finally {
      this.controllers.delete(entry.jobId);
    }
  }

  /** Coalesce parallel completes so an older list cannot overwrite a newer one. */
  private queueRefresh(albumId?: string | null, immediate = false) {
    if (albumId) {
      this.lastAlbumId = albumId;
    }
    if (this.trailingRefreshTimer) {
      clearTimeout(this.trailingRefreshTimer);
      this.trailingRefreshTimer = null;
    }
    if (immediate) {
      void this.refreshLibrary(this.lastAlbumId);
      return;
    }
    // Kick an immediate refresh for snappy UI, then a trailing one.
    void this.refreshLibrary(albumId);
    this.trailingRefreshTimer = setTimeout(() => {
      this.trailingRefreshTimer = null;
      void this.refreshLibrary(this.lastAlbumId);
    }, 350);
  }

  private async refreshLibrary(albumId?: string | null) {
    if (!this.userId) {
      return;
    }
    const generation = ++this.refreshGeneration;
    try {
      invalidateGalleryCache(this.userId, "all", CACHE_KIND);
      const [payload, albumPayload, albums] = await Promise.all([
        listGalleryMedia({
          limit: PAGE_SIZE,
          offset: 0,
          trash: false,
        }),
        albumId
          ? listGalleryMedia({
              limit: PAGE_SIZE,
              offset: 0,
              trash: false,
              albumId,
            })
          : Promise.resolve(null),
        listAlbums().catch(() => [] as Album[]),
      ]);
      // Ignore outdated responses (parallel uploads finishing out of order).
      if (generation !== this.refreshGeneration) {
        return;
      }
      writeGalleryCache({
        userId: this.userId,
        trash: false,
        kind: CACHE_KIND,
        items: payload.items,
        total: payload.pagination.total,
        nextOffset: payload.pagination.next_offset,
        hasMore: payload.pagination.has_more,
        savedAt: Date.now(),
      });
      const detail: LibraryRefreshDetail = {
        ...payload,
        albumId: albumId ?? null,
        albumPayload,
        albums,
        generation,
      };
      this.onLibraryRefresh?.(detail);
      this.dispatchEvent(
        new CustomEvent<LibraryRefreshDetail>("libraryrefresh", {
          detail,
        }),
      );
      this.dispatchEvent(
        new CustomEvent<Album[]>("albumsrefresh", { detail: albums }),
      );
    } catch {
      // UI can soft-retry on next focus
    }
  }
}

export const galleryUploadQueue = new GalleryUploadQueue();

export function filterGalleryUploadFiles(fileList: File[]): File[] {
  return fileList.filter((f) => {
    const type = (f.type || "").toLowerCase();
    return (
      type.startsWith("image/") ||
      type.startsWith("video/") ||
      /\.(jpe?g|png|gif|webp|heic|heif|mp4|m4v|webm|mov)$/i.test(f.name)
    );
  });
}
