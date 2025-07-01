## Agent Instructions for `qrvidencode`

This document provides guidance for AI agents working on the `qrvidencode` Go program located in this directory.

### 1. Core Logic

-   **File Input & Chunking**: The program reads an arbitrary input file (`-inputFile`) and splits it into byte chunks based on `-chunkSize`. This `chunkSize` refers to the original data portion.
-   **Metadata Prepending (Chunk Header Structure)**: Before QR code generation, each original data chunk is prepended with a header. The fields are serialized in the following order (all BigEndian):
    1.  `Checksum` (uint64, 8 bytes): XXH64 checksum.
    2.  `TotalChunks` (uint16, 2 bytes): The total number of chunks in the entire file/stream (max 65,535).
    3.  `SequenceNum` (uint16, 2 bytes): The 0-indexed sequence number of the current chunk (max 65,535).
    4.  `DataLength` (uint16, 2 bytes): The length of the `OriginalData` portion of the current chunk in bytes (max 65,535).
    The total header size is 14 bytes. The encoder includes checks and will exit if `TotalChunks`, `SequenceNum`, or `DataLength` exceed `math.MaxUint16`.
-   **Checksum Calculation**: The `Checksum` is calculated over the byte representations of the following fields concatenated in order: `TotalChunks` (2 bytes) + `SequenceNum` (2 bytes) + `DataLength` (2 bytes) + `OriginalData` (variable length).
-   **Final QR Payload**: The complete binary data that is encoded into the QR code consists of the full 14-byte header followed by the `OriginalData` chunk.
-   **QR Code Generation**: This final binary payload is then **ASCII85 encoded** into a string (using `encoding/ascii85`). This ASCII85 string is then passed to `github.com/boombuler/barcode/qr` to generate the QR code image (PNG). Parameters like error correction level (defaulting to Medium) and image pixel size (`-qrSize`) are configurable. The library handles the quiet zone automatically.
-   **Video Encoding**: The generated QR code PNG images (which include a 20px generated pattern border around a `qrSize`x`qrSize` QR symbol, making the PNG `qrSize+40`x`qrSize+40`) are stored temporarily on disk. The `ffmpeg` command-line tool is then invoked via `os/exec` to compile these images into a video file (`-outputFile`). Video parameters like frames per second (`-fps`), CRF value (`-crf`), duration each QR code is shown (`-framesPerQR`), and video resolution (`-resolution`) are configurable.
-   **Temporary Files**: A temporary directory is created to store intermediate QR code PNG files. This directory should be cleaned up using `defer os.RemoveAll(tempDir)`.

### 2. Dependencies

-   **Go Standard Library**: Used for file operations, command-line flags, `os/exec`, `encoding/binary`, `encoding/ascii85`, `math`, etc.
-   **`github.com/boombuler/barcode`** and **`github.com/boombuler/barcode/qr`**: External Go modules for QR code generation.
-   **`github.com/cespare/xxhash/v2`**: External Go module for XXH64 checksum calculation.
-   **`ffmpeg`**: External command-line tool. This is a runtime dependency that must be installed on the system where the program is run. The program calls `ffmpeg` directly.

### 3. Development & Testing

-   **Go Modules**: The project is a single Go module at the repository root (e.g., `qrvidproject`). The root `go.mod` and `go.sum` files manage dependencies for both `encode` and `decode` packages. Run `go mod tidy` from the repository root if dependencies change. Note: `encoding/ascii85` is part of the Go standard library.
-   **Building**: From the repository root, use a command like `go build -o encode/qrvidencoder ./encode` or `go build -o qrvidencoder ./encode`.
-   **Running Tests**: From the repository root, use `go test ./encode/...` (or `go test qrvidproject/encode` if using the full module path).
    -   Unit tests are in `main_test.go`.
    -   Tests for `ffmpeg` interaction are designed to be somewhat resilient to `ffmpeg` not being present in all test environments (e.g., some CI runners). They check for expected error messages if `ffmpeg` is not found.
    -   When adding tests related to `ffmpeg` output, ensure they can handle cases where `ffmpeg` might not be installed or might behave slightly differently across versions/platforms.
-   **Code Style**: Follow standard Go formatting (`gofmt` or `goimports`).
-   **Error Handling**:
    -   Check errors from file operations, QR code generation, and `ffmpeg` execution.
    -   Provide informative error messages to `os.Stderr`.
    -   Exit with a non-zero status code on critical errors.

### 4. Key Areas for Attention

-   **`ffmpeg` Command Construction**: The arguments passed to `ffmpeg` are critical. Pay close attention to:
    -   Input image pattern (`qr_frame_%04d.png`).
    -   Input framerate (`-framerate` flag for image sequences), which is calculated as `fps / framesPerQR`.
    -   Output framerate (`-r` flag for the video).
    -   Video resolution (`-s` flag).
    -   Pixel format (`-pix_fmt yuv420p`) for compatibility.
    -   Ensure `-y` is used to overwrite output files without prompting.
-   **Temporary File Management**: Ensure the temporary directory for QR images is always cleaned up, even if errors occur partway through processing. The `defer os.RemoveAll()` pattern is used for this.
-   **Chunk Payload Structure**: Be mindful of the new header structure: `[Checksum (8B)][TotalChunks (2B uint16)][SequenceNum (2B uint16)][DataLength (2B uint16)]` followed by `[OriginalData]`. The total header size is 14 bytes. Any changes to this must be reflected in the decoder(s). The `chunkSize` parameter refers only to the `[OriginalData]` part. The binary data (header + OriginalData) is ASCII85 encoded before being turned into a QR code. Max values for TotalChunks, SequenceNum, and DataLength are 65,535.
-   **Flag Validation**: Input flags like `-resolution`, `-crf`, etc. have specific formats or allowed values. Ensure these are validated. The encoder will also error if `TotalChunks`, `SequenceNum` (chunk index), or `DataLength` (original chunk data length) would exceed `math.MaxUint16`.
-   **Cross-Platform Compatibility**: While Go itself is cross-platform, reliance on `ffmpeg` means `ffmpeg` must be available on the target system. The `ffmpeg` command arguments used should be generally compatible across common versions and operating systems.

### 5. Potential Future Enhancements (If Requested)

-   **Pure Go Video Encoding**: Investigating or implementing a pure Go video encoding solution (if practical and performant) could remove the `ffmpeg` dependency. This would be a significant undertaking.
-   **In-Memory QR to Video Stream**: For advanced use cases, streaming QR code images directly to an encoder without saving to disk might improve performance or reduce I/O, but would require a suitable Go video library.
-   **Configuration File**: For many options, a configuration file might be more user-friendly than numerous command-line flags.
-   **Decoding Capability**: Adding a corresponding decoder to read a video and reconstruct the original data.

When making changes, ensure that the `README.md` is updated if command-line flags, build steps, or dependencies change.
Ensure all tests pass after your modifications.
