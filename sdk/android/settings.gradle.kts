pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "servify-android"

include(":servify-sdk")
include(":probe")
include(":demo")
include(":demo-baseline")
