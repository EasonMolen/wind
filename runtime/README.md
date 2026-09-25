# Portable Everything runtime

Place the official x64 portable `Everything.exe` (and optional
`Everything.lng`) in this directory before running `scripts/package.ps1`.
The packaging script copies them to the release's `runtime/` directory.

NewWind starts it with `-startup -first-instance`, so it runs in the
background and does not open an Everything search window. Do not use the
"Lite" package: it does not provide the IPC interface used by NewWind.

Before publishing, review Everything's redistribution terms and add the
applicable upstream license text to `third_party/licenses/`.
