package io.nekohasekai.swipt.settings

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.LockOpen
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.core.Core
import kotlin.math.hypot
import kotlinx.coroutines.launch

@Composable
internal fun AutoUnlockSetupScreen(onSaved: () -> Unit, navigationIcon: @Composable () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val settings by Core.settings.collectAsStateWithLifecycle()
    val initial = settings
    val initialType = initial?.androidUnlockType
    val initialCredential = initial?.unlockPasscode.orEmpty()

    var selectedType by rememberSaveable(initialType) { mutableStateOf(initialType) }
    var passcodeInput by rememberSaveable(initialType, initialCredential) {
        mutableStateOf(if (initialType == AndroidUnlockType.Pin || initialType == AndroidUnlockType.Password) initialCredential else "")
    }
    var patternInput by rememberSaveable(initialType, initialCredential) {
        mutableStateOf("")
    }
    var hasSavedPattern by rememberSaveable(initialType, initialCredential) {
        mutableStateOf(initialType == AndroidUnlockType.Pattern && initialCredential.isNotEmpty())
    }
    var saving by remember { mutableStateOf(false) }

    val activeCredential = when (selectedType) {
        AndroidUnlockType.None -> ""
        AndroidUnlockType.Pattern -> if (patternInput.isEmpty() && hasSavedPattern) initialCredential else patternInput
        AndroidUnlockType.Pin, AndroidUnlockType.Password -> passcodeInput
        null -> ""
    }
    val validationError = selectedType?.problem(activeCredential, context)
    val canSave = !saving && selectedType != null && validationError == null

    fun save() {
        val type = selectedType ?: return
        if (type.problem(activeCredential, context) != null || saving) return
        saving = true
        scope.launch {
            val saved = AutoUnlock.update {
                unlockType = type.proto
                unlockPasscode = if (type == AndroidUnlockType.None) "" else activeCredential
                autoUnlock = true
            }
            saving = false
            if (saved) onSaved()
        }
    }

    PageScaffold(
        title = stringResource(R.string.settings_screen_lock),
        navigationIcon = navigationIcon,
        actions = {
            TextButton(onClick = ::save, enabled = canSave) {
                Text(stringResource(R.string.save))
            }
        },
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 8.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(
                Modifier
                    .widthIn(max = 520.dp)
                    .fillMaxWidth(),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Icon(
                    Icons.Outlined.LockOpen,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(44.dp),
                )
                Spacer(Modifier.height(10.dp))
                Text(
                    stringResource(R.string.settings_auto_unlock_footer),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(horizontal = 16.dp),
                )
                Spacer(Modifier.height(20.dp))
                Column(
                    Modifier
                        .fillMaxWidth()
                        .selectableGroup(),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    for (type in AndroidUnlockType.entries) {
                        val isSelected = type == selectedType
                        LockTypeCard(
                            type = type,
                            isSelected = isSelected,
                            onSelect = {
                                if (selectedType != type) {
                                    selectedType = type
                                    if (type == AndroidUnlockType.Pin || type == AndroidUnlockType.Password) {
                                        passcodeInput = if (type == initialType) initialCredential else ""
                                    } else if (type == AndroidUnlockType.Pattern) {
                                        patternInput = ""
                                        hasSavedPattern = initialType == AndroidUnlockType.Pattern && initialCredential.isNotEmpty()
                                    }
                                }
                            },
                        ) {
                            when (type) {
                                AndroidUnlockType.Pattern -> {
                                    LockPatternSection(
                                        pattern = patternInput,
                                        savedDotCount = if (hasSavedPattern) initialCredential.length else 0,
                                        onPatternChange = { next ->
                                            patternInput = next
                                            hasSavedPattern = false
                                        },
                                        onClear = {
                                            patternInput = ""
                                            hasSavedPattern = false
                                        },
                                    )
                                }

                                AndroidUnlockType.Pin, AndroidUnlockType.Password -> {
                                    PasscodeInputSection(
                                        type = type,
                                        value = passcodeInput,
                                        onValueChange = { passcodeInput = it },
                                        error = if (passcodeInput.isNotEmpty()) type.problem(passcodeInput, context) else null,
                                        onDone = ::save,
                                    )
                                }

                                AndroidUnlockType.None -> {}
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun LockTypeCard(
    type: AndroidUnlockType,
    isSelected: Boolean,
    onSelect: () -> Unit,
    expandedContent: @Composable () -> Unit,
) {
    val border = if (isSelected) BorderStroke(2.dp, MaterialTheme.colorScheme.primary) else null
    Surface(
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = border,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column {
            Row(
                Modifier
                    .fillMaxWidth()
                    .selectable(selected = isSelected, role = Role.RadioButton, onClick = onSelect)
                    .padding(12.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(stringResource(type.titleRes), style = MaterialTheme.typography.titleMedium)
                    Text(
                        stringResource(type.subtitleRes),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                RadioButton(selected = isSelected, onClick = null)
            }
            if (isSelected) {
                expandedContent()
            }
        }
    }
}

@Composable
private fun PasscodeInputSection(
    type: AndroidUnlockType,
    value: String,
    onValueChange: (String) -> Unit,
    error: String?,
    onDone: () -> Unit,
) {
    var revealed by rememberSaveable(type) { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(type) { focus.requestFocus() }
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 12.dp, bottom = 12.dp)
            .focusRequester(focus),
        placeholder = { Text(stringResource(R.string.settings_lock_screen_passcode)) },
        visualTransformation = if (revealed) VisualTransformation.None else PasswordVisualTransformation(),
        trailingIcon = {
            IconButton(onClick = { revealed = !revealed }) {
                Icon(
                    if (revealed) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                    contentDescription = stringResource(if (revealed) R.string.hide else R.string.show),
                )
            }
        },
        isError = error != null,
        supportingText = error?.let { { Text(it) } },
        keyboardOptions = KeyboardOptions(
            capitalization = KeyboardCapitalization.None,
            autoCorrectEnabled = false,
            keyboardType = if (type == AndroidUnlockType.Pin) KeyboardType.NumberPassword else KeyboardType.Password,
            imeAction = ImeAction.Done,
        ),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        singleLine = true,
    )
}

@Composable
private fun LockPatternSection(
    pattern: String,
    savedDotCount: Int,
    onPatternChange: (String) -> Unit,
    onClear: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 12.dp, bottom = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        LockPatternPad(
            pattern = pattern,
            onPatternChange = onPatternChange,
            modifier = Modifier.size(228.dp),
        )
        Spacer(Modifier.height(6.dp))
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            val statusText = when {
                pattern.isEmpty() && savedDotCount > 0 -> stringResource(R.string.settings_pattern_saved, savedDotCount)
                pattern.isEmpty() -> stringResource(R.string.settings_pattern_draw)
                pattern.length < AutoUnlock.MINIMUM_LENGTH -> stringResource(R.string.settings_pattern_too_short)
                else -> stringResource(R.string.settings_pattern_drawn, pattern.length)
            }
            val statusColor = if (pattern.isNotEmpty() && pattern.length < AutoUnlock.MINIMUM_LENGTH) {
                MaterialTheme.colorScheme.error
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            }
            Text(
                statusText,
                style = MaterialTheme.typography.bodySmall,
                color = statusColor,
                modifier = Modifier.weight(1f),
            )
            if (pattern.isNotEmpty() || savedDotCount > 0) {
                TextButton(onClick = onClear) {
                    Text(stringResource(R.string.settings_pattern_clear))
                }
            }
        }
    }
}

@Composable
private fun LockPatternPad(
    pattern: String,
    onPatternChange: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val primary = MaterialTheme.colorScheme.primary
    val dotColor = MaterialTheme.colorScheme.onSurfaceVariant
    val activeHalo = MaterialTheme.colorScheme.primary.copy(alpha = 0.2f)
    var dragPosition by remember { mutableStateOf<Offset?>(null) }

    Box(
        modifier.pointerInput(Unit) {
            fun centerOf(cell: Int): Offset {
                val cellWidth = size.width / 3f
                val cellHeight = size.height / 3f
                val row = cell / 3
                val col = cell % 3
                return Offset((col + 0.5f) * cellWidth, (row + 0.5f) * cellHeight)
            }

            fun hitCell(position: Offset): Int? {
                val radius = minOf(size.width, size.height) / 6f * 0.72f
                for (cell in 0 until 9) {
                    val center = centerOf(cell)
                    if (hypot(position.x - center.x, position.y - center.y) <= radius) {
                        return cell
                    }
                }
                return null
            }

            fun appendDot(path: MutableList<Int>, dot: Int) {
                if (dot in path) return
                val last = path.lastOrNull()
                if (last != null) {
                    val r1 = last / 3
                    val c1 = last % 3
                    val r2 = dot / 3
                    val c2 = dot % 3
                    if ((r1 + r2) % 2 == 0 && (c1 + c2) % 2 == 0) {
                        val middle = ((r1 + r2) / 2) * 3 + ((c1 + c2) / 2)
                        if (middle != last && middle != dot && middle !in path) {
                            path.add(middle)
                        }
                    }
                }
                path.add(dot)
            }

            awaitEachGesture {
                val down = awaitFirstDown(requireUnconsumed = false)
                down.consume()
                val active = mutableListOf<Int>()
                hitCell(down.position)?.let { appendDot(active, it) }
                dragPosition = down.position
                onPatternChange(active.joinToString("") { (it + 1).toString() })

                while (true) {
                    val event = awaitPointerEvent()
                    val change = event.changes.firstOrNull { it.id == down.id } ?: event.changes.firstOrNull() ?: break
                    change.consume()
                    if (!change.pressed) {
                        break
                    }
                    dragPosition = change.position
                    val before = active.size
                    hitCell(change.position)?.let { appendDot(active, it) }
                    if (active.size != before) {
                        onPatternChange(active.joinToString("") { (it + 1).toString() })
                    }
                }
                dragPosition = null
            }
        },
    ) {
        Canvas(Modifier.matchParentSize()) {
            val selected = pattern.mapNotNull { ch -> (ch - '1').takeIf { it in 0..8 } }
            val cellWidth = size.width / 3f
            val cellHeight = size.height / 3f
            fun centerOf(cell: Int): Offset = Offset(((cell % 3) + 0.5f) * cellWidth, ((cell / 3) + 0.5f) * cellHeight)

            if (selected.isNotEmpty()) {
                val path = Path().apply {
                    val first = centerOf(selected.first())
                    moveTo(first.x, first.y)
                    for (index in 1 until selected.size) {
                        val next = centerOf(selected[index])
                        lineTo(next.x, next.y)
                    }
                    dragPosition?.let { lineTo(it.x, it.y) }
                }
                drawPath(
                    path = path,
                    color = primary.copy(alpha = 0.7f),
                    style = Stroke(width = 4.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round),
                )
            }

            val ringRadius = 15.dp.toPx()
            val dotRadius = 5.dp.toPx()
            val selectedDotRadius = 7.dp.toPx()
            for (cell in 0 until 9) {
                val center = centerOf(cell)
                val isHit = cell in selected
                if (isHit) {
                    drawCircle(color = activeHalo, radius = ringRadius, center = center)
                    drawCircle(color = primary, radius = selectedDotRadius, center = center)
                } else {
                    drawCircle(color = dotColor.copy(alpha = 0.45f), radius = dotRadius, center = center)
                }
            }
        }
    }
}
