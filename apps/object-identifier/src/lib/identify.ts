import type { IdentifyResult } from "@/components/ResultDisplay";
import { getGuestSessionID } from "@/lib/image-api";
import {
  IDENTIFY_MAX_FILE_BYTES,
  isIdentifyAllowedType,
} from "@/lib/identify-limits";

const DEFAULT_IMAGE_API_URL = "/api/v1";

function apiBaseURL(): string {
  return (
    process.env.NEXT_PUBLIC_IMAGE_API_URL || DEFAULT_IMAGE_API_URL
  ).replace(/\/$/, "");
}

function authHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(extra ?? {}),
  };
  if (typeof window !== "undefined") {
    try {
      const raw = window.localStorage.getItem("image_api_auth_session");
      if (raw) {
        const parsed = JSON.parse(raw) as {
          accessToken?: string;
          expiresAt?: number;
        };
        if (
          parsed.accessToken &&
          (!parsed.expiresAt || Date.now() < parsed.expiresAt)
        ) {
          headers.Authorization = `Bearer ${parsed.accessToken}`;
        }
      }
    } catch {
      // ignore
    }
  }
  const guest = getGuestSessionID();
  if (guest) {
    headers["X-Guest-Session-ID"] = guest;
  }
  return headers;
}

export type IdentifyResponse = IdentifyResult & {
  cache_hit?: boolean;
  cache_source?: string;
};

export type IdentifyErrorCode =
  | "RATE_LIMIT_EXCEEDED"
  | "QUOTA_EXCEEDED"
  | "FILE_TOO_LARGE"
  | "INVALID_TYPE"
  | "MISSING_IMAGE"
  | "MISSING_API_KEY"
  | "EMPTY_MODEL_RESPONSE"
  | "IDENTIFY_FAILED"
  | "NETWORK";

export class IdentifyError extends Error {
  code: IdentifyErrorCode;
  status: number;
  retryAfterSeconds: number | null;
  rateLimit: {
    limit: number | null;
    remaining: number | null;
    resetSeconds: number | null;
  };

  constructor(input: {
    message: string;
    code?: IdentifyErrorCode;
    status?: number;
    retryAfterSeconds?: number | null;
    rateLimit?: IdentifyError["rateLimit"];
  }) {
    super(input.message);
    this.name = "IdentifyError";
    this.code = input.code ?? "IDENTIFY_FAILED";
    this.status = input.status ?? 500;
    this.retryAfterSeconds = input.retryAfterSeconds ?? null;
    this.rateLimit = input.rateLimit ?? {
      limit: null,
      remaining: null,
      resetSeconds: null,
    };
  }

  get isRateLimited(): boolean {
    return (
      this.status === 429 ||
      this.code === "RATE_LIMIT_EXCEEDED" ||
      this.code === "QUOTA_EXCEEDED"
    );
  }
}

function headerInt(headers: Headers, name: string): number | null {
  const raw = headers.get(name);
  if (!raw) {
    return null;
  }
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) ? n : null;
}

/** SHA-256 hex digest of a File/Blob for exact identify caching. */
export async function sha256Hex(file: Blob): Promise<string> {
  const buffer = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export async function identifyImage(
  file: File,
  options?: { fingerprint?: string; imageId?: string },
): Promise<IdentifyResponse> {
  if (!isIdentifyAllowedType(file.type)) {
    throw new IdentifyError({
      message: "Please upload a JPEG, PNG, or WebP image.",
      code: "INVALID_TYPE",
      status: 400,
    });
  }
  if (file.size > IDENTIFY_MAX_FILE_BYTES) {
    const mb = IDENTIFY_MAX_FILE_BYTES / (1024 * 1024);
    throw new IdentifyError({
      message: `Image exceeds the ${mb}MB upload limit.`,
      code: "FILE_TOO_LARGE",
      status: 413,
    });
  }

  const formData = new FormData();
  formData.append("image", file);
  if (options?.fingerprint) {
    formData.append("fingerprint", options.fingerprint);
  }
  if (options?.imageId) {
    formData.append("image_id", options.imageId);
  }

  let response: Response;
  try {
    response = await fetch("/api/identify", {
      method: "POST",
      body: formData,
      credentials: "include",
      headers: authHeaders(),
    });
  } catch {
    throw new IdentifyError({
      message: "Network error while identifying. Check your connection.",
      code: "NETWORK",
      status: 0,
    });
  }

  const rateLimit = {
    limit: headerInt(response.headers, "X-RateLimit-Limit"),
    remaining: headerInt(response.headers, "X-RateLimit-Remaining"),
    resetSeconds: headerInt(response.headers, "X-RateLimit-Reset"),
  };
  const retryAfter =
    headerInt(response.headers, "Retry-After") ?? rateLimit.resetSeconds;

  const payload = (await response.json().catch(() => null)) as
    | { success: true; data: IdentifyResponse }
    | { success: false; error: { code?: string; message: string } }
    | null;

  if (!response.ok || !payload || !payload.success) {
    const code = (
      payload && !payload.success && payload.error?.code
        ? payload.error.code
        : response.status === 429
          ? "RATE_LIMIT_EXCEEDED"
          : response.status === 413
            ? "FILE_TOO_LARGE"
            : "IDENTIFY_FAILED"
    ) as IdentifyErrorCode;
    const message =
      payload && !payload.success
        ? payload.error.message
        : "Failed to identify object";
    throw new IdentifyError({
      message,
      code,
      status: response.status,
      retryAfterSeconds: retryAfter,
      rateLimit,
    });
  }

  return payload.data;
}

/** Link an identify result to a stored image id (dashboard / ownership). */
export async function linkIdentifyToImage(input: {
  fingerprint: string;
  imageId: string;
  result: IdentifyResult;
  embedding?: number[];
  cacheSource?: string;
}): Promise<void> {
  await fetch(`${apiBaseURL()}/identify/store`, {
    method: "POST",
    credentials: "include",
    headers: authHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({
      fingerprint: input.fingerprint,
      image_id: input.imageId,
      object_name: input.result.object_name,
      confidence_score: input.result.confidence_score,
      summary: input.result.summary,
      key_details: input.result.key_details,
      suggested_actions: input.result.suggested_actions,
      embedding: input.embedding,
      cache_source: input.cacheSource || "live",
    }),
  });
}
