import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "./providers";
import { Header } from "@/components/header";

export const metadata: Metadata = {
  title: "Plowered",
  description: "Open context layer for AI agents",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Providers>
          <Header />
          <main className="mx-auto max-w-5xl px-6 py-8">{children}</main>
        </Providers>
      </body>
    </html>
  );
}
