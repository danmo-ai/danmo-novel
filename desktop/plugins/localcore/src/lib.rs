use serde::{Deserialize, Serialize};
#[cfg(mobile)]
use serde_json::Value;
#[cfg(mobile)]
use tauri::Manager;
use tauri::{AppHandle, Runtime};

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CoreInfo {
    pub transport: &'static str,
    pub api_base: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CoreRequest {
    pub method: String,
    pub path: String,
    #[serde(default)]
    pub body: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct CoreResponse {
    pub status: u16,
    pub body: String,
}

#[cfg(mobile)]
mod mobile {
    use super::*;
    #[cfg(target_os = "ios")]
    tauri::ios_plugin_binding!(init_plugin_localcore);

    pub fn init<R: Runtime, C: serde::de::DeserializeOwned>(
        _app: &AppHandle<R>,
        api: tauri::plugin::PluginApi<R, C>,
    ) -> Result<tauri::plugin::PluginHandle<R>, String> {
        #[cfg(target_os = "ios")]
        return api
            .register_ios_plugin(init_plugin_localcore)
            .map_err(|e| e.to_string());
        #[cfg(target_os = "android")]
        return api
            .register_android_plugin("com.danmo.novel.localcore", "LocalCorePlugin")
            .map_err(|e| e.to_string());
    }
}

#[tauri::command]
fn core_bootstrap<R: Runtime>(app: AppHandle<R>) -> Result<CoreInfo, String> {
    #[cfg(desktop)]
    {
        let _ = app;
        return Ok(CoreInfo {
            transport: "http",
            api_base: Some("http://127.0.0.1:7804".into()),
        });
    }
    #[cfg(mobile)]
    {
        let home = app.path().app_data_dir().map_err(|e| e.to_string())?;
        let response: Value = app
            .localcore()
            .run_mobile_plugin("bootstrap", serde_json::json!({ "home": home }))
            .map_err(|e| e.to_string())?;
        if let Some(error) = response.get("error").and_then(Value::as_str) {
            return Err(error.to_owned());
        }
        return Ok(CoreInfo {
            transport: "ffi",
            api_base: None,
        });
    }
}

#[tauri::command]
fn core_request<R: Runtime>(
    app: AppHandle<R>,
    request: CoreRequest,
) -> Result<CoreResponse, String> {
    #[cfg(desktop)]
    {
        let _ = (app, request);
        Err("HTTP transport is used on desktop".into())
    }
    #[cfg(mobile)]
    {
        let value: Value = app
            .localcore()
            .run_mobile_plugin("request", request)
            .map_err(|e| e.to_string())?;
        let raw = value
            .get("value")
            .and_then(Value::as_str)
            .unwrap_or_default();
        serde_json::from_str(raw).map_err(|e| format!("{e}: {raw}"))
    }
}

#[tauri::command]
fn bootstrap<R: Runtime>(app: AppHandle<R>) -> Result<CoreInfo, String> {
    core_bootstrap(app)
}

#[tauri::command]
fn request<R: Runtime>(app: AppHandle<R>, request: CoreRequest) -> Result<CoreResponse, String> {
    core_request(app, request)
}

#[cfg(mobile)]
pub trait LocalCoreExt<R: Runtime> {
    fn localcore(&self) -> &tauri::plugin::PluginHandle<R>;
}
#[cfg(mobile)]
impl<R: Runtime, T: tauri::Manager<R>> LocalCoreExt<R> for T {
    fn localcore(&self) -> &tauri::plugin::PluginHandle<R> {
        self.state::<tauri::plugin::PluginHandle<R>>().inner()
    }
}

pub fn init<R: Runtime>() -> tauri::plugin::TauriPlugin<R> {
    tauri::plugin::Builder::new("localcore")
        .invoke_handler(tauri::generate_handler![bootstrap, request])
        .setup(|_app, _api| {
            #[cfg(mobile)]
            {
                let handle = mobile::init(_app, _api).map_err(|e| -> Box<dyn std::error::Error> { e.into() })?;
                _app.manage(handle);
            }
            Ok(())
        })
        .build()
}
