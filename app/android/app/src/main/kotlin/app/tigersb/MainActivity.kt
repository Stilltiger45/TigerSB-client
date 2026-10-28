package app.tigersb

import android.Manifest
import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.view.WindowManager
import app.tigersb.mobile.Mobile
import org.json.JSONArray
import java.io.ByteArrayOutputStream
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

/** Bridges the Flutter screen to the Go core and the VPN service. */
class MainActivity : FlutterActivity() {
    companion object {
        private const val REQ_VPN = 1
    }

    private val main = Handler(Looper.getMainLooper())
    private val worker = Executors.newSingleThreadExecutor()
    private var channel: MethodChannel? = null
    private var pendingConnect: MethodChannel.Result? = null
    private var pendingKey: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        pendingKey = keyFrom(intent)
        super.onCreate(savedInstanceState)
        // Other apps must not read or steer the screen: no screenshots,
        // screen recording or recents preview, no taps through an overlay,
        // and other apps' overlays hidden while TigerSB is open.
        window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)
        window.decorView.filterTouchesWhenObscured = true
        if (Build.VERSION.SDK_INT >= 31) window.setHideOverlayWindows(true)
        val filter = IntentFilter(Updates.ACTION_STATUS)
        if (Build.VERSION.SDK_INT >= 33) registerReceiver(updateStatus, filter, Context.RECEIVER_NOT_EXPORTED)
        else registerReceiver(updateStatus, filter)
    }

    /** The system installer's answer to an update: asks the user, or tells why it failed. */
    private val updateStatus = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            // Before Android 13 the receiver is open to other apps too: only
            // the answer about the session this app started counts.
            val id = intent.getIntExtra(PackageInstaller.EXTRA_SESSION_ID, -1)
            if (id < 0 || id != Updates.session) return
            val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
            if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
                val confirm = if (Build.VERSION.SDK_INT >= 33) {
                    intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
                } else {
                    @Suppress("DEPRECATION")
                    intent.getParcelableExtra(Intent.EXTRA_INTENT)
                }
                confirm?.let { startActivity(it.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
                return
            }
            if (status == PackageInstaller.STATUS_SUCCESS) return
            Updates.failure(status, intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE))
                ?.let { channel?.invokeMethod("updateFailed", it) }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        keyFrom(intent)?.let { channel?.invokeMethod("key", it) }
    }

    private fun keyFrom(intent: Intent?): String? =
        intent?.data?.takeIf { it.scheme == "tigersb" }?.toString()

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        Core.init(this)
        val messenger = flutterEngine.dartExecutor.binaryMessenger

        channel = MethodChannel(messenger, "tigersb/core").apply {
            setMethodCallHandler { call, result ->
                when (call.method) {
                    "current" -> background(result) { Mobile.current() }
                    "addKey" -> background(result) { Mobile.addKey(call.argument<String>("key") ?: "") }
                    "profiles" -> background(result) { Mobile.profiles() }
                    "refresh" -> background(result) {
                        val id = call.argument<String>("id")
                        if (id.isNullOrEmpty()) Mobile.refresh() else Mobile.refreshProfile(id)
                    }
                    "select" -> background(result) {
                        val info = Mobile.select(call.argument<String>("id") ?: "")
                        // A running VPN moves to the profile by reconnecting.
                        if (VpnState.phase == VpnState.Phase.CONNECTED) TigerVpnService.start(this@MainActivity)
                        info
                    }
                    "delete" -> background(result) { Mobile.delete(call.argument<String>("id") ?: "") }
                    "stats" -> background(result) { Mobile.stats() }
                    "apps" -> background(result) { installedApps() }
                    "setUserApps" -> background(result) {
                        Mobile.setUserApps(JSONArray(call.argument<List<String>>("apps") ?: emptyList<String>()).toString())
                        // A running VPN leaves the apps out after reconnecting.
                        if (VpnState.phase == VpnState.Phase.CONNECTED) TigerVpnService.start(this@MainActivity)
                        null
                    }
                    "takeKey" -> {
                        result.success(pendingKey)
                        pendingKey = null
                    }
                    "status" -> result.success(VpnState.toMap())
                    "mode" -> result.success(Core.mode(this@MainActivity))
                    "setMode" -> {
                        Core.setMode(this@MainActivity, call.argument<String>("mode") ?: "auto")
                        // A running VPN picks the new mode up by reconnecting.
                        if (VpnState.phase == VpnState.Phase.CONNECTED) TigerVpnService.start(this@MainActivity)
                        result.success(null)
                    }
                    "lightTheme" -> result.success(Core.lightTheme(this@MainActivity))
                    "setLightTheme" -> {
                        Core.setLightTheme(this@MainActivity, call.argument<Boolean>("light") == true)
                        result.success(null)
                    }
                    "connect" -> connect(result)
                    "shareJournal" -> background(result) {
                        val text = Mobile.journal()
                        main.post {
                            val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                                .putExtra(Intent.EXTRA_SUBJECT, "Журнал TigerSB")
                                .putExtra(Intent.EXTRA_TEXT, text)
                            startActivity(Intent.createChooser(send, "Отправить журнал"))
                        }
                        null
                    }
                    "about" -> background(result) { Mobile.about() }
                    "checkUpdate" -> background(result) {
                        try {
                            Updates.check(this@MainActivity)
                        } catch (e: Exception) {
                            throw Exception("Не удалось проверить обновление: ${e.message ?: e}")
                        }
                    }
                    "installUpdate" -> installUpdate(result)
                    "openUrl" -> {
                        val ok = try {
                            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(call.argument<String>("url") ?: "")))
                            true
                        } catch (_: ActivityNotFoundException) {
                            false
                        }
                        result.success(ok)
                    }
                    "disconnect" -> {
                        TigerVpnService.stop(this@MainActivity)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        }

        EventChannel(messenger, "tigersb/status").setStreamHandler(object : EventChannel.StreamHandler {
            private var listener: (() -> Unit)? = null

            override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
                val l = { events.success(VpnState.toMap()) }
                listener = l
                VpnState.listen(l)
                l()
            }

            override fun onCancel(arguments: Any?) {
                listener?.let { VpnState.unlisten(it) }
                listener = null
            }
        })
    }

    /** Apps with a launcher icon, but TigerSB itself: package, label and a small PNG icon. */
    private fun installedApps(): List<Map<String, Any>> {
        val pm = packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        @Suppress("DEPRECATION")
        val found = pm.queryIntentActivities(launcher, 0)
        val size = (40 * resources.displayMetrics.density).toInt()
        return found.map { it.activityInfo.applicationInfo }
            .distinctBy { it.packageName }
            .filter { it.packageName != packageName }
            .map { ai ->
                val bmp = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
                pm.getApplicationIcon(ai).apply { setBounds(0, 0, size, size); draw(Canvas(bmp)) }
                val png = ByteArrayOutputStream().also { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
                mapOf("package" to ai.packageName, "label" to pm.getApplicationLabel(ai).toString(), "icon" to png)
            }
            .sortedBy { (it["label"] as String).lowercase() }
    }

    /** Runs a core call off the main thread and answers on it. */
    private fun background(result: MethodChannel.Result, call: () -> Any?) {
        worker.execute {
            try {
                val v = call()
                main.post { result.success(v) }
            } catch (e: Exception) {
                main.post { result.error("core", e.message ?: e.toString(), null) }
            }
        }
    }

    /** Downloads the update on a thread of its own, so the core's calls do not wait for it, and installs it. */
    private fun installUpdate(result: MethodChannel.Result) {
        Thread {
            try {
                val apk = Updates.download(this) { p -> main.post { channel?.invokeMethod("updateProgress", p) } }
                val started = Updates.install(this, apk)
                main.post { result.success(started) }
            } catch (e: Exception) {
                main.post { result.error("update", "Не удалось скачать обновление: ${e.message ?: e}", null) }
            }
        }.start()
    }

    private fun connect(result: MethodChannel.Result) {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            // Only for the status notification; the VPN works without it.
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 2)
        }
        keepAliveInBackground()
        val ask = VpnService.prepare(this)
        if (ask == null) {
            TigerVpnService.start(this)
            result.success(true)
            return
        }
        pendingConnect?.success(false)
        pendingConnect = result
        startActivityForResult(ask, REQ_VPN)
    }

    /**
     * Asks once to leave TigerSB out of battery saving: on many phones
     * (Xiaomi, Huawei, Samsung) it otherwise stops the VPN minutes after the
     * screen goes off.
     */
    private fun keepAliveInBackground() {
        val prefs = getSharedPreferences("tigersb", MODE_PRIVATE)
        if (prefs.getBoolean("askedBattery", false)) return
        val pm = getSystemService(PowerManager::class.java)
        if (pm.isIgnoringBatteryOptimizations(packageName)) return
        prefs.edit().putBoolean("askedBattery", true).apply()
        try {
            @SuppressLint("BatteryLife")
            val i = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName"))
            startActivity(i)
        } catch (e: Exception) {
            Mobile.note("не удалось спросить про экономию батареи: ${e.message}")
        }
    }

    @Deprecated("FlutterActivity still delivers results here")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_VPN) return
        val ok = resultCode == RESULT_OK
        if (ok) TigerVpnService.start(this)
        pendingConnect?.success(ok)
        pendingConnect = null
    }

    override fun onDestroy() {
        unregisterReceiver(updateStatus)
        worker.shutdown()
        super.onDestroy()
    }
}
