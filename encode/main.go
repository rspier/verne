package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"bytes"           // For bytes.Buffer
	"encoding/binary" // For converting sequence number to bytes
	"encoding/hex"    // For hex encoding the payload
	"image/png"       // For saving QR code as PNG

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/cespare/xxhash/v2" // For XXH64 checksum
)

// ChunkHeader defines the metadata prepended to each data chunk.
// Note: The order of fields matters for serialization.
type ChunkHeader struct {
	Checksum    uint64 // XXH64 checksum of (SequenceNum (4B) + DataLength (4B) + OriginalData)
	SequenceNum uint32 // Sequence number of the chunk
	DataLength  uint32 // Length of the OriginalData part
}

// Recalculate static header size based on ChunkHeader struct using binary.Size
// We can't do this at package level easily without an instance.
// For now, let's define it manually, or calculate in main.
// Actual size of header when serialized: 8 (Checksum) + 4 (SequenceNum) + 4 (DataLength) = 16 bytes
const newHeaderSize = 16

// Note: qrLevelFlag and related types are removed as boombuler/barcode/qr uses constants like qr.M directly.
// We will use qr.M as a default for now. A new flag can be added later if customization is needed.

var (
	inputFile   string
	outputFile  string
	chunkSize   int
	qrSize      int
	fps         int
	framesPerQR int
	resolution  string
)

// findFFmpegExecutable attempts to find ffmpeg, first in PATH, then in common hardcoded locations.
func findFFmpegExecutable() (string, error) {
	// 1. Try PATH
	path, err := exec.LookPath("ffmpeg")
	if err == nil {
		return path, nil
	}

	// 2. Try common hardcoded paths
	commonPaths := []string{"/usr/bin/ffmpeg"} // Add more if needed, e.g., "/usr/local/bin/ffmpeg"
	for _, p := range commonPaths {
		info, err := os.Stat(p)
		if err == nil {
			// Check if it's a regular file and executable
			// Mode().IsRegular() is not available directly, check if not a dir and if executable by user
			if !info.IsDir() && (info.Mode()&0111 != 0) { // Check if executable by user/group/other
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("ffmpeg not found in PATH or common locations (%s): %w", strings.Join(commonPaths, ", "), err) // return original LookPath error
}


func main() {
	flag.StringVar(&inputFile, "inputFile", "", "Path to the input file (required)")
	flag.StringVar(&outputFile, "outputFile", "output.mp4", "Path to the output video file")
	flag.IntVar(&chunkSize, "chunkSize", 1024, "Size of original data chunks in bytes (metadata will be added)")
	// qrLevel flag removed for now, using default qr.M. Can be re-added if needed.
	flag.IntVar(&qrSize, "qrSize", 256, "QR code image size in pixels (width and height)")
	flag.IntVar(&fps, "fps", 1, "Frames per second for the output video")
	flag.IntVar(&framesPerQR, "framesPerQR", 1, "Number of video frames each QR code is displayed for")
	flag.StringVar(&resolution, "resolution", "256x256", "Video resolution (e.g., \"1920x1080\")")

	flag.Parse()

	ffmpegPath, err := findFFmpegExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding ffmpeg: %v\n", err)
		fmt.Fprintln(os.Stderr, "Please ensure ffmpeg is installed and in your PATH, or accessible at /usr/bin/ffmpeg.")
		os.Exit(1)
	}
	fmt.Printf("Using ffmpeg executable at: %s\n", ffmpegPath)


	if inputFile == "" {
		fmt.Println("Error: inputFile is required.")
		flag.Usage()
		os.Exit(1)
	}

	// qrRecoveryLevel := qr.M // Defaulting to Medium. Add flag if needed.

	fmt.Println("QR Code Video Encoder")
	fmt.Printf("Input File: %s\n", inputFile)
	fmt.Printf("Output File: %s\n", outputFile)
	fmt.Printf("Chunk Size (original data part): %d bytes\n", chunkSize)
	fmt.Printf("QR Error Correction Level: M (default)\n") // Placeholder
	fmt.Printf("QR Size: %dpx\n", qrSize)
	fmt.Printf("FPS: %d\n", fps)
	fmt.Printf("Frames per QR: %d\n", framesPerQR)
	fmt.Printf("Resolution: %s\n", resolution)
	// fmt.Printf("Actual QR Recovery Level for library: %v\n", qrRecoveryLevel) // For debugging

	// Read input file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file %s: %v\n", inputFile, err)
		os.Exit(1)
	}

	// Chunk data
	var chunks [][]byte
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}

	fmt.Printf("Read %d bytes from %s, split into %d chunks.\n", len(data), inputFile, len(chunks))

	if len(chunks) == 0 {
		fmt.Println("No data to encode. Exiting.")
		os.Exit(0)
	}

	// Create a temporary directory for QR code images
	tempDir, err := os.MkdirTemp("", "qrvid_frames_")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temporary directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir) // Clean up afterwards
	fmt.Printf("Using temporary directory for frames: %s\n", tempDir)

	// Generate and save QR code images
	imageFilePaths := []string{}
	for i, originalChunkData := range chunks {
		header := ChunkHeader{
			SequenceNum: uint32(i),
			DataLength:  uint32(len(originalChunkData)),
		}

		// Prepare data for checksum: SequenceNum (4B) + DataLength (4B) + OriginalData
		// Max size for this part of header is 4+4 = 8 bytes
		headerForChecksumBytes := make([]byte, 4+4)
		binary.BigEndian.PutUint32(headerForChecksumBytes[0:4], header.SequenceNum)
		binary.BigEndian.PutUint32(headerForChecksumBytes[4:8], header.DataLength)

		dataToChecksum := append(headerForChecksumBytes, originalChunkData...)
		header.Checksum = xxhash.Sum64(dataToChecksum)

		// Prepare final QR payload: Full Header (16B) + OriginalData
		qrPayloadBuffer := new(bytes.Buffer)
		err := binary.Write(qrPayloadBuffer, binary.BigEndian, &header)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing header to buffer for chunk %d (seq %d): %v\n", i, header.SequenceNum, err)
			continue
		}
		qrPayloadBuffer.Write(originalChunkData)
		finalPayloadBytes := qrPayloadBuffer.Bytes()

		// Note: boombuler/barcode/qr.Encode takes []byte directly for qr.Byte mode (auto-selected for []byte)
		// or a string. Forcing byte mode is best for binary.
		// The library will choose an appropriate QR code version automatically.
		// Hex-encode the binary payload to ensure it's compatible with QR string input,
		// even if the library has quirks with binary strings in byte mode.
		hexPayload := hex.EncodeToString(finalPayloadBytes)

		// Using qr.M for medium error correction.
		// qr.Auto should select Alphanumeric or Byte mode for a hex string.
		qrCode, err := qr.Encode(hexPayload, qr.M, qr.Auto)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating QR code for chunk %d (seq %d): %v\n", i, header.SequenceNum, err)
			continue
		}

		// Scale the barcode to the desired size
		scaledQrCode, err := barcode.Scale(qrCode, qrSize, qrSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scaling QR code for chunk %d (seq %d): %v\n", i, header.SequenceNum, err)
			continue
		}

		// Save QR code as a PNG file
		frameFileName := filepath.Join(tempDir, fmt.Sprintf("qr_frame_%04d.png", i))
		file, err := os.Create(frameFileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating PNG file for chunk %d (seq %d): %v\n", i, header.SequenceNum, err)
			continue
		}
		err = png.Encode(file, scaledQrCode)
		file.Close() // Close the file even if png.Encode fails, though it might be a bit late.
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing QR code PNG for chunk %d (seq %d) to %s: %v\n", i, header.SequenceNum, frameFileName, err)
			continue
		}

		imageFilePaths = append(imageFilePaths, frameFileName)
		// Log the size of the hexPayload string, as that's what's passed to qr.Encode
		fmt.Printf("Generated QR code for chunk %d (seq %d), hex payload size %d chars: %s\n", i, header.SequenceNum, len(hexPayload), frameFileName)
	}

	if len(imageFilePaths) == 0 {
		fmt.Fprintf(os.Stderr, "No QR code images were successfully generated. Cannot create video.\n")
		os.Exit(1)
	}

	// --- Video Encoding using ffmpeg ---
	fmt.Println("Starting video encoding with ffmpeg...")

	// Calculate ffmpeg's input frame rate.
	// If framesPerQR is 1, and video fps is 10, then each QR is shown for 1/10s.
	// ffmpeg's -r for image sequence means how many frames from the input sequence make up 1s of video.
	// Or, more simply, if each QR code image should last for `framesPerQR` video frames,
	// and the video output fps is `fps`, then the duration of one QR image is `framesPerQR / fps` seconds.
	// The input framerate for ffmpeg for the image sequence should be `1 / duration_of_one_image`.
	// So, `ffmpeg_input_fps = fps / framesPerQR`.
	ffmpegInputFPS := float64(fps) / float64(framesPerQR)

	// ffmpeg command arguments
	// Example: ffmpeg -framerate 1 -i tempdir/qr_frame_%04d.png -c:v libx264 -r 10 -pix_fmt yuv420p -s 256x256 output.mp4
	// -framerate <rate> : input frame rate for image sequence
	// -i <pattern> : input files
	// -c:v libx264 : video codec
	// -r <rate> : output video frame rate
	// -pix_fmt yuv420p : pixel format, good for compatibility
	// -s <WxH> : video size/resolution
	// -y : overwrite output file without asking

	ffmpegArgs := []string{
		"-y", // Overwrite output file
		"-framerate", strconv.FormatFloat(ffmpegInputFPS, 'f', -1, 64),
		"-i", filepath.Join(tempDir, "qr_frame_%04d.png"),
		"-c:v", "libx264",
		"-r", strconv.Itoa(fps),
		"-pix_fmt", "yuv420p",
	}

	// Validate and add resolution
	if _, _, err := parseResolution(resolution); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid resolution format '%s': %v. Using default %dx%d\n", resolution, err, qrSize, qrSize)
		ffmpegArgs = append(ffmpegArgs, "-s", fmt.Sprintf("%dx%d", qrSize, qrSize))
	} else {
		ffmpegArgs = append(ffmpegArgs, "-s", resolution)
	}

	ffmpegArgs = append(ffmpegArgs, outputFile)

	cmd := exec.Command(ffmpegPath, ffmpegArgs...) // Use found ffmpegPath
	fmt.Printf("Executing ffmpeg command: %s %s\n", ffmpegPath, strings.Join(ffmpegArgs, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running ffmpeg: %v\n", err)
		fmt.Fprintf(os.Stderr, "ffmpeg output:\n%s\n", string(output))
		os.Exit(1)
	}

	fmt.Printf("Video encoding successful. Output written to %s\n", outputFile)
	fmt.Printf("ffmpeg output:\n%s\n", string(output))
}

// parseResolution validates and parses a "WIDTHxHEIGHT" string.
func parseResolution(resStr string) (width int, height int, err error) {
	parts := strings.Split(strings.ToLower(resStr), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("resolution must be in format WIDTHxHEIGHT, got %s", resStr)
	}
	width, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid width: %s", parts[0])
	}
	height, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid height: %s", parts[1])
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("width and height must be positive, got %dx%d", width, height)
	}
	return width, height, nil
}
