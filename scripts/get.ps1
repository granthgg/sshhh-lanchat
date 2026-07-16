# Web installer for lanchat on Windows - no Go, no git, no build.
#
#   irm https://raw.githubusercontent.com/granthgg/sshhh-lanchat/main/scripts/get.ps1 | iex
#
# Downloads the prebuilt lanchat.exe for this machine from the latest GitHub
# release, verifies its SHA-256 against the release's checksums.txt, and
# installs it onto your user PATH.
#
# Why this avoids the SmartScreen "unknown publisher" popup: SmartScreen only
# screens files carrying the browser's mark-of-the-web. This installer
# verifies the file's checksum first, then removes that mark (Unblock-File) -
# the same thing package managers like Scoop do. The binary is unsigned either
# way; the checksum verification is what establishes it is the exact file CI
# built from the tagged source.
#
# Pin a version:   $env:LANCHAT_VERSION = 'v2.2.0'; irm ... | iex
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI, so non-ASCII characters corrupt the script.

$ErrorActionPreference = "Stop"
$Repo = "granthgg/sshhh-lanchat"

# Older Windows PowerShell defaults can miss TLS 1.2, which GitHub requires.
try {
    [Net.ServicePointManager]::SecurityProtocol = `
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {}

# --- 1. pick the asset for this machine --------------------------------------
$arch = "amd64"
try {
    if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq "Arm64") {
        $arch = "arm64"
    }
} catch {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { $arch = "arm64" }
}
$asset = "lanchat-windows-$arch.exe"

$version = $env:LANCHAT_VERSION
if ([string]::IsNullOrEmpty($version)) {
    $version = "latest"
    $base = "https://github.com/$Repo/releases/latest/download"
} else {
    $base = "https://github.com/$Repo/releases/download/$version"
}

# --- 2. download ---------------------------------------------------------------
$tmp = Join-Path $env:TEMP ("lanchat-install-" + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$exe = Join-Path $tmp $asset

Write-Host "==> downloading $asset ($version) ..."
Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $exe

# --- 3. verify against the release's checksums.txt ------------------------------
$sums = Join-Path $tmp "checksums.txt"
$haveSums = $true
try {
    Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums
} catch {
    $haveSums = $false
}
if ($haveSums) {
    $line = Get-Content $sums | Where-Object { $_ -match ("\s" + [regex]::Escape($asset) + "$") } | Select-Object -First 1
    if (-not $line) { throw "checksums.txt has no entry for $asset" }
    $want = ($line -split "\s+")[0].ToLower()
    $got = (Get-FileHash -Algorithm SHA256 -Path $exe).Hash.ToLower()
    if ($got -ne $want) {
        Remove-Item -Recurse -Force $tmp
        throw "checksum verification FAILED (expected $want, got $got) - refusing to install"
    }
    Write-Host "==> checksum verified"
} else {
    Write-Host "==> note: this release has no checksums.txt (pre-2.2.0); skipping verification"
}

# The file is verified; removing the mark-of-the-web is now the honest thing
# to do - you explicitly asked to install it.
Unblock-File -Path $exe

# --- 4. install (same layout as scripts/install.ps1) ----------------------------
$dest = Join-Path $env:LOCALAPPDATA "Programs\lanchat"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item $exe (Join-Path $dest "lanchat.exe") -Force
Remove-Item -Recurse -Force $tmp
Write-Host "==> installed $dest\lanchat.exe"

# Add the folder to the user PATH if it is not already there (idempotent).
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ([string]::IsNullOrEmpty($userPath)) { $userPath = "" }
$onPath = $false
foreach ($p in $userPath.Split(";")) {
    if ($p.TrimEnd("\") -ieq $dest.TrimEnd("\")) { $onPath = $true }
}
if (-not $onPath) {
    $newPath = if ($userPath.TrimEnd(";") -eq "") { $dest } else { $userPath.TrimEnd(";") + ";" + $dest }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "==> added $dest to your user PATH"
}
# Make it available in THIS session too.
if (($env:Path.Split(";") | ForEach-Object { $_.TrimEnd("\") }) -notcontains $dest.TrimEnd("\")) {
    $env:Path = "$env:Path;$dest"
}

Write-Host ""
Write-Host "Done. Run it from anywhere:"
Write-Host "    lanchat                  (open room 'lobby')"
Write-Host "    lanchat -r team -ask     (private room, prompts for the passphrase)"
Write-Host ""
Write-Host "On first run Windows will ask to allow 'lanchat' through the firewall -"
Write-Host "choose Private networks so people on your Wi-Fi can reach you."
