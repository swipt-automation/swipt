package io.nekohasekai.swipt.core

import android.content.Context
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

object CoreFailures {
    fun start(context: Context) {
        MainScope().launch {
            Core.state.map { (it as? Core.State.Failed)?.message }.distinctUntilChanged().collect { message ->
                if (message != null) ErrorAlert.report(AlertState.failure(context.getString(R.string.couldnt_start), message))
            }
        }
    }
}
