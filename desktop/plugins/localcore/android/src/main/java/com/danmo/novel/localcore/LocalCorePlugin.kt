package com.danmo.novel.localcore

import android.app.Activity
import android.view.WindowManager
import app.tauri.annotation.Command
import app.tauri.annotation.InvokeArg
import app.tauri.annotation.TauriPlugin
import app.tauri.plugin.Invoke
import app.tauri.plugin.JSObject
import app.tauri.plugin.Plugin
import danmonovel.Danmonovel
import org.json.JSONObject

@TauriPlugin
class LocalCorePlugin(private val activity: Activity) : Plugin(activity) {
    @Command
    fun bootstrap(invoke: Invoke) {
        val args = invoke.parseArgs(BootstrapArgs::class.java)
        val home = java.io.File(args.home).apply { mkdirs() }
        val result = Danmonovel.start(home.absolutePath)
        val error = JSONObject(result).optString("error")
        if (error.isNotEmpty()) {
            invoke.reject(error)
            return
        }
        activity.runOnUiThread {
            activity.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
        invoke.resolve(JSObject().put("ok", true))
    }

    @Command
    fun request(invoke: Invoke) {
        val args = invoke.parseArgs(RequestArgs::class.java)
        val raw = Danmonovel.request(args.method, args.path, args.body ?: "", "")
        invoke.resolve(JSObject().put("value", raw))
    }

    override fun onDestroy() {
        Danmonovel.stop()
        activity.runOnUiThread {
            activity.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }
}

@InvokeArg
class BootstrapArgs {
    var home: String = ""
}

@InvokeArg
class RequestArgs {
    var method: String = "GET"
    var path: String = ""
    var body: String? = null
}
