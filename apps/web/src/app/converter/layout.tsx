import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "WebP Converter",
  description:
    "Convert JPEG, PNG, and WebP images with quality control, presets, and batch download. Free online Any-to-WebP converter.",
};

export default function ConverterLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return children;
}
