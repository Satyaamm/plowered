import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "./providers";

const APP_NAME = process.env.NEXT_PUBLIC_APP_NAME ?? "PurpleCube AI Studio";

export const metadata: Metadata = {
  title: APP_NAME,
  description: "PurpleCube AI Studio — data context platform for catalog, governance, and AI-native lineage.",
};

// The root layout is intentionally minimal: only providers + body. The
// (authed) and (public) groups own their own chrome — sidebar+topbar
// for authed routes, centered card for login/signup/verify.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body style={{ margin: 0 }}>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
