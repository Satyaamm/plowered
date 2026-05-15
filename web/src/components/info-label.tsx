"use client";

import { ReactNode } from "react";
import {
  Label,
  Tooltip,
  tokens,
  type LabelProps,
} from "@fluentui/react-components";
import { Info16Regular } from "@fluentui/react-icons";

/**
 * Hover-triggered drop-in replacement for Fluent v9's <InfoLabel>.
 *
 * Fluent's InfoLabel renders the "i" as a button — its popover only
 * opens on click. Users expect a "what does this do?" affordance to
 * just appear on hover, so we wrap a small info icon in <Tooltip>
 * (which is hover-by-default) and call it InfoLabel so callsites
 * stay identical.
 *
 * API matches the subset of Fluent's InfoLabel that the codebase
 * actually uses:
 *   <InfoLabel info="…">Field label text</InfoLabel>
 */
export interface InfoLabelProps extends Omit<LabelProps, "children"> {
  /** The hover tooltip body. Fluent's Tooltip slot only accepts text
   *  or a JSXElement; keeping this `string` is the contract that maps
   *  cleanly to it. */
  info: string;
  /** The visible label text. */
  children: ReactNode;
}

export function InfoLabel({ info, children, ...labelProps }: InfoLabelProps) {
  return (
    <Label {...labelProps}>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
        {children}
        <Tooltip
          content={info}
          relationship="description"
          withArrow
          positioning="above"
        >
          <Info16Regular
            tabIndex={0}
            style={{
              color: tokens.colorNeutralForeground3,
              cursor: "help",
              outline: "none",
            }}
            aria-label="More info"
          />
        </Tooltip>
      </span>
    </Label>
  );
}
