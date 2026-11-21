package io.nekohasekai.swipt.automation

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.lifecycleScope
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.AppTheme
import kotlinx.coroutines.launch

class OpenSwipletActivity : ComponentActivity() {
    private var preview by mutableStateOf<SwipletPreview?>(null)
    private var alert by mutableStateOf<AlertState?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val uri = when (intent.action) {
            Intent.ACTION_VIEW -> intent.data
            Intent.ACTION_SEND -> intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else -> null
        }
        if (uri == null) {
            finish()
            return
        }
        setContent {
            AppTheme {
                preview?.let { preview ->
                    OpenSwipletDialog(preview) { finish() }
                }
                AlertHost(alert) { finish() }
            }
        }
        lifecycleScope.launch {
            try {
                preview = SwipletStore.open(uri)
            } catch (e: Exception) {
                alert = swipletFailure(this@OpenSwipletActivity, getString(R.string.swiplets_cant_open_swiplet), e)
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        if (isFinishing) preview?.let(SwipletStore::discard)
    }
}
