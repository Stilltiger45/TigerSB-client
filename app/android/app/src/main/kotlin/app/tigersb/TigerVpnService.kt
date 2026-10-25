package app.tigersb

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import android.util.Log
import app.tigersb.mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors

/**
 * Opens the system VPN and hands its descriptor to the Go core.
 *
 * The app's own traffic is excluded from the VPN, so Xray reaches its servers
 * and the direct sites over the real network without loops. The TUN takes
 * IPv4 and IPv6; the core answers DNS itself and refuses IPv6, so apps fall
 * back to IPv4 and nothing leaks past the tunnel.
 */
class TigerVpnService : VpnService() {
    companion object {
        private const val TAG = "TigerSB"
        const val ACTION_START = "app.tigersb.START"
        const val ACTION_STOP = "app.tigersb.STOP"
        private const val CHANNEL = "vpn"
        private const val NOTIFICATION_ID = 1
        private const val ORANGE = 0xFFFF7A1A.toInt()

        // How often the notification checks which protocol carries traffic.
        private const val PROTOCOL_EVERY_MS = 30_000L

        // The watchdog checks the tunnel this often; after FAILS_TO_RESTART
        // failed checks in a row it restarts the core, at most once per
        // RESTART_GAP_MS.
        private const val WATCH_EVERY_MS = 60_000L
        private const val FAILS_TO_RESTART = 3
        private const val RESTART_GAP_MS = 5 * 60_000L

        // After a network change the core restarts once the network has
        // been stable for NET_SETTLE_MS, and at most once per
        // NET_RESTART_GAP_MS.
        private const val NET_SETTLE_MS = 1_500L
        private const val NET_RESTART_GAP_MS = 10_000L

        // Addresses inside the TUN. DNS goes to TUN_DNS, the core answers it.
        private const val TUN_ADDR = "172.19.0.1"
        private const val TUN_DNS = "172.19.0.2"
        private const val TUN_ADDR6 = "fdfe:dcba:9876::1"

        fun start(context: Context) {
            val i = Intent(context, TigerVpnService::class.java).setAction(ACTION_START)
            if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(i) else context.startService(i)
        }

        fun stop(context: Context) {
            context.startService(Intent(context, TigerVpnService::class.java).setAction(ACTION_STOP))
        }
    }

    // All core calls run here, one at a time, off the main thread.
    private val worker = Executors.newSingleThreadExecutor()
    private var tun: ParcelFileDescriptor? = null
    private var mtu = 0
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var profileName = ""
    private var protocol = ""
    private val main = Handler(Looper.getMainLooper())
    private val protocolTick = object : Runnable {
        override fun run() {
            worker.execute { showProtocol() }
            main.postDelayed(this, PROTOCOL_EVERY_MS)
        }
    }
    private var fails = 0
    private var lastRestart = 0L
    private val watchTick = object : Runnable {
        override fun run() {
            worker.execute { watch() }
            main.postDelayed(this, WATCH_EVERY_MS)
        }
    }

    // The network the core's connections use; a new one means a restart.
    private var network: Network? = null
    private var lastNetRestart = 0L
    private val netRestart = Runnable { worker.execute { restartForNetwork() } }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            worker.execute {
                note("отключение по кнопке")
                shutdown()
            }
            return START_NOT_STICKY
        }
        if (intent == null) worker.execute { note("Android перезапустил службу VPN") }
        // ACTION_START from the app or the tile, or the system starting an
        // always-on VPN.
        startForegroundCompat(getString(R.string.vpn_connecting))
        worker.execute { connect() }
        return START_STICKY
    }

    override fun onRevoke() {
        // Another VPN app took over or the user turned us off in settings.
        worker.execute {
            note("Android отозвал VPN: включён другой VPN или выключено в настройках")
            shutdown()
        }
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        if (level >= android.content.ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) worker.execute { note("мало памяти: уровень $level") }
    }

    override fun onDestroy() {
        note("служба VPN остановлена")
        main.removeCallbacks(protocolTick)
        main.removeCallbacks(watchTick)
        main.removeCallbacks(netRestart)
        worker.execute { stopCore() }
        worker.shutdown()
        super.onDestroy()
    }

    private fun connect() {
        main.removeCallbacks(netRestart)
        VpnState.set(VpnState.Phase.CONNECTING)
        try {
            Core.init(this)
            stopCore()
            val info = Mobile.current()
            if (info.isEmpty()) throw IllegalStateException(getString(R.string.vpn_no_key))
            val profile = JSONObject(info)
            val mode = Core.mode(this)

            val (transport, linkMtu) = activeNetwork(network)
            mtu = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()

            val b = Builder()
                .setSession("TigerSB")
                .setMtu(mtu)
                .addAddress(TUN_ADDR, 30)
                .addRoute("0.0.0.0", 0)
                .addDnsServer(TUN_DNS)
                .addAddress(TUN_ADDR6, 126)
                .addRoute("::", 0)
                .setConfigureIntent(openAppIntent())
            if (Build.VERSION.SDK_INT >= 29) b.setMetered(false)
            b.addDisallowedApplication(packageName)
            val apps = profile.optJSONArray("directApps")
            for (i in 0 until (apps?.length() ?: 0)) {
                try {
                    b.addDisallowedApplication(apps!!.getString(i))
                } catch (_: PackageManager.NameNotFoundException) {
                    // The bank app is not installed: nothing to exclude.
                }
            }
            val pfd = b.establish() ?: throw IllegalStateException(getString(R.string.vpn_not_allowed))
            tun = pfd
            Mobile.start(pfd.fd.toLong(), mode, mtu.toLong())
            note("подключено: режим $mode, MTU $mtu, ${transport.ifEmpty { "сеть ?" }}, файлов открыто ${openFiles()}")

            profileName = profile.optString("name")
            protocol = Mobile.protocol()
            startForegroundCompat(getString(R.string.vpn_connected, profileName), protocol)
            VpnState.set(VpnState.Phase.CONNECTED)
            watchNetwork()
            main.removeCallbacks(protocolTick)
            main.postDelayed(protocolTick, PROTOCOL_EVERY_MS)
            fails = 0
            main.removeCallbacks(watchTick)
            main.postDelayed(watchTick, WATCH_EVERY_MS)
        } catch (e: Exception) {
            Log.e(TAG, "connect", e)
            note("ошибка подключения: ${e.message ?: e}")
            stopCore()
            VpnState.set(VpnState.Phase.ERROR, e.message ?: e.toString())
            stopForegroundCompat()
            stopSelf()
        }
    }

    /**
     * Shows the protocol of the server in use. Servers are measured after the
     * start, so the fastest one, and its protocol, can change.
     */
    private fun showProtocol() {
        if (tun == null) return
        val now = Mobile.protocol()
        if (now == protocol) return
        protocol = now
        startForegroundCompat(getString(R.string.vpn_connected, profileName), protocol)
    }

    /**
     * The watchdog. The core can lose its server without noticing: a dead
     * session after the phone slept or switched networks. When a page has
     * not opened through the server for three checks in a row, the core is
     * restarted; when the core is gone, the tunnel is rebuilt.
     */
    private fun watch() {
        if (tun == null) return
        if (!Mobile.running()) {
            note("сторож: ядро не работает, переподключаю")
            connect()
            return
        }
        if (Mobile.healthy()) {
            if (fails > 0) note("сторож: сервер снова отвечает")
            fails = 0
            return
        }
        fails++
        note("сторож: сервер не ответил ($fails)")
        val now = System.currentTimeMillis()
        if (fails >= FAILS_TO_RESTART && now - lastRestart > RESTART_GAP_MS) {
            lastRestart = now
            note("сторож: перезапускаю ядро")
            connect()
        }
    }

    /** Open files of the app: a number that keeps growing is a leak. */
    private fun openFiles(): Int = File("/proc/self/fd").list()?.size ?: -1

    private fun note(event: String) {
        Log.i(TAG, event)
        try {
            Mobile.note(event)
        } catch (_: Throwable) {
        }
    }

    private fun shutdown() {
        main.removeCallbacks(protocolTick)
        main.removeCallbacks(watchTick)
        main.removeCallbacks(netRestart)
        unwatchNetwork()
        stopCore()
        VpnState.set(VpnState.Phase.DISCONNECTED)
        stopForegroundCompat()
        stopSelf()
    }

    private fun stopCore() {
        try {
            Mobile.stop()
        } catch (e: Exception) {
            Log.w(TAG, "stop", e)
        }
        tun?.close()
        tun = null
    }

    /** The transport and MTU of the network the phone uses now. */
    private fun activeNetwork(network: Network? = null): Pair<String, Int> {
        val cm = getSystemService(ConnectivityManager::class.java)
        val n = network ?: cm.activeNetwork ?: return "" to 0
        val caps = cm.getNetworkCapabilities(n)
        val transport = when {
            caps == null -> ""
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> ""
        }
        val linkMtu = if (Build.VERSION.SDK_INT >= 29) cm.getLinkProperties(n)?.mtu ?: 0 else 0
        return transport to linkMtu
    }

    /**
     * Follows the real network: the best one with internet that is not a
     * VPN, the one Android would use without us. When it changes, say from
     * Wi-Fi to mobile and back, the core is restarted: its sessions to the
     * servers belonged to the old network and would hang until they time
     * out. When the MTU the network needs differs, the tunnel is rebuilt
     * with it. The default-network callback is no good here: Android may
     * report our own tunnel as the default network.
     */
    private fun watchNetwork() {
        if (networkCallback != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(n: Network) {
                val caps = cm.getNetworkCapabilities(n) ?: return
                if (!caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) follow(n)
            }

            override fun onLost(n: Network) {
                worker.execute { if (n == network) note("сеть пропала, жду другую") }
            }

            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) {
                worker.execute {
                    if (tun == null || network != this@TigerVpnService.network) return@execute
                    val (transport, linkMtu) = activeNetwork(network)
                    val want = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()
                    if (want != mtu) {
                        note("MTU сети $mtu -> $want, переподключаю")
                        connect()
                    }
                }
            }
        }
        // Both give the network that satisfies the request now and a new
        // onAvailable when another one takes over.
        cm.registerDefaultNetworkCallback(cb)
        networkCallback = cb
    }

    private fun follow(n: Network) {
        worker.execute {
            val old = network
            if (old == n) return@execute
            network = n
            setUnderlyingNetworks(arrayOf(n))
            if (tun == null || old == null) return@execute
            // A network that flaps restarts the core once, after it settles.
            val wait = maxOf(NET_SETTLE_MS, lastNetRestart + NET_RESTART_GAP_MS - SystemClock.elapsedRealtime())
            main.removeCallbacks(netRestart)
            main.postDelayed(netRestart, wait)
        }
    }

    private fun restartForNetwork() {
        val n = network ?: return
        if (tun == null) return
        lastNetRestart = SystemClock.elapsedRealtime()
        note("сеть сменилась (${activeNetwork(n).first.ifEmpty { "?" }}), переподключаю")
        connect()
    }

    private fun unwatchNetwork() {
        val cb = networkCallback ?: return
        networkCallback = null
        network = null
        try {
            getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(cb)
        } catch (_: IllegalArgumentException) {
        }
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
    )

    /** title on top; detail, such as the protocol, below it when set. */
    private fun startForegroundCompat(title: String, detail: String = "") {
        val nm = getSystemService(NotificationManager::class.java)
        val builder = if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL, getString(R.string.vpn_channel), NotificationManager.IMPORTANCE_LOW),
            )
            Notification.Builder(this, CHANNEL)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        val stop = PendingIntent.getService(
            this, 1, Intent(this, TigerVpnService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE,
        )
        val n = builder
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setColor(ORANGE)
            .setSubText("TigerSB")
            .setContentTitle(title)
            .setContentText(detail.ifEmpty { null })
            .setOngoing(true)
            .setContentIntent(openAppIntent())
            .addAction(Notification.Action.Builder(null, getString(R.string.vpn_disconnect), stop).build())
            .build()
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, n)
        }
    }

    private fun stopForegroundCompat() = stopForeground(STOP_FOREGROUND_REMOVE)
}
