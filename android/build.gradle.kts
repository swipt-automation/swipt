// The Android Gradle plugin and Kotlin follow React Native, whose Gradle plugin is built with them:
// react-native 0.88.1, gradle/libs.versions.toml.
buildscript {
    repositories {
        google()
        mavenCentral()
    }
    dependencies {
        classpath("com.android.tools.build:gradle")
        classpath("com.facebook.react:react-native-gradle-plugin")
        classpath("org.jetbrains.kotlin:kotlin-gradle-plugin")
        classpath("org.jetbrains.kotlin:compose-compiler-gradle-plugin:2.2.0")
        classpath("org.jetbrains.kotlin:kotlin-serialization:2.2.0")
        classpath("com.google.devtools.ksp:symbol-processing-gradle-plugin:2.2.0-2.0.2")
        classpath("com.google.protobuf:protobuf-gradle-plugin:0.10.1")
    }
}

allprojects {
    repositories {
        google()
        mavenCentral()
        maven("https://jitpack.io")
    }
}

apply(plugin = "expo-root-project")
apply(plugin = "com.facebook.react.rootproject")
