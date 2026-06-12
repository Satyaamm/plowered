// Public shell — login, signup, forgot/reset, verify, accept-invite.
// AuthShell renders its own full-bleed split layout (left form pane,
// right animated lineage pane), so this wrapper just hands the page
// through without imposing extra chrome.
export default function PublicLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}
