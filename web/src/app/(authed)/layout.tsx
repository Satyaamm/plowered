import { Sidebar } from "@/components/sidebar";
import { Topbar } from "@/components/topbar";
import { RequireAuth } from "@/components/require-auth";
import { ProductTour } from "@/components/product-tour";

const APP_NAME = process.env.NEXT_PUBLIC_APP_NAME ?? "plowered";

// Authed shell: left rail + top bar + main content. RequireAuth blocks
// the children until /v1/auth/me returns a verified user; on 401 the
// browser is redirected to /login.
export default function AuthedLayout({ children }: { children: React.ReactNode }) {
  return (
    <RequireAuth>
      <div style={{ display: "flex", minHeight: "100vh", background: "#FFFFFF" }}>
        <Sidebar appName={APP_NAME} />
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
          <Topbar />
          <main style={{ flex: 1, overflowY: "auto", padding: "24px 32px" }}>
            {children}
          </main>
        </div>
        <ProductTour />
      </div>
    </RequireAuth>
  );
}
