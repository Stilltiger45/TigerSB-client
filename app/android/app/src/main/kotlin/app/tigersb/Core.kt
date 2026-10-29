package app.tigersb

import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Context
import android.content.pm.PackageManager
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
     * Routing mode: "auto" (as "basic", plus a DPI evasion preset picked for
     * the network), "basic" (Russian sites direct) or "all" (everything
     * through the VPN).
     */
    fun mode(context: Context): String = prefs(context).getString("mode", "auto") ?: "auto"

    fun setMode(context: Context, mode: String) {
        prefs(context).edit().putString("mode", mode).apply()
    }

    /** The app's theme: light when the user chose it in «О приложении», dark otherwise. */
    fun lightTheme(context: Context): Boolean = prefs(context).getBoolean("lightTheme", false)

    fun setLightTheme(context: Context, light: Boolean) {
        prefs(context).edit().putBoolean("lightTheme", light).apply()
    }

    /** The app's style: the launcher alias of each, which carries its icon. */
    private val styles = mapOf(
        "tiger" to "app.tigersb.StyleTiger",
        "zarya" to "app.tigersb.StyleZarya",
        "curse" to "app.tigersb.StyleCurse",
        "miku" to "app.tigersb.StyleMiku",
    )

    fun style(context: Context): String = prefs(context).getString("style", "tiger") ?: "tiger"

    /** How the style looks outside the app: in the status bar, the notification and the tile. */
    class Look(val statusIcon: Int, val launcherIcon: Int, val color: Int)

    fun look(context: Context): Look = when (style(context)) {
        "zarya" -> Look(R.drawable.ic_stat_zarya, R.mipmap.ic_launcher_zarya, 0xFF8BE33A.toInt())
        "curse" -> Look(R.drawable.ic_stat_curse, R.mipmap.ic_launcher_curse, 0xFFEE1C2E.toInt())
        "miku" -> Look(R.drawable.ic_stat_miku, R.mipmap.ic_launcher_miku, 0xFF39C5BB.toInt())
        else -> Look(R.drawable.ic_stat_tiger, R.mipmap.ic_launcher, 0xFFFF7A1A.toInt())
    }

    /**
     * Saves the style and shows its icon on the home screen: its launcher
     * alias is turned on before the others are turned off, so the app always
     * has one. The launcher takes a few seconds to notice.
     */
    fun setStyle(context: Context, style: String) {
        val chosen = if (style in styles) style else "tiger"
        prefs(context).edit().putString("style", chosen).apply()
        val pm = context.packageManager
        fun set(alias: String, on: Boolean) = pm.setComponentEnabledSetting(
            ComponentName(context.packageName, alias),
            if (on) PackageManager.COMPONENT_ENABLED_STATE_ENABLED else PackageManager.COMPONENT_ENABLED_STATE_DISABLED,
            PackageManager.DONT_KILL_APP,
        )
        set(styles.getValue(chosen), true)
        for ((name, alias) in styles) if (name != chosen) set(alias, false)
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
