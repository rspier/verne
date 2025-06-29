# QR Code Video Encoder

This program encodes arbitrary data from an input file into a sequence of QR codes, which are then compiled into a video file. Each QR code represents a chunk of the input data.

## Features

-   Chunks input data to a specified size.
-   Encodes each data chunk into a QR code.
-   Generates a video where each QR code is displayed for a configurable number of frames.
-   Customizable QR code parameters (recovery level, quiet zone, size).
-   Customizable video parameters (resolution, FPS).

## Dependencies

-   **Go**: Version 1.18 or higher (for Go module support).
-   **FFmpeg**: This program relies on the `ffmpeg` command-line tool to create the video from QR code images. You must have `ffmpeg` installed and accessible in your system's PATH.

    *   **To install FFmpeg:**
        *   **macOS (using Homebrew):** `brew install ffmpeg`
        *   **Linux (Debian/Ubuntu):** `sudo apt update && sudo apt install ffmpeg`
        *   **Linux (Fedora):** `sudo dnf install ffmpeg`
        *   **Windows:** Download binaries from [ffmpeg.org](https://ffmpeg.org/download.html) and add the `bin` directory to your system's PATH.

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
3.  **Build the encoder executable:**
    Run this command from the repository root.
    ```bash
    go build -o qrvidencoder ./encode
    ```
    This will create an executable file named `qrvidencoder` (or `qrvidencoder.exe` on Windows) in the repository root directory.
    To place it inside the `encode` directory:
    ```bash
    go build -o encode/qrvidencoder ./encode
    ```

## Usage

If you built `qrvidencoder` in the repository root:
```bash
./qrvidencoder -inputFile <path_to_input_file> [options]
```
If you built it into the `encode` directory (e.g., `encode/qrvidencoder`):
```bash
./encode/qrvidencoder -inputFile <path_to_input_file> [options]
```

Or, using `go run` from the repository root (useful for quick tests):
```bash
go run ./encode/main.go -inputFile <path_to_input_file> [options]
```

### Command-Line Flags

| Flag            | Type   | Default     | Description                                                                 |
|-----------------|--------|-------------|-----------------------------------------------------------------------------|
| `-inputFile`    | string | (required)  | Path to the input file containing the data to encode.                       |
| `-outputFile`   | string | `output.mp4`| Path to the output video file.                                              |
| `-chunkSize`    | int    | `1024`      | Size of data chunks in bytes. Each chunk becomes one QR code.               |
| `-qrLevel`      | string | `M`         | QR code recovery level. Options: `L` (Low), `M` (Medium), `Q` (Quartile), `H` (High). |
| `-qrSize`       | int    | `256`       | Size (width and height) of the generated QR code image in pixels.           |
| `-fps`          | int    | `1`         | Frames per second for the output video.                                     |
| `-framesPerQR`  | int    | `1`         | Number of video frames each QR code image is displayed for.                 |
| `-resolution`   | string | `256x256`   | Video resolution in `WIDTHxHEIGHT` format (e.g., "1280x720"). Defaults to QR size. |

### Examples

1.  **Encode a text file `mydata.txt` into `data_video.mp4` with default settings:**
    ```bash
    ./qrvidencoder -inputFile mydata.txt -outputFile data_video.mp4
    ```

2.  **Encode `archive.zip` with smaller chunks, higher QR recovery, and specific video settings:**
    ```bash
    ./qrvidencoder \
        -inputFile archive.zip \
        -outputFile archive_qr.mp4 \
        -chunkSize 512 \
        -qrLevel H \
        -qrSize 512 \
        -fps 5 \
        -framesPerQR 2 \
        -resolution 512x512
    ```
    In this example, each QR code (512x512px) will represent 512 bytes of `archive.zip`. The video will be 5 FPS, and each QR code will be displayed for 2 frames (i.e., 2/5 = 0.4 seconds).

## How it Works

1.  The input file is read.
2.  The data is split into chunks based on the `-chunkSize`.
3.  The data is split into chunks based on the `-chunkSize`. The `chunkSize` refers to the amount of original data per chunk.
4.  For each chunk of original data:
    a.  A 4-byte sequence number (0-indexed, BigEndian) is prepended.
    b.  An 8-byte XXH64 checksum (BigEndian) of `[sequence_number_bytes][original_data_chunk_bytes]` is calculated and prepended to that.
    c.  The final payload for the QR code is `[checksum_bytes][sequence_number_bytes][original_data_chunk_bytes]`. This means each QR code carries an additional 12 bytes of metadata beyond the `chunkSize`.
    d.  A QR code is generated for this final payload using the specified parameters (`-qrLevel`). The library automatically includes a standard quiet zone/border.
    e.  The QR code is saved as a PNG image of size `-qrSize` x `-qrSize` in a temporary directory.
5.  `ffmpeg` is used to compile these PNG images into a video:
    *   The `-framerate` option for `ffmpeg`'s input images is calculated as `fps / framesPerQR`. This determines how long each unique QR image is shown.
    *   The output video uses the `-fps` and `-resolution` specified.
5.  The temporary directory containing PNG images is deleted.

## To Decode (Manual Process Idea)

This program only handles encoding. Decoding would involve:

1.  Extracting frames from the video (e.g., using `ffmpeg`).
2.  Using a QR code scanner/library on each unique frame to get the data.
3.  Concatenating the data from all chunks in the correct order.

This is not implemented here.
```
