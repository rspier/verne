// --- Type Declarations for Global Libraries (loaded via CDN) ---
declare var jsQR: any; // jsQR library
declare var xxhash: any; // xxhash-wasm library (assuming it exposes 'xxhash' globally or on window)

// --- DOM Element References ---
const videoElement = document.getElementById('video') as HTMLVideoElement | null;
const canvasElement = document.getElementById('canvas') as HTMLCanvasElement | null;
const startBtn = document.getElementById('startBtn') as HTMLButtonElement | null;
const stopBtn = document.getElementById('stopBtn') as HTMLButtonElement | null;
const statusOutput = document.getElementById('statusOutput') as HTMLPreElement | null;
const outputTextarea = document.getElementById('outputText') as HTMLTextAreaElement | null;
const downloadLink = document.getElementById('downloadLink') as HTMLAnchorElement | null;
const fileNameInput = document.getElementById('fileNameInput') as HTMLInputElement | null;
const progressOverview = document.getElementById('progressOverview') as HTMLDivElement | null;
const timingInfo = document.getElementById('timingInfo') as HTMLDivElement | null; // For timing info


// --- Core State Variables ---
let stream: MediaStream | null = null;
let frameProcessorIntervalId: number | null = null;
const FRAME_PROCESS_INTERVAL_MS = 500; // Process a frame every 500ms, adjust as needed
let frameCounter = 0; // Frame counter for status messages

// For storing and reassembling chunks
// Key: sequence number (number), Value: Uint8Array (original data chunk)
let collectedChunks: Map<number, Uint8Array> = new Map();
let highestSequenceNumberSeen = -1;
let totalChunksExpected = -1; // -1 means unknown, otherwise it's N if 0 to N-1 chunks are expected.
let presumedEncoderChunkSize = 1024; // Default from encode/main.go, can be refined if needed

// To store the initialized XXHash64 instance
let h64: any = null;


// --- Utility Functions (to be filled in later or moved to utils.ts) ---

/**
 * Converts a hex string to a Uint8Array.
 */
function hexToBytes(hex: string): Uint8Array {
    if (hex.length % 2 !== 0) {
        throw new Error("Hex string must have an even number of characters.");
    }
    const bytes = new Uint8Array(hex.length / 2);
    for (let i = 0; i < hex.length; i += 2) {
        bytes[i / 2] = parseInt(hex.substring(i, i + 2), 16);
    }
    return bytes;
}

/**
 * Reads a Big Endian Uint32 from a Uint8Array at a given offset.
 */
function bytesToUint32BE(bytes: Uint8Array, offset: number = 0): number {
    const view = new DataView(bytes.buffer, bytes.byteOffset + offset, 4);
    return view.getUint32(0, false); // false for Big Endian
}

/**
 * Reads a Big Endian Uint64 (as BigInt) from a Uint8Array at a given offset.
 */
function bytesToUint64BE(bytes: Uint8Array, offset: number = 0): BigInt {
    const view = new DataView(bytes.buffer, bytes.byteOffset + offset, 8);
    return view.getBigUint64(0, false); // false for Big Endian
}

function updateReceivedSequenceDisplay() {
    if (!progressOverview) return;

    const receivedCount = collectedChunks.size;
    let statusText = `Collected ${receivedCount} unique chunks. `;
    statusText += `Highest seq seen: ${highestSequenceNumberSeen}. `;

    if (totalChunksExpected !== -1) {
        statusText += `Expected total: ${totalChunksExpected}. `;
        const missing = totalChunksExpected - receivedCount;
        if (missing === 0 && receivedCount === totalChunksExpected) {
            statusText += `All ${totalChunksExpected} chunks received! Safe to stop capture.`;
            // Optionally, provide a more prominent visual cue here if desired
            if (statusOutput) { // Also update the main status line for high visibility
                // Append to existing status or set a new one
                statusOutput.textContent += "\nINFO: All expected chunks received. You can stop capturing.";
                statusOutput.style.color = 'green'; // Make it stand out
            }
        } else if (missing > 0) {
            statusText += `${missing} missing.`;
        } else { // receivedCount > totalChunksExpected -- should be rare due to checks in processFrame
            statusText += `Warning: Received ${receivedCount}, expected ${totalChunksExpected}.`;
        }
    } else {
        statusText += `Total expected: Unknown.`;
    }

    // Display ranges of received chunks
    if (receivedCount > 0) {
        const sortedKeys = Array.from(collectedChunks.keys()).sort((a, b) => a - b);
        let rangesString = " Received sequences: ";
        let rangeStart = -1;
        for (let i = 0; i < sortedKeys.length; i++) {
            const current = sortedKeys[i];
            if (rangeStart === -1) {
                rangeStart = current;
            }
            if (i + 1 === sortedKeys.length || sortedKeys[i+1] !== current + 1) {
                if (rangeStart === current) {
                    rangesString += `${current}, `;
                } else {
                    rangesString += `${rangeStart}-${current}, `;
                }
                rangeStart = -1;
            }
        }
        statusText += rangesString.slice(0, -2); // Remove trailing comma and space
    }
    progressOverview.textContent = statusText;
}


// --- Initialization and Event Listeners ---

/**
 * Updates the status message on the UI.
 */
function updateStatus(message: string, isError: boolean = false) {
    if (statusOutput) {
        statusOutput.textContent = message;
        statusOutput.style.color = isError ? 'red' : 'black'; // Default to black, red for errors
    }
    if (isError) {
        console.error(message);
    } else {
        console.log(message);
    }
}

/**
 * Initializes the XXHash module.
 */
async function initXxhash() {
    if (startBtn) startBtn.disabled = true; // Disable start button during init
    updateStatus('Initializing XXHash64 module...'); // This will set color to black
    try {
        // Check for the global xxhash function provided by the UMD script
        // Accessing through window for explicit global scope.
        if (typeof (window as any).xxhash !== 'function') {
            updateStatus('xxhash global function not found. Please ensure the library is loaded correctly from CDN.', true);
            throw new Error('xxhash global function not found.');
        }

        updateStatus('xxhash global function found, attempting to initialize WASM...');
        const xxhashModule = await (window as any).xxhash(); // Initialize and get the module object

        if (!xxhashModule || typeof xxhashModule.create64 !== 'function') {
            updateStatus('create64 (XXH64 streaming factory) not found in the resolved xxhash module.', true);
            console.log('Resolved xxhash module:', xxhashModule); // Log structure for inspection
            throw new Error('create64 (XXH64 streaming factory) not found in xxhash module.');
        }

        h64 = xxhashModule.create64; // Store the create64 factory (for streaming XXH64)
        updateStatus('XXH64 streaming module (create64) initialized successfully.');
        if (startBtn) startBtn.disabled = false; // Enable start button
    } catch (error: any) {
        updateStatus(`Error initializing XXHash64: ${error.message || error}`, true);
        if (startBtn) startBtn.disabled = true; // Keep it disabled if init fails
    }
}


// --- Main Application Logic (to be filled in: startCapture, stopCapture, processFrame, assembleData) ---

// Ensure all essential DOM elements are found
if (!videoElement || !canvasElement || !startBtn || !stopBtn || !statusOutput || !outputTextarea || !downloadLink || !fileNameInput || !progressOverview) {
    updateStatus('Critical error: One or more HTML elements were not found. Check element IDs.', true);
    // Depending on severity, might want to throw an error or disable functionality
} else {
    // Initial setup
    startBtn.disabled = true; // Start button is disabled until XXHash is ready
    stopBtn.disabled = true;
    downloadLink.style.display = 'none';

    // Initialize XXHash - this will attempt to enable startBtn upon success
    initXxhash();

    startBtn.addEventListener('click', startCapture);
    stopBtn.addEventListener('click', stopCapture);
}

async function startCapture() {
    if (!videoElement || !startBtn || !stopBtn || !outputTextarea || !downloadLink || !progressOverview) {
        updateStatus("Cannot start capture: critical HTML elements missing.", true);
        return;
    }
    if (!h64) {
        updateStatus("XXHash module not initialized. Cannot start capture.", true);
        await initXxhash(); // Try to re-initialize
        if (!h64) return; // Still not initialized
    }

    updateStatus("Requesting screen capture permission..."); // This will set color to black
    if (statusOutput) { // Explicitly reset color if it was green from a previous run
        statusOutput.style.color = 'black';
    }
    try {
        stream = await navigator.mediaDevices.getDisplayMedia({
            video: {
                // cursor: "never", // Optional: hide cursor from capture
                frameRate: 10 // Optional: suggest a frame rate
            },
            audio: false
        });

        videoElement.srcObject = stream;
        videoElement.play().catch(err => updateStatus(`Video play error: ${err}`, true)); // Play is often needed for some browsers

        startBtn.disabled = true;
        stopBtn.disabled = false;
        downloadLink.style.display = 'none';
        outputTextarea.value = '';
        progressOverview.textContent = '';

        // Reset state for new capture session
        collectedChunks.clear();
        highestSequenceNumberSeen = -1;
        totalChunksExpected = -1; // Reset, might be determined from first valid chunk
        frameCounter = 0; // Reset frame counter

        updateStatus("Screen capture started. Processing frames...");
        if (frameProcessorIntervalId !== null) {
            clearInterval(frameProcessorIntervalId);
        }
        frameProcessorIntervalId = window.setInterval(processFrame, FRAME_PROCESS_INTERVAL_MS);

    } catch (err: any) {
        updateStatus(`Error starting screen capture: ${err.message || err}`, true);
        if (startBtn) startBtn.disabled = false;
        if (stopBtn) stopBtn.disabled = true;
    }
}

function stopCapture() {
    if (!startBtn || !stopBtn) {
        updateStatus("Cannot stop capture: critical HTML elements missing.", true);
        return;
    }

    updateStatus("Stopping screen capture...");
    if (frameProcessorIntervalId !== null) {
        clearInterval(frameProcessorIntervalId);
        frameProcessorIntervalId = null;
    }

    if (stream) {
        stream.getTracks().forEach(track => track.stop());
        stream = null;
    }

    if (videoElement) {
        videoElement.srcObject = null;
    }

    startBtn.disabled = false;
    stopBtn.disabled = true;
    updateStatus("Capture stopped. Attempting to assemble data...");
    assembleData(); // Assemble and display/offer download
}


function processFrame() {
    if (!stream || !videoElement || !canvasElement || !statusOutput || !h64 || !timingInfo) {
        // updateStatus("processFrame: Prerequisites not met.", true);
        // Don't update status here as this might be called frequently before start
        return;
    }
    if (videoElement.readyState < videoElement.HAVE_METADATA || videoElement.videoWidth === 0) {
        return; // Video not ready yet
    }

    frameCounter++;
    let t0, t1, t2, t3, t4, t5; // Timestamps
    let captureDuration = 0, scanDuration = 0, dataProcessingDuration = 0;

    updateStatus(`Frame ${frameCounter}: Capturing...`);
    t0 = performance.now();

    const context = canvasElement.getContext('2d', { willReadFrequently: true });
    if (!context) {
        updateStatus(`Frame ${frameCounter}: Could not get 2D context from canvas.`, true);
        return;
    }

    canvasElement.width = videoElement.videoWidth;
    canvasElement.height = videoElement.videoHeight;

    context.drawImage(videoElement, 0, 0, canvasElement.width, canvasElement.height);
    t1 = performance.now();
    captureDuration = t1 - t0;

    updateStatus(`Frame ${frameCounter}: Decoding QR (Capture: ${captureDuration.toFixed(1)}ms)...`);
    const imageData = context.getImageData(0, 0, canvasElement.width, canvasElement.height);

    t2 = performance.now();
    const code = jsQR(imageData.data, imageData.width, imageData.height, {
        inversionAttempts: "dontInvert",
    });
    t3 = performance.now();
    scanDuration = t3 - t2;

    try {
        if (code && code.data) {
            if (typeof code.data !== 'string' || code.data.trim() === "") {
                updateStatus(`Frame ${frameCounter}: QR found, but data is empty/invalid (Scan: ${scanDuration.toFixed(1)}ms).`);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ---, Total: ${(captureDuration + scanDuration).toFixed(1)}ms`;
                return;
            }

            updateStatus(`Frame ${frameCounter}: QR found. Processing data (Scan: ${scanDuration.toFixed(1)}ms)...`);
            t4 = performance.now();

            const hexPayload = code.data;
            let finalPayloadBytes: Uint8Array;
            try {
                finalPayloadBytes = hexToBytes(hexPayload);
            } catch (e: any) {
                t5 = performance.now(); // Still record time up to the error
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Error hex-decoding payload: ${e.message} (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            if (finalPayloadBytes.length < 16) { // Minimum header size
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Payload too short (${finalPayloadBytes.length}B) (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            const headerChecksum = bytesToUint64BE(finalPayloadBytes, 0);
            const sequenceNum = bytesToUint32BE(finalPayloadBytes, 8);
            const dataLength = bytesToUint32BE(finalPayloadBytes, 12);

            if (16 + dataLength > finalPayloadBytes.length) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Header dataLength mismatch (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }
            const originalChunkData = finalPayloadBytes.slice(16, 16 + dataLength);

            const checksumDataBuffer = new Uint8Array(4 + 4 + originalChunkData.length);
            const view = new DataView(checksumDataBuffer.buffer);
            view.setUint32(0, sequenceNum, false);
            view.setUint32(4, dataLength, false);
            checksumDataBuffer.set(originalChunkData, 8);

            const calculatedChecksumBigInt = h64().update(checksumDataBuffer).digest('bigint');

            if (calculatedChecksumBigInt !== headerChecksum) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Checksum mismatch for Seq ${sequenceNum} (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            if (!collectedChunks.has(sequenceNum)) {
                collectedChunks.set(sequenceNum, originalChunkData);
                if (sequenceNum > highestSequenceNumberSeen) {
                    highestSequenceNumberSeen = sequenceNum;
                }
                // Final chunk detection logic (remains the same)
                if (totalChunksExpected !== -1 && sequenceNum >= totalChunksExpected) {
                    updateStatus(`Info: Chunk ${sequenceNum} received after a presumed final chunk. Resetting total expected chunks.`, false);
                    totalChunksExpected = -1;
                    if (statusOutput && statusOutput.style.color === 'green') {
                        statusOutput.style.color = 'black';
                    }
                }
                if (totalChunksExpected === -1 && dataLength < presumedEncoderChunkSize) {
                    totalChunksExpected = sequenceNum + 1;
                } else if (totalChunksExpected !== -1 && dataLength < presumedEncoderChunkSize && (sequenceNum + 1) < totalChunksExpected) {
                     updateStatus(`Info: A new, earlier short chunk ${sequenceNum} detected. Updating total expected from ${totalChunksExpected} to ${sequenceNum + 1}.`, false);
                    totalChunksExpected = sequenceNum + 1;
                }
                updateReceivedSequenceDisplay(); // This might update statusOutput too
            }
            t5 = performance.now();
            dataProcessingDuration = t5 - t4;
            updateStatus(`Frame ${frameCounter}: Chunk ${sequenceNum} (len ${dataLength}) OK. (DataProc: ${dataProcessingDuration.toFixed(1)}ms) Total unique: ${collectedChunks.size}.`);
            timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms, Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;

        } else { // No QR code found
            updateStatus(`Frame ${frameCounter}: No QR code found (Scan: ${scanDuration.toFixed(1)}ms).`);
            timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ---, Total: ${(captureDuration + scanDuration).toFixed(1)}ms`;
        }
    } catch (e: any) {
        // General error during processing after scan attempt
        const totalDurationSoFar = captureDuration + scanDuration + dataProcessingDuration;
        updateStatus(`Frame ${frameCounter}: Error processing frame: ${e.message || e} (Total time before error: ${totalDurationSoFar.toFixed(1)}ms)`, true);
        timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${totalDurationSoFar.toFixed(1)}ms (ERR)`;
    }
}


function assembleData() {
    if (!outputTextarea || !downloadLink || !fileNameInput || !progressOverview) {
        updateStatus("Cannot assemble data: critical HTML elements missing.", true);
        return;
    }

    if (collectedChunks.size === 0) {
        updateStatus("No data chunks collected to assemble.", false);
        outputTextarea.value = "<No data collected>";
        progressOverview.textContent = "No chunks collected.";
        downloadLink.style.display = 'none';
        return;
    }

    updateStatus(`Assembling ${collectedChunks.size} collected chunks... Highest sequence seen: ${highestSequenceNumberSeen}.`);

    const expectedCount = totalChunksExpected !== -1 ? totalChunksExpected : highestSequenceNumberSeen + 1;
    const finalHighestSeqToCheck = totalChunksExpected !== -1 ? totalChunksExpected - 1 : highestSequenceNumberSeen;

    if (totalChunksExpected !== -1 && collectedChunks.size < totalChunksExpected) {
        updateStatus(`Attempting to assemble, but not all expected chunks received. Expected ${totalChunksExpected}, Got ${collectedChunks.size}.`, true);
        // Proceeding anyway, but user should be aware.
    }


    // Check for missing chunks up to the finalHighestSeqToCheck
    let missingChunksExist = false;
    let firstMissing = -1;
    if (expectedCount > 0) { // Only check if we expect at least one chunk
        for (let i = 0; i <= finalHighestSeqToCheck; i++) {
            if (!collectedChunks.has(i)) {
                missingChunksExist = true;
                firstMissing = i;
                updateStatus(`Missing chunk with sequence number: ${i}`, true);
                break; // Stop at first missing for this message
            }
        }
    } else if (collectedChunks.size === 0 && expectedCount === 0) {
         // This case might happen if totalChunksExpected was set to 0 (e.g. an empty file was encoded)
         // For now, this results in "No data collected" earlier. If an empty file result is desired,
         // this logic might need adjustment.
    }


    if (missingChunksExist) {
        progressOverview.textContent = `Data assembly incomplete. Missing chunk(s) (e.g., seq ${firstMissing}). Collected ${collectedChunks.size} of ${expectedCount}.`;
        outputTextarea.value = `<Incomplete data: Missing chunk(s) up to sequence ${finalHighestSeqToCheck}. First missing: ${firstMissing}.>`;
        downloadLink.style.display = 'none';
        // Optionally, could still offer to assemble what we have. For now, require all up to expected.
        return;
    }

    progressOverview.textContent = `All ${expectedCount} chunks from 0 to ${finalHighestSeqToCheck} received! Assembling...`;

    // Concatenate all chunks in order
    let totalSize = 0;
    for (let i = 0; i <= finalHighestSeqToCheck; i++) {
        const chunkData = collectedChunks.get(i);
        if (chunkData) { // Should always be true if missingChunksExist is false
            totalSize += chunkData.length;
        } else {
            // This should not happen if the missing chunk check above is correct
            updateStatus(`Critical error during assembly: Chunk ${i} reported as present but not found.`, true);
            return;
        }
    }

    const reassembledData = new Uint8Array(totalSize);
    let currentOffset = 0;
    for (let i = 0; i <= finalHighestSeqToCheck; i++) {
        const chunk = collectedChunks.get(i)!; // Safe due to checks above
        reassembledData.set(chunk, currentOffset);
        currentOffset += chunk.length;
    }

    updateStatus(`Data reassembled. Total size: ${reassembledData.length} bytes.`);

    // Try to display as text (UTF-8)
    try {
        const textDecoder = new TextDecoder('utf-8', { fatal: true }); // fatal will throw on invalid UTF-8
        outputTextarea.value = textDecoder.decode(reassembledData);
        updateStatus("Displayed reassembled data as UTF-8 text.");
    } catch (e) {
        updateStatus("Reassembled data is not valid UTF-8 text, or contains null characters. Displaying as hex.", false);
        // Fallback to hex display if not valid UTF-8 or for binary data
        let hexString = '';
        for (let i = 0; i < Math.min(reassembledData.length, 1024); i++) { // Preview first 1KB as hex
            hexString += reassembledData[i].toString(16).padStart(2, '0');
        }
        outputTextarea.value = hexString + (reassembledData.length > 1024 ? "\n... (data truncated in hex preview)" : "");
    }

    // Offer download
    const blob = new Blob([reassembledData], { type: 'application/octet-stream' });
    const url = URL.createObjectURL(blob);
    downloadLink.href = url;
    downloadLink.download = fileNameInput.value || 'decoded_file';
    downloadLink.style.display = 'inline-block';
    updateStatus("Download link for reassembled file is ready.");

    // No automatic cleanup of URL.revokeObjectURL(url); as user might click multiple times.
    // It will be cleaned up when the page/document is unloaded.
}

// Initial message
if (statusOutput && !statusOutput.textContent?.includes("Critical error")) {
    updateStatus("Application loaded. Ready to start capture.");
}
console.log("webdecode/src/main.ts loaded");
