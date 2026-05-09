import { useEffect, useState } from "react";
import type { DaemonEnrollment } from "../../types";
import { useT } from "../../i18n/use-t";
import { ModalShell } from "../_shared/modal-shell";
import { Stepper, type StepperStep } from "../_shared/stepper";
import { CopyButton } from "../_shared/copy-button";
import { AlertPill } from "../_shared/alert-pill";
import { enrollmentStatus } from "./computer-utils";

type StepKey = "name" | "install" | "wait";

/**
 * Three-step Add Computer wizard.
 *
 * Step 1 — name the computer.
 * Step 2 — show the install command; operator runs it on the target host.
 * Step 3 — poll the enrollment endpoint; flip to success when connected.
 *
 * The host owns the enrollment lifecycle (create / poll / revoke); this
 * component is a pure controlled view over a provided DaemonEnrollment.
 */
export function NewComputerModal({
  open,
  enrollment,
  error,
  busy,
  onClose,
  onCreate,
  onPoll,
  onRegenerate
}: {
  open: boolean;
  enrollment: DaemonEnrollment | null;
  error?: string;
  busy?: boolean;
  onClose: () => void;
  onCreate: (displayName: string) => Promise<void>;
  onPoll: () => Promise<void>;
  onRegenerate?: () => Promise<void>;
}) {
  const { t } = useT();
  const [displayName, setDisplayName] = useState("");
  const [step, setStep] = useState<StepKey>("name");

  const status = enrollmentStatus(enrollment);
  const isConnected = status === "connected";

  useEffect(() => {
    if (!open) {
      setDisplayName("");
      setStep("name");
    }
  }, [open]);

  useEffect(() => {
    if (enrollment && step === "name") {
      setStep("install");
    }
  }, [enrollment, step]);

  useEffect(() => {
    if (!open || step !== "install") return undefined;
    const timer = window.setInterval(() => {
      void onPoll();
    }, 3000);
    return () => window.clearInterval(timer);
  }, [onPoll, open, step]);

  useEffect(() => {
    if (isConnected) setStep("wait");
  }, [isConnected]);

  const steps: readonly StepperStep[] = [
    { id: "name", label: t("computer.wizard.step1Title") },
    { id: "install", label: t("computer.wizard.step2Title") },
    { id: "wait", label: t("computer.wizard.step3Title") }
  ];
  const activeIndex = step === "name" ? 0 : step === "install" ? 1 : 2;

  const submitName = (event: React.FormEvent) => {
    event.preventDefault();
    if (!displayName.trim()) return;
    void onCreate(displayName.trim());
  };

  const command = enrollment?.installCommand || "";

  return (
    <ModalShell
      open={open}
      title={t("computer.wizard.title")}
      onClose={onClose}
      footer={
        <div className="modal-shell-footer-row">
          <button className="ghost-button" type="button" onClick={onClose}>
            {isConnected ? t("computer.wizard.finish") : t("computer.wizard.close")}
          </button>
          {step === "install" && onRegenerate ? (
            <button className="ghost-button" type="button" onClick={() => void onRegenerate()} disabled={busy}>
              {t("computer.wizard.regenerate")}
            </button>
          ) : null}
        </div>
      }
    >
      <Stepper steps={steps} activeIndex={activeIndex} />
      {error ? <AlertPill tone="danger" label={error} /> : null}
      {step === "name" ? (
        <form className="wizard-form" onSubmit={submitName}>
          <label className="form-field">
            <span>{t("auth.displayName")}</span>
            <input
              type="text"
              autoFocus
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              disabled={busy}
            />
          </label>
          <p className="detail-muted">{t("computer.wizard.step1Hint")}</p>
          <button type="submit" className="primary-button" disabled={busy || !displayName.trim()}>
            {t("computer.wizard.create")}
          </button>
        </form>
      ) : null}
      {step === "install" ? (
        <div className="wizard-install">
          <p className="detail-muted">{t("computer.wizard.step2Hint")}</p>
          {command ? (
            <div className="command-row">
              <pre className="command-block" aria-label={t("computer.connect.title")}>
                <code>{command}</code>
              </pre>
              <CopyButton value={command} ariaLabel={t("computer.connect.copy")} />
            </div>
          ) : (
            <p className="detail-muted">—</p>
          )}
          <AlertPill tone="info" label={t("computer.wizard.waiting")} />
        </div>
      ) : null}
      {step === "wait" ? <AlertPill tone="info" label={t("computer.wizard.connected")} /> : null}
    </ModalShell>
  );
}
