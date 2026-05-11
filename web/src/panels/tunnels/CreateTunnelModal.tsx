import { useEffect, useState } from "react";
import type { TunnelAccessPolicy, TunnelRecord } from "../../types";
import { useT } from "../../i18n/use-t";
import { ModalShell } from "../_shared/modal-shell";
import { CopyButton } from "../_shared/copy-button";
import { AlertPill } from "../_shared/alert-pill";

const TTL_PRESETS: ReadonlyArray<{ label: string; value: number }> = [
  { label: "30m", value: 30 * 60 },
  { label: "2h", value: 2 * 60 * 60 },
  { label: "8h", value: 8 * 60 * 60 },
  { label: "24h", value: 24 * 60 * 60 }
];

export function CreateTunnelModal({
  open,
  onClose,
  onSubmit
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (form: {
    localPort: number;
    label: string;
    accessPolicy: TunnelAccessPolicy;
    ttlSeconds: number;
  }) => void;
}) {
  const { t } = useT();
  const [port, setPort] = useState("3000");
  const [label, setLabel] = useState("");
  const [policy, setPolicy] = useState<TunnelAccessPolicy>("members");
  const [ttl, setTtl] = useState<number>(2 * 60 * 60);

  useEffect(() => {
    if (!open) {
      setPort("3000");
      setLabel("");
      setPolicy("members");
      setTtl(2 * 60 * 60);
    }
  }, [open]);

  const portNumber = Number(port);
  const portValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;

  return (
    <ModalShell
      open={open}
      title={t("tunnels.create.title")}
      onClose={onClose}
      footer={
        <div className="modal-shell-footer-row">
          <button className="ghost-button" type="button" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button
            className="primary-button"
            type="button"
            disabled={!portValid}
            onClick={() =>
              onSubmit({ localPort: portNumber, label: label.trim(), accessPolicy: policy, ttlSeconds: ttl })
            }
          >
            {t("tunnels.create.submit")}
          </button>
        </div>
      }
    >
      <label className="form-field">
        <span>{t("tunnels.create.portLabel")}</span>
        <input
          type="number"
          inputMode="numeric"
          min={1}
          max={65535}
          value={port}
          onChange={(event) => setPort(event.target.value)}
          autoFocus
        />
      </label>
      <label className="form-field">
        <span>{t("tunnels.create.labelLabel")}</span>
        <input
          value={label}
          onChange={(event) => setLabel(event.target.value)}
          placeholder={t("tunnels.create.labelPlaceholder")}
        />
      </label>
      <label className="form-field">
        <span>{t("tunnels.create.policyLabel")}</span>
        <select value={policy} onChange={(event) => setPolicy(event.target.value as TunnelAccessPolicy)}>
          <option value="members">{t("tunnels.accessPolicy.members")}</option>
          <option value="private">{t("tunnels.accessPolicy.private")}</option>
          <option value="public">{t("tunnels.accessPolicy.public")}</option>
        </select>
      </label>
      <label className="form-field">
        <span>{t("tunnels.create.ttlLabel")}</span>
        <div className="tunnel-ttl-group" role="radiogroup">
          {TTL_PRESETS.map((entry) => (
            <button
              key={entry.value}
              type="button"
              role="radio"
              aria-checked={ttl === entry.value}
              className={ttl === entry.value ? "primary-button" : "ghost-button"}
              onClick={() => setTtl(entry.value)}
            >
              {entry.label}
            </button>
          ))}
        </div>
      </label>
    </ModalShell>
  );
}

export function CreatedTunnelModal({
  tunnel,
  onClose
}: {
  tunnel: TunnelRecord | null;
  onClose: () => void;
}) {
  const { t } = useT();
  if (!tunnel) return null;
  return (
    <ModalShell
      open={Boolean(tunnel)}
      title={t("tunnels.created.title")}
      onClose={onClose}
      footer={
        <div className="modal-shell-footer-row">
          <button className="primary-button" type="button" onClick={onClose}>
            {t("common.done")}
          </button>
        </div>
      }
    >
      <AlertPill tone="info" label={t("tunnels.created.hint")} />
      <label className="form-field">
        <span>{t("tunnels.created.urlLabel")}</span>
        <div className="tunnel-url-row">
          <input value={tunnel.publicUrl} readOnly />
          <CopyButton value={tunnel.publicUrl} ariaLabel={t("common.copy")} />
        </div>
      </label>
    </ModalShell>
  );
}
