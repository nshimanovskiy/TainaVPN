package com.tainavpn.app

import org.json.JSONObject

/** Connection state shared between the service and the UI. */
object VpnState {
    @Volatile var state: String = "stopped" // stopped | starting | running | stopping
    @Volatile var error: String = ""

    fun json(): String = JSONObject().put("state", state).put("error", error).toString()
}
