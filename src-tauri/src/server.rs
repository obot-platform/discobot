#[cfg(not(debug_assertions))]
use std::fs;
#[cfg(not(debug_assertions))]
use std::net::TcpListener;
#[cfg(not(debug_assertions))]
use std::path::PathBuf;

#[cfg(not(debug_assertions))]
use rand::Rng;
#[cfg(not(debug_assertions))]
use tauri::Manager;
#[cfg(not(debug_assertions))]
use tauri_plugin_shell::process::CommandChild;
#[cfg(not(debug_assertions))]
use tauri_plugin_shell::ShellExt;

pub(crate) struct ServerState {
    pub(crate) port: u16,
    pub(crate) secret: String,
    #[cfg(not(debug_assertions))]
    pub(crate) ssh_port: u16,
    /// Held to keep the sidecar's stdin pipe open (server exits when stdin closes).
    #[cfg(not(debug_assertions))]
    #[allow(dead_code)]
    pub(crate) process: Option<CommandChild>,
}

#[cfg(not(debug_assertions))]
fn find_available_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .expect("Failed to bind to find available port")
        .local_addr()
        .expect("Failed to get local address")
        .port()
}

#[cfg(not(debug_assertions))]
fn preferred_ssh_port() -> u16 {
    if TcpListener::bind("127.0.0.1:3333").is_ok() {
        3333
    } else {
        find_available_port()
    }
}

#[cfg(not(debug_assertions))]
fn generate_secret() -> String {
    const CHARSET: &[u8] = b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
    let mut rng = rand::rng();
    (0..32)
        .map(|_| {
            let idx = rng.random_range(0..CHARSET.len());
            CHARSET[idx] as char
        })
        .collect()
}

#[cfg(not(debug_assertions))]
fn get_log_file_path() -> Result<PathBuf, String> {
    // Try XDG_STATE_HOME first, fallback to XDG_DATA_HOME, then ~/.local/state
    let state_dir = dirs::state_dir()
        .or_else(|| dirs::data_dir())
        .ok_or_else(|| "Could not determine state directory".to_string())?;

    let log_dir = state_dir.join("discobot").join("logs");

    // Create the directory if it doesn't exist
    fs::create_dir_all(&log_dir).map_err(|e| format!("Failed to create log directory: {}", e))?;

    Ok(log_dir.join("server.log"))
}

#[cfg(not(debug_assertions))]
fn start_server(
    app: &tauri::AppHandle,
    port: u16,
    ssh_port: u16,
    secret: &str,
) -> Result<CommandChild, String> {
    let log_path = get_log_file_path()?;

    #[allow(unused_mut)]
    let mut sidecar = app
        .shell()
        .sidecar("discobot-server")
        .map_err(|e| format!("Failed to create sidecar command: {}", e))?
        .env("PORT", port.to_string())
        .env("SSH_PORT", ssh_port.to_string())
        .env("CORS_ORIGINS", "http://tauri.localhost,tauri://localhost")
        .env("DISCOBOT_DESKTOP_RUNTIME", "tauri")
        .env("DISCOBOT_DESKTOP_SECRET", secret)
        .env("DISCOBOT_SECRET", secret)
        .env("TAURI", "true")
        .env("SUGGESTIONS_ENABLED", "true")
        .env("STDIN_KEEPALIVE", "true")
        .env("LOG_FILE", log_path.to_string_lossy().to_string());

    // Check for bundled VZ resources (macOS only)
    #[cfg(target_os = "macos")]
    {
        if let Ok(resource_dir) = app.path().resource_dir() {
            let vz_dir = resource_dir.join("vz");
            let kernel_path = vz_dir.join("vmlinux");
            let rootfs_path = vz_dir.join("discobot-rootfs.squashfs");

            // Check if both files exist
            if kernel_path.exists() && rootfs_path.exists() {
                println!("Found bundled VZ resources:");
                println!("  Kernel: {}", kernel_path.display());
                println!("  Rootfs: {}", rootfs_path.display());

                sidecar = sidecar
                    .env("VZ_KERNEL_PATH", kernel_path.to_string_lossy().to_string())
                    .env(
                        "VZ_BASE_DISK_PATH",
                        rootfs_path.to_string_lossy().to_string(),
                    );
            } else {
                println!("No bundled VZ resources found, will download from registry");
            }
        }
    }

    let (_rx, child) = sidecar
        .spawn()
        .map_err(|e| format!("Failed to spawn sidecar: {}", e))?;

    // The server handles its own logging via LOG_FILE + dup2.
    // We keep _rx alive (dropped when this function returns is fine since
    // child is moved out), but don't need to process stdout/stderr.

    Ok(child)
}

#[cfg(debug_assertions)]
pub(crate) fn initial_server_state() -> ServerState {
    ServerState {
        port: 3001,
        secret: String::new(),
    }
}

#[cfg(not(debug_assertions))]
pub(crate) fn initial_server_state() -> ServerState {
    ServerState {
        port: find_available_port(),
        secret: generate_secret(),
        ssh_port: preferred_ssh_port(),
        process: None,
    }
}

#[cfg(debug_assertions)]
pub(crate) fn setup_server(_app: &tauri::App) {}

#[cfg(not(debug_assertions))]
pub(crate) fn setup_server(app: &tauri::App) {
    if let Ok(log_path) = get_log_file_path() {
        println!("Server logs will be written to: {}", log_path.display());
    }

    let (port, ssh_port, secret) = {
        let state = app.state::<std::sync::Mutex<ServerState>>();
        let state = state.lock().unwrap();
        (state.port, state.ssh_port, state.secret.clone())
    };

    match start_server(app.handle(), port, ssh_port, &secret) {
        Ok(child) => {
            let state = app.state::<std::sync::Mutex<ServerState>>();
            state.lock().unwrap().process = Some(child);
            println!("Server started on port {}", port);
        }
        Err(e) => {
            eprintln!("Failed to start server: {}", e);
        }
    }
}
