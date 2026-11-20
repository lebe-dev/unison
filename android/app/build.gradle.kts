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

val appVersion = rootProject.file("../VERSION").readText().trim()
val sentryDsn = buildValue("SENTRY_DSN")

android {
    namespace = "app.unison"
    compileSdk = 37
    compileSdkMinor = 2

    defaultConfig {
        applicationId = "app.unison"
        minSdk = 29
        targetSdk = 37
        versionCode = 1
        versionName = appVersion
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // An empty DSN disables reporting in that build.
        buildConfigField("String", "SENTRY_DSN", quoted(sentryDsn))
        buildConfigField("String", "RELEASE", quoted("unison@$appVersion"))
    }

    buildTypes {
        debug {
            buildConfigField("String", "ENVIRONMENT", quoted("debug"))
        }
        release {
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

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
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
    testImplementation(libs.kotlinx.coroutines.test)

    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.androidx.test.runner)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
}
