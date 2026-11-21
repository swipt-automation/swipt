pluginManagement {
    val runtime = file("../internal/automation/runtime")
    fun resolve(request: String): File = File(
        providers.exec {
            workingDir(runtime)
            commandLine("node", "--print", request)
        }.standardOutput.asText.get().trim(),
    ).parentFile
    includeBuild(resolve("require.resolve('@react-native/gradle-plugin/package.json', { paths: [require.resolve('react-native/package.json')] })").path)
    includeBuild(File(resolve("require.resolve('expo-modules-autolinking/package.json', { paths: [require.resolve('expo/package.json')] })"), "android/expo-gradle-plugin").path)
}

plugins {
    id("com.facebook.react.settings")
    id("expo-autolinking-settings")
}

expoAutolinking.projectRoot = file("../internal/automation/runtime")
expoAutolinking.exclude = listOf("@expo/dom-webview", "@expo/log-box", "expo-file-system", "expo-maps")
extensions.configure<com.facebook.react.ReactSettingsExtension> {
    autolinkLibrariesFromCommand(expoAutolinking.rnConfigCommand)
}
expoAutolinking.useExpoModules()
expoAutolinking.useExpoVersionCatalog()
includeBuild(expoAutolinking.reactNativeGradlePlugin)

rootProject.name = "swipt"
include(":app")
