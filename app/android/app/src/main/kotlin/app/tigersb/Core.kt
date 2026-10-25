package app.tigersb

import android.annotation.SuppressLint
import android.content.Context
import android.os.Build
import android.provider.Settings
import android.os.Handler
import android.os.Looper
import app.tigersb.mobile.Mobile
import java.util.concurrent.CopyOnWriteArrayList

/** Starts the Go core once per process and remembers the user's choices. */
object Core {
    @Volatile
    private var ready = false

    fun init(context: Context) {
        if (ready) return
        synchronized(this) {
            if (ready) return
            val app = context.applicationContext
            val version = app.packageManager.getPackageInfo(app.packageName, 0).versionName ?: "dev"
            Mobile.init(app.filesDir.absolutePath, version)
            try {
                Vault.stateKey(app)?.let { Mobile.setStateKey(it) }
            } catch (e: Exception) {
                // The VPN still works; the state just stays unencrypted.
                android.util.Log.e("TigerSB", "state key", e)
            }
            // Panels with a device limit count devices by this id. ANDROID_ID
            // stays the same for this app until a factory reset.
            @SuppressLint("HardwareIds")
            val hwid = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: ""
            Mobile.setDevice(hwid, "Android", Build.VERSION.RELEASE ?: "", Build.MODEL ?: "")
            ready = true
        }
    }

    private fun prefs(context: Context) =
        context.applicationContext.getSharedPreferences("tigersb", Context.MODE_PRIVATE)

    /**
     * Routing mode: "auto" (every site checked through all servers), "basic"
     * (Russian sites direct) or "all" (everything through the VPN).
     */
    fun mode(context: Context): String = prefs(context).getString("mode", "auto") ?: "auto"

    fun setMode(context: Context, mode: String) {
        prefs(context).edit().putString("mode", mode).apply()
    }
}

/** What the VPN is doing now. The service writes it, the screen and the tile read it. */
object VpnState {
    enum class Phase { DISCONNECTED, CONNECTING, CONNECTED, ERROR }

    @Volatile
    var phase = Phase.DISCONNECTED
        private set

    @Volatile
    var error: String? = null
        private set

    private val main = Handler(Looper.getMainLooper())
    private val listeners = CopyOnWriteArrayList<() -> Unit>()

    fun set(p: Phase, err: String? = null) {
        phase = p
        error = err
        main.post { listeners.forEach { it() } }
    }

    fun listen(l: () -> Unit) = listeners.add(l)

    fun unlisten(l: () -> Unit) = listeners.remove(l)

    fun toMap(): Map<String, Any?> = mapOf("phase" to phase.name.lowercase(), "error" to error)
}
