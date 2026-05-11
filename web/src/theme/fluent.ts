// Fluent UI theme objects — client-only. Server components MUST NOT
// import this file because Fluent UI's runtime calls React.createContext
// at module load and breaks RSC builds.

import {
  createLightTheme,
  createDarkTheme,
  type BrandVariants,
  type Theme,
} from "@fluentui/react-components";

// 16-step brand ramp anchored on Azure blue (#0078D4 at 80) — the
// signature colour Microsoft uses across the Azure portal, Azure docs,
// and the Fluent design system. Light → dark.
export const ploweredBrand: BrandVariants = {
  10:  "#EFF6FC",
  20:  "#DEEDF8",
  30:  "#C7E0F4",
  40:  "#A6D1F0",
  50:  "#7EBCE7",
  60:  "#4FA3DC",
  70:  "#2589CA",
  80:  "#0078D4",
  90:  "#106EBE",
  100: "#005A9E",
  110: "#004C87",
  120: "#003E6F",
  130: "#003159",
  140: "#002647",
  150: "#001D38",
  160: "#00152B",
};

export const ploweredLight: Theme = {
  ...createLightTheme(ploweredBrand),
  // Azure-style neutral surface palette: pure white content, warm-grey
  // chrome for sidebars and panels, light grey separators.
  colorNeutralBackground1: "#FFFFFF",
  colorNeutralBackground2: "#F8F9FA",
  colorNeutralBackground3: "#F3F2F1",
  colorNeutralStroke1:     "#E1DFDD",
  colorNeutralStroke2:     "#EDEBE9",
};

export const ploweredDark: Theme = {
  ...createDarkTheme(ploweredBrand),
  colorNeutralBackground1: "#1B1B1B",
  colorNeutralBackground2: "#242424",
  colorNeutralBackground3: "#2D2D2D",
};
