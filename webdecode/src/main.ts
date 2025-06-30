// Type declarations for elements, as getElementById can return null
const videoElement = document.getElementById('video') as HTMLVideoElement | null;
const canvasElement = document.getElementById('canvas') as HTMLCanvasElement | null;
const startBtn = document.getElementById('startBtn') as HTMLButtonElement | null;
const stopBtn = document.getElementById('stopBtn') as HTMLButtonElement | null;
const outputDiv = document.getElementById('output') as HTMLDivElement | null;

// Declare jsQR globally, as it's loaded via CDN
// A more robust solution would involve a .d.ts file for jsQR if one existed and could be used.
declare var jsQR: any;

let stream: MediaStream | null = null;
let intervalId: number | null = null;
let collectedChunks: string[] = []; // Array to store decoded QR data chunks

// Ensure elements exist before adding event listeners or using them
if (startBtn && stopBtn && videoElement && canvasElement && outputDiv) {
    startBtn.addEventListener('click', async () => {
        try {
            stream = await navigator.mediaDevices.getDisplayMedia({ video: true });
            videoElement.srcObject = stream;
            startBtn.disabled = true;
            stopBtn.disabled = false;
            outputDiv.innerHTML = "Starting capture... Scan QR codes."; // Clear previous error messages
            collectedChunks = []; // Reset collected chunks for a new session
            // Ensure processFrame is available and correctly typed if it's going to be used by setInterval
            intervalId = window.setInterval(processFrame, 1000); // Process a frame every second, use window.setInterval for clarity
        } catch (err: any) { // Explicitly type err
            console.error("Error starting screen capture:", err);
            outputDiv.textContent = "Error starting screen capture: " + err.message;
        }
    });

    stopBtn.addEventListener('click', () => {
        if (stream) {
            const tracks = stream.getTracks();
            tracks.forEach(track => track.stop());
            videoElement.srcObject = null;
            stream = null;
            startBtn.disabled = false;
            stopBtn.disabled = true;
            if (intervalId !== null) { // Check for null before clearing
                clearInterval(intervalId);
                intervalId = null;
            }
            // Attempt to assemble and display the full message when capture stops
            if (outputDiv) { // Ensure outputDiv is not null
                if (collectedChunks.length > 0) {
                    const fullMessage = collectedChunks.join('');
                    // For text data, this is fine. For binary, it would need different handling.
                    outputDiv.textContent = `Final Assembled Message: ${fullMessage}`;
                    console.log("Final assembled message:", fullMessage);

                    // Offer download for binary data (conceptual, needs actual file type and name)
                    // const blob = new Blob([fullMessage], { type: 'application/octet-stream' });
                    // const link = document.createElement('a');
                    // link.href = URL.createObjectURL(blob);
                    // link.download = 'decoded_file';
                    // link.click();
                    // URL.revokeObjectURL(link.href);

                } else {
                    outputDiv.textContent = "Capture stopped. No data collected.";
                }
            }
        }
    });
} else {
    console.error("One or more HTML elements were not found. Check element IDs.");
}

function processFrame() {
    if (!stream || !videoElement || !canvasElement || !outputDiv || videoElement.readyState < 2) { // Ensure video is ready and elements exist
        return;
    }

    const context = canvasElement.getContext('2d');
    if (!context) {
        console.error("Could not get 2D context from canvas");
        return;
    }

    canvasElement.width = videoElement.videoWidth;
    canvasElement.height = videoElement.videoHeight;
    context.drawImage(videoElement, 0, 0, canvasElement.width, canvasElement.height);

    const imageData = context.getImageData(0, 0, canvasElement.width, canvasElement.height);

    // Attempt to decode QR code
    try {
        const code = jsQR(imageData.data, imageData.width, imageData.height, {
            inversionAttempts: "dontInvert", // Standard QR codes are dark on light. Can also try "attemptBoth" or "invertFirst"
        });

        if (code && code.data && code.data.trim() !== "") {
            // Basic de-duplication: only add if it's different from the last chunk
            // A more robust system would use sequence numbers if they were available
            if (collectedChunks.length === 0 || collectedChunks[collectedChunks.length - 1] !== code.data) {
                collectedChunks.push(code.data);
                console.log(`Collected chunk ${collectedChunks.length}: ${code.data.substring(0,30)}...`);
                // Display current progress or number of chunks
                outputDiv.textContent = `Collected ${collectedChunks.length} unique chunks. Last chunk: ${code.data.substring(0, 50)}...`;
            }
        } else {
            // No QR code found or decoded in this frame, or empty data
            // outputDiv.textContent = "Scanning for QR code..."; // Optionally provide feedback
        }
    } catch (e: any) {
        console.error("Error during QR decoding:", e);
        outputDiv.textContent = "Error during QR decoding: " + e.message;
    }
}
