package io.nekohasekai.swipt.automation

import androidx.compose.foundation.clickable
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.groupItemColors
import kotlinx.coroutines.launch

@Composable
fun SwipletUpdatesScreen(navigationIcon: @Composable () -> Unit, onDone: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val pending = SwipletStore.updates
    var alert by remember { mutableStateOf<AlertState?>(null) }

    fun apply(ids: List<String>) {
        scope.launch {
            try {
                SwipletStore.applyUpdates(ids)
            } catch (e: Exception) {
                alert = swipletFailure(context, context.getString(R.string.swiplets_cant_update_swiplet), e)
            }
        }
    }

    LaunchedEffect(pending.isEmpty()) {
        if (pending.isEmpty()) onDone()
    }

    PageScaffold(title = stringResource(R.string.swiplets_updates), navigationIcon = navigationIcon) {
        ListGroup {
            val enabled = pending.none { it.swiplet.id in SwipletStore.updating }
            ListItem(
                headlineContent = { Text(stringResource(R.string.swiplets_update_all), color = MaterialTheme.colorScheme.primary.copy(alpha = if (enabled) 1f else 0.38f)) },
                colors = groupItemColors(enabled),
                modifier = Modifier.clickable(enabled = enabled) { apply(pending.map { it.swiplet.id }) },
            )
        }
        SectionHeader(stringResource(R.string.swiplets_updatable))
        ListGroup {
            for (update in pending) {
                val installed = SwipletStore[update.swiplet.id]
                ListItem(
                    headlineContent = { Text(installed?.title ?: update.swiplet.id, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    supportingContent = if (update.swiplet.version.isEmpty()) {
                        null
                    } else {
                        { SwipletVersionChange(installed?.version.orEmpty(), update.swiplet.version) }
                    },
                    leadingContent = { installed?.let { SwipletIconImage(it.icon, 40.dp) } },
                    trailingContent = {
                        if (update.swiplet.id in SwipletStore.updating) {
                            SwipletDownloadIndicator(SwipletStore.downloads[update.swiplet.id])
                        } else {
                            FilledTonalButton(onClick = { apply(listOf(update.swiplet.id)) }) {
                                Text(stringResource(R.string.swiplets_update))
                            }
                        }
                    },
                    colors = groupItemColors(),
                )
            }
        }
    }
    AlertHost(alert) { alert = null }
}
