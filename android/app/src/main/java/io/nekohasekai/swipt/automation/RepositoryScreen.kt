package io.nekohasekai.swipt.automation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.groupItemColors
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RepositoryScreen(url: String, navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val repository = SwipletStore.repository(url)
    var isLoaded by remember { mutableStateOf(false) }
    var isRefreshing by remember { mutableStateOf(false) }
    var alert by remember { mutableStateOf<AlertState?>(null) }

    suspend fun refresh(force: Boolean) {
        try {
            SwipletStore.refreshRepository(url, force)
        } catch (e: Exception) {
            alert = AlertState.failure(context.getString(R.string.swiplets_couldnt_refresh_repository), e)
        }
    }

    fun install(swiplet: RepositorySwiplet, repository: Repository) {
        scope.launch {
            try {
                SwipletStore.install(swiplet, repository)
            } catch (e: Exception) {
                alert = swipletFailure(context, context.getString(R.string.swiplets_cant_add_swiplet), e)
            }
        }
    }

    LaunchedEffect(url) {
        refresh(force = false)
        isLoaded = true
    }

    PageScaffold(title = repository?.title.orEmpty(), navigationIcon = navigationIcon, scrollable = false) {
        PullToRefreshBox(
            isRefreshing = isRefreshing,
            onRefresh = {
                isRefreshing = true
                scope.launch {
                    refresh(force = true)
                    isRefreshing = false
                }
            },
            modifier = Modifier.fillMaxSize(),
        ) {
            val swiplets = repository?.swiplets.orEmpty()
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                if (repository != null && swiplets.isNotEmpty()) {
                    ListGroup {
                        for (swiplet in swiplets) {
                            RepositorySwipletRow(swiplet, repository) { install(swiplet, repository) }
                        }
                    }
                } else if (isLoaded) {
                    ListGroup {
                        ListItem(
                            headlineContent = { Text(stringResource(R.string.swiplets_repository_empty), color = MaterialTheme.colorScheme.onSurfaceVariant) },
                            colors = groupItemColors(),
                        )
                    }
                }
            }
            if (!isLoaded && swiplets.isEmpty()) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            }
        }
    }
    AlertHost(alert) { alert = null }
}

@Composable
private fun RepositorySwipletRow(swiplet: RepositorySwiplet, repository: Repository, install: () -> Unit) {
    val installed = SwipletStore[swiplet.id]
    val update = SwipletStore.update(swiplet.id)?.takeIf { it.repository.url == repository.url }
    ListItem(
        headlineContent = { Text(swiplet.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = {
            Column {
                if (swiplet.description.isNotEmpty()) Text(swiplet.description, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (swiplet.version.isNotEmpty()) SwipletVersionChange(if (update != null) installed?.version.orEmpty() else "", swiplet.version)
            }
        },
        leadingContent = { SwipletIconImage(swiplet.icon, 40.dp) },
        trailingContent = {
            when {
                swiplet.id in SwipletStore.updating -> SwipletDownloadIndicator(SwipletStore.downloads[swiplet.id])
                installed?.repository != repository.url -> FilledTonalButton(onClick = install) { Text(stringResource(R.string.swiplets_add)) }
                update != null -> FilledTonalButton(onClick = install) { Text(stringResource(R.string.swiplets_update)) }
                else -> Icon(Icons.Outlined.Check, contentDescription = null)
            }
        },
        colors = groupItemColors(),
    )
}
