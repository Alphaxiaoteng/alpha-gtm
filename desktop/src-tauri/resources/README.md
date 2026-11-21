# resources/

`build-desktop.sh` copies the compiled Go control plane binary here before
`cargo tauri build`:

- macOS / Linux: `alpha-nexus-egolite`
- Windows: `alpha-nexus-egolite.exe`

`tauri.conf.json` bundles this directory (`"resources": { "resources/": "" }`),
so the file ends up inside the app bundle's resource directory, where
`src/main.rs` looks it up. The binaries themselves are gitignored — build them,
do not commit them.
