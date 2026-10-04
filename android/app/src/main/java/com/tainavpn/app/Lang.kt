package com.tainavpn.app

import android.content.Context

/** Native strings in the language chosen in the app UI (saved by the JS bridge). */
object Lang {
    private val text = mapOf(
        "ru" to mapOf(
            "notifText" to "Трафик идёт через прокси",
            "disconnect" to "Отключить",
            "vpnDenied" to "Разрешение на VPN не выдано",
        ),
        "en" to mapOf(
            "notifText" to "Traffic goes through the proxy",
            "disconnect" to "Disconnect",
            "vpnDenied" to "VPN permission was not granted",
        ),
    )

    fun set(ctx: Context, lang: String) {
        ctx.getSharedPreferences("tainavpn", Context.MODE_PRIVATE).edit().putString("lang", lang).apply()
    }

    fun get(ctx: Context, key: String): String {
        val lang = ctx.getSharedPreferences("tainavpn", Context.MODE_PRIVATE).getString("lang", null)
            ?: if (java.util.Locale.getDefault().language == "ru") "ru" else "en"
        return text[lang]?.get(key) ?: text["en"]!![key] ?: key
    }
}
