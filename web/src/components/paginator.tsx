"use client";

import {
  Button,
  Dropdown,
  Option,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ChevronLeft20Regular,
  ChevronRight20Regular,
} from "@fluentui/react-icons";

const useStyles = makeStyles({
  root: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "12px",
    padding: "10px 14px",
    backgroundColor: tokens.colorNeutralBackground1,
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
  },
  group: { display: "flex", alignItems: "center", gap: "8px" },
  sizeBox: { minWidth: "84px" },
});

export const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

/**
 * Industry-standard list-page footer: page-size selector on the left,
 * "Showing X-Y of Z" in the middle, prev/next on the right.
 *
 * Designed for client-side pagination over an already-fetched array.
 * For cursor pagination we can wire onNext/onPrev to a backend cursor
 * instead — same component, different data source.
 */
export function Paginator({
  total,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
  sizeOptions = PAGE_SIZE_OPTIONS,
}: {
  total: number;
  page: number; // 0-based
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
  sizeOptions?: readonly number[];
}) {
  const styles = useStyles();
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : page * pageSize + 1;
  const to = Math.min(total, (page + 1) * pageSize);

  return (
    <div className={styles.root}>
      <div className={styles.group}>
        <span>Rows per page</span>
        <Dropdown
          className={styles.sizeBox}
          size="small"
          value={String(pageSize)}
          selectedOptions={[String(pageSize)]}
          onOptionSelect={(_, d) => {
            const n = Number(d.optionValue);
            if (!Number.isNaN(n)) {
              onPageSizeChange(n);
              onPageChange(0);
            }
          }}
        >
          {sizeOptions.map((n) => (
            <Option key={n} value={String(n)}>
              {String(n)}
            </Option>
          ))}
        </Dropdown>
      </div>

      <Text>
        {total === 0
          ? "No rows"
          : `Showing ${from}–${to} of ${total}`}
      </Text>

      <div className={styles.group}>
        <Button
          size="small"
          appearance="subtle"
          icon={<ChevronLeft20Regular />}
          onClick={() => onPageChange(Math.max(0, page - 1))}
          disabled={page <= 0}
          aria-label="Previous page"
        />
        <Text>
          Page {page + 1} of {pageCount}
        </Text>
        <Button
          size="small"
          appearance="subtle"
          icon={<ChevronRight20Regular />}
          onClick={() => onPageChange(Math.min(pageCount - 1, page + 1))}
          disabled={page >= pageCount - 1}
          aria-label="Next page"
        />
      </div>
    </div>
  );
}
