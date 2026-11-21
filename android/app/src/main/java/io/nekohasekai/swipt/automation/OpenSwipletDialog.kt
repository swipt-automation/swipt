package io.nekohasekai.swipt.automation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import kotlinx.coroutines.launch

@Composable
fun OpenSwipletDialog(preview: SwipletPreview, onDone: (imported: Boolean) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var isImporting by remember(preview) { mutableStateOf(false) }
    var alert by remember { mutableStateOf<AlertState?>(null) }
    val installed = SwipletStore[preview.id]
    val cancel = {
        SwipletStore.discard(preview)
        onDone(false)
    }
    AlertDialog(
        onDismissRequest = cancel,
        confirmButton = {
            if (isImporting) {
                CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 3.dp)
            } else {
                TextButton(
                    onClick = {
                        isImporting = true
                        scope.launch {
                            try {
                                SwipletStore.importSwiplet(preview)
                                onDone(true)
                            } catch (e: Exception) {
                                alert = swipletFailure(context, context.getString(R.string.swiplets_cant_import_swiplet), e)
                                isImporting = false
                            }
                        }
                    },
                ) {
                    Text(stringResource(if (installed == null) R.string.swiplets_add else R.string.swiplets_update))
                }
            }
        },
        dismissButton = {
            TextButton(onClick = cancel, enabled = !isImporting) {
                Text(stringResource(R.string.cancel))
            }
        },
        title = { Text(stringResource(if (installed == null) R.string.swiplets_add_swiplet else R.string.swiplets_update_swiplet)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    SwipletIconImage(preview.icon, 40.dp)
                    Column {
                        Text(preview.title, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        if (preview.version.isNotEmpty()) {
                            Text(preview.version, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                if (installed != null) {
                    Text(
                        if (installed.version.isEmpty()) {
                            stringResource(R.string.swiplets_installed_copy_replaced)
                        } else {
                            stringResource(R.string.swiplets_installed_version_replaced, installed.version)
                        },
                    )
                }
            }
        },
        properties = DialogProperties(dismissOnBackPress = !isImporting, dismissOnClickOutside = !isImporting),
    )
    AlertHost(alert) { alert = null }
}
