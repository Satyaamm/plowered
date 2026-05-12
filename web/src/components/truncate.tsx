"use client";

import { Tooltip } from "@fluentui/react-components";

/**
 * Single-line truncation with a native browser tooltip on hover.
 * Use in table cells, lists, and any flex/grid layout where long
 * strings would otherwise overflow and bleed into the next column.
 *
 * `text` is required and is what the tooltip shows. `display` lets
 * you render styled content (badge + value, monospace, etc.) while
 * keeping the tooltip text raw.
 */
export function Truncate({
  text,
  max,
  className,
  style,
}: {
  text: string;
  max?: number;
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <Tooltip content={text} relationship="label" withArrow>
      <span
        className={className}
        style={{
          display: "inline-block",
          maxWidth: max ?? "100%",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
          verticalAlign: "middle",
          // Tiny right margin so adjacent column content has visual
          // breathing room even when the cell's own padding is small.
          paddingRight: "6px",
          ...style,
        }}
      >
        {text}
      </span>
    </Tooltip>
  );
}
