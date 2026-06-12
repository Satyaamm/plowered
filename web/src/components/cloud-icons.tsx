"use client";

// Brand SVG icons for the cloud-integrations surface. Hand-drawn
// geometric marks in each vendor's official palette — consistent shape
// language (cylinders = databases/storage, envelopes = email, bolts =
// caches, sparkles = AI, magnifiers = query engines) so a scan of the
// page reads by shape first, vendor colour second.
//
// All icons share a 28x28 viewBox and accept an optional size prop.

import { ReactNode } from "react";

interface IconProps {
  size?: number;
}

function Svg({ size = 24, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 28 28"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

// ---- shape primitives -------------------------------------------------

function Cylinder({ color, accent }: { color: string; accent?: string }) {
  return (
    <>
      <ellipse cx="14" cy="7" rx="9" ry="3.5" fill={accent ?? color} />
      <path d="M5 7v14c0 1.93 4.03 3.5 9 3.5s9-1.57 9-3.5V7" fill={color} />
      <ellipse cx="14" cy="7" rx="9" ry="3.5" fill={accent ?? color} stroke="rgba(255,255,255,0.45)" strokeWidth="1" />
      <path d="M5 12.5c0 1.93 4.03 3.5 9 3.5s9-1.57 9-3.5" stroke="rgba(255,255,255,0.35)" strokeWidth="1" />
      <path d="M5 17.5c0 1.93 4.03 3.5 9 3.5s9-1.57 9-3.5" stroke="rgba(255,255,255,0.35)" strokeWidth="1" />
    </>
  );
}

function Bucket({ color, lid }: { color: string; lid?: string }) {
  return (
    <>
      <path d="M5 8l2.2 15.2c.1.9 3.1 2.3 6.8 2.3s6.7-1.4 6.8-2.3L23 8" fill={color} />
      <ellipse cx="14" cy="8" rx="9" ry="3.2" fill={lid ?? color} stroke="rgba(255,255,255,0.5)" strokeWidth="1" />
      <ellipse cx="14" cy="8" rx="5" ry="1.7" fill="rgba(255,255,255,0.3)" />
    </>
  );
}

function Envelope({ color, flap }: { color: string; flap?: string }) {
  return (
    <>
      <rect x="3" y="7" width="22" height="15" rx="2" fill={color} />
      <path d="M3.5 8.5L14 16l10.5-7.5" stroke={flap ?? "rgba(255,255,255,0.85)"} strokeWidth="1.6" fill="none" />
    </>
  );
}

function Bolt({ color, bg }: { color: string; bg: string }) {
  return (
    <>
      <rect x="3" y="3" width="22" height="22" rx="5" fill={bg} />
      <path d="M15.5 5.5L9 15.5h4.2L12 22.5l6.8-10.4h-4.4l1.1-6.6z" fill={color} />
    </>
  );
}

function Sparkle({ color, bg }: { color: string; bg?: string }) {
  return (
    <>
      {bg && <rect x="3" y="3" width="22" height="22" rx="5" fill={bg} />}
      <path
        d="M14 5l2.1 6.4L22.5 14l-6.4 2.1L14 22.5l-2.1-6.4L5.5 14l6.4-2.6L14 5z"
        fill={color}
      />
      <circle cx="21" cy="7" r="2" fill={color} opacity="0.7" />
    </>
  );
}

function Magnifier({ color, bg }: { color: string; bg?: string }) {
  return (
    <>
      {bg && <rect x="3" y="3" width="22" height="22" rx="5" fill={bg} />}
      <circle cx="12.5" cy="12.5" r="6" stroke={color} strokeWidth="2.4" fill="none" />
      <path d="M17 17l5.5 5.5" stroke={color} strokeWidth="2.6" strokeLinecap="round" />
    </>
  );
}

function Blocks({ color }: { color: string }) {
  return (
    <>
      <rect x="4" y="14" width="9" height="9" rx="1.5" fill={color} />
      <rect x="15" y="14" width="9" height="9" rx="1.5" fill={color} opacity="0.75" />
      <rect x="9.5" y="4" width="9" height="9" rx="1.5" fill={color} opacity="0.55" />
    </>
  );
}

function DiamondStack({ color }: { color: string }) {
  return (
    <>
      <path d="M14 3l8 4.5-8 4.5-8-4.5L14 3z" fill={color} />
      <path d="M14 11.5l8 4.5-8 4.5-8-4.5 8-4.5z" fill={color} opacity="0.75" />
      <path d="M6 16l8 4.5L22 16" stroke={color} strokeWidth="1.4" fill="none" opacity="0.5" />
    </>
  );
}

// ---- cloud provider logos --------------------------------------------

export function AWSLogo({ size = 24 }: IconProps) {
  return (
    <Svg size={size}>
      <text
        x="14"
        y="14"
        textAnchor="middle"
        fontFamily="ui-sans-serif, system-ui, sans-serif"
        fontWeight="800"
        fontSize="11"
        fill="#232F3E"
        letterSpacing="-0.5"
      >
        aws
      </text>
      {/* the smile swoosh with arrow tip */}
      <path
        d="M4 18.5c3 2.6 7.2 4 10.5 4 3 0 6.3-.9 8.5-2.4"
        stroke="#FF9900"
        strokeWidth="2"
        strokeLinecap="round"
        fill="none"
      />
      <path d="M22.2 17.5l2.6 1.4-1.7 2.4-.9-3.8z" fill="#FF9900" />
    </Svg>
  );
}

export function AzureLogo({ size = 24 }: IconProps) {
  return (
    <Svg size={size}>
      {/* stylised "A" — two angled facets, official Azure blues */}
      <path d="M13 4L5.5 23h5.6L18 4h-5z" fill="#0F6CBD" />
      <path d="M17.2 9.5L11 23h12L17.2 9.5z" fill="#2899F5" />
    </Svg>
  );
}

export function GCPLogo({ size = 24 }: IconProps) {
  return (
    <Svg size={size}>
      {/* simplified four-colour cloud */}
      <path d="M9 20a5 5 0 01-1.5-9.8" stroke="#EA4335" strokeWidth="2.6" strokeLinecap="round" fill="none" />
      <path d="M7.5 10.2A7 7 0 0119.5 8.5" stroke="#4285F4" strokeWidth="2.6" strokeLinecap="round" fill="none" />
      <path d="M19.5 8.5A5.5 5.5 0 0121 19.4" stroke="#FBBC05" strokeWidth="2.6" strokeLinecap="round" fill="none" />
      <path d="M21 19.4c-.5.4-1.3.6-2 .6H9" stroke="#34A853" strokeWidth="2.6" strokeLinecap="round" fill="none" />
    </Svg>
  );
}

// ---- AWS services ------------------------------------------------------

export const S3Icon = (p: IconProps) => (
  <Svg {...p}><Bucket color="#569A31" lid="#6CAE3E" /></Svg>
);
export const SESIcon = (p: IconProps) => (
  <Svg {...p}><Envelope color="#DD344C" /></Svg>
);
export const BedrockIcon = (p: IconProps) => (
  <Svg {...p}><Sparkle color="#FFFFFF" bg="#01A88D" /></Svg>
);
export const RDSIcon = (p: IconProps) => (
  <Svg {...p}><Cylinder color="#527FFF" accent="#7A9BFF" /></Svg>
);
export const ElastiCacheIcon = (p: IconProps) => (
  <Svg {...p}><Bolt color="#FFFFFF" bg="#C925D1" /></Svg>
);
export const AthenaIcon = (p: IconProps) => (
  <Svg {...p}><Magnifier color="#FFFFFF" bg="#8C4FFF" /></Svg>
);
export const RedshiftIcon = (p: IconProps) => (
  <Svg {...p}><Cylinder color="#8C4FFF" accent="#A97FFF" /></Svg>
);
export const DynamoDBIcon = (p: IconProps) => (
  <Svg {...p}><DiamondStack color="#527FFF" /></Svg>
);

// ---- Azure services ----------------------------------------------------

export const AzureBlobIcon = (p: IconProps) => (
  <Svg {...p}><Blocks color="#0078D4" /></Svg>
);
export const AzureOpenAIIcon = (p: IconProps) => (
  <Svg {...p}><Sparkle color="#FFFFFF" bg="#0078D4" /></Svg>
);
export const AzurePostgresIcon = (p: IconProps) => (
  <Svg {...p}><Cylinder color="#0078D4" accent="#2899F5" /></Svg>
);
export const AzureCacheIcon = (p: IconProps) => (
  <Svg {...p}><Bolt color="#FFFFFF" bg="#0078D4" /></Svg>
);
export const ACSEmailIcon = (p: IconProps) => (
  <Svg {...p}><Envelope color="#0078D4" /></Svg>
);
export const CosmosDBIcon = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="14" cy="14" r="9.5" stroke="#0078D4" strokeWidth="2" fill="none" />
    <ellipse cx="14" cy="14" rx="9.5" ry="3.8" stroke="#2899F5" strokeWidth="1.6" fill="none" />
    <circle cx="14" cy="14" r="3" fill="#0078D4" />
  </Svg>
);

// ---- GCP services -------------------------------------------------------

export const GCSIcon = (p: IconProps) => (
  <Svg {...p}><Bucket color="#4285F4" lid="#669DF6" /></Svg>
);
export const BigQueryIcon = (p: IconProps) => (
  <Svg {...p}><Magnifier color="#FFFFFF" bg="#4285F4" /></Svg>
);
export const VertexIcon = (p: IconProps) => (
  <Svg {...p}>
    <Sparkle color="#4285F4" />
    <circle cx="7" cy="21" r="2" fill="#EA4335" />
    <circle cx="21" cy="21" r="2" fill="#34A853" />
  </Svg>
);
export const CloudSQLIcon = (p: IconProps) => (
  <Svg {...p}><Cylinder color="#4285F4" accent="#669DF6" /></Svg>
);
export const MemorystoreIcon = (p: IconProps) => (
  <Svg {...p}><Bolt color="#FFFFFF" bg="#34A853" /></Svg>
);
export const FirestoreIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M6 9l8-4 8 4-8 4-8-4z" fill="#FFCA28" />
    <path d="M6 14.5l8-4 8 4-8 4-8-4z" fill="#F4B400" />
    <path d="M14 19.5l5-2.5 3 1.5-8 4v-3z" fill="#FFA000" />
  </Svg>
);

// ---- neutral / OSS ------------------------------------------------------

export const PostgresIcon = (p: IconProps) => (
  <Svg {...p}><Cylinder color="#336791" accent="#4A82AC" /></Svg>
);
export const RedisIcon = (p: IconProps) => (
  <Svg {...p}><DiamondStack color="#DC382D" /></Svg>
);
export const NATSIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="3" y="3" width="22" height="22" rx="5" fill="#27AAE1" />
    <path d="M8 19V9l8 7V9h4v10h-4l-8-7v7H8z" fill="#FFFFFF" transform="scale(0.78) translate(4 4)" />
  </Svg>
);
export const MinIOIcon = (p: IconProps) => (
  <Svg {...p}><Bucket color="#C72E49" lid="#E04A63" /></Svg>
);
export const ResendIcon = (p: IconProps) => (
  <Svg {...p}><Envelope color="#18181B" /></Svg>
);
export const SnowflakeIcon = (p: IconProps) => (
  <Svg {...p}>
    <g stroke="#29B5E8" strokeWidth="2" strokeLinecap="round">
      <path d="M14 4v20M5.3 9l17.4 10M5.3 19L22.7 9" />
    </g>
    <circle cx="14" cy="14" r="2.6" fill="#29B5E8" />
  </Svg>
);
export const MemoryIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="5" y="8" width="18" height="12" rx="2" fill="#9CA3AF" />
    <path d="M8 8V5.5M12 8V5.5M16 8V5.5M20 8V5.5M8 22.5V20M12 22.5V20M16 22.5V20M20 22.5V20" stroke="#9CA3AF" strokeWidth="1.6" />
    <rect x="9" y="12" width="10" height="4" rx="1" fill="rgba(255,255,255,0.6)" />
  </Svg>
);
export const LogIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="5" y="4" width="18" height="20" rx="2.5" fill="#6B7280" />
    <path d="M9 9.5h10M9 14h10M9 18.5h6" stroke="#FFFFFF" strokeWidth="1.8" strokeLinecap="round" />
  </Svg>
);
