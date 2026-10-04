package com.tainavpn.app

import android.content.ContentProvider
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.database.Cursor
import android.net.Uri
import android.os.ParcelFileDescriptor
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/** Downloads a new APK, checks its SHA-256 and opens the system installer. */
object Updater {
    @Volatile private var state = "idle" // idle | downloading | installing | error
    @Volatile private var progress = 0.0
    @Volatile private var error = ""

    fun status(): String = JSONObject().put("state", state).put("progress", progress).put("error", error).toString()

    fun apkFile(ctx: Context) = File(File(ctx.cacheDir, "update").apply { mkdirs() }, "tainavpn.apk")

    fun start(ctx: Context, url: String, sha256: String) {
        if (!url.startsWith("https://") || state == "downloading") return
        state = "downloading"; progress = 0.0; error = ""
        val app = ctx.applicationContext
        Thread {
            try {
                val file = apkFile(app)
                val digest = MessageDigest.getInstance("SHA-256")
                var conn = URL(url).openConnection() as HttpURLConnection
                conn.instanceFollowRedirects = true
                conn.connectTimeout = 20000
                conn.readTimeout = 30000
                conn.setRequestProperty("User-Agent", "Tainavpn-Android")
                // GitHub redirects to another host; follow manually across protocols just in case
                var redirects = 0
                while (conn.responseCode in 300..399 && redirects++ < 5) {
                    val next = conn.getHeaderField("Location")
                    conn.disconnect()
                    conn = URL(URL(url), next).openConnection() as HttpURLConnection
                }
                if (conn.responseCode != 200) throw Exception("HTTP ${conn.responseCode}")
                val total = conn.contentLengthLong
                var done = 0L
                conn.inputStream.use { input ->
                    file.outputStream().use { out ->
                        val buf = ByteArray(64 * 1024)
                        while (true) {
                            val n = input.read(buf)
                            if (n < 0) break
                            out.write(buf, 0, n)
                            digest.update(buf, 0, n)
                            done += n
                            if (total > 0) progress = done.toDouble() / total
                        }
                    }
                }
                val want = sha256.removePrefix("sha256:").lowercase()
                val got = digest.digest().joinToString("") { "%02x".format(it) }
                if (want.isNotEmpty() && want != got) throw Exception("checksum mismatch")
                state = "installing"; progress = 1.0
                val uri = Uri.parse("content://${app.packageName}.apk/tainavpn.apk")
                val intent = Intent(Intent.ACTION_VIEW)
                    .setDataAndType(uri, "application/vnd.android.package-archive")
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
                app.startActivity(intent)
                state = "idle"
            } catch (e: Exception) {
                error = e.message ?: e.toString()
                state = "error"
            }
        }.start()
    }
}

/** Serves the downloaded APK to the package installer (read-only, granted per intent). */
class ApkProvider : ContentProvider() {
    override fun onCreate() = true
    override fun getType(uri: Uri) = "application/vnd.android.package-archive"
    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        val file = Updater.apkFile(context!!)
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
    }
    override fun query(uri: Uri, p: Array<out String>?, s: String?, a: Array<out String>?, o: String?): Cursor? {
        val file = Updater.apkFile(context!!)
        val c = android.database.MatrixCursor(arrayOf(android.provider.OpenableColumns.DISPLAY_NAME, android.provider.OpenableColumns.SIZE))
        c.addRow(arrayOf<Any>("tainavpn.apk", file.length()))
        return c
    }
    override fun insert(uri: Uri, values: ContentValues?): Uri? = null
    override fun delete(uri: Uri, s: String?, a: Array<out String>?) = 0
    override fun update(uri: Uri, v: ContentValues?, s: String?, a: Array<out String>?) = 0
}
