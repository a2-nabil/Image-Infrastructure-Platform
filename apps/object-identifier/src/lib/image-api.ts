export type ImageVariantData = {
  id: string;
  preset_name: string;
  url: string;
  width: number;
  height: number;
  size_bytes: number;
  mime_type: string;
};

export type ImageUploadData = {
  id: string;
  fingerprint: string;
  original_url: string;
  width: number;
  height: number;
  format: string;
  size_bytes: number;
  status?: string;
  deduplicated?: boolean;
  variants?: ImageVariantData[];
};

export type ImageUploadSuccess = {
  success: true;
  data: ImageUploadData;
  mocked?: boolean;
};

export type ImageUploadFailure = {
  success: false;
  error: {
    code: string;
    message: string;
  };
  mocked?: boolean;
};

export type ImageUploadResult = ImageUploadSuccess | ImageUploadFailure;

const GUEST_SESSION_KEY = "image_api_guest_session_id";

// Prefer same-origin "/api/v1" so Netlify (or next.config rewrites) can proxy to Render/local Go.
const DEFAULT_IMAGE_API_URL = "/api/v1";

function apiBaseURL(): string {
  return (
    process.env.NEXT_PUBLIC_IMAGE_API_URL || DEFAULT_IMAGE_API_URL
  ).replace(/\/$/, "");
}

export function getGuestSessionID(): string | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    return window.localStorage.getItem(GUEST_SESSION_KEY);
  } catch {
    return null;
  }
}

export function setGuestSessionID(id: string): void {
  if (typeof window === "undefined" || !id) {
    return;
  }
  try {
    window.localStorage.setItem(GUEST_SESSION_KEY, id);
  } catch {
    // ignore quota / private mode
  }
}

function authHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...(extra ?? {}) };
  const guest = getGuestSessionID();
  if (guest) {
    headers["X-Guest-Session-ID"] = guest;
  }
  return headers;
}

function mockUpload(file: File): ImageUploadSuccess {
  return {
    success: true,
    mocked: true,
    data: {
      id: crypto.randomUUID(),
      fingerprint: `pending-${file.name}-${file.size}`,
      original_url: "",
      width: 0,
      height: 0,
      format: file.type.split("/")[1] || "unknown",
      size_bytes: file.size,
      status: "ready",
      deduplicated: false,
      variants: [],
    },
  };
}

/**
 * Uploads an image to the Go Image API (`POST /images`).
 * Falls back to a mocked payload when the backend is unavailable.
 */
export async function uploadImage(file: File): Promise<ImageUploadResult> {
  const baseUrl = apiBaseURL();
  const formData = new FormData();
  formData.append("image", file);

  const requestId =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : `req-${Date.now()}`;

  try {
    const response = await fetch(`${baseUrl}/images`, {
      method: "POST",
      headers: authHeaders({ "X-Request-ID": requestId }),
      body: formData,
    });

    const guestFromHeader = response.headers.get("X-Guest-Session-ID");
    if (guestFromHeader) {
      setGuestSessionID(guestFromHeader);
    }

    const payload = (await response.json().catch(() => null)) as
      | ImageUploadResult
      | null;

    if (!response.ok || !payload) {
      return mockUpload(file);
    }

    if (payload.success) {
      return {
        ...payload,
        data: {
          ...payload.data,
          variants: payload.data.variants ?? [],
          deduplicated: Boolean(payload.data.deduplicated),
        },
      };
    }

    return mockUpload(file);
  } catch {
    return mockUpload(file);
  }
}

/** Poll image status until ready/failed or timeout (async worker variants). */
export async function waitForImageReady(
  imageId: string,
  options?: {
    intervalMs?: number;
    timeoutMs?: number;
    signal?: AbortSignal;
    onUpdate?: (data: ImageUploadData) => void;
  },
): Promise<ImageUploadData | null> {
  const intervalMs = options?.intervalMs ?? 800;
  const timeoutMs = options?.timeoutMs ?? 45000;
  const baseUrl = apiBaseURL();
  const started = Date.now();

  while (Date.now() - started < timeoutMs) {
    if (options?.signal?.aborted) {
      return null;
    }

    try {
      const response = await fetch(`${baseUrl}/images/${imageId}`, {
        signal: options?.signal,
        headers: authHeaders(),
      });
      const payload = (await response.json().catch(() => null)) as
        | ImageUploadSuccess
        | ImageUploadFailure
        | null;

      if (response.ok && payload && payload.success) {
        const data = {
          ...payload.data,
          variants: payload.data.variants ?? [],
        };
        options?.onUpdate?.(data);
        if (data.status === "ready" || data.status === "failed") {
          return data;
        }
      }
    } catch {
      // keep polling through transient network blips
    }

    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }

  return null;
}

/** @deprecated Prefer uploadImage */
export async function uploadImageToBackend(
  file: File,
): Promise<ImageUploadResult> {
  return uploadImage(file);
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}
