package ru.razgon.app

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.telephony.TelephonyManager
import java.security.MessageDigest

data class NetworkProfile(val key: String, val label: String, val online: Boolean)

fun networkProfile(context: Context): NetworkProfile {
    val manager = context.getSystemService(ConnectivityManager::class.java)
    val network = manager.activeNetwork ?: return NetworkProfile("offline", "Нет подключения", false)
    val caps = manager.getNetworkCapabilities(network)
    val type = when {
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true -> "wifi"
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true -> "cellular"
        else -> "other"
    }
    val operator = runCatching { context.getSystemService(TelephonyManager::class.java).networkOperator }.getOrDefault("")
    val links = manager.getLinkProperties(network)
    // Адреса используются только для отпечатка; запросов к этим DNS нет.
    val addresses = links?.dnsServers?.mapNotNull { it.hostAddress }?.sorted().orEmpty() +
        links?.routes?.mapNotNull { it.gateway?.hostAddress }?.sorted().orEmpty()
    val digest = MessageDigest.getInstance("SHA-256").digest(addresses.joinToString("|").toByteArray())
        .joinToString("") { "%02x".format(it) }
    return NetworkProfile("$type|$operator|$digest", if (type == "wifi") "Wi-Fi" else if (type == "cellular") "Мобильная сеть" else "Сеть", caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true)
}
