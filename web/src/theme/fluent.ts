// Fluent UI theme objects — client-only. Server components MUST NOT
// import this file because Fluent UI's runtime calls React.createContext
// at module load and breaks RSC builds.

import {
  createLightTheme,
  createDarkTheme,
  type BrandVariants,
  type Theme,
} from "@fluentui/react-components";

// 16-step brand ramp generated around the primary terracotta (#B8521B at 80).
// Light → dark.
export const ploweredBrand: BrandVariants = {
  10: "#FBF1EB",
  20: "#F7E2D4",
  30: "#F3D2BC",
  40: "#EFC2A4",
  50: "#E9AE85",
  60: "#DF9762",
  70: "#D27C44",
  80: "#B8521B",
  90: "#A14918",
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
