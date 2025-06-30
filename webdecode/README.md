# Webdecode - QR Video Decoder

This application allows you to decode a sequence of QR codes from a screen capture (e.g., a video playing in another window/tab) and reassemble the original data. It's designed to be the client-side counterpart to a video encoder that displays data as a series of QR code frames.

## Features

*   Screen capture of a window or tab.
*   Real-time QR code detection from the captured video stream.
*   De-chunking of data from sequential QR codes.
*   Display of the reassembled data.

## Setup and Prerequisites

*   A modern web browser that supports the Screen Capture API (`getDisplayMedia`) and WebRTC.
*   Node.js and npm installed on your system to build the project.

## Building the Project

1.  **Clone the repository (if you haven't already).**
2.  **Navigate to the `webdecode` directory:**
    ```bash
    cd path/to/your/repo/webdecode
    ```
3.  **Install dependencies:**
    This project uses TypeScript. The necessary dependencies are listed in `package.json`.
    ```bash
    npm install
    ```
4.  **Build the TypeScript code:**
    The TypeScript source code is in the `src` directory. The build process compiles it into JavaScript in the `dist` directory.
    ```bash
    npm run build
    ```
    You can also use `npm run watch` to automatically rebuild on changes during development.

## Running the Application

1.  **Ensure you have built the project (see "Building the Project" above).**
    This will generate the `dist/main.js` file.
2.  **Open `webdecode/index.html` in your web browser.**
    You can usually do this by double-clicking the file or using your browser's "Open File" option. For local development, serving it via a local HTTP server can sometimes avoid issues with file path restrictions, though it's often not strictly necessary for simple client-side projects like this.
3.  **Click the "Start Capture" button.**
    Your browser will prompt you to select a screen, window, or tab to share. Choose the source that is displaying the QR code video.
4.  **Let the application scan.**
    The application will process frames from the selected source, looking for QR codes. It will collect and assemble the data from these QR codes. The output area will show the number of unique chunks collected.
5.  **Click the "Stop Capture" button.**
    Once you believe all QR codes have been scanned, or you wish to finalize the current data, click "Stop Capture". The application will then display the fully reassembled message.

## How it Works

*   The application uses `navigator.mediaDevices.getDisplayMedia()` to capture video from a user-selected screen.
*   The video stream is rendered to an invisible `<video>` element.
*   Periodically, frames from the video are drawn onto an invisible `<canvas>` element.
*   The image data from the canvas is then passed to the `jsQR` library, which attempts to decode any QR code present.
*   The data from successfully decoded QR codes is collected. Since the `encode` program (assumed counterpart) does not include explicit chunk metadata in the QR codes, this decoder assumes chunks are received in order and performs basic de-duplication.
*   When capture is stopped, all collected (and de-duplicated) chunks are concatenated to form the final message.

## Notes

*   **QR Code Clarity:** The success of decoding heavily depends on the clarity, size, and stillness of the QR codes in the captured stream.
*   **Performance:** Processing video frames continuously can be resource-intensive.
*   **Error Handling:** Basic error handling is in place, but complex capture scenarios or very noisy QR codes might lead to incomplete or incorrect decoding.
*   **Data Type:** The current implementation primarily handles text data. If the original data encoded into QR codes was binary, the reassembled output might not be directly usable without further processing (e.g., saving as a file with the correct type). The application currently displays the reassembled string.
*   **jsQR:** The `jsQR` library is loaded via a CDN link in `index.html`.
```
