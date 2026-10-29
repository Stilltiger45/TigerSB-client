package app.tigersb

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
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
import java.net.InetAddress
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException

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

        // Shows the notification again in the style the user just chose.
        const val ACTION_RESTYLE = "app.tigersb.RESTYLE"

        private const val CHANNEL = "vpn"
        private const val NOTIFICATION_ID = 1

        // How often the notification checks which protocol carries traffic.
        private const val PROTOCOL_EVERY_MS = 30_000L

        // The watchdog checks the channel this often, and RECHECK_MS after a
        // failed check. After FAILS_TO_RESET failed checks in a row it
        // reopens the connections to the servers, after FAILS_TO_RESTART it
        // restarts the core, at most once per RESTART_GAP_MS. While the
        // phone's network itself has no internet it only looks every
        // OFFLINE_RECHECK_MS for its return.
        private const val WATCH_EVERY_MS = 20_000L
        private const val RECHECK_MS = 3_000L
        private const val OFFLINE_RECHECK_MS = 2_000L
        private const val FAILS_TO_RESET = 2
        private const val FAILS_TO_RESTART = 4
        private const val RESTART_GAP_MS = 2 * 60_000L

        // After a network change the VPN moves to it once the network has
        // been stable for NET_SETTLE_MS, and at most once per
        // NET_SWITCH_GAP_MS.
        private const val NET_SETTLE_MS = 1_000L
        private const val NET_SWITCH_GAP_MS = 5_000L

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

        /** Restyles the notification of a running VPN; does nothing when it is off. */
        fun restyle(context: Context) {
            if (VpnState.phase == VpnState.Phase.DISCONNECTED || VpnState.phase == VpnState.Phase.ERROR) return
            context.startService(Intent(context, TigerVpnService::class.java).setAction(ACTION_RESTYLE))
        }
    }

    // All core calls run here, one at a time, off the main thread.
    private val worker = Executors.newSingleThreadExecutor()

    // The watchdog's checks take seconds when the channel is down, so they
    // run here: a change of network never waits for them.
    private val checker = Executors.newSingleThreadExecutor()
    private var checking = false
    // Grows with every connect and change of network, so that a check begun
    // before it is not judged after it.
    private var epoch = 0
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
    // The phone's network had no internet at the last check.
    private var offline = false
    private val watchTick = object : Runnable {
        override fun run() {
            worker.execute { watch() }
            main.postDelayed(this, WATCH_EVERY_MS)
        }
    }

    // The network the core's connections use, and its addresses: a new
    // network, or a lost address, means the connections are reopened.
    private var network: Network? = null
    private var addrs = emptySet<InetAddress>()
    private var lastNetSwitch = 0L

    // Networks with internet that are not a VPN, as the worker last heard.
    private val nets = HashMap<Network, NetworkCapabilities>()
    private val netSwitch = Runnable { onWorker { switchNetwork() } }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            worker.execute {
                note("отключение по кнопке")
                shutdown()
            }
            return START_NOT_STICKY
        }
        if (intent?.action == ACTION_RESTYLE) {
            // Started only for this: there is no VPN to restyle.
            val (title, detail) = shown ?: run {
                stopSelf(startId)
                return START_NOT_STICKY
            }
            startForegroundCompat(title, detail)
            return START_STICKY
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
        main.removeCallbacks(netSwitch)
        worker.execute {
            unwatchNetwork()
            stopCore()
        }
        worker.shutdown()
        checker.shutdown()
        super.onDestroy()
    }

    private fun connect() {
        epoch++
        main.removeCallbacks(netSwitch)
        VpnState.set(VpnState.Phase.CONNECTING)
        try {
            Core.init(this)
            stopCore()
            val info = Mobile.current()
            if (info.isEmpty()) throw IllegalStateException(getString(R.string.vpn_no_key))
            val profile = JSONObject(info)
            val mode = Core.mode(this)

            seedNetwork()
            network?.let { addrs = addresses(it) }
            val (transport, linkMtu) = activeNetwork(network)
            mtu = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()
            // The smart mode tunes its DPI evasion per kind of network.
            Mobile.setNetwork(transport)

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
            offline = false
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
     * session after the phone slept, or after the mobile signal dropped for a
     * while with Android still holding the network. Every check asks t.me and
     * YouTube through the server in use (Mobile.healthy); a failed one is
     * repeated after 3 seconds. Two failures in a row reopen the connections
     * to the servers, four restart the core. When the phone's network itself
     * has no internet, nothing is restarted: the watchdog waits for it and
     * reopens the connections the moment it is back, so the apps do not hang
     * on the dead ones. When the core is gone, the tunnel is rebuilt.
     */
    private fun watch() {
        if (tun == null || checking) return
        if (!Mobile.running()) {
            note("сторож: ядро не работает, переподключаю")
            connect()
            return
        }
        // The core started without the routing lists and has fetched them.
        if (Mobile.takeRestart()) {
            connect()
            return
        }
        checking = true
        val wasOffline = offline
        val began = epoch
        try {
            checker.execute {
                var online = if (wasOffline) Mobile.online() else true
                val healthy = online && Mobile.healthy()
                if (!healthy && !wasOffline) online = Mobile.online()
                onWorker {
                    checking = false
                    // A check from before a connect or a change of network
                    // says nothing about now: check again soon.
                    if (began == epoch) judge(healthy, online) else recheck(RECHECK_MS)
                }
            }
        } catch (_: RejectedExecutionException) {
            checking = false
        }
    }

    private fun judge(healthy: Boolean, online: Boolean) {
        if (tun == null) return
        if (healthy) {
            if (offline || fails >= FAILS_TO_RESET) {
                note("сторож: канал вернулся, переоткрыто соединений: ${Mobile.resetConnections()}")
            } else if (fails > 0) {
                note("сторож: сервер снова отвечает")
            }
            fails = 0
            offline = false
            return
        }
        if (!online) {
            if (!offline) note("сторож: в сети телефона нет интернета, жду его")
            offline = true
            recheck(OFFLINE_RECHECK_MS)
            return
        }
        // The internet is back, but not the channel: reopen at once.
        val back = offline
        offline = false
        fails++
        note("сторож: сервер не ответил ($fails)")
        val now = System.currentTimeMillis()
        if (fails >= FAILS_TO_RESTART && now - lastRestart > RESTART_GAP_MS) {
            lastRestart = now
            note("сторож: перезапускаю ядро")
            connect()
            return
        }
        if (back || fails == FAILS_TO_RESET) {
            note("сторож: переоткрыто соединений: ${Mobile.resetConnections()}")
        }
        // One failure may be a lost packet: check again soon.
        recheck(RECHECK_MS)
    }

    private fun recheck(delayMs: Long) {
        main.removeCallbacks(watchTick)
        main.postDelayed(watchTick, delayMs)
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
        main.removeCallbacks(netSwitch)
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
     * Follows the real network: of the networks with internet that are not
     * a VPN, the one Android would pick itself, a working Wi-Fi before mobile
     * data. When it changes, say from Wi-Fi to mobile and back, the core is
     * restarted: its sessions to the servers belonged to the old network and
     * would hang until they time out. When the MTU the network needs
     * differs, the tunnel is rebuilt with it.
     *
     * Only a plain listen is safe here. The default-network callback may
     * report our own tunnel, and registerBestMatchingNetworkCallback brought
     * the Android 14 emulator down within minutes in the soak test.
     */
    private fun watchNetwork() {
        if (networkCallback != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        val cb = object : ConnectivityManager.NetworkCallback() {
            // Android 7 may skip onCapabilitiesChanged after onAvailable.
            override fun onAvailable(n: Network) {
                cm.getNetworkCapabilities(n)?.let { onCapabilitiesChanged(n, it) }
            }

            override fun onCapabilitiesChanged(n: Network, caps: NetworkCapabilities) {
                onWorker {
                    nets[n] = caps
                    pickNetwork()
                }
            }

            override fun onLost(n: Network) {
                onWorker {
                    nets.remove(n)
                    if (n == network && nets.isEmpty()) note("сеть пропала, жду другую")
                    pickNetwork()
                }
            }

            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) {
                onWorker {
                    if (tun == null || network != this@TigerVpnService.network) return@onWorker
                    // The operator gave the phone another address: the
                    // connections from the old one are dead.
                    val now = lp.linkAddresses.map { it.address }.toSet()
                    val lost = !now.containsAll(addrs)
                    addrs = now
                    val (transport, linkMtu) = activeNetwork(network)
                    val want = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()
                    if (needsSmaller(want, linkMtu)) {
                        note("MTU сети $mtu -> $want, переподключаю")
                        connect()
                    } else if (lost) {
                        note("адрес в сети сменился, переоткрыто соединений: ${Mobile.resetConnections()}")
                    }
                }
            }
        }
        cm.registerNetworkCallback(request, cb)
        networkCallback = cb
    }

    /**
     * Before the first connect the network the core starts on is the one
     * Android uses now. Without it, the first network the callback reports,
     * often mobile data while Wi-Fi is up, would count as the one in use and
     * the next report would restart the fresh core.
     */
    private fun seedNetwork() {
        if (network != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val n = cm.activeNetwork ?: return
        val caps = cm.getNetworkCapabilities(n) ?: return
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return
        network = n
        nets[n] = caps
    }

    /** Follows the best of [nets]; the current one stays on a tie. */
    private fun pickNetwork() {
        fun score(c: NetworkCapabilities) =
            (if (c.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) 4 else 0) +
                (if (c.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) 0 else 2)
        val best = nets.entries.maxByOrNull { score(it.value) } ?: return
        val now = network
        val nowCaps = now?.let { nets[it] }
        if (now != null && nowCaps != null && score(nowCaps) >= score(best.value)) return
        follow(best.key)
    }

    /** Runs on the worker; does nothing once the service is gone. */
    private fun onWorker(block: () -> Unit) {
        try {
            worker.execute(block)
        } catch (_: RejectedExecutionException) {
        }
    }

    private fun follow(n: Network) {
        val old = network
        if (old == n) return
        network = n
        setUnderlyingNetworks(arrayOf(n))
        if (tun == null || old == null) return
        // A network that flaps is followed once, after it settles.
        val wait = maxOf(NET_SETTLE_MS, lastNetSwitch + NET_SWITCH_GAP_MS - SystemClock.elapsedRealtime())
        main.removeCallbacks(netSwitch)
        main.postDelayed(netSwitch, wait)
    }

    /**
     * Moves the VPN to the new network without rebuilding the tunnel: the
     * connections of the old network are closed, so the apps reconnect at
     * once over the new one (Mobile.switchNetwork). Only a path measured
     * narrower than the tunnel's MTU rebuilds it; a wider one, or one that
     * could not be measured, keeps the tunnel's until the next connect.
     */
    private fun switchNetwork() {
        val n = network ?: return
        if (tun == null) return
        lastNetSwitch = SystemClock.elapsedRealtime()
        epoch++
        addrs = addresses(n)
        val (transport, linkMtu) = activeNetwork(n)
        val kind = transport.ifEmpty { "?" }
        val want = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()
        if (needsSmaller(want, linkMtu)) {
            note("сеть сменилась ($kind), MTU $mtu -> $want, переподключаю")
            connect()
            return
        }
        val closed = Mobile.switchNetwork(transport)
        note("сеть сменилась ($kind): переоткрыто соединений $closed, туннель тот же")
        fails = 0
        offline = false
        // Check the channel on the new network soon.
        recheck(RECHECK_MS)
    }

    /**
     * Whether the tunnel must be rebuilt with the smaller MTU [want]: only
     * when the path was measured narrower, or the network itself says so. A
     * guess for a path that drops pings, common on mobile data, keeps the
     * working tunnel; a switch from Wi-Fi to such a network stays seamless.
     */
    private fun needsSmaller(want: Int, linkMtu: Int): Boolean =
        want < mtu && (Mobile.pathKnown() || linkMtu in 1 until mtu)

    private fun addresses(n: Network): Set<InetAddress> =
        getSystemService(ConnectivityManager::class.java).getLinkProperties(n)
            ?.linkAddresses?.map { it.address }?.toSet() ?: emptySet()

    private fun unwatchNetwork() {
        val cb = networkCallback ?: return
        networkCallback = null
        network = null
        addrs = emptySet()
        nets.clear()
        try {
            getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(cb)
        } catch (_: IllegalArgumentException) {
        }
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
    )

    // The notification's title and detail, to show them again in a new style.
    @Volatile
    private var shown: Pair<String, String>? = null

    /**
     * title on top; detail, such as the protocol, below it when set. The icons
     * and the colour are the style's.
     */
    private fun startForegroundCompat(title: String, detail: String = "") {
        shown = title to detail
        val look = Core.look(this)
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
            .setSmallIcon(look.statusIcon)
            .setLargeIcon(Icon.createWithResource(this, look.launcherIcon))
            .setColor(look.color)
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
