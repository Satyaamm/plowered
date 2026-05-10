"use client";

import { PageHeader } from "@/components/page-header";
import { ComingSoon } from "@/components/coming-soon";

export default function IdentityPage() {
  return (
    <>
      <PageHeader
        title="Identity"
        subtitle="Users, sessions, API keys, MFA enrollment, group memberships."
        crumbs={[{ label: "Home", href: "/" }, { label: "Management" }, { label: "Identity" }]}
      />
      <ComingSoon what="Identity & access" />
    </>
  );
}
