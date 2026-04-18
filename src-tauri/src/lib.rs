mod app_updater;
mod commands;
mod server;
mod tray;

use std::sync::Mutex;

use tauri_plugin_window_state::StateFlags;
use tray::show_window;

fn window_state_flags() -> StateFlags {
    // Save all state except decorations (we manage those ourselves)
    StateFlags::all() - StateFlags::DECORATIONS
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            show_window(app);
        }))
        .plugin(
            tauri_plugin_window_state::Builder::new()
                .with_state_flags(window_state_flags())
                .build(),
        )
        .manage(Mutex::new(server::initial_server_state()))
        .setup(|app| {
            tray::sync_macos_activation_policy(app);
            server::setup_server(app);
            tray::setup_tray(app)?;
            Ok(())
        })
        .on_window_event(tray::handle_window_event)
        .invoke_handler(tauri::generate_handler![
            commands::get_desktop_server_port,
            commands::get_desktop_server_secret,
            commands::get_server_port,
            commands::get_server_secret,
            commands::save_file_to_downloads,
            app_updater::check_for_app_update,
            app_updater::download_app_update,
            app_updater::install_app_update,
            app_updater::close_app_update
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
