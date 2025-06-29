## Agent Instructions for `qrviddecode`

This document provides guidance for AI agents working on the `qrviddecode` Go program located in this directory. This program is designed to decode videos created by the `qrvidencode` program (found in the sibling `encode` directory).

### 1. Core Logic

-   **Video Input**: The program takes a video file as input (`-inputFile`). This video is expected to contain a sequence of QR codes.
-   **Frame Extraction (`ffmpeg`)**:
    -   It uses the external `ffmpeg` command-line tool (via `os/exec`) to extract frames from the input video. These frames are saved as PNG images in a temporary directory.
    -   Command-line flags `-framesToSkip` and `-maxFramesToProcess` control which frames are extracted.
    -   The `ffmpeg` command construction is critical. Pay attention to input (`-i`), video filter (`-vf select`), frame limiting (`-frames:v`), and output pattern (`frame_%06d.png`).
-   **QR Code Decoding (`gozxing`)**:
    -   Each extracted frame image is processed.
    -   The `github.com/makiuchi-d/gozxing` library is used to detect and decode QR codes from these images.
    -   The program is designed to be flexible: if a frame doesn't contain a scannable QR code, it's skipped.
-   **Data Deduplication & Ordering**:
    -   Extracted frames are processed in sequence (sorted by filename).
    -   To handle the case where one QR code (one data chunk) is displayed for multiple video frames (as per `framesPerQR` in the encoder), the decoder only appends the textual data from a QR code if it's different from the *immediately preceding successfully decoded QR code's data*. This ensures each unique data chunk is recorded once and in the correct order.
-   **Data Output**: The concatenated, unique data payloads from the QR codes are written to an output file (`-outputFile`).
-   **Temporary Files**: A temporary directory (prefix `-tempDirPrefix`) is created to store intermediate frame PNG files. This directory is cleaned up using `defer os.RemoveAll()`.

### 2. Dependencies

-   **Go Standard Library**: Used for file operations, command-line flags, `os/exec`, image processing stubs, etc.
-   **`github.com/makiuchi-d/gozxing`**: External Go module for QR code decoding.
    -   Includes sub-packages like `github.com/makiuchi-d/gozxing/qrcode`.
    -   Requires `image` and image format specific packages (e.g., `image/png`) to be imported for `image.Decode` to work.
-   **`ffmpeg`**: External command-line tool. This is a **runtime dependency** that must be installed on the system where the program is run. The program calls `ffmpeg` directly.

### 3. Development & Testing

-   **Go Modules**: The project uses Go modules. Any new external Go dependencies should be added via `go get` and managed in `go.mod` / `go.sum`. Run `go mod tidy` after changes.
-   **Building**: Use `go build -o qrviddecoder .` within the `decode` directory.
-   **Running Tests**:
    -   Use `go test .` within the `decode` directory.
    -   The `main_test.go` file contains primarily a placeholder and instructions for manual end-to-end testing due to the reliance on `ffmpeg` and the encoder program.
    -   For true E2E testing, you'll need to:
        1.  Build the encoder from `../encode`.
        2.  Create a sample file.
        3.  Encode it to a video.
        4.  Run this decoder on that video.
        5.  Compare the original sample file with the decoded output.
-   **Code Style**: Follow standard Go formatting (`gofmt` or `goimports`).
-   **Error Handling**:
    -   Check errors from file operations, `ffmpeg` execution, image decoding, and QR code scanning.
    -   Provide informative error messages to `os.Stderr`.
    -   Exit with a non-zero status code on critical errors.
    -   Non-critical errors (e.g., a single frame failing to decode) should be logged as warnings, allowing the program to continue with other frames.

### 4. Key Areas for Attention

-   **`ffmpeg` Command Interaction**: This is a common source of issues.
    -   Ensure the command arguments are correct and robust.
    -   Handle potential errors from `ffmpeg` (e.g., file not found, invalid video format, `ffmpeg` not installed).
-   **QR Code Scanning Robustness**:
    -   The current implementation uses default decoding hints. If specific types of QR codes are problematic, hints might need to be passed to `qrReader.Decode()`.
    -   Image quality from `ffmpeg` can affect QR scanning. Default PNG extraction is usually good.
-   **Temporary File Management**: Ensure the temporary directory for frames is always cleaned up.
-   **Cross-Platform Compatibility**: While Go is cross-platform, `ffmpeg` must be available. `ffmpeg` commands used should be generally compatible.

### 5. Potential Future Enhancements (If Requested)

-   **Pure Go Frame Extraction**: Replacing `ffmpeg` for frame extraction would remove the main external binary dependency but is a very complex task (requires Go libraries for video demuxing and decoding).
-   **More Sophisticated Deduplication**: The current deduplication logic is simple (compare with last). For extremely noisy videos, a more robust system (e.g., content-based hashing of payloads over a small window) might be considered, but adds complexity.
-   **Error Correction/Reporting**: If some QR codes are consistently missed, providing more detailed feedback or attempting error correction on image processing (e.g. contrast adjustment) could be options, but are advanced.
-   **Support for other QR libs**: If `gozxing` has issues, other libs could be explored.

When making changes, ensure that the `README.md` is updated if command-line flags, build steps, or dependencies change.
Ensure `go test .` (even with its current manual focus) can be run and any actual unit tests pass.
Verify the manual end-to-end test procedure still works.
