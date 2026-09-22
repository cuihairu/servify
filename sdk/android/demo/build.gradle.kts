plugins {
    id("com.android.application") version "8.7.3"
    kotlin("android") version "2.1.20"
}

// M1 验收②：demo 宿主 ≤10 行集成。纯 View（无 Compose），证明 SDK 不强加宿主技术栈。
// 体积门禁（M1 验收③）与 demo-baseline 对比：两 APK 差值 = SDK 及其全部传递依赖增量。
android {
    namespace = "servify.sdk.demo"
    compileSdk = 35

    defaultConfig {
        applicationId = "servify.sdk.demo"
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
            // D9 体积口径按 R8 后计：宿主集成 SDK 的现实场景都开 minify，
            // 未 minify 的 Compose 全家桶远超 1.5MB 预算（本地实测 5MB+）。
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }
}

dependencies {
    implementation("androidx.activity:activity:1.9.3")
    implementation(project(":servify-sdk"))
}
