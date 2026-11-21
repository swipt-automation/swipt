package io.nekohasekai.swipt.developer

import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import io.nekohasekai.swipt.app.SwiptApplication
import java.net.Inet4Address
import java.net.NetworkInterface
import java.net.SocketException

private const val TAG = "LocalAddresses"

@Composable
internal fun rememberLocalAddresses(): List<String> {
    var addresses by remember { mutableStateOf(emptyList<String>()) }
    DisposableEffect(Unit) {
        val connectivity = SwiptApplication.application.getSystemService(ConnectivityManager::class.java)
        val links = HashMap<Network, String>()
        val cellular = HashSet<Network>()
        fun refresh() {
            addresses = localAddresses(
                local = links.filterKeys { it !in cellular }.values.toSet(),
                cellular = links.filterKeys { it in cellular }.values.toSet(),
            )
        }
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) cellular += network else cellular -= network
                refresh()
            }

            override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
                links[network] = linkProperties.interfaceName.orEmpty()
                refresh()
            }

            override fun onLost(network: Network) {
                links -= network
                cellular -= network
                refresh()
            }
        }
        connectivity.registerNetworkCallback(NetworkRequest.Builder().clearCapabilities().build(), callback, Handler(Looper.getMainLooper()))
        refresh()
        onDispose { connectivity.unregisterNetworkCallback(callback) }
    }
    return addresses
}

private fun localAddresses(local: Set<String>, cellular: Set<String>): List<String> {
    val interfaces = try {
        NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
    } catch (e: SocketException) {
        Log.w(TAG, "read the network interfaces", e)
        return emptyList()
    }
    return interfaces
        .filter { it.name !in cellular && isLocal(it) }
        .sortedBy { it.name !in local }
        .flatMap { networkInterface -> networkInterface.inetAddresses.toList().filterIsInstance<Inet4Address>().mapNotNull { it.hostAddress } }
}

private fun isLocal(networkInterface: NetworkInterface): Boolean = try {
    networkInterface.isUp && !networkInterface.isLoopback && !networkInterface.isPointToPoint
} catch (e: SocketException) {
    false
}
