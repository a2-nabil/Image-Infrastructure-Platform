import { clearAllGalleryCaches } from "@/lib/gallery-cache";
import { clearGuestSessionID, getGuestSessionID } from "@/lib/image-api";
import {
  clearSession,
  getAccessToken,
  saveSession,
  type AuthSession,
  type AuthUser,
} from "@/lib/auth";

const DEFAULT_IMAGE_API_URL = "/api/v1";

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
  const guest = getGuestSessionID();
  if (guest) {
    headers["X-Guest-Session-ID"] = guest;
  }
  return headers;
}

/** Prefer same-origin /api/v1 paths so Netlify/local proxy works for <img src>. */
export function sameOriginProxyURL(url: string): string {
  if (!url) {
    return url;
  }
  try {
    const parsed = new URL(url, "http://localhost");
    if (parsed.pathname.startsWith("/api/v1/")) {
      return `${parsed.pathname}${parsed.search}`;
    }
  } catch {
    // keep original
  }
  return url;
}

export type ImageVariantDetail = {
  id: string;
  preset_name: string;
  url: string;
  width: number;
  height: number;
  size_bytes: number;
  mime_type: string;
};

export type ImageLocationDetail = {
  latitude: number;
  longitude: number;
  altitude?: number | null;
  source: string;
};

export type ImageIdentificationDetail = {
  id: string;
  image_id?: string | null;
  fingerprint: string;
  object_name: string;
  confidence_score: number;
  summary: string;
  key_details: string[];
  suggested_actions: string[];
  model: string;
  cache_source: string;
  created_at: string;
  updated_at: string;
};

export type UserImageDetail = {
  id: string;
  fingerprint: string;
  original_filename: string;
  storage_path?: string;
  original_url: string;
  mime_type: string;
  format: string;
  file_size_bytes: number;
  width: number;
  height: number;
  status: string;
  media_kind?: "image" | "video" | string;
  duration_ms?: number | null;
  user_id?: string | null;
  guest_session_id?: string | null;
  expires_at?: string | null;
  trashed_at?: string | null;
  location?: ImageLocationDetail | null;
  created_at: string;
  updated_at: string;
  variants: ImageVariantDetail[];
  identification?: ImageIdentificationDetail | null;
};

export type UserImagesSummary = {
  image_count: number;
  variant_count: number;
  total_size_bytes: number;
  status_counts: Record<string, number>;
};

export type UserImagesPagination = {
  limit: number;
  offset: number;
  next_offset: number;
  total: number;
  has_more: boolean;
};

export type UserImagesPayload = {
  items: UserImageDetail[];
  summary: UserImagesSummary;
  pagination: UserImagesPagination;
};

type ApiSuccess<T> = { success: true; data: T };
type ApiFailure = {
  success: false;
  error: { code: string; message: string };
};

async function parseJSON<T>(response: Response): Promise<T> {
  const payload = (await response.json().catch(() => null)) as
    | ApiSuccess<T>
    | ApiFailure
    | null;
  if (!response.ok || !payload || !payload.success) {
    const message =
      payload && !payload.success
        ? payload.error.message
        : `Request failed (${response.status})`;
    throw new Error(message);
  }
  return payload.data;
}

function normalizeImage(item: UserImageDetail): UserImageDetail {
  return {
    ...item,
    original_url: sameOriginProxyURL(item.original_url),
    variants: (item.variants || []).map((v) => ({
      ...v,
      url: sameOriginProxyURL(v.url),
    })),
  };
}

/** Exchange Google ID token for API JWT and persist session. */
export async function loginWithGoogleIdToken(
  idToken: string,
): Promise<AuthSession> {
  let response: Response;
  try {
    response = await fetch(`${apiBaseURL()}/auth/google`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      credentials: "include",
      body: JSON.stringify({ id_token: idToken }),
    });
  } catch {
    throw new Error(
      "Cannot reach Image API for Google sign-in. Is the API running, and is NEXT_PUBLIC_IMAGE_API_URL set to /api/v1?",
    );
  }

  const data = await parseJSON<{
    access_token: string;
    token_type: string;
    expires_in: number;
    user: AuthUser;
  }>(response);

  const session: AuthSession = {
    accessToken: data.access_token,
    tokenType: data.token_type || "Bearer",
    expiresIn: data.expires_in,
    expiresAt: Date.now() + data.expires_in * 1000,
    user: data.user,
  };
  saveSession(session);
  return session;
}

export async function fetchMe(): Promise<AuthUser> {
  const response = await fetch(`${apiBaseURL()}/users/me`, {
    headers: authHeaders(),
    credentials: "include",
  });
  return parseJSON<AuthUser>(response);
}

export async function fetchMyImages(options?: {
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
    `${apiBaseURL()}/users/me/images?${params.toString()}`,
    {
      headers: authHeaders(),
      credentials: "include",
    },
  );
  const data = await parseJSON<UserImagesPayload>(response);
  return {
    ...data,
    items: (data.items || []).map(normalizeImage),
    pagination: data.pagination ?? {
      limit,
      offset,
      next_offset: offset + (data.items?.length ?? 0),
      total: data.summary?.image_count ?? data.items?.length ?? 0,
      has_more: false,
    },
  };
}

export async function claimGuestMedia(
  guestSessionID?: string | null,
): Promise<{ claimed_count: number; merged_count?: number; message: string }> {
  const guest = guestSessionID || getGuestSessionID();
  if (!guest) {
    return { claimed_count: 0, merged_count: 0, message: "No guest session to claim." };
  }
  const response = await fetch(`${apiBaseURL()}/users/claim-guest-media`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ guest_session_id: guest }),
  });
  const result = await parseJSON<{
    claimed_count: number;
    merged_count?: number;
    message: string;
  }>(response);
  // Guest uploads are now owned — rotate guest id so another login cannot reclaim.
  clearGuestSessionID();
  return result;
}

/** Move one image to trash (soft delete). */
export async function trashMyImage(
  imageID: string,
): Promise<{ id: string; trashed: boolean; count: number }> {
  const response = await fetch(
    `${apiBaseURL()}/users/me/images/${encodeURIComponent(imageID)}`,
    {
      method: "DELETE",
      headers: authHeaders(),
      credentials: "include",
    },
  );
  return parseJSON(response);
}

/** Move many images to trash. */
export async function trashMyImages(
  ids: string[],
): Promise<{ trashed: boolean; count: number }> {
  const response = await fetch(`${apiBaseURL()}/users/me/images/trash`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ ids }),
  });
  return parseJSON(response);
}

/** Restore images from trash. */
export async function restoreMyImages(
  ids: string[],
): Promise<{ restored: boolean; count: number }> {
  const response = await fetch(`${apiBaseURL()}/users/me/images/restore`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ ids }),
  });
  return parseJSON(response);
}

/** Permanently delete images from DB and S3. */
export async function purgeMyImages(
  ids: string[],
): Promise<{ purged: boolean; count: number }> {
  const response = await fetch(`${apiBaseURL()}/users/me/images/purge`, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    credentials: "include",
    body: JSON.stringify({ ids }),
  });
  return parseJSON(response);
}

/** Permanently delete one owned WebP variant from DB and S3. */
export async function deleteMyVariant(
  imageID: string,
  preset: string,
): Promise<{ id: string; preset: string; deleted: boolean; scope: string }> {
  const response = await fetch(
    `${apiBaseURL()}/users/me/images/${encodeURIComponent(imageID)}/variants/${encodeURIComponent(preset)}`,
    {
      method: "DELETE",
      headers: authHeaders(),
      credentials: "include",
    },
  );
  return parseJSON(response);
}

export function logoutLocal(): void {
  clearSession();
  clearAllGalleryCaches();
  clearGuestSessionID();
}
