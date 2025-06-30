package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	// No longer need "flag" or "fmt" here if not used by other tests
)

// Helper function to reset package-level flag variables if used by any test.
// runEncoderApp uses its own FlagSet, so this is mainly for consistency if other tests were to modify them.
func resetFlags() {
	outputFile = "output.mp4"
	chunkSize = 1024
	qrSize = 256
	fps = 1
	framesPerQR = 1
	resolution = "256x256"
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		name           string
		resStr         string
		wantWidth      int
		wantHeight     int
		wantErr        bool
		expectedErrMsg string
	}{
		{"valid", "1920x1080", 1920, 1080, false, ""},
		{"valid lowercase", "800x600", 800, 600, false, ""},
		{"valid small", "64x64", 64, 64, false, ""},
		{"invalid format no x", "19201080", 0, 0, true, "resolution must be in format WIDTHxHEIGHT"},
		{"invalid format too many x", "1920x1080x720", 0, 0, true, "resolution must be in format WIDTHxHEIGHT"},
		{"invalid width not a number", "ax1080", 0, 0, true, "invalid width"},
		{"invalid height not a number", "1920xb", 0, 0, true, "invalid height"},
		{"zero width", "0x1080", 0, 0, true, "width and height must be positive"},
		{"zero height", "1920x0", 0, 0, true, "width and height must be positive"},
		{"negative width", "-1920x1080", 0, 0, true, "width and height must be positive"},
		{"empty string", "", 0, 0, true, "resolution must be in format WIDTHxHEIGHT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height, err := parseResolution(tt.resStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseResolution(%q) error = %v, wantErr %v", tt.resStr, err, tt.wantErr)
				return
			}
			if err != nil && tt.wantErr && !strings.Contains(err.Error(), tt.expectedErrMsg) {
				t.Errorf("parseResolution(%q) error message = %q, expected to contain %q", tt.resStr, err.Error(), tt.expectedErrMsg)
			}
			if width != tt.wantWidth {
				t.Errorf("parseResolution(%q) width = %v, want %v", tt.resStr, width, tt.wantWidth)
			}
			if height != tt.wantHeight {
				t.Errorf("parseResolution(%q) height = %v, want %v", tt.resStr, height, tt.wantHeight)
			}
		})
	}
}

func TestDataChunking(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		chunkSize   int
		wantChunks  [][]byte
		// expectError bool // Not used here as this test focuses on chunking logic with valid sizes
	}{
		{name: "empty data", data: []byte{}, chunkSize: 10, wantChunks: nil},
		{name: "data smaller than chunk size", data: []byte("hello"), chunkSize: 10, wantChunks: [][]byte{[]byte("hello")}},
		{name: "data equals chunk size", data: []byte("0123456789"), chunkSize: 10, wantChunks: [][]byte{[]byte("0123456789")}},
		{name: "data larger than chunk size, exact multiple", data: []byte("0123456789abcdefghij"), chunkSize: 10, wantChunks: [][]byte{[]byte("0123456789"), []byte("abcdefghij")}},
		{name: "data larger than chunk size, not exact multiple", data: []byte("0123456789abc"), chunkSize: 10, wantChunks: [][]byte{[]byte("0123456789"), []byte("abc")}},
		{name: "chunk size 1", data: []byte("abc"), chunkSize: 1, wantChunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var chunks [][]byte
			if len(tt.data) > 0 { // Ensure chunking only happens if data exists
				if tt.chunkSize <= 0 { // Guard against invalid chunk size for this test logic
					t.Fatalf("TestDataChunking: chunkSize must be positive, got %d", tt.chunkSize)
				}
				for i := 0; i < len(tt.data); i += tt.chunkSize {
					end := i + tt.chunkSize
					if end > len(tt.data) {
						end = len(tt.data)
					}
					chunks = append(chunks, tt.data[i:end])
				}
			}

			if !reflect.DeepEqual(chunks, tt.wantChunks) {
				gotStr := make([]string, len(chunks))
				for i, c := range chunks { gotStr[i] = string(c) }
				wantStr := make([]string, len(tt.wantChunks))
				for i, c := range tt.wantChunks { wantStr[i] = string(c) }
				t.Errorf("Chunking data %q with size %d: got %v, want %v", string(tt.data), tt.chunkSize, gotStr, wantStr)
			}
		})
	}
}

func TestRunEncoderApp_ArgumentHandling(t *testing.T) {
	t.Run("input file not found returns error", func(t *testing.T) {
		resetFlags()
		args := []string{"qrvidencode", "nonexistentfile.txt"}
		err := runEncoderApp(args)
		if err == nil {
			t.Fatal("runEncoderApp did not return an error for a non-existent input file")
		}
		if !strings.Contains(err.Error(), "no such file or directory") && !strings.Contains(err.Error(), "nonexistentfile.txt") {
			t.Errorf("Expected error to contain 'no such file or directory' or filename, got: %v", err)
		}
		t.Logf("runEncoderApp correctly returned error for non-existent file: %v", err)
	})

	t.Run("no input file provided returns error", func(t *testing.T) {
		resetFlags()
		args := []string{"qrvidencode"} // No positional argument
		err := runEncoderApp(args)
		if err == nil {
			t.Fatal("runEncoderApp did not return an error when no input file was provided")
		}
		if !strings.Contains(err.Error(), "exactly one positional argument") {
			t.Errorf("Expected error about missing positional argument, got: %v", err)
		}
		t.Logf("runEncoderApp correctly returned error for missing input file: %v", err)
	})

	t.Run("too many input files provided returns error", func(t *testing.T) {
		resetFlags()
		args := []string{"qrvidencode", "file1.txt", "file2.txt"} // Too many positional
		err := runEncoderApp(args)
		if err == nil {
			t.Fatal("runEncoderApp did not return an error when too many input files were provided")
		}
		if !strings.Contains(err.Error(), "exactly one positional argument") {
			t.Errorf("Expected error about too many positional arguments, got: %v", err)
		}
		t.Logf("runEncoderApp correctly returned error for too many input files: %v", err)
	})
}

func TestRunEncoderApp_FFmpegHandling(t *testing.T) {
	t.Log("TestRunEncoderApp_FFmpegHandling: Starting test")
	resetFlags()

	tmpInputFile, err := os.CreateTemp("", "test_ffmpeg_input_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp input file: %v", err)
	}
	defer os.Remove(tmpInputFile.Name())
	if _, err := tmpInputFile.WriteString("testdata"); err != nil {
		t.Fatalf("Failed to write to temp input file: %v", err)
	}
	tmpInputFile.Close()

	tmpOutputDir, err := os.MkdirTemp("", "test_ffmpeg_output_dir_")
	if err != nil {
		t.Fatalf("Failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(tmpOutputDir)
	testOutputFile := filepath.Join(tmpOutputDir, "test_out.mp4")

	args := []string{
		"qrvidencode",
		"--out", testOutputFile,
		"--chunkSize", "5",
		"--qrSize", "128",
		tmpInputFile.Name(),
	}

	var ffmpegPathForTest string
	var appShouldFindFFmpeg bool

	pathFromLookPath, errLookPath := exec.LookPath("ffmpeg")
	if errLookPath == nil {
		ffmpegPathForTest = pathFromLookPath
		appShouldFindFFmpeg = true
	} else {
		hardcodedPath := "/usr/bin/ffmpeg"
		info, errStat := os.Stat(hardcodedPath)
		if errStat == nil && !info.IsDir() && (info.Mode()&0111 != 0) {
			ffmpegPathForTest = hardcodedPath
			appShouldFindFFmpeg = true
		} else {
			appShouldFindFFmpeg = false
		}
	}

	t.Logf("Test environment check: appShouldFindFFmpeg: %v, path: %q", appShouldFindFFmpeg, ffmpegPathForTest)

	err = runEncoderApp(args)

	if appShouldFindFFmpeg {
		if err != nil {
			t.Errorf("runEncoderApp failed unexpectedly (ffmpeg was expected to be found and run by app): %v", err)
		} else {
			if _, statErr := os.Stat(testOutputFile); os.IsNotExist(statErr) {
				t.Errorf("Output video file %s was not created, even though runEncoderApp reported success.", testOutputFile)
			} else {
				t.Logf("runEncoderApp completed successfully, output video created at %s", testOutputFile)
			}
		}
	} else {
		if err == nil {
			t.Error("runEncoderApp succeeded but an error was expected because ffmpeg should not be found by its logic.")
		} else if !strings.Contains(err.Error(), "ffmpeg not found") && !strings.Contains(err.Error(), "executable file not found") {
			t.Errorf("runEncoderApp returned an unexpected error when ffmpeg was not expected to be found: %v", err)
		} else {
			t.Logf("runEncoderApp correctly returned an error related to ffmpeg not being found: %v", err)
		}
	}
}
