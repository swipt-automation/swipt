import com.google.protobuf.gradle.proto
import org.jetbrains.kotlin.gradle.dsl.ExplicitApiMode
import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.plugin.KotlinApiPlugin
import org.jetbrains.kotlin.gradle.plugin.getKotlinPluginVersion

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
    id("com.google.devtools.ksp")
    id("com.facebook.react")
    id("com.google.protobuf")
}

val grpcVersion = "1.85.1"
val grpcKotlinVersion = "1.5.0"
val protobufVersion = "4.36.2"

val runtime = file("../../internal/automation/runtime")

react {
    root = runtime
    reactNativeDir = file("../../node_modules/react-native")
    codegenDir = file("../../node_modules/@react-native/codegen")
    debuggableVariants = listOf("debug", "release")
    autolinkLibrariesWithApp()
}

android {
    namespace = "io.nekohasekai.swipt"
    compileSdk = 37
    ndkVersion = "27.1.12297006"

    defaultConfig {
        applicationId = "io.nekohasekai.swipt"
        minSdk = 34
        targetSdk = 36
        versionCode = 1
        versionName = "0.0.1"
        buildConfigField("String", "CHANNEL", "\"${providers.gradleProperty("swiptChannel").getOrElse("nightly")}\"")
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    androidResources {
        generateLocaleConfig = true
    }

    buildFeatures {
        aidl = true
        buildConfig = true
        compose = true
    }

    sourceSets["main"].assets.srcDir("../../build/runtime/android")
    sourceSets["main"].res.srcDir("../../build/runtime/android-res")
    sourceSets["main"].proto { srcDir("../../internal/android") }
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:$protobufVersion"
    }
    plugins {
        create("grpc") {
            artifact = "io.grpc:protoc-gen-grpc-java:$grpcVersion"
        }
        create("grpckt") {
            artifact = "io.grpc:protoc-gen-grpc-kotlin:$grpcKotlinVersion:jdk8@jar"
        }
    }
    generateProtoTasks {
        all().forEach { task ->
            task.builtins {
                create("java") { option("lite") }
                create("kotlin") { option("lite") }
            }
            task.plugins {
                create("grpc") { option("lite") }
                create("grpckt") { option("lite") }
            }
        }
    }
}

ksp {
    arg("room.generateKotlin", "true")
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

val androidJar = files(provider { android.bootClasspath })

val libswiptClasses = tasks.register<Copy>("libswiptClasses") {
    from(zipTree("libs/libswipt.aar")) { include("classes.jar") }
    into(layout.buildDirectory.dir("intermediates/libswipt"))
}

val compileDeviceStubs = tasks.register<JavaCompile>("compileDeviceStubs") {
    source = fileTree("../device/stubs")
    classpath = androidJar
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device/stubs"))
    options.release.set(17)
    options.compilerArgs.add("-nowarn")
}

val kotlinApi = plugins.apply(KotlinApiPlugin::class.java)
val kotlinStdlib = configurations.detachedConfiguration(dependencies.create("org.jetbrains.kotlin:kotlin-stdlib:${getKotlinPluginVersion()}"))

val compileDevice = kotlinApi.registerKotlinJvmCompileTask(
    "compileDevice",
    kotlinApi.createCompilerJvmOptions().apply {
        jvmTarget.set(JvmTarget.JVM_17)
        moduleName.set("device")
        noJdk.set(true)
    },
    provider { ExplicitApiMode.Disabled },
)
compileDevice.configure {
    multiPlatformEnabled.set(false)
    useModuleDetection.set(false)
    source(fileTree("../device/src"))
    libraries.from(files(compileDeviceStubs) + androidJar + files(libswiptClasses.map { it.destinationDir.resolve("classes.jar") }).builtBy(libswiptClasses) + kotlinStdlib)
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device/classes"))
}

val deviceJar = tasks.register<Jar>("deviceJar") {
    from(compileDevice.flatMap { it.destinationDirectory })
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device"))
    archiveFileName.set("device.jar")
}

dependencies {
    implementation(files(deviceJar))
    implementation(files("libs/libswipt.aar"))
    val shizukuVersion = "13.1.5"
    implementation("dev.rikka.shizuku:api:$shizukuVersion")
    implementation("dev.rikka.shizuku:provider:$shizukuVersion")
    val libsuVersion = "6.0.0"
    implementation("com.github.topjohnwu.libsu:core:$libsuVersion")
    implementation("com.github.topjohnwu.libsu:service:$libsuVersion")
    implementation("com.facebook.react:react-android")
    implementation("com.facebook.react:hermes-android")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.activity:activity-compose:1.12.2")
    implementation("androidx.compose.ui:ui-android:1.12.1")
    implementation("androidx.compose.foundation:foundation-android:1.12.1")
    implementation("androidx.compose.material3:material3-android:1.5.0-beta01")
    implementation("androidx.compose.material:material-icons-extended-android:1.7.8")
    implementation("androidx.lifecycle:lifecycle-runtime-compose-android:2.10.0")
    implementation("androidx.lifecycle:lifecycle-process:2.10.0")
    implementation("androidx.navigation:navigation-compose-android:2.9.8")
    implementation("sh.calvin.reorderable:reorderable:3.1.0")
    val roomVersion = "2.8.4"
    implementation("androidx.room:room-runtime:$roomVersion")
    implementation("androidx.room:room-ktx:$roomVersion")
    ksp("androidx.room:room-compiler:$roomVersion")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
    implementation("io.grpc:grpc-okhttp:$grpcVersion")
    implementation("io.grpc:grpc-protobuf-lite:$grpcVersion")
    implementation("io.grpc:grpc-stub:$grpcVersion")
    implementation("io.grpc:grpc-kotlin-stub:$grpcKotlinVersion")
    implementation("com.google.protobuf:protobuf-kotlin-lite:$protobufVersion")
}
