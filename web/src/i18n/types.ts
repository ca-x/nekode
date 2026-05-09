import type en from "./locales/en.json";

/** Canonical catalog shape, derived from the English source of truth. */
export type LocaleCatalog = typeof en;

/** Supported locale codes. Keep in sync with locales/registry.ts. */
export type Locale = "en" | "zh-CN";

/**
 * MessageKey is every dotted path into the catalog, e.g.
 * "nav.messages" or "empty.noChannelSelected.messagesBody".
 *
 * The recursive mapped type below computes the union automatically from
 * LocaleCatalog so adding a new JSON key immediately widens MessageKey,
 * and removing one breaks the TypeScript build in every call site.
 */
type Primitive = string | number | boolean | null | undefined;

type Join<K, P> = K extends string
  ? P extends string
    ? `${K}.${P}`
    : never
  : never;

type Paths<T> = T extends Primitive
  ? never
  : {
      [K in keyof T & string]: T[K] extends Primitive ? K : K | Join<K, Paths<T[K]>>;
    }[keyof T & string];

export type MessageKey = Paths<LocaleCatalog>;
