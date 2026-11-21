@file:OptIn(ExperimentalPathApi::class)

package io.nekohasekai.swipt.model

import android.system.ErrnoException
import android.system.Os
import io.nekohasekai.swipt.app.SwiptApplication
import java.io.File
import java.nio.file.Files
import kotlin.io.path.ExperimentalPathApi
import kotlin.io.path.deleteRecursively

object AppDirectory {
    val caches: File get() = SwiptApplication.application.cacheDir

    fun size(directory: File): Long = directory.walkTopDown().onEnter { !Files.isSymbolicLink(it.toPath()) }.filter { it.isFile }.sumOf { file ->
        try {
            Os.lstat(file.path).st_blocks * 512
        } catch (error: ErrnoException) {
            0L
        }
    }

    fun clearCaches() {
        val items = caches.listFiles() ?: throw FileSystemException(caches)
        for (item in items) {
            item.toPath().deleteRecursively()
        }
    }
}
