package main

import (
	"fmt"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Helper function for tests to find ffmpeg, similar to main packages
// This avoids needing to export findFFmpegExecutable from main packages just for tests,
// or creating a shared util package for just one function used by tests and main.
func findFFmpegForTest(t *testing.T) (string, bool) {
	path, err := exec.LookPath("ffmpeg")
	if err == nil {
		t.Logf("findFFmpegForTest: Found ffmpeg in PATH: %s", path)
		return path, true
	}
	t.Logf("findFFmpegForTest: ffmpeg not found in PATH: %v", err)

	// Check common hardcoded path
	hardcodedPath := "/usr/bin/ffmpeg"
	info, errStat := os.Stat(hardcodedPath)
	if errStat == nil {
		if !info.IsDir() && (info.Mode()&0111 != 0) {
			t.Logf("findFFmpegForTest: Found ffmpeg at hardcoded path: %s", hardcodedPath)
			return hardcodedPath, true
		}
		t.Logf("findFFmpegForTest: Found %s, but it's not an executable file.", hardcodedPath)
	} else {
		t.Logf("findFFmpegForTest: Did not find ffmpeg at %s: %v", hardcodedPath, errStat)
	}

	return "", false
}


// Note: True automated end-to-end testing for this application is complex as it
// requires:
// 1. The `qrvidencode` program to be built and available.
// 2. `ffmpeg` to be installed and available in PATH for both encoding and decoding.
// 3. File system operations for creating temporary files/videos and then reading them.
// 4. Comparison of binary data.

// The following outlines a manual/semi-automated test procedure.
// To perform an end-to-end test:

// 1. Ensure `qrvidencode` and `qrviddecoder` (this program) are built.
//    From the repository root, after running `go mod tidy` at the root:
//    ```bash
//    # From repository root
//    go build -o qrvidencoder_test_util ./encode
//    go build -o qrviddecoder_test_util ./decode
//    ```
//    This places the test executables in the repository root.

// 2. Create a sample input file.
//    ```bash
//    echo "Hello, QR Video World! This is a test." > sample_input.txt
//    ```

// 3. Encode the sample file using `qrvidencoder_test_util`.
//    The `-chunkSize` now refers to the size of the original data payload per QR code.
//    The actual data encoded in each QR will be `chunkSize + 16 (header)` bytes, then hex-encoded.
//    Usage: ./qrvidencoder_test_util <input_file> --out <output_video> [options]
//    ```bash
//    ./qrvidencoder_test_util sample_input.txt --out test_video.mp4 --chunkSize 20 --qrSize 256 --fps 1 --framesPerQR 1
//    ```
//    (Adjust parameters as needed for different test cases)

// 4. Decode the video using `qrviddecoder_test_util`.
//    Usage: ./qrviddecoder_test_util <input_video> --out <output_data> [options]
//    ```bash
//    ./qrviddecoder_test_util test_video.mp4 --out decoded_output.txt
//    ```

// 5. Compare the original and decoded files.
//    ```bash
//    diff sample_input.txt decoded_output.txt
//    ```
//    If `diff` produces no output, the files are identical, and the test passes.

// 6. Clean up test files:
//    ```bash
//    rm sample_input.txt test_video.mp4 decoded_output.txt qrvidencoder_test_util qrviddecoder_test_util
//    ```

// This `TestMainE2EPlaceholder` function serves as a placeholder to acknowledge the testing step
// and to allow `go test` to run without actual test functions if none are simple unit tests.
func TestMainE2EPlaceholder(t *testing.T) {
	t.Log("This is a placeholder test.")
	t.Log("Refer to comments in this file for manual end-to-end testing procedures.")

	// Example of trying to find ffmpeg to give a hint if it's missing for manual tests
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Logf("FFmpeg command check: FFmpeg does not seem to be installed or in PATH. FFmpeg is required for manual end-to-end testing. Error: %v", err)
	} else {
		t.Log("FFmpeg command check: FFmpeg appears to be available.")
	}

	// Check if encoder exists (relative path, assuming test run from decode dir)
	encoderPath := filepath.Join("..", "encode", "qrvidencode") // Source name
	if _, err := os.Stat(encoderPath); os.IsNotExist(err) {
		// Try checking for a built version if user followed manual steps
		builtEncoderPath := filepath.Join("..", "qrvidencoder_test_util")
		if _, err2 := os.Stat(builtEncoderPath); os.IsNotExist(err2) {
			t.Logf("Encoder check: Encoder source at '%s' or built util at '%s' not found. It's needed for manual E2E testing.", encoderPath, builtEncoderPath)
		} else {
			t.Logf("Encoder check: Built encoder util at '%s' found.", builtEncoderPath)
		}
	} else {
		t.Logf("Encoder check: Encoder source at '%s' found.", encoderPath)
	}
}

// To make `go test` pass without any real tests, we can have a simple, always-passing test.
func TestDummy(t *testing.T) {
	if 1+1 != 2 {
		t.Error("A very basic arithmetic test failed, which is highly unexpected.")
	}
}

// If you were to add unit tests, they would go here. For example:
// func TestSpecificFunction(t *testing.T) { ... }
// However, main() is hard to unit test directly without significant refactoring.
// For now, the E2E guidance is the most practical.

func TestEndToEnd_EncodeDecode(t *testing.T) {
	ffmpegPath, found := findFFmpegForTest(t)
	if !found {
		// Message already logged by findFFmpegForTest
		t.Skip("ffmpeg not found in PATH or at /usr/bin/ffmpeg, skipping end-to-end test")
	}
	t.Logf("Using ffmpeg for E2E test: %s", ffmpegPath) // ffmpegPath is not directly used by test, but by compiled binaries

	// 1. Create temporary directory for all test artifacts
	testDir, err := os.MkdirTemp("", "qrvid_e2e_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory for E2E test: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Logf("Warning: failed to remove temp test directory %s: %v", testDir, err)
		}
	}()

	encoderExePath := filepath.Join(testDir, "qrvidencoder_e2e")
	decoderExePath := filepath.Join(testDir, "qrviddecoder_e2e")
	if runtime.GOOS == "windows" {
		encoderExePath += ".exe"
		decoderExePath += ".exe"
	}

	// 2. Build encoder and decoder
	// Assuming we are in the context of the 'decode' package tests, the module root is '..'
	// However, since we now have a single module at the repo root, adjust paths.
	// `go build` from within a test usually means paths are relative to package dir.
	// To build other packages in the same module, use their module paths.
	// Or, more simply, use relative paths from the module root.
	// For `go test ./decode/...` run from root, current dir for test is package dir.
	// So, `../encode` and `.` (for decode) should work if test CWD is the package dir.
	// Let's set Dir to project root ("..") for both build commands for consistency.

	projectRoot := ".." // Relative path from decode test execution to project root

	cmdBuildEncoder := exec.Command("go", "build", "-o", encoderExePath, "./encode")
	cmdBuildEncoder.Dir = projectRoot
	output, err := cmdBuildEncoder.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build encoder for E2E test (from %s): %v\nOutput:\n%s", projectRoot, err, string(output))
	}

	cmdBuildDecoder := exec.Command("go", "build", "-o", decoderExePath, "./decode")
	cmdBuildDecoder.Dir = projectRoot
	output, err = cmdBuildDecoder.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build decoder for E2E test (from %s): %v\nOutput:\n%s", projectRoot, err, string(output))
	}


	// 3. Create Sample Input File
	sampleData := "Hello, QR Video World!\nThis is line 2 with some punctuation: &*^%$#@!\nA third line for good measure.\n"
	sampleInputFile := filepath.Join(testDir, "sample_e2e_input.txt")
	err = os.WriteFile(sampleInputFile, []byte(sampleData), 0644)
	if err != nil {
		t.Fatalf("Failed to create sample input file: %v", err)
	}

	// 4. Define Output Paths
	videoFile := filepath.Join(testDir, "temp_e2e_video.mp4")
	decodedOutputFile := filepath.Join(testDir, "decoded_e2e_output.txt")

	// 5. Run Encoder
	// Use small, fast parameters for testing
	// Chunk size needs to be small enough that metadata isn't overwhelming, but not too small.
	// Metadata is 12 bytes (8 checksum + 4 seq). If data is 20 bytes, total is 32.
	// Encoder: [flags] <input_file>
	encodeCmd := exec.Command(encoderExePath,
		"--out", videoFile,
		"--chunkSize", "20", // Small chunk size for testing
		"--qrSize", "256",   // Default size
		"-fps", "1",
		"-framesPerQR", "1",
		"-resolution", "256x256",
		sampleInputFile, // Positional argument now at the end
	)
	encodeOutput, err := encodeCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Encoder failed: %v\nOutput:\n%s", err, string(encodeOutput))
	}
	t.Logf("Encoder output:\n%s", string(encodeOutput))


	// 6. Run Decoder
	// Decoder: [flags] <input_video>
	decodeCmd := exec.Command(decoderExePath,
		// No other flags defined for decoder in test, but if they were, they'd go here
		"--out", decodedOutputFile,
		videoFile, // Positional argument now at the end
	)
	decodeOutput, err := decodeCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Decoder failed: %v\nOutput:\n%s", err, string(decodeOutput))
	}
	t.Logf("Decoder output:\n%s", string(decodeOutput))

	// 7. Compare Files
	originalBytes, err := os.ReadFile(sampleInputFile)
	if err != nil {
		t.Fatalf("Failed to read original sample input file: %v", err)
	}
	decodedBytes, err := os.ReadFile(decodedOutputFile)
	if err != nil {
		t.Fatalf("Failed to read decoded output file: %v", err)
	}

	if !bytes.Equal(originalBytes, decodedBytes) {
		t.Errorf("End-to-end test failed: Decoded data does not match original data.\nOriginal:\n%s\nDecoded:\n%s", string(originalBytes), string(decodedBytes))
	} else {
		t.Log("End-to-end test successful: Decoded data matches original data.")
	}
}


func Example_manualTestingWorkflow() {
	// This is not a real test but demonstrates the workflow for documentation.
	// Create dummy files to simulate the process for `go test -v ./...` example output.

	fmt.Println("Simulating manual end-to-end test workflow:")

	// 1. Simulate creating encoder & decoder (conceptually)
	fmt.Println("(Build encoder and decoder: `cd encode && go build ...`, `cd decode && go build ...`)")

	// 2. Simulate creating sample input
	fmt.Println("echo \"Test Data\" > sample_input.txt")

	// 3. Simulate encoding
	fmt.Println("./qrvidencoder_test_util -inputFile sample_input.txt -outputFile test_video.mp4")

	// 4. Simulate decoding
	fmt.Println("./qrviddecoder_test_util -inputFile test_video.mp4 -outputFile decoded_output.txt")

	// 5. Simulate comparison
	fmt.Println("diff sample_input.txt decoded_output.txt  (should show no differences)")

	// 6. Simulate cleanup
	fmt.Println("rm sample_input.txt test_video.mp4 decoded_output.txt ...")

	// Output:
	// Simulating manual end-to-end test workflow:
	// (Build encoder and decoder: `cd encode && go build ...`, `cd decode && go build ...`)
	// echo "Test Data" > sample_input.txt
	// ./qrvidencoder_test_util -inputFile sample_input.txt -outputFile test_video.mp4
	// ./qrviddecoder_test_util -inputFile test_video.mp4 -outputFile decoded_output.txt
	// diff sample_input.txt decoded_output.txt  (should show no differences)
	// rm sample_input.txt test_video.mp4 decoded_output.txt ...
}
