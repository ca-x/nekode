import { useState } from "react";
import { Check, Clipboard } from "lucide-react";
import { useT } from "../../i18n/use-t";

export function CopyButton({
  value,
  ariaLabel,
  size = 16,
  className
}: {
  value: string;
  ariaLabel: string;
  size?: number;
  className?: string;
}) {
  const { t } = useT();
  const [state, setState] = useState<"idle" | "copied" | "error">("idle");

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setState("copied");
      window.setTimeout(() => setState("idle"), 1500);
    } catch {
      setState("error");
      window.setTimeout(() => setState("idle"), 2000);
    }
  };

  const label =
    state === "copied"
      ? t("computer.connect.copied")
      : state === "error"
        ? t("computer.connect.copyFailed")
        : t("computer.connect.copy");

  return (
    <button
      type="button"
      className={className ?? "copy-button"}
      aria-label={ariaLabel}
      title={label}
      onClick={() => void onCopy()}
      disabled={!value}
    >
      {state === "copied" ? <Check size={size} aria-hidden="true" /> : <Clipboard size={size} aria-hidden="true" />}
      <span>{label}</span>
    </button>
  );
}
