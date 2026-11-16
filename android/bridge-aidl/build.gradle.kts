plugins {
    alias(libs.plugins.android.library)
    `maven-publish`
}

android {
    namespace = "app.unison.bridge"
    compileSdk = 37
    compileSdkMinor = 2

    defaultConfig {
        minSdk = 29
    }

    buildFeatures {
        aidl = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    publishing {
        singleVariant("release")
    }
}

group = "app.unison"
version = rootProject.file("../VERSION").readText().trim()

publishing {
    repositories {
        maven {
            name = "build"
            url = uri(rootProject.layout.buildDirectory.dir("maven"))
        }
    }
}

afterEvaluate {
    publishing {
        publications {
            register<MavenPublication>("release") {
                from(components["release"])
            }
        }
    }
}
