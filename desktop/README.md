# desktop — Alpha-Nexus Tauri 2 shell

A separate desktop client skeleton. It does **not** contain any product logic:
the shell only hosts the Go control plane UI that already runs on this machine
at <http://127.0.0.1:17421>. The full Go stack (SQLite, EgoLite, tools registry)
stays in the repository root and is unchanged by this directory.

```
desktop/
  build-desktop.sh              # build Go binary + ui-web, copy into resources, cargo tauri build
  README.md
  src-tauri/
    Cargo.toml                  # tauri 2 + tauri-build
    build.rs
    tauri.conf.json             # identifier ltd.alphaintelligence.alpha-nexus-egolite, 1280x820
    capabilities/default.json   # core:default only, no remote IPC
    icons/                      # placeholder icons, replace via `cargo tauri icon`
    resources/                  # alpha-nexus-egolite binary lands here (gitignored)
    src/main.rs                 # healthz probe, sidecar spawn, window, cleanup
```

## How the shell behaves at startup

1. `GET http://127.0.0.1:17421/healthz` (500 ms connect / 1.5 s read budget).
2. If it answers, the shell only opens a window — it never owns the process.
3. If it does not answer, the shell looks for the Go binary in
   `$RESOURCE_DIR/alpha-nexus-egolite` (then `$RESOURCE_DIR/resources/…`, then
   next to the executable) and runs:

   ```bash
   alpha-nexus-egolite ui serve --data-dir <app_data_dir>/data --port 17421
   ```

   `--data-dir` is a persistent flag of the Go CLI, so it is accepted on the
   `ui serve` subcommand.
4. It polls `/healthz` for at most **15 s**, then creates the window
   (1280x820, title `Alpha-Nexus`) pointing at <http://127.0.0.1:17421>.
5. On exit the sidecar it started gets `SIGTERM` (5 s grace) and then `SIGKILL`.
   A control plane that was already running is left alone.
6. `tauri-plugin-single-instance` keeps one shell per machine; a second launch
   focuses the existing window.

Dev shortcut: `ALPHA_NEXUS_SIDECAR=/path/to/alpha-nexus-egolite cargo tauri dev`
skips the copy-into-resources step.

Sidecar stdout/stderr go to `<app_data_dir>/desktop-sidecar.log`
(macOS: `~/Library/Application Support/ltd.alphaintelligence.alpha-nexus-egolite/`).

## Prerequisites

| Tool | Version | Notes |
| --- | --- | --- |
| Rust | 1.90+ | `rustup default stable` |
| Tauri CLI | 2.x | `cargo install tauri-cli --version "^2" --locked` |
| Xcode CLT | current | macOS builds (`xcode-select --install`) |
| Node | 20+ | only to build `ui-web` |
| Go | matching `go.mod` | to build the control plane |

Windows additionally needs the **WebView2** runtime (the bundle uses the
`downloadBootstrapper` install mode) and the MSVC build tools with the
`x86_64-pc-windows-msvc` Rust target.

## macOS build

```bash
# 1. Do a normal macOS build (dev-friendly wrapper, see the script header)
./desktop/build-desktop.sh

# or explicitly, for the Apple Silicon / Intel triple of this machine:
./desktop/build-desktop.sh aarch64-apple-darwin
```

Output:

```
desktop/src-tauri/target/<triple>/release/bundle/macos/Alpha-Nexus.app
desktop/src-tauri/target/<triple>/release/bundle/dmg/Alpha-Nexus_0.2.0_<arch>.dmg
```

Verify by hand before shipping:

```bash
open "desktop/src-tauri/target/aarch64-apple-darwin/release/bundle/macos/Alpha-Nexus.app"
# stop your own `ui serve` first, to prove the bundled sidecar starts
```

The app is **not** sandboxed and needs no Tauri capability beyond `core:default`,
because the UI is a plain HTTP page served by the Go process.

## Windows build

Build **on Windows** (or in a Windows VM/CI runner):

```powershell
# from the repository root
cargo install tauri-cli --version "^2" --locked
.\desktop\build-desktop.ps1   # not provided yet: run the steps below by hand
```

Manual equivalent:

```powershell
cd ui-web; npm ci; npm run build; cd ..
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -o desktop\src-tauri\resources\alpha-nexus-egolite.exe .\cmd\alpha-nexus-egolite\
cd desktop\src-tauri
cargo tauri build
```

Output: `desktop\src-tauri\target\release\bundle\nsis\Alpha-Nexus_0.2.0_x64-setup.exe`
and/or `bundle\msi\*.msi`.

> **Known blocker (verified on 2026-11-20):** cross-compiling the Go sidecar
> from macOS fails — `internal/sidecar/runtime.go` uses `syscall.Flock` and
> `internal/publishworker/runner.go` uses `syscall.Setpgid` / `syscall.Kill`,
> none of which exist on Windows. `build.sh` already disables the Windows Go
> target for the same reason. A Windows installer therefore needs either a
> native Windows build after those call sites gain a `_windows.go` variant, or
> shipping the sidecar as a separate download. The Tauri shell itself compiles
> for `x86_64-pc-windows-msvc` independently of that.

## Putting the Go binary into resources

`build-desktop.sh` does this for you. By hand:

```bash
go build -trimpath -ldflags="-s -w" -o desktop/src-tauri/resources/alpha-nexus-egolite ./cmd/alpha-nexus-egolite
chmod +x desktop/src-tauri/resources/alpha-nexus-egolite
```

`tauri.conf.json` bundles the directory with
`"resources": { "resources/": "" }`, so inside the bundle it becomes:

- macOS: `Alpha-Nexus.app/Contents/Resources/alpha-nexus-egolite`
- Windows: `<install dir>\resources\alpha-nexus-egolite.exe`

Both are found by `locate_sidecar()` in `src/main.rs`. The binary must be for
the **same architecture** as the shell; a universal (`lipo`) sidecar is the
cleanest option for a universal `.dmg`.

Note on the embedded UI: `internal/ui/assets.go` embeds `internal/ui/dist` with
`//go:embed all:dist`. That directory is committed, so `go build` works without
touching it. If `ui-web/` changed, refresh the embedded copy yourself
(`cd ui-web && npm run build && rm -rf ../internal/ui/dist && cp -r dist ../internal/ui/dist`)
before packaging — the script deliberately does not write outside `desktop/`.

## Icons

The checked-in PNGs are generated placeholders. Replace them with the real
brand mark before any external release:

```bash
cd desktop/src-tauri
cargo tauri icon path/to/logo-1024.png   # writes icons/ incl. icon.icns and icon.ico
```

Then add `icons/icon.icns` and `icons/icon.ico` to `bundle.icon` in
`tauri.conf.json` — macOS prefers `.icns` and the Windows bundler needs `.ico`.

## Signing and notarization

### macOS

1. Get a **Developer ID Application** certificate and install it in the login
   keychain (`security find-identity -v -p codesigning` should list it).
2. Export signing credentials for Tauri:

   ```bash
   export APPLE_SIGNING_IDENTITY="Developer ID Application: Your Name (TEAMID)"
   export APPLE_ID="you@example.com"
   export APPLE_PASSWORD="app-specific-password"      # app-specific, not the account password
   export APPLE_TEAM_ID="TEAMID"
   ```

   (Alternatively `APPLE_API_ISSUER` / `APPLE_API_KEY` / `APPLE_API_KEY_PATH`
   for App Store Connect API keys.)
3. `cargo tauri build` then signs, submits to `notarytool`, and staples the
   ticket. Verify:

   ```bash
   codesign --verify --deep --strict --verbose=2 "…/Alpha-Nexus.app"
   spctl -a -vvv -t install "…/Alpha-Nexus.app"    # expect "source=Notarized Developer ID"
   xcrun stapler validate "…/Alpha-Nexus_0.2.0_aarch64.dmg"
   ```

4. Hardened runtime is enabled by Tauri by default. The sidecar is a plain
   executable started as a child process, **not** a Mach-O injected into the
   webview, so no extra entitlement is needed. If you later add JIT or
   screen-capture APIs, add the matching entitlements plist.

Unsigned local builds run only on your machine (Gatekeeper quirk after download:
`xattr -dr com.apple.quarantine Alpha-Nexus.app`).

### Windows

1. Buy a code-signing certificate (OV/EV); export it as `.pfx`.
2. Sign either through Tauri's built-in support:

   ```powershell
   $env:CERTIFICATE_THUMBPRINT = "…"
   cargo tauri build
   ```

   or sign the artifacts afterwards with `signtool`:

   ```powershell
   signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f cert.pfx /p <password> bundle\nsis\Alpha-Nexus_0.2.0_x64-setup.exe
   ```

3. EV certificates require a hardware token; timestamping is mandatory for
   signatures to outlive the certificate.

## Not covered here

- Auto-update (would need `tauri-plugin-updater` + a signed `latest.json`).
- Tray icon, deep links, second-window flows.
- CI packaging; the script is a local convenience wrapper.
