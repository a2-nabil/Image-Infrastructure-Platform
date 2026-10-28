"use client";

import { useEffect, useRef, useState } from "react";
import { Loader, Upload } from "lucide-react";
import ResultDisplay, {
  type IdentifyResult,
} from "@/components/ResultDisplay";
import {
  formatBytes,
  uploadImage,
  waitForImageReady,
  type ImageUploadData,
} from "@/lib/image-api";

type IdentifyApiResponse =
  | { success: true; data: IdentifyResult }
  | { success: false; error: { code: string; message: string } };

const ALLOWED_IMAGE_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;

const ALLOWED_DATA_URL_PREFIXES = [
  "data:image/jpeg;",
  "data:image/png;",
  "data:image/webp;",
] as const;

function isAllowedImageType(type: string): boolean {
  return (ALLOWED_IMAGE_TYPES as readonly string[]).includes(type);
}

/** Build a data:image URL only after MIME checks (safe for <img src>). */
async function readSafeImagePreview(file: File): Promise<string> {
  if (!isAllowedImageType(file.type)) {
    throw new Error("Please upload a valid image file (JPEG, PNG, or WebP)");
  }
  if (file.size > 4 * 1024 * 1024) {
    throw new Error("Image size must be less than 4MB");
  }

  const dataUrl = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result !== "string") {
        reject(new Error("Failed to read image"));
        return;
      }
      resolve(reader.result);
    };
    reader.onerror = () => reject(new Error("Failed to read image"));
    reader.readAsDataURL(file);
  });

  if (!ALLOWED_DATA_URL_PREFIXES.some((prefix) => dataUrl.startsWith(prefix))) {
    throw new Error("Please upload a valid image file (JPEG, PNG, or WebP)");
  }

  return dataUrl;
}

function asSafeImageSrc(value: string | null): string | undefined {
  if (!value) {
    return undefined;
  }
  return ALLOWED_DATA_URL_PREFIXES.some((prefix) => value.startsWith(prefix))
    ? value
    : undefined;
}

async function identifyWithGemini(file: File): Promise<IdentifyResult> {
  const formData = new FormData();
  formData.append("image", file);

  const response = await fetch("/api/identify", {
    method: "POST",
    body: formData,
  });

  const payload = (await response.json()) as IdentifyApiResponse;

  if (!response.ok || !payload.success) {
    const message =
      !payload.success && payload.error?.message
        ? payload.error.message
        : "Failed to identify object";
    throw new Error(message);
  }

  return payload.data;
}

export default function Home() {
  const [preview, setPreview] = useState<string | null>(null);
  const [identifyLoading, setIdentifyLoading] = useState(false);
  const [uploadLoading, setUploadLoading] = useState(false);
  const [variantsLoading, setVariantsLoading] = useState(false);
  const [result, setResult] = useState<IdentifyResult | null>(null);
  const [uploadData, setUploadData] = useState<ImageUploadData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [identifyError, setIdentifyError] = useState<string | null>(null);
  const [backendNote, setBackendNote] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const handleImageUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) {
      return;
    }

    void (async () => {
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      try {
        setError(null);
        setIdentifyError(null);
        setResult(null);
        setUploadData(null);
        setBackendNote(null);
        setVariantsLoading(false);

        const safePreview = await readSafeImagePreview(file);
        setPreview(safePreview);

        // Fire both APIs in parallel; UI updates as each settles.
        setIdentifyLoading(true);
        setUploadLoading(true);

        const identifyPromise = identifyWithGemini(file)
          .then((data) => {
            if (!controller.signal.aborted) {
              setResult(data);
              setIdentifyError(null);
            }
          })
          .catch((err: unknown) => {
            if (!controller.signal.aborted) {
              setIdentifyError(
                err instanceof Error ? err.message : "Failed to identify object",
              );
              setResult(null);
            }
          })
          .finally(() => {
            if (!controller.signal.aborted) {
              setIdentifyLoading(false);
            }
          });

        const uploadPromise = uploadImage(file)
          .then(async (uploadResult) => {
            if (controller.signal.aborted) {
              return;
            }

            if (!uploadResult.success) {
              setBackendNote(
                uploadResult.error?.message || "Image upload failed.",
              );
              return;
            }

            if (uploadResult.mocked) {
              setBackendNote(
                "Go Image API unavailable — continuing with local identify flow.",
              );
              setUploadData(uploadResult.data);
              return;
            }

            setUploadData(uploadResult.data);

            // Async worker path: poll until variants are ready.
            if (
              !uploadResult.data.deduplicated &&
              (uploadResult.data.status === "processing" ||
                (uploadResult.data.variants?.length ?? 0) === 0)
            ) {
              setVariantsLoading(true);
              const ready = await waitForImageReady(uploadResult.data.id, {
                signal: controller.signal,
                onUpdate: (partial) => {
                  if (!controller.signal.aborted) {
                    setUploadData(partial);
                  }
                },
              });
              if (!controller.signal.aborted && ready) {
                setUploadData(ready);
              }
            }
          })
          .catch((err: unknown) => {
            if (!controller.signal.aborted) {
              setBackendNote(
                err instanceof Error
                  ? err.message
                  : "Image upload failed unexpectedly.",
              );
            }
          })
          .finally(() => {
            if (!controller.signal.aborted) {
              setUploadLoading(false);
              setVariantsLoading(false);
            }
          });

        await Promise.allSettled([identifyPromise, uploadPromise]);
      } catch (err) {
        setPreview(null);
        setResult(null);
        setUploadData(null);
        setBackendNote(null);
        setError(err instanceof Error ? err.message : "Failed to load image");
        setIdentifyLoading(false);
        setUploadLoading(false);
        setVariantsLoading(false);
      }
    })();
  };

  const variants = uploadData?.variants ?? [];
  const safePreviewSrc = asSafeImageSrc(preview);
  const anyLoading = identifyLoading || uploadLoading || variantsLoading;

  return (
    <main className="container mx-auto px-4 py-8 max-w-4xl">
      <div className="text-center mb-12">
        <h1 className="text-4xl font-bold text-gray-900 mb-4">
          Object Identifier
        </h1>
        <p className="text-lg text-gray-600">
          Upload an image and let AI identify what&apos;s in it
        </p>
        <p className="text-sm text-gray-500 mt-2">
          Identify and upload run in parallel — results appear as each finishes.
        </p>
      </div>

      <div className="bg-white rounded-xl shadow-lg p-6 mb-8">
        <div className="border-2 border-dashed border-gray-300 rounded-lg text-center">
          <input
            type="file"
            accept="image/jpeg,image/png,image/webp"
            capture="environment"
            onChange={handleImageUpload}
            className="hidden"
            id="image-upload"
          />
          <label
            htmlFor="image-upload"
            className="cursor-pointer flex flex-col items-center p-8"
          >
            {safePreviewSrc ? (
              <div className="mb-4 relative">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={safePreviewSrc}
                  alt="Preview"
                  className="max-h-64 rounded-lg"
                />
                <div className="absolute inset-0 bg-black/40 flex items-center justify-center opacity-0 hover:opacity-100 transition-opacity rounded-lg">
                  <span className="text-white text-sm">
                    Click to change image
                  </span>
                </div>
              </div>
            ) : (
              <div className="mb-4 text-center flex items-center justify-center flex-col">
                <Upload className="w-12 h-12 text-gray-400" />
                <p className="mt-2 text-sm text-gray-500">
                  Click to upload an image
                </p>
                <p className="text-xs text-gray-400 mt-1">
                  JPEG, PNG, WebP up to 4MB
                </p>
              </div>
            )}
            {!safePreviewSrc && (
              <span className="bg-blue-500 text-white px-4 py-2 rounded-md hover:bg-blue-600 transition">
                Choose Image
              </span>
            )}
          </label>
        </div>
      </div>

      {anyLoading && (
        <div className="bg-slate-50 border border-slate-200 rounded-lg p-4 mb-8 space-y-2">
          {identifyLoading && (
            <p className="text-sm text-slate-700 flex items-center gap-2">
              <Loader className="w-4 h-4 animate-spin text-blue-500" />
              AI identify running…
            </p>
          )}
          {uploadLoading && (
            <p className="text-sm text-slate-700 flex items-center gap-2">
              <Loader className="w-4 h-4 animate-spin text-blue-500" />
              Uploading to image API…
            </p>
          )}
          {variantsLoading && (
            <p className="text-sm text-slate-700 flex items-center gap-2">
              <Loader className="w-4 h-4 animate-spin text-blue-500" />
              Worker generating variants…
            </p>
          )}
        </div>
      )}

      {backendNote && (
        <div className="bg-amber-50 border border-amber-200 text-amber-800 p-4 rounded-lg mb-8">
          <p className="text-sm">{backendNote}</p>
        </div>
      )}

      {uploadData?.status === "processing" && variants.length === 0 && (
        <div className="mb-8">
          <span className="inline-flex items-center rounded-full bg-sky-50 px-4 py-2 text-sm font-medium text-sky-700 border border-sky-200">
            Queued for async variant worker
          </span>
        </div>
      )}

      {uploadData?.deduplicated && (
        <div className="mb-8">
          <span className="inline-flex items-center rounded-full bg-emerald-50 px-4 py-2 text-sm font-medium text-emerald-700 border border-emerald-200">
            Instant Deduplication Hit
          </span>
        </div>
      )}

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-600 p-4 rounded-lg mb-8">
          <p className="font-medium">Error</p>
          <p className="text-sm">{error}</p>
        </div>
      )}

      {identifyError && (
        <div className="bg-red-50 border border-red-200 text-red-600 p-4 rounded-lg mb-8">
          <p className="font-medium">Identify error</p>
          <p className="text-sm">{identifyError}</p>
        </div>
      )}

      {variants.length > 0 && (
        <div className="bg-white rounded-xl shadow-lg p-6 mb-8 animate-fade-in">
          <h2 className="text-2xl font-semibold mb-6 text-gray-800">
            Image Variants
          </h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
            {variants.map((variant) => (
              <div
                key={variant.id || variant.preset_name}
                className="border border-gray-200 rounded-lg p-4 flex flex-col"
              >
                <div className="mb-3 bg-gray-50 rounded-md overflow-hidden flex items-center justify-center min-h-32">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img
                    src={variant.url}
                    alt={`${variant.preset_name} variant`}
                    className="max-h-40 w-full object-contain"
                  />
                </div>
                <p className="text-sm font-semibold text-gray-900 capitalize mb-1">
                  {variant.preset_name}
                </p>
                <p className="text-xs text-gray-500 mb-1">
                  {variant.width} x {variant.height}
                </p>
                <p className="text-xs text-gray-500 mb-4">
                  {formatBytes(variant.size_bytes)}
                </p>
                <a
                  href={variant.url}
                  download={`${variant.preset_name}.webp`}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-auto inline-flex justify-center items-center rounded-md bg-blue-500 px-3 py-2 text-sm font-medium text-white hover:bg-blue-600 transition"
                >
                  Download
                </a>
              </div>
            ))}
          </div>
        </div>
      )}

      {result && (
        <div className="mb-8">
          <ResultDisplay result={result} />
        </div>
      )}
    </main>
  );
}
