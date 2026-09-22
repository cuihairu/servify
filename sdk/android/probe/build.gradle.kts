plugins {
    kotlin("jvm") version "2.1.20"
    kotlin("plugin.serialization") version "2.1.20"
    application
}

// M0 联调探针：纯 JVM CLI，与 servify-sdk 共享 shared/ 的协议层与会话核心源。
// applicationName 保持 "servify-android-probe"，使验收脚本的 installDist 产物路径稳定。
application {
    applicationName = "servify-android-probe"
    mainClass.set("servify.sdk.android.probe.ProbeMainKt")
}

dependencies {
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.1")
}

kotlin {
    jvmToolchain(21)
    sourceSets {
        main {
            kotlin.srcDirs("src/main/kotlin", "../shared/protocol", "../shared/core")
        }
    }
}
