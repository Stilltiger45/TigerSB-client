package app.tigersb

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import app.tigersb.mobile.Mobile
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
                    "refresh" -> background(result) { Mobile.refresh() }
                    "forget" -> background(result) { Mobile.forget(); null }
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
                    "connect" -> connect(result)
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

    private fun connect(result: MethodChannel.Result) {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            // Only for the status notification; the VPN works without it.
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 2)
        }
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
        worker.shutdown()
        super.onDestroy()
    }
}
