plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "dev.cymonkey.reference"
    compileSdk = 36
    defaultConfig { applicationId = "dev.cymonkey.reference"; minSdk = 29; targetSdk = 36; versionCode = 1; versionName = "0.1" }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
kotlin { jvmToolchain(17) }
dependencies { implementation(project(":helper")); implementation("com.squareup.okhttp3:okhttp:4.12.0") }
