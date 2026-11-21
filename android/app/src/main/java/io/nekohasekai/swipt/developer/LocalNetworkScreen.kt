package io.nekohasekai.swipt.developer

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.DropdownField
import io.nekohasekai.swipt.app.GeneratedValue
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.SwitchRow

@Composable
internal fun LocalNetworkScreen(navigationIcon: @Composable () -> Unit) {
    val addresses = rememberLocalAddresses()
    var host by rememberSaveable { mutableStateOf("") }
    val urlLabel = stringResource(R.string.developer_url)
    DeveloperPage(title = stringResource(R.string.developer_local_network), navigationIcon = navigationIcon) { api ->
        val local = api.localNetwork
        ListGroup {
            SwitchRow(stringResource(R.string.developer_enabled), local.isEnabled, API::setLocalNetworkEnabled)
        }
        SectionHeader(stringResource(R.string.developer_configuration))
        ListGroup {
            ValueRow(
                stringResource(R.string.developer_port),
                local.port.toString(),
                keyboardType = KeyboardType.Number,
            ) { text ->
                text.trim().toIntOrNull()?.takeIf { it in API.ports }?.let(API::setPort)
            }
        }
        SectionHeader(stringResource(R.string.developer_authentication))
        ListGroup {
            SwitchRow(stringResource(R.string.developer_enabled), local.authenticates, API::setLocalAuthenticates)
            if (local.authenticates) {
                ValueRow(
                    stringResource(R.string.developer_token),
                    local.token,
                    isSecret = true,
                    generated = GeneratedValue(stringResource(R.string.developer_random_token), API::newToken),
                ) { text ->
                    val token = text.trim()
                    if (token.isNotEmpty()) API.setLocalToken(token)
                }
            }
        }
        val hosts = if (addresses.isEmpty()) emptyList() else listOfNotNull(local.hostname.ifEmpty { null }) + addresses
        if (hosts.isNotEmpty()) {
            val selected = if (host in hosts) host else hosts[0]
            val address = "$selected:${local.port}"
            val token = local.token.takeIf { local.authenticates }
            UsageSection("Swipt API", leading = {
                DropdownField(stringResource(R.string.developer_host), selected, hosts, { it }, { host = it })
            }) { revealed ->
                apiUsage(urlLabel, "http", address, token, revealed)
            }
            UsageSection("MCP") { revealed ->
                mcpUsage(urlLabel, "http", address, token, revealed)
            }
        }
    }
}
