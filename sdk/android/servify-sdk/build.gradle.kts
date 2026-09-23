plugins {
    id("com.android.library") version "8.7.3"
    kotlin("android") version "2.1.20"
    kotlin("plugin.serialization") version "2.1.20"
    id("org.jetbrains.kotlin.plugin.compose") version "2.1.20"
}

// 单 AAR、内部多包（平台规格 §2）：shared/ 的 protocol/core 源直接编入本模块，
// Android 专属层（配置、错误模型、连接、UI）在 src/main/kotlin。
android {
    namespace = "servify.sdk.android"
    compileSdk = 35

    defaultConfig {
        minSdk = 24
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }

    // 覆盖率口径（AGP 内置 jacoco，零新依赖）：单测覆盖率报告走
    // ./gradlew createDebugUnitTestCoverageReport（HTML/Xml 落 build/reports/coverage）。
    buildTypes {
        getByName("debug") {
            enableUnitTestCoverage = true
        }
    }

    sourceSets.getByName("main") {
        java.srcDirs("src/main/kotlin", "../shared/protocol", "../shared/core")
    }
}

dependencies {
    api("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.9.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.1")
    // 依赖白名单（D3/D9）：Compose 仅 runtime/ui/foundation 子集（material/material3 禁入，
    // 视觉组件自绘）；版本经 BOM 对齐，任何白名单外依赖须过评审（D9 依赖冻结机制）。
    implementation(platform("androidx.compose:compose-bom:2024.12.01"))
    implementation("androidx.compose.runtime:runtime")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.foundation:foundation")
    testImplementation(kotlin("test"))
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
}
