import type { ReactNode } from "react";

export type KeyValue = {
  label: string;
  value: ReactNode;
  /** Used for aria-describedby and automation. Defaults to label. */
  id?: string;
  /** Optional helper text shown below the value in muted type. */
  hint?: string;
  /** Treat this row as tabular numerals (versions, counts, timestamps). */
  monospace?: boolean;
};

/**
 * Stacked definition list for INFO / RUNTIME CONFIG blocks. Reused by
 * Computer detail and Agent detail so all metadata rows line up even when
 * the label lengths differ between languages.
 */
export function KeyValueList({ items }: { items: readonly KeyValue[] }) {
  return (
    <dl className="kv-list">
      {items.map((item) => (
        <div className="kv-row" key={item.id ?? item.label}>
          <dt className="kv-label">{item.label}</dt>
          <dd className={item.monospace ? "kv-value tabular-nums" : "kv-value"}>
            {item.value}
            {item.hint ? <span className="kv-hint">{item.hint}</span> : null}
          </dd>
        </div>
      ))}
    </dl>
  );
}
