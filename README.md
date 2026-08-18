# flac

A pure Go FLAC ([RFC 9639](https://rfc-editor.org/rfc/rfc9639)) decoder with zero dependencies.

## Test results

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

## テスト結果

### 結果

| グループ   | ファイル数 | 結果     |
| ---------- | ---------: | -------- |
| `subset`   |         64 | **合格** |
| `uncommon` |         11 | 未実施   |
| `faulty`   |         11 | **合格** |

### 確認観点

**subset**

- 全てのストリームをエラーなくデコードできること
- 全てのフレームヘッダのCRC-8が一致すること(RFC 9639 §9.1.8)
- 全てのフレームフッタのCRC-16が一致すること(§9.3)
- デコード後のPCMのMD5チェックサムがSTREAMINFOに格納されているものと一致すること(§8.2)

**faulty**

- デコーダーがクラッシュまたはハングしないこと

## TODO

- エラー処理の一貫性
- パフォーマンス最適化
- テスト拡充
- uncommonのテスト
- ログ機能
