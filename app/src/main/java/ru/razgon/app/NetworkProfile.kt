package ru.razgon.app

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import java.security.MessageDigest

data class NetworkProfile(val key: String, val network: Network? = null) { val online get() = network != null }
fun networkProfile(context: Context): NetworkProfile {
    val manager = context.getSystemService(ConnectivityManager::class.java)
    fun usable(network: Network): Boolean = manager.getNetworkCapabilities(network)?.let {
        it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) && !it.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
    } == true
    val network = manager.activeNetwork?.takeIf(::usable) ?: manager.allNetworks.filter(::usable).sortedByDescending {
        manager.getNetworkCapabilities(it)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true
    }.firstOrNull() ?: return NetworkProfile("offline")
    val caps = manager.getNetworkCapabilities(network)
    val type = if (caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true) "wifi" else "cellular"
    val links = manager.getLinkProperties(network)
    val addresses = links?.dnsServers?.mapNotNull { it.hostAddress }?.sorted().orEmpty() +
        links?.routes?.mapNotNull { it.gateway?.hostAddress }?.sorted().orEmpty()
    val digest = MessageDigest.getInstance("SHA-256").digest(addresses.joinToString("|").toByteArray())
        .joinToString("") { "%02x".format(it) }
    return NetworkProfile("$type|$digest", network)
}
