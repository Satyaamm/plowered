import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "./providers";
import { Header } from "@/components/header";
import { layout } from "@/theme";

const APP_NAME = process.env.NEXT_PUBLIC_APP_NAME ?? "plowered";

export const metadata: Metadata = {
  title: APP_NAME,
  description: "Open context layer for AI agents",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Providers>
          <Header appName={APP_NAME} />
          <main
            style={{
              maxWidth: layout.pageMaxWidth,
              margin: "0 auto",
              padding: `${layout.pagePaddingY} ${layout.pagePaddingX}`,
            }}
          >
            {children}
          </main>
        </Providers>
      </body>
    </html>
  );
}
