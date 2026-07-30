# フレームヘッダ実装ガイド（RFC 9639 §9.1）

対象: `frame.go`
前提: メタデータ層（§6, §8.1, §8.2）実装済み、`Decoder.src` は `*bufio.Reader`（メタデータ読み込み後にラップ済み）

---

## 0. 全体の分割と実装順

フレームヘッダは 5 つの部品に分かれる。**依存関係が少ない順**に作ると、各段階でテストが閉じる。

| Step | 内容 | RFC | 依存 |
|---|---|---|---|
| **1** | **固定 4 バイトの分解 + テーブル引き + MUST 検証** | §9.1–§9.1.4 | なし |
| 2 | coded number（UTF-8 風可変長） | §9.1.5 | なし（独立した純粋関数） |
| 3 | CRC-8 | §9.1.8 | なし（独立した純粋関数） |
| 4 | uncommon block size / sample rate + 全体の組み立て | §9.1.6, §9.1.7 | 1, 2, 3 |
| 5 | `Peek`/`Discard` の I/O シェル + streaminfo 突き合わせ | §9.1, §8.2 | 4 |

このドキュメントは **Step 1** を扱う。

---

## 1. 先に決めること: `parseFrameHeader` は純粋関数にする

`Peek(16)` → パース → `Discard(n)` という方針から、パース本体のシグネチャが決まる。

```
parseFrameHeader(b []byte, si streamInfo) (frameHeader, int, error)
                                                        ↑ 実際に消費したバイト数 n（Discard に渡す）
```

`[]byte` を受ける純粋関数にする理由:

- `Peek` が返すのは `[]byte` なので、そのまま渡せる
- **テストが `bufio` を必要としない**。バイト列を直接渡せる（メタデータ層のテストと同じスタイルを維持できる）
- 消費バイト数を戻り値にすることで、I/O 側（`Discard`）と RFC 側（パース）の責務が完全に分かれる

I/O を伴う `readFrameHeader(br *bufio.Reader, si streamInfo)` は、`Peek` → `parseFrameHeader` → `Discard` の 3 行のシェルになる。**RFC のロジックは全部 `parseFrameHeader` の中**、という配置。

### ヘッダ長の上限は 16 バイト

RFC のビット幅から導出できる事実。`Peek` に渡す値の根拠になる。

```
  4  sync(15) + blocking strategy(1) + block size bits(4) + sample rate bits(4)
     + channel bits(4) + bit depth bits(3) + reserved(1)
+ 7  coded number 最大（§9.1.5 Table 18）
+ 2  uncommon block size 最大（§9.1.6）
+ 2  uncommon sample rate 最大（§9.1.7）
+ 1  CRC-8（§9.1.8）
────
 16
```

> Step 5 の注意: ファイル末尾では 16 バイト取れない。`Peek(16)` は取れた分のスライスと `io.EOF` を返すので、`len(b) > 0` なら短いスライスで続行する扱いが要る。`example_1.flac` はヘッダ 7 + サブフレーム 7 + フッタ 2 = ちょうど 16 バイトで通るが、これは偶然。

### 関数内部の処理順

```
① b[0..1]  sync + blocking strategy                      §9.1
② b[2..3]  4つのコード値を取り出すだけ（まだ解決しない）    §9.1.1-9.1.4
③ b[3]&1   reserved ビット検証（MUST be zero）            §9.1.4
④          Reserved / Forbidden のコード値を弾く          §9.1.1-9.1.4
⑤ b[4..]   coded number を読む → オフセット前進           §9.1.5
⑥          uncommon block size を読む（必要なら）          §9.1.6
⑦          uncommon sample rate を読む（必要なら）         §9.1.7
⑧          ②のコード値と⑥⑦の値から最終値を確定
⑨          CRC-8 検証                                     §9.1.8
```

**② と ⑧ を分けるのが肝。** ブロックサイズとサンプルレートの「実際の値」は、後続バイト（⑥⑦）まで読まないと確定しない。②で無理に確定させようとすると詰まる。

一方 **④（Reserved / Forbidden の検証）は②の直後**に置く。不正なコード値のまま⑤以降でゴミをパースしても意味がないので、早く落とす。

---

## 2. Step 1: 固定 4 バイトの分解

### 2.1 バイト列 → ビット → 値

`example_1.flac` の `0x2a` からの先頭 4 バイト `ff f8 69 18` を例に、RFC のレイアウトと突き合わせる。

```
b[0] = 0xff = 1111 1111    ┐ 15-bit sync code (§9.1)
b[1] = 0xf8 = 1111 100 0   ┘                 + blocking strategy(1)
                       ^blocking strategy

b[2] = 0x69 = 0110  1001
              ^^^^  ^^^^
              │     └─ sample rate bits (§9.1.2, Table 15)
              └─────── block size bits   (§9.1.1, Table 14)

b[3] = 0x18 = 0001  100  0
              ^^^^  ^^^  ^
              │     │    └─ reserved (§9.1.4「MUST be zero」)
              │     └────── bit depth bits (§9.1.4, Table 17)
              └──────────── channel bits   (§9.1.3, Table 16)
```

### 2.2 sync + blocking strategy（§9.1）

> Each frame MUST start on a byte boundary and start with the 15-bit frame sync code `0b111111111111100`.

15 bit を数値にすると `0b111111111111100` = **`0x7FFC`**（1 が 13 個 + `00`）。取り出し方は 2 通り、どちらでもよい。

- `b[0]` が `0xFF` かつ `b[1] & 0xFE` が `0xF8`
- `(uint16(b[0])<<8 | uint16(b[1])) >> 1` が `0x7FFC`

blocking strategy は `b[1] & 0x01`。RFC 本文が *"the first two bytes of a frame are either 0xFFF8 ... or 0xFFF9"* と書いているのは、この 2 つを合わせた結果。

- **型の判断**: blocking strategy は bool でも独自型でもよいが、§9.1.5 で「0 ならフレーム番号 / 1 ならサンプル番号」と意味が分岐するので、`bool` より**名前付き定数型**（fixed / variable）の方が呼び出し側が読みやすい。
- **検証**: sync が一致しなければエラー（§9.1 MUST）。これは同時に「メタデータ層で位置がずれていないか」の検出網にもなる。
- **将来の検証**: blocking strategy は *"MUST NOT change during the audio stream"*。2 フレーム目以降で最初のフレームと比較する項目（Step 5 以降）。

### 2.3 block size bits（§9.1.1, Table 14）

`b[2] >> 4` の 4 bit を v とする。

| v | ブロックサイズ | 後続バイト |
|---|---|---|
| `0b0000` | **Reserved** → エラー | — |
| `0b0001` | 192 | 0 |
| `0b0010`–`0b0101` | `144 << v`（576 / 1152 / 2304 / 4608） | 0 |
| `0b0110` | 後続 8-bit の値 + 1 | **1** |
| `0b0111` | 後続 16-bit の値 + 1 | **2** |
| `0b1000`–`0b1111` | `1 << v`（256 / 512 / 1024 / 2048 / 4096 / 8192 / 16384 / 32768） | 0 |

**RFC のビット幅から導出できる事実**: `144 * 2^v` と `2^v` は式で計算できる（`144 << v`、`1 << v`）。テーブルを配列で持つ必要はない。一方、次のサンプルレートは式にできない。

### 2.4 sample rate bits（§9.1.2, Table 15）

`b[2] & 0x0F`。

| 値 | サンプルレート | 後続バイト |
|---|---|---|
| `0b0000` | **streaminfo の値を使う** | 0 |
| `0b0001` | 88200 | 0 |
| `0b0010` | 176400 | 0 |
| `0b0011` | 192000 | 0 |
| `0b0100` | 8000 | 0 |
| `0b0101` | 16000 | 0 |
| `0b0110` | 22050 | 0 |
| `0b0111` | 24000 | 0 |
| `0b1000` | 32000 | 0 |
| `0b1001` | 44100 | 0 |
| `0b1010` | 48000 | 0 |
| `0b1011` | 96000 | 0 |
| `0b1100` | 後続 8-bit × 1000（kHz 単位） | **1** |
| `0b1101` | 後続 16-bit（Hz 単位） | **2** |
| `0b1110` | 後続 16-bit × 10（Hz÷10 単位） | **2** |
| `0b1111` | **Forbidden** → エラー | — |

注意点 3 つ:

1. 値が昇順でも規則的でもないので、**配列 / switch のテーブル引きが必須**。ブロックサイズと違って式にできない
2. `0b1100` / `0b1101` / `0b1110` は**単位がそれぞれ違う**（kHz / Hz / Hz÷10）。実装ミスが起きやすい箇所
3. 型: 最大値は `0b1110` の 65535 × 10 = **655350 Hz**。streaminfo は u(20) で最大 1048575。どちらも `uint32` に収まる

### 2.5 channel bits（§9.1.3, Table 16）

`b[3] >> 4`。**チャンネル数とステレオデコリレーション方式の 2 つの情報が混ざっている。**

| 値 | チャンネル数 | デコリレーション |
|---|---|---|
| `0b0000`–`0b0111` | 値 + 1（1〜8） | なし（independent） |
| `0b1000` | 2 | left-side |
| `0b1001` | 2 | side-right |
| `0b1010` | 2 | mid-side |
| `0b1011`–`0b1111` | **Reserved** → エラー | — |

**設計判断**: `channels uint8` と `stereoMode`（独自型）の **2 フィールドに分けて持つ**。理由は 2 つあり、どちらも後段で効く。

- §4.2 のステレオ復元で、モードによって計算が分岐する
- §9.2 のサブフレームで、side チャンネルは **1 bit 深く**格納される（left-side なら 2 番目、side-right なら 1 番目、mid-side なら 2 番目が side）。ここでモードとチャンネル位置の両方が要る

### 2.6 bit depth bits（§9.1.4, Table 17）

`(b[3] >> 1) & 0x07`。

| 値 | ビット深度 |
|---|---|
| `0b000` | **streaminfo の値を使う** |
| `0b001` | 8 |
| `0b010` | 12 |
| `0b011` | **Reserved** → エラー |
| `0b100` | 16 |
| `0b101` | 20 |
| `0b110` | 24 |
| `0b111` | 32 |

### 2.7 reserved ビット（§9.1.4）

> The next bit is reserved and MUST be zero.

`b[3] & 0x01` が 0 でなければ MUST 違反なのでエラーにできる。

---

## 3. `frameHeader` 型の設計

| フィールド | 型 | 理由 |
|---|---|---|
| `blockingStrategy` | 独自型 | §9.1.5 で coded number の意味が分岐するため、bool より意図が明確 |
| `blockSize` | `uint16` | §9.1.6 が 65536 を禁止しているので最大 65535。`uint16` にちょうど収まる |
| `sampleRate` | `uint32` | 最大 655350 Hz（§2.4 参照） |
| `channels` | `uint8` | 1〜8 |
| `stereoMode` | 独自型 | §2.5 の理由 |
| `bitsPerSample` | `uint8` | 4〜32 |
| `codedNumber` | `uint64` | §9.1.5 で最大 36 bit |

`blockSize` が `uint16` にちょうど収まるのは偶然ではなく、§9.1.6 の
*"A value of 65535 ... is forbidden and MUST NOT be used, because such a block size cannot be represented in the streaminfo metadata block"*（streaminfo が u(16) だから）に由来する。**RFC の制約が Go の型選択に直結している例。**

### 判断が必要な点: `0b0000`（streaminfo 参照）の扱い

- **案 A**: `parseFrameHeader` に `streamInfo` を渡し、**解決済みの値だけ**を `frameHeader` に入れる
- 案 B: 生のコード値を残し、解決は呼び出し側でやる

案 A を推奨。`frameHeader` が自己完結し、下流（サブフレーム読み）が単純になる。テストでは合成した `streamInfo` を渡せばよく、テスタビリティも落ちない。

---

## 4. テストベクタ

すべて `testdata/` の実ファイルから取得し、CRC-8 の一致まで確認済み。**先頭 4 バイト**が Step 1 の対象で、それ以降は Step 2 以降の予告として載せる。

| ファイル | オフセット | ヘッダ全体 | b[0..3] から読めること |
|---|---|---|---|
| `flac-specification/example_1.flac` | `0x2a` | `ff f8 69 18` `00` `00` `bf` | fixed / bs=`0b0110`(8bit後続) / 44100 / 2ch independent / 16bit |
| `flac-specification/example_2.flac` | `0x88` | `ff f8 69 98` `00` `0f` `99` | fixed / bs=`0b0110` / 44100 / **side-right** / 16bit |
| `flac-specification/example_3.flac` | `0x2a` | `ff f8 68 02` `00` `17` `e9` | fixed / bs=`0b0110` / 32000 / **1ch mono** / **8bit** |
| `subset/04 - blocksize 192.flac` | `0x88` | `ff f8 19 18` `00` `ed` | bs=`0b0001` → **192**（後続なし） |
| `subset/02 - blocksize 4608.flac` | `0x88` | `ff f8 59 18` `00` `6b` | bs=`0b0101` → **144<<5 = 4608** |
| `subset/38 - 3 channels (3.0).flac` | `0x88` | `ff f8 c9 28` `00` `3b` | bs=`0b1100` → **1<<12 = 4096** / **3ch** |
| `subset/22 - 12 bit per sample.flac` | `0x88` | `ff f8 c9 a4` `00` `71` | mid-side / **12bit** |
| `subset/37 - 20 bit per sample.flac` | `0x88` | `ff f8 cb aa` `00` `71` | 4096 / sr=`0b1011`→**96000** / mid-side / **20bit** |
| `uncommon/05 - 32bps audio.flac` | `0x88` | `ff f8 c9 1e` `00` `bc` | 2ch independent / **32bit**（bd=`0b111`） |
| `subset/19 - samplerate 35467Hz.flac` | `0x88` | `ff f8 cd 88` `00` `8a 8b` `92` | sr=`0b1101`(16bit Hz後続) / **left-side** |
| `subset/20 - samplerate 39kHz.flac` | `0x88` | `ff f8 cc a8` `00` `27` `11` | sr=`0b1100`(8bit kHz後続) / mid-side |
| `subset/35 - samplerate 134560Hz.flac` | `0x88` | `ff f8 ce ac` `00` `34 90` `7a` | sr=`0b1110`(16bit Hz÷10後続) / mid-side / 24bit |
| `subset/24 - variable blocksize...flac` | `0x2048` | `ff f9` `b9 18` `00` `b3` | **`0xf9` → variable** / bs=`0b1011` → `1<<11`=2048 |
| `subset/07 - blocksize 725.flac` | `0x2070` | `ff f8 79 18` `00` `02 d4` `0e` | bs=`0b0111`(16bit後続) → 0x02d4+1 = **725** |
| `uncommon/08 - blocksize 65535.flac` | `0x88` | `ff f8 79 a8` `00` `ff fe` `bd` | 0xfffe+1 = **65535**（禁止値 65536 の直前） |

`0x1e`（32bps）の分解を確認しておくと:
`0x1e = 0001 111 0` → channel bits `0b0001`（2ch independent）、bit depth `0b111`（32 bit）、reserved 0。
`(0x1e >> 1) & 0x07` = `0x0F & 0x07` = `0b111`。

### 異常系（Step 1 で弾けるもの）

正常ベクタの 1 バイトを書き換えて合成する。

| ケース | 入力（b[0..3]） | 根拠 |
|---|---|---|
| sync 不一致 | `ff f0 69 18` | §9.1 MUST |
| block size bits Reserved | `ff f8 `**`0`**`9 18` | §9.1.1 Table 14 |
| sample rate bits Forbidden | `ff f8 6`**`f`**` 18` | §9.1.2 Table 15 |
| channel bits Reserved | `ff f8 69 `**`b`**`8` | §9.1.3 Table 16（`0b1011`） |
| bit depth bits Reserved | `ff f8 69 1`**`6`** | §9.1.4 Table 17（`0001 011 0`） |
| reserved bit が 1 | `ff f8 69 1`**`9`** | §9.1.4「MUST be zero」 |

`0x16` = `0001 011 0` → channel `0b0001`、bit depth `0b011`（Reserved）、reserved 0。
`0x19` = `0001 100 1` → bit depth `0b100`（16bit、正常）、reserved **1**（違反）。

### ヘッダのバイト列を自分で確認するには

```sh
xxd -s 0x2a -l 16 testdata/flac-specification/example_1.flac
```

---

## 5. Step 1 のまとめ

**参照した RFC**

- §9.1 — sync code と blocking strategy
- §9.1.1 Table 14 — block size bits
- §9.1.2 Table 15 — sample rate bits
- §9.1.3 Table 16 — channel bits
- §9.1.4 Table 17 — bit depth bits と reserved ビット

**理解すべき要点**

1. 固定 4 バイトのうち、`b[2]` は 4bit + 4bit、`b[3]` は 4bit + 3bit + 1bit に分かれる
2. block size は式（`144<<v`, `1<<v`）、sample rate はテーブル引き。この非対称性が RFC の表にそのまま現れている
3. `b[2]` / `b[3]` の**コード値を取り出す**ことと、**実際の値に解決する**ことは別の工程。解決は後続バイト（§9.1.6 / §9.1.7）を読んだ後
4. Step 1 で弾ける MUST 違反は 4 つ — block size Reserved / sample rate Forbidden / channel Reserved / bit depth Reserved、加えて reserved ビット

**通るべきテスト**

- 上表 15 個の実ファイル由来ベクタで、先頭 4 バイトから blocking strategy / チャンネル数 / ステレオモード / ビット深度が期待通りに得られること
- 異常系 6 ケースがエラーになること

**検証コマンド**

```sh
go test ./... -run TestParseFrameHeader -v
```

---

## 6. 次に来るもの（予告）

型設計に影響するので先に共有しておく。

### Step 2: coded number（§9.1.5）

UTF-8 に似た可変長だが、RFC 3629 の 4 バイトではなく **最大 7 バイト / 36 bit** に拡張されている。

```
0xxxxxxx                    → 1 byte,  7 bit
110xxxxx 10xxxxxx           → 2 byte, 11 bit
1110xxxx 10xxxxxx ×2        → 3 byte, 16 bit
11110xxx 10xxxxxx ×3        → 4 byte, 21 bit
111110xx 10xxxxxx ×4        → 5 byte, 26 bit
1111110x 10xxxxxx ×5        → 6 byte, 31 bit
11111110 10xxxxxx ×6        → 7 byte, 36 bit
```

- blocking strategy が fixed → **フレーム番号**、variable → **先頭サンプル番号**
- フレーム番号のときは *"MUST NOT be larger than ... 31 bits unencoded or 6 bytes encoded"*（7 バイト形式は使えない）
- 継続バイトが `10xxxxxx` でない、先頭が `10xxxxxx` や `0xFF` は不正
- **判断が必要**: overlong encoding（値 0 を 2 バイトで符号化など）を拒否するか。RFC 9639 §9.1.5 に明示的な禁止文はないが「Section 3 of RFC 3629 の手順に従え」とあり、RFC 3629 側は禁止している。これは**参照先からの推論**なので、方針を決めてコメントに残す

多バイトの実物（`subset/03 - blocksize 16.flac`）:

| オフセット | ヘッダ | coded number |
|---|---|---|
| `0x0391f` | `ff f8 69 18 c2 80 0f f4` | `c2 80` → `(0b00010 << 6) \| 0b000000` = **128** |
| `0x03952` | `ff f8 69 18 c2 81 0f e1` | **129** |
| `0x03985` | `ff f8 69 18 c2 82 0f de` | **130** |
| `0x039b7` | `ff f8 69 a8 c2 83 0f 53` | **131**（このフレームだけ mid-side） |

RFC 本文にも例がある（§9.1.5、51 billion samples → 7 バイト）。

### Step 3: CRC-8（§9.1.8）

> This CRC is initialized with 0 and has the polynomial x^8 + x^2 + x^1 + x^0. This CRC covers the whole frame header before the CRC, including the sync code.

多項式 `0x07`、初期値 0、MSB first、反転なし、xorout なし。Go 標準ライブラリに CRC-8 はないので自前実装（1 bit ずつ、または 256 エントリのテーブル）。**sync code を含む**点に注意。

検証: `ff f8 69 18 00 00` の CRC-8 が `0xbf`（example_1）、`ff f8 68 02 00 17` が `0xe9`（example_3）。

### Step 4 の罠 2 つ

- **65536 のオーバーフロー**: `0b0111`（16-bit uncommon block size）で値 65535 は §9.1.6 で禁止。`uint16` で「+1」してから検証すると 0 に巻き戻るので、**+1 する前に検証**する。`uncommon/08 - blocksize 65535.flac` の `0xfffe`（→ 65535）が境界のすぐ手前で良いテストになる
- **ブロックサイズ 1〜15 は最終フレームのみ**（§9.1.6 MUST NOT）。`example_1.flac` はブロックサイズ 1 だが唯一のフレームなので合法。「最終フレームかどうか」は先読みしないと分からないので、後追い検証にするかスコープ外にするかの判断が要る

### Step 5: streaminfo との突き合わせ

§9.1 は *"a decoder MAY choose to stop decoding on such a change"*、§8.2 は「streaminfo を無検証で信じると buffer overflow の危険」と述べている。つまり「フレーム間で属性が変わったら止める」も「追従する」も **RFC 上はどちらも許容**。

「最初のフレームの属性を記録し、変化したらエラー」が単純で安全だが、これは**実装方針であって RFC 要件ではない**ことをコメントに残す。`uncommon/01`〜`04`（changing samplerate / channels / bitdepth）がこの分岐のテスト材料になる。

---

## 参考

- RFC 9639: https://www.rfc-editor.org/rfc/rfc9639 （ローカルコピー: `rfc9639.txt`）
- Appendix D.1.4 Table 29 — example_1 のフレームヘッダ分解
- Appendix D.3.4 Table 46 — example_3 のフレームヘッダ分解
- `testdata/flac-test-files/` — IETF テストベンチ（CC0）
