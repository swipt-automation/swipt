package io.nekohasekai.swipt.app

import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import io.nekohasekai.swipt.R

@Composable
fun AlertHost(alert: AlertState?, onDismiss: () -> Unit) {
    if (alert == null) return

    @Composable
    fun AlertButton(button: AlertState.Button) {
        TextButton(
            onClick = {
                onDismiss()
                button.onClick()
            },
            colors = if (button.destructive) {
                ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)
            } else {
                ButtonDefaults.textButtonColors()
            },
        ) {
            Text(button.label)
        }
    }

    AlertDialog(
        onDismissRequest = {
            onDismiss()
            alert.onDismiss()
        },
        confirmButton = { AlertButton(alert.confirm) },
        dismissButton = (
            alert.dismiss ?: alert.copyText?.let { text ->
                AlertState.Button(stringResource(R.string.copy)) {
                    AppClipboard.write(text)
                    alert.onDismiss()
                }
            }
            )?.let { { AlertButton(it) } },
        title = { Text(alert.title) },
        text = if (alert.message.isEmpty()) null else ({ Text(alert.message, Modifier.verticalScroll(rememberScrollState())) }),
    )
}

@Composable
fun ErrorAlertHost() {
    AlertHost(ErrorAlert.alert, ErrorAlert::dismiss)
}
