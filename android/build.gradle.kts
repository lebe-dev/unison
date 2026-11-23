import io.gitlab.arturbosch.detekt.extensions.DetektExtension

buildscript {
    dependencies {
        // Android Gradle plugin 9 compiles Kotlin itself; the version it depends on is raised here.
        classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.4.21")
    }
}

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.ktlint) apply false
    alias(libs.plugins.detekt) apply false
}

subprojects {
    plugins.withId("io.gitlab.arturbosch.detekt") {
        extensions.configure<DetektExtension> {
            buildUponDefaultConfig = true
            config.setFrom(rootProject.files("config/detekt/detekt.yml"))
            baseline = rootProject.file("config/detekt/baseline.xml")
        }
    }
}
