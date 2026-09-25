[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v?\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$')]
    [string]$Version,
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$releaseVersion = $Version.TrimStart('v')
$releaseDirectory = Join-Path $projectRoot "dist\NewWind-$releaseVersion-windows-amd64"
$releaseArchive = "$releaseDirectory.zip"
$runtimeSourceDirectory = Join-Path $projectRoot 'runtime'
$runtimeExecutable = Join-Path $runtimeSourceDirectory 'Everything.exe'
$runtimeLanguagePack = Join-Path $runtimeSourceDirectory 'Everything.lng'
$sdkDll = Join-Path $projectRoot 'Everything64.dll'

if (-not (Test-Path -LiteralPath $runtimeExecutable -PathType Leaf)) {
    throw "Missing portable Everything runtime: $runtimeExecutable. Copy the official x64 portable files to runtime/."
}
if (-not (Test-Path -LiteralPath $sdkDll -PathType Leaf)) {
    throw "Missing Everything SDK DLL: $sdkDll"
}

if (Test-Path -LiteralPath $releaseDirectory) {
    Remove-Item -LiteralPath $releaseDirectory -Recurse -Force
}
if (Test-Path -LiteralPath $releaseArchive) {
    Remove-Item -LiteralPath $releaseArchive -Force
}
New-Item -ItemType Directory -Path $releaseDirectory | Out-Null
New-Item -ItemType Directory -Path (Join-Path $releaseDirectory 'runtime') | Out-Null
New-Item -ItemType Directory -Path (Join-Path $releaseDirectory 'licenses') | Out-Null

Push-Location $projectRoot
try {
    if (-not $SkipTests) {
        go test ./internal/config ./internal/hotkey ./internal/launcher ./internal/ui
    }

    $ldflags = "-X wind/internal/buildinfo.Version=$releaseVersion"
    go build -trimpath -ldflags $ldflags -o (Join-Path $releaseDirectory 'NewWind.exe') ./cmd/launcher

    Copy-Item -LiteralPath $sdkDll -Destination (Join-Path $releaseDirectory 'Everything64.dll')
    Copy-Item -LiteralPath $runtimeExecutable -Destination (Join-Path $releaseDirectory 'runtime\Everything.exe')
    if (Test-Path -LiteralPath $runtimeLanguagePack -PathType Leaf) {
        Copy-Item -LiteralPath $runtimeLanguagePack -Destination (Join-Path $releaseDirectory 'runtime\Everything.lng')
    }
    Copy-Item -LiteralPath (Join-Path $projectRoot 'README.md') -Destination $releaseDirectory
    Get-ChildItem -LiteralPath (Join-Path $projectRoot 'third_party\licenses') -Force |
        Copy-Item -Destination (Join-Path $releaseDirectory 'licenses') -Recurse -Force

    Compress-Archive -Path (Join-Path $releaseDirectory '*') -DestinationPath $releaseArchive -Force
}
finally {
    Pop-Location
}

Write-Host "Portable package created: $releaseArchive"
