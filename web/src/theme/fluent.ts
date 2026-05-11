// Fluent UI theme objects — client-only. Server components MUST NOT
// import this file because Fluent UI's runtime calls React.createContext
// at module load and breaks RSC builds.

import {
  createLightTheme,
  createDarkTheme,
  type BrandVariants,
  type Theme,
} from "@fluentui/react-components";

// 16-step brand ramp anchored on Cloudflare orange (#F38020 at 80).
// Same shade Cloudflare uses across cloudflare.com, the dashboard, and
// their developer docs. Light → dark.
export const ploweredBrand: BrandVariants = {
  10:  "#FEF4E8",
  20:  "#FDE6CC",
  30:  "#FCD5A8",
  40:  "#FBC388",
  50:  "#F9AE65",
  60:  "#F69842",
  70:  "#F38C2A",
  80:  "#F38020",
  90:  "#D86E18",
  100: "#BC5E10",
  110: "#9F4F0A",
  120: "#824006",
  130: "#663104",
  140: "#4D2502",
  150: "#371A01",
  160: "#221001",
};

export const ploweredLight: Theme = {
  ...createLightTheme(ploweredBrand),
  // Cloudflare-style neutral palette: pure white content area, warm
  // light-grey chrome for sidebars and panels, light grey separators.
  colorNeutralBackground1: "#FFFFFF",
  colorNeutralBackground2: "#FAFAFA",
  colorNeutralBackground3: "#F5F5F5",
  colorNeutralStroke1:     "#E5E7EB",
  colorNeutralStroke2:     "#EDEDED",
};

export const ploweredDark: Theme = {
  ...createDarkTheme(ploweredBrand),
  colorNeutralBackground1: "#1B1B1B",
  colorNeutralBackground2: "#242424",
  colorNeutralBackground3: "#2D2D2D",
};
