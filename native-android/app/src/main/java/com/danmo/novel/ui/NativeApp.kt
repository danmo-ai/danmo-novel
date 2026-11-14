package com.danmo.novel.ui

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.danmo.novel.data.pathSegment

private const val ShelfRoute = "shelf"
private const val RunsRoute = "runs"
private const val SettingsRoute = "settings"

private data class TopLevelDestination(val route: String, val label: String)

private val topLevelDestinations = listOf(
    TopLevelDestination(ShelfRoute, "书架"),
    TopLevelDestination(RunsRoute, "运行记录"),
    TopLevelDestination(SettingsRoute, "设置"),
)

@Composable
fun NativeApp(viewModel: LibraryViewModel = viewModel()) {
    val navController = rememberNavController()
    val ready by viewModel.ready.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val route = backStackEntry?.destination?.route
    val showNavigation = route in setOf(ShelfRoute, RunsRoute, SettingsRoute)

    Scaffold(
        bottomBar = {
            if (showNavigation) {
                NavigationBar {
                    topLevelDestinations.forEach { destination ->
                        NavigationBarItem(
                            selected = route == destination.route,
                            onClick = {
                                navController.navigate(destination.route) {
                                    popUpTo(navController.graph.findStartDestination().id) { saveState = true }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            },
                            icon = {
                                val icon = when (destination.route) {
                                    ShelfRoute -> Icons.AutoMirrored.Filled.List
                                    RunsRoute -> Icons.Default.History
                                    else -> Icons.Default.Settings
                                }
                                Icon(icon, contentDescription = destination.label)
                            },
                            label = { Text(destination.label) },
                        )
                    }
                }
            }
        },
    ) { contentPadding ->
        NavHost(
            navController = navController,
            startDestination = ShelfRoute,
            modifier = Modifier.fillMaxSize().padding(contentPadding),
            enterTransition = { slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.Start, tween(220)) },
            exitTransition = { slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.Start, tween(220)) },
            popEnterTransition = { slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.End, tween(220)) },
            popExitTransition = { slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.End, tween(220)) },
        ) {
            composable(ShelfRoute) {
                ShelfScreen(
                    viewModel = viewModel,
                    onOpenBook = { bookId -> navController.navigate("book/${bookId.pathSegment()}") },
                )
            }
            composable(RunsRoute) { RunsScreen(viewModel) }
            composable(SettingsRoute) {
                SettingsScreen(
                    viewModel = viewModel,
                    onAddProvider = { navController.navigate("provider/new") },
                    onEditProvider = { id -> navController.navigate("provider/${id.pathSegment()}") },
                )
            }
            composable("book/{bookId}") { entry ->
                val bookId = entry.arguments?.getString("bookId").orEmpty()
                BookScreen(
                    viewModel = viewModel,
                    bookId = bookId,
                    onBack = { navController.popBackStack() },
                    onOpenUnit = { unitId ->
                        navController.navigate("book/${bookId.pathSegment()}/unit/${unitId.pathSegment()}")
                    },
                )
            }
            composable("book/{bookId}/unit/{unitId}") { entry ->
                UnitReaderScreen(
                    viewModel = viewModel,
                    bookId = entry.arguments?.getString("bookId").orEmpty(),
                    unitId = entry.arguments?.getString("unitId").orEmpty(),
                    onBack = { navController.popBackStack() },
                )
            }
            composable("provider/new") {
                ProviderEditorScreen(
                    viewModel = viewModel,
                    providerId = null,
                    onBack = { navController.popBackStack() },
                )
            }
            composable("provider/{providerId}") { entry ->
                ProviderEditorScreen(
                    viewModel = viewModel,
                    providerId = entry.arguments?.getString("providerId"),
                    onBack = { navController.popBackStack() },
                )
            }
        }
    }

    if (!ready) LoadingDialog("正在准备本地工作区…")
    error?.let { message ->
        AlertDialog(
            onDismissRequest = viewModel::dismissError,
            confirmButton = {
                TextButton(onClick = {
                    viewModel.dismissError()
                    if (!ready) viewModel.start()
                }) { Text(if (ready) "好" else "重试") }
            },
            title = { Text(if (ready) "操作未完成" else "本地内核未能启动") },
            text = { Text(message) },
        )
    }
}
