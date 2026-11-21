package io.nekohasekai.swipt.privilege

import androidx.annotation.StringRes
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.ListItem
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionHeader
import io.nekohasekai.swipt.app.groupItemColors

@get:StringRes
val Privilege.Mode.title: Int
    get() = when (this) {
        Privilege.Mode.Shizuku -> R.string.privilege_shizuku
        Privilege.Mode.Root -> R.string.privilege_root
    }

@Composable
fun PrivilegeModes() {
    val mode by Privilege.mode.collectAsStateWithLifecycle()
    SectionHeader(stringResource(R.string.privilege_run_as))
    ListGroup(Modifier.selectableGroup()) {
        for (option in Privilege.Mode.entries) {
            ListItem(
                headlineContent = { Text(stringResource(option.title)) },
                leadingContent = { RadioButton(selected = option == mode, onClick = null) },
                colors = groupItemColors(),
                modifier = Modifier.selectable(selected = option == mode, role = Role.RadioButton) { Privilege.setMode(option) },
            )
        }
    }
}

@Composable
fun PrivilegesScreen(navigationIcon: @Composable () -> Unit) {
    PageScaffold(title = stringResource(R.string.privilege_title), navigationIcon = navigationIcon) {
        PrivilegeModes()
    }
}
