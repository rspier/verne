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

Due to limitations on the number of files that can be generated, running a full Gradle build (`./gradlew :app:assembleDebug`) is not possible in this environment.

**Workflow:**

1.  Make the necessary code changes.
2.  Submit the changes without running a build.
3.  Add a note to the submission indicating that the build could not be verified due to environmental constraints.
