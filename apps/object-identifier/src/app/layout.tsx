import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Object Identifier",
  description: "Upload an image to identify objects using AI",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-gray-50 text-gray-900 antialiased">
        {children}
      </body>
    </html>
  );
}
