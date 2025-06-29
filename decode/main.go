package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"image"
	_ "image/png" // Needed for image.Decode to recognize PNGs
	"sort"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

var (
	inputFile         string
	outputFile        string
	tempDirPrefix     string
	framesToSkip      int
	maxFramesToProcess int
)

func main() {
	flag.StringVar(&inputFile, "inputFile", "", "Path to the input video file (required)")
	flag.StringVar(&outputFile, "outputFile", "", "Path to the output file for decoded data (required)")
	flag.StringVar(&tempDirPrefix, "tempDirPrefix", "qrvid_decode_frames_", "Prefix for temporary directory to store extracted frames")
	flag.IntVar(&framesToSkip, "framesToSkip", 0, "Number of initial frames to skip in the video")
	flag.IntVar(&maxFramesToProcess, "maxFramesToProcess", 0, "Maximum number of frames to process after skipping (0 for all)")

	flag.Parse()

	if inputFile == "" || outputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -inputFile and -outputFile are required.")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Println("QR Video Decoder")
	fmt.Printf("Input Video File: %s\n", inputFile)
	fmt.Printf("Output Data File: %s\n", outputFile)
	fmt.Printf("Temp Dir Prefix: %s\n", tempDirPrefix)
	fmt.Printf("Frames to Skip: %d\n", framesToSkip)
	if maxFramesToProcess > 0 {
		fmt.Printf("Max Frames to Process: %d\n", maxFramesToProcess)
	} else {
		fmt.Println("Max Frames to Process: All")
	}

	// Create a temporary directory for extracted frames
	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temporary directory: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		fmt.Printf("Cleaning up temporary directory: %s\n", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary directory %s: %v\n", tempDir, err)
		}
	}()
	fmt.Printf("Using temporary directory for frames: %s\n", tempDir)

	// --- Frame Extraction using ffmpeg ---
	fmt.Println("Starting frame extraction with ffmpeg...")

	ffmpegArgs := []string{
		"-i", inputFile,
	}

	// Frame filtering
	// Select frames greater than or equal to framesToSkip.
	// If maxFramesToProcess is set, limit the number of output frames.
	var vfOptions []string
	if framesToSkip > 0 {
		vfOptions = append(vfOptions, fmt.Sprintf("select='gte(n\\,%d)'", framesToSkip))
	}

	if len(vfOptions) > 0 {
		ffmpegArgs = append(ffmpegArgs, "-vf", strings.Join(vfOptions, ","))
	}

	// If maxFramesToProcess is set, apply it using the -frames:v option.
	// This option should come after -vf according to some ffmpeg documentation,
	// or it might apply to the input depending on version.
	// It's generally safer to apply it as an output option.
	if maxFramesToProcess > 0 {
		ffmpegArgs = append(ffmpegArgs, "-frames:v", strconv.Itoa(maxFramesToProcess))
	}

	ffmpegArgs = append(ffmpegArgs, filepath.Join(tempDir, "frame_%06d.png"))

	cmd := exec.Command("ffmpeg", ffmpegArgs...)
	fmt.Printf("Executing ffmpeg command: ffmpeg %s\n", strings.Join(cmd.Args, " ")) // Use cmd.Args for safety

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running ffmpeg for frame extraction: %v\n", err)
		fmt.Fprintf(os.Stderr, "ffmpeg output:\n%s\n", string(outputBytes))
		os.Exit(1)
	}

	fmt.Printf("Frame extraction successful. Frames saved in %s\n", tempDir)
	// For debugging, print ffmpeg output if needed, but can be verbose
	// fmt.Printf("ffmpeg output:\n%s\n", string(outputBytes))

	// --- QR Code Decoding ---
	fmt.Println("Starting QR code decoding from extracted frames...")

	// List extracted frames
	framePattern := filepath.Join(tempDir, "frame_*.png")
	extractedFrames, err := filepath.Glob(framePattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing extracted frames from %s: %v\n", tempDir, err)
		os.Exit(1)
	}
	if len(extractedFrames) == 0 {
		fmt.Fprintf(os.Stderr, "No frames found in %s. ffmpeg might not have extracted any images.\n", tempDir)
		os.Exit(1)
	}
	// Sort frames to ensure correct order, Glob doesn't guarantee order
	sort.Strings(extractedFrames)

	fmt.Printf("Found %d frames to process for QR decoding.\n", len(extractedFrames))

	var decodedPayloads []string
	var lastSuccessfullyDecodedPayload string = "" // Initialize to a value that won't match any valid QR content initially

	for i, framePath := range extractedFrames {
		// Open image file
		imgFile, err := os.Open(framePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open frame image %s: %v. Skipping.\n", framePath, err)
			continue
		}

		img, _, err := image.Decode(imgFile)
		if err != nil {
			imgFile.Close()
			fmt.Fprintf(os.Stderr, "Warning: could not decode image format for %s: %v. Skipping.\n", framePath, err)
			continue
		}
		imgFile.Close()

		// Prepare for ZXing
		bmp, err := gozxing.NewBinaryBitmapFromImage(img)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create binary bitmap for %s: %v. Skipping.\n", framePath, err)
			continue
		}

		// Decode QR Code
		qrReader := qrcode.NewQRCodeReader()
		result, err := qrReader.Decode(bmp, nil) // No hints needed for now

		if err != nil {
			// This is common if a frame doesn't have a QR code or it's not scannable
			// fmt.Printf("Frame %d (%s): No QR code found or failed to decode: %v\n", i+1, filepath.Base(framePath), err)
			continue
		}

		currentPayload := result.GetText()
		// Add payload only if it's different from the last successfully decoded one
		if i == 0 || currentPayload != lastSuccessfullyDecodedPayload { // Always add the first successful one
			decodedPayloads = append(decodedPayloads, currentPayload)
			lastSuccessfullyDecodedPayload = currentPayload
			fmt.Printf("Frame %d (%s): Decoded QR successfully. Data length: %d. New unique data.\n", i+1, filepath.Base(framePath), len(currentPayload))
		} else {
			fmt.Printf("Frame %d (%s): Decoded QR successfully. Data is same as previous. Skipping.\n", i+1, filepath.Base(framePath))
		}
	}

	if len(decodedPayloads) == 0 {
		fmt.Println("No QR codes were successfully decoded from any frames.")
		// Decide if this is an error or just an empty output case
		// For now, let it proceed to write an empty file if that's the case.
	}

	fmt.Printf("Total unique QR code payloads decoded: %d\n", len(decodedPayloads))

	// --- Data Aggregation and Output ---
	if len(decodedPayloads) > 0 {
		fullDecodedData := strings.Join(decodedPayloads, "")
		err = os.WriteFile(outputFile, []byte(fullDecodedData), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing decoded data to output file %s: %v\n", outputFile, err)
			os.Exit(1)
		}
		fmt.Printf("Successfully wrote %d bytes of decoded data to %s\n", len(fullDecodedData), outputFile)
	} else {
		// If no payloads, write an empty file or log a message
		// Current behavior: if no QR codes decoded, it means an empty file will be written (if it doesn't exist)
		// or an existing file might be truncated if WriteFile is used.
		// Let's ensure an empty file is created if it doesn't exist.
		fmt.Println("No data decoded, writing an empty output file.")
		err = os.WriteFile(outputFile, []byte{}, 0644) // Write empty byte slice
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing empty output file %s: %v\n", outputFile, err)
			os.Exit(1) // Exit if we can't even write an empty file
		}
	}

	fmt.Println("Decoder finished.")
}
