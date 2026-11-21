package io.nekohasekai.swipt.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.exclude
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScaffoldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.automation.swipletsDestinations
import io.nekohasekai.swipt.developer.API
import io.nekohasekai.swipt.developer.developerDestinations
import io.nekohasekai.swipt.privilege.Privilege
import io.nekohasekai.swipt.privilege.SetupView
import io.nekohasekai.swipt.privilege.isSetupNeeded
import io.nekohasekai.swipt.settings.settingsDestinations

@Composable
fun HomeView() {
    val privilege by Privilege.state.collectAsStateWithLifecycle()
    var isSettingUp by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(privilege) {
        if (privilege.isSetupNeeded) isSettingUp = true else if (privilege == Privilege.State.Connected) isSettingUp = false
    }
    if (privilege.isSetupNeeded || isSettingUp && privilege != Privilege.State.Connected) {
        SetupView()
    } else {
        Tabs()
    }
}

@Composable
private fun Tabs() {
    val navController = rememberNavController()
    val navBackStackEntry by navController.currentBackStackEntryAsState()
    val currentTab = navBackStackEntry?.destination?.route?.substringBefore('/')
    val topBarController = remember { TopBarController(mutableStateOf(emptyList())) }
    val api by API.state.collectAsStateWithLifecycle()

    val isPublished = api?.isPublished == true
    val published = stringResource(R.string.developer_published)
    val tabState: (HomeTab) -> Modifier = { tab ->
        if (tab == HomeTab.Developer && isPublished) Modifier.semantics { stateDescription = published } else Modifier
    }

    @Composable
    fun TabIcon(tab: HomeTab, selected: Boolean) {
        BadgedBox(badge = { if (tab == HomeTab.Developer && isPublished) Badge(containerColor = MaterialTheme.colorScheme.positive) }) {
            Icon(if (selected) tab.selectedIcon else tab.icon, contentDescription = null)
        }
    }

    val topBarContent: @Composable () -> Unit = { topBarController.Content() }
    val scaffoldContent: @Composable (PaddingValues) -> Unit = { paddingValues ->
        CompositionLocalProvider(LocalScaffoldPadding provides paddingValues) {
            Box(Modifier.fillMaxSize().consumeWindowInsets(paddingValues)) {
                NavHost(
                    navController = navController,
                    startDestination = HomeTab.Swiplets.route,
                    modifier = Modifier.fillMaxSize(),
                    enterTransition = enterPage,
                    exitTransition = exitPage,
                    popEnterTransition = popEnterPage,
                    popExitTransition = popExitPage,
                ) {
                    swipletsDestinations(navController)
                    developerDestinations(navController)
                    settingsDestinations(navController)
                }
            }
        }
    }

    CompositionLocalProvider(LocalTopBarController provides topBarController) {
        if (LocalWindowInfo.current.containerDpSize.width >= 600.dp) {
            val railInsets = WindowInsets.safeDrawing.only(WindowInsetsSides.Start)
            Row(Modifier.fillMaxSize()) {
                Surface(tonalElevation = 1.dp) {
                    NavigationRail(Modifier.fillMaxHeight()) {
                        for (tab in HomeTab.entries) {
                            val selected = currentTab == tab.route
                            NavigationRailItem(
                                selected = selected,
                                onClick = { navController.select(tab) },
                                icon = { TabIcon(tab, selected) },
                                label = { Text(stringResource(tab.title)) },
                                modifier = tabState(tab),
                            )
                        }
                    }
                }
                Scaffold(
                    modifier = Modifier
                        .weight(1f)
                        .consumeWindowInsets(railInsets),
                    contentWindowInsets = ScaffoldDefaults.contentWindowInsets.exclude(railInsets),
                    topBar = topBarContent,
                    content = scaffoldContent,
                )
            }
        } else {
            Scaffold(
                modifier = Modifier.fillMaxSize(),
                topBar = topBarContent,
                bottomBar = {
                    NavigationBar {
                        for (tab in HomeTab.entries) {
                            val selected = currentTab == tab.route
                            NavigationBarItem(
                                selected = selected,
                                onClick = { navController.select(tab) },
                                icon = { TabIcon(tab, selected) },
                                label = { Text(stringResource(tab.title)) },
                                modifier = tabState(tab),
                            )
                        }
                    }
                },
                content = scaffoldContent,
            )
        }
    }
}
