# install.ps1 — one-line installer for prebuilt Sensei binaries (Windows).
#
#   irm https://raw.githubusercontent.com/globulario/sensei/main/install.ps1 | iex
#
# Downloads the self-contained windows-amd64 release tarball (sensei.exe +
# awareness-graph.exe + awareness-mcp.exe + oxigraph.exe), verifies its
# checksum, installs the binaries, and adds them to your user PATH.
#
# Options (environment variables):
#   $env:SENSEI_VERSION = 'v1.1.0'          pin a release (default: latest)
#   $env:SENSEI_PREFIX  = 'C:\tools\sensei' install dir
#                                           (default: %LOCALAPPDATA%\Programs\sensei)
#
# Note: the CLI (serve/build/gate/queries) runs natively; the pre-edit
# enforcement hooks are bash, so local enforcement needs Git Bash or WSL.
$ErrorActionPreference = 'Stop'

$Repo     = 'globulario/sensei'
$Version  = if ($env:SENSEI_VERSION) { $env:SENSEI_VERSION } else { 'latest' }
$Prefix   = if ($env:SENSEI_PREFIX)  { $env:SENSEI_PREFIX }  else { Join-Path $env:LOCALAPPDATA 'Programs\sensei' }
$Platform = 'windows-amd64'
$Tarball  = "sensei-$Platform.tar.gz"
$Base     = if ($Version -eq 'latest') { "https://github.com/$Repo/releases/latest/download" } `
            else { "https://github.com/$Repo/releases/download/$Version" }

if (-not (Get-Command tar -ErrorAction SilentlyContinue)) {
  throw "install.ps1: 'tar' not found (Windows 10 build 17063+ ships it). Update Windows or extract manually."
}

$Tmp = Join-Path $env:TEMP ("sensei-" + [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force -Path $Tmp | Out-Null
try {
  Write-Host "Installing Sensei ($Version, $Platform)"
  Write-Host "  down $Base/$Tarball"
  $tgz = Join-Path $Tmp $Tarball
  Invoke-WebRequest -Uri "$Base/$Tarball" -OutFile $tgz -UseBasicParsing

  # Checksum (best effort — the .sha256 lists the unix filename).
  try {
    $shaFile = "$tgz.sha256"
    Invoke-WebRequest -Uri "$Base/$Tarball.sha256" -OutFile $shaFile -UseBasicParsing
    $expected = ((Get-Content $shaFile -Raw).Trim() -split '\s+')[0].ToLower()
    $actual   = (Get-FileHash $tgz -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { throw "checksum mismatch (expected $expected, got $actual)" }
    Write-Host "  ok  checksum verified"
  } catch [System.Net.WebException] { <# no checksum published; skip #> }

  # bsdtar (native Windows tar) handles .tar.gz and drive-letter paths fine.
  tar -xzf $tgz -C $Tmp
  $src = Join-Path $Tmp "sensei-$Platform\bin"
  if (-not (Test-Path $src)) { throw "unexpected tarball layout (no bin\)" }

  New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
  Copy-Item (Join-Path $src '*') -Destination $Prefix -Force
  Write-Host "Installed -> $Prefix"

  # Add to the user PATH if missing.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $Prefix) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$Prefix", 'User')
    Write-Host "Added $Prefix to your user PATH (restart your terminal to pick it up)."
  } else {
    Write-Host "$Prefix is already on your PATH."
  }

  Write-Host ""
  Write-Host "Give your agent the MCP tools (Claude Code, .mcp.json at your repo root):"
  Write-Host "    { ""mcpServers"": { ""sensei"": {"
  Write-Host "        ""command"": ""$($Prefix -replace '\\','\\')\\awareness-mcp.exe"","
  Write-Host "        ""args"": [""--awareness-addr"", ""localhost:10120""] } } }"
  Write-Host "Then:  sensei serve -no-seed   # start the local store + server"
}
finally {
  Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}
