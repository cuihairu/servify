plugins {
    id("com.android.application") version "8.7.3"
    kotlin("android") version "2.1.20"
}

// M1 验收③的基线 app：与 demo 同一宿主 UI、零 SDK。CI 体积门禁 =
// demo APK 与本 app APK 的差值（= SDK 及其全部传递依赖增量）≤ 1.5MB（D9）。
android {
    namespace = "servify.sdk.demo.baseline"
    compileSdk = 35

    defaultConfig {
        applicationId = "servify.sdk.demo.baseline"
        minSdk = 24
        targetSdk = 35
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildTypes {
        release {
            // 与 demo 同口径（R8），保证差值纯度。
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }
}
