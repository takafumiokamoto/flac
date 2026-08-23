# flac

A pure Go FLAC ([RFC 9639](https://rfc-editor.org/rfc/rfc9639)) decoder with zero dependencies.

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
io.Copy(f, dec)
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

**faulty**

- The decoder does not crash or hang.

### Results

| Group      | Files | Result         |
| ---------- | ----: | -------------- |
| `subset`   |    64 | **PASS**       |
| `uncommon` |    11 | not tested yet |
| `faulty`   |    11 | **PASS**       |

### Reproduce

```shell
git clone --recurse-submodules https://github.com/takafumiokamoto/flac
cd flac
go test ./...
```
