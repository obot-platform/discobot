use std::sync::Mutex;

use tauri::State;

use crate::server::ServerState;

fn desktop_server_port(state: State<'_, Mutex<ServerState>>) -> u16 {
    state.lock().unwrap().port
}

fn desktop_server_secret(state: State<'_, Mutex<ServerState>>) -> String {
    state.lock().unwrap().secret.clone()
}

#[tauri::command]
pub(crate) fn get_desktop_server_port(state: State<'_, Mutex<ServerState>>) -> u16 {
    desktop_server_port(state)
}

#[tauri::command]
pub(crate) fn get_desktop_server_secret(state: State<'_, Mutex<ServerState>>) -> String {
    desktop_server_secret(state)
}

#[tauri::command]
pub(crate) fn get_server_port(state: State<'_, Mutex<ServerState>>) -> u16 {
    desktop_server_port(state)
}

#[tauri::command]
pub(crate) fn get_server_secret(state: State<'_, Mutex<ServerState>>) -> String {
    desktop_server_secret(state)
}

#[tauri::command]
pub(crate) fn save_file_to_downloads(filename: String, content: Vec<u8>) -> Result<String, String> {
    let downloads_dir = dirs::download_dir()
        .or_else(|| dirs::home_dir().map(|h| h.join("Downloads")))
        .ok_or_else(|| "Could not determine Downloads directory".to_string())?;
    std::fs::create_dir_all(&downloads_dir)
        .map_err(|e| format!("Failed to create Downloads directory: {}", e))?;
    let path = downloads_dir.join(&filename);
    std::fs::write(&path, content).map_err(|e| format!("Failed to write file: {}", e))?;
    Ok(path.to_string_lossy().to_string())
}
