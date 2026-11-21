//! Alpha-Nexus desktop shell.
//!
//! The window is a thin host: it loads the local Go control plane UI at
//! http://127.0.0.1:17421. If that control plane is not already running, this
//! process starts the bundled sidecar binary (`alpha-nexus-egolite ui serve`)
//! and terminates it again when the app exits. No publishing logic lives here.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant};

use tauri::{AppHandle, Manager, RunEvent, Url, WebviewUrl, WebviewWindowBuilder};

/// Where the Go control plane listens. Must match `--port` below.
const UI_HOST: &str = "127.0.0.1";
const UI_PORT: u16 = 17421;
const UI_URL: &str = "http://127.0.0.1:17421";
const HEALTH_PATH: &str = "/healthz";

/// Upper bound for "wait until the control plane answers /healthz".
const HEALTH_TIMEOUT: Duration = Duration::from_secs(15);
const HEALTH_POLL: Duration = Duration::from_millis(250);
/// Per-attempt socket budget; a hung listener must not eat the whole deadline.
const PROBE_CONNECT_TIMEOUT: Duration = Duration::from_millis(500);
const PROBE_READ_TIMEOUT: Duration = Duration::from_millis(1500);
/// Grace period between SIGTERM and SIGKILL for the sidecar we spawned.
const SIDECAR_TERM_GRACE: Duration = Duration::from_secs(5);

const SIDECAR_STEM: &str = "alpha-nexus-egolite";
const SIDECAR_ENV_OVERRIDE: &str = "ALPHA_NEXUS_SIDECAR";

/// The sidecar process this run started, if any. `None` means the control
/// plane was already running before we launched, and is therefore not ours
/// to stop.
struct Sidecar(Mutex<Option<Child>>);

fn main() {
    // Ctrl-C / SIGTERM must run the same cleanup as a normal quit: without
    // this, killing the shell leaves the sidecar serving forever (verified as
    // an orphan process during release testing).
    for signal in [libc::SIGTERM, libc::SIGINT] {
        unsafe {
            // SAFETY: plain signal registration; the handler only flips an
            // atomic flag, which is async-signal-safe.
            let mut action: libc::sigaction = std::mem::zeroed();
            action.sa_sigaction = sigterm_handler as *const () as usize;
            action.sa_flags = libc::SA_SIGINFO;
            libc::sigaction(signal, &action, std::ptr::null_mut());
        }
    }
    // A watcher thread translates the flag into a cooperative exit. Exiting
    // this way does not run the run-loop teardown, so the watcher asks the
    // sidecar directly (see terminate_sidecar_pid) before leaving.
    thread::spawn(|| loop {
        thread::sleep(Duration::from_millis(200));
        if EXIT_REQUESTED.load(std::sync::atomic::Ordering::SeqCst) {
            terminate_sidecar_pid();
            std::process::exit(0);
        }
    });

    let app = tauri::Builder::default()
        // A second launch must focus the existing window instead of starting a
        // second shell (and a second sidecar probing the same port).
        .plugin(tauri_plugin_single_instance::init(
            |app, _argv, _cwd| focus_main_window(app),
        ))
        .setup(|app| {
            let handle = app.handle().clone();

            // Reuse a control plane that is already up (dev workflows, or the
            // user started `ui serve` themselves).
            let already_up = probe_health().is_some();
            let mut child = None;
            if !already_up {
                match spawn_sidecar(&handle) {
                    Some(spawned) => child = Some(spawned),
                    None => eprintln!(
                        "[alpha-nexus] sidecar binary not found; expecting an already running \
                         control plane on {UI_URL}"
                    ),
                }
            }
            let spawned = child.is_some();
            app.manage(Sidecar(Mutex::new(child)));

            // Only a sidecar we just started is worth the full wait; with no
            // binary and no running server there is nothing to wait for.
            let ready = if already_up {
                true
            } else if spawned {
                wait_for_health(HEALTH_TIMEOUT)
            } else {
                probe_health().is_some()
            };
            if ready {
                eprintln!("[alpha-nexus] control plane ready at {UI_URL}");
            } else {
                // Do not dead-end the user: open the window anyway so the
                // webview can show the connection error and logs stay visible.
                eprintln!(
                    "[alpha-nexus] {UI_URL} did not answer {HEALTH_PATH}; opening the window anyway"
                );
            }

            let url = Url::parse(UI_URL).expect("UI_URL is a valid absolute URL");
            WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
                .title("Alpha-Nexus")
                .inner_size(1280.0, 820.0)
                .min_inner_size(960.0, 600.0)
                .build()?;

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build the Alpha-Nexus desktop shell");

    app.run(|handle, event| {
        match event {
            // ExitRequested fires before the loop tears down; stopping the
            // sidecar here also covers paths that bypass RunEvent::Exit.
            RunEvent::ExitRequested { .. } => stop_sidecar(handle),
            RunEvent::Exit => stop_sidecar(handle),
            _ => {}
        }
    });
}

/// Sidecar pid recorded right after spawn so the signal watcher can clean it up
/// on an abrupt exit (std::process::exit skips the Tauri run-loop teardown).
static SIDECAR_PID: Mutex<Option<u32>> = Mutex::new(None);

fn record_sidecar_pid(child: &Child) {
    if let Ok(mut guard) = SIDECAR_PID.lock() {
        *guard = Some(child.id());
    }
}

/// SIGTERM/SIGINT path: kill the recorded sidecar, then leave.
fn terminate_sidecar_pid() {
    if let Ok(guard) = SIDECAR_PID.lock() {
        if let Some(pid) = *guard {
            unsafe {
                libc::kill(pid as i32, libc::SIGTERM);
            }
        }
    }
}

/// Writes one byte to a self-pipe so the main thread can exit the event loop.
/// Only async-signal-safe calls belong here.
extern "C" fn sigterm_handler(_sig: libc::c_int, _info: *mut libc::siginfo_t, _ctx: *mut libc::c_void) {
    // Only flip an atomic flag: async-signal-safe. The watcher thread does the
    // actual cleanup and exits.
    EXIT_REQUESTED.store(true, std::sync::atomic::Ordering::SeqCst);
}

static EXIT_REQUESTED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

fn focus_main_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

/// One blocking GET /healthz attempt. Returns the response text on HTTP 200.
fn probe_health() -> Option<String> {
    let addr = SocketAddr::from(([127, 0, 0, 1], UI_PORT));
    let mut stream = TcpStream::connect_timeout(&addr, PROBE_CONNECT_TIMEOUT).ok()?;
    stream.set_read_timeout(Some(PROBE_READ_TIMEOUT)).ok()?;
    stream.set_write_timeout(Some(PROBE_READ_TIMEOUT)).ok()?;

    let request = format!(
        "GET {HEALTH_PATH} HTTP/1.1\r\nHost: {UI_HOST}:{UI_PORT}\r\nConnection: close\r\n\r\n"
    );
    stream.write_all(request.as_bytes()).ok()?;

    let mut raw = Vec::new();
    // healthz is a small JSON object; cap the read so a foreign server on the
    // port cannot stream forever into our buffer.
    stream.take(8192).read_to_end(&mut raw).ok()?;
    let text = String::from_utf8_lossy(&raw);
    if text.starts_with("HTTP/1.1 200") || text.starts_with("HTTP/1.0 200") {
        Some(text.into_owned())
    } else {
        None
    }
}

/// Polls /healthz until the deadline. `true` when the control plane answered.
fn wait_for_health(timeout: Duration) -> bool {
    let deadline = Instant::now() + timeout;
    loop {
        if probe_health().is_some() {
            return true;
        }
        if Instant::now() >= deadline {
            return false;
        }
        thread::sleep(HEALTH_POLL);
    }
}

fn sidecar_file_name() -> String {
    if cfg!(windows) {
        format!("{SIDECAR_STEM}.exe")
    } else {
        SIDECAR_STEM.to_string()
    }
}

/// Looks for the Go control plane binary next to the shell or in the bundle's
/// resource directory. `ALPHA_NEXUS_SIDECAR` wins, which keeps
/// `cargo tauri dev` usable without copying the binary into `resources/`.
fn locate_sidecar(app: &AppHandle) -> Option<PathBuf> {
    if let Some(explicit) = std::env::var_os(SIDECAR_ENV_OVERRIDE) {
        let path = PathBuf::from(explicit);
        if path.is_file() {
            return Some(path);
        }
        eprintln!(
            "[alpha-nexus] {SIDECAR_ENV_OVERRIDE}={} is not a file; falling back to resources",
            path.display()
        );
    }

    let name = sidecar_file_name();
    let mut candidates: Vec<PathBuf> = Vec::new();

    // `resources/` is bundled as-is, so both layouts are possible depending on
    // the tauri.conf.json resource mapping.
    if let Ok(dir) = app.path().resource_dir() {
        candidates.push(dir.join(&name));
        candidates.push(dir.join("resources").join(&name));
    }
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join(&name));
        }
    }

    candidates.into_iter().find(|path| path.is_file())
}

fn spawn_sidecar(app: &AppHandle) -> Option<Child> {
    let binary = locate_sidecar(app)?;
    let data_root = app.path().app_data_dir().ok()?;
    let data_dir = data_root.join("data");
    if let Err(err) = std::fs::create_dir_all(&data_dir) {
        eprintln!(
            "[alpha-nexus] cannot create data dir {}: {err}",
            data_dir.display()
        );
        return None;
    }

    let mut command = Command::new(&binary);
    command
        .arg("ui")
        .arg("serve")
        // `--data-dir` is a persistent CLI flag, accepted on every subcommand.
        .arg("--data-dir")
        .arg(&data_dir)
        .arg("--port")
        .arg(UI_PORT.to_string())
        .stdin(Stdio::null());

    // Sidecar output is diagnostic only; keep it on disk instead of losing it.
    match std::fs::File::create(data_root.join("desktop-sidecar.log")) {
        Ok(log) => match log.try_clone() {
            Ok(log_err) => {
                command.stdout(Stdio::from(log));
                command.stderr(Stdio::from(log_err));
            }
            Err(_) => {
                command.stdout(Stdio::from(log));
                command.stderr(Stdio::null());
            }
        },
        Err(_) => {
            command.stdout(Stdio::null());
            command.stderr(Stdio::null());
        }
    }

    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        // CREATE_NO_WINDOW: the sidecar must not flash a console window.
        command.creation_flags(0x0800_0000);
    }

    match command.spawn() {
        Ok(child) => {
            eprintln!(
                "[alpha-nexus] started sidecar {} (pid {}) with data dir {}",
                binary.display(),
                child.id(),
                data_dir.display()
            );
            record_sidecar_pid(&child);
            Some(child)
        }
        Err(err) => {
            eprintln!(
                "[alpha-nexus] failed to start sidecar {}: {err}",
                binary.display()
            );
            None
        }
    }
}

fn stop_sidecar(app: &AppHandle) {
    let Some(state) = app.try_state::<Sidecar>() else {
        return;
    };
    let mut guard = match state.0.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    };
    if let Some(mut child) = guard.take() {
        terminate(&mut child);
    }
}

/// Asks the sidecar to shut down cleanly, then forces it after the grace
/// period. SIGTERM matters: `ui serve` closes the SQLite handle and releases
/// EgoLite spaces on it.
fn terminate(child: &mut Child) {
    #[cfg(unix)]
    {
        let pid = child.id() as i32;
        // SAFETY: plain signal delivery to a child process we spawned; no
        // memory is shared with the callee.
        unsafe {
            libc::kill(pid, libc::SIGTERM);
        }
        let deadline = Instant::now() + SIDECAR_TERM_GRACE;
        while Instant::now() < deadline {
            match child.try_wait() {
                Ok(Some(_)) => return,
                Ok(None) => thread::sleep(Duration::from_millis(100)),
                Err(_) => break,
            }
        }
        eprintln!("[alpha-nexus] sidecar ignored SIGTERM; killing it");
    }

    let _ = child.kill();
    let _ = child.wait();
}
