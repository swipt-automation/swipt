package io.nekohasekai.swipt.device

import android.content.Context
import android.content.ContextWrapper
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Handler
import android.os.Looper
import android.util.Log
import java.net.InetAddress

// NsdService of Android 17 connects only a caller whose package belongs to its UID, which no package does for root,
// and drops the host name of a caller without NETWORK_SETTINGS, which apps cannot hold and the shell does. It
// publishes only the addresses that come with a host name, not those of the device.
internal class Advertiser(
    shellContext: Context,
    private val name: String,
    private val type: String,
    private val port: Int,
    private val hostname: String,
    private val attributes: List<String>,
    private val onHostname: (String) -> Unit,
    private val onFailed: (String) -> Unit,
) {
    private val main = Handler(Looper.getMainLooper())
    private val nsd = shellContext.getSystemService(NsdManager::class.java)
    private val connectivity = shellContext.getSystemService(ConnectivityManager::class.java)
    private val setHostname = SET_HOSTNAME.takeIf { hostname.isNotEmpty() }
    private val links = HashMap<Network, LinkProperties>()
    private val remote = HashSet<Network>()
    private val refreshing = Runnable { refresh() }
    private var addresses: Set<InetAddress>? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var registration: NsdManager.RegistrationListener? = null
    private var stopped = false

    fun start() {
        main.post {
            if (stopped) return@post
            try {
                if (setHostname == null) register() else watchNetworks()
            } catch (e: Exception) {
                fail(Failure.of("advertise", e).message)
            }
        }
    }

    fun stop() {
        main.post {
            if (!stopped) release()
        }
    }

    private fun watchNetworks() {
        for (network in connectivity.allNetworks) {
            val capabilities = connectivity.getNetworkCapabilities(network) ?: continue
            links[network] = connectivity.getLinkProperties(network) ?: continue
            updateCapabilities(network, capabilities)
        }
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                updateCapabilities(network, capabilities)
                settle()
            }

            override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) {
                links[network] = properties
                settle()
            }

            override fun onLost(network: Network) {
                links.remove(network)
                remote.remove(network)
                settle()
            }
        }
        connectivity.registerNetworkCallback(NetworkRequest.Builder().clearCapabilities().build(), callback, main)
        networkCallback = callback
        if (localAddresses().isNotEmpty()) refresh() else settle()
    }

    private fun updateCapabilities(network: Network, capabilities: NetworkCapabilities) {
        if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) || capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) {
            remote.add(network)
        } else {
            remote.remove(network)
        }
    }

    private fun fail(message: String) {
        release()
        onFailed(message)
    }

    private fun release() {
        stopped = true
        main.removeCallbacks(refreshing)
        networkCallback?.let {
            try {
                connectivity.unregisterNetworkCallback(it)
            } catch (e: RuntimeException) {
                Log.w(TAG, "unregister the network callback", e)
            }
        }
        networkCallback = null
        registration?.let {
            try {
                nsd.unregisterService(it)
            } catch (e: RuntimeException) {
                Log.w(TAG, "unregister the service", e)
            }
        }
        registration = null
    }

    private fun settle() {
        if (stopped) return
        main.removeCallbacks(refreshing)
        main.postDelayed(refreshing, SETTLE_MS)
    }

    private fun refresh() {
        if (stopped) return
        val current = localAddresses()
        if (current == addresses) return
        addresses = current
        try {
            register()
        } catch (e: Exception) {
            fail(Failure.of("advertise", e).message)
        }
    }

    private fun localAddresses(): Set<InetAddress> = links.filterKeys { it !in remote }.values.flatMapTo(HashSet()) { properties -> properties.linkAddresses.map { it.address } }

    private fun register() {
        registration?.let { nsd.unregisterService(it) }
        registration = null
        val info = NsdServiceInfo()
        info.serviceName = name
        info.serviceType = type
        info.port = port
        for (attribute in attributes) {
            info.setAttribute(attribute.substringBefore('='), attribute.substringAfter('=', ""))
        }
        setHostname?.let {
            it.invoke(info, hostname)
            info.hostAddresses = addresses.orEmpty().toList()
        }
        val current = object : NsdManager.RegistrationListener {
            override fun onRegistrationFailed(info: NsdServiceInfo, code: Int) {
                if (!stopped && registration === this) {
                    registration = null
                    fail("NsdManager error $code")
                }
            }

            override fun onUnregistrationFailed(info: NsdServiceInfo, code: Int) {
                Log.w(TAG, "unregister the service: NsdManager error $code")
            }

            override fun onServiceRegistered(info: NsdServiceInfo) {
                if (stopped || registration !== this || setHostname == null) return
                val registered = try {
                    GET_HOSTNAME?.invoke(info) as String?
                } catch (_: ReflectiveOperationException) {
                    null
                }
                onHostname(registered?.takeIf { it.isNotEmpty() } ?: hostname)
            }

            override fun onServiceUnregistered(info: NsdServiceInfo) {}
        }
        registration = current
        nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, { main.post(it) }, current)
    }

    companion object {
        private const val SETTLE_MS = 200L
        private val SET_HOSTNAME = runCatching { NsdServiceInfo::class.java.getMethod("setHostname", String::class.java) }.getOrNull()
        private val GET_HOSTNAME = runCatching { NsdServiceInfo::class.java.getMethod("getHostname") }.getOrNull()

        val supportsHostname get() = SET_HOSTNAME != null

        fun shellContext(base: Context): Context {
            val packageContext = base.createPackageContext(Clipboard.SHELL_PACKAGE, 0)
            val shell = object : ContextWrapper(packageContext) {
                override fun getPackageName() = Clipboard.SHELL_PACKAGE

                override fun getOpPackageName() = Clipboard.SHELL_PACKAGE
            }
            // ContextImpl creates the managers of the system services on its outer context, whose package they name to
            // the services.
            packageContext.javaClass.getDeclaredMethod("setOuterContext", Context::class.java)
                .apply { isAccessible = true }
                .invoke(packageContext, shell)
            return shell
        }
    }
}
