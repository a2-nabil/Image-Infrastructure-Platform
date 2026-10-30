import { getAccessToken } from "@/lib/auth";
import {
  sameOriginProxyURL,
  type UserImageDetail,
  type UserImagesPayload,
} from "@/lib/user-api";

const DEFAULT_IMAGE_API_URL = "/api/v1";
/** Parallel S3 parts — higher = faster large uploads on good links. */
const CHUNK_CONCURRENCY = 6;

function apiBaseURL(): string {
  return (
    process.env.NEXT_PUBLIC_IMAGE_API_URL || DEFAULT_IMAGE_API_URL
  ).replace(/\/$/, "");
}

function authHeaders(extra?: Record<string, string>): HeadersInit {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(extra ?? {}),
  };
  const token = getAccessToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

async function parseJSON<T>(response: Response): Promise<T> {
  const payload = (await response.json().catch(() => null)) as {
    success?: boolean;
    data?: T;
    error?: { message?: string; code?: string };
  } | null;
  if (!response.ok || !payload?.success) {
    const fallback =
      response.status === 404
        ? "Gallery upload API not found. Restart the Image API so /users/me/uploads is available."
        : response.status === 401
          ? "Sign in required for gallery uploads."
          : `Request failed (${response.status})`;
    throw new Error(payload?.error?.message || fallback);
  }
  return payload.data as T;
}

export type Album = {
  id: string;
  user_id: string;
  title: string;
  description: string;
  cover_image_id?: string | null;
  item_count?: number;
  created_at: string;
  updated_at: string;
};

export type UploadInitResult = {
  upload_id: string;
  chunk_size: number;
  total_chunks: number;
  media_kind: "image" | "video";
  expires_at: string;
  received_chunks: number;
};

export type UploadCompleteResult = {
  id: string;
  status: string;
  media_kind: string;
  original_filename: string;
  mime_type: string;
  file_size_bytes: number;
  fingerprint: string;
};

function normalizeGalleryImage(item: UserImageDetail): UserImageDetail {
  return {
    ...item,
    original_url: sameOriginProxyURL(item.original_url),
    variants: (item.variants || []).map((v) => ({
      ...v,
      url: sameOriginProxyURL(v.url),
    })),
  };
}

/** Personal gallery media only — never Converter / Identify / Dashboard tools library. */
export async function listGalleryMedia(options?: {
  limit?: number;
  offset?: number;
  trash?: boolean;
  albumId?: string | null;
  mediaKind?: "image" | "video" | "";
}): Promise<UserImagesPayload> {
  const limit = options?.limit ?? 24;
  const offset = options?.offset ?? 0;
  const params = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  if (options?.trash) {
    params.set("trash", "1");
  }
  if (options?.albumId) {
    params.set("album_id", options.albumId);
  }
  if (options?.mediaKind) {
    params.set("media_kind", options.mediaKind);
  }
  const response = await fetch(
    `${apiBaseURL()}/users/me/gallery/media?${params.toString()}`,
    {
      headers: authHeaders(),
      credentials: "include",
    },
  );
  const data = await parseJSON<UserImagesPayload>(response);
  return {
    ...data,
    items: (data.items || []).map(normalizeGalleryImage),
    pagination: data.pagination ?? {
      limit,
      offset,
      next_offset: offset + (data.items?.length ?? 0),
      total: data.summary?.image_count ?? data.items?.length ?? 0,
      has_more: false,
    },
  };
}

export async function listAlbums(): Promise<Album[]> {
  const response = await fetch(`${apiBaseURL()}/users/me/albums`, {
    headers: authHeaders(),
    credentials: "include",
  });
  const data = await parseJSON<Album[]>(response);
  return data ?? [];
}

export async function createAlbum(
  title: string,
  description = "",
): Promise<Album> {
  const response = await fetch(`${apiBaseURL()}/users/me/albums`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ title, description }),
  });
  return parseJSON<Album>(response);
}

export async function deleteAlbum(albumId: string): Promise<void> {
  const response = await fetch(
    `${apiBaseURL()}/users/me/albums/${encodeURIComponent(albumId)}`,
    {
      method: "DELETE",
      headers: authHeaders(),
      credentials: "include",
    },
  );
  await parseJSON(response);
}

export async function addAlbumItems(
  albumId: string,
  ids: string[],
): Promise<void> {
  const response = await fetch(
    `${apiBaseURL()}/users/me/albums/${encodeURIComponent(albumId)}/items`,
    {
      method: "POST",
      headers: authHeaders({ "Content-Type": "application/json" }),
      credentials: "include",
      body: JSON.stringify({ ids }),
    },
  );
  await parseJSON(response);
}

async function initUpload(file: File, albumId?: string | null) {
  const response = await fetch(`${apiBaseURL()}/users/me/uploads`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({
      filename: file.name,
      content_type: file.type || "application/octet-stream",
      total_size: file.size,
      album_id: albumId || undefined,
    }),
  });
  return parseJSON<UploadInitResult>(response);
}

/**
 * PUT one chunk with XMLHttpRequest so upload.onprogress fires live byte events
 * (fetch() cannot report request-body upload progress).
 */
function putChunkWithProgress(
  uploadId: string,
  index: number,
  blob: Blob,
  onByteProgress: (loadedInChunk: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new Error("Upload cancelled"));
      return;
    }
    const xhr = new XMLHttpRequest();
    const url = `${apiBaseURL()}/users/me/uploads/${encodeURIComponent(uploadId)}/chunks/${index}`;
    xhr.open("PUT", url);
    xhr.withCredentials = true;
    const headers = authHeaders({
      "Content-Type": "application/octet-stream",
    }) as Record<string, string>;
    for (const [key, value] of Object.entries(headers)) {
      xhr.setRequestHeader(key, value);
    }

    const onAbort = () => {
      xhr.abort();
      reject(new Error("Upload cancelled"));
    };
    signal?.addEventListener("abort", onAbort, { once: true });

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onByteProgress(event.loaded);
      } else {
        onByteProgress(event.loaded);
      }
    };
    xhr.upload.onload = () => {
      onByteProgress(blob.size);
    };

    xhr.onerror = () => {
      signal?.removeEventListener("abort", onAbort);
      reject(new Error("Network error while uploading chunk"));
    };
    xhr.onabort = () => {
      signal?.removeEventListener("abort", onAbort);
      reject(new Error("Upload cancelled"));
    };
    xhr.onload = () => {
      signal?.removeEventListener("abort", onAbort);
      let payload: {
        success?: boolean;
        error?: { message?: string };
      } | null = null;
      try {
        payload = JSON.parse(xhr.responseText || "{}") as {
          success?: boolean;
          error?: { message?: string };
        };
      } catch {
        payload = null;
      }
      if (xhr.status >= 200 && xhr.status < 300 && payload?.success) {
        resolve();
        return;
      }
      reject(
        new Error(
          payload?.error?.message ||
            `Chunk upload failed (${xhr.status || "network"})`,
        ),
      );
    };

    xhr.send(blob);
  });
}

async function completeUpload(uploadId: string) {
  const response = await fetch(
    `${apiBaseURL()}/users/me/uploads/${encodeURIComponent(uploadId)}/complete`,
    {
      method: "POST",
      headers: authHeaders(),
      credentials: "include",
    },
  );
  return parseJSON<UploadCompleteResult>(response);
}

async function completeUploadWithRetry(uploadId: string) {
  try {
    return await completeUpload(uploadId);
  } catch (err) {
    const message = err instanceof Error ? err.message : "";
    if (!/missing chunks/i.test(message)) {
      throw err;
    }
    // Last concurrent part may still be committing — brief retry.
    await new Promise((r) => window.setTimeout(r, 500));
    return completeUpload(uploadId);
  }
}

export async function abortUpload(uploadId: string): Promise<void> {
  const response = await fetch(
    `${apiBaseURL()}/users/me/uploads/${encodeURIComponent(uploadId)}`,
    {
      method: "DELETE",
      headers: authHeaders(),
      credentials: "include",
    },
  );
  await parseJSON(response);
}

export type ChunkedUploadProgress = {
  fileName: string;
  loadedChunks: number;
  totalChunks: number;
  bytesLoaded: number;
  bytesTotal: number;
  percent: number;
};

/**
 * Auth-only chunked gallery upload with parallel parts + live byte progress.
 */
export async function uploadGalleryFile(
  file: File,
  options?: {
    albumId?: string | null;
    onProgress?: (progress: ChunkedUploadProgress) => void;
    signal?: AbortSignal;
  },
): Promise<UploadCompleteResult> {
  const init = await initUpload(file, options?.albumId);
  const uploadId = init.upload_id;
  const chunkSize = init.chunk_size;
  const totalChunks = init.total_chunks;
  let completedChunks = 0;
  let committedBytes = 0;
  const inFlightBytes = new Map<number, number>();

  const report = () => {
    let flying = 0;
    for (const n of inFlightBytes.values()) {
      flying += n;
    }
    const bytesLoaded = Math.min(file.size, committedBytes + flying);
    const percent =
      file.size <= 0
        ? 100
        : Math.min(99, Math.round((bytesLoaded / file.size) * 100));
    options?.onProgress?.({
      fileName: file.name,
      loadedChunks: completedChunks,
      totalChunks,
      bytesLoaded,
      bytesTotal: file.size,
      percent: completedChunks >= totalChunks ? 99 : percent,
    });
  };
  report();

  try {
    const indexes = Array.from({ length: totalChunks }, (_, i) => i + 1);
    let cursor = 0;

    const worker = async () => {
      while (cursor < indexes.length) {
        if (options?.signal?.aborted) {
          throw new Error("Upload cancelled");
        }
        const index = indexes[cursor];
        cursor += 1;
        const start = (index - 1) * chunkSize;
        const end = Math.min(file.size, start + chunkSize);
        const blob = file.slice(start, end);
        inFlightBytes.set(index, 0);
        await putChunkWithProgress(
          uploadId,
          index,
          blob,
          (loadedInChunk) => {
            inFlightBytes.set(index, Math.min(blob.size, loadedInChunk));
            report();
          },
          options?.signal,
        );
        inFlightBytes.delete(index);
        committedBytes += blob.size;
        completedChunks += 1;
        report();
      }
    };

    const workers = Array.from(
      { length: Math.min(CHUNK_CONCURRENCY, Math.max(1, totalChunks)) },
      () => worker(),
    );
    await Promise.all(workers);

    options?.onProgress?.({
      fileName: file.name,
      loadedChunks: totalChunks,
      totalChunks,
      bytesLoaded: file.size,
      bytesTotal: file.size,
      percent: 99,
    });
    const result = await completeUploadWithRetry(uploadId);
    options?.onProgress?.({
      fileName: file.name,
      loadedChunks: totalChunks,
      totalChunks,
      bytesLoaded: file.size,
      bytesTotal: file.size,
      percent: 100,
    });
    return result;
  } catch (err) {
    try {
      await abortUpload(uploadId);
    } catch {
      // best-effort
    }
    throw err;
  }
}

export const GALLERY_ACCEPT =
  "image/jpeg,image/png,image/webp,image/gif,image/heic,image/heif,video/mp4,video/webm,video/quicktime,.heic,.heif,.mov,.mp4,.webm,.jpg,.jpeg,.png,.gif,.webp";

export type GalleryAccessEntry = {
  email: string;
  note: string;
  created_by_user_id?: string | null;
  created_at: string;
};

export type GalleryAccessList = {
  super_admin_email: string;
  entries: GalleryAccessEntry[];
};

export async function listGalleryAccess(): Promise<GalleryAccessList> {
  const response = await fetch(`${apiBaseURL()}/admin/gallery-access`, {
    headers: authHeaders(),
    credentials: "include",
  });
  return parseJSON<GalleryAccessList>(response);
}

export async function allowGalleryEmail(
  email: string,
  note = "",
): Promise<GalleryAccessEntry> {
  const response = await fetch(`${apiBaseURL()}/admin/gallery-access`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ email, note }),
  });
  return parseJSON<GalleryAccessEntry>(response);
}

export async function revokeGalleryEmail(email: string): Promise<void> {
  const response = await fetch(`${apiBaseURL()}/admin/gallery-access/revoke`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ email }),
  });
  await parseJSON(response);
}
