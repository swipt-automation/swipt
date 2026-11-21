package io.nekohasekai.swipt.privilege

import android.content.Context
import android.content.Intent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.openLink

private const val SHIZUKU_PACKAGE = "moe.shizuku.privileged.api"
private const val SHIZUKU_DOWNLOAD = "https://shizuku.rikka.app/download/"

private fun shizukuLaunch(context: Context): Intent? = context.packageManager.getLaunchIntentForPackage(SHIZUKU_PACKAGE)

val Privilege.State.isSetupNeeded: Boolean
    get() = this == Privilege.State.ShizukuNotRunning ||
        this == Privilege.State.ShizukuNotGranted ||
        this == Privilege.State.ShizukuRoot ||
        this == Privilege.State.RootDenied ||
        this is Privilege.State.Exited

@Composable
fun SetupView() {
    val context = LocalContext.current
    val current by Privilege.state.collectAsStateWithLifecycle()
    var problem by remember { mutableStateOf(current) }
    LaunchedEffect(current) {
        if (current.isSetupNeeded) problem = current
    }
    val state = if (current.isSetupNeeded) current else problem
    val (title, action) = when (state) {
        Privilege.State.ShizukuNotGranted -> R.string.privilege_shizuku_not_allowed to R.string.privilege_allow
        Privilege.State.ShizukuRoot -> R.string.privilege_shizuku_root to R.string.privilege_use_root
        Privilege.State.RootDenied -> R.string.privilege_root_denied to R.string.privilege_try_again
        is Privilege.State.Exited -> R.string.privilege_exited to R.string.privilege_reconnect
        else -> R.string.privilege_shizuku_not_running to if (shizukuLaunch(context) != null) R.string.privilege_open_shizuku else R.string.privilege_get_shizuku
    }
    Surface(Modifier.fillMaxSize()) {
        Column(Modifier.safeDrawingPadding()) {
            Column(
                Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 32.dp, vertical = 24.dp),
                verticalArrangement = Arrangement.spacedBy(24.dp, Alignment.CenterVertically),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Icon(Icons.Outlined.AdminPanelSettings, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(56.dp))
                Text(stringResource(title), style = MaterialTheme.typography.headlineMedium, textAlign = TextAlign.Center)
                Text(
                    stringResource(R.string.privilege_setup_message),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
                Column(Modifier.widthIn(max = 520.dp)) {
                    PrivilegeModes()
                }
            }
            Button(
                enabled = current.isSetupNeeded,
                onClick = {
                    when (state) {
                        Privilege.State.ShizukuNotGranted -> Privilege.requestShizukuPermission()
                        Privilege.State.ShizukuRoot -> Privilege.setMode(Privilege.Mode.Root)
                        Privilege.State.RootDenied, is Privilege.State.Exited -> Privilege.reconnect()
                        else -> shizukuLaunch(context)?.let(context::startActivity) ?: context.openLink(SHIZUKU_DOWNLOAD)
                    }
                },
                modifier = Modifier
                    .align(Alignment.CenterHorizontally)
                    .widthIn(max = 480.dp)
                    .fillMaxWidth()
                    .padding(horizontal = 32.dp, vertical = 8.dp),
            ) {
                Text(stringResource(action))
            }
        }
    }
}
