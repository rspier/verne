## Agent Instructions for `qrviddecode`

This document provides guidance for AI agents working on the `qrviddecode` Go program located in this directory. This program is designed to decode videos created by the `qrvidencode` program (found in the sibling `encode` directory).

### 1. Core Logic

-   **Video Input**: The program takes a video file path as a **positional argument**. This video is expected to contain a sequence of QR codes.
-   **Frame Extraction (`ffmpeg`)**:
    -   It uses the external `ffmpeg` command-line tool (via `os/exec`) to extract frames from the input video. These frames are saved as PNG images in a temporary directory.
    -   Command-line flags `--framesToSkip` and `--maxFramesToProcess` control which frames are extracted.
    -   The `ffmpeg` command construction is critical. Pay attention to input (`-i`), video filter (`-vf select`), frame limiting (`-frames:v`), and output pattern (`frame_%06d.png`).
-   **QR Code Payload Processing (`gozxing`, `encoding/hex`, `encoding/binary`, `xxhash`)**:
    -   Each extracted frame image is processed using `github.com/makiuchi-d/gozxing` to detect and decode QR codes.
    -   If a QR code is found, its text content (which is expected to be a hex string) is retrieved using `result.GetText()`.
    -   **Deduplication of Hex Payloads**: A map of seen hex string payloads (`seenRawPayloads`) is used to quickly skip reprocessing the exact same QR code if it appears on consecutive video frames.
    -   **Hex Decoding**: The retrieved hex string is decoded into its original binary form (`rawPayloadBytes`).
    -   **Header Deserialization**: A `ChunkHeader` struct is deserialized from the beginning of `rawPayloadBytes` using `encoding/binary.Read` (BigEndian). This header contains `Checksum (uint64)`, `SequenceNum (uint32)`, and `DataLength (uint32)`.
    -   **Data Extraction**: The `OriginalData` is extracted from `rawPayloadBytes` immediately following the header, using `header.DataLength`.
    -   **Checksum Validation**: The checksum is verified. It was calculated in the encoder over: `SequenceNum` bytes + `DataLength` bytes + `OriginalData` bytes. This sequence is reconstructed and checksummed again. If mismatched with `header.Checksum`, the chunk is discarded.
-   **Data Storage and Ordering**:
    -   Valid `OriginalData` chunks are stored in a map (`decodedChunks`), keyed by their `header.SequenceNum`.
    -   The maximum sequence number encountered is tracked.
-   **Data Output**: After all frames are processed, the program reconstructs the full data by iterating from sequence number 0 to the maximum sequence number seen, appending chunks from the `decodedChunks` map. If any sequence numbers are missing, an error is reported. The concatenated data is written to the file specified by `--out`.
-   **Temporary Files**: A temporary directory (prefix `-tempDirPrefix`) is created to store intermediate frame PNG files. This directory is cleaned up using `defer os.RemoveAll()`.

### 2. Dependencies

-   **Go Standard Library**: Used for file operations, command-line flags, `os/exec`, `encoding/binary`, `bytes`, image processing stubs, etc.
-   **`github.com/makiuchi-d/gozxing`**: External Go module for QR code decoding.
    -   Includes sub-packages like `github.com/makiuchi-d/gozxing/qrcode`.
    -   Requires `image` and image format specific packages (e.g., `image/png`) to be imported for `image.Decode` to work.
-   **`github.com/cespare/xxhash/v2`**: External Go module for XXH64 checksum calculation.
-   **`ffmpeg`**: External command-line tool. This is a **runtime dependency** that must be installed on the system where the program is run. The program calls `ffmpeg` directly.

### 3. Development & Testing

-   **Go Modules**: The project is a single Go module at the repository root (e.g., `qrvidproject`). The root `go.mod` and `go.sum` files manage dependencies for both `encode` and `decode` packages. Run `go mod tidy` from the repository root if dependencies change.
-   **Building**: From the repository root, use a command like `go build -o decode/qrviddecoder ./decode` or `go build -o qrviddecoder ./decode`.
-   **Running Tests**:
    -   From the repository root, use `go test ./decode/...` (or `go test qrvidproject/decode` if using the full module path).
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
-   **QR Code Payload Parsing and Validation**:
    -   The structure `[checksum (8B)][sequence_number (4B)][data]` is critical. Ensure parsing logic (byte slicing, `binary.BigEndian` usage) correctly extracts these fields.
    -   Checksum validation using `xxhash.Sum64` must cover the same byte range as the encoder (`sequence_number_bytes + data_bytes`).
-   **Sequence Number Handling**:
    -   The system relies on 0-indexed, contiguous sequence numbers.
    -   Detection of missing sequence numbers during final assembly is important for data integrity assessment.
-   **QR Code Scanning Robustness**:
    -   The current implementation uses default decoding hints. If specific types of QR codes are problematic, hints might need to be passed to `qrReader.Decode()`.
    -   Image quality from `ffmpeg` can affect QR scanning. Default PNG extraction is usually good.
-   **Temporary File Management**: Ensure the temporary directory for frames is always cleaned up.
-   **Cross-Platform Compatibility**: While Go is cross-platform, `ffmpeg` must be available. `ffmpeg` commands used should be generally compatible.

### 5. Potential Future Enhancements (If Requested)

-   **Pure Go Frame Extraction**: Replacing `ffmpeg` for frame extraction would remove the main external binary dependency but is a very complex task (requires Go libraries for video demuxing and decoding).
-   **More Sophisticated Error Handling for Missing Chunks**: Allow configurable behavior for missing chunks (e.g., fill with zeros, use special marker, strict fail).
-   **Sliding Window for Deduplication/Ordering**: For very large numbers of chunks or extremely out-of-order frames, a more memory-efficient approach than holding all chunks in a map might be needed, though this adds complexity.
-   **Support for other QR libs**: If `gozxing` has issues, other libs could be explored.

When making changes, ensure that the `README.md` is updated if command-line flags, build steps, or dependencies change.
Ensure `go test .` (even with its current manual focus) can be run and any actual unit tests pass.
Verify the manual end-to-end test procedure still works.
Remember to run `go mod tidy` after changing dependencies.
