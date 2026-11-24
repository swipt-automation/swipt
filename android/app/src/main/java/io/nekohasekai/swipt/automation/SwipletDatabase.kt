package io.nekohasekai.swipt.automation

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.ForeignKey
import androidx.room.Index
import androidx.room.Insert
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert

@Entity(tableName = "swiplets")
class SwipletRecord(@PrimaryKey val id: String, val position: Int, val repository: String)

@Entity(tableName = "repositories")
data class RepositoryRecord(
    @PrimaryKey val url: String,
    val position: Int,
    val name: String,
    val localizedNames: Map<String, String>,
    val iconColor: String?,
    val iconSymbol: String?,
    val iconSymbolColor: String?,
    val image: ByteArray?,
    val etag: String,
)

@Entity(
    tableName = "repositorySwiplets",
    primaryKeys = ["repository", "id"],
    foreignKeys = [ForeignKey(RepositoryRecord::class, ["url"], ["repository"], onDelete = ForeignKey.CASCADE)],
    indices = [Index("repository")],
)
class RepositorySwipletRecord(
    val repository: String,
    val position: Int,
    val id: String,
    val name: String,
    val localizedNames: Map<String, String>,
    val description: String,
    val version: String,
    val versionCode: Long,
    val minimumAPILevel: Long,
    val minimumOSVersion: String,
    val iconColor: String?,
    val iconSymbol: String,
    val iconSymbolColor: String?,
    val url: String,
    val sha256: String,
)

@Dao
abstract class SwipletDatabase {
    @Query("SELECT * FROM swiplets ORDER BY position")
    abstract fun swiplets(): List<SwipletRecord>

    @Query("SELECT * FROM swiplets WHERE id = :id")
    abstract fun swiplet(id: String): SwipletRecord?

    @Query("SELECT COALESCE(MAX(position) + 1, 0) FROM swiplets")
    abstract fun nextSwipletPosition(): Int

    @Query("DELETE FROM swiplets WHERE id NOT IN (:ids)")
    abstract fun keepSwiplets(ids: List<String>)

    @Upsert
    abstract fun upsert(swiplet: SwipletRecord)

    @Transaction
    open fun setSwipletIDs(ids: List<String>) {
        keepSwiplets(ids)
        ids.forEachIndexed { position, id -> upsert(SwipletRecord(id, position, swiplet(id)?.repository.orEmpty())) }
    }

    @Transaction
    open fun setRepository(repository: String, id: String) {
        upsert(SwipletRecord(id, swiplet(id)?.position ?: nextSwipletPosition(), repository))
    }

    @Query("SELECT * FROM repositories ORDER BY position")
    abstract fun repositories(): List<RepositoryRecord>

    @Query("SELECT * FROM repositories WHERE url = :url")
    abstract fun repository(url: String): RepositoryRecord?

    @Query("SELECT COALESCE(MAX(position) + 1, 0) FROM repositories")
    abstract fun nextRepositoryPosition(): Int

    @Query("SELECT * FROM repositorySwiplets WHERE repository = :repository ORDER BY position")
    abstract fun repositorySwiplets(repository: String): List<RepositorySwipletRecord>

    @Upsert
    abstract fun upsert(repository: RepositoryRecord)

    @Query("DELETE FROM repositorySwiplets WHERE repository = :repository")
    abstract fun clearRepositorySwiplets(repository: String)

    @Insert
    abstract fun insert(swiplets: List<RepositorySwipletRecord>)

    @Query("DELETE FROM repositories WHERE url = :url")
    abstract fun removeRepository(url: String)

    @Transaction
    open fun loadRepositories(): List<Pair<RepositoryRecord, List<RepositorySwipletRecord>>> = repositories().map { it to repositorySwiplets(it.url) }

    @Transaction
    open fun save(repository: RepositoryRecord, swiplets: List<RepositorySwipletRecord>) {
        upsert(repository.copy(position = repository(repository.url)?.position ?: nextRepositoryPosition()))
        clearRepositorySwiplets(repository.url)
        insert(swiplets)
    }
}
