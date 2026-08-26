# flac

A pure Go FLAC ([RFC 9639](https://rfc-editor.org/rfc/rfc9639)) decoder with zero dependencies.

## Performance

**1.5× faster, with 1/29 the allocated memory and 1/90 the allocations**, compared to [mewkiz/flac@v1.0.14](https://github.com/mewkiz/flac).

The benchmark decodes `flac-test-files/subset/01 - blocksize 4096.flac` end to end.  
Both decoders do: decode every frame, verify the frame header CRC-8 and frame footer CRC-16, and verify the MD5 of the decoded PCM.  
Additionally, this decoder writes the interleaved PCM to the caller's buffer.

```text
goos: darwin
goarch: arm64
pkg: github.com/takafumiokamoto/flac/benchmark
cpu: Apple M5
          │  bench.log  │
          │   sec/op    │
This-10     11.56m ± 1%
Mewkiz-10   17.64m ± 0%
geomean     14.28m

          │  bench.log   │
          │     B/s      │
This-10     45.37Mi ± 1%
Mewkiz-10   29.74Mi ± 0%
geomean     36.73Mi

          │  bench.log   │
          │     B/op     │
This-10     84.93Ki ± 0%
Mewkiz-10   2.421Mi ± 0%
geomean     458.8Ki

          │  bench.log  │
          │  allocs/op  │
This-10      18.00 ± 0%
Mewkiz-10   1.620k ± 0%
geomean      170.8
```

### Reproduce

```shell
go install golang.org/x/perf/cmd/benchstat@latest
git clone --recurse-submodules https://github.com/takafumiokamoto/flac
cd flac/benchmark
go test -run='^$' -bench=. -benchmem -count=10 > bench.log
benchstat bench.log
```

## Usage

flac decodes a FLAC stream into PCM as defined in RFC 9639 §8.2: interleaved, signed, little-endian, each sample sign-extended to whole bytes.

### One-shot

```go
pcm, err := flac.Decode(r) // r is io.Reader (can be file or stream)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%d Hz, %d ch, %d bit, %d bytes\n",
    pcm.SampleRate, pcm.Channels, pcm.BitsPerSample, len(pcm.Data))
```

### Streaming

```go
dec, err := flac.NewDecoder(r)
if err != nil {
    log.Fatal(err)
}
// format is available via dec.StreamInfo()
buf := make([]byte, 8192)
for {
    n, err := dec.Read(buf)
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    // do something with buf[:n]
}
```

`Decoder` is an `io.Reader`, so the PCM can be copied into any `io.Writer` with `io.Copy(w, dec)`.

```go
if _, err := io.Copy(w, dec); err != nil {
    log.Fatal(err)
}
```

## Conformance test status

Tested against the IETF FLAC decoder testbench
[ietf-wg-cellar/flac-test-files](https://github.com/ietf-wg-cellar/flac-test-files) (commit `aa7b0c6`).

### Expectations

**subset**

- the whole stream decodes without error
- the frame header CRC-8 matches for every frame (RFC 9639 §9.1.8)
- the frame footer CRC-16 matches for every frame (§9.3)
- the MD5 of the decoded PCM equals the checksum stored in STREAMINFO (§8.2)

**uncommon**

- same checks as `subset`
- known limitation: files 10 and 11 begin without the `fLaC` marker, raw FLAC steams are not supported yet.

**faulty**

- The decoder does not crash or hang.

### Results

| Group      | Files | Result   |
| ---------- | ----: | -------- |
| `subset`   |    64 | **PASS** |
| `uncommon` |    11 | 9/11     |
| `faulty`   |    11 | **PASS** |

### Reproduce

```shell
git clone --recurse-submodules https://github.com/takafumiokamoto/flac
cd flac
go test ./...
```
