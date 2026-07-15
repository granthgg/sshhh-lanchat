# One-command installer for lanchat on Windows (PowerShell).
#
#   powershell -ExecutionPolicy Bypass -File install.ps1
#
# Builds the single .exe from source and installs it so you can run `lanchat`
# from any directory. It copies the binary to a per-user folder and adds that
# folder to your user PATH, so every new terminal finds it automatically.
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI, so non-ASCII characters (em dashes, smart quotes) corrupt the script.

$ErrorActionPreference = "Stop"
$Binary = "lanchat.exe"
$SrcDir = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Go is not installed. Get it from https://go.dev/dl/ (need 1.25+), then re-run." -ForegroundColor Red
    exit 1
}
Write-Host "==> using $(go env GOVERSION)"

Write-Host "==> building $Binary ..."
Push-Location $SrcDir
try {
    go build -ldflags "-s -w" -o $Binary .
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
} finally {
    Pop-Location
}

# Install to a stable per-user location.
$Dest = Join-Path $env:LOCALAPPDATA "Programs\lanchat"
New-Item -ItemType Directory -Force -Path $Dest | Out-Null
Copy-Item (Join-Path $SrcDir $Binary) (Join-Path $Dest $Binary) -Force
Write-Host "==> installed $Dest\$Binary"

# Add the folder to the user PATH if it is not already there (idempotent).
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ([string]::IsNullOrEmpty($userPath)) { $userPath = "" }
$onPath = $false
foreach ($p in $userPath.Split(";")) {
    if ($p.TrimEnd("\") -ieq $Dest.TrimEnd("\")) { $onPath = $true }
}
if (-not $onPath) {
    $newPath = if ($userPath.TrimEnd(";") -eq "") { $Dest } else { $userPath.TrimEnd(";") + ";" + $Dest }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "==> added $Dest to your user PATH"
}
# Make it available in THIS session too.
if (($env:Path.Split(";") | ForEach-Object { $_.TrimEnd("\") }) -notcontains $Dest.TrimEnd("\")) {
    $env:Path = "$env:Path;$Dest"
}

Write-Host ""
Write-Host "Done. Open a NEW terminal and run it from anywhere:"
Write-Host "    lanchat                  (open room 'lobby')"
Write-Host "    lanchat -r team -ask     (private room, prompts for the passphrase)"
Write-Host ""
Write-Host "On first run Windows may ask to allow 'lanchat' through the firewall -"
Write-Host "choose Private networks so people on your Wi-Fi can reach you."
