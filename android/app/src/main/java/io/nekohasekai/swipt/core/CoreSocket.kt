package io.nekohasekai.swipt.core

import android.os.ParcelFileDescriptor
import android.system.ErrnoException
import android.system.Os
import android.system.OsConstants
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketAddress
import javax.net.SocketFactory

internal class CoreSocketFactory(private val connect: () -> ParcelFileDescriptor) : SocketFactory() {
    override fun createSocket(): Socket = CoreSocket(connect())

    override fun createSocket(host: String?, port: Int): Socket = createSocket()

    override fun createSocket(host: String?, port: Int, localHost: InetAddress?, localPort: Int): Socket = createSocket()

    override fun createSocket(host: InetAddress?, port: Int): Socket = createSocket()

    override fun createSocket(address: InetAddress?, port: Int, localAddress: InetAddress?, localPort: Int): Socket = createSocket()
}

private class CoreSocket(private val descriptor: ParcelFileDescriptor) : Socket() {
    private val input = FileInputStream(descriptor.fileDescriptor)
    private val output = FileOutputStream(descriptor.fileDescriptor)
    private val address = InetSocketAddress(InetAddress.getLoopbackAddress(), 0)

    @Volatile
    private var closed = false

    @Volatile
    private var inputShutdown = false

    @Volatile
    private var outputShutdown = false

    override fun connect(endpoint: SocketAddress?) {}

    override fun connect(endpoint: SocketAddress?, timeout: Int) {}

    override fun getInputStream(): InputStream = input

    override fun getOutputStream(): OutputStream = output

    override fun isConnected(): Boolean = true

    override fun isBound(): Boolean = true

    override fun isClosed(): Boolean = closed

    override fun isInputShutdown(): Boolean = inputShutdown

    override fun isOutputShutdown(): Boolean = outputShutdown

    override fun getRemoteSocketAddress(): SocketAddress = address

    override fun getLocalSocketAddress(): SocketAddress = address

    override fun getInetAddress(): InetAddress = address.address

    override fun getLocalAddress(): InetAddress = address.address

    override fun setTcpNoDelay(on: Boolean) {}

    override fun setSoTimeout(timeout: Int) {}

    override fun setKeepAlive(on: Boolean) {}

    override fun shutdownInput() {
        inputShutdown = true
        shutdown(OsConstants.SHUT_RD)
    }

    override fun shutdownOutput() {
        outputShutdown = true
        shutdown(OsConstants.SHUT_WR)
    }

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        shutdown(OsConstants.SHUT_RDWR)
        descriptor.close()
    }

    private fun shutdown(how: Int) {
        try {
            Os.shutdown(descriptor.fileDescriptor, how)
        } catch (e: ErrnoException) {
            if (e.errno != OsConstants.ENOTCONN) throw IOException(e)
        }
    }
}
