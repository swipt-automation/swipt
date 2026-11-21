package io.nekohasekai.swipt.automation

import android.text.format.Formatter
import androidx.compose.foundation.clickable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.groupItemColors
import kotlinx.coroutines.launch

@Composable
fun SwipletInfoScreen(id: String, navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val swiplet = SwipletStore[id]
    var dataSize by remember { mutableStateOf<Long?>(null) }
    var alert by remember { mutableStateOf<AlertState?>(null) }

    LaunchedEffect(swiplet) {
        dataSize = swiplet?.let { runCatching { SwipletStore.dataSize(it) }.getOrNull() }
    }

    PageScaffold(title = stringResource(R.string.swiplets_swiplet_info), navigationIcon = navigationIcon) {
        ListGroup {
            ListItem(
                headlineContent = { Text(stringResource(R.string.swiplets_name)) },
                supportingContent = { Text(swiplet?.title ?: id) },
                colors = groupItemColors(),
            )
            if (!swiplet?.version.isNullOrEmpty()) {
                ListItem(
                    headlineContent = { Text(stringResource(R.string.swiplets_version)) },
                    supportingContent = { Text(swiplet.version) },
                    colors = groupItemColors(),
                )
            }
        }
        SectionHeader(stringResource(R.string.swiplets_data))
        val size = dataSize
        ListGroup {
            ListItem(
                headlineContent = { Text(stringResource(R.string.swiplets_data_size)) },
                supportingContent = if (size == null) null else ({ Text(Formatter.formatFileSize(context, size)) }),
                leadingContent = { Icon(Icons.Outlined.Storage, contentDescription = null) },
                colors = groupItemColors(),
            )
            if (swiplet != null && size != null && size > 0) {
                ListItem(
                    headlineContent = { Text(stringResource(R.string.swiplets_clear_data)) },
                    leadingContent = { Icon(Icons.Outlined.Delete, contentDescription = null) },
                    colors = groupItemColors(destructive = true),
                    modifier = Modifier.clickable {
                        scope.launch {
                            dataSize = null
                            try {
                                SwipletStore.clearData(swiplet)
                            } catch (e: Exception) {
                                alert = AlertState.failure(context.getString(R.string.swiplets_couldnt_clear_data), e)
                            }
                            dataSize = runCatching { SwipletStore.dataSize(swiplet) }.getOrNull()
                        }
                    },
                )
            }
        }
    }
    AlertHost(alert) { alert = null }
}
