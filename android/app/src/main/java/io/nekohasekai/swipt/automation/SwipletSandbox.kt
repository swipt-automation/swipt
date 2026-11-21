package io.nekohasekai.swipt.automation

import android.app.Application
import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri
import android.os.ParcelFileDescriptor
import com.facebook.fbreact.specs.NativeImageStoreAndroidSpec
import com.facebook.react.BaseReactPackage
import com.facebook.react.PackageList
import com.facebook.react.ReactPackage
import com.facebook.react.bridge.Callback
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.module.model.ReactModuleInfo
import com.facebook.react.module.model.ReactModuleInfoProvider
import expo.modules.ExpoModulesPackage
import expo.modules.adapters.react.ModuleRegistryAdapter
import expo.modules.adapters.react.ReactModuleRegistryProvider
import expo.modules.kotlin.ExpoModulesHelper
import expo.modules.kotlin.ModulesProvider
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.readSwipletFileRequest
import java.io.File
import java.io.FileNotFoundException
import java.net.URI
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

object SwipletSandbox {
    internal data class SwipletFile(val swipletId: String, val root: String, val path: String)

    private val fileScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    fun packages(application: Application): List<ReactPackage> =
        PackageList(application).packages.map { if (it is ExpoModulesPackage) expoModules() else it } + SandboxPackage()

    internal fun resolveFile(rawPath: String?): SwipletFile {
        if (rawPath.isNullOrEmpty() || '\u0000' in rawPath) {
            throw SecurityException("invalid swiplet file path")
        }
        val normalized = File(rawPath).toPath().normalize().toString()
        for (entry in SwipletHost.interfaces.values) {
            val source = entry.source
            val roots = arrayOf(
                source.dataDirectory to "data",
                source.assetsDirectory to "assets",
                source.cacheDirectory to "cache",
            )
            for ((directory, kind) in roots) {
                if (directory.isEmpty()) continue
                val base = File(directory).toPath().normalize().toString().trimEnd('/')
                if (normalized.startsWith("$base/")) {
                    val relative = normalized.substring(base.length + 1)
                    if (relative.isNotEmpty()) {
                        return SwipletFile(entry.id, kind, relative)
                    }
                }
            }
        }
        throw SecurityException("swiplets can only read files in their own directories")
    }

    internal fun openPipe(target: SwipletFile): ParcelFileDescriptor {
        val (readEnd, writeEnd) = ParcelFileDescriptor.createReliablePipe()
        fileScope.launch {
            try {
                ParcelFileDescriptor.AutoCloseOutputStream(writeEnd).use { output ->
                    Core.swiplets.readSwipletFile(readSwipletFileRequest {
                        swipletId = target.swipletId
                        root = target.root
                        path = target.path
                    }).collect { part ->
                        part.chunk.writeTo(output)
                    }
                }
            } catch (e: Throwable) {
                runCatching { writeEnd.closeWithError(e.message ?: e.toString()) }
            }
        }
        return readEnd
    }

    private fun expoModules(): ReactPackage {
        val modules = checkNotNull(ExpoModulesHelper.modulesProvider)
        val sandboxed = object : ModulesProvider {
            override fun getModulesMap() = modules.getModulesMap().mapKeys { (module, _) ->
                if (module.name == "expo.modules.asset.AssetModule") SwipletAssetModule::class.java else module
            }

            override fun getServices() = modules.getServices()
        }
        return ModuleRegistryAdapter(ReactModuleRegistryProvider(ExpoModulesPackage.packageList, null), sandboxed)
    }

    private class SandboxPackage : BaseReactPackage() {
        override fun getModule(name: String, reactContext: ReactApplicationContext): NativeModule? =
            if (name == NativeImageStoreAndroidSpec.NAME) SwipletImageStore(reactContext) else null

        override fun getReactModuleInfoProvider() = ReactModuleInfoProvider {
            mapOf(
                NativeImageStoreAndroidSpec.NAME to ReactModuleInfo(
                    NativeImageStoreAndroidSpec.NAME,
                    SwipletImageStore::class.java.name,
                    canOverrideExistingModule = true,
                    needsEagerInit = false,
                    isCxxModule = false,
                    isTurboModule = true,
                ),
            )
        }
    }
}

// expo-asset saves a download under a name and an extension that the caller picks, without confining the path to the
// cache directory (expo-asset 58.0.19, android/src/main/java/expo/modules/asset/AssetModule.kt:85-101).
class SwipletAssetModule : Module() {
    override fun definition() = ModuleDefinition {
        Name("ExpoAsset")

        AsyncFunction("downloadAsync") { uri: URI, _: String?, _: String ->
            if (uri.scheme != "file") throw CodedException("ERR_ASSET_DOWNLOAD", "swiplets load assets from file URLs", null)
            try {
                SwipletSandbox.resolveFile(uri.path)
            } catch (e: SecurityException) {
                throw CodedException("ERR_ASSET_DOWNLOAD", e.message, e)
            }
            uri.normalize().toString()
        }
    }
}

// React Native's image store reads any URI that the app can open (react-native 0.88.1,
// ReactAndroid/src/main/java/com/facebook/react/modules/camera/ImageStoreManager.kt:35-55).
private class SwipletImageStore(context: ReactApplicationContext) : NativeImageStoreAndroidSpec(context) {
    override fun getBase64ForTag(uri: String, success: Callback, error: Callback) {
        error.invoke("swiplets read files with @swipt/api/file-system")
    }
}

class SwipletFileProvider : ContentProvider() {
    override fun onCreate(): Boolean = true

    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        if (mode != "r") throw SecurityException("swiplet files are read-only here")
        val target = try {
            SwipletSandbox.resolveFile(uri.getQueryParameter("path"))
        } catch (e: SecurityException) {
            throw FileNotFoundException(e.message)
        }
        return SwipletSandbox.openPipe(target)
    }

    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?): Cursor? = null

    override fun getType(uri: Uri): String? = null

    override fun insert(uri: Uri, values: ContentValues?): Uri? = null

    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = 0

    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?): Int = 0
}
