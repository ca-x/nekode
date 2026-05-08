package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ca-x/nekode/internal/version"
)

func (s *Server) renderDaemonInstallShell(enrollment daemonEnrollment, token string) string {
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
`, shellDoubleQuoted(daemonArtifactVersion()), shellDoubleQuoted(daemonDownloadBaseURL()), s.daemonConfigJSON(enrollment, token))
}

func (s *Server) renderDaemonInstallPowerShell(enrollment daemonEnrollment, token string) string {
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
`, powerShellDoubleQuoted(daemonArtifactVersion()), powerShellDoubleQuoted(daemonDownloadBaseURL()), s.daemonConfigJSON(enrollment, token))
}

func (s *Server) daemonConfigJSON(enrollment daemonEnrollment, token string) string {
	body := map[string]string{
		"grpcAddr":          s.cfg.GRPCAddr,
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
