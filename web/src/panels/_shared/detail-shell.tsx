import type { ReactNode } from "react";
import { TabBar, type TabDescriptor } from "./tab-bar";

/**
 * Shared layout for object detail pages (Computer / Agent). Renders a sticky
 * header + tab strip + scrollable body so every detail screen has the same
 * chrome and keyboard/tab behaviour. Callers supply the tab definitions and
 * the panel body per active tab.
 */
export function DetailShell<Key extends string>({
  eyebrow,
  title,
  subtitle,
  headerActions,
  tabs,
  activeTab,
  onChangeTab,
  banner,
  children
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  headerActions?: ReactNode;
  tabs: readonly TabDescriptor<Key>[];
  activeTab: Key;
  onChangeTab: (next: Key) => void;
  banner?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="detail-shell">
      <header className="detail-shell-header">
        <div className="detail-shell-heading">
          {eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}
          <h2 className="detail-shell-title">{title}</h2>
          {subtitle ? <p className="detail-shell-subtitle">{subtitle}</p> : null}
        </div>
        {headerActions ? <div className="detail-shell-actions">{headerActions}</div> : null}
      </header>
      {banner ? <div className="detail-shell-banner">{banner}</div> : null}
      <TabBar tabs={tabs} active={activeTab} onChange={onChangeTab} />
      <div
        className="detail-shell-body"
        role="tabpanel"
        id={`tab-panel-${activeTab}`}
        aria-labelledby={`tab-${activeTab}`}
      >
        {children}
      </div>
    </section>
  );
}
