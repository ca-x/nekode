import { useCallback, useEffect, type ReactNode } from "react";
import { X } from "lucide-react";

/**
 * Modal overlay with focus trap fallback, Esc-to-close, and click-outside
 * dismissal. Used by "Add computer" wizard and other one-shot dialogs.
 */
export function ModalShell({
  open,
  title,
  onClose,
  footer,
  children,
  maxWidth = "520px",
  dismissOnBackdropClick = true
}: {
  open: boolean;
  title: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  children: ReactNode;
  maxWidth?: string;
  dismissOnBackdropClick?: boolean;
}) {
  const handleKey = useCallback(
    (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    },
    [onClose]
  );

  useEffect(() => {
    if (!open) return undefined;
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [handleKey, open]);

  if (!open) return null;

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={dismissOnBackdropClick ? onClose : undefined}
    >
      <div
        className="modal-shell"
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === "string" ? title : undefined}
        style={{ maxWidth }}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="modal-shell-header">
          <h2 className="modal-shell-title">{title}</h2>
          <button type="button" className="modal-shell-close" aria-label="Close" onClick={onClose}>
            <X size={18} aria-hidden="true" />
          </button>
        </header>
        <div className="modal-shell-body">{children}</div>
        {footer ? <footer className="modal-shell-footer">{footer}</footer> : null}
      </div>
    </div>
  );
}
