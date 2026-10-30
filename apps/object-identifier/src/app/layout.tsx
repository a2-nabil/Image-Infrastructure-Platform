import type { Metadata } from "next";
import { Fraunces, Manrope } from "next/font/google";
import { AlertProvider } from "@/components/PhotosAlert";
import { AuthProvider } from "@/components/AuthProvider";
import GalleryUploadHost from "@/components/GalleryUploadHost";
import SiteHeader from "@/components/SiteHeader";
import { ThemeProvider } from "@/components/ThemeProvider";
import "./globals.css";

const display = Fraunces({
  subsets: ["latin"],
  variable: "--font-display",
});

const sans = Manrope({
  subsets: ["latin"],
  variable: "--font-sans",
});

export const metadata: Metadata = {
  title: {
    default: "A2N Gallery",
    template: "%s · A2N Gallery",
  },
  description:
    "Convert images to WebP, identify objects with AI, and manage your photo library with Google sign-in.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${display.variable} ${sans.variable} min-h-screen bg-white font-[family-name:var(--font-sans)] text-slate-900 antialiased dark:bg-[#0b1220] dark:text-slate-100`}
      >
        <ThemeProvider>
          <AuthProvider>
            <AlertProvider>
              <SiteHeader />
              {children}
              <GalleryUploadHost />
            </AlertProvider>
          </AuthProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
