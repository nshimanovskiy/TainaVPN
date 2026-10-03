package com.tainavpn.app

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import com.tainavpn.tainacore.Tainacore
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : Activity() {

    private lateinit var web: WebView
    private val vpnRequest = 1001

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        web = WebView(this)
        web.setBackgroundColor(0xFF0E1014.toInt())
        web.settings.javaScriptEnabled = true
        web.settings.domStorageEnabled = true
        web.webViewClient = WebViewClient()
        web.webChromeClient = WebChromeClient()
        web.addJavascriptInterface(Bridge(), "TainaAndroid")
        setContentView(web)
        web.loadUrl("file:///android_asset/index.html")

        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == vpnRequest) {
            if (resultCode == RESULT_OK) {
                TainaVpnService.start(this)
            } else {
                VpnState.state = "stopped"
                VpnState.error = "Разрешение на VPN не выдано"
            }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        // close an open sheet first, otherwise go to background (keep the VPN running)
        web.evaluateJavascript(
            "(function(){var s=[...document.querySelectorAll('.sheet')].filter(x=>!x.hidden);s.forEach(x=>x.hidden=true);return s.length})()"
        ) { r -> if (r == "0") moveTaskToBack(true) }
    }

    /** Methods called from ui/app.js (window.TainaAndroid). Runs on a background thread. */
    inner class Bridge {
        private val storeFile get() = File(filesDir, "store.json")

        @JavascriptInterface
        fun loadStore(): String = if (storeFile.exists()) storeFile.readText() else ""

        @JavascriptInterface
        fun saveStore(data: String) {
            val tmp = File(filesDir, "store.json.tmp")
            tmp.writeText(data)
            tmp.renameTo(storeFile)
        }

        @JavascriptInterface
        fun start(config: String): String {
            return try {
                TainaVpnService.configFile(this@MainActivity).writeText(config)
                VpnState.state = "starting"
                VpnState.error = ""
                val prepare = VpnService.prepare(this@MainActivity)
                runOnUiThread {
                    if (prepare != null) startActivityForResult(prepare, vpnRequest)
                    else TainaVpnService.start(this@MainActivity)
                }
                ""
            } catch (e: Exception) {
                VpnState.state = "stopped"
                e.message ?: e.toString()
            }
        }

        @JavascriptInterface
        fun stop() {
            TainaVpnService.stop(this@MainActivity)
        }

        @JavascriptInterface
        fun status(): String = VpnState.json()

        @JavascriptInterface
        fun logs(): String = try { Tainacore.logs() } catch (e: Throwable) { e.toString() }

        @JavascriptInterface
        fun deviceName(): String = "${Build.MANUFACTURER} ${Build.MODEL}"

        @JavascriptInterface
        fun version(): String {
            val v = packageManager.getPackageInfo(packageName, 0).versionName
            val core = try { Tainacore.coreVersion() } catch (e: Throwable) { "?" }
            return "Tainavpn $v · sing-box $core"
        }

        @JavascriptInterface
        fun http(method: String, url: String, body: String): String {
            val out = JSONObject()
            try {
                val conn = URL(url).openConnection() as HttpURLConnection
                conn.requestMethod = method
                conn.connectTimeout = 15000
                conn.readTimeout = 20000
                conn.setRequestProperty("Content-Type", "application/json")
                conn.setRequestProperty("User-Agent", "Tainavpn-Android")
                if (body.isNotEmpty()) {
                    conn.doOutput = true
                    conn.outputStream.use { it.write(body.toByteArray()) }
                }
                val code = conn.responseCode
                val stream = if (code >= 400) conn.errorStream else conn.inputStream
                val text = stream?.bufferedReader()?.use { it.readText() } ?: ""
                out.put("status", code).put("body", text)
                conn.disconnect()
            } catch (e: Exception) {
                out.put("error", e.message ?: e.toString())
            }
            return out.toString()
        }
    }
}
