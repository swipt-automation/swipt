package io.nekohasekai.swipt.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import io.nekohasekai.swipt.R
import kotlinx.coroutines.delay

@Composable
fun CopyableText(text: String, copyText: String = text, modifier: Modifier = Modifier, style: TextStyle = MaterialTheme.typography.bodyMedium) {
    var isCopied by remember { mutableStateOf(false) }
    LaunchedEffect(isCopied) {
        if (!isCopied) return@LaunchedEffect
        delay(1500)
        isCopied = false
    }
    Row(
        modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainerHighest, MaterialTheme.shapes.small)
            .padding(start = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SelectionContainer(
            Modifier
                .weight(1f)
                .padding(vertical = 12.dp),
        ) {
            Text(text, style = style)
        }
        IconButton(onClick = {
            AppClipboard.write(copyText)
            isCopied = true
        }) {
            Icon(
                if (isCopied) Icons.Filled.Check else Icons.Outlined.ContentCopy,
                contentDescription = stringResource(R.string.copy),
                modifier = Modifier.size(20.dp),
            )
        }
    }
}
