// Plowered design tokens — single source of truth for color, spacing, and
// typography. Anything visual in src/ should reach for these tokens, not
// hard-coded hex values.
//
// Theme: "Loamy" — warm terracotta on cream. Distinctive against the sea of
// blue developer tools, on-brand with the plow/earth metaphor.

import {
  createLightTheme,
  createDarkTheme,
  type BrandVariants,
  type Theme,
} from "@fluentui/react-components";

// 16-step brand ramp generated around the primary terracotta (#B8521B at 80).
// Light → dark.
export const ploweredBrand: BrandVariants = {
  10:  "#FBF1EB",
  20:  "#F7E2D4",
  30:  "#F3D2BC",
  40:  "#EFC2A4",
  50:  "#E9AE85",
  60:  "#DF9762",
  70:  "#D27C44",
  80:  "#B8521B",
  90:  "#A14918",
  100: "#884015",
  110: "#6E3611",
  120: "#552B0E",
  130: "#3D2008",
  140: "#2A1605",
  150: "#1A0E03",
  160: "#100802",
};

export const ploweredLight: Theme = {
  ...createLightTheme(ploweredBrand),
  // Surface overrides: shift the canvas warm so it reads "cream", not "ash".
  colorNeutralBackground1: "#FAF6F0",
  colorNeutralBackground2: "#F4ECDF",
  colorNeutralBackground3: "#EDE2D0",
};

export const ploweredDark: Theme = {
  ...createDarkTheme(ploweredBrand),
  colorNeutralBackground1: "#1A1410",
  colorNeutralBackground2: "#221A14",
  colorNeutralBackground3: "#2B201A",
};

// Spacing tokens — Fluent provides `tokens.spacingHorizontalM` etc., but a
// few app-specific gaps live here so layout choices are reviewable.
export const layout = {
  pageMaxWidth: "1024px",
  pagePaddingY: "32px",
  pagePaddingX: "24px",
  cardGap: "12px",
};
