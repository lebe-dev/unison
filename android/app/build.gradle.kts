import org.gradle.api.GradleException
import java.io.File

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.ktlint)
    alias(libs.plugins.detekt)
}

/** Value of the environment first and of the repository .env file second, empty when absent. */
fun buildValue(name: String): String {
    val fromEnvironment = System.getenv(name)?.trim()
    if (!fromEnvironment.isNullOrEmpty()) {
        return fromEnvironment
    }
    val file = rootProject.file("../.env")
    if (!file.isFile) {
        return ""
    }
    return file
        .readLines()
        .firstNotNullOfOrNull { line ->
            val text = line.trim()
            if (text.isEmpty() || text.startsWith("#")) {
                return@firstNotNullOfOrNull null
            }
            val separator = text.indexOf('=')
            if (separator <= 0 || text.substring(0, separator).trim() != name) {
                return@firstNotNullOfOrNull null
            }
            text
                .substring(separator + 1)
                .trim()
                .removeSurrounding("\"")
                .removeSurrounding("'")
                .ifEmpty { null }
        }.orEmpty()
}

/** BuildConfig string literal. */
fun quoted(value: String): String = "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\""

/** Absolute path, or a path relative to the repository root. */
fun repositoryFile(path: String): File {
    val file = File(path)
    return if (file.isAbsolute) file else rootProject.file("../$path")
}

/** Numeric versionCode derived from semver (major * 10000 + minor * 100 + patch). */
fun computeVersionCode(version: String): Int {
    val override = buildValue("UNISON_ANDROID_VERSION_CODE")
    if (override.isNotEmpty()) {
        val parsed =
            override.toIntOrNull()
                ?: error("UNISON_ANDROID_VERSION_CODE must be a positive integer, got '$override'")
        require(parsed > 0) { "UNISON_ANDROID_VERSION_CODE must be > 0, got $parsed" }
        return parsed
    }
    val match =
        Regex("""^(\d+)\.(\d+)\.(\d+)""").find(version)
            ?: error("Cannot derive Android versionCode from version '$version'")
    val major = match.groupValues[1].toInt()
    val minor = match.groupValues[2].toInt()
    val patch = match.groupValues[3].toInt()
    return major * 10_000 + minor * 100 + patch
}

/** Version of the build, from UNISON_VERSION when it is set. */
val appVersion =
    System.getenv("UNISON_VERSION")?.trim()?.takeIf { it.isNotEmpty() }
        ?: rootProject.file("../VERSION").readText().trim()
val appVersionCode = computeVersionCode(appVersion)
val sentryDsn = buildValue("SENTRY_DSN")

/** Keystore of the release APK; release builds fail when it is not configured. */
val releaseKeystore = buildValue("UNISON_ANDROID_KEYSTORE")
val releaseStorePassword = buildValue("UNISON_ANDROID_STORE_PASSWORD")
val releaseKeyAlias = buildValue("UNISON_ANDROID_KEY_ALIAS")
val releaseKeyPassword = buildValue("UNISON_ANDROID_KEY_PASSWORD")
val releaseSigningInputs =
    listOf(releaseKeystore, releaseStorePassword, releaseKeyAlias, releaseKeyPassword)
val hasReleaseSigning = releaseSigningInputs.all { it.isNotEmpty() }
val hasPartialReleaseSigning = releaseSigningInputs.any { it.isNotEmpty() } && !hasReleaseSigning
val releaseKeystoreFile = if (releaseKeystore.isNotEmpty()) repositoryFile(releaseKeystore) else null

if (hasPartialReleaseSigning) {
    error(
        "Incomplete Android release signing configuration: set all of " +
            "UNISON_ANDROID_KEYSTORE, UNISON_ANDROID_STORE_PASSWORD, " +
            "UNISON_ANDROID_KEY_ALIAS, and UNISON_ANDROID_KEY_PASSWORD",
    )
}
if (hasReleaseSigning && releaseKeystoreFile?.isFile != true) {
    error("Android release keystore not found at ${releaseKeystoreFile?.absolutePath}")
}

android {
    namespace = "app.unison"
    compileSdk = 37
    compileSdkMinor = 2

    defaultConfig {
        applicationId = "app.unison"
        minSdk = 29
        targetSdk = 37
        versionCode = appVersionCode
        versionName = appVersion
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a", "x86_64")
        }

        // An empty DSN disables reporting in that build.
        buildConfigField("String", "SENTRY_DSN", quoted(sentryDsn))
        buildConfigField("String", "RELEASE", quoted("unison@$appVersion"))
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = releaseKeystoreFile
                storePassword = releaseStorePassword
                keyAlias = releaseKeyAlias
                keyPassword = releaseKeyPassword
            }
        }
    }

    buildTypes {
        debug {
            buildConfigField("String", "ENVIRONMENT", quoted("debug"))
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (hasReleaseSigning) {
                signingConfig = signingConfigs.getByName("release")
            }
            buildConfigField("String", "ENVIRONMENT", quoted("release"))
        }
    }

    buildFeatures {
        buildConfig = true
        compose = true
    }

    androidResources {
        localeFilters += listOf("en", "ru")
    }

    packaging {
        resources {
            excludes +=
                setOf(
                    "DebugProbesKt.bin",
                    "META-INF/*.version",
                )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

val requestedReleaseTask =
    gradle.startParameter.taskNames.any { it.contains("release", ignoreCase = true) }
if (requestedReleaseTask && !hasReleaseSigning) {
    throw GradleException(
        "Android release signing is not configured. Set UNISON_ANDROID_KEYSTORE, " +
            "UNISON_ANDROID_STORE_PASSWORD, UNISON_ANDROID_KEY_ALIAS, and " +
            "UNISON_ANDROID_KEY_PASSWORD.",
    )
}

dependencies {
    // The Go core, built by `just aar`.
    implementation(files("../core/core.aar"))

    implementation(project(":bridge-aidl"))

    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.mlkit.vision)
    implementation(libs.androidx.camera.view)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.mlkit.barcode.scanning)
    implementation(libs.protobuf.javalite)
    implementation(libs.sentry.android)
    implementation(libs.zxing.core)
    implementation(platform(libs.androidx.compose.bom))
    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
    testImplementation(libs.json)
    testImplementation(libs.kotlinx.coroutines.test)

    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.androidx.test.runner)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
}
