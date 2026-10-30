import { createHash } from "crypto";
import { GoogleGenAI } from "@google/genai";
import { NextRequest, NextResponse } from "next/server";
import {
  IDENTIFY_ALLOWED_TYPES,
  IDENTIFY_MAX_FILE_BYTES,
  IDENTIFY_RATE_LIMIT,
  IDENTIFY_RATE_WINDOW_SECONDS,
  isIdentifyAllowedType,
} from "@/lib/identify-limits";

export const runtime = "nodejs";

type RateBucket = { count: number; resetAt: number };

const rateBuckets = new Map<string, RateBucket>();

function clientIP(request: NextRequest): string {
  const xff = request.headers.get("x-forwarded-for");
  if (xff) {
    const first = xff.split(",")[0]?.trim();
    if (first) {
      return first;
    }
  }
  const real = request.headers.get("x-real-ip")?.trim();
  if (real) {
    return real;
  }
  return "local";
}

function checkRateLimit(ip: string): {
  allowed: boolean;
  limit: number;
  remaining: number;
  retryAfter: number;
} {
  const now = Date.now();
  const windowMs = IDENTIFY_RATE_WINDOW_SECONDS * 1000;
  let bucket = rateBuckets.get(ip);
  if (!bucket || now >= bucket.resetAt) {
    bucket = { count: 0, resetAt: now + windowMs };
    rateBuckets.set(ip, bucket);
  }
  bucket.count += 1;
  const remaining = Math.max(0, IDENTIFY_RATE_LIMIT - bucket.count);
  const retryAfter = Math.max(1, Math.ceil((bucket.resetAt - now) / 1000));
  return {
    allowed: bucket.count <= IDENTIFY_RATE_LIMIT,
    limit: IDENTIFY_RATE_LIMIT,
    remaining,
    retryAfter,
  };
}

function rateLimitHeaders(meta: {
  limit: number;
  remaining: number;
  retryAfter: number;
}): HeadersInit {
  return {
    "X-RateLimit-Limit": String(meta.limit),
    "X-RateLimit-Remaining": String(meta.remaining),
    "X-RateLimit-Reset": String(meta.retryAfter),
  };
}

function isQuotaOrRateError(message: string): boolean {
  const lower = message.toLowerCase();
  return (
    lower.includes("429") ||
    lower.includes("rate limit") ||
    lower.includes("resource_exhausted") ||
    lower.includes("quota") ||
    lower.includes("too many requests")
  );
}

const identifySchema = {
  type: "object",
  properties: {
    object_name: { type: "string" },
    confidence_score: { type: "number" },
    summary: { type: "string" },
    key_details: {
      type: "array",
      items: { type: "string" },
    },
    suggested_actions: {
      type: "array",
      items: { type: "string" },
    },
  },
  required: [
    "object_name",
    "confidence_score",
    "summary",
    "key_details",
    "suggested_actions",
  ],
} as const;

const PROMPT = `Analyze the uploaded image and identify the primary object or subject.
Return structured JSON only with:
- object_name: concise name of the main object
- confidence_score: number from 0 to 1
- summary: 1-2 sentence description
- key_details: notable visual characteristics
- suggested_actions: practical next steps related to the object`;

type IdentifyData = {
  object_name: string;
  confidence_score: number;
  summary: string;
  key_details: string[];
  suggested_actions: string[];
};

function imageApiOrigin(): string {
  return (
    process.env.IMAGE_API_ORIGIN ||
    process.env.NEXT_PUBLIC_IMAGE_API_ORIGIN ||
    "http://localhost:8080"
  ).replace(/\/$/, "");
}

function sha256Hex(buf: Buffer): string {
  return createHash("sha256").update(buf).digest("hex");
}

async function lookupCache(
  fingerprint: string,
  embedding: number[] | undefined,
  authHeader: string | null,
  guestHeader: string | null,
): Promise<{ hit: boolean; source?: string; data?: IdentifyData & Record<string, unknown> } | null> {
  try {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    if (authHeader) headers.Authorization = authHeader;
    if (guestHeader) headers["X-Guest-Session-ID"] = guestHeader;

    const response = await fetch(`${imageApiOrigin()}/api/v1/identify/lookup`, {
      method: "POST",
      headers,
      body: JSON.stringify({ fingerprint, embedding }),
    });
    if (!response.ok) {
      return null;
    }
    const payload = (await response.json()) as {
      success: boolean;
      data?: { hit: boolean; source?: string; data?: IdentifyData & Record<string, unknown> };
    };
    if (!payload.success || !payload.data) {
      return null;
    }
    return payload.data;
  } catch {
    return null;
  }
}

async function storeIdentify(
  body: Record<string, unknown>,
  authHeader: string | null,
  guestHeader: string | null,
): Promise<void> {
  try {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    if (authHeader) headers.Authorization = authHeader;
    if (guestHeader) headers["X-Guest-Session-ID"] = guestHeader;

    await fetch(`${imageApiOrigin()}/api/v1/identify/store`, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    });
  } catch {
    // Persistence failures should not fail the user-facing identify response.
  }
}

async function embedText(ai: GoogleGenAI, text: string): Promise<number[] | undefined> {
  try {
    const result = await ai.models.embedContent({
      model: "text-embedding-004",
      contents: text,
    });
    const values =
      result.embeddings?.[0]?.values ||
      // some SDK shapes
      (result as { embedding?: { values?: number[] } }).embedding?.values;
    if (Array.isArray(values) && values.length > 0) {
      return values.map((v) => Number(v));
    }
  } catch {
    // embedding is optional for semantic cache
  }
  return undefined;
}

export async function POST(request: NextRequest) {
  const ip = clientIP(request);
  const rate = checkRateLimit(ip);
  const rateHeaders = rateLimitHeaders(rate);

  if (!rate.allowed) {
    return NextResponse.json(
      {
        success: false,
        error: {
          code: "RATE_LIMIT_EXCEEDED",
          message: `Too many identify requests. Limit is ${IDENTIFY_RATE_LIMIT} per minute. Try again in ${rate.retryAfter}s.`,
        },
      },
      {
        status: 429,
        headers: {
          ...rateHeaders,
          "Retry-After": String(rate.retryAfter),
        },
      },
    );
  }

  try {
    const apiKey = process.env.GEMINI_API_KEY;
    if (!apiKey) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "MISSING_API_KEY",
            message: "GEMINI_API_KEY is not configured",
          },
        },
        { status: 500, headers: rateHeaders },
      );
    }

    const formData = await request.formData();
    const image = formData.get("image");
    const imageIdRaw = formData.get("image_id");
    const fingerprintRaw = formData.get("fingerprint");

    if (!(image instanceof File)) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "MISSING_IMAGE",
            message: "No image provided",
          },
        },
        { status: 400, headers: rateHeaders },
      );
    }

    if (!isIdentifyAllowedType(image.type)) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "INVALID_TYPE",
            message: `Unsupported type. Allowed: ${IDENTIFY_ALLOWED_TYPES.join(", ")}`,
          },
        },
        { status: 400, headers: rateHeaders },
      );
    }

    if (image.size > IDENTIFY_MAX_FILE_BYTES) {
      const mb = IDENTIFY_MAX_FILE_BYTES / (1024 * 1024);
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "FILE_TOO_LARGE",
            message: `Image exceeds the ${mb}MB upload limit for identify.`,
          },
        },
        { status: 413, headers: rateHeaders },
      );
    }

    const buffer = Buffer.from(await image.arrayBuffer());
    if (buffer.byteLength > IDENTIFY_MAX_FILE_BYTES) {
      const mb = IDENTIFY_MAX_FILE_BYTES / (1024 * 1024);
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "FILE_TOO_LARGE",
            message: `Image exceeds the ${mb}MB upload limit for identify.`,
          },
        },
        { status: 413, headers: rateHeaders },
      );
    }
    const fingerprint =
      typeof fingerprintRaw === "string" && fingerprintRaw.trim()
        ? fingerprintRaw.trim().toLowerCase()
        : sha256Hex(buffer);
    const imageId =
      typeof imageIdRaw === "string" && imageIdRaw.trim()
        ? imageIdRaw.trim()
        : undefined;

    const authHeader = request.headers.get("authorization");
    const guestHeader = request.headers.get("x-guest-session-id");
    const ai = new GoogleGenAI({ apiKey });

    // Warm semantic probe embedding from filename/mime hint is weak; exact fingerprint
    // hits Redis/DB first. Optional empty embedding still allows exact path.
    const cached = await lookupCache(fingerprint, undefined, authHeader, guestHeader);
    if (cached?.hit && cached.data?.object_name) {
      if (imageId) {
        await storeIdentify(
          {
            fingerprint,
            image_id: imageId,
            object_name: cached.data.object_name,
            confidence_score: cached.data.confidence_score,
            summary: cached.data.summary,
            key_details: cached.data.key_details ?? [],
            suggested_actions: cached.data.suggested_actions ?? [],
            model: cached.data.model || "gemini-3.1-flash-lite",
            cache_source: cached.source || "exact_cache",
            embedding: cached.data.embedding,
          },
          authHeader,
          guestHeader,
        );
      }

      return NextResponse.json(
        {
          success: true,
          data: {
            object_name: cached.data.object_name,
            confidence_score: cached.data.confidence_score,
            summary: cached.data.summary,
            key_details: cached.data.key_details ?? [],
            suggested_actions: cached.data.suggested_actions ?? [],
            cache_hit: true,
            cache_source: cached.source || "exact_cache",
          },
        },
        { headers: rateHeaders },
      );
    }

    const bytes = buffer.toString("base64");
    const mimeType = image.type || "image/jpeg";

    const response = await ai.models.generateContent({
      model: "gemini-3.1-flash-lite",
      contents: [
        {
          inlineData: {
            mimeType,
            data: bytes,
          },
        },
        { text: PROMPT },
      ],
      config: {
        responseMimeType: "application/json",
        responseJsonSchema: identifySchema,
      },
    });

    const text = response.text;
    if (!text) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "EMPTY_MODEL_RESPONSE",
            message: "Model returned an empty response",
          },
        },
        { status: 502, headers: rateHeaders },
      );
    }

    const parsed = JSON.parse(text) as IdentifyData;
    const embedding = await embedText(
      ai,
      `${parsed.object_name}. ${parsed.summary}. ${(parsed.key_details || []).join("; ")}`,
    );

    await storeIdentify(
      {
        fingerprint,
        image_id: imageId,
        object_name: parsed.object_name,
        confidence_score: parsed.confidence_score,
        summary: parsed.summary,
        key_details: parsed.key_details ?? [],
        suggested_actions: parsed.suggested_actions ?? [],
        model: "gemini-3.1-flash-lite",
        cache_source: "live",
        embedding,
      },
      authHeader,
      guestHeader,
    );

    return NextResponse.json(
      {
        success: true,
        data: {
          ...parsed,
          cache_hit: false,
          cache_source: "live",
        },
      },
      { headers: rateHeaders },
    );
  } catch (error) {
    const message =
      error instanceof Error ? error.message : "Failed to identify object";
    if (isQuotaOrRateError(message)) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "QUOTA_EXCEEDED",
            message:
              "AI provider rate limit or quota was hit. Please wait a minute and try again.",
          },
        },
        {
          status: 429,
          headers: {
            ...rateHeaders,
            "Retry-After": String(IDENTIFY_RATE_WINDOW_SECONDS),
          },
        },
      );
    }
    return NextResponse.json(
      {
        success: false,
        error: {
          code: "IDENTIFY_FAILED",
          message,
        },
      },
      { status: 500, headers: rateHeaders },
    );
  }
}
