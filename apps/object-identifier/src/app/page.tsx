"use client";

import { useState } from "react";
import { Loader, Upload } from "lucide-react";
import ResultDisplay, {
  type IdentifyResult,
} from "@/components/ResultDisplay";
import {
  formatBytes,
  uploadImage,
  type ImageUploadData,
} from "@/lib/image-api";

type IdentifyApiResponse =
  | { success: true; data: IdentifyResult }
  | { success: false; error: { code: string; message: string } };

export default function Home() {
  const [preview, setPreview] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<IdentifyResult | null>(null);
  const [uploadData, setUploadData] = useState<ImageUploadData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [backendNote, setBackendNote] = useState<string | null>(null);

  const handleImageUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) {
      return;
    }

    setPreview(URL.createObjectURL(file));
    void identifyObject(file);
  };

  const identifyObject = async (file: File) => {
    try {
      setLoading(true);
      setError(null);
      setResult(null);
      setUploadData(null);
      setBackendNote(null);

      if (file.size > 4 * 1024 * 1024) {
        throw new Error("Image size must be less than 4MB");
      }

      const validTypes = ["image/jpeg", "image/png", "image/webp"];
      if (!validTypes.includes(file.type)) {
        throw new Error("Please upload a valid image file (JPEG, PNG, or WebP)");
      }

      const uploadResult = await uploadImage(file);
      if (uploadResult.mocked) {
        setBackendNote(
          "Go Image API unavailable — continuing with local identify flow.",
        );
      } else if (uploadResult.success) {
        setUploadData(uploadResult.data);
      }

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

      setResult(payload.data);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to identify object";
      setError(message);
      setResult(null);
    } finally {
      setLoading(false);
    }
  };

  const variants = uploadData?.variants ?? [];

  return (
    <main className="container mx-auto px-4 py-8 max-w-4xl">
      <div className="text-center mb-12">
        <h1 className="text-4xl font-bold text-gray-900 mb-4">
          Object Identifier
        </h1>
        <p className="text-lg text-gray-600">
          Upload an image and let AI identify what&apos;s in it
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
            {preview ? (
              <div className="mb-4 relative">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={preview}
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
            {!preview && (
              <span className="bg-blue-500 text-white px-4 py-2 rounded-md hover:bg-blue-600 transition">
                Choose Image
              </span>
            )}
          </label>
        </div>
      </div>

      {loading && (
        <div className="text-center py-8">
          <Loader className="w-8 h-8 animate-spin mx-auto mb-4 text-blue-500" />
          <p className="text-gray-600">Analyzing your image...</p>
        </div>
      )}

      {backendNote && !loading && (
        <div className="bg-amber-50 border border-amber-200 text-amber-800 p-4 rounded-lg mb-8">
          <p className="text-sm">{backendNote}</p>
        </div>
      )}

      {uploadData?.deduplicated && !loading && (
        <div className="mb-8">
          <span className="inline-flex items-center rounded-full bg-emerald-50 px-4 py-2 text-sm font-medium text-emerald-700 border border-emerald-200">
            ⚡ Instant Deduplication Hit
          </span>
        </div>
      )}

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-600 p-4 rounded-lg mb-8">
          <p className="font-medium">Error</p>
          <p className="text-sm">{error}</p>
        </div>
      )}

      {variants.length > 0 && !loading && (
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

      {result && !loading && (
        <div className="mb-8">
          <ResultDisplay result={result} />
        </div>
      )}
    </main>
  );
}
