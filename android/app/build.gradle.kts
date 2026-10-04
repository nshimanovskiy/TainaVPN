plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val appVersionName = (System.getenv("APP_VERSION") ?: file("../../VERSION").readText().trim()).removePrefix("v")
val appVersionCode = (System.getenv("APP_VERSION_CODE") ?: "1").toInt()

android {
    namespace = "com.tainavpn.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.tainavpn.app"
        minSdk = 24
        targetSdk = 34
        versionCode = appVersionCode
        versionName = appVersionName
    }

    signingConfigs {
        create("release") {
            // CI passes a keystore from secrets; otherwise the key committed to this private repo is used
            val ksFile = System.getenv("ANDROID_KEYSTORE_FILE")?.let { file(it) } ?: file("tainavpn.jks")
            storeFile = ksFile
            storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD") ?: "tainavpn"
            keyAlias = System.getenv("ANDROID_KEY_ALIAS") ?: "tainavpn"
            keyPassword = System.getenv("ANDROID_KEY_PASSWORD") ?: "tainavpn"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("release")
        }
        debug {
            signingConfig = signingConfigs.getByName("release")
        }
    }

    sourceSets {
        getByName("main") {
            // shared UI from ../../ui
            assets.srcDirs("../../ui")
        }
    }

    androidResources {
        ignoreAssetsPattern = "!*.go:!.*"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }
}

dependencies {
    // built by `gomobile bind` in CI (see .github/workflows/build.yml)
    implementation(files("libs/tainacore.aar"))
}
