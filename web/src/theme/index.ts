// Plowered design tokens — single source of truth for color, spacing, and
// typography. Anything visual in src/ should reach for these tokens, not
// hard-coded hex values.
//
// Theme: "Loamy" — warm terracotta on cream. Distinctive against the sea of
// blue developer tools, on-brand with the plow/earth metaphor.
//
// IMPORTANT: this barrel only re-exports the *server-safe* tokens (layout
// spacing, status colors). The Fluent UI theme objects live in
// `theme/fluent.ts` because importing Fluent UI into a Server Component
// breaks the RSC build with "createContext is not a function".

export { layout } from "./layout";
export * from "./status";
