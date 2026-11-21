package io.nekohasekai.swipt.automation

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.calculateEndPadding
import androidx.compose.foundation.layout.calculateStartPadding
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.InlineTextContent
import androidx.compose.foundation.text.appendInlineContent
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.outlined.ArrowCircleDown
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.onLongClick
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.Placeholder
import androidx.compose.ui.text.PlaceholderVerticalAlign
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.core.content.FileProvider
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.navigation.NavController
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.BackButton
import io.nekohasekai.swipt.app.HomeTab
import io.nekohasekai.swipt.app.LocalScaffoldPadding
import io.nekohasekai.swipt.app.OverrideTopBar
import io.nekohasekai.swipt.app.SwipeToDelete
import io.nekohasekai.swipt.app.groupItemColors
import io.nekohasekai.swipt.app.listGroupItem
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import sh.calvin.reorderable.ReorderableCollectionItemScope
import sh.calvin.reorderable.ReorderableItem
import sh.calvin.reorderable.rememberReorderableLazyListState

private val SWIPLETS = HomeTab.Swiplets.route
private val INFO = "$SWIPLETS/info"
private val UPDATES = "$SWIPLETS/updates"
private val ADD = "$SWIPLETS/add"
private val ADD_FROM_URL = "$SWIPLETS/add/url"
private val ADD_REPOSITORY = "$SWIPLETS/add/repository"
private val REPOSITORY = "$SWIPLETS/repository"

fun NavGraphBuilder.swipletsDestinations(navController: NavController) {
    val back: @Composable () -> Unit = { BackButton { navController.navigateUp() } }
    val returnToList: () -> Unit = { navController.popBackStack(SWIPLETS, inclusive = false) }
    composable(SWIPLETS) {
        SwipletsScreen(navController)
    }
    composable(
        "$INFO/{id}",
        listOf(navArgument("id") { type = NavType.StringType }),
    ) { entry ->
        SwipletInfoScreen(entry.arguments?.getString("id").orEmpty(), back)
    }
    composable(UPDATES) {
        SwipletUpdatesScreen(back, returnToList)
    }
    composable(ADD) {
        NewSwipletScreen(
            back,
            openRepository = { navController.navigate("$REPOSITORY?url=${Uri.encode(it)}") },
            addRepository = { navController.navigate(ADD_REPOSITORY) },
            addFromURL = { navController.navigate(ADD_FROM_URL) },
            onImported = returnToList,
        )
    }
    composable(ADD_FROM_URL) {
        ImportFromURLScreen(back, onImported = returnToList)
    }
    composable(ADD_REPOSITORY) {
        AddRepositoryScreen(back, onAdded = { navController.navigateUp() })
    }
    composable(
        "$REPOSITORY?url={url}",
        listOf(navArgument("url") { type = NavType.StringType }),
    ) { entry ->
        RepositoryScreen(entry.arguments?.getString("url").orEmpty(), back)
    }
}

@Composable
private fun SwipletsScreen(navController: NavController) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val swiplets = SwipletStore.swiplets
    var alert by remember { mutableStateOf<AlertState?>(null) }
    var menu by remember { mutableStateOf<String?>(null) }
    var dragOrder by remember { mutableStateOf<List<String>?>(null) }

    LaunchedEffect(lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            while (true) {
                SwipletStore.refreshRepositories()
                delay(SwipletStore.UPDATE_INTERVAL)
            }
        }
    }

    fun open(swiplet: Swiplet) {
        when {
            swiplet.needsUpdate -> alert = AlertState(
                context.getString(R.string.swiplets_outdated),
                context.getString(R.string.swiplets_outdated_message, swiplet.title),
            )
            swiplet.id in SwipletHost.interfaces -> SwipletActivity.open(context, swiplet.id)
        }
    }

    fun share(swiplet: Swiplet) {
        scope.launch {
            try {
                val file = SwipletStore.exportFile(swiplet.id)
                val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
                val send = Intent(Intent.ACTION_SEND)
                    .setType("application/zip")
                    .putExtra(Intent.EXTRA_STREAM, uri)
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                context.startActivity(Intent.createChooser(send, null))
            } catch (e: Exception) {
                alert = AlertState.failure(context.getString(R.string.swiplets_cant_share_swiplet), e)
            }
        }
    }

    fun checkForUpdate(swiplet: Swiplet) {
        scope.launch {
            try {
                SwipletStore.refreshRepository(swiplet.repository, force = true)
                if (SwipletStore.update(swiplet.id) == null) alert = AlertState(context.getString(R.string.swiplets_up_to_date))
            } catch (e: Exception) {
                alert = AlertState.failure(context.getString(R.string.swiplets_couldnt_check_for_updates), e)
            }
        }
    }

    fun delete(swiplet: Swiplet) {
        alert = AlertState(
            context.getString(R.string.swiplets_confirm_delete, swiplet.title),
            confirm = AlertState.Button(context.getString(R.string.swiplets_delete), destructive = true) {
                scope.launch {
                    try {
                        SwipletStore.remove(swiplet.id)
                    } catch (e: Exception) {
                        alert = AlertState.failure(context.getString(R.string.swiplets_cant_delete_swiplet), e)
                    }
                }
            },
            dismiss = AlertState.Button(context.getString(R.string.cancel)),
        )
    }

    OverrideTopBar {
        TopAppBar(
            title = { Text(stringResource(R.string.swiplets)) },
            actions = {
                if (SwipletStore.updates.isNotEmpty()) {
                    IconButton(onClick = { navController.navigate(UPDATES) }) {
                        Icon(Icons.Outlined.ArrowCircleDown, contentDescription = stringResource(R.string.swiplets_updates))
                    }
                }
            },
        )
    }

    val padding = LocalScaffoldPadding.current
    val layoutDirection = LocalLayoutDirection.current
    val horizontalPadding = Modifier.padding(start = padding.calculateStartPadding(layoutDirection), end = padding.calculateEndPadding(layoutDirection))
    val shown = dragOrder?.mapNotNull(SwipletStore::get) ?: swiplets
    Box(Modifier.fillMaxSize()) {
        if (swiplets.isEmpty()) {
            EmptySwiplets(Modifier.fillMaxSize().padding(padding))
        } else {
            val listState = rememberLazyListState()
            val reorderState = rememberReorderableLazyListState(listState) { from, to ->
                val ids = (dragOrder ?: swiplets.map(Swiplet::id)).toMutableList()
                ids.add(ids.indexOf(to.key as String), ids.removeAt(ids.indexOf(from.key as String)))
                dragOrder = ids
                menu = null
            }
            LazyColumn(
                Modifier
                    .fillMaxSize()
                    .then(horizontalPadding)
                    .dismissMenuOnMove(isShown = { menu != null }, dismiss = { menu = null }),
                state = listState,
                contentPadding = PaddingValues(top = padding.calculateTopPadding() + 8.dp, bottom = padding.calculateBottomPadding() + FabSpace),
            ) {
                itemsIndexed(shown, key = { _, swiplet -> swiplet.id }) { index, swiplet ->
                    ReorderableItem(reorderState, key = swiplet.id) { isDragging ->
                        SwipletRow(
                            swiplet,
                            Modifier.listGroupItem(index, shown.size, isRaised = isDragging),
                            isMenuShown = menu == swiplet.id,
                            setMenuShown = { menu = if (it) swiplet.id else null },
                            onClick = { open(swiplet) },
                            onDragStopped = {
                                dragOrder?.let(SwipletStore::reorder)
                                dragOrder = null
                            },
                            showInfo = { navController.navigate("$INFO/${swiplet.id}") },
                            share = { share(swiplet) },
                            checkForUpdate = { checkForUpdate(swiplet) },
                            delete = { delete(swiplet) },
                        )
                    }
                }
            }
        }
        FloatingActionButton(
            onClick = { navController.navigate(ADD) },
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .then(horizontalPadding)
                .padding(bottom = padding.calculateBottomPadding())
                .padding(16.dp),
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        ) {
            Icon(Icons.Filled.Add, contentDescription = stringResource(R.string.swiplets_add_swiplet))
        }
    }
    AlertHost(alert) { alert = null }
}

private val FabSpace = 88.dp

@Composable
private fun ReorderableCollectionItemScope.SwipletRow(
    swiplet: Swiplet,
    modifier: Modifier,
    isMenuShown: Boolean,
    setMenuShown: (Boolean) -> Unit,
    onClick: () -> Unit,
    onDragStopped: () -> Unit,
    showInfo: () -> Unit,
    share: () -> Unit,
    checkForUpdate: () -> Unit,
    delete: () -> Unit,
) {
    val haptics = LocalHapticFeedback.current
    SwipeToDelete(delete, modifier) {
        Box(Modifier.background(MaterialTheme.colorScheme.surfaceContainer)) {
            ListItem(
                headlineContent = { Text(swiplet.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingContent = { SwipletIconImage(swiplet.icon, 40.dp) },
                colors = groupItemColors(),
                modifier = Modifier
                    .longPressDraggableHandle(
                        onDragStarted = {
                            haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                            setMenuShown(true)
                        },
                        onDragStopped = onDragStopped,
                    )
                    .clickable { if (!isMenuShown) onClick() }
                    .semantics {
                        onLongClick {
                            setMenuShown(true)
                            true
                        }
                    },
            )
            DropdownMenu(expanded = isMenuShown, onDismissRequest = { setMenuShown(false) }) {
                @Composable
                fun Item(title: Int, icon: ImageVector, action: () -> Unit) {
                    DropdownMenuItem(
                        text = { Text(stringResource(title)) },
                        leadingIcon = { Icon(icon, contentDescription = null) },
                        onClick = {
                            setMenuShown(false)
                            action()
                        },
                    )
                }
                Item(R.string.swiplets_info, Icons.Outlined.Info, showInfo)
                Item(R.string.swiplets_share, Icons.Outlined.Share, share)
                if (SwipletStore.update(swiplet.id) == null && SwipletStore.repository(swiplet.repository) != null) {
                    Item(R.string.swiplets_check_for_updates, Icons.Outlined.Refresh, checkForUpdate)
                }
                HorizontalDivider()
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.swiplets_delete)) },
                    leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null) },
                    onClick = {
                        setMenuShown(false)
                        delete()
                    },
                    colors = MenuDefaults.itemColors(textColor = MaterialTheme.colorScheme.error, leadingIconColor = MaterialTheme.colorScheme.error),
                )
            }
        }
    }
}

@Composable
private fun EmptySwiplets(modifier: Modifier) {
    Column(
        modifier.padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(Icons.Outlined.AutoAwesome, contentDescription = null, modifier = Modifier.size(48.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.height(16.dp))
        Text(stringResource(R.string.swiplets_empty), style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
        Spacer(Modifier.height(8.dp))
        val marker = "\uFFFC"
        val description = stringResource(R.string.swiplets_tap_to_add, marker)
        val index = description.indexOf(marker)
        val text = buildAnnotatedString {
            if (index < 0) {
                append(description)
            } else {
                append(description.substring(0, index))
                appendInlineContent("add", "+")
                append(description.substring(index + marker.length))
            }
        }
        Text(
            text,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
            inlineContent = mapOf(
                "add" to InlineTextContent(Placeholder(1.2.em, 1.2.em, PlaceholderVerticalAlign.TextCenter)) {
                    Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.fillMaxSize())
                },
            ),
        )
    }
}

private fun Modifier.dismissMenuOnMove(isShown: () -> Boolean, dismiss: () -> Unit): Modifier = pointerInput(Unit) {
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        while (true) {
            val change = awaitPointerEvent(PointerEventPass.Initial).changes.firstOrNull { it.id == down.id } ?: break
            if (!change.pressed) break
            if (isShown() && (change.position - down.position).getDistance() > viewConfiguration.touchSlop) dismiss()
        }
    }
}
