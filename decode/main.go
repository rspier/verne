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
	"encoding/ascii85"// For ASCII85 decoding
	"bytes"           // For bytes.Buffer
	"io"              // For io.ReadFull

	"github.com/cespare/xxhash/v2" // For XXH64 checksum
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"compress/gzip" // For Gzip decompression
)

// ChunkHeader defines the metadata prepended to each data chunk.
// Note: The order of fields matters for serialization.
// This must be identical to the definition in the encoder.
// Order: Checksum, TotalChunks, SequenceNum, DataLength
type ChunkHeader struct {
	Checksum    uint64 // XXH64 checksum
	TotalChunks uint16 // Total number of chunks in the transmission (max 65535)
	SequenceNum uint16 // Sequence number of this chunk (0-indexed, max 65535)
	DataLength  uint16 // Length of the OriginalData part of this chunk (max 65535)
}

// Actual size of header when serialized:
// 8 (Checksum) + 2 (TotalChunks) + 2 (SequenceNum) + 2 (DataLength) = 14 bytes
const newHeaderSize = 14

var (
	inputFile         string
	outputFile        string
	tempDirPrefix     string
	framesToSkip      int
	maxFramesToProcess int
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
			if !info.IsDir() && (info.Mode()&0111 != 0) { // Check if executable by user/group/other
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("ffmpeg not found in PATH or common locations (%s): %w", strings.Join(commonPaths, ", "), err) // return original LookPath error
}

func main() {
	flag.StringVar(&inputFile, "inputFile", "", "Path to the input video file (required)")
	flag.StringVar(&outputFile, "outputFile", "", "Path to the output file for decoded data (required)")
	flag.StringVar(&tempDirPrefix, "tempDirPrefix", "qrvid_decode_frames_", "Prefix for temporary directory to store extracted frames")
	flag.IntVar(&framesToSkip, "framesToSkip", 0, "Number of initial frames to skip in the video")
	flag.IntVar(&maxFramesToProcess, "maxFramesToProcess", 0, "Maximum number of frames to process after skipping (0 for all)")

	flag.Parse()

	ffmpegPath, err := findFFmpegExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding ffmpeg: %v\n", err)
		fmt.Fprintln(os.Stderr, "Please ensure ffmpeg is installed and in your PATH, or accessible at /usr/bin/ffmpeg.")
		os.Exit(1)
	}
	fmt.Printf("Using ffmpeg executable at: %s\n", ffmpegPath)

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

	cmd := exec.Command(ffmpegPath, ffmpegArgs...) // Use found ffmpegPath
	fmt.Printf("Executing ffmpeg command: %s %s\n", ffmpegPath, strings.Join(cmd.Args, " "))

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

	// decodedChunks stores the original data part of valid chunks, keyed by sequence number (now uint16).
	decodedChunks := make(map[uint16][]byte)
	// seenRawPayloads helps in quickly skipping already processed identical raw QR payloads
	// that might appear on consecutive frames for the same data chunk.
	seenRawPayloads := make(map[string]bool)
	var maxSequenceNum uint16 = 0 // Keep track of the highest sequence number encountered.
	var knownTotalChunks uint16 = 0 // Will be set from the first valid chunk's header.
	firstChunkProcessed := false
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

		// Encoder now writes an ASCII85 string into the QR code.
		ascii85StringFromQR := result.GetText()

		// Deduplicate based on the ASCII85 string content of the QR code
		if _, seen := seenRawPayloads[ascii85StringFromQR]; seen {
			// fmt.Printf("Frame %d (%s): Duplicate raw QR payload (ASCII85 string) already processed. Skipping.\n", frameIdx+1, filepath.Base(framePath))
			continue
		}
		seenRawPayloads[ascii85StringFromQR] = true // Mark this raw payload as processed.

		// ASCII85-decode the content
		// Create a new reader for the ASCII85 string
		ascii85Reader := ascii85.NewDecoder(strings.NewReader(ascii85StringFromQR))
		rawPayloadBytes, err := io.ReadAll(ascii85Reader) // Requires Go 1.16+
		// If using older Go, use: rawPayloadBytes, err := ioutil.ReadAll(ascii85Reader) and import "io/ioutil"
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Failed to ASCII85-decode QR content: %v. Content: '%s'. Skipping.\n", frameIdx+1, filepath.Base(framePath), err, ascii85StringFromQR)
			continue
		}

		if len(rawPayloadBytes) < newHeaderSize { // newHeaderSize is now 14
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Decoded QR payload too short (%d bytes) for header. Min required: %d. Skipping.\n", frameIdx+1, filepath.Base(framePath), len(rawPayloadBytes), newHeaderSize)
			continue
		}

		reader := bytes.NewReader(rawPayloadBytes)
		var header ChunkHeader
		err = binary.Read(reader, binary.BigEndian, &header)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Failed to read chunk header: %v. Skipping.\n", frameIdx+1, filepath.Base(framePath), err)
			continue
		}

		if reader.Len() < int(header.DataLength) {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Payload data length mismatch. Header.DataLength=%d, remaining_payload_bytes=%d. Skipping chunk.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.DataLength, reader.Len())
			continue
		}

		originalData := make([]byte, header.DataLength)
		_, err = io.ReadFull(reader, originalData) // Ensure all expected bytes are read
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Failed to read original data (expected %d bytes): %v. Skipping.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.DataLength, err)
			continue
		}

		// Verify checksum: Checksum covers TotalChunks (2B) + SequenceNum (2B) + DataLength (2B) + OriginalData
		headerFieldsForChecksumBytes := make([]byte, 2+2+2) // For TotalChunks, SequenceNum, DataLength (all uint16)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[0:2], header.TotalChunks)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[2:4], header.SequenceNum)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[4:6], header.DataLength)

		dataThatWasChecksummed := append(headerFieldsForChecksumBytes, originalData...)
		calculatedChecksum := xxhash.Sum64(dataThatWasChecksummed)

		if calculatedChecksum != header.Checksum {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d, Total %d: Checksum mismatch! Expected %016x, got %016x. Discarding chunk.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.TotalChunks, header.Checksum, calculatedChecksum)
			continue
		}

		foundAnyValidChunk = true

		if !firstChunkProcessed {
			knownTotalChunks = header.TotalChunks
			if knownTotalChunks == 0 { // This case should ideally not occur with the new encoder for non-empty files
				fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Header reports TotalChunks as 0. Assembly will rely on max seen sequence number if this persists.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum)
			}
			firstChunkProcessed = true
			fmt.Printf("Frame %d (%s): First valid chunk. Expecting %d total chunks (Seq %d, DataLen %d).\n", frameIdx+1, filepath.Base(framePath), knownTotalChunks, header.SequenceNum, header.DataLength)
		} else if header.TotalChunks != knownTotalChunks {
			fmt.Fprintf(os.Stderr, "CRITICAL ERROR: Frame %d (%s), Seq %d: Inconsistent TotalChunks in header! Expected %d (from first chunk), got %d. Halting processing.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, knownTotalChunks, header.TotalChunks)
			os.Exit(1) // Exit due to corrupted/inconsistent stream
		}

		// Validate sequence number if knownTotalChunks is set and > 0
		if knownTotalChunks > 0 && header.SequenceNum >= knownTotalChunks {
			fmt.Fprintf(os.Stderr, "Error: Frame %d (%s): Sequence number %d is out of bounds (TotalChunks: %d). Discarding chunk.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, knownTotalChunks)
			continue // Skip this invalid chunk
		}

		if _, exists := decodedChunks[header.SequenceNum]; !exists {
			decodedChunks[header.SequenceNum] = originalData
			fmt.Printf("Frame %d (%s): Stored Seq %d (of %d), Checksum OK. Data len: %d. Total unique: %d\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, knownTotalChunks, len(originalData), len(decodedChunks))
		} else {
			// fmt.Printf("Frame %d (%s): Seq %d (Checksum OK) already stored. Ignoring duplicate.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum)
		}

		// Update maxSequenceNum for fallback assembly if knownTotalChunks remains 0
		if header.SequenceNum > maxSequenceNum {
			maxSequenceNum = header.SequenceNum
		}
	}

	if !foundAnyValidChunk {
		fmt.Println("No valid QR code chunks were successfully decoded from any frames.")
		if writeErr := os.WriteFile(outputFile, []byte{}, 0644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "Error writing empty output file %s: %v\n", outputFile, writeErr)
			os.Exit(1)
		}
		fmt.Println("Empty output file written.")
		os.Exit(0)
	}

	// Determine the number of chunks to assemble
	var numChunksToAssemble uint16 = knownTotalChunks // Use var to allow modification in fallback
	if !firstChunkProcessed || knownTotalChunks == 0 {
		fmt.Fprintf(os.Stderr, "Warning: Total number of chunks not definitively known from headers (or was 0). Assembling based on highest sequence number seen: %d.\n", maxSequenceNum)
		if !foundAnyValidChunk && maxSequenceNum == 0 { // No chunks found, maxSequenceNum is 0 by init
			numChunksToAssemble = 0
		} else {
			numChunksToAssemble = maxSequenceNum + 1
		}
	}

	fmt.Printf("Attempting to assemble %d chunks. Unique chunks found: %d.\n", numChunksToAssemble, len(decodedChunks))

	// --- Data Aggregation and Output ---
	var finalDataBuffer bytes.Buffer
	missingSequences := false
	if numChunksToAssemble == 0 && len(decodedChunks) == 0 { // Check if numChunksToAssemble is 0
		fmt.Println("No data to assemble.")
	} else {
		for i := uint16(0); i < numChunksToAssemble; i++ { // Iterate from 0 to numChunksToAssemble-1
			chunkData, ok := decodedChunks[i] // decodedChunks keys are uint32, but we are iterating with uint16 'i'. This needs care.
			                                   // The map keys should also be uint16 if sequence numbers are uint16.
			                                   // Let's assume decodedChunks keys are uint16.
			if !ok {
				fmt.Fprintf(os.Stderr, "Error: Missing data chunk for sequence number %d (expected %d total).\n", i, numChunksToAssemble)
				missingSequences = true
			} else {
				finalDataBuffer.Write(chunkData)
			}
		}
	}


	if missingSequences {
		fmt.Fprintf(os.Stderr, "Critical Error: One or more data chunks were missing. The decoded data is incomplete and likely corrupted.\n")
		// Optional: Could write partial data, but for now, let's emphasize the error.
		// To strictly prevent writing incomplete data if any chunk is missing based on knownTotalChunks:
		if firstChunkProcessed && knownTotalChunks > 0 { // Only be this strict if we had a valid TotalChunks count
			fmt.Println("Output file will not be written due to missing chunks based on header's TotalChunks count.")
			os.Exit(1)
		}
	}

	// At this point, finalDataBuffer contains the reassembled Gzipped data.
	// Decompress it.
	fmt.Printf("Reassembled %d bytes of gzipped data. Decompressing with gzip...\n", finalDataBuffer.Len())

	gzipReader, err := gzip.NewReader(bytes.NewReader(finalDataBuffer.Bytes()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating gzip reader: %v\n", err)
		os.Exit(1)
	}
	defer gzipReader.Close()

	decompressedData, err := io.ReadAll(gzipReader) // Requires Go 1.16+
	// If using older Go, use: decompressedData, err := ioutil.ReadAll(gzipReader) and import "io/ioutil"
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decompressing data with gzip: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Decompressed data size: %d bytes.\n", len(decompressedData))

	err = os.WriteFile(outputFile, decompressedData, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing decompressed data to output file %s: %v\n", outputFile, err)
		os.Exit(1)
	}

	if missingSequences {
		fmt.Printf("Wrote %d bytes of decompressed data to %s, BUT THE ORIGINAL GZIPPED STREAM WAS INCOMPLETE due to missing sequence numbers.\n", len(decompressedData), outputFile)
	} else {
		fmt.Printf("Successfully wrote %d bytes of decompressed data to %s\n", len(decompressedData), outputFile)
	}

	fmt.Println("Decoder finished.")
}
