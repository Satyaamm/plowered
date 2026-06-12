"use client";

import { usePrincipal } from "./use-principal";

// Roles + verbs mirror the canonical matrix in
// internal/core/policy/policy.go. Keep these in lockstep — when the
// backend gains a new role / verb, update this file in the same PR.
//
// The browser uses these helpers only to *hide* UI that the backend
// would 403 anyway. Hiding a button is a UX nicety, not a security
// boundary: the gate that matters is the policy.Engine call in the
// http handler.

export type Role =
  | "viewer"
  | "editor"
  | "steward"
  | "admin"
  | "super_admin"
  | "platform_admin";
export type Verb =
  | "read"
  | "edit"
  | "propose"
  | "certify"
  | "delete"
  | "run"
  | "admin"
  | "purge"
  | "platform";

const ROLE_GRANTS: Record<Role, Set<Verb>> = {
  viewer: new Set<Verb>(["read"]),
  editor: new Set<Verb>(["read", "edit", "propose", "run"]),
  steward: new Set<Verb>(["read", "edit", "propose", "certify", "run"]),
  admin: new Set<Verb>([
    "read",
    "edit",
    "propose",
    "certify",
    "delete",
    "run",
    "admin",
  ]),
  super_admin: new Set<Verb>([
    "read",
    "edit",
    "propose",
    "certify",
    "delete",
    "run",
    "admin",
    "purge",
  ]),
  // platform_admin is the operator-of-the-platform — sees cross-tenant
  // feedback + system stats. Read everywhere so a triager can open the
  // asset/pipeline a bug references, plus the dedicated VerbPlatform
  // for the operator surfaces themselves.
  platform_admin: new Set<Verb>(["read", "platform"]),
};

function rolesHave(roles: string[], verb: Verb): boolean {
  for (const r of roles) {
    const grants = ROLE_GRANTS[r as Role];
    if (grants?.has(verb)) return true;
  }
  return false;
}

// useRole exposes role-aware helpers driven by GET /v1/auth/me. Pages
// use it to conditionally render destructive / privileged controls.
//
// Loading state matters: while the principal is still being fetched the
// `loading` flag is true and `can()` returns false. Always render the
// "loading skeleton" or omit the button in that window — flashing an
// admin-only button before it disappears is a worse UX than waiting.
export function useRole() {
  const { principal, loading, authenticated } = usePrincipal();
  const roles = (principal?.roles ?? []) as string[];

  return {
    loading,
    authenticated,
    roles,
    // hasRole is a strict equality check against the role string. Use
    // when a feature is gated by a *specific* role rather than the
    // ability matrix (eg. surfaces only super_admin sees).
    hasRole(role: Role): boolean {
      return roles.includes(role);
    },
    // can checks the verb against the role grants. Mirror of the Go
    // engine's role-grant table; matches `policy.roleAllows`.
    can(verb: Verb): boolean {
      return rolesHave(roles, verb);
    },
    // canAny returns true when the principal can perform any of the
    // given verbs — useful when a page surfaces multiple actions and
    // you only want to hide it when *all* are denied.
    canAny(...verbs: Verb[]): boolean {
      return verbs.some((v) => rolesHave(roles, v));
    },
  };
}
