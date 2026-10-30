import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Object Identifier",
  description:
    "Upload photos (JPEG/PNG/WebP, max 4MB each) and let AI identify objects. Batch up to 20 files with rate-limited identify requests.",
};

export default function IdentifyLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return children;
}
