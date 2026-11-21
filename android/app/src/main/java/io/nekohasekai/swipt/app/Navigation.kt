package io.nekohasekai.swipt.app

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.AnimatedContentTransitionScope.SlideDirection
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.navigation.NavBackStackEntry

private typealias Transition = AnimatedContentTransitionScope<NavBackStackEntry>

private val NavBackStackEntry.tab: String? get() = destination.route?.substringBefore('/')

private val Transition.isWithinTab: Boolean get() = initialState.tab == targetState.tab

internal val enterPage: Transition.() -> EnterTransition = {
    if (isWithinTab) slideIntoContainer(SlideDirection.Start, tween(300)) else fadeIn(tween(250))
}

internal val exitPage: Transition.() -> ExitTransition = {
    if (isWithinTab) slideOutOfContainer(SlideDirection.Start, tween(300)) else fadeOut(tween(250))
}

internal val popEnterPage: Transition.() -> EnterTransition = {
    if (isWithinTab) slideIntoContainer(SlideDirection.End, tween(300)) else fadeIn(tween(250))
}

internal val popExitPage: Transition.() -> ExitTransition = {
    if (isWithinTab) slideOutOfContainer(SlideDirection.End, tween(300)) else fadeOut(tween(250))
}
