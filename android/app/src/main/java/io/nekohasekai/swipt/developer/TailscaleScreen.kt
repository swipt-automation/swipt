package io.nekohasekai.swipt.developer

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertHost
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.DropdownField
import io.nekohasekai.swipt.app.GeneratedValue
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.SectionFooter
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.SwitchRow
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

@Composable
internal fun TailscaleScreen(navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    var host by rememberSaveable { mutableStateOf("") }
    var login by remember { mutableStateOf<Job?>(null) }
    var isLoggingOut by remember { mutableStateOf(false) }
    var alert by remember { mutableStateOf<AlertState?>(null) }
    val urlLabel = stringResource(R.string.developer_url)

    fun logIn() {
        login = scope.launch {
            try {
                API.logInTailscale { openLogin(context, it) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                alert = AlertState.failure(resources.getString(R.string.developer_cant_log_in), e)
            } finally {
                login = null
            }
        }
    }

    fun logOut() {
        isLoggingOut = true
        scope.launch {
            try {
                API.logOutTailscale()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                alert = AlertState.failure(resources.getString(R.string.developer_cant_log_out), e)
            } finally {
                isLoggingOut = false
            }
        }
    }

    DeveloperPage(title = "Tailscale", navigationIcon = navigationIcon) { api ->
        val tailscale = api.tailscale
        val status = tailscale.status
        ListGroup {
            SwitchRow(
                stringResource(R.string.developer_enabled),
                tailscale.isEnabled,
                API::setTailscaleEnabled,
                enabled = tailscale.isEnabled || status?.isLoggedIn == true,
            )
        }
        SectionHeader(stringResource(R.string.developer_account))
        ListGroup {
            when (status?.state) {
                API.TailscaleStatus.State.NeedsLogin -> ActionRow(stringResource(R.string.developer_log_in), enabled = login == null, onClick = ::logIn)

                API.TailscaleStatus.State.Stopped, API.TailscaleStatus.State.NeedsMachineAuth, API.TailscaleStatus.State.Running -> {
                    InfoRow(stringResource(R.string.developer_name), status.loginName)
                    if (status.tailnet.isNotEmpty() && status.tailnet != status.loginName) {
                        InfoRow(stringResource(R.string.developer_network), status.tailnet)
                    }
                    ActionRow(stringResource(R.string.developer_log_out), enabled = !isLoggingOut && login == null, destructive = true, onClick = ::logOut)
                }

                API.TailscaleStatus.State.Starting, null -> ProgressRow(stringResource(R.string.developer_connecting))
            }
        }
        if (status?.state == API.TailscaleStatus.State.NeedsMachineAuth) {
            SectionFooter(stringResource(R.string.developer_tailscale_needs_approval))
        }
        SectionHeader(stringResource(R.string.developer_authentication))
        ListGroup {
            SwitchRow(stringResource(R.string.developer_enabled), tailscale.authenticates, API::setTailscaleAuthenticates)
            if (tailscale.authenticates) {
                ValueRow(
                    stringResource(R.string.developer_token),
                    tailscale.token,
                    isSecret = true,
                    generated = GeneratedValue(stringResource(R.string.developer_random_token), API::newToken),
                ) { text ->
                    val token = text.trim()
                    if (token.isNotEmpty()) API.setTailscaleToken(token)
                }
            }
        }
        if (status != null && status.state == API.TailscaleStatus.State.Running) {
            val hosts = (listOf(status.dnsName) + status.addresses.map { if (':' in it) "[$it]" else it }).filter { it.isNotEmpty() }
            if (hosts.isNotEmpty()) {
                val selected = if (host in hosts) host else hosts[0]
                val token = tailscale.token.takeIf { tailscale.authenticates }
                UsageSection("Swipt API", leading = {
                    DropdownField(stringResource(R.string.developer_host), selected, hosts, { it }, { host = it })
                }) { revealed ->
                    apiUsage(urlLabel, "http", selected, token, revealed)
                }
                UsageSection("MCP") { revealed ->
                    mcpUsage(urlLabel, "http", selected, token, revealed)
                }
            }
        }
    }
    AlertHost(alert) { alert = null }
}
