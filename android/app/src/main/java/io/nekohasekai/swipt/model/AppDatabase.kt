package io.nekohasekai.swipt.model

import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.TypeConverter
import androidx.room.TypeConverters
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.automation.RepositoryRecord
import io.nekohasekai.swipt.automation.RepositorySwipletRecord
import io.nekohasekai.swipt.automation.SwipletDatabase
import io.nekohasekai.swipt.automation.SwipletRecord
import kotlinx.serialization.json.Json

class AppConverters {
    @TypeConverter
    fun fromNames(value: Map<String, String>): String = Json.encodeToString(value)

    @TypeConverter
    fun toNames(value: String): Map<String, String> = Json.decodeFromString(value)
}

@Database(entities = [SwipletRecord::class, RepositoryRecord::class, RepositorySwipletRecord::class], version = 1, exportSchema = false)
@TypeConverters(AppConverters::class)
abstract class AppDatabase : RoomDatabase() {
    abstract fun swiplets(): SwipletDatabase

    companion object {
        val shared: AppDatabase by lazy {
            Room.databaseBuilder(SwiptApplication.application, AppDatabase::class.java, "Swipt.sqlite")
                .fallbackToDestructiveMigration(dropAllTables = true)
                .build()
        }
    }
}
