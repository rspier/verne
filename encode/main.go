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
	"io"              // For io.Copy
	"encoding/hex"    // No longer used for QR payload
	"encoding/ascii85"// For ASCII85 encoding QR payload
	"image/png"       // For saving QR code as PNG
	"image"
	"image/color"
	"image/draw"      // For drawing image onto another
	"math/rand"
	"time"
	"math" // For math.MaxUint16

	"github.com/klauspost/compress/zstd" // For Zstandard compression
	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/cespare/xxhash/v2" // For XXH64 checksum
)

// ChunkHeader defines the metadata prepended to each data chunk.
// Note: The order of fields matters for serialization.
// Checksum is calculated over: TotalChunks (2B) + SequenceNum (2B) + DataLength (2B) + OriginalData
type ChunkHeader struct {
	Checksum    uint64 // XXH64 checksum
	TotalChunks uint16 // Total number of chunks in the transmission (max 65535)
	SequenceNum uint16 // Sequence number of this chunk (0-indexed, max 65535)
	DataLength  uint16 // Length of the OriginalData part of this chunk (max 65535 bytes)
}

// Actual size of header when serialized:
// 8 (Checksum) + 2 (TotalChunks) + 2 (SequenceNum) + 2 (DataLength) = 14 bytes
const newHeaderSize = 14

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
	crfValue    int
)

// Helper function to draw a dot (actually a small square)
func drawDot(img *image.RGBA, x, y int, c color.Color, dotSize int) {
	for i := 0; i < dotSize; i++ {
		for j := 0; j < dotSize; j++ {
			// Basic boundary check, though pattern generation should handle it
			if x+i >= 0 && x+i < img.Bounds().Dx() && y+j >= 0 && y+j < img.Bounds().Dy() {
				img.Set(x+i, y+j, c)
			}
		}
	}
}

// abs returns the absolute value of x.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// drawLine draws a line using a simple algorithm (modified Bresenham for 1px thickness).
func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	dx := abs(x2 - x1)
	dy := -abs(y2 - y1) // Adjusted for typical algorithm style with negative dy

	sx := 1
	if x1 > x2 {
		sx = -1
	}
	sy := 1
	if y1 > y2 {
		sy = -1
	}
	err := dx + dy // Error value e_xy

	for {
		if x1 >= 0 && x1 < img.Bounds().Dx() && y1 >= 0 && y1 < img.Bounds().Dy() {
			img.Set(x1, y1, c)
		}
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy { // e_xy+e_x > 0
			err += dy
			x1 += sx
		}
		if e2 <= dx { // e_xy+e_y < 0
			err += dx
			y1 += sy
		}
	}
}


const (
	dotDensityPer1000Px  = 8  // Number of dots per 1000 pixels of border area
	lineDensityPer1000Px = 3  // Number of lines per 1000 pixels of border area
	dotSizeVal           = 2  // px; Renamed from dotSize to avoid conflict with var
	minLineLength        = 5  // px
	maxLineLength        = 8  // px
)
var patternColor = color.Gray{Y: 200} // Light gray for pattern elements


// drawPatternInRect fills the specified rectangle with a random pattern of dots and lines
func drawPatternInRect(img *image.RGBA, rect image.Rectangle) {
	rectWidth := rect.Dx()
	rectHeight := rect.Dy()
	if rectWidth <= 0 || rectHeight <= 0 {
		return // Nothing to draw on
	}
	area := float64(rectWidth * rectHeight)

	// Calculate number of dots and lines, ensuring at least one if area is small but > 0
	numDots := int(area/1000.0*float64(dotDensityPer1000Px) + 0.5) // Add 0.5 for rounding
	if area > 0 && numDots == 0 && dotDensityPer1000Px > 0 {
		numDots = 1
	}
	numLines := int(area/1000.0*float64(lineDensityPer1000Px) + 0.5) // Add 0.5 for rounding
	if area > 0 && numLines == 0 && lineDensityPer1000Px > 0 {
		numLines = 1
	}


	// Draw dots
	for k := 0; k < numDots; k++ {
		if rectWidth-dotSizeVal < 0 || rectHeight-dotSizeVal < 0 { continue } // Avoid panic if rect is too small for dot
		randX := rect.Min.X + rand.Intn(rectWidth-dotSizeVal+1)
		randY := rect.Min.Y + rand.Intn(rectHeight-dotSizeVal+1)
		drawDot(img, randX, randY, patternColor, dotSizeVal)
	}

	// Draw lines
	for k := 0; k < numLines; k++ {
		lineLength := minLineLength + rand.Intn(maxLineLength-minLineLength+1)

		// Ensure rect is large enough for the line at all
		if rectWidth-lineLength < 0 || rectHeight-lineLength < 0 { continue }


		startX := rect.Min.X + rand.Intn(rectWidth-lineLength+1)
		startY := rect.Min.Y + rand.Intn(rectHeight-lineLength+1) // Start Y can be anywhere if line can go up/down

		var endX, endY int
		orientation := rand.Intn(4) // 0: '\', 1: '/', 2: '-', 3: '|' (more variety)

		switch orientation {
		case 0: // '\'
			endX = startX + lineLength
			endY = startY + lineLength
		case 1: // '/'
			endX = startX + lineLength
			endY = startY - lineLength
		case 2: // '-' horizontal
			endX = startX + lineLength
			endY = startY
		case 3: // '|' vertical
			endX = startX
			endY = startY + lineLength
		}

		// Clip line endpoints to be within the specific rectangle 'rect'
		// This is a simple clipping, more advanced cohen-sutherland could be used if lines frequently cross boundaries
		finalStartX, finalStartY, finalEndX, finalEndY := startX, startY, endX, endY

		// Clip X
		if finalStartX < rect.Min.X { finalStartX = rect.Min.X }
		if finalStartX >= rect.Max.X { finalStartX = rect.Max.X -1 }
		if finalEndX < rect.Min.X { finalEndX = rect.Min.X }
		if finalEndX >= rect.Max.X { finalEndX = rect.Max.X -1 }
		// Clip Y
		if finalStartY < rect.Min.Y { finalStartY = rect.Min.Y }
		if finalStartY >= rect.Max.Y { finalStartY = rect.Max.Y -1 }
		if finalEndY < rect.Min.Y { finalEndY = rect.Min.Y }
		if finalEndY >= rect.Max.Y { finalEndY = rect.Max.Y -1 }

		// Check if after clipping the line has any length left
		if finalStartX == finalEndX && finalStartY == finalEndY && lineLength > 0 {
			// If it's a point after clipping a line, draw it as a dot
			if rectWidth-dotSizeVal >=0 && rectHeight-dotSizeVal >=0 { // ensure dot fits
			   drawDot(img, finalStartX, finalStartY, patternColor, dotSizeVal)
			}
		} else if !(finalStartX == finalEndX && finalStartY == finalEndY) { // only draw if not collapsed to a single point by clipping unless it was a point
			drawLine(img, finalStartX, finalStartY, finalEndX, finalEndY, patternColor)
		}
	}
}


// createBlankImage creates a blank (black) PNG image.
func createBlankImage(filePath string, width int, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill image with black color (default is transparent black for NewRGBA)
	// To ensure it's opaque black:
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.Black)
		}
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("creating blank image file %s: %w", filePath, err)
	}
	defer file.Close()

	err = png.Encode(file, img)
	if err != nil {
		return fmt.Errorf("encoding blank image to PNG %s: %w", filePath, err)
	}
	return nil
}

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
	flag.IntVar(&crfValue, "crf", 18, "Constant Rate Factor (CRF) for libx264. 0-51. Lower is higher quality. 18 is often considered visually lossless. Default is 23.")

	flag.Parse()

	// Seed random number generator for pattern generation
	rand.Seed(time.Now().UnixNano())

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
	fmt.Printf("CRF Value: %d\n", crfValue)
	// fmt.Printf("Actual QR Recovery Level for library: %v\n", qrRecoveryLevel) // For debugging

	// Read input file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file %s: %v\n", inputFile, err)
		os.Exit(1)
	}

	// Compress the data using Zstandard
	fmt.Printf("Read %d bytes from %s. Compressing with zstd...\n", len(data), inputFile)
	zstdEncoder, errEnc := zstd.NewWriter(nil)
	if errEnc != nil {
		fmt.Fprintf(os.Stderr, "Error creating zstd writer: %v\n", errEnc)
		os.Exit(1)
	}
	compressedData := zstdEncoder.EncodeAll(data, make([]byte, 0, len(data)/2)) // Pre-allocate roughly half size
	zstdEncoder.Close() // Important to close to flush any buffered data, though EncodeAll usually does this.

	fmt.Printf("Compressed data size: %d bytes (Original: %d bytes).\n", len(compressedData), len(data))

	// Chunk data (now chunking the compressedData)
	var chunks [][]byte
	for i := 0; i < len(compressedData); i += chunkSize {
		end := i + chunkSize
		if end > len(compressedData) {
			end = len(compressedData)
		}
		chunks = append(chunks, compressedData[i:end])
	}

	fmt.Printf("Split compressed data into %d chunks.\n", len(chunks))

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

	// Parse video resolution
	videoWidth, videoHeight, err := parseResolution(resolution)
	if err != nil {
		// This error is already handled by ffmpeg arg validation later, but good to catch early if needed for other logic
		// For the purpose of the QR size check, we need valid videoWidth and videoHeight.
		// If parseResolution fails, ffmpeg will also fail. We can let that happen,
		// or exit here if we strictly need videoWidth/Height for the check.
		// Let's assume parseResolution is robust or ffmpeg's error is sufficient if resolution is malformed.
		// However, the plan is to check video resolution against QR image size.
		fmt.Fprintf(os.Stderr, "Error parsing video resolution '%s': %v\n", resolution, err)
		os.Exit(1)
	}

	// Check if video resolution is sufficient for the QR code size + border
	const borderSize = 20 // This is defined in the QR generation part, ensure it's consistent or make it a package/main const
	finalQrImageWidth := qrSize + (2 * borderSize)
	finalQrImageHeight := qrSize + (2 * borderSize) // Same as width

	if videoWidth < finalQrImageWidth || videoHeight < finalQrImageHeight {
		fmt.Fprintf(os.Stderr, "Error: Video resolution (%dx%d) is too small to display the QR code at its native size with border (%dx%d).\n", videoWidth, videoHeight, finalQrImageWidth, finalQrImageHeight)
		fmt.Fprintf(os.Stderr, "Please increase the video -resolution to at least %dx%d, or reduce -qrSize.\n", finalQrImageWidth, finalQrImageHeight)
		os.Exit(1)
	}


	paddingDurationSeconds := 2
	numPaddingFrames := paddingDurationSeconds * fps // Total frames for one side of padding

	allFrameFilePaths := []string{} // Will hold all frames: initial padding, QR, final padding

	// Generate initial padding frames
	fmt.Printf("Generating %d initial padding frames...\n", numPaddingFrames)
	for i := 0; i < numPaddingFrames; i++ {
		frameFileName := filepath.Join(tempDir, fmt.Sprintf("padding_frame_init_%04d.png", i))
		err := createBlankImage(frameFileName, videoWidth, videoHeight)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating initial padding frame %s: %v\n", frameFileName, err)
			os.Exit(1) // Critical error, exit
		}
		allFrameFilePaths = append(allFrameFilePaths, frameFileName)
	}

	// Generate and save QR code images
	// qrImageFilePaths will store the paths to each QR code image, duplicated framesPerQR times.
	actualQrImageFiles := []string{} // Temporary list for unique QR images

	if len(chunks) > math.MaxUint16 {
		fmt.Fprintf(os.Stderr, "Error: Number of chunks (%d) exceeds uint16 capacity (%d).\n", len(chunks), math.MaxUint16)
		os.Exit(1)
	}
	totalNumberOfChunks := uint16(len(chunks))

	for i, originalChunkData := range chunks {
		if i > math.MaxUint16 {
			// This should not happen if totalNumberOfChunks validation passed, but as a safeguard:
			fmt.Fprintf(os.Stderr, "Error: Chunk index (%d) exceeds uint16 capacity.\n", i)
			os.Exit(1)
		}
		sequenceNum := uint16(i)

		if len(originalChunkData) > math.MaxUint16 {
			fmt.Fprintf(os.Stderr, "Error: Length of original data chunk %d (%d bytes) exceeds uint16 capacity (%d).\n", i, len(originalChunkData), math.MaxUint16)
			// This could also mean -chunkSize is too large.
			os.Exit(1)
		}
		dataLen := uint16(len(originalChunkData))

		header := ChunkHeader{
			TotalChunks: totalNumberOfChunks,
			SequenceNum: sequenceNum,
			DataLength:  dataLen,
		}

		// Prepare data for checksum: TotalChunks (2B) + SequenceNum (2B) + DataLength (2B) + OriginalData
		// Size of this part of header for checksum is 2+2+2 = 6 bytes
		headerFieldsForChecksumBytes := make([]byte, 2+2+2)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[0:2], header.TotalChunks)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[2:4], header.SequenceNum)
		binary.BigEndian.PutUint16(headerFieldsForChecksumBytes[4:6], header.DataLength)

		dataToChecksum := append(headerFieldsForChecksumBytes, originalChunkData...)
		header.Checksum = xxhash.Sum64(dataToChecksum)

		// Prepare final QR payload: Full Header (14B) + OriginalData
		qrPayloadBuffer := new(bytes.Buffer)
		// Write the header (which now includes TotalChunks and the calculated Checksum)
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
		// ASCII85-encode the binary payload for the QR code.
		// Calculate the maximum possible length of the ASCII85 encoded data.
		maxEncodedLen := ascii85.MaxEncodedLen(len(finalPayloadBytes))
		dstEncoded := make([]byte, maxEncodedLen)
		// Perform the encoding.
		numEncodedBytes := ascii85.Encode(dstEncoded, finalPayloadBytes)
		// Slice dstEncoded to get the actual encoded data.
		ascii85Payload := string(dstEncoded[:numEncodedBytes])

		// Using qr.M for medium error correction.
		// qr.Auto should select an appropriate mode for ASCII85 characters.
		qrCode, err := qr.Encode(ascii85Payload, qr.M, qr.Auto)
		if err != nil {
			// 'i' is the current chunk index from the loop, header.SequenceNum is the value set in the header. They should match.
			fmt.Fprintf(os.Stderr, "Error generating QR code for chunk %d (header seq %d) with ASCII85 payload: %v\n", i, header.SequenceNum, err)
			continue
		}

		// Scale the barcode to the desired size (this is the QR symbol itself)
		originalScaledQrCode, err := barcode.Scale(qrCode, qrSize, qrSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scaling QR code for chunk %d (seq %d) to %dx%d: %v\n", i, header.SequenceNum, qrSize, qrSize, err)
			continue
		}

		// Add a 20px white border around the originalScaledQrCode
		const borderSize = 20
		finalCanvasWidth := qrSize + (2 * borderSize)
		finalCanvasHeight := qrSize + (2 * borderSize)

		// Create a new canvas image, filled with white. This will be the background for QR and pattern.
		borderedQrImageCanvas := image.NewRGBA(image.Rect(0, 0, finalCanvasWidth, finalCanvasHeight))
		draw.Draw(borderedQrImageCanvas, borderedQrImageCanvas.Bounds(), image.White, image.Point{}, draw.Src)

		// Draw the original scaled QR code (which is qrSize x qrSize) onto the canvas,
		// positioning its top-left corner at (borderSize, borderSize)
		qrRectInCanvas := image.Rect(borderSize, borderSize, borderSize+qrSize, borderSize+qrSize)
		draw.Draw(borderedQrImageCanvas,
			qrRectInCanvas,
			originalScaledQrCode,
			image.Point{}, // Start drawing from originalScaledQrCode's origin (0,0)
			draw.Over)

		// Define the four border areas and draw patterns in them
		// Top border strip
		topBorderRect := image.Rect(0, 0, finalCanvasWidth, borderSize)
		drawPatternInRect(borderedQrImageCanvas, topBorderRect)

		// Bottom border strip
		bottomBorderRect := image.Rect(0, borderSize+qrSize, finalCanvasWidth, finalCanvasHeight)
		drawPatternInRect(borderedQrImageCanvas, bottomBorderRect)

		// Left border strip (excluding corners covered by top/bottom strips)
		leftBorderRect := image.Rect(0, borderSize, borderSize, borderSize+qrSize)
		drawPatternInRect(borderedQrImageCanvas, leftBorderRect)

		// Right border strip (excluding corners covered by top/bottom strips)
		rightBorderRect := image.Rect(borderSize+qrSize, borderSize, finalCanvasWidth, borderSize+qrSize)
		drawPatternInRect(borderedQrImageCanvas, rightBorderRect)

		// Save the bordered QR code canvas with pattern as a PNG file
		// The final image will be (qrSize + 40) x (qrSize + 40)
		frameFileName := filepath.Join(tempDir, fmt.Sprintf("qr_frame_%04d.png", i))
		file, err := os.Create(frameFileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating PNG file for chunk %d (seq %d): %v\n", i, header.SequenceNum, err)
			continue
		}
		err = png.Encode(file, borderedQrImageCanvas) // Encode the canvas with the QR code and its new external border
		file.Close() // Close the file even if png.Encode fails, though it might be a bit late.
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing QR code PNG for chunk %d (seq %d) to %s: %v\n", i, header.SequenceNum, frameFileName, err)
			continue
		}

		actualQrImageFiles = append(actualQrImageFiles, frameFileName)
		// Log the size of the ascii85Payload string
		fmt.Printf("Generated QR code for chunk %d (seq %d), ASCII85 payload size %d chars: %s\n", i, header.SequenceNum, len(ascii85Payload), frameFileName)
	}

	if len(actualQrImageFiles) == 0 {
		fmt.Fprintf(os.Stderr, "No QR code images were successfully generated. Cannot create video without QR frames.\n")
		os.Exit(1)
	}

	// Now, populate qrImageFilePaths by duplicating according to framesPerQR
	qrImageFilePaths := []string{}
	for _, qrFile := range actualQrImageFiles {
		for j := 0; j < framesPerQR; j++ {
			// If framesPerQR > 1, we need to either duplicate the file or make ffmpeg hold the frame.
			// The renaming strategy (frame_00000X.png) requires distinct files for ffmpeg's image sequence input if we set input fps = output fps.
			// So, we must duplicate the actual files.
			if j == 0 {
				qrImageFilePaths = append(qrImageFilePaths, qrFile) // Use the original for the first instance
			} else {
				// Create a copy of the QR file for duplicates
				// This is inefficient but ensures each frame for ffmpeg is a unique file if input fps = output fps.
				// A better ffmpeg command might use concat for this, but file duplication is simpler for now.
				dupQrFileName := strings.TrimSuffix(qrFile, ".png") + fmt.Sprintf("_dup%d.png", j)

				sourceFile, err := os.Open(qrFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error opening QR source file for duplication %s: %v\n", qrFile, err)
					os.Exit(1)
				}
				defer sourceFile.Close()

				destFile, err := os.Create(dupQrFileName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating QR duplicate file %s: %v\n", dupQrFileName, err)
					os.Exit(1)
				}
				defer destFile.Close()

				_, err = io.Copy(destFile, sourceFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error copying QR file for duplication from %s to %s: %v\n", qrFile, dupQrFileName, err)
					os.Exit(1)
				}
				qrImageFilePaths = append(qrImageFilePaths, dupQrFileName)
			}
		}
	}
	fmt.Printf("Expanded %d unique QR images to %d frames (framesPerQR: %d)\n", len(actualQrImageFiles), len(qrImageFilePaths), framesPerQR)


	allFrameFilePaths = append(allFrameFilePaths, qrImageFilePaths...)

	// Generate final padding frames
	fmt.Printf("Generating %d final padding frames...\n", numPaddingFrames)
	for i := 0; i < numPaddingFrames; i++ {
		frameFileName := filepath.Join(tempDir, fmt.Sprintf("padding_frame_final_%04d.png", i))
		err := createBlankImage(frameFileName, videoWidth, videoHeight)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating final padding frame %s: %v\n", frameFileName, err)
			os.Exit(1) // Critical error, exit
		}
		allFrameFilePaths = append(allFrameFilePaths, frameFileName)
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
	// This calculation changes slightly. Each frame in allFrameFilePaths is unique and shown once.
	// If framesPerQR > 1, it means each QR code *image* was intended to last longer.
	// We now have distinct images for each "slot", including padding.
	// The concept of framesPerQR as a multiplier for QR images is less direct.
	// Instead, ffmpeg should process each image file as one frame.
	// If a QR code was meant to be displayed for `framesPerQR` video frames,
	// we should have duplicated that QR image `framesPerQR` times in `qrImageFilePaths` before padding.
	// For now, let's assume framesPerQR = 1 for simplicity with the new padding scheme,
	// meaning each QR image file corresponds to one video frame duration as set by `fps`.
	// If framesPerQR > 1 was used, the original code would make ffmpeg hold one QR image for multiple output frames.
	// With padding, we want each *image file* to be one input frame for ffmpeg.
	// The most straightforward way is to ensure each image in allFrameFilePaths is one frame.
	// If a QR code itself needs to be displayed longer (e.g. for `framesPerQR` video frames),
	// then the `qrImageFilePaths` list should have contained duplicates of that QR frame *before* being added to `allFrameFilePaths`.
	// The current code does not do this; it generates one PNG per chunk.
	//
	// Let's adjust how `ffmpegInputFPS` is determined or used.
	// The `-framerate` option for ffmpeg's image sequence input dictates how many input images make up one second of video.
	// If we want each image file (padding or QR) to have a duration of 1/fps seconds (where fps is the output video fps),
	// then the input framerate for the image sequence should be `fps`.
	// However, the existing `framesPerQR` logic means one QR image file is held for `framesPerQR` output frames.
	//
	// Let's simplify: each entry in `allFrameFilePaths` will become one frame in an intermediate sequence.
	// We will then tell ffmpeg to pick up these frames.
	// The duration of each of these frames in the final video is effectively (1/fps) if framesPerQR is 1.
	// If framesPerQR > 1, the original code made *ffmpeg* hold the frame.
	//
	// To implement padding correctly, we need a continuous sequence of images.
	// Step 1: Rename all files in allFrameFilePaths to a consistent pattern.
	finalSequenceTempDir, err := os.MkdirTemp(tempDir, "final_sequence_")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating final sequence temporary directory: %v\n", err)
		os.Exit(1)
	}
	// Note: finalSequenceTempDir is inside tempDir, so it will be cleaned up by the main defer.

	renamedFramePaths := []string{}
	for i, oldPath := range allFrameFilePaths {
		newFileName := fmt.Sprintf("frame_%06d.png", i) // Use 6 digits for safety
		newPath := filepath.Join(finalSequenceTempDir, newFileName)
		err := os.Rename(oldPath, newPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error renaming frame from %s to %s: %v\n", oldPath, newPath, err)
			os.Exit(1)
		}
		renamedFramePaths = append(renamedFramePaths, newPath)
	}
	fmt.Printf("Renamed %d frames into %s for ffmpeg input.\n", len(renamedFramePaths), finalSequenceTempDir)

	// ffmpegInputFPS should be `fps / framesPerQR` as per original logic.
	// This means if fps=10 and framesPerQR=2, ffmpeg consumes images at 5 images/sec,
	// and since output is 10fps, each input image is shown for 2 output frames.
	// This logic should still apply to the QR portion. Padding frames are 1:1.
	// This makes it complex.
	//
	// Simpler model: each file in `renamedFramePaths` is one input frame.
	// The `-framerate` for ffmpeg input should be the desired final video `fps`.
	// And each QR code image should be duplicated `framesPerQR` times in the `qrImageFilePaths` list
	// *before* it's merged into `allFrameFilePaths`.
	//
	// Let's modify the QR image generation loop to handle `framesPerQR`.

	// Revisit: The current plan step is to adjust ffmpeg input.
	// The simplest way to handle padding is to make all images (padding & QR) part of one sequence
	// and have each image be one input frame for ffmpeg.
	// If framesPerQR > 1, we must duplicate the QR image files.
	// This change should occur when qrImageFilePaths is populated.
	//
	// For now, assuming framesPerQR = 1 for the purpose of ffmpeg input.
	// If framesPerQR > 1, the current code would extend duration, which is fine for padding too.
	// The critical ffmpeg parameter is `-framerate` for the input sequence.
	// If we have N total images, and output is `fps` with each image held for `framesPerQR` output frames,
	// then total duration is N * framesPerQR / fps.
	// The input framerate to ffmpeg should be `fps / framesPerQR`.
	//
	// UPDATE: With file duplication for framesPerQR, each file in renamedFramePaths
	// corresponds to one output video frame. So, the input framerate to ffmpeg
	// should be the same as the output video fps.
	// ffmpegInputFPS := float64(fps) / float64(framesPerQR) // Old logic
	ffmpegInputFrameRate := strconv.Itoa(fps) // New logic: input fps = output fps


	// ffmpeg command arguments
	ffmpegArgs := []string{
		"-y", // Overwrite output file
		"-framerate", ffmpegInputFrameRate, // Input image sequence framerate
		"-i", filepath.Join(finalSequenceTempDir, "frame_%06d.png"), // Use the new pattern
		"-c:v", "libx264",
		"-crf", strconv.Itoa(crfValue), // Add CRF flag
		"-r", strconv.Itoa(fps), // Output video frame rate (should match input -framerate)
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
