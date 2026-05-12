// Fluent UI theme objects — client-only. Server components MUST NOT
// import this file because Fluent UI's runtime calls React.createContext
// at module load and breaks RSC builds.

import {
  createLightTheme,
  createDarkTheme,
  type BrandVariants,
  type Theme,
} from "@fluentui/react-components";

// 16-step brand ramp anchored on vivid violet (#7C3AED at 80) for the
// PurpleCube AI Studio brand. Light → dark.
export const ploweredBrand: BrandVariants = {
  10:  "#F3EEFE",
  20:  "#E4D9FD",
  30:  "#D2BFFB",
  40:  "#BFA4F8",
  50:  "#AB89F4",
  60:  "#996FF0",
  70:  "#895AEC",
  80:  "#7C3AED",
  90:  "#6D28D9",
  100: "#5B21B6",
  110: "#4C1D95",
  120: "#3D1A77",
  130: "#2E1559",
  140: "#211040",
  150: "#160B2B",
  160: "#0B071A",
};

export const ploweredLight: Theme = {
  ...createLightTheme(ploweredBrand),
  // Neutral palette: pure white content area, light-grey chrome for
  // sidebars and panels, soft grey separators.
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
