package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/version"
)

func (s *Server) renderDaemonInstallShell(r *http.Request, enrollment daemonEnrollment, token string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

log() { printf '%%s\n' "$*"; }
fail() { printf 'nekode install failed: %%s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || fail "run this installer with sudo"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux) SERVICE_MODE="systemd" ;;
  darwin) SERVICE_MODE="launchd" ;;
  *) fail "unsupported OS: $OS" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) fail "unsupported arch: $ARCH" ;;
esac

VERSION="${NEKODE_DAEMON_VERSION:-%s}"
DOWNLOAD_BASE_URL="${NEKODE_DAEMON_DOWNLOAD_BASE_URL:-%s}"
ARTIFACT="nekode-daemon_${VERSION}_${OS}_${ARCH}.tar.gz"
TMPDIR="$(mktemp -d)"
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

log "Downloading $ARTIFACT"
curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR/$ARTIFACT" "$DOWNLOAD_BASE_URL/$ARTIFACT"
curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR/SHA256SUMS.txt" "$DOWNLOAD_BASE_URL/SHA256SUMS.txt"
grep "  $ARTIFACT$" "$TMPDIR/SHA256SUMS.txt" > "$TMPDIR/$ARTIFACT.sha256" || fail "checksum missing for $ARTIFACT"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMPDIR" && sha256sum -c "$ARTIFACT.sha256")
else
  (cd "$TMPDIR" && shasum -a 256 -c "$ARTIFACT.sha256")
fi

tar -xzf "$TMPDIR/$ARTIFACT" -C "$TMPDIR"
mkdir -p /usr/local/bin /etc/nekode
install -m 0755 "$TMPDIR/nekode-daemon_${VERSION}_${OS}_${ARCH}/nekode-daemon" /usr/local/bin/nekode-daemon
umask 077
cat > /etc/nekode/daemon.json <<'NEKODE_CONFIG'
%s
NEKODE_CONFIG
chmod 0600 /etc/nekode/daemon.json

if [ "$SERVICE_MODE" = "systemd" ] && command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/system/nekode-daemon.service <<'NEKODE_SYSTEMD'
[Unit]
Description=Nekode daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/nekode-daemon run --config /etc/nekode/daemon.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
NEKODE_SYSTEMD
  systemctl daemon-reload
  systemctl enable --now nekode-daemon

  # Verify the daemon registered within a few seconds.
  # If the server is unreachable (e.g. IPv6-only address that
  # isn't open) the service will loop restarting — surface
  # that early instead of silently failing.
  sleep 3
  if ! systemctl is-active --quiet nekode-daemon 2>/dev/null; then
    log "Daemon failed to start — checking logs:"
    journalctl -u nekode-daemon --no-pager -n 5 >&2 || true
    fail "daemon service did not stay active; check: systemctl status nekode-daemon"
  fi
  log "Daemon installed and running"

elif [ "$SERVICE_MODE" = "launchd" ]; then
  cat > /Library/LaunchDaemons/tech.nekode.daemon.plist <<'NEKODE_PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>tech.nekode.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/nekode-daemon</string>
    <string>run</string>
    <string>--config</string>
    <string>/etc/nekode/daemon.json</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/nekode-daemon.log</string>
  <key>StandardErrorPath</key><string>/var/log/nekode-daemon.err.log</string>
</dict>
</plist>
NEKODE_PLIST
  launchctl bootout system /Library/LaunchDaemons/tech.nekode.daemon.plist >/dev/null 2>&1 || true
  launchctl bootstrap system /Library/LaunchDaemons/tech.nekode.daemon.plist
  launchctl enable system/tech.nekode.daemon
else
  log "No supported service manager found. Run: /usr/local/bin/nekode-daemon run --config /etc/nekode/daemon.json"
fi

log "Nekode daemon install finished"
`, shellDoubleQuoted(daemonArtifactVersion()), shellDoubleQuoted(daemonDownloadBaseURL()), s.daemonConfigJSON(r, enrollment, token))
}

func (s *Server) renderDaemonInstallPowerShell(r *http.Request, enrollment daemonEnrollment, token string) string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"

function Fail($Message) {
  Write-Error "nekode install failed: $Message"
  exit 1
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  Fail "run this installer from an elevated PowerShell"
}

$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -eq "AMD64") {
  $goarch = "amd64"
} else {
  Fail "unsupported Windows architecture: $arch"
}

$version = if ($env:NEKODE_DAEMON_VERSION) { $env:NEKODE_DAEMON_VERSION } else { "%s" }
$downloadBaseUrl = if ($env:NEKODE_DAEMON_DOWNLOAD_BASE_URL) { $env:NEKODE_DAEMON_DOWNLOAD_BASE_URL.TrimEnd("/") } else { "%s" }
$artifact = "nekode-daemon_${version}_windows_${goarch}.zip"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("nekode-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/$artifact" -OutFile (Join-Path $tmp $artifact)
  Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/SHA256SUMS.txt" -OutFile (Join-Path $tmp "SHA256SUMS.txt")
  $expectedLine = Get-Content (Join-Path $tmp "SHA256SUMS.txt") | Where-Object { $_ -match "  $([regex]::Escape($artifact))$" } | Select-Object -First 1
  if (-not $expectedLine) { Fail "checksum missing for $artifact" }
  $expected = ($expectedLine -split "\s+")[0].ToLowerInvariant()
  $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $artifact)).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { Fail "checksum mismatch for $artifact" }

  Expand-Archive -Force -Path (Join-Path $tmp $artifact) -DestinationPath $tmp
  $installDir = Join-Path $env:ProgramFiles "Nekode"
  $configDir = Join-Path $env:ProgramData "Nekode"
  New-Item -ItemType Directory -Force -Path $installDir,$configDir | Out-Null
  Copy-Item -Force -Path (Join-Path $tmp "nekode-daemon_${version}_windows_${goarch}\nekode-daemon.exe") -Destination (Join-Path $installDir "nekode-daemon.exe")
  @'
%s
'@ | Set-Content -Encoding UTF8 -Path (Join-Path $configDir "daemon.json")

  $serviceName = "nekode-daemon"
  $q = [char]34
  $binaryPath = "$q$installDir\nekode-daemon.exe$q run --config $q$configDir\daemon.json$q"
  $existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
  if ($existing) {
    Stop-Service -Name $serviceName -ErrorAction SilentlyContinue
    sc.exe config $serviceName binPath= $binaryPath | Out-Null
  } else {
    New-Service -Name $serviceName -DisplayName "Nekode daemon" -BinaryPathName $binaryPath -StartupType Automatic | Out-Null
  }
  Start-Service -Name $serviceName
  Write-Host "Nekode daemon install finished"
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
`, powerShellDoubleQuoted(daemonArtifactVersion()), powerShellDoubleQuoted(daemonDownloadBaseURL()), s.daemonConfigJSON(r, enrollment, token))
}

func (s *Server) renderDaemonManagementShell(action string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

ACTION="%s"
log() { printf '%%s\n' "$*"; }
fail() { printf 'nekode daemon %%s failed: %%s\n' "$ACTION" "$*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || fail "run this script with sudo"

BIN_PATH="${NEKODE_DAEMON_BIN_PATH:-/usr/local/bin/nekode-daemon}"
CONFIG_PATH="${NEKODE_DAEMON_CONFIG_PATH:-/etc/nekode/daemon.json}"
SERVICE_NAME="${NEKODE_DAEMON_SERVICE_NAME:-nekode-daemon}"
PLIST_PATH="${NEKODE_DAEMON_PLIST_PATH:-/Library/LaunchDaemons/tech.nekode.daemon.plist}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux) SERVICE_MODE="systemd" ;;
  darwin) SERVICE_MODE="launchd" ;;
  *) fail "unsupported OS: $OS" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) fail "unsupported arch: $ARCH" ;;
esac

stop_service() {
  if [ "$SERVICE_MODE" = "systemd" ] && command -v systemctl >/dev/null 2>&1; then
    systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  elif [ "$SERVICE_MODE" = "launchd" ] && command -v launchctl >/dev/null 2>&1; then
    launchctl bootout system "$PLIST_PATH" >/dev/null 2>&1 || true
  fi
}

remove_service() {
  stop_service
  if [ "$SERVICE_MODE" = "systemd" ] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable "$SERVICE_NAME" >/dev/null 2>&1 || true
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    systemctl daemon-reload
  elif [ "$SERVICE_MODE" = "launchd" ]; then
    rm -f "$PLIST_PATH"
  fi
}

purge_config_if_requested() {
  if [ "${NEKODE_PURGE_CONFIG:-0}" = "1" ]; then
    rm -f "$CONFIG_PATH"
    rmdir "$(dirname "$CONFIG_PATH")" >/dev/null 2>&1 || true
  fi
}

install_service() {
  if [ "$SERVICE_MODE" = "systemd" ] && command -v systemctl >/dev/null 2>&1; then
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<NEKODE_SYSTEMD
[Unit]
Description=Nekode daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_PATH} run --config ${CONFIG_PATH}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
NEKODE_SYSTEMD
    systemctl daemon-reload
    systemctl enable --now "$SERVICE_NAME"
  elif [ "$SERVICE_MODE" = "launchd" ]; then
    mkdir -p "$(dirname "$PLIST_PATH")"
    cat > "$PLIST_PATH" <<NEKODE_PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>tech.nekode.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BIN_PATH}</string>
    <string>run</string>
    <string>--config</string>
    <string>${CONFIG_PATH}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/nekode-daemon.log</string>
  <key>StandardErrorPath</key><string>/var/log/nekode-daemon.err.log</string>
</dict>
</plist>
NEKODE_PLIST
    launchctl bootstrap system "$PLIST_PATH"
    launchctl enable system/tech.nekode.daemon
  else
    log "No supported service manager found. Run: $BIN_PATH run --config $CONFIG_PATH"
  fi
}

install_binary() {
  command -v curl >/dev/null 2>&1 || fail "curl is required"
  command -v tar >/dev/null 2>&1 || fail "tar is required"
  VERSION="${NEKODE_DAEMON_VERSION:-%s}"
  DOWNLOAD_BASE_URL="${NEKODE_DAEMON_DOWNLOAD_BASE_URL:-%s}"
  ARTIFACT="nekode-daemon_${VERSION}_${OS}_${ARCH}.tar.gz"
  TMPDIR="$(mktemp -d)"
  cleanup() { rm -rf "$TMPDIR"; }
  trap cleanup EXIT

  log "Downloading $ARTIFACT"
  curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR/$ARTIFACT" "$DOWNLOAD_BASE_URL/$ARTIFACT"
  curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR/SHA256SUMS.txt" "$DOWNLOAD_BASE_URL/SHA256SUMS.txt"
  grep "  $ARTIFACT$" "$TMPDIR/SHA256SUMS.txt" > "$TMPDIR/$ARTIFACT.sha256" || fail "checksum missing for $ARTIFACT"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMPDIR" && sha256sum -c "$ARTIFACT.sha256")
  else
    (cd "$TMPDIR" && shasum -a 256 -c "$ARTIFACT.sha256")
  fi
  tar -xzf "$TMPDIR/$ARTIFACT" -C "$TMPDIR"
  mkdir -p "$(dirname "$BIN_PATH")"
  install -m 0755 "$TMPDIR/nekode-daemon_${VERSION}_${OS}_${ARCH}/nekode-daemon" "$BIN_PATH"
}

case "$ACTION" in
  uninstall)
    remove_service
    rm -f "$BIN_PATH"
    purge_config_if_requested
    log "Nekode daemon uninstall finished"
    ;;
  upgrade|reinstall)
    [ -f "$CONFIG_PATH" ] || fail "missing config at $CONFIG_PATH; use a new enrollment install first"
    if [ "$ACTION" = "reinstall" ]; then
      remove_service
      rm -f "$BIN_PATH"
    else
      stop_service
    fi
    install_binary
    install_service
    log "Nekode daemon $ACTION finished"
    ;;
  *)
    fail "unsupported action: $ACTION"
    ;;
esac
`, shellDoubleQuoted(action), shellDoubleQuoted(daemonArtifactVersion()), shellDoubleQuoted(daemonDownloadBaseURL()))
}

func (s *Server) renderDaemonManagementPowerShell(action string) string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"
$action = "%s"

function Fail($Message) {
  Write-Error "nekode daemon $action failed: $Message"
  exit 1
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  Fail "run this script from an elevated PowerShell"
}

$installDir = if ($env:NEKODE_DAEMON_INSTALL_DIR) { $env:NEKODE_DAEMON_INSTALL_DIR } else { Join-Path $env:ProgramFiles "Nekode" }
$configDir = if ($env:NEKODE_DAEMON_CONFIG_DIR) { $env:NEKODE_DAEMON_CONFIG_DIR } else { Join-Path $env:ProgramData "Nekode" }
$configPath = Join-Path $configDir "daemon.json"
$binaryPath = Join-Path $installDir "nekode-daemon.exe"
$serviceName = if ($env:NEKODE_DAEMON_SERVICE_NAME) { $env:NEKODE_DAEMON_SERVICE_NAME } else { "nekode-daemon" }

function Stop-DaemonService {
  $existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
  if ($existing) {
    Stop-Service -Name $serviceName -ErrorAction SilentlyContinue
  }
}

function Remove-DaemonService {
  Stop-DaemonService
  $existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
  if ($existing) {
    sc.exe delete $serviceName | Out-Null
  }
}

function Purge-ConfigIfRequested {
  if ($env:NEKODE_PURGE_CONFIG -eq "1") {
    Remove-Item -Force $configPath -ErrorAction SilentlyContinue
    Remove-Item -Force -Recurse $configDir -ErrorAction SilentlyContinue
  }
}

function Install-DaemonBinary {
  $arch = $env:PROCESSOR_ARCHITECTURE
  if ($arch -eq "AMD64") {
    $goarch = "amd64"
  } else {
    Fail "unsupported Windows architecture: $arch"
  }

  $version = if ($env:NEKODE_DAEMON_VERSION) { $env:NEKODE_DAEMON_VERSION } else { "%s" }
  $downloadBaseUrl = if ($env:NEKODE_DAEMON_DOWNLOAD_BASE_URL) { $env:NEKODE_DAEMON_DOWNLOAD_BASE_URL.TrimEnd("/") } else { "%s" }
  $artifact = "nekode-daemon_${version}_windows_${goarch}.zip"
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("nekode-" + [System.Guid]::NewGuid().ToString("N"))
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  try {
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/$artifact" -OutFile (Join-Path $tmp $artifact)
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/SHA256SUMS.txt" -OutFile (Join-Path $tmp "SHA256SUMS.txt")
    $expectedLine = Get-Content (Join-Path $tmp "SHA256SUMS.txt") | Where-Object { $_ -match "  $([regex]::Escape($artifact))$" } | Select-Object -First 1
    if (-not $expectedLine) { Fail "checksum missing for $artifact" }
    $expected = ($expectedLine -split "\s+")[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $artifact)).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { Fail "checksum mismatch for $artifact" }

    Expand-Archive -Force -Path (Join-Path $tmp $artifact) -DestinationPath $tmp
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item -Force -Path (Join-Path $tmp "nekode-daemon_${version}_windows_${goarch}\nekode-daemon.exe") -Destination $binaryPath
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }
}

function Install-DaemonService {
  $q = [char]34
  $serviceBinaryPath = "$q$binaryPath$q run --config $q$configPath$q"
  $existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
  if ($existing) {
    sc.exe config $serviceName binPath= $serviceBinaryPath | Out-Null
  } else {
    New-Service -Name $serviceName -DisplayName "Nekode daemon" -BinaryPathName $serviceBinaryPath -StartupType Automatic | Out-Null
  }
  Start-Service -Name $serviceName
}

switch ($action) {
  "uninstall" {
    Remove-DaemonService
    Remove-Item -Force $binaryPath -ErrorAction SilentlyContinue
    Purge-ConfigIfRequested
    Write-Host "Nekode daemon uninstall finished"
  }
  "upgrade" {
    if (-not (Test-Path $configPath)) { Fail "missing config at $configPath; use a new enrollment install first" }
    Stop-DaemonService
    Install-DaemonBinary
    Install-DaemonService
    Write-Host "Nekode daemon upgrade finished"
  }
  "reinstall" {
    if (-not (Test-Path $configPath)) { Fail "missing config at $configPath; use a new enrollment install first" }
    Remove-DaemonService
    Remove-Item -Force $binaryPath -ErrorAction SilentlyContinue
    Install-DaemonBinary
    Install-DaemonService
    Write-Host "Nekode daemon reinstall finished"
  }
  default { Fail "unsupported action: $action" }
}
`, powerShellDoubleQuoted(action), powerShellDoubleQuoted(daemonArtifactVersion()), powerShellDoubleQuoted(daemonDownloadBaseURL()))
}

func (s *Server) daemonConfigJSON(r *http.Request, enrollment daemonEnrollment, token string) string {
	serverURL := s.daemonRPCURL(r)
	body := map[string]string{
		"serverUrl":         serverURL,
		"token":             token,
		"computerId":        enrollment.ComputerID,
		"displayName":       enrollment.DisplayName,
		"hostname":          enrollment.Hostname,
		"heartbeatInterval": "30s",
	}
	if strings.TrimSpace(enrollment.DaemonID) != "" {
		body["daemonId"] = strings.TrimSpace(enrollment.DaemonID)
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	return string(data)
}

// daemonRPCURL returns the HTTP base URL the installed daemon should dial.
func (s *Server) daemonRPCURL(r *http.Request) string {
	if advertise := strings.TrimSpace(s.cfg.DaemonRPCURL); advertise != "" {
		return normalizeDaemonRPCURL(advertise)
	}
	base := strings.TrimSpace(s.cfg.BaseURL)
	if base == "" || normalizeDaemonRPCURL(base) == normalizeDaemonRPCURL(config.DefaultBaseURL) {
		if derived := deriveExternalBase(r); derived != "" {
			return normalizeDaemonRPCURL(derived)
		}
	}
	if base != "" {
		return normalizeDaemonRPCURL(base)
	}
	return normalizeDaemonRPCURL(config.DefaultBaseURL)
}

func normalizeDaemonRPCURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	return strings.TrimRight(value, "/")
}

func daemonDownloadBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("NEKODE_DAEMON_DOWNLOAD_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "https://github.com/ca-x/nekode/releases/download/" + url.PathEscape(daemonArtifactVersion())
}

func daemonArtifactVersion() string {
	if value := strings.TrimSpace(os.Getenv("NEKODE_DAEMON_DOWNLOAD_VERSION")); value != "" {
		return value
	}
	if value := strings.TrimSpace(version.Current().Version); value != "" {
		return value
	}
	return "dev"
}

func shellDoubleQuoted(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "$", `\$`)
	value = strings.ReplaceAll(value, "`", "\\`")
	return value
}

func powerShellDoubleQuoted(value string) string {
	value = strings.ReplaceAll(value, "`", "``")
	value = strings.ReplaceAll(value, `"`, "`\"")
	value = strings.ReplaceAll(value, "$", "`$")
	return value
}
