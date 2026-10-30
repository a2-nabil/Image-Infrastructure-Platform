/** Shared display / download names based on the uploaded filename. */

export function fileBaseName(filename: string): string {
  const trimmed = (filename || "image").trim() || "image";
  const i = trimmed.lastIndexOf(".");
  if (i <= 0) {
    return trimmed;
  }
  return trimmed.slice(0, i);
}

export function fileExtension(filename: string, fallback = ""): string {
  const trimmed = (filename || "").trim();
  const i = trimmed.lastIndexOf(".");
  if (i < 0 || i === trimmed.length - 1) {
    return fallback;
  }
  return trimmed.slice(i + 1).toLowerCase();
}

export type MediaNameInput = {
  originalFilename: string;
  /** Uploaded source bytes vs generated WebP. */
  kind: "source" | "webp";
  width: number;
  height: number;
  sourceWidth: number;
  sourceHeight: number;
};

/** True when this version's pixel size differs from the uploaded image. */
export function dimensionsChanged(input: MediaNameInput): boolean {
  if (input.kind === "source") {
    return false;
  }
  if (input.width <= 0 || input.height <= 0) {
    return false;
  }
  if (input.sourceWidth <= 0 || input.sourceHeight <= 0) {
    return false;
  }
  return (
    input.width !== input.sourceWidth || input.height !== input.sourceHeight
  );
}

/**
 * Filename for UI + downloads.
 * - Source: exact uploaded name
 * - Same-size WebP: `{name}.webp`
 * - Resized WebP: `{name}-{w}x{h}.webp` (size suffix only when dimensions change)
 */
export function mediaFileName(input: MediaNameInput): string {
  const base = fileBaseName(input.originalFilename);
  if (input.kind === "source") {
    const ext = fileExtension(input.originalFilename);
    return ext ? `${base}.${ext}` : input.originalFilename || base;
  }
  if (dimensionsChanged(input)) {
    return `${base}-${input.width}x${input.height}.webp`;
  }
  return `${base}.webp`;
}

/** Short chip label (filename only; bytes shown separately in UI). */
export function mediaChipLabel(input: MediaNameInput): string {
  return mediaFileName(input);
}

/**
 * Friendly UI-only label. Downloads / Content-Disposition still use mediaFileName().
 */
export function mediaDisplayLabel(
  kind: "source" | "webp",
  presetName?: string,
): string {
  if (kind === "source") {
    return "Original";
  }
  switch ((presetName || "").toLowerCase()) {
    case "original":
      return "Original WebP";
    case "thumbnail":
      return "Thumbnail";
    case "small":
      return "Small";
    case "medium":
      return "Medium";
    case "large":
      return "Large";
    case "custom":
      return "Custom";
    default:
      return presetName
        ? presetName.charAt(0).toUpperCase() + presetName.slice(1)
        : "Original WebP";
  }
}
