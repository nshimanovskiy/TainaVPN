package com.tainavpn.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import com.tainavpn.tainacore.Host
import com.tainavpn.tainacore.Tainacore
import java.io.File
import java.util.concurrent.Executors

/**
 * Runs sing-box (via the gomobile library) and provides it the TUN interface.
 * The addresses here must match the tun inbound generated in ui/app.js.
 */
class TainaVpnService : VpnService() {

    companion object {
        const val ACTION_START = "com.tainavpn.START"
        const val ACTION_STOP = "com.tainavpn.STOP"
        private const val CHANNEL = "vpn"
        private const val NOTIFY_ID = 1
        private const val TAG = "TainaVpn"

        fun configFile(ctx: Context) = File(ctx.filesDir, "config.json")

        fun start(ctx: Context) {
            val i = Intent(ctx, TainaVpnService::class.java).setAction(ACTION_START)
            if (Build.VERSION.SDK_INT >= 26) ctx.startForegroundService(i) else ctx.startService(i)
        }

        fun stop(ctx: Context) {
            ctx.startService(Intent(ctx, TainaVpnService::class.java).setAction(ACTION_STOP))
        }
    }

    private val worker = Executors.newSingleThreadExecutor()
    private var tun: ParcelFileDescriptor? = null

    private val host = object : Host {
        override fun openTun(): Int {
            tun?.close()
            val builder = Builder()
                .setSession("Tainavpn")
                .setMtu(9000)
                .addAddress("172.19.0.1", 30)
                .addRoute("0.0.0.0", 0)
                .addDnsServer("172.19.0.2")
            try {
                builder.addAddress("fdfe:dcba:9876::1", 126)
                builder.addRoute("::", 0)
            } catch (e: Exception) {
                Log.w(TAG, "IPv6 not available: $e")
            }
            // our own sockets (sing-box) must not loop back into the tunnel
            builder.addDisallowedApplication(packageName)
            if (Build.VERSION.SDK_INT >= 29) builder.setMetered(false)
            val pfd = builder.establish() ?: throw IllegalStateException("VPN permission was not granted")
            tun = pfd
            return pfd.fd
        }

        override fun protect(fd: Int): Boolean = this@TainaVpnService.protect(fd)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopVpn()
                return START_NOT_STICKY
            }
            else -> startVpn()
        }
        return START_NOT_STICKY
    }

    private fun startVpn() {
        showNotification()
        VpnState.state = "starting"
        VpnState.error = ""
        worker.execute {
            try {
                val config = configFile(this).readText()
                Tainacore.start(config, File(filesDir, "core").absolutePath, host)
                VpnState.state = "running"
                // debug builds: keep core logs in a file for CI
                if ((applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0) {
                    Thread {
                        while (VpnState.state == "running") {
                            try { File(filesDir, "core.log").writeText(Tainacore.logs()) } catch (_: Exception) {}
                            Thread.sleep(1000)
                        }
                    }.start()
                }
            } catch (e: Exception) {
                Log.e(TAG, "start failed", e)
                VpnState.error = e.message ?: e.toString()
                if (Lang.killSwitch(this)) {
                    blockTraffic()
                } else {
                    VpnState.state = "stopped"
                    closeTun()
                    stopForegroundCompat()
                    stopSelf()
                }
            }
        }
    }

    /**
     * Kill switch: the VPN couldn't start, so keep (or create) the tunnel without anything
     * reading from it — all traffic is captured and dropped instead of leaking past the proxy.
     */
    private fun blockTraffic() {
        try {
            try { Tainacore.stop() } catch (_: Exception) {}
            if (tun == null) host.openTun()
            VpnState.state = "blocked"
            showNotification(blocked = true)
        } catch (e: Exception) {
            Log.e(TAG, "kill switch failed", e)
            VpnState.state = "stopped"
            closeTun()
            stopForegroundCompat()
            stopSelf()
        }
    }

    private fun stopVpn() {
        VpnState.state = "stopping"
        worker.execute {
            try {
                Tainacore.stop()
            } catch (e: Exception) {
                Log.w(TAG, "stop failed", e)
            }
            closeTun()
            VpnState.state = "stopped"
            stopForegroundCompat()
            stopSelf()
        }
    }

    private fun closeTun() {
        try { tun?.close() } catch (_: Exception) {}
        tun = null
    }

    override fun onRevoke() {
        // another VPN app took over or the user revoked permission
        stopVpn()
    }

    override fun onDestroy() {
        if (VpnState.state != "stopped") {
            try { Tainacore.stop() } catch (_: Exception) {}
            closeTun()
            VpnState.state = "stopped"
        }
        super.onDestroy()
    }

    private fun stopForegroundCompat() {
        if (Build.VERSION.SDK_INT >= 24) stopForeground(Service.STOP_FOREGROUND_REMOVE)
        else @Suppress("DEPRECATION") stopForeground(true)
    }

    private fun showNotification(blocked: Boolean = false) {
        val nm = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL, "VPN", NotificationManager.IMPORTANCE_LOW)
            )
        }
        val piFlags = PendingIntent.FLAG_UPDATE_CURRENT or
            (if (Build.VERSION.SDK_INT >= 23) PendingIntent.FLAG_IMMUTABLE else 0)
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), piFlags)
        val stop = PendingIntent.getService(
            this, 1, Intent(this, TainaVpnService::class.java).setAction(ACTION_STOP), piFlags
        )
        val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, CHANNEL)
        else @Suppress("DEPRECATION") Notification.Builder(this)
        val n = builder
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle("Tainavpn")
            .setContentText(Lang.get(this, if (blocked) "notifBlocked" else "notifText"))
            .setContentIntent(open)
            .setOngoing(true)
            .addAction(Notification.Action.Builder(null, Lang.get(this, "disconnect"), stop).build())
            .build()
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIFY_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFY_ID, n)
        }
    }
}
