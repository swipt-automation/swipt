package io.nekohasekai.swipt.app

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.privilege.Privilege

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) handleSetup(intent)
        addOnNewIntentListener(::handleSetup)
        setContent {
            AppTheme {
                HomeView()
                ErrorAlertHost()
            }
        }
    }

    private fun handleSetup(intent: Intent?) {
        if (intent?.getStringExtra(EXTRA_SETUP) == null) return
        intent.removeExtra(EXTRA_SETUP)
        val preferredMode = intent.getStringExtra(EXTRA_PRIVILEGE)?.let { name ->
            Privilege.Mode.entries.firstOrNull { it.name == name }
        }
        intent.removeExtra(EXTRA_PRIVILEGE)
        Core.applySetup(preferredMode)
    }

    companion object {
        private const val EXTRA_SETUP = "io.nekohasekai.swipt.SETUP"
        private const val EXTRA_PRIVILEGE = "io.nekohasekai.swipt.PRIVILEGE"

        fun reveal(trampoline: Activity) {
            if (trampoline.isTaskRoot) trampoline.startActivity(Intent(trampoline, MainActivity::class.java))
        }
    }
}
