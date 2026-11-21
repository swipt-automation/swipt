package io.nekohasekai.swipt.developer

import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.ListItem
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.GeneratedValue
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.NavigationRow
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionFooter
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.SwitchRow
import io.nekohasekai.swipt.app.groupItemColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

@Composable
internal fun CloudflareScreen(onMode: () -> Unit, navigationIcon: @Composable () -> Unit) {
    val urlLabel = stringResource(R.string.developer_url)
    DeveloperPage(title = "Cloudflare", navigationIcon = navigationIcon) { api ->
        val cloudflare = api.cloudflare
        ListGroup {
            SwitchRow(
                stringResource(R.string.developer_enabled),
                cloudflare.isEnabled,
                API::setCloudflareEnabled,
                enabled = cloudflare.isEnabled || cloudflare.isComplete,
            )
        }
        SectionHeader(stringResource(R.string.developer_configuration))
        ListGroup {
            NavigationRow(stringResource(R.string.developer_mode), onMode, supporting = cloudflare.mode.title, enabled = !cloudflare.isEnabled)
            ValueRow(
                stringResource(R.string.developer_prefix),
                cloudflare.path,
                keyboardType = KeyboardType.Uri,
                generated = GeneratedValue(stringResource(R.string.developer_random_path), API::newPath),
                enabled = !cloudflare.isEnabled,
            ) { text ->
                val path = text.trim().trimEnd('/')
                API.setCloudflarePath(if (path.startsWith("/")) path else "/$path")
            }
        }
        SectionHeader(stringResource(R.string.developer_authentication))
        ListGroup {
            ValueRow(
                stringResource(R.string.developer_token),
                cloudflare.token,
                isSecret = true,
                generated = GeneratedValue(stringResource(R.string.developer_random_token), API::newToken),
            ) { text ->
                val token = text.trim()
                if (token.isNotEmpty()) API.setCloudflareToken(token)
            }
        }
        val hostname = cloudflare.publicHostname
        if (hostname.isNotEmpty()) {
            val address = { revealed: Boolean -> maskedHost(hostname, revealed) + cloudflare.pathPrefix }
            UsageSection("Swipt API") { revealed ->
                apiUsage(urlLabel, "https", address(revealed), cloudflare.token, revealed)
            }
            UsageSection("MCP") { revealed ->
                mcpUsage(urlLabel, "https", address(revealed), cloudflare.token, revealed)
            }
        }
    }
}

@Composable
internal fun CloudflareModeScreen(onSaved: () -> Unit, navigationIcon: @Composable () -> Unit) {
    val api by API.state.collectAsStateWithLifecycle()
    val cloudflare = api?.cloudflare
    if (cloudflare == null) {
        DeveloperPage(title = "Cloudflare", navigationIcon = navigationIcon) {}
    } else {
        CloudflareModeForm(cloudflare, onSaved, navigationIcon)
    }
}

@Composable
private fun CloudflareModeForm(current: API.Cloudflare, onSaved: () -> Unit, navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    val initial = remember { current }
    var mode by rememberSaveable { mutableStateOf(initial.mode) }
    var account by remember { mutableStateOf(initial.account) }
    var subdomain by rememberSaveable {
        val suffix = initial.account?.let { ".${it.zone}" }
        mutableStateOf(if (suffix != null && initial.hostname.endsWith(suffix)) initial.hostname.removeSuffix(suffix) else "")
    }
    var login by remember { mutableStateOf<Job?>(null) }
    var alert by remember { mutableStateOf<AlertState?>(null) }
    val isComplete = when (mode) {
        API.CloudflareMode.QuickTunnel -> true
        API.CloudflareMode.Tunnel -> account != null && subdomain.isNotEmpty()
    }

    fun logIn() {
        login = scope.launch {
            try {
                account = API.logInCloudflare { openLogin(context, it) }
                if (subdomain.isEmpty()) subdomain = API.hostLabel.lowercase()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                alert = AlertState.failure(resources.getString(R.string.developer_cant_log_in), e)
            } finally {
                login = null
            }
        }
    }

    PageScaffold(
        title = "Cloudflare",
        navigationIcon = navigationIcon,
        actions = {
            TextButton(
                onClick = {
                    val zone = account?.zone
                    API.setCloudflareTunnel(mode, account, if (zone != null && subdomain.isNotEmpty()) "$subdomain.$zone" else "")
                    onSaved()
                },
                enabled = isComplete,
            ) {
                Text(stringResource(R.string.save))
            }
        },
    ) {
        SectionHeader(stringResource(R.string.developer_mode))
        ListGroup(Modifier.selectableGroup()) {
            ModeRow(API.CloudflareMode.QuickTunnel, stringResource(R.string.developer_quick_tunnel_description), mode) { mode = it }
            ModeRow(API.CloudflareMode.Tunnel, stringResource(R.string.developer_tunnel_description), mode) { mode = it }
        }
        if (mode == API.CloudflareMode.Tunnel) {
            val current = account
            SectionHeader(stringResource(R.string.developer_account))
            ListGroup {
                if (current != null && current.name.isNotEmpty()) {
                    InfoRow(stringResource(R.string.developer_name), current.name)
                }
                if (current == null) {
                    ActionRow(stringResource(R.string.developer_log_in), enabled = login == null, onClick = ::logIn)
                } else {
                    ActionRow(stringResource(R.string.developer_reauthorize), enabled = login == null, onClick = ::logIn)
                }
            }
            if (current != null) {
                SectionHeader(stringResource(R.string.developer_domain))
                ListGroup {
                    InfoRow(stringResource(R.string.developer_domain), current.zone)
                    ValueRow(stringResource(R.string.developer_subdomain), subdomain, keyboardType = KeyboardType.Uri) { text ->
                        val label = text.trim().lowercase()
                        subdomain = label.removeSuffix(".${current.zone}").trim('.')
                    }
                }
            }
        }
    }
    AlertHost(alert) { alert = null }
}

@Composable
private fun ModeRow(value: API.CloudflareMode, description: String, selected: API.CloudflareMode, onSelect: (API.CloudflareMode) -> Unit) {
    ListItem(
        headlineContent = { Text(value.title) },
        supportingContent = { Text(description) },
        leadingContent = { RadioButton(selected = value == selected, onClick = null) },
        colors = groupItemColors(),
        modifier = Modifier.selectable(selected = value == selected, role = Role.RadioButton) { onSelect(value) },
    )
}
