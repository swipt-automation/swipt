package io.nekohasekai.swipt.automation

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.FieldModifier
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.SwipeToDelete
import io.nekohasekai.swipt.app.groupItemColors
import kotlinx.coroutines.launch

private val LEADING_SIZE = 32.dp

@Composable
private fun LeadingIcon(icon: ImageVector) {
    Box(Modifier.size(LEADING_SIZE), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = null)
    }
}

@Composable
fun NewSwipletScreen(
    navigationIcon: @Composable () -> Unit,
    openRepository: (String) -> Unit,
    addRepository: () -> Unit,
    addFromURL: () -> Unit,
    onImported: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var isImporting by remember { mutableStateOf(false) }
    var preview by remember { mutableStateOf<SwipletPreview?>(null) }
    var alert by remember { mutableStateOf<AlertState?>(null) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        isImporting = true
        scope.launch {
            try {
                preview = SwipletStore.open(uri)
            } catch (e: Exception) {
                alert = swipletFailure(context, context.getString(R.string.swiplets_cant_open_swiplet), e)
            }
            isImporting = false
        }
    }

    Box(Modifier.fillMaxSize()) {
        PageScaffold(title = stringResource(R.string.swiplets_add_swiplet), navigationIcon = navigationIcon) {
            SectionHeader(stringResource(R.string.swiplets_repository))
            ListGroup {
                for (repository in SwipletStore.repositories) {
                    key(repository.url) {
                        SwipeToDelete({ SwipletStore.removeRepository(repository.url) }, enabled = !repository.isOfficial && !isImporting) {
                            ListItem(
                                headlineContent = { Text(repository.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                                leadingContent = { SwipletIconImage(repository.shownIcon, LEADING_SIZE) },
                                colors = groupItemColors(enabled = !isImporting),
                                modifier = Modifier
                                    .background(MaterialTheme.colorScheme.surfaceContainer)
                                    .clickable(enabled = !isImporting) { openRepository(repository.url) },
                            )
                        }
                    }
                }
                ListItem(
                    headlineContent = { Text(stringResource(R.string.swiplets_add_repository)) },
                    leadingContent = { LeadingIcon(Icons.Outlined.Add) },
                    colors = groupItemColors(enabled = !isImporting),
                    modifier = Modifier.clickable(enabled = !isImporting, onClick = addRepository),
                )
            }
            SectionHeader(stringResource(R.string.swiplets_manual))
            ListGroup {
                ListItem(
                    headlineContent = { Text(stringResource(R.string.swiplets_import_from_file)) },
                    leadingContent = { LeadingIcon(Icons.Outlined.FileOpen) },
                    colors = groupItemColors(enabled = !isImporting),
                    modifier = Modifier.clickable(enabled = !isImporting) { pick.launch(arrayOf("*/*")) },
                )
                ListItem(
                    headlineContent = { Text(stringResource(R.string.swiplets_import_from_url)) },
                    leadingContent = { LeadingIcon(Icons.Outlined.Link) },
                    colors = groupItemColors(enabled = !isImporting),
                    modifier = Modifier.clickable(enabled = !isImporting, onClick = addFromURL),
                )
            }
        }
        if (isImporting) CircularProgressIndicator(Modifier.align(Alignment.Center))
    }
    preview?.let { opened ->
        OpenSwipletDialog(opened) { imported ->
            preview = null
            if (imported) onImported()
        }
    }
    AlertHost(alert) { alert = null }
}

@Composable
fun ImportFromURLScreen(navigationIcon: @Composable () -> Unit, onImported: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var url by rememberSaveable { mutableStateOf("") }
    var isImporting by remember { mutableStateOf(false) }
    var preview by remember { mutableStateOf<SwipletPreview?>(null) }
    var alert by remember { mutableStateOf<AlertState?>(null) }

    fun submit() {
        if (isImporting || url.isBlank()) return
        isImporting = true
        scope.launch {
            try {
                preview = SwipletStore.open(url)
            } catch (e: Exception) {
                alert = swipletFailure(context, context.getString(R.string.swiplets_cant_open_swiplet), e)
            }
            isImporting = false
        }
    }

    URLForm(
        title = stringResource(R.string.swiplets_import_from_url),
        confirmation = stringResource(R.string.swiplets_import),
        url = url,
        onURLChange = { url = it },
        isWorking = isImporting,
        submit = ::submit,
        navigationIcon = navigationIcon,
    )
    preview?.let { opened ->
        OpenSwipletDialog(opened) { imported ->
            preview = null
            if (imported) onImported()
        }
    }
    AlertHost(alert) { alert = null }
}

@Composable
fun AddRepositoryScreen(navigationIcon: @Composable () -> Unit, onAdded: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var url by rememberSaveable { mutableStateOf("") }
    var isAdding by remember { mutableStateOf(false) }
    var alert by remember { mutableStateOf<AlertState?>(null) }

    fun submit() {
        if (isAdding || url.isBlank()) return
        isAdding = true
        scope.launch {
            try {
                SwipletStore.addRepository(url)
                onAdded()
            } catch (e: Exception) {
                alert = AlertState.failure(context.getString(R.string.swiplets_cant_add_repository), e)
            }
            isAdding = false
        }
    }

    URLForm(
        title = stringResource(R.string.swiplets_add_repository),
        confirmation = stringResource(R.string.swiplets_add),
        url = url,
        onURLChange = { url = it },
        isWorking = isAdding,
        submit = ::submit,
        navigationIcon = navigationIcon,
    )
    AlertHost(alert) { alert = null }
}

@Composable
private fun URLForm(
    title: String,
    confirmation: String,
    url: String,
    onURLChange: (String) -> Unit,
    isWorking: Boolean,
    submit: () -> Unit,
    navigationIcon: @Composable () -> Unit,
) {
    PageScaffold(
        title = title,
        navigationIcon = navigationIcon,
        actions = {
            if (isWorking) {
                CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 3.dp)
            } else {
                TextButton(onClick = submit, enabled = url.isNotBlank()) {
                    Text(confirmation)
                }
            }
        },
    ) {
        OutlinedTextField(
            value = url,
            onValueChange = onURLChange,
            modifier = FieldModifier,
            enabled = !isWorking,
            label = { Text(stringResource(R.string.swiplets_url)) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { submit() }),
        )
    }
}
