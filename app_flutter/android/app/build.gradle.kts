import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// The release key, when a build has one. The path is an environment variable
// rather than a checked-in file, so the key never lands in the repository; a
// build without it is debug-signed and still installable.
val keyPropertiesPath: String? = System.getenv("CARPOOL_KEY_PROPERTIES")
val keyProperties: Properties? = keyPropertiesPath?.let { path ->
    val file = file(path)
    if (!file.exists()) {
        logger.lifecycle("CARPOOL_KEY_PROPERTIES points at $path, which does not exist: debug signing")
        null
    } else {
        Properties().apply { file.inputStream().use { load(it) } }
    }
}

android {
    namespace = "gr.carpool.app.flutter"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        // flutter_local_notifications needs the newer Java time APIs on older
        // Android versions, which desugaring provides.
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // Installs next to the Expo app (gr.carpool.app), so both can sit on
        // one phone during the two-phone test.
        applicationId = "gr.carpool.app.flutter"
        // maplibre_gl needs 23; anything older has no map and no runtime worth
        // supporting.
        minSdk = maxOf(flutter.minSdkVersion, 23)
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (keyProperties != null) {
            create("release") {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = if (keyProperties != null) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
}

// Push needs the project's Firebase config. Without it the app builds with push
// off rather than failing: a phone with no notifications still finds a lift.
val googleServicesJson = file("google-services.json")
if (googleServicesJson.exists()) {
    apply(plugin = "com.google.gms.google-services")
} else {
    logger.lifecycle("android/app/google-services.json is missing: this APK has push notifications off")
}

flutter {
    source = "../.."
}
