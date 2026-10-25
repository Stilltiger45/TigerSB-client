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
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import app.tigersb.mobile.Mobile
import org.json.JSONObject
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

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            worker.execute { shutdown() }
            return START_NOT_STICKY
        }
        // ACTION_START from the app or the tile, or the system starting an
        // always-on VPN.
        startForegroundCompat(getString(R.string.vpn_connecting))
        worker.execute { connect() }
        return START_STICKY
    }

    override fun onRevoke() {
        // Another VPN app took over or the user turned us off in settings.
        worker.execute { shutdown() }
    }

    override fun onDestroy() {
        worker.execute { stopCore() }
        worker.shutdown()
        super.onDestroy()
    }

    private fun connect() {
        VpnState.set(VpnState.Phase.CONNECTING)
        try {
            Core.init(this)
            stopCore()
            val info = Mobile.current()
            if (info.isEmpty()) throw IllegalStateException(getString(R.string.vpn_no_key))
            val profile = JSONObject(info)
            val mode = Core.mode(this)

            val (transport, linkMtu) = activeNetwork()
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

            val name = profile.optString("name")
            startForegroundCompat(getString(R.string.vpn_connected, name))
            VpnState.set(VpnState.Phase.CONNECTED)
            watchNetwork()
        } catch (e: Exception) {
            Log.e(TAG, "connect", e)
            stopCore()
            VpnState.set(VpnState.Phase.ERROR, e.message ?: e.toString())
            stopForegroundCompat()
            stopSelf()
        }
    }

    private fun shutdown() {
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
     * Follows the real network. The app itself is outside the VPN, so its
     * default network is the Wi-Fi or mobile one. When the MTU that network
     * needs differs, the tunnel is rebuilt with the new one.
     */
    private fun watchNetwork() {
        if (networkCallback != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                setUnderlyingNetworks(arrayOf(network))
            }

            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) {
                worker.execute {
                    if (tun == null) return@execute
                    val (transport, linkMtu) = activeNetwork(network)
                    val want = Mobile.pickMTU(transport, linkMtu.toLong()).toInt()
                    if (want != mtu) {
                        Log.i(TAG, "network changed, MTU $mtu -> $want")
                        connect()
                    }
                }
            }
        }
        cm.registerDefaultNetworkCallback(cb)
        networkCallback = cb
    }

    private fun unwatchNetwork() {
        val cb = networkCallback ?: return
        networkCallback = null
        try {
            getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(cb)
        } catch (_: IllegalArgumentException) {
        }
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
    )

    private fun startForegroundCompat(text: String) {
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
            .setContentTitle("TigerSB")
            .setContentText(text)
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
