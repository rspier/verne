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
	"encoding/hex"    // For hex decoding
	"bytes"           // For bytes.Buffer
	"io"              // For io.ReadFull

	"github.com/cespare/xxhash/v2" // For XXH64 checksum
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// ChunkHeader defines the metadata prepended to each data chunk.
// Note: The order of fields matters for serialization.
// This must be identical to the definition in the encoder.
type ChunkHeader struct {
	Checksum    uint64 // XXH64 checksum of (SequenceNum (4B) + DataLength (4B) + OriginalData)
	SequenceNum uint32 // Sequence number of the chunk
	DataLength  uint32 // Length of the OriginalData part
}

// Actual size of header when serialized: 8 (Checksum) + 4 (SequenceNum) + 4 (DataLength) = 16 bytes
const newHeaderSize = 16

var (
	// inputFile         string // Removed, will use positional arg
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

func runDecoderApp(cmdLineArgs []string) error {
	decoderFlags := flag.NewFlagSet("qrviddecode", flag.ContinueOnError)

	// Using package-level vars for flags for this refactor step, similar to encoder.
	decoderFlags.StringVar(&outputFile, "out", "", "Path to the output file for decoded data (required)")
	decoderFlags.StringVar(&tempDirPrefix, "tempDirPrefix", "qrvid_decode_frames_", "Prefix for temporary directory to store extracted frames")
	decoderFlags.IntVar(&framesToSkip, "framesToSkip", 0, "Number of initial frames to skip in the video")
	decoderFlags.IntVar(&maxFramesToProcess, "maxFramesToProcess", 0, "Maximum number of frames to process after skipping (0 for all)")

	err := decoderFlags.Parse(cmdLineArgs[1:])
	if err != nil {
		return fmt.Errorf("error parsing flags: %w", err)
	}

	args := decoderFlags.Args()
	if len(args) != 1 {
		errorBuf := new(bytes.Buffer)
		decoderFlags.SetOutput(errorBuf)
		decoderFlags.Usage()
		return fmt.Errorf("exactly one positional argument (the input video file path) is required. Usage:\n%s", errorBuf.String())
	}
	inputFileArg := args[0]

	if outputFile == "" { // inputFileArg already checked by len(args) != 1
		// This specific check for outputFile might be redundant if the flag has a default or is marked required by FlagSet.
		// However, explicit check after parsing is safer if defaults are empty strings.
		// For now, assuming "" means not provided if flag is optional and has no default, or user explicitly set it to "".
		// The current flag definition has `""` as default and implies it's required by the program logic later.
		errorBuf := new(bytes.Buffer)
		decoderFlags.SetOutput(errorBuf)
		decoderFlags.Usage()
		return fmt.Errorf("--out <output_file> is required. Usage:\n%s", errorBuf.String())
	}


	ffmpegPath, err := findFFmpegExecutable()
	if err != nil {
		return fmt.Errorf("failed to find ffmpeg: %w. Please ensure ffmpeg is installed and in your PATH, or accessible at /usr/bin/ffmpeg", err)
	}
	fmt.Printf("Using ffmpeg executable at: %s\n", ffmpegPath)


	fmt.Println("QR Video Decoder")
	fmt.Printf("Input Video File: %s\n", inputFileArg)
	fmt.Printf("Output Data File (--out): %s\n", outputFile)
	fmt.Printf("Temp Dir Prefix (--tempDirPrefix): %s\n", tempDirPrefix)
	fmt.Printf("Frames to Skip (--framesToSkip): %d\n", framesToSkip)
	if maxFramesToProcess > 0 {
		fmt.Printf("Max Frames to Process (--maxFramesToProcess): %d\n", maxFramesToProcess)
	} else {
		fmt.Println("Max Frames to Process: All")
	}

	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer func() {
		fmt.Printf("Cleaning up temporary directory: %s\n", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary directory %s: %v\n", tempDir, err)
		}
	}()
	fmt.Printf("Using temporary directory for frames: %s\n", tempDir)

	fmt.Println("Starting frame extraction with ffmpeg...")
	ffmpegArgs := []string{
		"-i", inputFileArg,
	}
	var vfOptions []string
	if framesToSkip > 0 {
		vfOptions = append(vfOptions, fmt.Sprintf("select='gte(n\\,%d)'", framesToSkip))
	}
	if len(vfOptions) > 0 {
		ffmpegArgs = append(ffmpegArgs, "-vf", strings.Join(vfOptions, ","))
	}
	if maxFramesToProcess > 0 {
		ffmpegArgs = append(ffmpegArgs, "-frames:v", strconv.Itoa(maxFramesToProcess))
	}
	ffmpegArgs = append(ffmpegArgs, filepath.Join(tempDir, "frame_%06d.png"))

	cmd := exec.Command(ffmpegPath, ffmpegArgs...)
	fmt.Printf("Executing ffmpeg command: %s %s\n", ffmpegPath, strings.Join(cmd.Args, " "))

	outputBytes, errCmd := cmd.CombinedOutput()
	if errCmd != nil {
		return fmt.Errorf("error running ffmpeg for frame extraction: %w\nffmpeg output:\n%s", errCmd, string(outputBytes))
	}
	fmt.Printf("Frame extraction successful. Frames saved in %s\n", tempDir)

	fmt.Println("Starting QR code decoding from extracted frames...")
	framePattern := filepath.Join(tempDir, "frame_*.png")
	extractedFrames, err := filepath.Glob(framePattern)
	if err != nil {
		return fmt.Errorf("error listing extracted frames from %s: %w", tempDir, err)
	}
	if len(extractedFrames) == 0 {
		fmt.Fprintf(os.Stderr, "No frames found in %s. ffmpeg might not have extracted any images.\n", tempDir)
		if writeErr := os.WriteFile(outputFile, []byte{}, 0644); writeErr != nil {
			return fmt.Errorf("error writing empty output file %s (no frames): %w", outputFile, writeErr)
		}
		fmt.Println("No frames extracted, empty output file written.")
		return nil
	}
	sort.Strings(extractedFrames)
	fmt.Printf("Found %d frames to process for QR decoding.\n", len(extractedFrames))

	decodedChunks := make(map[uint32][]byte)
	seenRawPayloads := make(map[string]bool)
	var maxSequenceNum uint32 = 0
	foundAnyValidChunk := false

	for frameIdx, framePath := range extractedFrames {
		imgFile, ferr := os.Open(framePath)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open frame image %s: %v. Skipping.\n", framePath, ferr)
			continue
		}
		img, _, derr := image.Decode(imgFile)
		imgFile.Close()
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
		result, qrerr := qrReader.Decode(bmp, nil)
		if qrerr != nil {
			continue
		}
		hexStringFromQR := result.GetText()
		if _, seen := seenRawPayloads[hexStringFromQR]; seen {
			continue
		}
		seenRawPayloads[hexStringFromQR] = true
		rawPayloadBytes, errHex := hex.DecodeString(hexStringFromQR)
		if errHex != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Failed to hex-decode QR content: %v. Content: '%s'. Skipping.\n", frameIdx+1, filepath.Base(framePath), errHex, hexStringFromQR)
			continue
		}
		if len(rawPayloadBytes) < newHeaderSize {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Decoded QR payload too short (%d bytes) for header. Min required: %d. Skipping.\n", frameIdx+1, filepath.Base(framePath), len(rawPayloadBytes), newHeaderSize)
			continue
		}
		reader := bytes.NewReader(rawPayloadBytes)
		var header ChunkHeader
		errReadHeader := binary.Read(reader, binary.BigEndian, &header)
		if errReadHeader != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s): Failed to read chunk header: %v. Skipping.\n", frameIdx+1, filepath.Base(framePath), errReadHeader)
			continue
		}
		if reader.Len() < int(header.DataLength) {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Payload data length mismatch. Header.DataLength=%d, remaining_payload_bytes=%d. Skipping chunk.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.DataLength, reader.Len())
			continue
		}
		originalData := make([]byte, header.DataLength)
		_, errReadData := io.ReadFull(reader, originalData)
		if errReadData != nil {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Failed to read original data (expected %d bytes): %v. Skipping.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.DataLength, errReadData)
			continue
		}
		headerForChecksumBytes := make([]byte, 4+4)
		binary.BigEndian.PutUint32(headerForChecksumBytes[0:4], header.SequenceNum)
		binary.BigEndian.PutUint32(headerForChecksumBytes[4:8], header.DataLength)
		dataThatWasChecksummed := append(headerForChecksumBytes, originalData...)
		calculatedChecksum := xxhash.Sum64(dataThatWasChecksummed)
		if calculatedChecksum != header.Checksum {
			fmt.Fprintf(os.Stderr, "Warning: Frame %d (%s), Seq %d: Checksum mismatch! Expected %016x, got %016x. Discarding chunk.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, header.Checksum, calculatedChecksum)
			continue
		}
		foundAnyValidChunk = true
		if _, exists := decodedChunks[header.SequenceNum]; !exists {
			decodedChunks[header.SequenceNum] = originalData
			fmt.Printf("Frame %d (%s): Stored Seq %d, Checksum OK. Data len: %d.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum, len(originalData))
		} else {
			fmt.Printf("Frame %d (%s): Seq %d (Checksum OK) already stored. Ignoring duplicate.\n", frameIdx+1, filepath.Base(framePath), header.SequenceNum)
		}
		if header.SequenceNum > maxSequenceNum {
			maxSequenceNum = header.SequenceNum
		}
	}

	if !foundAnyValidChunk {
		fmt.Println("No valid QR code chunks were successfully decoded from any frames.")
		if writeErr := os.WriteFile(outputFile, []byte{}, 0644); writeErr != nil {
			return fmt.Errorf("error writing empty output file %s (no valid chunks): %w", outputFile, writeErr)
		}
		fmt.Println("Empty output file written.")
		return nil
	}

	fmt.Printf("Total unique, valid QR data chunks to assemble: %d (highest sequence number seen: %d)\n", len(decodedChunks), maxSequenceNum)
	var finalDataBuffer bytes.Buffer
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
		// Log critical error, but still write what we have. The error message will inform the user.
		fmt.Fprintf(os.Stderr, "Critical Error: One or more data chunks were missing. The decoded data is incomplete and likely corrupted.\n")
	}

	errWriteFile := os.WriteFile(outputFile, finalDataBuffer.Bytes(), 0644)
	if errWriteFile != nil {
		return fmt.Errorf("error writing decoded data to output file %s: %w", outputFile, errWriteFile)
	}

	if missingSequences {
		fmt.Printf("Wrote %d bytes to %s, but the data is incomplete due to missing sequence numbers.\n", finalDataBuffer.Len(), outputFile)
	} else {
		fmt.Printf("Successfully wrote %d bytes of decoded data to %s\n", finalDataBuffer.Len(), outputFile)
	}

	fmt.Println("Decoder finished.")
	return nil
}

func main() {
	if err := runDecoderApp(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
