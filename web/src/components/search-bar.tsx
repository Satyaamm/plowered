"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { Input, Button, makeStyles } from "@fluentui/react-components";
import { Search24Regular } from "@fluentui/react-icons";

const useStyles = makeStyles({
  form: {
    display: "flex",
    gap: "8px",
    width: "100%",
    maxWidth: "640px",
  },
  input: { flex: 1 },
});

export function SearchBar({ initial = "" }: { initial?: string }) {
  const styles = useStyles();
  const router = useRouter();
  const [q, setQ] = useState(initial);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!q.trim()) return;
    router.push(`/search?q=${encodeURIComponent(q.trim())}`);
  }

  return (
    <form onSubmit={onSubmit} className={styles.form}>
      <Input
        className={styles.input}
        value={q}
        onChange={(_, data) => setQ(data.value)}
        placeholder="Search assets — table name, qualified name, or keyword"
        contentBefore={<Search24Regular />}
      />
      <Button type="submit" appearance="primary">Search</Button>
    </form>
  );
}
