plugins { alias(libs.plugins.android.application) }
android {
    namespace = "ru.youtubeplugin.probe"
    compileSdk = 37
    defaultConfig { applicationId = "ru.youtubeplugin.probe"; minSdk = 24; targetSdk = 36; versionCode = 1; versionName = "1" }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
