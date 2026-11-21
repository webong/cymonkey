plugins {
    id("com.android.library") version "8.11.1"
    id("org.jetbrains.kotlin.android") version "2.0.21"
}

android {
    namespace = "dev.cymonkey.androidhelper"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    val extAndroidAar = file(".dependencies/ext-android-0.2.0.aar")
    require(extAndroidAar.isFile) {
        "Ext Android 0.2.0 is missing; run ./fetch-ext-android.sh before building"
    }
    // The host app also declares this AAR as an implementation dependency.
    // compileOnly keeps the Cymonkey library AAR from embedding another AAR.
    compileOnly(files(extAndroidAar))
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("androidx.webkit:webkit:1.9.0")
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
