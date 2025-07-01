module qrvidproject

go 1.22

toolchain go1.24.3

require (
	github.com/boombuler/barcode v1.0.2
	github.com/cespare/xxhash/v2 v2.3.0
	// github.com/klauspost/compress/zstd v1.16.0 // Removed to let 'go test' resolve it
	github.com/makiuchi-d/gozxing v0.1.1
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
)

require (
	github.com/klauspost/compress v1.18.0 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
)
