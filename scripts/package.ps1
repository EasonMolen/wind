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
$runtimeExecutable = Join-Path $projectRoot 'runtime\Everything.exe'
$sdkDll = Join-Path $projectRoot 'internal\search\sdk\dll\Everything64.dll'

if (-not (Test-Path -LiteralPath $runtimeExecutable -PathType Leaf)) {
    throw "缺少便携版 Everything：$runtimeExecutable。请从 voidtools 官方 x64 portable ZIP 放入该位置。"
}
if (-not (Test-Path -LiteralPath $sdkDll -PathType Leaf)) {
    throw "缺少 Everything SDK DLL：$sdkDll"
}

if (Test-Path -LiteralPath $releaseDirectory) {
    Remove-Item -LiteralPath $releaseDirectory -Recurse -Force
}
New-Item -ItemType Directory -Path $releaseDirectory | Out-Null
New-Item -ItemType Directory -Path (Join-Path $releaseDirectory 'runtime') | Out-Null

Push-Location $projectRoot
try {
    if (-not $SkipTests) {
        go test ./internal/config ./internal/hotkey ./internal/launcher ./internal/ui
    }

    $ldflags = "-X wind/internal/buildinfo.Version=$releaseVersion"
    go build -trimpath -ldflags $ldflags -o (Join-Path $releaseDirectory 'NewWind.exe') ./cmd/launcher

    Copy-Item -LiteralPath $sdkDll -Destination (Join-Path $releaseDirectory 'Everything64.dll')
    Copy-Item -LiteralPath $runtimeExecutable -Destination (Join-Path $releaseDirectory 'runtime\Everything.exe')
    Copy-Item -LiteralPath (Join-Path $projectRoot 'README.md') -Destination $releaseDirectory
    Copy-Item -LiteralPath (Join-Path $projectRoot 'third_party\licenses') -Destination (Join-Path $releaseDirectory 'licenses') -Recurse

    Compress-Archive -Path (Join-Path $releaseDirectory '*') -DestinationPath "$releaseDirectory.zip" -Force
}
finally {
    Pop-Location
}

Write-Host "便携版已生成：$releaseDirectory.zip"
