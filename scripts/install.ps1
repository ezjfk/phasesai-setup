param(
  [Parameter(Mandatory=$true)][ValidatePattern('^v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$')][string]$Version,
  [Parameter(ValueFromRemainingArguments=$true)][string[]]$InstallerArguments
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'Install the official GitHub CLI first to verify build provenance.' }
$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
$arch = switch ($architecture) { 'X64' { 'amd64' }; 'Arm64' { 'arm64' }; default { throw 'Unsupported architecture.' } }
$asset = "phasesai-setup-windows-$arch.exe"
$setupTempPath = Join-Path ([System.IO.Path]::GetTempPath()) ('phasesai-setup-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $setupTempPath)
$binary = Join-Path $setupTempPath $asset
$checksums = Join-Path $setupTempPath 'SHA256SUMS'
try {
  Invoke-WebRequest -Uri "https://downloads.phasesai.cc/setup/$Version/windows-$arch/$asset" -OutFile $binary
  Invoke-WebRequest -Uri "https://github.com/ezjfk/phasesai-setup/releases/download/$Version/SHA256SUMS" -OutFile $checksums
  $expected = @((Get-Content -LiteralPath $checksums) | ForEach-Object { if ($_ -match ('^([a-f0-9]{64})\s+' + [regex]::Escape($asset) + '\s*$')) { $Matches[1] } })
  if ($expected.Count -ne 1 -or (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected[0]) { throw 'Checksum missing or mismatched; nothing executed.' }
  & gh attestation verify $binary --repo ezjfk/phasesai-setup --signer-workflow ezjfk/phasesai-setup/.github/workflows/preview.yml --source-ref "refs/tags/$Version"
  if ($LASTEXITCODE -ne 0) { throw 'GitHub build provenance verification failed; nothing executed.' }
  Write-Host 'Verified checksum and GitHub build provenance. Starting interactive setup.'
  & $binary @InstallerArguments
  if ($LASTEXITCODE -ne 0) { throw 'Installer exited unsuccessfully.' }
} finally {
  # Delete only the two exact files created above, then the now-empty directory.
  if (Test-Path -LiteralPath $binary) { Remove-Item -LiteralPath $binary -Force }
  if (Test-Path -LiteralPath $checksums) { Remove-Item -LiteralPath $checksums -Force }
  if (Test-Path -LiteralPath $setupTempPath) { Remove-Item -LiteralPath $setupTempPath }
}
