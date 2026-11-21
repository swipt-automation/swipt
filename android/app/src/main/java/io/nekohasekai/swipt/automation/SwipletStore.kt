package io.nekohasekai.swipt.automation

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.icu.text.Collator
import android.net.Uri
import android.os.LocaleList
import android.util.Log
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.google.protobuf.ByteString
import io.nekohasekai.swipt.BuildConfig
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert
import io.nekohasekai.swipt.app.LocalizedException
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.app.description
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.core.ProtoJson
import io.nekohasekai.swipt.model.AppDatabase
import io.nekohasekai.swipt.model.AppDirectory
import io.nekohasekai.swipt.proto.Incompatibility
import io.nekohasekai.swipt.proto.InspectSwipletResponse
import io.nekohasekai.swipt.proto.ServiceStatus
import io.nekohasekai.swipt.proto.Swiplet as CoreSwiplet
import io.nekohasekai.swipt.proto.SwipletIcon as CoreSwipletIcon
import io.nekohasekai.swipt.proto.checkCompatibilityRequest
import io.nekohasekai.swipt.proto.clearSwipletDataRequest
import io.nekohasekai.swipt.proto.deploySwipletRequest
import io.nekohasekai.swipt.proto.downloadSwipletRequest
import io.nekohasekai.swipt.proto.exportSwipletRequest
import io.nekohasekai.swipt.proto.fetchRepositoryRequest
import io.nekohasekai.swipt.proto.getSwipletDataSizeRequest
import io.nekohasekai.swipt.proto.getSwipletIconRequest
import io.nekohasekai.swipt.proto.removeSwipletRequest
import io.nekohasekai.swipt.proto.requirement
import java.io.File
import java.io.FileNotFoundException
import java.io.InputStream
import java.io.OutputStream
import java.util.Locale
import java.util.UUID
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject

class Swiplet(
    val id: String,
    val title: String,
    val version: String,
    val versionCode: Long,
    val icon: SwipletIcon,
    val actions: List<SwipletAction>,
    val hasService: Boolean,
    val needsUpdate: Boolean,
    val hasInterface: Boolean,
    val installed: Long,
    val repository: String,
) {
    val streams: List<String> = actions.filter(SwipletAction::stream).map(SwipletAction::name)
}

class SwipletAction(val name: String, val title: String, val description: String, val stream: Boolean, val input: JsonObject?)

class SwipletPreview(val file: File, val id: String, val title: String, val version: String, val icon: SwipletIcon)

class SwipletUpdate(val repository: Repository, val swiplet: RepositorySwiplet)

class DownloadProgress(val received: Long, val total: Long)

class Repository(
    val url: String,
    val name: String,
    val localizedNames: Map<String, String>,
    val icon: SwipletIcon?,
    val image: ByteArray?,
    val etag: String,
    val swiplets: List<RepositorySwiplet>,
) {
    val isOfficial: Boolean get() = isOfficialURL(url)

    val title: String get() = SwipletStore.preferredName(name, localizedNames)

    val shownIcon: SwipletIcon by lazy {
        ((icon ?: if (isOfficial) official.icon else null) ?: SwipletIcon.default).copy(
            image = image?.let { BitmapFactory.decodeByteArray(it, 0, it.size) },
        )
    }

    companion object {
        private const val STABLE = "https://github.com/swipt-automation/official-swiplets/releases/latest/download/swiplets.json"
        private const val NIGHTLY = "https://github.com/swipt-automation/official-swiplets/releases/download/nightly/swiplets.json"

        val official: Repository
            get() = Repository(
                if (BuildConfig.CHANNEL == "stable") STABLE else NIGHTLY,
                SwiptApplication.application.getString(R.string.swiplets_official),
                emptyMap(),
                SwipletIcon(null, "verified", null),
                null,
                "",
                emptyList(),
            )

        fun isOfficialURL(url: String): Boolean = url == STABLE || url == NIGHTLY
    }
}

class RepositorySwiplet(
    val id: String,
    val name: String,
    val localizedNames: Map<String, String>,
    val description: String,
    val version: String,
    val versionCode: Long,
    val minimumAPILevel: Long,
    val minimumOSVersion: String,
    val icon: SwipletIcon,
    val url: String,
    val checksum: String,
) {
    val title: String get() = SwipletStore.preferredName(name, localizedNames).ifEmpty { id }
}

class SwiptOutdatedException : LocalizedException(SwiptApplication.application.getString(R.string.swiplets_requires_newer_swipt))

object SwipletStore {
    private const val TAG = "SwipletStore"
    private const val CHUNK = 256 * 1024
    const val UPDATE_INTERVAL = 60 * 60 * 1000L

    private val scope = MainScope()
    private val storage = Dispatchers.IO.limitedParallelism(1)
    private val database: SwipletDatabase get() = AppDatabase.shared.swiplets()
    private val repositoriesChecked = HashMap<String, Long>()
    private val images = HashMap<Pair<String, Long>, Bitmap?>()

    var swiplets by mutableStateOf(emptyList<Swiplet>())
        private set

    var isLoaded by mutableStateOf(false)
        private set

    var loadFailure by mutableStateOf<String?>(null)
        private set

    var repositories by mutableStateOf(listOf(Repository.official))
        private set

    var updating by mutableStateOf(emptySet<String>())
        private set

    var downloads by mutableStateOf(emptyMap<String, DownloadProgress>())
        private set

    private var compatible by mutableStateOf(emptySet<Pair<Long, String>>())
    private val links = MutableStateFlow(0)

    private val restored = scope.launch {
        val stored = withContext(storage) {
            try {
                database.loadRepositories().map { (record, swiplets) -> repository(record, swiplets) }
            } catch (e: Exception) {
                Log.e(TAG, "load the repositories", e)
                emptyList()
            }
        }
        for (repository in stored.filter { it.isOfficial && it.url != Repository.official.url }) {
            removeRepository(repository.url)
        }
        repositories = listOf(stored.firstOrNull { it.url == Repository.official.url } ?: Repository.official) + stored.filterNot { it.isOfficial }
    }

    val updates: List<SwipletUpdate> by derivedStateOf { swiplets.mapNotNull(::findUpdate) }

    operator fun get(id: String): Swiplet? = swiplets.firstOrNull { it.id == id }

    fun repository(url: String): Repository? = repositories.firstOrNull { it.url == url }

    fun start() {
        scope.launch {
            combine(Core.installed, links) { installed, _ -> installed }.collect { installed ->
                if (installed == null) return@collect
                loadFailure = null
                val loaded = try {
                    withContext(storage) { load(installed) }
                } catch (e: Exception) {
                    loadFailure = e.description
                    ErrorAlert.report(SwiptApplication.application.getString(R.string.swiplets_couldnt_load), e)
                    return@collect
                }
                swiplets = loaded
                isLoaded = true
            }
        }
        scope.launch {
            val reported = HashSet<String>()
            Core.services.collect { services ->
                for ((id, service) in services) {
                    if (!service.enabled || service.status == ServiceStatus.SERVICE_STATUS_RUNNING) {
                        reported -= id
                    } else if (service.status == ServiceStatus.SERVICE_STATUS_FAILED && reported.add(id)) {
                        val title = SwiptApplication.application.getString(R.string.swiplets_service_stopped, this@SwipletStore[id]?.title ?: id)
                        ErrorAlert.report(AlertState.failure(title, service.error.orEmpty()))
                    }
                }
            }
        }
    }

    fun reload() {
        links.update { it + 1 }
    }

    private suspend fun load(installed: List<CoreSwiplet>): List<Swiplet> {
        val stored = database.swiplets()
        val repositories = stored.associate { it.id to it.repository }
        val byID = installed.associate { it.id to swiplet(it, repositories[it.id].orEmpty()) }
        val storedIDs = stored.map(SwipletRecord::id)
        val collator = Collator.getInstance()
        val added = byID.values.filter { it.id !in storedIDs }.sortedWith { left, right -> collator.compare(left.title, right.title) }
        val order = storedIDs.filter(byID::containsKey) + added.map(Swiplet::id)
        if (order != storedIDs) database.setSwipletIDs(order)
        return order.map(byID::getValue)
    }

    private suspend fun swiplet(info: CoreSwiplet, repository: String): Swiplet {
        val installed = info.installed.seconds
        val image = if (!info.icon.hasImage) {
            null
        } else {
            images.getOrPut(info.id to installed) {
                try {
                    val data = Core.swiplets.getSwipletIcon(getSwipletIconRequest { swipletId = info.id }).image.toByteArray()
                    BitmapFactory.decodeByteArray(data, 0, data.size)
                } catch (e: Exception) {
                    Log.w(TAG, "read the icon of ${info.id}", e)
                    null
                }
            }
        }
        return swipletOf(info, image, repository)
    }

    private fun swipletOf(info: CoreSwiplet, image: Bitmap?, repository: String) = Swiplet(
        id = info.id,
        title = preferredName(info.title, info.localizedTitlesMap).ifEmpty { info.id },
        version = info.version,
        versionCode = info.versionCode,
        icon = iconOf(info.icon).copy(image = image),
        actions = info.actionsList.map { SwipletAction(it.name, it.title, it.description, it.stream, if (it.hasInputSchema()) ProtoJson.element(it.inputSchema) else null) },
        hasService = info.hasService && !info.needsUpdate,
        needsUpdate = info.needsUpdate,
        hasInterface = info.hasInterface,
        installed = info.installed.seconds * 1000 + info.installed.nanos / 1_000_000,
        repository = repository,
    )

    private fun iconOf(icon: CoreSwipletIcon) = SwipletIcon(icon.color.ifEmpty { null }, icon.symbol, icon.symbolColor.ifEmpty { null })

    fun reorder(ids: List<String>) {
        val byID = swiplets.associateBy(Swiplet::id)
        swiplets = ids.mapNotNull(byID::get)
        scope.launch(storage) {
            try {
                database.setSwipletIDs(ids)
            } catch (e: Exception) {
                Log.e(TAG, "save the swiplet order", e)
            }
        }
    }

    suspend fun open(uri: Uri): SwipletPreview {
        val file = temporaryFile()
        try {
            withContext(Dispatchers.IO) {
                val input = SwiptApplication.application.contentResolver.openInputStream(uri) ?: throw FileNotFoundException(uri.toString())
                input.use { stream -> file.outputStream().use(stream::copyTo) }
            }
            return preview(file)
        } catch (e: Exception) {
            discard(file)
            throw e
        }
    }

    suspend fun open(link: String): SwipletPreview {
        val file = temporaryFile()
        try {
            download(link.trim(), file, "", null)
            return preview(file)
        } catch (e: Exception) {
            discard(file)
            throw e
        }
    }

    suspend fun importSwiplet(preview: SwipletPreview) {
        val id = install(preview.file)
        withContext(storage) { database.setRepository("", id) }
        links.update { it + 1 }
        discard(preview.file)
    }

    fun discard(preview: SwipletPreview) {
        scope.launch { discard(preview.file) }
    }

    fun update(id: String): SwipletUpdate? = updates.firstOrNull { it.swiplet.id == id }

    private fun findUpdate(swiplet: Swiplet): SwipletUpdate? {
        val repository = repository(swiplet.repository) ?: return null
        val available = repository.swiplets.firstOrNull { it.id == swiplet.id } ?: return null
        val isNewer = if (available.versionCode > 0) {
            available.versionCode > swiplet.versionCode
        } else {
            available.version.isNotEmpty() && available.version != swiplet.version
        }
        if (!isNewer || (available.minimumAPILevel to available.minimumOSVersion) !in compatible) return null
        return SwipletUpdate(repository, available)
    }

    private fun checkCompatibility() {
        scope.launch {
            val requirements = repositories.flatMap { it.swiplets }.map { it.minimumAPILevel to it.minimumOSVersion }.distinct()
            try {
                val checked = Core.swiplets.checkCompatibility(checkCompatibilityRequest {
                    this.requirements += requirements.map { (level, version) ->
                        requirement {
                            minimumApiLevel = level
                            minimumOsVersion = version
                        }
                    }
                }).incompatibilitiesList
                compatible = requirements.filterIndexed { index, _ -> checked.getOrNull(index) == Incompatibility.INCOMPATIBILITY_UNSPECIFIED }.toSet()
            } catch (e: Exception) {
                Log.w(TAG, "check the compatibility of the repositories", e)
            }
        }
    }

    suspend fun addRepository(link: String) {
        restored.join()
        val url = link.trim()
        if (repositories.any { it.url == url } || Repository.isOfficialURL(url)) {
            throw LocalizedException(SwiptApplication.application.getString(R.string.swiplets_repository_already_added))
        }
        val repository = fetchRepository(url, "") ?: return
        save(repository)
        repositories = repositories + repository
        repositoriesChecked[url] = System.currentTimeMillis()
        checkCompatibility()
    }

    fun removeRepository(url: String) {
        repositories = repositories.filterNot { it.url == url }
        repositoriesChecked.remove(url)
        scope.launch(storage) {
            try {
                database.removeRepository(url)
            } catch (e: Exception) {
                Log.e(TAG, "remove repository $url", e)
            }
        }
    }

    suspend fun refreshRepository(url: String, force: Boolean) {
        restored.join()
        val stored = repository(url) ?: return
        val checked = repositoriesChecked[url]
        if (!force && checked != null && System.currentTimeMillis() - checked < UPDATE_INTERVAL) return
        val fetched = fetchRepository(url, stored.etag)
        repositoriesChecked[url] = System.currentTimeMillis()
        if (fetched != null && repository(url) != null) {
            save(fetched)
            repositories = repositories.map { if (it.url == url) fetched else it }
        }
        checkCompatibility()
    }

    suspend fun refreshRepositories() {
        restored.join()
        for (repository in repositories) {
            try {
                refreshRepository(repository.url, force = false)
            } catch (e: Exception) {
                Log.w(TAG, "refresh repository ${repository.url}", e)
            }
        }
    }

    suspend fun install(swiplet: RepositorySwiplet, repository: Repository) = scope.async {
        updating = updating + swiplet.id
        try {
            val file = temporaryFile()
            try {
                download(swiplet.url, file, swiplet.checksum, swiplet.id)
                val installed = install(file, swiplet.id)
                withContext(storage) { database.setRepository(repository.url, installed) }
                links.update { it + 1 }
            } finally {
                discard(file)
            }
        } finally {
            updating = updating - swiplet.id
        }
    }.await()

    suspend fun applyUpdates(ids: List<String>) = scope.async {
        val pending = ids.filter { it !in updating }.mapNotNull(::update)
        updating = updating + pending.map { it.swiplet.id }
        var failure: Exception? = null
        for (update in pending) {
            try {
                install(update.swiplet, update.repository)
            } catch (e: Exception) {
                failure = failure ?: e
            }
        }
        if (failure != null) throw failure
    }.await()

    suspend fun exportFile(id: String): File = withContext(Dispatchers.IO) {
        val folder = File(AppDirectory.caches, "Exports/${UUID.randomUUID()}")
        if (!folder.mkdirs()) throw FileSystemException(folder)
        val file = File(folder, "$id.swiplet")
        try {
            file.outputStream().use { export(id, it) }
        } catch (e: Exception) {
            folder.deleteRecursively()
            throw e
        }
        file
    }

    private suspend fun export(id: String, output: OutputStream) {
        Core.swiplets.exportSwiplet(exportSwipletRequest { swipletId = id }).collect { output.write(it.chunk.toByteArray()) }
    }

    suspend fun remove(id: String) {
        Core.swiplets.removeSwiplet(removeSwipletRequest { swipletId = id })
    }

    suspend fun clearData(swiplet: Swiplet) {
        Core.swiplets.clearSwipletData(clearSwipletDataRequest { swipletId = swiplet.id })
    }

    suspend fun dataSize(swiplet: Swiplet): Long =
        Core.swiplets.getSwipletDataSize(getSwipletDataSizeRequest { swipletId = swiplet.id }).bytes

    fun preferredName(name: String, localized: Map<String, String>): String {
        if (localized.isEmpty()) return name
        val languages = (listOf("en") + localized.keys.filter { it != "en" }.sorted()).map(Locale::forLanguageTag)
        val preferences = LocaleList.getDefault()
        for (index in 0 until preferences.size()) {
            val preference = preferences[index]
            val matches = languages.filter { LocaleList.matchesLanguageAndScript(it, preference) }
            val language = matches.firstOrNull { it.country == preference.country } ?: matches.firstOrNull() ?: continue
            return localized[language.toLanguageTag()] ?: name
        }
        return name
    }

    private fun chunks(file: File): Flow<io.nekohasekai.swipt.proto.DeploySwipletRequest> = flow {
        file.inputStream().use { input: InputStream ->
            val buffer = ByteArray(CHUNK)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                emit(deploySwipletRequest { chunk = ByteString.copyFrom(buffer, 0, count) })
            }
        }
    }.flowOn(Dispatchers.IO)

    private fun failure(inspected: InspectSwipletResponse): Exception? {
        val application = SwiptApplication.application
        return when (inspected.incompatibility) {
            Incompatibility.INCOMPATIBILITY_UNSPECIFIED -> null
            Incompatibility.INCOMPATIBILITY_SWIPT_OUTDATED -> SwiptOutdatedException()
            Incompatibility.INCOMPATIBILITY_OS_OUTDATED -> LocalizedException(application.getString(R.string.swiplets_requires_android, inspected.minimumOsVersion))
            Incompatibility.INCOMPATIBILITY_OS_UNSUPPORTED -> LocalizedException(application.getString(R.string.swiplets_unsupported_android))
            else -> LocalizedException(inspected.incompatibilityMessage)
        }
    }

    private suspend fun install(file: File, expected: String? = null): String {
        val inspected = Core.swiplets.inspectSwiplet(chunks(file))
        if (expected != null && inspected.swiplet.id != expected) {
            throw LocalizedException(SwiptApplication.application.getString(R.string.swiplets_repository_sent_different))
        }
        failure(inspected)?.let { throw it }
        return Core.swiplets.deploySwiplet(chunks(file)).swiplet.id
    }

    private suspend fun preview(file: File): SwipletPreview {
        val inspected = try {
            Core.swiplets.inspectSwiplet(chunks(file))
        } catch (e: io.grpc.StatusException) {
            if (e.status.code == io.grpc.Status.Code.INVALID_ARGUMENT) {
                throw LocalizedException(SwiptApplication.application.getString(R.string.swiplets_file_not_swiplet), e)
            }
            throw e
        }
        failure(inspected)?.let { throw it }
        val swiplet = inspected.swiplet
        val image = inspected.icon.takeIf { !it.isEmpty }?.toByteArray()?.let { BitmapFactory.decodeByteArray(it, 0, it.size) }
        return SwipletPreview(
            file,
            swiplet.id,
            preferredName(swiplet.title, swiplet.localizedTitlesMap).ifEmpty { swiplet.id },
            swiplet.version,
            iconOf(swiplet.icon).copy(image = image),
        )
    }

    private suspend fun save(repository: Repository) = withContext(storage) {
        val record = RepositoryRecord(
            repository.url, 0, repository.name, repository.localizedNames, repository.icon?.color, repository.icon?.symbol,
            repository.icon?.symbolColor, repository.image, repository.etag,
        )
        database.save(
            record,
            repository.swiplets.mapIndexed { position, swiplet ->
                RepositorySwipletRecord(
                    repository.url, position, swiplet.id, swiplet.name, swiplet.localizedNames, swiplet.description, swiplet.version,
                    swiplet.versionCode, swiplet.minimumAPILevel, swiplet.minimumOSVersion, swiplet.icon.color, swiplet.icon.symbol,
                    swiplet.icon.symbolColor, swiplet.url, swiplet.checksum,
                )
            },
        )
    }

    private fun repository(record: RepositoryRecord, swiplets: List<RepositorySwipletRecord>) = Repository(
        record.url, record.name, record.localizedNames, record.iconSymbol?.let { SwipletIcon(record.iconColor, it, record.iconSymbolColor) },
        record.image, record.etag,
        swiplets.map {
            RepositorySwiplet(
                it.id, it.name, it.localizedNames, it.description, it.version, it.versionCode, it.minimumAPILevel, it.minimumOSVersion,
                SwipletIcon(it.iconColor, it.iconSymbol, it.iconSymbolColor), it.url, it.sha256,
            )
        },
    )

    private suspend fun fetchRepository(url: String, etag: String): Repository? {
        val fetched = Core.swiplets.fetchRepository(fetchRepositoryRequest {
            this.url = url
            entityTag = etag
        })
        if (fetched.notModified) return null
        val index = fetched.repository
        val icon = if (index.hasIcon()) iconOf(index.icon) else if (Repository.isOfficialURL(url)) Repository.official.icon else null
        return Repository(
            url, index.name, index.localizedNamesMap, icon, index.image.takeIf { !it.isEmpty }?.toByteArray(), fetched.entityTag,
            index.swipletsList.map {
                RepositorySwiplet(
                    it.id, it.title, it.localizedTitlesMap, it.description, it.version, it.versionCode, it.minimumApiLevel,
                    it.minimumOsVersion, iconOf(it.icon), it.url, it.sha256,
                )
            },
        )
    }

    private suspend fun download(link: String, file: File, checksum: String, progressOf: String?) {
        val latest = AtomicReference<DownloadProgress?>()
        try {
            withContext(Dispatchers.IO) {
                file.outputStream().use { output ->
                    var received = 0L
                    Core.swiplets.downloadSwiplet(downloadSwipletRequest {
                        url = link
                        sha256 = checksum
                    }).collect { part ->
                        part.chunk.writeTo(output)
                        received += part.chunk.size()
                        if (progressOf != null && latest.getAndSet(DownloadProgress(received, part.total)) == null) {
                            scope.launch { latest.getAndSet(null)?.let { downloads = downloads + (progressOf to it) } }
                        }
                    }
                }
            }
        } finally {
            if (progressOf != null) downloads = downloads - progressOf
        }
    }

    private suspend fun temporaryFile(): File = withContext(Dispatchers.IO) {
        val folder = File(AppDirectory.caches, "Imports/${UUID.randomUUID()}")
        if (!folder.mkdirs()) throw FileSystemException(folder)
        File(folder, "swiplet")
    }

    private suspend fun discard(file: File) {
        withContext(NonCancellable + Dispatchers.IO) { file.parentFile?.deleteRecursively() }
    }
}
