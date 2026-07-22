# Uninstaller for lanchat on Windows (PowerShell). Reverses scripts\install.ps1
# and scripts\get.ps1:
#
#   1. deletes the installed program folder, and
#   2. removes that folder from your user PATH.
#
# lanchat stores no config, data, or history, so this removes it completely.
#
#   powershell -ExecutionPolicy Bypass -File scripts\uninstall.ps1
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI, so non-ASCII characters corrupt the script.

$ErrorActionPreference = "Stop"
$Dest = Join-Path $env:LOCALAPPDATA "Programs\lanchat"

# 1) delete the program folder
if (Test-Path $Dest) {
    Remove-Item -Recurse -Force $Dest
    Write-Host "removed $Dest"
} else {
    Write-Host "nothing at $Dest (already removed?)"
}

# 2) remove the folder from the user PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not [string]::IsNullOrEmpty($userPath)) {
    $kept = $userPath.Split(";") | Where-Object { $_ -and ($_.TrimEnd("\") -ine $Dest.TrimEnd("\")) }
    $newPath = ($kept -join ";")
    if ($newPath -ne $userPath) {
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Write-Host "removed $Dest from your user PATH"
    }
}

# 3) also drop it from this session's PATH
$env:Path = (($env:Path.Split(";") | Where-Object { $_ -and ($_.TrimEnd("\") -ine $Dest.TrimEnd("\")) }) -join ";")

# 4) warn about any other copy still on PATH (e.g. a manually placed exe)
$leftover = Get-Command lanchat -ErrorAction SilentlyContinue
if ($leftover) {
    Write-Host "note: 'lanchat' still resolves to $($leftover.Source) - remove that copy manually if it is a leftover."
}

Write-Host ""
Write-Host "Done - lanchat is fully removed."
Write-Host "Open a NEW terminal for the PATH change to take effect."
