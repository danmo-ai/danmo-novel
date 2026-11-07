use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream, ToSocketAddrs};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter, Manager};

const HELP_URL: &str = "https://github.com/danmo-ai/danmo-novel#macos-%E5%AE%89%E8%A3%85";
const BACKEND_ADDR: &str = "127.0.0.1:7804";
const BACKEND_READY_TIMEOUT: Duration = Duration::from_secs(45);
const BACKEND_READY_INTERVAL: Duration = Duration::from_millis(200);
const BACKEND_PID_FILE: &str = "backend.pid";

/// Owns the desktop sidecar. Rust's `Child` drop does **not** kill the process,
/// so we must terminate explicitly on app exit / Drop.
struct SidecarState {
    child: Mutex<Option<std::process::Child>>,
    home: PathBuf,
    stopped: AtomicBool,
}

impl SidecarState {
    fn shutdown(&self) {
        if self
            .stopped
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .is_err()
        {
            return;
        }
        let mut guard = match self.child.lock() {
            Ok(g) => g,
            Err(p) => p.into_inner(),
        };
        if let Some(mut child) = guard.take() {
            terminate_child(&mut child);
        }
        let pid_path = self.home.join(BACKEND_PID_FILE);
        if let Ok(raw) = fs::read_to_string(&pid_path) {
            if let Ok(pid) = raw.trim().parse::<u32>() {
                eprintln!("[sidecar] exit: stopping pidfile backend pid={pid}");
                stop_pid(pid);
            }
        }
        let _ = fs::remove_file(&pid_path);
        eprintln!("[sidecar] shutdown complete");
    }
}

impl Drop for SidecarState {
    fn drop(&mut self) {
        self.shutdown();
    }
}

fn novel_home(app: &AppHandle) -> Result<PathBuf, String> {
    let home = app
        .path()
        .home_dir()
        .map_err(|e| format!("failed to resolve home dir: {e}"))?;
    Ok(home.join(".danmo-novel"))
}

fn find_sidecar_binary() -> Result<PathBuf, String> {
    let exe_dir = std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .ok_or_else(|| "cannot determine exe directory".to_string())?;

    let names = [
        "danmo-novel-backend-aarch64-apple-darwin",
        "danmo-novel-backend-x86_64-apple-darwin",
        "danmo-novel-backend-x86_64-unknown-linux-gnu",
        "danmo-novel-backend-x86_64-pc-windows-msvc.exe",
        "danmo-novel-backend-x86_64-pc-windows-msvc",
        "danmo-novel-backend",
        "danmo-novel-backend.exe",
    ];
    for name in &names {
        let candidate = exe_dir.join(name);
        if candidate.exists() {
            return Ok(candidate);
        }
    }
    Err(format!("sidecar binary not found in {}", exe_dir.display()))
}

/// Copy sidecar out of the .app bundle into ~/.danmo-novel/bin.
/// macOS App Translocation / Gatekeeper often kills helpers launched from a
/// quarantined or translocated bundle path; running from the home data dir is stable.
fn prepare_runtime_binary(bundled: &Path, home: &Path) -> Result<PathBuf, String> {
    let bin_dir = home.join("bin");
    fs::create_dir_all(&bin_dir).map_err(|e| format!("create bin dir: {e}"))?;
    let runtime_bin = bin_dir.join("danmo-novel-backend");
    let stamp_path = bin_dir.join("danmo-novel-backend.stamp");
    let src_meta = fs::metadata(bundled).map_err(|e| format!("sidecar metadata: {e}"))?;
    let src_mtime = src_meta
        .modified()
        .ok()
        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let wanted_stamp = format!("{}:{}", src_meta.len(), src_mtime);
    let have_stamp = fs::read_to_string(&stamp_path).unwrap_or_default();
    let need_copy = have_stamp.trim() != wanted_stamp || !runtime_bin.exists();
    if need_copy {
        fs::copy(bundled, &runtime_bin).map_err(|e| format!("copy sidecar: {e}"))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perms = fs::metadata(&runtime_bin)
                .map_err(|e| format!("sidecar chmod stat: {e}"))?
                .permissions();
            perms.set_mode(0o755);
            fs::set_permissions(&runtime_bin, perms).map_err(|e| format!("sidecar chmod: {e}"))?;
        }
        let _ = fs::write(&stamp_path, &wanted_stamp);
        eprintln!("[sidecar] refreshed runtime binary ({wanted_stamp})");
    }
    Ok(runtime_bin)
}

fn external_backend_requested() -> bool {
    for key in [
        "NOVEL_EXTERNAL_BACKEND",
        "WORK_EXTERNAL_BACKEND",
        "SKIP_BACKEND",
    ] {
        match std::env::var(key).as_deref() {
            Ok("1") | Ok("true") | Ok("TRUE") | Ok("yes") | Ok("YES") => return true,
            _ => {}
        }
    }
    false
}

fn spawn_backend(app: &AppHandle) -> Result<(), String> {
    let home = novel_home(app)?;
    fs::create_dir_all(&home).map_err(|e| format!("failed to create ~/.danmo-novel: {e}"))?;
    let work_dir = home.join("library");
    fs::create_dir_all(&work_dir).map_err(|e| format!("failed to create library dir: {e}"))?;
    let _ = fs::create_dir_all(home.join("config"));

    let log_path = home.join("backend.log");
    let pid_path = home.join(BACKEND_PID_FILE);

    if external_backend_requested() {
        eprintln!(
            "[sidecar] external backend mode — not reclaiming or spawning; expecting API on {BACKEND_ADDR}"
        );
        if let Ok(mut f) = OpenOptions::new().create(true).append(true).open(&log_path) {
            let _ = writeln!(
                f,
                "\n--- external backend mode (skip sidecar spawn) {BACKEND_ADDR} ---"
            );
        }
        if backend_ready(BACKEND_ADDR) {
            let _ = app.emit("backend-ready", ());
        }
        return Ok(());
    }

    reclaim_backend_port(&pid_path, &log_path);

    let bundled = find_sidecar_binary()?;
    let binary = prepare_runtime_binary(&bundled, &home)?;
    eprintln!("[sidecar] home: {}", home.display());
    eprintln!("[sidecar] runtime: {}", binary.display());

    let log_file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&log_path)
        .map_err(|e| format!("open backend log: {e}"))?;
    let log_err = log_file
        .try_clone()
        .map_err(|e| format!("clone backend log: {e}"))?;
    if let Ok(mut f) = OpenOptions::new().create(true).append(true).open(&log_path) {
        let _ = writeln!(f, "\n--- sidecar spawn ---");
        let _ = writeln!(f, "binary={}", binary.display());
    }

    let mut cmd = Command::new(&binary);
    cmd.current_dir(&home)
        .env("NOVEL_HOME", home.to_string_lossy().as_ref())
        .env("DQ_HTTP_HOST", "127.0.0.1")
        .env("DQ_BACKEND_PORT", "7804")
        .env("DANMO_NOVEL_PORT", "7804")
        .env(
            "NOVEL_DB_PATH",
            home.join("novel.db").to_string_lossy().as_ref(),
        )
        .env("NOVEL_LIBRARY", work_dir.to_string_lossy().as_ref())
        .stdout(std::process::Stdio::from(log_file))
        .stderr(std::process::Stdio::from(log_err));
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        cmd.process_group(0);
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| format!("failed to spawn backend: {e}"))?;

    if let Err(e) = fs::write(&pid_path, child.id().to_string()) {
        eprintln!("[sidecar] write pid file: {e}");
    }

    std::thread::sleep(Duration::from_millis(400));
    match child.try_wait() {
        Ok(Some(status)) => {
            let _ = fs::remove_file(&pid_path);
            let tail = fs::read_to_string(&log_path).unwrap_or_default();
            let tail = tail.chars().rev().take(2000).collect::<String>();
            let tail: String = tail.chars().rev().collect();
            return Err(format!(
                "backend exited immediately ({status}). log tail:\n{tail}"
            ));
        }
        Ok(None) => {}
        Err(e) => return Err(format!("backend wait failed: {e}")),
    }

    let child_pid = child.id();
    app.manage(SidecarState {
        child: Mutex::new(Some(child)),
        home: home.clone(),
        stopped: AtomicBool::new(false),
    });

    let app_handle = app.clone();
    let log_for_ready = log_path.clone();
    let pid_for_ready = pid_path.clone();
    std::thread::spawn(move || {
        if wait_for_our_backend(
            BACKEND_ADDR,
            child_pid,
            BACKEND_READY_TIMEOUT,
            BACKEND_READY_INTERVAL,
        ) {
            eprintln!("[sidecar] backend pid={child_pid} listening on {BACKEND_ADDR}");
            let _ = app_handle.emit("backend-ready", ());
        } else {
            let _ = fs::remove_file(&pid_for_ready);
            let tail = fs::read_to_string(&log_for_ready).unwrap_or_default();
            let tail = tail.chars().rev().take(2000).collect::<String>();
            let tail: String = tail.chars().rev().collect();
            eprintln!(
                "[sidecar] backend did not become ready within {:?}. log tail:\n{tail}",
                BACKEND_READY_TIMEOUT
            );
            let _ = app_handle.emit("backend-failed", ());
        }
    });

    eprintln!(
        "[sidecar] backend spawned pid={child_pid} on {BACKEND_ADDR} (log: {})",
        log_path.display()
    );
    Ok(())
}

fn reclaim_backend_port(pid_path: &Path, log_path: &Path) {
    if let Ok(raw) = fs::read_to_string(pid_path) {
        if let Ok(pid) = raw.trim().parse::<u32>() {
            eprintln!("[sidecar] stopping previous backend pid={pid}");
            stop_pid(pid);
        }
        let _ = fs::remove_file(pid_path);
    }
    for pid in listeners_on_backend_port() {
        eprintln!("[sidecar] reclaiming {BACKEND_ADDR} from pid={pid}");
        stop_pid(pid);
    }
    std::thread::sleep(Duration::from_millis(300));
    if let Ok(mut f) = OpenOptions::new().create(true).append(true).open(log_path) {
        let _ = writeln!(f, "--- reclaim {BACKEND_ADDR} ---");
    }
}

fn stop_pid(pid: u32) {
    if pid == 0 {
        return;
    }
    #[cfg(unix)]
    {
        let pg = format!("-{pid}");
        let _ = Command::new("kill").args(["-TERM", &pg]).status();
        let _ = Command::new("kill")
            .args(["-TERM", &pid.to_string()])
            .status();
        std::thread::sleep(Duration::from_millis(200));
        let _ = Command::new("kill").args(["-KILL", &pg]).status();
        let _ = Command::new("kill")
            .args(["-KILL", &pid.to_string()])
            .status();
    }
    #[cfg(windows)]
    {
        let _ = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .status();
    }
}

fn terminate_child(child: &mut std::process::Child) {
    let pid = child.id();
    eprintln!("[sidecar] terminating backend pid={pid}");
    #[cfg(unix)]
    {
        let pg = format!("-{pid}");
        let _ = Command::new("kill").args(["-TERM", &pg]).status();
        let _ = Command::new("kill")
            .args(["-TERM", &pid.to_string()])
            .status();
        std::thread::sleep(Duration::from_millis(250));
        match child.try_wait() {
            Ok(Some(status)) => {
                eprintln!("[sidecar] backend pid={pid} exited ({status})");
                return;
            }
            _ => {
                let _ = Command::new("kill").args(["-KILL", &pg]).status();
                let _ = child.kill();
                let _ = child.wait();
                eprintln!("[sidecar] backend pid={pid} force-killed");
            }
        }
    }
    #[cfg(windows)]
    {
        let _ = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .status();
        let _ = child.wait();
        eprintln!("[sidecar] backend pid={pid} taskkilled");
    }
    #[cfg(not(any(unix, windows)))]
    {
        let _ = child.kill();
        let _ = child.wait();
    }
}

fn shutdown_sidecar(app: &AppHandle) {
    if let Some(state) = app.try_state::<SidecarState>() {
        state.shutdown();
        return;
    }
    if let Ok(home) = novel_home(app) {
        let pid_path = home.join(BACKEND_PID_FILE);
        if let Ok(raw) = fs::read_to_string(&pid_path) {
            if let Ok(pid) = raw.trim().parse::<u32>() {
                eprintln!("[sidecar] exit: stopping orphan pidfile backend pid={pid}");
                stop_pid(pid);
            }
        }
        let _ = fs::remove_file(&pid_path);
    }
}

fn listeners_on_backend_port() -> Vec<u32> {
    #[cfg(unix)]
    {
        let output = Command::new("lsof")
            .args(["-nP", "-iTCP:7804", "-sTCP:LISTEN", "-t"])
            .output();
        let Ok(out) = output else {
            return Vec::new();
        };
        String::from_utf8_lossy(&out.stdout)
            .lines()
            .filter_map(|line| line.trim().parse::<u32>().ok())
            .collect()
    }
    #[cfg(windows)]
    {
        let output = Command::new("netstat").args(["-ano"]).output();
        let Ok(out) = output else {
            return Vec::new();
        };
        let mut pids = Vec::new();
        for line in String::from_utf8_lossy(&out.stdout).lines() {
            let lower = line.to_ascii_lowercase();
            if !(lower.contains("7804") && lower.contains("listen")) {
                continue;
            }
            if let Some(pid) = line.split_whitespace().last().and_then(|s| s.parse().ok()) {
                if pid > 0 && !pids.contains(&pid) {
                    pids.push(pid);
                }
            }
        }
        pids
    }
    #[cfg(not(any(unix, windows)))]
    {
        Vec::new()
    }
}

fn wait_for_our_backend(addr: &str, child_pid: u32, timeout: Duration, interval: Duration) -> bool {
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if !wait_for_backend_listen(addr, Duration::from_millis(100), Duration::from_millis(20)) {
            std::thread::sleep(interval);
            continue;
        }
        let listeners = listeners_on_backend_port();
        if listeners.is_empty() || listeners.contains(&child_pid) {
            if backend_ready(addr) {
                return true;
            }
        } else {
            eprintln!(
                "[sidecar] {addr} held by {:?}, expected pid={child_pid}; reclaiming",
                listeners
            );
            for pid in listeners {
                if pid != child_pid {
                    stop_pid(pid);
                }
            }
        }
        std::thread::sleep(interval);
    }
    false
}

fn backend_ready(addr: &str) -> bool {
    let Ok(mut stream) = TcpStream::connect(addr) else {
        return false;
    };
    let _ = stream.set_read_timeout(Some(Duration::from_millis(500)));
    let _ = stream.set_write_timeout(Some(Duration::from_millis(500)));
    let req = "GET /api/system/health HTTP/1.0\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n";
    if stream.write_all(req.as_bytes()).is_err() {
        return false;
    }
    let mut buf = Vec::new();
    let _ = stream.read_to_end(&mut buf);
    let body = String::from_utf8_lossy(&buf);
    body.contains("\"ok\"") || body.contains("HTTP/1.0 200") || body.contains("HTTP/1.1 200")
}

fn wait_for_backend_listen(addr: &str, timeout: Duration, interval: Duration) -> bool {
    let targets: Vec<SocketAddr> = match addr.to_socket_addrs() {
        Ok(iter) => iter.collect(),
        Err(e) => {
            eprintln!("[sidecar] resolve {addr}: {e}");
            return false;
        }
    };
    if targets.is_empty() {
        return false;
    }
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        for target in &targets {
            if TcpStream::connect_timeout(target, Duration::from_millis(150)).is_ok() {
                return true;
            }
        }
        std::thread::sleep(interval);
    }
    false
}

fn handle_first_launch(app: &AppHandle) {
    #[cfg(target_os = "macos")]
    {
        if let Ok(home) = novel_home(app) {
            let marker = home.join(".first_launch_done");
            if !marker.exists() {
                let _ = fs::create_dir_all(&home);
                let _ = fs::write(&marker, "1");
                let _ = open::that(HELP_URL);
            }
        }
    }
}

#[tauri::command]
fn open_external(url: String) -> Result<(), String> {
    open::that(&url).map_err(|e| format!("failed to open: {e}"))
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_localcore::init())
        .plugin(tauri_plugin_process::init())
        .on_page_load(|window, payload| {
            if matches!(payload.event(), tauri::webview::PageLoadEvent::Finished) {
                let _ = window.window().show();
            }
        })
        .invoke_handler(tauri::generate_handler![open_external])
        .setup(|app| {
            handle_first_launch(&app.handle());
            #[cfg(not(any(target_os = "ios", target_os = "android")))]
            if let Err(e) = spawn_backend(&app.handle()) {
                eprintln!("WARNING: backend sidecar failed to start: {e}");
                eprintln!("The app will run without backend API. Start it manually if needed.");
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|app_handle, event| match event {
            tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit => {
                #[cfg(not(any(target_os = "ios", target_os = "android")))]
                shutdown_sidecar(&app_handle);
            }
            _ => {}
        });
}
