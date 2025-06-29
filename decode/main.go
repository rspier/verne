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
	"encoding/binary" // For parsing sequence number and checksum
	"bytes"           // For bytes.Buffer

	"github.com/cespare/xxhash/v2" // For XXH64 checksum
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

const (
	// Size of the XXH64 checksum in bytes
	checksumSize = 8
	// Size of the sequence number (uint32) in bytes
	sequenceNumberSize = 4
	// Total metadata size per chunk
	metadataHeaderSize = checksumSize + sequenceNumberSize
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
		// If no frames, it means no data, so write an empty output file.
		if writeErr := os.WriteFile(outputFile, []byte{}, 0644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "Error writing empty output file %s: %v\n", outputFile, writeErr)
		} else {
			fmt.Println("No frames extracted, empty output file written.")
		}
		os.Exit(0) // Successful exit, as there was no data to process.
	}
	// Sort frames to ensure correct order, Glob doesn't guarantee order
	sort.Strings(extractedFrames)

	fmt.Printf("Found %d frames to process for QR decoding.\n", len(extractedFrames))

	// decodedChunks stores the original data part of valid chunks, keyed by sequence number.
	decodedChunks := make(map[uint32][]byte)
	// seenRawPayloads helps in quickly skipping already processed identical raw QR payloads
	// that might appear on consecutive frames for the same data chunk.
	seenRawPayloads := make(map[string]bool)
	var maxSequenceNum uint32 = 0 // Keep track of the highest sequence number encountered for ordered reconstruction.
	                               // Initialized to 0, assuming sequence numbers are >= 0.
	foundAnyValidChunk := false

	for frameIdx, framePath := range extractedFrames {
		imgFile, ferr := os.Open(framePath)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open frame image %s: %v. Skipping.\n", framePath, ferr)
			continue
		}

		img, _, derr := image.Decode(imgFile)
		imgFile.Close() // Close file immediately after decode attempt
		if derr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not decode image format for %s: %v. Skipping.\n", framePath, derr)
			continue
		}

		bmp, berr := gozxing.NewBinaryBitmapFromImage(img)
		if berr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create binary bitmap for %s: %v. Skipping.\n", framePath, berr)
			continue
		}

		qrReader := qrcode.NewQRCodeReader()
		result, qrerr := qrReader.Decode(bmp, nil) // No hints needed for now

		if qrerr != nil {
			// This is common if a frame doesn't have a QR code or it's not scannable
			// fmt.Printf("Frame %d (%s): No QR code found or failed to decode: %v\n", frameIdx+1, filepath.Base(framePath), qrerr)
			continue
		}

		rawPayloadString := result.GetText()
		if _, seen := seenRawPayloads[rawPayloadString]; seen {
			// fmt.Printf("Frame %d (%s): Duplicate raw QR payload already processed. Skipping.\n", frameIdx+1, filepath.Base(framePath))
			continue
		}
		seenRawPayloads[rawPayloadString] = true // Mark this raw payload as processed.

		rawPayloadBytes := []byte(rawPayloadString)

		if len(rawPayloadBytes) < metadataHeaderSize {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): QR payload too short (%d bytes) for metadata. Skipping.\n", frameIdx+1, filepath.Base(framePath), len(rawPayloadBytes))
			continue
		}

		receivedChecksumBytes := rawPayloadBytes[:checksumSize]
		sequenceNumBytes := rawPayloadBytes[checksumSize : metadataHeaderSize]
		originalData := rawPayloadBytes[metadataHeaderSize:]

		receivedChecksum := binary.BigEndian.Uint64(receivedChecksumBytes)
		sequenceNum := binary.BigEndian.Uint32(sequenceNumBytes)

		dataThatWasChecksummed := rawPayloadBytes[checksumSize:] // This is sequenceNumBytes + originalData
		calculatedDigest := xxhash.Sum64(dataThatWasChecksummed)

		if calculatedDigest != receivedChecksum {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Checksum mismatch! Expected %016x, got %016x. Discarding chunk.\n", frameIdx+1, filepath.Base(framePath), sequenceNum, receivedChecksum, calculatedDigest)
			continue
		}

		foundAnyValidChunk = true
		if _, exists := decodedChunks[sequenceNum]; !exists {
			decodedChunks[sequenceNum] = originalData
			fmt.Printf("Frame %d (%s): Stored Seq %d, Checksum OK. Data len: %d.\n", frameIdx+1, filepath.Base(framePath), sequenceNum, len(originalData))
		} else if len(originalData) > len(decodedChunks[sequenceNum]) {
			// Basic heuristic: if we see the same sequence number again and the data is longer,
			// maybe the previous one was truncated? This is unlikely with good QR codes
			// but could be a fallback. Or simply prefer first-seen. For now, prefer first.
			// To prefer first, this 'else if' block could be removed.
			// For now, let's just log if we see it again and it's different.
			// If we decide to overwrite, we should log that too.
			// Current logic: first one wins.
			fmt.Printf("Frame %d (%s): Seq %d (Checksum OK) already stored. Ignoring duplicate.\n", frameIdx+1, filepath.Base(framePath), sequenceNum)
		}


		if sequenceNum > maxSequenceNum {
			maxSequenceNum = sequenceNum
		}
	}

	if !foundAnyValidChunk {
		fmt.Println("No valid QR code chunks were successfully decoded from any frames.")
		// Write an empty output file.
		if writeErr := os.WriteFile(outputFile, []byte{}, 0644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "Error writing empty output file %s: %v\n", outputFile, writeErr)
			os.Exit(1)
		}
		fmt.Println("Empty output file written.")
		os.Exit(0)
	}

	fmt.Printf("Total unique, valid QR data chunks to assemble: %d (highest sequence number seen: %d)\n", len(decodedChunks), maxSequenceNum)

	// --- Data Aggregation and Output ---
	var finalDataBuffer bytes.Buffer // Use bytes.Buffer for efficient concatenation
	missingSequences := false
	for i := uint32(0); i <= maxSequenceNum; i++ {
		chunkData, ok := decodedChunks[i]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: Missing data chunk for sequence number %d.\n", i)
			missingSequences = true
		} else {
			finalDataBuffer.Write(chunkData)
		}
	}

	if missingSequences {
		fmt.Fprintf(os.Stderr, "Critical Error: One or more data chunks were missing. The decoded data is incomplete and likely corrupted. Output file will not be written or will be incomplete.\n")
		// Decide on behavior: exit, or write partial data.
		// For now, let's write what we have but ensure the user knows it's bad.
		// To prevent writing corrupted data, uncomment os.Exit(1)
		// os.Exit(1)
	}

	err = os.WriteFile(outputFile, finalDataBuffer.Bytes(), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing decoded data to output file %s: %v\n", outputFile, err)
		os.Exit(1)
	}

	if missingSequences {
		fmt.Printf("Wrote %d bytes to %s, but the data is incomplete due to missing sequence numbers.\n", finalDataBuffer.Len(), outputFile)
	} else {
		fmt.Printf("Successfully wrote %d bytes of decoded data to %s\n", finalDataBuffer.Len(), outputFile)
	}

	fmt.Println("Decoder finished.")
}
