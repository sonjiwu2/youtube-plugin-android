plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "ru.razgon.app"
    compileSdk = 37

    defaultConfig {
        applicationId = "ru.razgon.app"
        minSdk = 24
        targetSdk = 36
        versionCode = 3
        versionName = "0.1.0"
        ndk { abiFilters += listOf("arm64-v8a", "armeabi-v7a", "x86_64") }
    }

    signingConfigs {
        val keystorePath = System.getenv("ANDROID_KEYSTORE_FILE")
        if (!keystorePath.isNullOrEmpty()) create("production") {
            storeFile = file(keystorePath)
            storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
            keyAlias = "razgon"
            keyPassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("production")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging { jniLibs { useLegacyPackaging = true; keepDebugSymbols += "**/libciadpi.so" } }
    splits {
        abi {
            isEnable = providers.gradleProperty("splitApk").isPresent
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = true
        }
    }
}

dependencies {
    implementation(files("libs/engine.aar"))
    implementation(libs.androidx.webkit)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    debugImplementation(libs.compose.ui.tooling)

    testImplementation(libs.junit)
}

tasks.register("checkEngine") {
    doLast { check(file("libs/engine.aar").exists()) { "Сначала выполните scripts/build-engine.ps1 или scripts/build-engine.sh" } }
}
tasks.named("preBuild") { dependsOn("checkEngine") }
