package app.tigersb

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import android.os.Process
import android.provider.Settings
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * Updates the app from its latest GitHub release. The release carries the two
 * APKs and version.json, which gives each APK's versionCode: an update is
 * the APK of this phone's kind with a higher versionCode than the installed
 * one. Android installs it over the app only when it is signed with the same
 * key.
 */
object Updates {
    private const val RELEASE = "https://github.com/Stilltiger45/TigerSB-client/releases/latest/download/"

    /** What PackageInstaller tells about the install arrives with this action. */
    const val ACTION_STATUS = "app.tigersb.UPDATE_STATUS"

    /** The session of the install under way, so a status from elsewhere is ignored. */
    @Volatile
    var session = -1
        private set

    /** The APK the app was installed from: the 32-bit one on old phones. */
    private fun apkName() =
        if (Process.is64Bit()) "TigerSB-android.apk" else "TigerSB-android-old-phones.apk"

    private fun installedCode(ctx: Context): Long {
        val info = ctx.packageManager.getPackageInfo(ctx.packageName, 0)
        @Suppress("DEPRECATION")
        return if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else info.versionCode.toLong()
    }

    /** The newer version on the release as JSON {version, build}, or null when this one is the newest. */
    fun check(ctx: Context): String? {
        val j = JSONObject(open(RELEASE + "version.json").inputStream.use { it.readBytes().decodeToString() })
        val code = j.optJSONObject("apks")?.optLong(apkName(), 0) ?: 0L
        if (code <= installedCode(ctx)) return null
        return JSONObject().put("version", j.optString("version")).put("build", j.optInt("build")).toString()
    }

    /** Downloads the APK of this phone's kind, telling progress from 0 to 1. */
    fun download(ctx: Context, progress: (Double) -> Unit): File {
        val file = File(ctx.cacheDir, "update.apk")
        val c = open(RELEASE + apkName())
        val total = c.contentLengthLong
        var done = 0L
        var told = 0L
        c.inputStream.use { input ->
            file.outputStream().use { out ->
                val buf = ByteArray(64 shl 10)
                while (true) {
                    val n = input.read(buf)
                    if (n < 0) break
                    out.write(buf, 0, n)
                    done += n
                    if (total > 0 && done - told > total / 100) {
                        told = done
                        progress(done.toDouble() / total)
                    }
                }
            }
        }
        if (total > 0 && done != total) throw IOException("скачано $done из $total байт")
        return file
    }

    /**
     * Hands the APK to the system installer, which asks the user to confirm.
     * Returns false, after opening the setting, when the user has not yet let
     * TigerSB install apps.
     */
    fun install(ctx: Context, apk: File): Boolean {
        if (Build.VERSION.SDK_INT >= 26 && !ctx.packageManager.canRequestPackageInstalls()) {
            ctx.startActivity(
                Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${ctx.packageName}"))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
            return false
        }
        val installer = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(ctx.packageName)
        val id = installer.createSession(params)
        session = id
        installer.openSession(id).use { s ->
            s.openWrite("update.apk", 0, apk.length()).use { out ->
                apk.inputStream().use { it.copyTo(out) }
                s.fsync(out)
            }
            // The system fills the status in, so the intent must be mutable;
            // naming the package keeps it explicit.
            val status = Intent(ACTION_STATUS).setPackage(ctx.packageName)
            val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
            s.commit(PendingIntent.getBroadcast(ctx, id, status, flags).intentSender)
        }
        apk.delete()
        return true
    }

    /** A failed install in words, or null when the user only cancelled it. */
    fun failure(status: Int, message: String?): String? = when (status) {
        PackageInstaller.STATUS_FAILURE_ABORTED -> null
        PackageInstaller.STATUS_FAILURE_CONFLICT, PackageInstaller.STATUS_FAILURE_INCOMPATIBLE ->
            "Новая версия подписана другим ключом. Удалите TigerSB и установите её заново"
        PackageInstaller.STATUS_FAILURE_STORAGE -> "Не хватает места для обновления"
        else -> "Обновление не установилось: ${message ?: status}"
    }

    private fun open(url: String): HttpURLConnection {
        val c = URL(url).openConnection() as HttpURLConnection
        c.connectTimeout = 15_000
        c.readTimeout = 30_000
        c.setRequestProperty("User-Agent", "TigerSB-android")
        if (c.responseCode != 200) {
            c.disconnect()
            throw IOException("сервер обновлений ответил ${c.responseCode}")
        }
        return c
    }
}
