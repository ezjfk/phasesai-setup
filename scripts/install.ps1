param(
  [ValidatePattern('^v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$')][string]$Version = 'v0.2.0',
  [ValidateSet('all','claude','codex','opencode')][string]$Harness = 'all',
  [Parameter(ValueFromRemainingArguments=$true)][string[]]$InstallerArguments
)
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
$arch = switch ($architecture) { 'X64' { 'amd64' }; 'Arm64' { 'arm64' }; default { throw 'Windows x64 or ARM64 is required.' } }
$asset = if ($Harness -eq 'all') { "phasesai-setup-windows-$arch.exe" } else { "phasesai-$Harness-setup-windows-$arch.exe" }
$setupTempPath = Join-Path ([IO.Path]::GetTempPath()) ('phasesai-setup-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $setupTempPath)
$binary = Join-Path $setupTempPath $asset
$checksums = Join-Path $setupTempPath 'SHA256SUMS'
$release = "https://github.com/ezjfk/phasesai-setup/releases/download/$Version"
try {
  Invoke-WebRequest -UseBasicParsing -Uri "$release/$asset" -OutFile $binary
  Invoke-WebRequest -UseBasicParsing -Uri "$release/SHA256SUMS" -OutFile $checksums
  $expected = @((Get-Content -LiteralPath $checksums) | ForEach-Object { if ($_ -match ('^([a-f0-9]{64})\s+' + [regex]::Escape($asset) + '\s*$')) { $Matches[1] } })
  if ($expected.Count -ne 1 -or (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected[0]) { throw 'Checksum missing or mismatched; nothing executed.' }
  # Provenance verification is available to maintainers with gh. End users can
  # download/run the EXE directly and do not need a separate GitHub CLI install.
  Write-Host 'Checksum verified. Starting PhasesAI Windows setup.' -ForegroundColor Cyan
  & $binary @InstallerArguments
  if ($LASTEXITCODE -ne 0) { throw 'Installer exited unsuccessfully.' }
} finally {
  if (Test-Path -LiteralPath $binary) { Remove-Item -LiteralPath $binary -Force }
  if (Test-Path -LiteralPath $checksums) { Remove-Item -LiteralPath $checksums -Force }
  if (Test-Path -LiteralPath $setupTempPath) { Remove-Item -LiteralPath $setupTempPath }
}
