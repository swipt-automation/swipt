package io.nekohasekai.swipt.developer

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import androidx.navigation.NavController
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.BackButton
import io.nekohasekai.swipt.app.HomeTab
import io.nekohasekai.swipt.app.ListGroup
import io.nekohasekai.swipt.app.NavigationRow
import io.nekohasekai.swipt.app.PageScaffold
import io.nekohasekai.swipt.app.SectionHeader

private val LOCAL_NETWORK = "${HomeTab.Developer.route}/localNetwork"
private val CLOUDFLARE = "${HomeTab.Developer.route}/cloudflare"
private val CLOUDFLARE_MODE = "${HomeTab.Developer.route}/cloudflare/mode"
private val TAILSCALE = "${HomeTab.Developer.route}/tailscale"

fun NavGraphBuilder.developerDestinations(navController: NavController) {
    composable(HomeTab.Developer.route) {
        DeveloperScreen(navController)
    }
    composable(LOCAL_NETWORK) {
        LocalNetworkScreen { BackButton { navController.navigateUp() } }
    }
    composable(CLOUDFLARE) {
        CloudflareScreen(onMode = { navController.navigate(CLOUDFLARE_MODE) }) { BackButton { navController.navigateUp() } }
    }
    composable(CLOUDFLARE_MODE) {
        CloudflareModeScreen(onSaved = { navController.navigateUp() }) { BackButton { navController.navigateUp() } }
    }
    composable(TAILSCALE) {
        TailscaleScreen { BackButton { navController.navigateUp() } }
    }
}

@Composable
private fun DeveloperScreen(navController: NavController) {
    PageScaffold(title = stringResource(R.string.developer), navigationIcon = {}) {
        SectionHeader("API")
        ListGroup {
            NavigationRow(
                stringResource(R.string.developer_local_network),
                { navController.navigate(LOCAL_NETWORK) },
                Icons.Outlined.Wifi,
                stringResource(R.string.developer_local_network_description),
            )
            NavigationRow(
                "Cloudflare",
                { navController.navigate(CLOUDFLARE) },
                Icons.Outlined.Cloud,
                stringResource(R.string.developer_cloudflare_description),
            )
            NavigationRow(
                "Tailscale",
                { navController.navigate(TAILSCALE) },
                Icons.Outlined.Hub,
                stringResource(R.string.developer_tailscale_description),
            )
        }
    }
}
