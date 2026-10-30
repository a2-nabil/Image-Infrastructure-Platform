/** Shared identify tool limits (API + UI). */

export const IDENTIFY_MAX_FILE_BYTES = 4 * 1024 * 1024; // 4 MB
export const IDENTIFY_MAX_QUEUE_FILES = 20;
export const IDENTIFY_RATE_LIMIT = 10;
export const IDENTIFY_RATE_WINDOW_SECONDS = 60;

export const IDENTIFY_ALLOWED_TYPES = [
  "image/jpeg",
  "image/png",
  "image/webp",
] as const;

export function isIdentifyAllowedType(type: string): boolean {
  return (IDENTIFY_ALLOWED_TYPES as readonly string[]).includes(type);
}

export function formatIdentifyLimitLabel(): string {
  const mb = IDENTIFY_MAX_FILE_BYTES / (1024 * 1024);
  return `JPEG, PNG, WebP · up to ${mb}MB each · max ${IDENTIFY_MAX_QUEUE_FILES} files · ${IDENTIFY_RATE_LIMIT} identifies / minute`;
}
