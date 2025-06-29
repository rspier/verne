package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"encoding/binary" // For converting sequence number to bytes

	"github.com/cespare/xxhash/v2" // For XXH64 checksum
	qrcode "github.com/skip2/go-qrcode"
)

const (
	// Size of the XXH64 checksum in bytes
	checksumSize = 8
	// Size of the sequence number (uint32) in bytes
	sequenceNumberSize = 4
	// Total metadata size per chunk
	metadataHeaderSize = checksumSize + sequenceNumberSize
)

// Enum for QR Code recovery level
type recoveryLevelVar qrcode.RecoveryLevel

func (r *recoveryLevelVar) String() string {
	switch qrcode.RecoveryLevel(*r) {
	case qrcode.Low:
		return "L"
	case qrcode.Medium:
		return "M"
	case qrcode.High:
		return "H"
	case qrcode.Highest:
		return "Q" // Note: library uses Highest for Q
	}
	return "M" // Default
}

func (r *recoveryLevelVar) Set(value string) error {
	switch value {
	case "L":
		*r = recoveryLevelVar(qrcode.Low)
	case "M":
		*r = recoveryLevelVar(qrcode.Medium)
	case "H":
		*r = recoveryLevelVar(qrcode.High)
	case "Q":
		*r = recoveryLevelVar(qrcode.Highest)
	default:
		return fmt.Errorf("invalid QR recovery level: %s. Must be one of L, M, Q, H", value)
	}
	return nil
}

var (
	inputFile   string
	outputFile  string
	chunkSize   int
	// qrQuietZone int // Removed as library handles quiet zone automatically
	qrLevelFlag recoveryLevelVar = recoveryLevelVar(qrcode.Medium) // Default
	qrSize      int
	fps         int
	framesPerQR int
	resolution  string
)

func main() {
	flag.StringVar(&inputFile, "inputFile", "", "Path to the input file (required)")
	flag.StringVar(&outputFile, "outputFile", "output.mp4", "Path to the output video file")
	flag.IntVar(&chunkSize, "chunkSize", 1024, "Size of data chunks in bytes")
	// flag.IntVar(&qrQuietZone, "qrQuietZone", 4, "QR code quiet zone") // Removed
	flag.Var(&qrLevelFlag, "qrLevel", "QR code recovery level (L, M, Q, H)")
	flag.IntVar(&qrSize, "qrSize", 256, "QR code image size in pixels")
	flag.IntVar(&fps, "fps", 1, "Frames per second for the output video")
	flag.IntVar(&framesPerQR, "framesPerQR", 1, "Number of video frames each QR code is displayed for")
	flag.StringVar(&resolution, "resolution", "256x256", "Video resolution (e.g., \"1920x1080\")")

	flag.Parse()

	if inputFile == "" {
		fmt.Println("Error: inputFile is required.")
		flag.Usage()
		os.Exit(1)
	}

	// Map qrLevelFlag to the library's type
	qrRecoveryLevel := qrcode.RecoveryLevel(qrLevelFlag)

	fmt.Println("QR Code Video Encoder")
	fmt.Printf("Input File: %s\n", inputFile)
	fmt.Printf("Output File: %s\n", outputFile)
	fmt.Printf("Chunk Size: %d bytes\n", chunkSize)
	// fmt.Printf("QR Quiet Zone: %d\n", qrQuietZone) // Removed
	fmt.Printf("QR Recovery Level: %s\n", qrLevelFlag.String()) // Use the String() method for display
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
	for i, originalChunk := range chunks {
		sequenceNum := uint32(i) // Using 0-based indexing for sequence number

		// Prepare sequence number bytes (BigEndian)
		seqNumBytes := make([]byte, sequenceNumberSize)
		binary.BigEndian.PutUint32(seqNumBytes, sequenceNum)

		// Data to be checksummed: sequence number + original data chunk
		dataToChecksum := append(seqNumBytes, originalChunk...)

		// Calculate XXH64 checksum
		digest := xxhash.Sum64(dataToChecksum)
		checksumBytes := make([]byte, checksumSize)
		binary.BigEndian.PutUint64(checksumBytes, digest)

		// Final payload for QR code: checksum + sequence number + original data
		finalPayload := append(checksumBytes, dataToChecksum...)

		if len(finalPayload) > qrSize*qrSize { // A very rough check, actual QR capacity is complex
			fmt.Fprintf(os.Stderr, "Warning: payload size for chunk %d (%d bytes) might be too large for QR code parameters. QR Capacity depends on version and error correction level.\n", i, len(finalPayload))
		}

		// Generate QR code for this finalPayload
		// Note: The QR code library expects a string. For binary data, this is okay
		// as long as the decoder interprets it as bytes.
		qr, err := qrcode.New(string(finalPayload), qrRecoveryLevel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating QR code for chunk %d (seq %d): %v\n", i, sequenceNum, err)
			continue
		}
		qr.DisableBorder = false // Ensure the standard border/quiet zone is active.

		// Save QR code as a PNG file
		frameFileName := filepath.Join(tempDir, fmt.Sprintf("qr_frame_%04d.png", i))
		err = qr.WriteFile(qrSize, frameFileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing QR code PNG for chunk %d (seq %d) to %s: %v\n", i, sequenceNum, frameFileName, err)
			continue
		}
		imageFilePaths = append(imageFilePaths, frameFileName)
		fmt.Printf("Generated QR code for chunk %d (seq %d), payload size %d bytes: %s\n", i, sequenceNum, len(finalPayload), frameFileName)
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

	cmd := exec.Command("ffmpeg", ffmpegArgs...)
	fmt.Printf("Executing ffmpeg command: ffmpeg %s\n", strings.Join(ffmpegArgs, " "))

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
