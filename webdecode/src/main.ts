// --- Type Declarations for Global Libraries (loaded via CDN) ---
declare var jsQR: any; // jsQR library
declare var xxhash: any; // xxhash-wasm library
// No longer declaring external ASCII85 library, will use native implementation.
// No specific ZSTD library declaration needed for native DecompressionStream.
// However, DecompressionStream itself needs to be declared if target < ES2022 or not in default lib.
// For now, assume it's available in modern browser contexts targeted by tsconfig.json.

// --- Global instances for initialized libraries ---
let h64: any = null; // For xxhash
// let zstdSimple: ZstdSimpleApi | null = null; // No longer needed

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

// --- Utility Functions (to be filled in later or moved to utils.ts) ---

function decodeAdobeAscii85(a85: string): Uint8Array {
    // Remove whitespace. Go's encoder doesn't add it, but robust decoders often handle it.
    const cleanA85 = a85.replace(/\s/g, '');
    const n = cleanA85.length;

    // According to Go's encoding/ascii85, "z" and "y" are not used by the Encoder if the result would need padding.
    // This simplifies decoding "z" and "y" as they always represent 4 bytes.

    let decodedBytes: number[] = [];

    for (let i = 0; i < n; ) {
        const char = cleanA85[i];

        if (char === 'z') {
            decodedBytes.push(0, 0, 0, 0);
            i++;
            continue;
        }
        if (char === 'y') {
            decodedBytes.push(32, 32, 32, 32);
            i++;
            continue;
        }

        // Process a 5-char block
        let val = 0;
        const charsInBlock = Math.min(5, n - i);

        if (charsInBlock === 1) { // Should not happen with valid Adobe ASCII85 if not delimited
            throw new Error("Invalid ASCII85: block of 1 char found mid-stream.");
        }

        for (let j = 0; j < 5; j++) {
            let c: number;
            if (j < charsInBlock) {
                c = cleanA85.charCodeAt(i + j);
                if (c < 33 || c > 117) { // '!' to 'u'
                    throw new Error(`Invalid ASCII85 character '${cleanA85[i+j]}' at index ${i+j}`);
                }
                val = val * 85 + (c - 33);
            } else { // Padding for the last block
                val = val * 85 + 84; // Pad with 'u'
            }
        }
        i += charsInBlock;

        // Extract bytes
        const numBytesToOutput = charsInBlock - 1;
        decodedBytes.push((val >> 24) & 0xFF);
        if (numBytesToOutput > 1) decodedBytes.push((val >> 16) & 0xFF);
        if (numBytesToOutput > 2) decodedBytes.push((val >> 8) & 0xFF);
        if (numBytesToOutput > 3) decodedBytes.push(val & 0xFF);

        // The conditional push logic above already ensures the correct number of bytes (numBytesToOutput)
        // are added for the current block, including handling for the last padded block.
        // No further slicing per block is needed here.
    }
    return new Uint8Array(decodedBytes);
}


/**
 * Reads a Big Endian Uint32 from a Uint8Array at a given offset.
 */
function bytesToUint32BE(bytes: Uint8Array, offset: number = 0): number {
    const view = new DataView(bytes.buffer, bytes.byteOffset + offset, 4);
    return view.getUint32(0, false); // false for Big Endian
}

/**
 * Reads a Big Endian Uint16 from a Uint8Array at a given offset.
 */
function bytesToUint16BE(bytes: Uint8Array, offset: number = 0): number {
    const view = new DataView(bytes.buffer, bytes.byteOffset + offset, 2);
    return view.getUint16(0, false); // false for Big Endian
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

    // Initialize necessary libraries (xxhash only for now, Gzip is native)
    initXxhash()
        .then(() => {
            updateStatus("Application ready. You can start screen capture.");
            if (startBtn) startBtn.disabled = false;
        })
        .catch(error => {
            updateStatus(`XXHash Initialization failed: ${error.message || error}`, true);
            if (startBtn) startBtn.disabled = true;
        });

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

            const ascii85Payload = code.data;
            let finalPayloadBytes: Uint8Array;
            try {
                finalPayloadBytes = decodeAdobeAscii85(ascii85Payload);
            } catch (e: any) {
                t5 = performance.now(); // Still record time up to the error
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Error ASCII85-decoding payload: ${e.message} (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            const HEADER_SIZE = 14; // New header size (8 + 2 + 2 + 2)

            if (finalPayloadBytes.length < HEADER_SIZE) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Payload too short (${finalPayloadBytes.length}B, need ${HEADER_SIZE}) (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR_SHORT_PAYLOAD), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            // Parse new header: Checksum (8B), TotalChunks (2B), SequenceNum (2B), DataLength (2B)
            const headerChecksum = bytesToUint64BE(finalPayloadBytes, 0);     // Offset 0
            const totalChunks = bytesToUint16BE(finalPayloadBytes, 8);        // Offset 8
            const sequenceNum = bytesToUint16BE(finalPayloadBytes, 10);       // Offset 10
            const dataLength = bytesToUint16BE(finalPayloadBytes, 12);        // Offset 12

            if (HEADER_SIZE + dataLength > finalPayloadBytes.length) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Header dataLength mismatch (payload ${finalPayloadBytes.length}B, header ${HEADER_SIZE}B, data ${dataLength}B) (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR_LEN_MISMATCH), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }
            const originalChunkData = finalPayloadBytes.slice(HEADER_SIZE, HEADER_SIZE + dataLength);

            // Checksum data: TotalChunks (2B) + SequenceNum (2B) + DataLength (2B) + OriginalData
            const checksumDataBuffer = new Uint8Array(2 + 2 + 2 + originalChunkData.length);
            const view = new DataView(checksumDataBuffer.buffer);
            view.setUint16(0, totalChunks, false);      // TotalChunks (uint16)
            view.setUint16(2, sequenceNum, false);      // SequenceNum (uint16)
            view.setUint16(4, dataLength, false);     // DataLength (uint16)
            checksumDataBuffer.set(originalChunkData, 6); // OriginalData starts after 6 bytes of these fields

            const calculatedChecksumBigInt = h64().update(checksumDataBuffer).digest('bigint');

            if (calculatedChecksumBigInt !== headerChecksum) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Frame ${frameCounter}: Checksum mismatch for Seq ${sequenceNum} (Total ${totalChunks}) (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return;
            }

            // --- Utilize totalChunks ---
            if (totalChunksExpected === -1) { // First valid chunk sets the expectation
                totalChunksExpected = totalChunks;
                if (totalChunksExpected === 0) { // Should not happen from new encoder for non-empty files
                     updateStatus(`Warning: Frame ${frameCounter}, Seq ${sequenceNum}: Header reports TotalChunks as 0. This is unexpected.`, false);
                }
                console.log(`Total chunks expected set to: ${totalChunksExpected} from chunk ${sequenceNum}`);
            } else if (totalChunks !== totalChunksExpected) {
                // This is a critical inconsistency
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`CRITICAL ERROR: Frame ${frameCounter}, Seq ${sequenceNum}: Inconsistent TotalChunks! Expected ${totalChunksExpected}, got ${totalChunks}. Halting. (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (CRIT_ERR), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                stopCapture(); // Stop capture due to inconsistent stream
                return;
            }

            // Validate sequenceNum against totalChunksExpected (if known and > 0)
            if (totalChunksExpected > 0 && sequenceNum >= totalChunksExpected) {
                t5 = performance.now();
                dataProcessingDuration = t5 - t4;
                updateStatus(`Error: Frame ${frameCounter}, Seq ${sequenceNum}: Sequence number out of bounds (TotalChunks: ${totalChunksExpected}). (DataProc: ${dataProcessingDuration.toFixed(1)}ms)`, true);
                timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms (ERR_BOUNDS), Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;
                return; // Discard this chunk
            }

            if (!collectedChunks.has(sequenceNum)) {
                collectedChunks.set(sequenceNum, originalChunkData);
                if (sequenceNum > highestSequenceNumberSeen) {
                    highestSequenceNumberSeen = sequenceNum; // Still track highest for fallback/logging
                }
                updateReceivedSequenceDisplay();
            }

            t5 = performance.now();
            dataProcessingDuration = t5 - t4;
            updateStatus(`Frame ${frameCounter}: Chunk ${sequenceNum} (of ${totalChunksExpected > 0 ? totalChunksExpected : '?'}, len ${dataLength}) OK. (DataProc: ${dataProcessingDuration.toFixed(1)}ms) Total unique: ${collectedChunks.size}.`);
            timingInfo.textContent = `Capture: ${captureDuration.toFixed(1)}ms, Scan: ${scanDuration.toFixed(1)}ms, Data: ${dataProcessingDuration.toFixed(1)}ms, Total: ${(captureDuration + scanDuration + dataProcessingDuration).toFixed(1)}ms`;

            // Check for auto-stop condition
            if (totalChunksExpected !== -1 && collectedChunks.size === totalChunksExpected) {
                if (frameProcessorIntervalId !== null) { // Check if already stopping/stopped
                    updateStatus(`All ${totalChunksExpected} chunks received. Auto-stopping capture...`);
                    console.log(`All ${totalChunksExpected} chunks received. Auto-stopping capture.`);
                    stopCapture();
                    // Note: stopCapture itself clears frameProcessorIntervalId,
                    // so this check ensures stopCapture is effectively called once.
                }
            }

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

async function decompressGzipStream(compressedData: Uint8Array): Promise<Uint8Array> {
    if (typeof DecompressionStream === 'undefined') {
        console.error("DecompressionStream API not available in this browser.");
        throw new Error("DecompressionStream API not available.");
    }
    const ds = new DecompressionStream('gzip');
    const writer = ds.writable.getWriter();
    writer.write(compressedData);
    writer.close();

    const chunks: Uint8Array[] = [];
    let totalSize = 0;
    const reader = ds.readable.getReader();
    while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        chunks.push(value);
        totalSize += value.length;
    }

    const decompressed = new Uint8Array(totalSize);
    let offset = 0;
    for (const chunk of chunks) {
        decompressed.set(chunk, offset);
        offset += chunk.length;
    }
    return decompressed;
}


async function assembleData() { // Made async
    console.log("[assembleData] Called."); // Removed zstdSimple state log
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

    let numChunksToAssemble = 0;
    if (totalChunksExpected > 0) {
        numChunksToAssemble = totalChunksExpected;
        updateStatus(`Assembling based on header TotalChunks: ${numChunksToAssemble}. Collected: ${collectedChunks.size}.`);
        if (collectedChunks.size < numChunksToAssemble) {
            updateStatus(`Warning: Not all expected chunks received. Expected ${numChunksToAssemble}, Got ${collectedChunks.size}. Assembling what's available.`, true);
        }
    } else {
        // Fallback if totalChunksExpected was not set (e.g. old format, or header TotalChunks was 0)
        numChunksToAssemble = highestSequenceNumberSeen + 1;
        if (numChunksToAssemble === 0 && collectedChunks.size > 0) { // only one chunk, seq 0
            numChunksToAssemble = 1;
        }
        updateStatus(`TotalChunks not definitively known from header. Assembling up to highest seen sequence ${highestSequenceNumberSeen} (implying ${numChunksToAssemble} chunks). Collected: ${collectedChunks.size}.`, false);
    }

    if (numChunksToAssemble === 0 && collectedChunks.size === 0) { // Handles case where highestSequenceNumberSeen = -1
        updateStatus("No data chunks collected to assemble.", false);
        outputTextarea.value = "<No data collected>";
        progressOverview.textContent = "No chunks collected.";
        downloadLink.style.display = 'none';
        return;
    }


    // Check for missing chunks up to numChunksToAssemble - 1
    let missingChunksExist = false;
    let firstMissing = -1;
    for (let i = 0; i < numChunksToAssemble; i++) {
        if (!collectedChunks.has(i)) {
            missingChunksExist = true;
            firstMissing = i;
            updateStatus(`Error: Missing chunk with sequence number: ${i}`, true);
            break;
        }
    }

    if (missingChunksExist) {
        progressOverview.textContent = `Data assembly incomplete. Missing chunk(s) (e.g., seq ${firstMissing}). Collected ${collectedChunks.size} of expected ${numChunksToAssemble}.`;
        outputTextarea.value = `<Incomplete data: Missing chunk(s). First missing: ${firstMissing}. Expected ${numChunksToAssemble} total.>`;
        downloadLink.style.display = 'none';
        return;
    }

    // If we are here, all chunks up to numChunksToAssemble are present.
    progressOverview.textContent = `All ${numChunksToAssemble} chunks (0 to ${numChunksToAssemble > 0 ? numChunksToAssemble - 1 : 0}) received! Assembling...`;

    // Calculate total size for the reassembledCompressedData buffer
    let totalSize = 0;
    for (let i = 0; i < numChunksToAssemble; i++) { // Use numChunksToAssemble
        const chunkData = collectedChunks.get(i); // Should exist due to missingChunksExist check
        if (chunkData) {
            totalSize += chunkData.length;
        }
        // No else needed here because missingChunksExist would have been true and an error reported already.
    }

    const reassembledCompressedData = new Uint8Array(totalSize);
    let currentOffset = 0;
    // Iterate up to numChunksToAssemble (which is derived from totalChunksExpected or highestSequenceNumberSeen)
    for (let i = 0; i < numChunksToAssemble; i++) {
        const chunk = collectedChunks.get(i)!; // Assumes missing chunks check has passed or is handled
        reassembledCompressedData.set(chunk, currentOffset);
        currentOffset += chunk.length;
    }

    updateStatus(`Gzipped data reassembled. Total gzipped size: ${reassembledCompressedData.length} bytes. Decompressing...`);

    let finalDecompressedData: Uint8Array;
    try {
        finalDecompressedData = await decompressGzipStream(reassembledCompressedData);
        updateStatus(`Data decompressed successfully using Gzip. Original size: ${finalDecompressedData.length} bytes.`);
    } catch (err: any) {
        updateStatus(`Error during Gzip decompression: ${err.message || err}. Displaying raw gzipped data instead.`, true);
        finalDecompressedData = reassembledCompressedData;
        outputTextarea.value = "<Error during Gzip decompression. Raw (gzipped) data might be available for download.>";
    }


    // Try to display as text (UTF-8)
    try {
        const textDecoder = new TextDecoder('utf-8', { fatal: true });
        outputTextarea.value = textDecoder.decode(finalDecompressedData);
        updateStatus("Displayed final (decompressed) data as UTF-8 text.");
    } catch (e) {
        updateStatus("Final (decompressed) data is not valid UTF-8 text, or contains null characters. Displaying as hex.", false);
        let hexString = '';
        for (let i = 0; i < Math.min(finalDecompressedData.length, 1024); i++) {
            hexString += finalDecompressedData[i].toString(16).padStart(2, '0');
        }
        outputTextarea.value = hexString + (finalDecompressedData.length > 1024 ? "\n... (data truncated in hex preview)" : "");
    }

    // Offer download of the final (ideally decompressed) data
    const blob = new Blob([finalDecompressedData], { type: 'application/octet-stream' });
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
