package io.nekohasekai.swipt.settings

import android.text.format.Formatter
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material.icons.outlined.Apps
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.LockOpen
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
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.google.protobuf.Empty
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.BackButton
import io.nekohasekai.swipt.app.ChoiceDialog
import io.nekohasekai.swipt.app.HomeTab
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.NavigationRow
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionFooter
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.SwitchRow
import io.nekohasekai.swipt.app.groupItemColors
import io.nekohasekai.swipt.app.openLink
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.model.AppDirectory
import io.nekohasekai.swipt.privilege.PrivilegesScreen
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val SOURCE_CODE = "https://github.com/swipt-automation/swipt"

private val APP = "${HomeTab.Settings.route}/app"
private val PRIVILEGES = "${HomeTab.Settings.route}/privileges"
private val AUTO_UNLOCK = "${HomeTab.Settings.route}/autoUnlock"
private val AUTO_UNLOCK_SETUP = "${HomeTab.Settings.route}/autoUnlock/setup"

fun NavGraphBuilder.settingsDestinations(navController: NavController) {
    val back: @Composable () -> Unit = { BackButton { navController.navigateUp() } }
    composable(HomeTab.Settings.route) {
        SettingsScreen(navController)
    }
    composable(APP) { AppSettingsView(back) }
    composable(PRIVILEGES) { PrivilegesScreen(back) }
    composable(AUTO_UNLOCK) { AutoUnlockView({ navController.navigate(AUTO_UNLOCK_SETUP) }, back) }
    composable(AUTO_UNLOCK_SETUP) { AutoUnlockSetupScreen({ navController.navigateUp() }, back) }
}

@Composable
private fun SettingsScreen(navController: NavController) {
    val context = LocalContext.current
    PageScaffold(title = stringResource(R.string.settings), navigationIcon = {}) {
        ListGroup {
            NavigationRow(stringResource(R.string.settings_app), { navController.navigate(APP) }, Icons.Outlined.Apps)
            NavigationRow(stringResource(R.string.privilege_title), { navController.navigate(PRIVILEGES) }, Icons.Outlined.AdminPanelSettings)
            NavigationRow(stringResource(R.string.settings_auto_unlock), { navController.navigate(AUTO_UNLOCK) }, Icons.Outlined.LockOpen)
        }
        SectionHeader(stringResource(R.string.settings_about))
        ListGroup {
            ListItem(
                headlineContent = { Text(stringResource(R.string.settings_source_code)) },
                leadingContent = { Icon(Icons.Outlined.Code, contentDescription = null) },
                trailingContent = { Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null) },
                colors = groupItemColors(),
                modifier = Modifier.clickable { context.openLink(SOURCE_CODE) },
            )
        }
    }
}

@Composable
private fun AutoUnlockView(onSetup: () -> Unit, navigationIcon: @Composable () -> Unit) {
    val settings by Core.settings.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    PageScaffold(title = stringResource(R.string.settings_auto_unlock), navigationIcon = navigationIcon) {
        val current = settings ?: return@PageScaffold
        ListGroup {
            SwitchRow(
                stringResource(R.string.developer_enabled),
                current.autoUnlock,
                { enabled -> scope.launch { AutoUnlock.update { autoUnlock = enabled } } },
                enabled = current.autoUnlock || current.isUnlockConfigured,
            )
        }
        SectionFooter(stringResource(R.string.settings_auto_unlock_footer))
        Spacer(Modifier.height(8.dp))
        ListGroup {
            NavigationRow(
                stringResource(R.string.settings_screen_lock),
                onSetup,
                supporting = current.androidUnlockType?.let { stringResource(it.titleRes) } ?: stringResource(R.string.settings_not_set),
            )
        }
    }
}

private suspend fun readCacheSize(): Long = withContext(Dispatchers.IO) {
    val appBytes = AppDirectory.size(AppDirectory.caches)
    val coreBytes = if (Core.state.value is Core.State.Running) {
        try {
            Core.system.getCacheSize(Empty.getDefaultInstance()).bytes
        } catch (_: Exception) {
            0L
        }
    } else {
        0L
    }
    appBytes + coreBytes
}

@Composable
private fun AppSettingsView(navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    val coreState by Core.state.collectAsStateWithLifecycle()
    var cacheSize by remember { mutableStateOf<Long?>(null) }
    var alert by remember { mutableStateOf<AlertState?>(null) }

    LaunchedEffect(coreState is Core.State.Running) {
        cacheSize = readCacheSize()
    }

    PageScaffold(title = stringResource(R.string.settings_app), navigationIcon = navigationIcon) {
        ListGroup {
            LanguageRow()
        }
        SectionHeader(stringResource(R.string.settings_cache))
        val size = cacheSize
        ListGroup {
            ListItem(
                headlineContent = { Text(stringResource(R.string.settings_cache_size)) },
                supportingContent = if (size == null) null else ({ Text(Formatter.formatFileSize(context, size)) }),
                leadingContent = { Icon(Icons.Outlined.Storage, contentDescription = null) },
                colors = groupItemColors(),
            )
            if (size != null && size > 0L) {
                ListItem(
                    headlineContent = { Text(stringResource(R.string.settings_clear_cache)) },
                    leadingContent = { Icon(Icons.Outlined.Delete, contentDescription = null) },
                    colors = groupItemColors(destructive = true),
                    modifier = Modifier.clickable {
                        scope.launch {
                            cacheSize = null
                            try {
                                withContext(Dispatchers.IO) {
                                    AppDirectory.clearCaches()
                                    if (Core.state.value is Core.State.Running) {
                                        Core.system.clearCaches(Empty.getDefaultInstance())
                                    }
                                }
                            } catch (error: Exception) {
                                alert = AlertState.failure(resources.getString(R.string.settings_couldnt_clear_cache), error)
                            }
                            cacheSize = readCacheSize()
                        }
                    },
                )
            }
        }
    }
    AlertHost(alert) { alert = null }
}

@Composable
private fun LanguageRow() {
    var language by remember { mutableStateOf(AppLanguage.selected) }
    var isPicking by remember { mutableStateOf(false) }
    ListItem(
        headlineContent = { Text(stringResource(R.string.settings_language)) },
        supportingContent = { Text(language?.let(AppLanguage::name) ?: stringResource(R.string.settings_system_default)) },
        leadingContent = { Icon(Icons.Outlined.Language, contentDescription = null) },
        colors = groupItemColors(),
        modifier = Modifier.clickable { isPicking = true },
    )
    if (isPicking) {
        val available = remember { AppLanguage.available }
        ChoiceDialog(
            title = stringResource(R.string.settings_language),
            choices = listOf<Pair<String?, String>>(null to stringResource(R.string.settings_system_default)) + available.map { it to AppLanguage.name(it) },
            selected = language,
            onSelect = { code ->
                isPicking = false
                if (code != language) {
                    language = code
                    AppLanguage.select(code)
                }
            },
            onDismiss = { isPicking = false },
        )
    }
}
