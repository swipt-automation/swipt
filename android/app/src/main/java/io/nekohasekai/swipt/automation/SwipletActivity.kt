package io.nekohasekai.swipt.automation

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Modifier
import io.nekohasekai.swipt.app.AppTheme
import io.nekohasekai.swipt.app.ErrorAlertHost
import io.nekohasekai.swipt.app.MainActivity
import io.nekohasekai.swipt.core.Core
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first

// Expo modules require an AppCompatActivity (expo-modules-core 58.0.29, android/src/main/java/expo/modules/kotlin/AppContext.kt:302-304).
class SwipletActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val name = intent.getStringExtra(EXTRA_SWIPLET).orEmpty()
        val entry = SwipletHost.interfaces[name]
        if (entry == null) {
            finish()
            return
        }
        enableEdgeToEdge()
        title = entry.swiplet.title
        setContent {
            AppTheme {
                Scaffold(Modifier.fillMaxSize()) { padding ->
                    SwipletInterface(
                        name,
                        entry,
                        ::finish,
                        Modifier
                            .fillMaxSize()
                            .padding(padding)
                            .consumeWindowInsets(padding)
                            .imePadding(),
                    )
                }
                ErrorAlertHost()
            }
        }
    }

    companion object {
        private const val EXTRA_SWIPLET = "swiplet"

        fun open(context: Context, swiplet: String) {
            val intent = Intent(context, SwipletActivity::class.java).putExtra(EXTRA_SWIPLET, swiplet)
            if (context !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(intent)
        }

        suspend fun openWhenReady(activity: Activity, swiplet: String) {
            val hasInterface = combine(Core.state, snapshotFlow { (swiplet in SwipletHost.interfaces) to (SwipletStore.isLoaded && SwipletStore[swiplet]?.hasInterface != true) }) { state, (shown, hasNone) ->
                when {
                    shown -> true
                    hasNone || state is Core.State.Failed -> false
                    else -> null
                }
            }.filterNotNull().first()
            if (hasInterface) {
                open(activity, swiplet)
            } else {
                MainActivity.reveal(activity)
            }
        }
    }
}
