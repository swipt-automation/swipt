import com.google.protobuf.gradle.proto
import java.io.ByteArrayInputStream
import java.io.FileInputStream
import java.util.Base64
import java.util.Properties
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
    id("com.diffplug.spotless")
}

fun getProps(propName: String): String {
    val propsInEnv = System.getenv("LOCAL_PROPERTIES")
    if (propsInEnv != null) {
        val props = Properties()
        props.load(ByteArrayInputStream(Base64.getDecoder().decode(propsInEnv)))
        val value = props.getProperty(propName)
        if (value != null) {
            return value
        }
    }
    val propsFile = rootProject.file("local.properties")
    if (propsFile.exists()) {
        val props = Properties()
        props.load(FileInputStream(propsFile))
        val value = props.getProperty(propName)
        if (value != null) {
            return value
        }
    }
    return ""
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
        targetSdk = 37
        versionCode = providers.gradleProperty("versionCode").orNull?.toInt() ?: 1
        versionName = providers.gradleProperty("versionName").orNull ?: "0.1.0"
        base.archivesName.set("Swipt")
        buildConfigField("String", "CHANNEL", "\"${providers.gradleProperty("swiptChannel").getOrElse("nightly")}\"")
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    signingConfigs {
        create("release") {
            storeFile = file("release.keystore")
            storePassword = getProps("KEYSTORE_PASS")
            keyAlias = getProps("ALIAS_NAME")
            keyPassword = getProps("ALIAS_PASS")
        }
    }

    buildTypes {
        debug {
            if (getProps("KEYSTORE_PASS").isNotEmpty()) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
            vcsInfo.include = false
        }
    }

    dependenciesInfo {
        includeInApk = false
        includeInBundle = false
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

    packaging {
        jniLibs {
            useLegacyPackaging = false
        }
    }

    lint {
        fatal += "NewApi"
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

val deviceStubsJar = tasks.register<Jar>("deviceStubsJar") {
    from(compileDeviceStubs.flatMap { it.destinationDirectory })
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device"))
    archiveFileName.set("device-stubs.jar")
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
    libraries.from(files(deviceStubsJar) + androidJar + files(libswiptClasses.map { it.destinationDir.resolve("classes.jar") }).builtBy(libswiptClasses) + kotlinStdlib)
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device/classes"))
}

val deviceJar = tasks.register<Jar>("deviceJar") {
    from(compileDevice.flatMap { it.destinationDirectory })
    destinationDirectory.set(layout.buildDirectory.dir("intermediates/device"))
    archiveFileName.set("device.jar")
}

dependencies {
    compileOnly(files(deviceStubsJar))
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

spotless {
    kotlin {
        target("src/**/*.kt", "../device/src/**/*.kt")
        ktlint("1.8.0")
            .editorConfigOverride(
                mapOf(
                    "ktlint_standard_backing-property-naming" to "disabled",
                    "ktlint_standard_blank-line-before-declaration" to "disabled",
                    "ktlint_standard_blank-line-between-when-conditions" to "disabled",
                    "ktlint_standard_filename" to "disabled",
                    "ktlint_standard_max-line-length" to "disabled",
                    "ktlint_standard_property-naming" to "disabled",
                    "ktlint_standard_function-naming" to "disabled",
                ),
            )
    }
}
