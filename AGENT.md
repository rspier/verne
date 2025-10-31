# Agent Instructions

This document provides instructions for setting up the environment and building this Android project.

## Environment Setup

The Android SDK is not pre-installed in the environment and must be set up manually.

### 1. Install the Android SDK

1.  Create the necessary directories:
    ```bash
    mkdir -p ~/.android/sdk/cmdline-tools/latest
    ```
2.  Download the Android SDK command-line tools to a temporary location:
    ```bash
    wget https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip -P /tmp
    ```
3.  Unzip the tools and move them to the installation directory:
    ```bash
    unzip -qo /tmp/commandlinetools-linux-13114758_latest.zip -d /tmp && \
    mv /tmp/cmdline-tools/* ~/.android/sdk/cmdline-tools/latest/
    ```
4.  Clean up the downloaded file:
    ```bash
    rm /tmp/commandlinetools-linux-13114758_latest.zip
    ```

### 2. Configure `local.properties`

Create a `local.properties` file in the project root with the following content:

```
sdk.dir=/home/jules/.android/sdk
```

### 3. Accept SDK Licenses

Accept the Android SDK licenses using the following command:

```bash
yes | ~/.android/sdk/cmdline-tools/latest/bin/sdkmanager --licenses
```

## Building the Project

The project can be built successfully using the standard Gradle command:

```bash
./gradlew :app:assembleDebug
```

### Build Troubleshooting

Previously, the build failed due to several issues. Here is a summary of the fixes:

*   **`.gitignore` was missing:** The `.gitignore` file was missing entries for the `build/` and `.gradle/` directories, which caused the build to fail due to an excessive number of generated files. This has been corrected.
*   **Outdated Dependencies:** The `androidx.media3` dependencies were outdated, leading to compilation errors. They have been updated to version `1.8.0`.
*   **Incorrect `compileSdk`:** The `compileSdk` and `targetSdk` were set to `34`, but the updated dependencies required `35`. This has been corrected in `app/build.gradle`.
*   **Missing Imports:** The `SilenceMediaBrowserService.kt` file was missing necessary imports for `LibraryResult` and `LibraryParams`. These have been added.
