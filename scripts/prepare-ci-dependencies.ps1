$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("newwind-ci-deps-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempRoot | Out-Null

$portableVersion = '1.4.1.1032'
$portableUrl = "https://www.voidtools.com/Everything-$portableVersion.x64.zip"
$portableSha256 = '698df475ec44e638f66f1b6a32d28fea613cec78d3b6310e6abe53431eeb940c'
$sdkUrl = 'https://www.voidtools.com/Everything-SDK.zip'
$sdkDllSha256 = 'C7AB8B47F7DD4C41AA735F4BA40B35AD5460A86FA7ABE0C94383F12BCE33BFB6'

$portableArchive = Join-Path $tempRoot 'everything-portable.zip'
$portableDirectory = Join-Path $tempRoot 'portable'
$sdkArchive = Join-Path $tempRoot 'everything-sdk.zip'
$sdkDirectory = Join-Path $tempRoot 'sdk'

Invoke-WebRequest -Uri $portableUrl -OutFile $portableArchive
$actualPortableSha256 = (Get-FileHash -LiteralPath $portableArchive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualPortableSha256 -ne $portableSha256) {
    throw "Everything portable package checksum mismatch. Expected $portableSha256, got $actualPortableSha256"
}
Expand-Archive -LiteralPath $portableArchive -DestinationPath $portableDirectory

$everythingExecutable = Get-ChildItem -LiteralPath $portableDirectory -Filter 'Everything.exe' -File -Recurse |
    Select-Object -First 1
if ($null -eq $everythingExecutable -or $everythingExecutable.VersionInfo.ProductVersion -ne $portableVersion) {
    throw "Everything portable package does not contain the expected Everything $portableVersion executable."
}

Invoke-WebRequest -Uri $sdkUrl -OutFile $sdkArchive
Expand-Archive -LiteralPath $sdkArchive -DestinationPath $sdkDirectory
$sdkDll = Get-ChildItem -LiteralPath $sdkDirectory -Filter 'Everything64.dll' -File -Recurse |
    Select-Object -First 1
if ($null -eq $sdkDll) {
    throw 'Everything SDK package does not contain Everything64.dll.'
}
$actualSdkDllSha256 = (Get-FileHash -LiteralPath $sdkDll.FullName -Algorithm SHA256).Hash.ToUpperInvariant()
if ($actualSdkDllSha256 -ne $sdkDllSha256) {
    throw "Everything SDK DLL checksum mismatch. Expected $sdkDllSha256, got $actualSdkDllSha256"
}

$runtimeDirectory = Join-Path $projectRoot 'runtime'
New-Item -ItemType Directory -Path $runtimeDirectory -Force | Out-Null
Copy-Item -LiteralPath $everythingExecutable.FullName -Destination (Join-Path $runtimeDirectory 'Everything.exe') -Force
$languagePack = Get-ChildItem -LiteralPath $portableDirectory -Filter 'Everything.lng' -File -Recurse |
    Select-Object -First 1
if ($null -ne $languagePack) {
    Copy-Item -LiteralPath $languagePack.FullName -Destination (Join-Path $runtimeDirectory 'Everything.lng') -Force
}
Copy-Item -LiteralPath $sdkDll.FullName -Destination (Join-Path $projectRoot 'Everything64.dll') -Force

Write-Host "Prepared verified Everything $portableVersion runtime and SDK DLL."
