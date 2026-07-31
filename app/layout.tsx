import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Wasted Cycles — Wall-clock profiler for coding agents",
  description:
    "See where Codex, Claude Code, Cursor, and Grok Build runs spend time outside model reasoning.",
  metadataBase: new URL("https://zozo123.github.io/wasted-cycles/"),
  openGraph: {
    title: "Wasted Cycles",
    description: "Find where coding-agent runs stop coding.",
    type: "website",
    url: "https://zozo123.github.io/wasted-cycles/",
    images: [{ url: "/og.png", width: 1731, height: 909, alt: "Wasted Cycles terminal histogram" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "Wasted Cycles",
    description: "Find where coding-agent runs stop coding.",
    images: ["/og.png"],
  },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className={`${geistSans.variable} ${geistMono.variable}`}>{children}</body>
    </html>
  );
}
