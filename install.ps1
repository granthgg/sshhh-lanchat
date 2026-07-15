# One-command installer for `lanchat` on Windows (PowerShell).
#
#   powershell -ExecutionPolicy Bypass -File install.ps1
#
# Builds the single .exe from source with the Go toolchain and copies it to a
# per-user directory on your PATH.

$ErrorActionPreference = "Stop"
$Binary  = "lanchat.exe"
$SrcDir  = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed. Get it from https://go.dev/dl/ (need 1.25+), then re-run."
}
Write-Host "==> using $(go env GOVERSION)"

Write-Host "==> building $Binary ..."
Push-Location $SrcDir
try {
    go build -ldflags "-s -w" -o $Binary .
} finally {
    Pop-Location
}

$Dest = Join-Path $env:LOCALAPPDATA "Programs\lanchat"
New-Item -ItemType Directory -Force -Path $Dest | Out-Null
Copy-Item (Join-Path $SrcDir $Binary) (Join-Path $Dest $Binary) -Force
Write-Host "==> installed $Dest\$Binary"

# Add to the user PATH if missing.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$Dest*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$Dest", "User")
    Write-Host "==> added $Dest to your user PATH (restart your terminal)"
}

Write-Host ""
Write-Host "done. try it:   lanchat                 (open room 'lobby')"
Write-Host "                lanchat -r team -ask     (private room)"
Write-Host ""
Write-Host "Windows will likely prompt to allow 'lanchat' through the firewall on"
Write-Host "first run — choose Private networks so peers can reach you."
