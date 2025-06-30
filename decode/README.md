# QR Code Video Decoder

This program decodes data from a video file that was previously encoded as a sequence of QR codes (e.g., by the `qrvidencode` program). It extracts frames from the video, scans them for QR codes, and reconstructs the original data.

## Features

-   Extracts frames from various video formats (relies on `ffmpeg`).
-   Scans frames for QR codes using a pure Go library.
-   Handles videos where QR codes are displayed for multiple frames.
-   Reconstructs the original data from the sequence of QR code payloads.
-   Customizable frame processing (skip initial frames, limit total frames).

## Dependencies

-   **Go**: Version 1.18 or higher (for Go module support).
-   **FFmpeg**: This program relies on the `ffmpeg` command-line tool to extract frames from the input video. You must have `ffmpeg` installed and accessible in your system's PATH.
    *   Refer to the `encode/README.md` or official FFmpeg documentation for installation instructions.

## Setup and Build

This program is part of a single Go module at the repository root.

1.  **Navigate to the repository root directory.**
    ```bash
    cd path/to/yourproject
    ```
2.  **Fetch dependencies (if not already done):**
    This command, run from the root, will download all necessary libraries for both the encoder and decoder.
    ```bash
    go mod tidy
    ```
3.  **Build the decoder executable:**
    Run this command from the repository root.
    ```bash
    go build -o qrviddecoder ./decode
    ```
    This will create an executable file named `qrviddecoder` (or `qrviddecoder.exe` on Windows) in the repository root directory.
    To place it inside the `decode` directory:
    ```bash
    go build -o decode/qrviddecoder ./decode
    ```

## Usage

The input video file is now a positional argument.

If you built `qrviddecoder` in the repository root:
```bash
./qrviddecoder <path_to_input_video.mp4> --out <path_to_decoded_data> [options]
```
If you built it into the `decode` directory (e.g., `decode/qrviddecoder`):
```bash
./decode/qrviddecoder <path_to_input_video.mp4> --out <path_to_decoded_data> [options]
```

Or, using `go run` from the repository root (useful for quick tests):
```bash
go run ./decode/main.go <path_to_input_video.mp4> --out <path_to_decoded_data> [options]
```

### Arguments & Flags

**Positional Arguments:**
1.  `<input_video_path>` (required): Path to the input video file.

**Flags:**
| Flag                 | Type   | Default                    | Description                                                                            |
|----------------------|--------|----------------------------|----------------------------------------------------------------------------------------|
| `--out`              | string | (required)                 | Path to the output file where decoded data will be written.                            |
| `--tempDirPrefix`    | string | `qrvid_decode_frames_`     | Prefix for the temporary directory used to store extracted frames.                     |
| `--framesToSkip`     | int    | `0`                        | Number of initial frames to skip in the video before starting QR code processing.      |
| `--maxFramesToProcess`| int    | `0` (process all)          | Maximum number of frames to process after skipping initial frames. `0` means no limit.   |

### Examples

1.  **Decode `data_video.mp4` and save the original data to `retrieved_data.txt`:**
    ```bash
    ./qrviddecoder data_video.mp4 --out retrieved_data.txt
    ```

2.  **Decode a video, skipping the first 10 frames:**
    ```bash
    ./qrviddecoder data_video.mp4 --out retrieved_data.txt --framesToSkip 10
    ```

## How it Works

1.  The program parses command-line arguments.
2.  A temporary directory is created to store video frames.
3.  `ffmpeg` is invoked to extract frames from the input video file (`-inputFile`) into the temporary directory as PNG images.
    *   The `-framesToSkip` flag controls how many initial frames are ignored.
    *   The `-maxFramesToProcess` flag can limit how many frames are extracted after the skipped ones.
4.  The program iterates through the extracted frame images in sequence (filenames are sorted).
5.  For each frame:
    a.  The image is loaded.
    b.  The `gozxing` library attempts to find and decode a QR code within the image.
    c.  If a QR code is found, its raw payload (a string of bytes) is processed. To avoid reprocessing identical QR codes from consecutive video frames, a map of seen raw payloads is maintained.
    d.  The raw payload is parsed to extract:
        i.  An 8-byte XXH64 checksum.
        ii. A 4-byte sequence number (uint32, BigEndian).
        iii. The original data chunk.
    e.  The checksum of `[sequence_number_bytes][original_data_chunk_bytes]` is recalculated and compared against the received checksum. If it mismatches, the chunk is discarded.
    f.  Valid chunks (original data part) are stored in a map, keyed by their sequence number. This handles out-of-order frame processing or QR detection. Only the first valid instance of a sequence number is stored.
6.  After all frames are processed, the program reconstructs the original data:
    a.  It iterates from sequence number 0 up to the maximum sequence number encountered.
    b.  Data chunks are retrieved from the map in order.
    c.  If any sequence number is missing, an error is reported, and the output data will be incomplete.
7.  The concatenated, ordered data is written to the specified output file (`-outputFile`).
8.  The temporary directory containing the frame images is deleted.

## Note on `ffmpeg`

If `ffmpeg` is not found in your system's PATH, the program will fail during the frame extraction step. Ensure `ffmpeg` is installed and accessible.
The program prints the `ffmpeg` command it attempts to execute, which can be useful for debugging.
