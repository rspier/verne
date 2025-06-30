# Webdecode - Advanced QR Video Decoder

This application allows you to decode a sequence of QR codes from a screen capture (e.g., a video playing in another window/tab) and reassemble the original data. It is designed to work with QR video streams where each QR code contains a hex-encoded payload that includes a binary header (with sequence number, data length, and checksum) followed by the actual data chunk.

## Features

*   Screen capture of a window or tab via `getDisplayMedia()`.
*   Real-time QR code detection using `jsQR`.
*   Hex decoding of QR payloads.
*   Parsing of a binary header containing:
    *   XXH64 checksum (for data integrity).
    *   Sequence number (for ordering chunks).
    *   Data length (for extracting the original data).
*   Checksum verification using `xxhash-wasm`.
*   Reassembly of ordered and verified data chunks.
*   Display of reassembled text data (UTF-8) or a hex preview for binary data.
*   Download functionality for the reassembled binary data.

## Setup and Prerequisites

*   A modern web browser that supports:
    *   Screen Capture API (`getDisplayMedia`).
    *   WebRTC.
    *   WebAssembly (for `xxhash-wasm`).
    *   `TextDecoder` and `DataView` APIs.
*   Node.js and npm installed on your system if you wish to modify the TypeScript source or manage dependencies locally (though the application can be run from `index.html` using CDN-hosted libraries).

## Building the Project (Optional - for development)

The project is written in TypeScript. If you modify the `.ts` files, you'll need to rebuild.

1.  **Navigate to the `webdecode` directory:**
    ```bash
    cd path/to/your/repo/webdecode
    ```
2.  **(Optional) Install dependencies if you haven't or want to update them:**
    Dependencies like `typescript` and `eslint` are listed in `package.json`.
    ```bash
    npm install
    ```
    *Note: `xxhash-wasm` is also listed as a dependency for completeness but is loaded via CDN in `index.html` for direct browser use.*
3.  **Build the TypeScript code:**
    The TypeScript source code is in `src/main.ts`. The build process compiles it into JavaScript in `dist/main.js`.
    ```bash
    npm run build
    ```
    You can also use `npm run watch` to automatically rebuild on changes during development.

## Running the Application

1.  **Ensure `dist/main.js` is present.** If you are not developing and just running pre-built code, this file should exist. If developing, build the project first (see above).
2.  **Open `webdecode/index.html` in your web browser.**
    You can usually do this by double-clicking the file or using your browser's "Open File" option.
3.  **Click the "Start Capture" button.**
    Your browser will prompt you to select a screen, window, or tab to share. Choose the source that is displaying the QR code video.
4.  **Monitor the Status.**
    The application will process frames, looking for QR codes. It will display status messages, including chunks received, verified, and any errors.
5.  **Click the "Stop Capture" button.**
    Once you believe all QR codes have been scanned, or you wish to finalize the current data, click "Stop Capture".
6.  **View/Download Output.**
    The application will attempt to reassemble the data.
    *   If it appears to be UTF-8 text, it will be displayed in the textarea.
    *   If not, a hex preview might be shown.
    *   A download link will appear, allowing you to save the reassembled file. You can specify a filename before downloading.

## QR Code Payload Format

This decoder expects QR codes to contain a **hex-encoded string**. When this string is hex-decoded, it yields a binary payload with the following structure:

1.  **Header** (16 bytes total):
    *   **Checksum** (8 bytes): `uint64`, Big Endian. XXH64 checksum of the "Checksum Data" (see below).
    *   **Sequence Number** (4 bytes): `uint32`, Big Endian. The sequential index of this chunk, starting from 0.
    *   **Data Length** (4 bytes): `uint32`, Big Endian. The length of the `Original Data` part in bytes.
2.  **Original Data** (`Data Length` bytes): The actual chunk of the original file/data.

**Checksum Data:** The checksum is calculated over the concatenation of:
*   Sequence Number (as 4 bytes, Big Endian)
*   Data Length (as 4 bytes, Big Endian)
*   Original Data (the raw bytes)

## Libraries Used

*   **jsQR:** For QR code decoding from canvas image data. Loaded via CDN.
*   **xxhash-wasm:** For calculating XXH64 checksums. Loaded via CDN.

## Notes

*   **QR Code Clarity:** The success of decoding heavily depends on the clarity, size, and stillness of the QR codes in the captured stream.
*   **Performance:** Processing video frames continuously can be resource-intensive. The frame processing interval is set in `src/main.ts` (`FRAME_PROCESS_INTERVAL_MS`).
*   **Completeness:** The application currently assumes all chunks from sequence number 0 up to the highest sequence number seen must be present for successful reassembly.
```
