package io.nekohasekai.swipt.automation

import android.content.Context
import android.text.format.Formatter
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.openLink

@Composable
fun SwipletVersionChange(installed: String, available: String) {
    val style = MaterialTheme.typography.bodySmall
    val color = MaterialTheme.colorScheme.onSurfaceVariant
    if (installed.isEmpty() || installed == available) {
        Text(available, style = style, color = color)
        return
    }
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(installed, style = style, color = color)
        Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = null, modifier = Modifier.size(12.dp), tint = color)
        Text(available, style = style, color = color)
    }
}

@Composable
fun SwipletDownloadIndicator(progress: DownloadProgress?) {
    val context = LocalContext.current
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        if (progress != null) {
            val received = Formatter.formatShortFileSize(context, progress.received)
            Text(
                if (progress.total > 0) "$received / ${Formatter.formatShortFileSize(context, progress.total)}" else received,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        val fraction = progress?.takeIf { it.total > 0 }?.let { it.received.toFloat() / it.total }
        if (fraction != null) {
            CircularProgressIndicator({ fraction }, Modifier.size(24.dp), strokeWidth = 3.dp)
        } else {
            CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 3.dp)
        }
    }
}

fun swipletFailure(context: Context, failure: String, error: Throwable): AlertState {
    val alert = AlertState.failure(failure, error)
    if (error !is SwiptOutdatedException) return alert
    return alert.copy(
        confirm = AlertState.Button(context.getString(R.string.swiplets_learn_more)) { context.openLink(UPDATE_GUIDE) },
        dismiss = alert.confirm,
    )
}

private const val UPDATE_GUIDE = "https://swipt.io/"
