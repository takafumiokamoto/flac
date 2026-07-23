# メタデータ領域の読み取りガイド

## このステップの目的

`fLaC` マーカーの直後からメタデータブロックを順番に読み、最後のメタデータブロックのペイロード末尾まで入力位置を進める。処理が正常に終わった時点で、`io.Reader` の次の1バイトは最初の音声フレームの先頭でなければならない（[RFC 9639 Section 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-6)、[Section 9, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-9-1)）。

ここでいう「メタデータ領域全体の読み取り」は、すべてのメタデータの内容を復号することではない。今回の範囲は次のとおり。

- 先頭の STREAMINFO を復号する
- 各メタデータブロックの4バイトヘッダーを読む
- STREAMINFO 以外のペイロードを、ヘッダーに記録された長さだけスキップする
- `isLast` が立ったブロックを処理し終えたら走査を終了する
- ヘッダーとブロック列だけで判定できる不正をエラーにする
- 切り詰められたヘッダーやペイロードをエラーにする

SEEKTABLE、Vorbis Comment、PICTURE などのペイロード内容は復号しない。音声フレームの同期コードもこのステップでは読まない。これらは実装範囲の分割であり、RFCがAPIや関数の境界を定めているわけではない。

既存の `readMetadataBlockHeader` は「1個のヘッダー」を復号する関数である。このステップでは、それを使って「メタデータブロック列全体」を走査する処理を追加する。

## RFC の根拠

- [RFC 9639 Section 5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-5-1): reserved と forbidden の違い。未知の予約済み方式に遭遇した古いデコーダは、デコードを中止してもよい。
- [RFC 9639 Section 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-6): `fLaC`、必須の STREAMINFO、0個以上の他メタデータ、音声フレームという全体順序。
- [RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1): すべてのメタデータが音声フレームより前にあり、先頭ブロックが STREAMINFO でなければならないこと。
- [RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1): 4バイトヘッダー、last フラグ、ブロック型、24ビット長の定義。
- [RFC 9639 Table 2](https://www.rfc-editor.org/rfc/rfc9639.html#table-2): メタデータブロック型の割り当て。
- [RFC 9639 Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1): STREAMINFO は先頭にちょうど1個だけ置けること。
- [RFC 9639 Section 8.5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.5-1): SEEKTABLE は最大1個であること。
- [RFC 9639 Section 8.6, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.6-1): Vorbis Comment は最大1個であること。
- [RFC 9639 Section 9, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-9-1): 最後のメタデータブロックの直後から音声フレームが始まること。
- [RFC 9639 Section 11, paragraph 3](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3): malformed payload によるメモリ破壊や過剰なリソース消費を防ぐこと。
- [RFC 9639 Section 11, paragraph 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-6): 内部の長さや個数が、上位のメタデータブロック長を超えないようにすること。
- [RFC 9639 Appendix D.2.3--D.2.7](https://www.rfc-editor.org/rfc/rfc9639.html#appendix-D.2.3): 複数のメタデータブロックから最初のフレームまでを追える具体例。

## FLAC 先頭部分の構造

[RFC 9639 Section 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-6) を、このステップの処理単位に直すと次のようになる。

```text
fLaC marker
    |
    v
+----------------------+  必ず最初、必ず1個
| STREAMINFO header    |
| STREAMINFO payload   |
+----------------------+
    |
    | isLast == false の間だけ続く
    v
+----------------------+  0個以上
| other metadata header|
| skipped payload      |
+----------------------+
    |
    | isLast == true のブロックのペイロードを処理し終える
    v
first audio frame
```

終了条件は、ブロック型や長さではなく `isLast` である。長さ0のブロックもあり得るため、`length == 0` を終了条件にしてはいけない。例えば PADDING は長さ0でもよい（[RFC 9639 Section 8.3](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.3)、[Table 4](https://www.rfc-editor.org/rfc/rfc9639.html#table-4)）。

また、`isLast` は「このヘッダーが最後」という意味ではなく、「このヘッダーとそれに属するペイロードが最後のメタデータブロック」という意味である。したがって、`isLast == true` でも、先にそのブロックのペイロード全体を読み終える必要がある（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。

## メタデータブロックヘッダーの復習

各ヘッダーは4バイトである（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。

```text
byte 0
  7             6                    0
 +---------------+--------------------+
 | isLast: 1 bit | block type: 7 bits |
 +---------------+--------------------+

byte 1..3
 +------------------------------------+
 | payload length: unsigned 24-bit BE |
 +------------------------------------+
```

- `isLast`: `buf[0] & 0x80 != 0`
- `blockType`: `buf[0] & 0x7f`
- `length`: `uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])`

`length` はペイロードだけのバイト数であり、4バイトのヘッダー自身を含まない（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。したがって、ペイロードをスキップするときに4を足してはいけない。

24ビット値から導かれる最大ペイロード長は `0xFFFFFF`、つまり16,777,215バイトである。これはRFCのフィールド幅から導かれる値であり、別に記録された上限値ではない。

## ブロック型と今回の扱い

[RFC 9639 Table 2](https://www.rfc-editor.org/rfc/rfc9639.html#table-2) とMVPの方針を対応させると、次のようになる。

| 値 | RFC 上の型 | 今回の処理 |
|---:|---|---|
| `0` | STREAMINFO | 先頭で復号する。2個目はエラー |
| `1` | PADDING | `length` バイトをスキップ |
| `2` | APPLICATION | `length` バイトをスキップ |
| `3` | SEEKTABLE | `length` バイトをスキップ。2個目はエラー |
| `4` | Vorbis Comment | `length` バイトをスキップ。2個目はエラー |
| `5` | CUESHEET | `length` バイトをスキップ |
| `6` | PICTURE | `length` バイトをスキップ |
| `7..126` | Reserved | MVPでは `length` バイトをスキップ |
| `127` | Forbidden | エラー |

ここでは次の区別が重要になる。

- `127` が forbidden で、ビットストリームに現れてはならないことはRFCの直接の規定である（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)、[Table 2](https://www.rfc-editor.org/rfc/rfc9639.html#table-2)）。
- `7..126` は reserved であり、forbidden ではない。未知の方式でデコードを中止することもRFC上は許される（[RFC 9639 Section 5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-5-1)）。
- このプロジェクトでは、MVPの「他のメタデータは長さ分スキップする」という方針に従い、`7..126` もスキップする。これはRFCの MUST ではなく、前方互換性を選ぶ実装判断である。

SEEKTABLE と Vorbis Comment の内容は使わないが、型だけで判定できる個数制約は検証できる。SEEKTABLE は最大1個（[RFC 9639 Section 8.5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.5-1)）、Vorbis Comment も最大1個である（[RFC 9639 Section 8.6, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.6-1)）。この検証には、それぞれを既に見たかどうかを表す真偽値だけあればよい。

一方、スキップするペイロード内部の制約までは、このステップでは検証しない。例えばPADDINGがすべてゼロか、SEEKTABLE内のseek pointが整列済みかは調べない。これは「そのメタデータがRFC上正しい」と認定するという意味ではなく、ペイロードを解釈しないというMVPの実装範囲である。将来これらを復号するときは、ブロック長を上限として内部の長さと個数を検証する（[RFC 9639 Section 11, paragraph 6](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-6)）。

## 処理の責務

メタデータ領域全体を担当する非公開関数の候補は次の形である。

```go
func readMetadata(r io.Reader) (streamInfo, error)
```

これはGo実装上の設計案であり、RFCの規定ではない。この関数の事前条件と事後条件を明確にする。

- 事前条件: `fLaC` マーカーは既に読み終わっている。
- 正常終了時: STREAMINFO を返し、`r` は最初の音声フレームの先頭を指す。
- エラー終了時: 読み取り位置の巻き戻しは保証しない。

`NewDecoder` は次の順で初期化を行う。

1. `validateMarker` でマーカーを読む。
2. `readMetadata` でメタデータ領域全体を読む。
3. 得られた STREAMINFO と、音声フレーム先頭を指す `io.Reader` を `Decoder` に保持する。

公開APIでSTREAMINFOをどう見せるかは別の設計判断である。このステップでは、非公開値として `Decoder` に保持できればよい。

## 走査アルゴリズム

最初のブロックだけは特別に扱う。

```text
1. 最初のヘッダーを読む
2. blockType == STREAMINFO を検証する
3. length == 34 を検証する
4. STREAMINFO の34バイトを復号する
5. isLast == true なら正常終了する
6. それ以外は後続ブロックのループへ進む
```

先頭ブロックが STREAMINFO でなければならないのはRFCの MUST である（[RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)、[Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)）。STREAMINFO のペイロードが34バイトであることは、[RFC 9639 Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3) のフィールド幅の合計272ビットから導かれる。

後続ブロックは次のループで処理する。

```text
1. ヘッダーを読む
2. STREAMINFO が再度現れたらエラーにする
3. SEEKTABLE / Vorbis Comment の個数制約を検証する
4. ペイロードを length バイトだけスキップする
5. isLast == true なら正常終了する
6. isLast == false なら次のヘッダーへ戻る
```

`isLast` の判定をペイロード処理より前に置くと、最後のペイロードを読み残してしまう。判定してループを抜けるのは、必ずペイロードを復号またはスキップした後である。

同期コードらしい `0xFF 0xF8` または `0xFF 0xF9` を探してメタデータの終端を決めてはいけない。メタデータのバイト列中にも同じ並びは現れ得る。メタデータの境界は `isLast` と `length` から決め、フレーム同期コードの検証は次のステップで行う（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)、[Section 9.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1-1)）。

## ペイロードを安全にスキップする

入力由来の `length` をそのまま `make([]byte, length)` に渡す必要はない。内容を使わないため、一定サイズの作業領域で正確に `length` バイトを消費すればよい。標準ライブラリでは [`io.CopyN`](https://pkg.go.dev/io#CopyN) と `io.Discard` を組み合わせられる。

```go
n, err := io.CopyN(io.Discard, r, int64(header.length))
```

実装では `err` を必ず確認し、要求された `length` バイトを消費できなければ、切り詰められたメタデータとしてエラーを返す。エラーにはブロック型、宣言された長さ、実際に読めた長さを含めると原因を追いやすい。

この方法には次の性質がある。

- ペイロード長に比例する巨大なスライスを確保しない。
- `io.Reader` だけで動き、`io.Seeker` を要求しない。
- `length == 0` でも正常に0バイトを消費し、次のヘッダーへ進める。
- `uint32` の `length` は安全に `int64` へ変換できる。

malformed payload によってメモリを超過したり過剰なリソースを消費したりしてはならないという要件に対し、入力長と同じ大きさのバッファを確保しないことが重要である（[RFC 9639 Section 11, paragraph 3](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3)）。

## この段階で検証すること

| 条件 | 処理 | 根拠の種類 |
|---|---|---|
| 最初の型がSTREAMINFOでない | エラー | RFCの MUST（[Section 8](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)） |
| 最初のSTREAMINFOの長さが34でない | エラー | [Table 3](https://www.rfc-editor.org/rfc/rfc9639.html#table-3) からの導出 |
| 2個目のSTREAMINFO | エラー | RFCの MUST NOT（[Section 8.2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)） |
| 2個目のSEEKTABLE | エラー | RFCの MUST NOT（[Section 8.5](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.5-1)） |
| 2個目のVorbis Comment | エラー | RFCの MUST NOT（[Section 8.6](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.6-1)） |
| 型127 | エラー | RFCの forbidden（[Table 2](https://www.rfc-editor.org/rfc/rfc9639.html#table-2)） |
| 型7..126 | 長さ分スキップ | MVPの実装判断。RFC上はreserved（[Section 5](https://www.rfc-editor.org/rfc/rfc9639.html#section-5-1)） |
| 4バイト未満のヘッダー | エラー | 4バイト構造を満たさない（[Section 8.1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)） |
| 宣言長より短いペイロード | エラー | ブロック境界を満たさない。堅牢性要件（[Section 11](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3)） |
| `isLast == false` のままEOF | エラー | 次のメタデータブロックが必要（[Section 8.1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)） |
| 長さ0の後続ブロック | 受け入れて次へ進む | 0は終端値ではない。PADDINGでは明示的に許可（[Table 4](https://www.rfc-editor.org/rfc/rfc9639.html#table-4)） |

このステップでは、最後のメタデータ直後に実際にフレームが存在するかまでは検証しない。正常終了は「入力位置をフレーム開始予定位置まで進めた」という意味である。フレームがない、または同期コードが不正というエラーは、フレームヘッダーを読む次のステップで検出する。

## テストを先に書く

### RFC example 1: STREAMINFO だけの最短経路

`testdata/flac-specification/example_1.flac` では、STREAMINFO のヘッダーで `isLast == true` になっている（[RFC 9639 Appendix D.1.3 / Table 27](https://www.rfc-editor.org/rfc/rfc9639.html#table-27)）。ヘッダー位置 `0x04` + ヘッダー4バイト + ペイロード34バイトから、メタデータ読み取り後の次の2バイトはファイルオフセット `0x2A` の `FF F8` になる。この位置にフレーム同期コードがあることは [Appendix D.1.4 / Table 29](https://www.rfc-editor.org/rfc/rfc9639.html#table-29) に示されている。

このテストは、「最初のSTREAMINFOが最後なら余分なヘッダーを読まない」ことを保証する。

### RFC example 2: 複数ブロックの走査

`testdata/flac-specification/example_2.flac` は、このステップの主オラクルに向いている。RFCが示す並びは次のとおり。

| ヘッダー位置 | 型 | `isLast` | ペイロード長 | 次の位置 | RFC |
|---:|---|:---:|---:|---:|---|
| `0x04` | STREAMINFO (`0`) | false | 34 | `0x2A` | [Table 32](https://www.rfc-editor.org/rfc/rfc9639.html#table-32) |
| `0x2A` | SEEKTABLE (`3`) | false | 18 | `0x40` | [Table 33](https://www.rfc-editor.org/rfc/rfc9639.html#table-33) |
| `0x40` | Vorbis Comment (`4`) | false | 58 | `0x7E` | [Table 34](https://www.rfc-editor.org/rfc/rfc9639.html#table-34) |
| `0x7E` | PADDING (`1`) | true | 6 | `0x88` | [Table 35](https://www.rfc-editor.org/rfc/rfc9639.html#table-35) |

読み取り後の次の2バイトが `FF F8` であることを確認する。最初のフレームが `0x88` から始まることは [RFC 9639 Appendix D.2.7 / Table 36](https://www.rfc-editor.org/rfc/rfc9639.html#table-36) に示されている。

このテストは、各 `length` がヘッダーを含まないこと、最後のPADDINGも読み切ること、最初のフレームを先読みしないことを一度に保証する。

### 最小限の異常系

1. STREAMINFO より前に別の型がある入力をエラーにする（[RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)）。IETF testbench の `faulty/07 - other metadata blocks preceding streaminfo metadata block.flac` もオラクルに使える。
2. STREAMINFO がない入力をエラーにする（[RFC 9639 Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)）。IETF testbench の `faulty/06 - missing streaminfo metadata block.flac` も使える。
3. 後続に2個目のSTREAMINFOが現れたらエラーにする（[RFC 9639 Section 8.2, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)）。
4. 2個目のSEEKTABLEと2個目のVorbis Commentをそれぞれエラーにする（[RFC 9639 Section 8.5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.5-1)、[Section 8.6, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.6-1)）。
5. 後続ヘッダーが4バイト未満ならエラーにする（[RFC 9639 Section 8.1, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。
6. 後続ブロックの宣言長より実データが短ければエラーにする（[RFC 9639 Section 11, paragraph 3](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3)）。
7. 長さ0かつ `isLast == false` のブロックの後も、次のヘッダーを正しく読めることを確認する（[RFC 9639 Table 4](https://www.rfc-editor.org/rfc/rfc9639.html#table-4)）。
8. 予約型 `7..126` を宣言長だけスキップできることを確認する。これはMVPの実装方針を固定するテストである（reserved の意味は [RFC 9639 Section 5, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-5-1) を参照）。
9. `isLast == true` のペイロード直後のバイトを読み残し、フレーム側へ渡せることを確認する（[RFC 9639 Section 9, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-9-1)）。

テスト用の入力を自作する場合は、「有効な34バイトのSTREAMINFO」を返す小さなテストヘルパーを用意すると、ブロック列のテスト意図が読みやすくなる。STREAMINFO自体のフィールドテストは `STREAMINFO.md` の範囲なので、ここでは重複させない。

## 既存テストへの影響

`NewDecoder` がマーカーだけでなくメタデータ領域まで読むようになると、`"fLaC"` だけの入力は正常系ではなく「STREAMINFOヘッダーが欠けた入力」になる（[RFC 9639 Section 8, paragraph 1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)）。

そのため、責務を次のように分ける。

- `validateMarker` の単体テストでは、引き続き `"fLaC"` だけを正常入力としてよい。
- `NewDecoder` の正常系には、少なくとも有効なマーカー、STREAMINFOヘッダー、34バイトのSTREAMINFOペイロードが必要になる。
- 公開APIの統合テストには、RFC example 1またはexample 2を使うと、実際の初期化契約を確認できる。

これは `NewDecoder` を「初期化時にメタデータまで検証する」という既存のAPI方針から生じる変更であり、RFCがGo APIの挙動を指定しているわけではない。

## 実装を小さく進める順序

1. RFC example 1を使い、STREAMINFOだけで正常終了してフレーム先頭を読み残すテストを書く。
2. `readMetadata` に先頭STREAMINFOの検証と復号だけを接続し、テストを通す。
3. RFC example 2を使い、複数ブロックをスキップして `0x88` に到達するテストを書く。
4. 後続ブロックのループと、安全なスキップ処理を追加する。
5. 先頭型、重複型、切り詰め、予約型、長さ0のテストを追加する。
6. `NewDecoder` から `readMetadata` を呼び、STREAMINFOを保持する。
7. `go test ./...` で全テストを確認する。

## このステップの完了条件

- 最初のブロックがSTREAMINFOであることを検証する（[RFC 9639 Section 8](https://www.rfc-editor.org/rfc/rfc9639.html#section-8-1)）。
- STREAMINFOを1回だけ復号し、重複を拒否する（[RFC 9639 Section 8.2](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.2-1)）。
- 後続メタデータを `isLast` が立つまで順に処理する（[RFC 9639 Section 8.1](https://www.rfc-editor.org/rfc/rfc9639.html#section-8.1-1)）。
- 未使用のペイロードを、長さに比例するバッファを確保せず正確にスキップする（[RFC 9639 Section 11](https://www.rfc-editor.org/rfc/rfc9639.html#section-11-3)）。
- 切り詰められたヘッダーとペイロードでエラーを返す。
- 正常終了後、`io.Reader` が最初の音声フレームの先頭を指す（[RFC 9639 Section 9](https://www.rfc-editor.org/rfc/rfc9639.html#section-9-1)）。
- RFC example 1とexample 2を使ったテストが通る。
- `go test ./...` が通る。

ここまで終われば、次はMSB-firstのビットリーダを用意し、フレームヘッダーを復号するステップへ進める（[RFC 9639 Section 9.1](https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1)）。
