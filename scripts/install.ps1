param(
  [string]$Version = $(if ($env:SOL_CLOUD_VERSION) { $env:SOL_CLOUD_VERSION } else { "latest" }),
  [string]$Repo = "CharlieAIO/sol-cloud",
  [string]$InstallDir = "$HOME\bin"
)

$ErrorActionPreference = "Stop"

$arch = $env:PROCESSOR_ARCHITECTURE
switch ($arch) {
  "AMD64" { $goArch = "amd64" }
  "ARM64" { $goArch = "arm64" }
  default { throw "Unsupported Windows architecture: $arch" }
}

if ($Version.ToLower() -eq "latest") {
  $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
  $tag = $release.tag_name
} elseif ($Version.StartsWith("v")) {
  $tag = $Version
} else {
  $tag = "v$Version"
}

$asset = "sol-cloud_${tag}_windows_${goArch}.zip"
$url = "https://github.com/$Repo/releases/download/$tag/$asset"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$tmpZip = Join-Path $env:TEMP $asset
$tmpDir = Join-Path $env:TEMP "sol-cloud-install-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null

try {
  Write-Host "Downloading $url"
  Invoke-WebRequest -Uri $url -OutFile $tmpZip
  Expand-Archive -Path $tmpZip -DestinationPath $tmpDir -Force

  $src = Join-Path $tmpDir "sol-cloud.exe"
  $dst = Join-Path $InstallDir "sol-cloud.exe"
  Move-Item -Path $src -Destination $dst -Force

  Write-Host "Installed: $dst"
  & $dst --version
  Write-Host "If needed, add '$InstallDir' to your PATH."
}
finally {
  if (Test-Path $tmpZip) { Remove-Item $tmpZip -Force }
  if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
}
