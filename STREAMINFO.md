# STREAMINFO デコードガイド

## このステップの目的

[RFC 9639 Section 8.2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2) に従い、STREAMINFO メタデータブロックの34バイトのペイロードを Go の値へ復号する。34バイトという長さは、同節のフィールド定義を合計した272ビットから導かれる（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。

このステップでは、次の範囲だけを扱う。

- STREAMINFO ペイロードを固定長で読み込む
- 各フィールドを仕様どおりに取り出す
- STREAMINFO 自体で判定できる不正値をエラーにする
- 復号結果を `Decoder` が保持できる形にする

後続メタデータブロックの読み飛ばしや音声フレームの処理は、次のステップに分ける。

この文書では、RFCが定める形式・制約には根拠を併記する。関数の分割、Goの型、エラーを返す場所などはRFCの規定ではなく、この実装における設計判断である。

## RFC の根拠

- [RFC 9639 Section 2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-2-2): `u(n)` と `s(n)` の意味、および big-endian の規約。
- [RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1): メタデータの配置と、先頭ブロックが STREAMINFO であるという規定。
- [RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1): メタデータブロックヘッダーの構造と、24ビットのペイロード長。
- [RFC 9639 Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1): STREAMINFO の順序と個数の制約。
- [RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3): STREAMINFO の全フィールド構成。
- [RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5): minimum/maximum block size の制約。
- [RFC 9639 Section 8.2, paragraph 8](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-8): sample rate 0 の扱い。
- [RFC 9639 Section 11, paragraph 3](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3): malformed payload によるメモリ超過や過剰なリソース消費の禁止。
- [RFC 9639 Section 11, paragraph 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-6): メタデータ内の長さを上位のブロック長と照合してからメモリを確保するという規定。
- [RFC 9639 Appendix D.1.3](https://www.rfc-editor.org/rfc/rfc9639.html#appendix-D.1.3): 検証済みの具体例。

STREAMINFO のペイロードは、Table 3 に記載された各フィールドの合計34バイトで構成される（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。

```text
2 + 2 + 3 + 3 + 8 + 16 = 34 bytes
```

メタデータブロックヘッダーの4バイトは、この34バイトには含まれない。ヘッダーに格納される長さが「ヘッダーを除いたブロック本体の長さ」であるためである（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。

## 責務の分離

既存の `readMetadataBlockHeader` と STREAMINFO の復号処理は分ける。

`NewDecoder` 側の責務:

1. `readMetadataBlockHeader` を呼ぶ。
2. 最初のブロックの型が STREAMINFO であることを確認する（[RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)）。
3. ブロック長が、Table 3 の合計サイズである34バイトか確認する（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。
4. `readStreamInfo` を呼ぶ。
5. 結果を `Decoder` に保持する。

`readStreamInfo` 側の責務:

1. ヘッダーを除いた34バイトだけを読む。
2. 各フィールドを復号する。
3. STREAMINFO 内で完結する制約を検証する（[RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2)、[paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
4. 復号結果またはエラーを返す。

そのため、関数は値を返す形にする。

```go
const streamInfoLength = 34

func readStreamInfo(r io.Reader) (streamInfo, error)
```

公開するメタデータ API はまだ決めていないため、`streamInfo` は当面非公開でよい。これは実装上の方針であり、RFCはAPIの形を規定しない。

## フィールド構成

以下は [RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3) を、34バイトのペイロード先頭をオフセット0として書き直したもの。

| バイト位置 | RFC 上の型 | 内容 | Go の型候補 |
|---|---:|---|---:|
| `0:2` | `u(16)` | minimum block size | `uint16` |
| `2:4` | `u(16)` | maximum block size | `uint16` |
| `4:7` | `u(24)` | minimum frame size | `uint32` |
| `7:10` | `u(24)` | maximum frame size | `uint32` |
| `10:18` | 複合64ビット | sample rate、channels、bits per sample、total samples | `uint64` |
| `18:34` | `u(128)` | PCM の MD5 | `[16]byte` |

構造体の候補は次のとおり。Goの型の選択は実装上の判断である。

```go
type streamInfo struct {
	minBlockSize  uint16
	maxBlockSize  uint16
	minFrameSize  uint32
	maxFrameSize  uint32
	sampleRate    uint32
	channels      uint8
	bitsPerSample uint8
	totalSamples  uint64
	md5Sum        [16]byte
}
```

フィールド名や整列は実装時に調整してよい。重要なのは、24ビット値を `uint32`、36ビット値を `uint64` で保持することである。

## 読み取り方

入力由来の長さでメモリを確保せず、固定長配列と `io.ReadFull` を使う。固定長にするのは、STREAMINFO のサイズが仕様から決まることに加え、メタデータ値を無検証でバッファサイズなどに使う危険をRFCが指摘しているためである（[RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2)、[Section 11, paragraph 3](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3)、[paragraph 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-6)）。

```go
var buf [streamInfoLength]byte
```

16ビット値は `binary.BigEndian.Uint16` で読める。24ビット整数用の標準関数はないため、既存のメタデータブロック長と同様にシフトと OR で組み立てる。

```text
uint32(buf[i]) << 16 | uint32(buf[i+1]) << 8 | uint32(buf[i+2])
```

RFC 9639 の `u(n)` は、`n` ビットの unsigned big-endian integer を意味する（[RFC 9639 Section 2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-2-2)）。

## 複合64ビットフィールド

バイト位置 `10:18` は、Table 3 に並ぶ `u(20)`、`u(3)`、`u(5)`、`u(36)` が連続した64ビットである（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。

```text
63                    44 43      41 40          36 35                 0
+-----------------------+----------+--------------+--------------------+
| sample rate: 20 bits  | ch-1: 3  | bps-1: 5     | total samples: 36  |
+-----------------------+----------+--------------+--------------------+
```

まず全体を big-endian の `uint64` として読み、その後にシフトとマスクで分解する。

```go
sampleRate := uint32(packed >> 44)
channels := uint8((packed>>41)&0x7) + 1
bitsPerSample := uint8((packed>>36)&0x1f) + 1
totalSamples := packed & ((uint64(1) << 36) - 1)
```

チャネル数とビット深度は、それぞれ「値 - 1」が保存されるため、復号時に1を加える（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。3ビットのチャネル値は0〜7なので、復号後は必ずRFCが規定する1〜8チャネルになる。

## 値の検証

STREAMINFO の読み取り時点で、少なくとも次を検証する。なお、以下の制約はファイル側に課された MUST であり、違反するファイルは非適合となる。一方で、それを検出したデコーダがエラーで停止すること自体は RFC の要求ではなく、「decoder behavior is left unspecified」「A decoder MAY choose to stop」([RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2))に基づくこの実装の方針である。

- minimum block size が16〜65535であることを検証する（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
- maximum block size が16〜65535であることを検証する（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
- minimum block size が maximum block size 以下であることを検証する（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
- bits per sample が FLAC の仕様範囲である4〜32であることを検証する（[RFC 9639 Section 1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-1-1)、[Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）。

`uint16` で保持すれば、block size の上限65535は型によって自動的に満たされる。これはGoの型選択による実装上の性質である。下限16と大小関係は別途検証が必要になる。

同様に、ビット幅から自動的に満たされる制約がある。bits per sample は `u(5)` の復号値に1を加えるため上限32を超えられず、検証が必要なのは下限4のみである。sample rate も `u(20)` のため、FLAC が表現できる上限1048575 Hzを超えられない([RFC 9639 Section 1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-1-1))。これらはRFCのビット幅から導出した事実である。

minimum block size と maximum block size が等しい場合は固定ブロックサイズのストリームを表す。これは正常な値であり、エラー条件ではない（[RFC 9639 Section 8.2, paragraph 7](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-7)）。

次のゼロ値は不正ではなく「不明」を意味するため、受け入れる。

- minimum frame size が0（unknownを表す。[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）
- maximum frame size が0（unknownを表す。[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）
- total samples が0（unknownを表す。[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）
- MD5 が全ゼロ（unknownを表す。[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）

MD5 の具体的な検証はPCMデコード後に行う。ハッシュ対象は、全チャネルのサンプルをインターリーブし、符号付きlittle-endianでバイト境界に揃えた列である（[RFC 9639 Section 8.2, paragraph 9](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-9)）。バイト境界に揃わないビット深度の符号拡張とバイト整列の具体例（6bit・12bitの図）は [Section 8.2, paragraph 10〜13](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-10) にあり、12/20bit 対応時に参照する。

sample rate 0 は、音声を格納する場合には禁止される一方、非音声データに対しては許可されている（[RFC 9639 Section 8.2, paragraph 8](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-8)）。したがって、STREAMINFO のビット列として直ちに不正と決めつけず、MVP の音声デコーダとして受け入れるかどうかを機能制約の検証として分けて考える。

FLAC 自体は4〜32 bits per sampleを許す（[RFC 9639 Section 1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-1-1)）。そのため、RFC上は有効だが現在のMVPが対応しないビット深度と、4未満という不正値を区別する。未対応値をどの段階でエラーにするかは、この実装の機能制約として決める。

また、STREAMINFO の値を信用してバッファを確保するだけでは不十分である。後でフレームを読む際にも、実際のblock size、channels、bits per sampleなどとの整合性を再確認する（[RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2)）。これはSTREAMINFOデコード後のステップで扱う。

## テストを先に書く

最初の正常系には、RFC Appendix D の `testdata/flac-specification/example_1.flac` を使う。このファイルはRFC本文が詳しく分解している検証済みの例である（[RFC 9639 Appendix D](https://www.rfc-editor.org/rfc/rfc9639.html#appendix-D)、[Appendix D.1.3](https://www.rfc-editor.org/rfc/rfc9639.html#appendix-D.1.3)）。

このファイルでは、4バイトのマーカーと4バイトのメタデータブロックヘッダーに続き、STREAMINFO がファイルオフセット `0x08` から34バイト格納される（[RFC 9639 Table 27](https://www.rfc-editor.org/rfc/rfc9639.html#table-27)）。期待値は [RFC 9639 Table 28](https://www.rfc-editor.org/rfc/rfc9639.html#table-28) のとおりで、「ファイル内位置」列は Table 28 の Start 列（バイトオフセット + ビットオフセット）に対応する。

| フィールド | 期待値 | ファイル内位置（Table 28） |
|---|---:|---|
| minimum block size | `4096` | `0x08+0`（2バイト） |
| maximum block size | `4096` | `0x0a+0`（2バイト） |
| minimum frame size | `15` | `0x0c+0`（3バイト） |
| maximum frame size | `15` | `0x0f+0`（3バイト） |
| sample rate | `44100` | `0x12+0`（20ビット） |
| channels | `2` | `0x14+4`（3ビット、格納値 `0b001`） |
| bits per sample | `16` | `0x14+7`（5ビット、格納値 `0b01111`） |
| total samples | `1` | `0x15+4`（36ビット） |
| MD5 | `3e84b41807dc690307586a3dad1a2e0f` | `0x1a`（16バイト） |

MD5 の期待値だけは Table 28 上で `(...)` と省略されて記載がないため、[RFC 9639 Appendix D.1.1](https://www.rfc-editor.org/rfc/rfc9639.html#appendix-D.1.1) のファイル全体の16進ダンプ（オフセット `0x1a`〜`0x29` のバイト列）から読み取る。

### 最小限のテストケース

1. RFC example 1 の34バイトを Table 28 の値へ正しく復号できる（[RFC 9639 Table 28](https://www.rfc-editor.org/rfc/rfc9639.html#table-28)）。
2. 入力が33バイト以下ならエラーになる。不完全なSTREAMINFOでデコードを停止する方針をテストする（[RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2)）。
3. メタデータブロックヘッダーの長さが、Table 3 から算出した34バイト以外ならエラーになる（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)、[Section 11, paragraph 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-6)）。
4. minimum block size が16未満ならエラーになる（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
5. minimum block size が maximum block size より大きければエラーになる（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)）。
6. bits per sample の復号結果が4未満ならエラーになる（[RFC 9639 Section 1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-1-1)）。

`readStreamInfo` の単体テストでは34バイトのペイロードだけを渡す。最初のブロック型と長さの確認は、`NewDecoder` またはメタデータ読み取り処理のテストに分ける。

## このステップの完了条件

- STREAMINFO の全フィールドを復号できる（[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）
- 切り詰められた入力でエラーを返す（[RFC 9639 Section 8.2, paragraph 2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-2)）
- RFC が定める block size と bit depth の制約を検証する（[RFC 9639 Section 8.2, paragraph 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-5)、[Section 1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-1-1)）
- `NewDecoder` が最初のメタデータブロックの型と長さを確認する（[RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)、[Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3)）
- 復号した STREAMINFO を `Decoder` が保持する
- RFC example 1 を使ったテストが通る（[RFC 9639 Table 28](https://www.rfc-editor.org/rfc/rfc9639.html#table-28)）
- `go test ./...` が通る

後続メタデータの読み飛ばしは、STREAMINFO の復号が動作することを確認した後の独立したステップとする。その走査中に2個目のSTREAMINFOを見つけた場合はエラーにする。1つのFLACストリームにSTREAMINFOを複数置くことは禁止されている（[RFC 9639 Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)）。
