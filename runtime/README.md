# Portable Everything runtime

Place the official x64 portable `Everything.exe` in this directory before
running `scripts/package.ps1`.

NewWind starts it with `-startup -first-instance`, so it runs in the
background and does not open an Everything search window. Do not use the
"Lite" package: it does not provide the IPC interface used by NewWind.

Before publishing, review Everything's redistribution terms and add the
applicable upstream license text to `third_party/licenses/`.
