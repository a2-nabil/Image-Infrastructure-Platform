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

const DEFAULT_IMAGE_API_URL = "http://localhost:8080/api/v1";

function apiBaseURL(): string {
  return (
    process.env.NEXT_PUBLIC_IMAGE_API_URL || DEFAULT_IMAGE_API_URL
  ).replace(/\/$/, "");
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
      headers: {
        "X-Request-ID": requestId,
      },
      body: formData,
    });

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
