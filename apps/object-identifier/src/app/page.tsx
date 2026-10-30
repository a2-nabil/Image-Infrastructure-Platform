import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight, ImageIcon, ScanSearch } from "lucide-react";

export const metadata: Metadata = {
  title: "A2N Gallery · Image tools that feel like a photo library",
  description:
    "Convert images to WebP, identify objects with AI, and keep everything in one Google-signed gallery. Fast variants, private library, and ownership that stays yours.",
  keywords: [
    "WebP converter",
    "image converter",
    "object identifier",
    "AI image recognition",
    "photo gallery",
    "A2N Gallery",
  ],
  openGraph: {
    title: "A2N Gallery",
    description:
      "Convert to WebP, identify what is in a photo, and own your library.",
    type: "website",
  },
};

export default function HomePage() {
  return (
    <main className="relative isolate min-h-[calc(100vh-4rem)] overflow-hidden bg-white text-slate-900 dark:bg-[#0b1220] dark:text-slate-100">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10"
      >
        <div className="absolute inset-0 bg-[radial-gradient(120%_80%_at_12%_-8%,#ede9fe_0%,transparent_55%),radial-gradient(90%_70%_at_90%_0%,#e2e8f0_0%,transparent_50%),linear-gradient(180deg,#f8fafc_0%,#ffffff_45%,#f1f5f9_100%)] dark:bg-[radial-gradient(120%_80%_at_10%_-10%,rgba(109,40,217,0.22)_0%,transparent_55%),radial-gradient(90%_70%_at_95%_10%,rgba(30,41,59,0.9)_0%,transparent_50%),linear-gradient(180deg,#0b1220_0%,#0f172a_50%,#0b1220_100%)]" />
        <div className="home-grid-fade absolute inset-0 opacity-[0.35] dark:opacity-[0.2]" />
      </div>

      <section className="mx-auto grid min-h-[calc(100vh-4.25rem)] max-w-6xl items-center gap-10 px-4 pb-16 pt-10 sm:px-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)] lg:gap-14 lg:pb-20 lg:pt-6">
        <div className="home-hero-copy relative z-10 max-w-xl">
          <p className="font-[family-name:var(--font-display)] text-4xl leading-none tracking-tight text-slate-900 sm:text-5xl md:text-6xl dark:text-white">
            A2N Gallery
          </p>
          <h1 className="mt-5 text-balance text-xl font-semibold leading-snug text-slate-800 sm:text-2xl dark:text-slate-100">
            Convert, identify, and keep every photo in one place.
          </h1>
          <p className="mt-4 max-w-md text-pretty text-base leading-relaxed text-slate-600 dark:text-slate-300">
            Turn any JPEG or PNG into lean WebP variants, ask AI what&apos;s in
            the frame, then sign in to own your library—originals and all sizes
            together.
          </p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link
              href="/converter"
              className="home-cta inline-flex cursor-pointer items-center gap-2 rounded-xl bg-violet-600 px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-violet-600/25 transition hover:bg-violet-500"
            >
              <ImageIcon className="h-4 w-4" />
              WebP Converter
              <ArrowRight className="h-4 w-4 opacity-70" />
            </Link>
            <Link
              href="/identify"
              className="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-sm font-semibold text-slate-800 shadow-sm transition hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800"
            >
              <ScanSearch className="h-4 w-4" />
              Object Identifier
            </Link>
          </div>
        </div>

        <div
          className="home-hero-stage relative mx-auto aspect-[4/3] w-full max-w-lg lg:max-w-none"
          aria-hidden
        >
          <div className="absolute inset-[8%] rounded-[1.75rem] bg-slate-200/80 shadow-2xl shadow-slate-900/10 ring-1 ring-slate-300/60 dark:bg-slate-800/80 dark:shadow-black/40 dark:ring-slate-700/80" />
          <div className="home-frame home-frame-a absolute left-[4%] top-[12%] h-[58%] w-[42%] overflow-hidden rounded-2xl bg-gradient-to-br from-violet-200 via-slate-100 to-slate-300 shadow-xl ring-1 ring-white/70 dark:from-violet-900 dark:via-slate-800 dark:to-slate-950 dark:ring-white/10">
            <div className="absolute inset-3 rounded-xl bg-[linear-gradient(135deg,rgba(255,255,255,0.55),transparent_45%),linear-gradient(to_bottom,transparent_40%,rgba(15,23,42,0.25))]" />
            <div className="absolute bottom-3 left-3 right-3 h-1.5 rounded-full bg-white/50 dark:bg-white/20" />
          </div>
          <div className="home-frame home-frame-b absolute right-[6%] top-[6%] h-[48%] w-[46%] overflow-hidden rounded-2xl bg-gradient-to-bl from-fuchsia-100 via-slate-200 to-violet-100 shadow-xl ring-1 ring-white/70 dark:from-fuchsia-950 dark:via-slate-800 dark:to-violet-950 dark:ring-white/10">
            <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_25%,rgba(255,255,255,0.7),transparent_45%)]" />
            <div className="absolute left-4 top-4 h-8 w-8 rounded-full border border-white/60 bg-white/30 dark:border-white/20 dark:bg-white/10" />
          </div>
          <div className="home-frame home-frame-c absolute bottom-[8%] right-[10%] h-[36%] w-[52%] overflow-hidden rounded-2xl bg-gradient-to-tr from-slate-300 via-violet-50 to-slate-100 shadow-xl ring-1 ring-white/70 dark:from-slate-900 dark:via-violet-950 dark:to-slate-800 dark:ring-white/10">
            <div className="absolute inset-x-4 top-1/2 h-px -translate-y-1/2 bg-slate-400/40 dark:bg-white/15" />
            <div className="absolute inset-y-4 left-1/2 w-px -translate-x-1/2 bg-slate-400/40 dark:bg-white/15" />
          </div>
          <div className="home-badge absolute -left-1 bottom-[22%] rounded-full border border-slate-200 bg-white/95 px-3 py-1.5 text-[11px] font-semibold text-slate-700 shadow-md backdrop-blur dark:border-slate-600 dark:bg-slate-900/95 dark:text-slate-200">
            WebP · AI · Gallery
          </div>
        </div>
      </section>
    </main>
  );
}
