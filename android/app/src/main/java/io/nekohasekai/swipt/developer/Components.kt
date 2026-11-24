package io.nekohasekai.swipt.developer

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.CopyableText
import io.nekohasekai.swipt.app.GeneratedValue
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.TextFieldDialog
import io.nekohasekai.swipt.app.groupItemColors
import kotlin.math.min

internal class UsageItem(val label: String, val text: String)

internal val API.CloudflareMode.title: String
    get() = when (this) {
        API.CloudflareMode.QuickTunnel -> "Quick Tunnel"
        API.CloudflareMode.Tunnel -> "Tunnel"
    }

private fun maskedToken(token: String): String = "•".repeat(min(token.length, 3))

internal fun maskedHost(host: String, revealed: Boolean): String = if (revealed || !host.endsWith(".trycloudflare.com")) host else "***" + host.dropWhile { it != '.' }

private fun apiURL(scheme: String, address: String, token: String?, revealed: Boolean): String {
    val credentials = token?.let { (if (revealed) Uri.encode(it) else maskedToken(it)) + "@" }.orEmpty()
    return "$scheme://$credentials$address"
}

internal fun apiUsage(urlLabel: String, scheme: String, address: String, token: String?, revealed: Boolean): List<UsageItem> {
    val api = apiURL(scheme, address, token, revealed)
    return listOf(UsageItem(urlLabel, api), UsageItem("Swipt CLI", "swipt device add $api"))
}

internal fun mcpUsage(urlLabel: String, scheme: String, address: String, token: String?, revealed: Boolean): List<UsageItem> {
    val mcp = apiURL(scheme, address, token, revealed) + API.MCP_PATH
    val name = API.hostLabel
    return listOf(
        UsageItem(urlLabel, mcp),
        UsageItem("Claude Code", "claude mcp add --transport http $name $mcp"),
        UsageItem("Codex", "codex mcp add $name --url $mcp"),
    )
}

internal fun openLogin(context: Context, url: String) {
    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
}

@Composable
internal fun DeveloperPage(
    title: String,
    navigationIcon: @Composable () -> Unit,
    actions: @Composable RowScope.() -> Unit = {},
    content: @Composable ColumnScope.(API.State) -> Unit,
) {
    val api by API.state.collectAsStateWithLifecycle()
    PageScaffold(title = title, navigationIcon = navigationIcon, actions = actions) {
        api?.let { content(it) }
    }
}

@Composable
internal fun UsageSection(title: String, leading: @Composable () -> Unit = {}, items: (revealed: Boolean) -> List<UsageItem>) {
    var isRevealed by rememberSaveable { mutableStateOf(false) }
    val commands = items(true)
    val displays = items(isRevealed)
    Row(
        Modifier.padding(start = 32.dp, end = 20.dp, top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            title,
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier
                .weight(1f)
                .semantics { heading() },
        )
        IconButton(onClick = { isRevealed = !isRevealed }) {
            Icon(
                if (isRevealed) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                contentDescription = stringResource(if (isRevealed) R.string.hide else R.string.show),
            )
        }
    }
    ListGroup {
        leading()
        for ((command, display) in commands.zip(displays)) {
            Column(
                Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(command.label, style = MaterialTheme.typography.bodyLarge)
                CopyableText(display.text, command.text, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace, letterSpacing = 0.sp))
            }
        }
    }
}

@Composable
internal fun ValueRow(
    title: String,
    value: String,
    isSecret: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
    generated: GeneratedValue? = null,
    enabled: Boolean = true,
    save: (String) -> Unit,
) {
    var isEditing by rememberSaveable { mutableStateOf(false) }
    ListItem(
        headlineContent = { Text(title) },
        supportingContent = {
            Text(
                when {
                    value.isEmpty() -> stringResource(R.string.developer_required)
                    isSecret -> maskedToken(value)
                    else -> value
                },
            )
        },
        colors = groupItemColors(enabled),
        modifier = Modifier.clickable(enabled = enabled) { isEditing = true },
    )
    if (!isEditing) return
    TextFieldDialog(
        title = title,
        initial = value,
        onDismiss = { isEditing = false },
        onSave = {
            isEditing = false
            save(it)
        },
        keyboardType = keyboardType,
        generated = generated,
    )
}

@Composable
internal fun InfoRow(title: String, value: String) {
    ListItem(
        headlineContent = { Text(title) },
        supportingContent = { Text(value) },
        colors = groupItemColors(),
    )
}

@Composable
internal fun ActionRow(title: String, enabled: Boolean = true, destructive: Boolean = false, onClick: () -> Unit) {
    val color = if (destructive) Color.Unspecified else MaterialTheme.colorScheme.primary.copy(alpha = if (enabled) 1f else 0.38f)
    ListItem(
        headlineContent = { Text(title, color = color) },
        colors = groupItemColors(enabled, destructive),
        modifier = Modifier.clickable(enabled = enabled, onClick = onClick),
    )
}

@Composable
internal fun ProgressRow(title: String) {
    ListItem(
        headlineContent = { Text(title) },
        trailingContent = { CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 3.dp) },
        colors = groupItemColors(),
    )
}
