"use client";

import {
  Button,
  Caption1,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Info20Regular } from "@fluentui/react-icons";

// PageIntro renders a single info icon that, on click, opens a popover
// explaining what the page is, what the user gets, and how to start.
// Drops into a PageHeader's `actions` slot. New users get the context
// when they want it; veterans never see a banner.

const useStyles = makeStyles({
  surface: {
    maxWidth: "360px",
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    padding: "14px 16px",
  },
  title: { fontWeight: 600, fontSize: "14px" },
  body: {
    fontSize: "13px",
    color: tokens.colorNeutralForeground2,
    lineHeight: 1.5,
  },
  list: {
    margin: 0,
    paddingLeft: "18px",
    display: "flex",
    flexDirection: "column",
    gap: "4px",
  },
  cta: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    paddingTop: "6px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    marginTop: "2px",
  },
});

export function PageIntro({
  title,
  body,
  bullets,
  cta,
}: {
  title: string;
  body: string;
  bullets: string[];
  cta?: string;
}) {
  const styles = useStyles();
  return (
    <Popover withArrow positioning="below-end">
      <PopoverTrigger disableButtonEnhancement>
        <Button
          appearance="subtle"
          icon={<Info20Regular />}
          aria-label={`About this page: ${title}`}
          title="What is this page?"
          data-tour="page-intro"
        />
      </PopoverTrigger>
      <PopoverSurface className={styles.surface}>
        <Text className={styles.title}>{title}</Text>
        <Text className={styles.body}>{body}</Text>
        <ul className={styles.list}>
          {bullets.map((b) => (
            <li key={b} className={styles.body}>
              {b}
            </li>
          ))}
        </ul>
        {cta && <Caption1 className={styles.cta}>{cta}</Caption1>}
      </PopoverSurface>
    </Popover>
  );
}
