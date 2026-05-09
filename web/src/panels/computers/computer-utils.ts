import type { DaemonEnrollment, DaemonInventoryComputer, DaemonInventoryRuntime } from "../../types";

export type EnrollmentPlatform = "linux" | "macos" | "windows";

export type EnrollmentStatus =
  | "idle"
  | "pending"
  | "connected"
  | "expired"
  | "revoked"
  | "failed";

export type PlatformInstallCommand = {
  platform: EnrollmentPlatform;
  label: string;
  detail: string;
  command: string;
  ready: boolean;
};

/**
 * Derive the user-visible enrollment status. A "pending" record whose
 * expiresUnix has passed flips to "expired" so the UI can offer a regenerate
 * button instead of a stale copy-command block.
 */
export function enrollmentStatus(enrollment: DaemonEnrollment | null): EnrollmentStatus {
  if (!enrollment) return "idle";
  if (
    enrollment.status === "pending" &&
    enrollment.expiresUnix &&
    enrollment.expiresUnix <= Math.floor(Date.now() / 1000)
  ) {
    return "expired";
  }
  return (enrollment.status as EnrollmentStatus) || "pending";
}

/**
 * Merge the refreshed enrollment record with the locally-held token + install
 * command. The status-polling endpoint never returns the one-time token, so
 * we preserve whatever was returned by the initial create call on the client.
 */
export function mergeEnrollmentStatus(
  current: DaemonEnrollment | null,
  next: DaemonEnrollment
): DaemonEnrollment {
  return {
    ...next,
    installCommand: current?.installCommand,
    installScriptUrl: current?.installScriptUrl,
    token: current?.token
  };
}

/**
 * Synthesize the three platform install commands from the Linux script URL.
 * The server emits the Linux bash one-liner as the canonical command; macOS
 * reuses bash with a platform query flip, Windows swaps to PowerShell with
 * the .ps1 variant.
 */
export function platformInstallCommands(enrollment: DaemonEnrollment | null): PlatformInstallCommand[] {
  if (!enrollment) return [];
  const installScriptUrl = enrollment.installScriptUrl || "";
  const macScriptUrl = installScriptUrl.replace("platform=linux", "platform=darwin");
  const windowsScriptUrl = macScriptUrl
    .replace("/install.sh", "/install.ps1")
    .replace("platform=darwin", "platform=windows");
  return [
    {
      platform: "linux",
      label: "Linux",
      detail: "Bash entry for systemd hosts",
      command: enrollment.installCommand || "",
      ready: Boolean(enrollment.installCommand)
    },
    {
      platform: "macos",
      label: "macOS",
      detail: "Bash entry for launchd hosts",
      command: macScriptUrl ? `sudo bash -c "$(curl -fsSL ${macScriptUrl})"` : "",
      ready: Boolean(macScriptUrl)
    },
    {
      platform: "windows",
      label: "Windows",
      detail: "PowerShell entry for Windows Service",
      command: windowsScriptUrl
        ? `powershell -ExecutionPolicy Bypass -Command "iwr ${windowsScriptUrl} | iex"`
        : "",
      ready: Boolean(windowsScriptUrl)
    }
  ];
}

/**
 * Is the daemon heartbeat recent enough to treat the computer as online?
 * Threshold matches what the sidebar already uses: 120 seconds of silence
 * drops the computer to offline.
 */
export function isComputerOnline(computer: DaemonInventoryComputer, nowMs: number = Date.now()): boolean {
  const last = computer.lastHeartbeatUnix ?? 0;
  if (last <= 0) return false;
  return nowMs / 1000 - last < 120;
}

/**
 * Aggregate installed runtimes across the inventory entry. DaemonInventory
 * carries a `runtimes` list on each computer; older server builds omit it,
 * in which case we return an empty list and let the caller show a hint.
 */
export function detectedRuntimeKinds(computer: DaemonInventoryComputer): string[] {
  const runtimes = (computer.runtimes as DaemonInventoryRuntime[] | undefined) ?? [];
  const kinds = new Set<string>();
  for (const runtime of runtimes) {
    if (!runtime.installed) continue;
    const kind = runtime.kind;
    if (kind) kinds.add(kind);
  }
  return [...kinds].sort((left, right) => left.localeCompare(right));
}
