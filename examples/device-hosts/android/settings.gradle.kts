pluginManagement { repositories { google(); mavenCentral(); gradlePluginPortal() } }
dependencyResolutionManagement { repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS); repositories { google(); mavenCentral() } }
rootProject.name = "CymonkeyDeviceReference"
include(":app", ":helper")
project(":helper").projectDir = file("../../../pkg/android-helper")
