export type ImageUploadData = {
  id: string;
  fingerprint: string;
  original_url: string;
  width: number;
  height: number;
  format: string;
  size_bytes: number;
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
    },
  };
}

/**
 * Uploads an image to the Go Image API (`POST /images` under NEXT_PUBLIC_IMAGE_API_URL).
 * Falls back to a mocked payload when the backend is unavailable or not configured.
 */
export async function uploadImageToBackend(file: File): Promise<ImageUploadResult> {
  const baseUrl = process.env.NEXT_PUBLIC_IMAGE_API_URL?.replace(/\/$/, "");

  if (!baseUrl) {
    return mockUpload(file);
  }

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
      // Backend early-phase / offline: degrade gracefully for the UI flow.
      return mockUpload(file);
    }

    if (payload.success) {
      return payload;
    }

    return mockUpload(file);
  } catch {
    return mockUpload(file);
  }
}
