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
	// inputFile string // Removed, will be taken from positional args
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

func runEncoderApp(cmdLineArgs []string) error {
	encoderFlags := flag.NewFlagSet("qrvidencode", flag.ContinueOnError) // ContinueOnError to allow error handling

	// Define flags for this specific command set
	// Need to re-declare variables here if they are to be scoped to this function,
	// or pass them in / return them, or keep them as package-level vars (current approach).
	// For now, using package-level vars for simplicity of this refactor step.
	// If they were local, they'd be:
	// var localOutputFile string
	// encoderFlags.StringVar(&localOutputFile, "out", "output.mp4", "Path to the output video file")
	// ... and so on for all flags. Then use localOutputFile etc.
	// This would be cleaner but a larger diff. Sticking to package vars for now.

	encoderFlags.StringVar(&outputFile, "out", "output.mp4", "Path to the output video file")
	encoderFlags.IntVar(&chunkSize, "chunkSize", 1024, "Size of original data chunks in bytes (metadata will be added)")
	encoderFlags.IntVar(&qrSize, "qrSize", 256, "QR code image size in pixels (width and height)")
	encoderFlags.IntVar(&fps, "fps", 1, "Frames per second for the output video")
	encoderFlags.IntVar(&framesPerQR, "framesPerQR", 1, "Number of video frames each QR code is displayed for")
	encoderFlags.StringVar(&resolution, "resolution", "256x256", "Video resolution (e.g., \"1920x1080\")")

	// Parse flags from cmdLineArgs (excluding program name)
	err := encoderFlags.Parse(cmdLineArgs[1:])
	if err != nil {
		// FlagSet with ContinueOnError will print usage and return err.
		// We might want to wrap this error or let it propagate.
		return fmt.Errorf("error parsing flags: %w", err)
	}

	args := encoderFlags.Args()
	if len(args) != 1 {
		// To provide usage information similar to flag.Usage(), we might need to
		// capture FlagSet's output or manually construct a usage string.
		// For now, a simple error.
		errorBuf := new(bytes.Buffer)
		encoderFlags.SetOutput(errorBuf)
		encoderFlags.Usage()
		return fmt.Errorf("exactly one positional argument (the input file path) is required. Usage:\n%s", errorBuf.String())
	}
	inputFileArg := args[0]

	ffmpegPath, err := findFFmpegExecutable()
	if err != nil {
		// Error message already good from findFFmpegExecutable, just need to return it
		return fmt.Errorf("failed to find ffmpeg: %w. Please ensure ffmpeg is installed and in your PATH, or accessible at /usr/bin/ffmpeg", err)
	}
	fmt.Printf("Using ffmpeg executable at: %s\n", ffmpegPath)


	// qrRecoveryLevel := qr.M // Defaulting to Medium. Add flag if needed.

	fmt.Println("QR Code Video Encoder")
	fmt.Printf("Input File: %s\n", inputFileArg)
	fmt.Printf("Output File (--out): %s\n", outputFile)
	fmt.Printf("Chunk Size (--chunkSize): %d bytes\n", chunkSize)
	fmt.Printf("QR Error Correction Level: M (default)\n")
	fmt.Printf("QR Size (--qrSize): %dpx\n", qrSize)
	fmt.Printf("FPS (--fps): %d\n", fps)
	fmt.Printf("Frames per QR (--framesPerQR): %d\n", framesPerQR)
	fmt.Printf("Resolution (--resolution): %s\n", resolution)

	data, err := os.ReadFile(inputFileArg)
	if err != nil {
		return fmt.Errorf("error reading input file %s: %w", inputFileArg, err)
	}

	var chunks [][]byte
	if len(data) > 0 { // Ensure chunking only happens if data exists
		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunks = append(chunks, data[i:end])
		}
	}


	fmt.Printf("Read %d bytes from %s, split into %d chunks.\n", len(data), inputFileArg, len(chunks))

	if len(chunks) == 0 && len(data) > 0 { // If data exists but no chunks (e.g. chunkSize is huge)
        fmt.Println("Warning: Input data present but resulted in zero chunks. Check chunkSize.")
        // Or, if data itself was empty, this is fine.
	} else if len(chunks) == 0 && len(data) == 0 {
        fmt.Println("No data to encode (input file was empty). Exiting.")
		return nil // Successful exit for empty input
    }


	tempDir, err := os.MkdirTemp("", "qrvid_frames_")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	fmt.Printf("Using temporary directory for frames: %s\n", tempDir)

	imageFilePaths := []string{}
	for i, originalChunkData := range chunks {
		header := ChunkHeader{
			SequenceNum: uint32(i),
			DataLength:  uint32(len(originalChunkData)),
		}

		headerForChecksumBytes := make([]byte, 4+4)
		binary.BigEndian.PutUint32(headerForChecksumBytes[0:4], header.SequenceNum)
		binary.BigEndian.PutUint32(headerForChecksumBytes[4:8], header.DataLength)

		dataToChecksum := append(headerForChecksumBytes, originalChunkData...)
		header.Checksum = xxhash.Sum64(dataToChecksum)

		qrPayloadBuffer := new(bytes.Buffer)
		err := binary.Write(qrPayloadBuffer, binary.BigEndian, &header)
		if err != nil {
			// Log error and continue, might result in partial video if some chunks fail
			fmt.Fprintf(os.Stderr, "Error writing header to buffer for chunk %d (seq %d): %v. Skipping chunk.\n", i, header.SequenceNum, err)
			continue
		}
		qrPayloadBuffer.Write(originalChunkData)
		finalPayloadBytes := qrPayloadBuffer.Bytes()

		hexPayload := hex.EncodeToString(finalPayloadBytes)

		qrCode, err := qr.Encode(hexPayload, qr.M, qr.Auto)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating QR code for chunk %d (seq %d): %v. Skipping chunk.\n", i, header.SequenceNum, err)
			continue
		}

		scaledQrCode, err := barcode.Scale(qrCode, qrSize, qrSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scaling QR code for chunk %d (seq %d): %v. Skipping chunk.\n", i, header.SequenceNum, err)
			continue
		}

		frameFileName := filepath.Join(tempDir, fmt.Sprintf("qr_frame_%04d.png", i))
		file, err := os.Create(frameFileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating PNG file for chunk %d (seq %d): %v. Skipping chunk.\n", i, header.SequenceNum, err)
			continue
		}
		encodeErr := png.Encode(file, scaledQrCode)
		// It's important to close the file descriptor, regardless of png.Encode outcome.
		if closeErr := file.Close(); closeErr != nil && encodeErr == nil { // Report closeErr only if png.Encode succeeded
			fmt.Fprintf(os.Stderr, "Error closing PNG file for chunk %d (seq %d) %s: %v. QR data might be written.\n", i, header.SequenceNum, frameFileName, closeErr)
			// Continue as QR might be fine, but this is a warning.
		}
		if encodeErr != nil {
			fmt.Fprintf(os.Stderr, "Error writing QR code PNG for chunk %d (seq %d) to %s: %v. Skipping chunk.\n", i, header.SequenceNum, frameFileName, encodeErr)
			continue
		}

		imageFilePaths = append(imageFilePaths, frameFileName)
		fmt.Printf("Generated QR code for chunk %d (seq %d), hex payload size %d chars: %s\n", i, header.SequenceNum, len(hexPayload), frameFileName)
	}

	if len(imageFilePaths) == 0 {
		return fmt.Errorf("no QR code images were successfully generated. Cannot create video")
	}

	fmt.Println("Starting video encoding with ffmpeg...")
	ffmpegInputFPS := float64(fps) / float64(framesPerQR)
	ffmpegArgs := []string{
		"-y",
		"-framerate", strconv.FormatFloat(ffmpegInputFPS, 'f', -1, 64),
		"-i", filepath.Join(tempDir, "qr_frame_%04d.png"),
		"-c:v", "libx264",
		"-r", strconv.Itoa(fps),
		"-pix_fmt", "yuv420p",
	}

	if _, _, err := parseResolution(resolution); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid resolution format '%s': %v. Using default %dx%d\n", resolution, err, qrSize, qrSize)
		ffmpegArgs = append(ffmpegArgs, "-s", fmt.Sprintf("%dx%d", qrSize, qrSize))
	} else {
		ffmpegArgs = append(ffmpegArgs, "-s", resolution)
	}
	ffmpegArgs = append(ffmpegArgs, outputFile)

	cmd := exec.Command(ffmpegPath, ffmpegArgs...)
	fmt.Printf("Executing ffmpeg command: %s %s\n", ffmpegPath, strings.Join(ffmpegArgs, " "))

	output, errCmd := cmd.CombinedOutput()
	if errCmd != nil {
		return fmt.Errorf("error running ffmpeg: %w\nffmpeg output:\n%s", errCmd, string(output))
	}

	fmt.Printf("Video encoding successful. Output written to %s\n", outputFile)
	fmt.Printf("ffmpeg output:\n%s\n", string(output))
	return nil
}

func main() {
	if err := runEncoderApp(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
