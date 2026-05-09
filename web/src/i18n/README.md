# Web i18n module

All user-visible copy in the nekode web console lives here. Application
code must only depend on the public surface exported from `index.ts` and
`use-t.ts`. Don't import `i18next` directly from panels.

## Layout

```
index.ts           # i18next init + top-level exports (i18n, t, changeLocale)
provider.tsx       # <I18nProvider> — mount once at the app root
use-t.ts           # useT(), useLocale(), useFormat() hooks
switcher.tsx       # <LocaleSwitcher /> UI
types.ts           # Locale, MessageKey, LocaleCatalog (all typed from en.json)
locales/
  en.json          # source of truth
  zh-CN.json
  registry.ts      # locale metadata (code, label, nativeLabel, dir, intlTag)
```

## Adding or editing a string

1. Edit `locales/en.json` (source of truth).
2. Mirror the key into every other catalog in `locales/`. A missing key
   falls back to English at runtime, but the lint rule still flags any
   hardcoded JSX string, so don't skip this step.
3. Consume the key from a component through `useT()`:

   ```tsx
   import { useT } from "../i18n/use-t";

   function Example() {
     const { t } = useT();
     return <button>{t("common.save")}</button>;
   }
   ```

4. For dates, numbers, or relative times, reach for `useFormat()` —
   never hand-roll a string with the locale appended.

## Adding a locale

1. Add the catalog JSON under `locales/<code>.json`.
2. Register its descriptor in `locales/registry.ts`.
3. Add the code to the `Locale` union in `types.ts`.
4. Wire the catalog into the `resources` map in `index.ts`.

## Skipping translation on purpose

Some strings are intentionally locale-agnostic — command blocks,
version numbers, license text. Mark the JSX with a sibling comment:

```tsx
/* i18n-skip: command literal */
<code>nekode-daemon --version</code>
```

The lint rule below recognises that comment and leaves the literal
alone.

## Lint rule

`eslint-plugin-react-intl-translations` or a project-local rule runs
during `npm run lint` and fails the build on any untranslated JSX
string literal outside this folder. See `../../.eslintrc.cjs`.
