import { tokens } from "@fluentui/react-components";

// Public shell — login, signup, verify all live here. No sidebar, no
// topbar; one centered card on a soft cream canvas. Mirrors Azure's
// portal sign-in: white surface, single primary action, branded chrome.
export default function PublicLayout({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        minHeight: "100vh",
        display: "grid",
        gridTemplateColumns: "1fr",
        background: "#FAF6F0",
      }}
    >
      {children}
    </div>
  );
}
