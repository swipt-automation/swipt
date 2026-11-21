package io.nekohasekai.swipt.app

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.model.Settings

enum class HomeTab(val route: String, @StringRes val title: Int, val selectedIcon: ImageVector, val icon: ImageVector) {
    Swiplets("swiplets", R.string.swiplets, Icons.Filled.AutoAwesome, Icons.Outlined.AutoAwesome),
    Developer("developer", R.string.developer, Icons.Filled.Terminal, Icons.Outlined.Terminal),
    Settings("settings", R.string.settings, Icons.Filled.Settings, Icons.Outlined.Settings),
}

fun NavHostController.select(tab: HomeTab) {
    val route = currentDestination?.route
    if (route?.substringBefore('/') == tab.route) {
        if (route != tab.route) popBackStack(tab.route, inclusive = false)
        return
    }
    navigate(tab.route) {
        popUpTo(graph.findStartDestination().id) {
            saveState = true
        }
        launchSingleTop = true
        restoreState = true
    }
}
