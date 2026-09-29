# Tool PDF untuk Satuan Pendidikan

Dua tool CLI untuk mengolah SK Menkumham + Akta Notaris PBNU yang bersifat general
per satuan pendidikan (satpen).

## Latar belakang

SK bersifat umum, sehingga satu satpen yang mendapatkannya bisa membagikannya ke
satpen lain tanpa melalui mekanisme yang seharusnya. Karena itu tiap satpen perlu
mendapat salinan yang **membawa identitasnya sendiri**.

Proteksi yang dipakai bertumpu pada satu prinsip: identitas ditempel di **setiap
halaman**, bukan hanya halaman cover. Alasannya, proteksi yang hanya ada di cover
bisa dijebol dengan cara sederhana:

1. Ambil halaman 2–39 (isi SK asli, tanpa identitas)
2. Buat cover baru dengan nama satpen lain
3. Gabungkan

Karena itu watermark di sini digambar pada seluruh halaman. Halaman hasil split
tetap membawa identitas satpen asalnya.

## Build

```
build.bat
```

Menghasilkan:

- `bin\pdfsplit.exe` — pecah PDF
- `bin\watermark.exe` — tempel identitas satpen

Catatan: `GOOS` di `go env` mesin ini diset `linux`, sehingga `build.bat`
menimpanya menjadi `windows`. Tangani dengan `go env -w GOOS=windows` bila ingin
build biasa juga menghasilkan binary Windows.

## watermark — tempel identitas satpen

```
watermark -in SK.pdf -sekolah "SMA Negeri 1 Kediri" \
          -reg "BHPNU/2026/001234" -npsn 20512345
```

Menghasilkan `SK_20512345_SMA_Negeri_1_Kediri.pdf` dengan identitas satpen di
seluruh 39 halaman berisi nama sekolah, nomor registrasi, dan NPSN.

### Mode batch dari CSV

```
watermark -in SK.pdf -csv satpen.csv -outdir hasil
```

Format CSV (header dideteksi otomatis, urutan kolom mengikuti nama header):

```csv
sekolah,registrasi,npsn
"SMA Negeri 1 Kediri","BHPNU/2026/001234",20512345
"MTs NU Al-Hidayah Surabaya","BHPNU/2026/005678",20567890
```

Tanpa header, urutan yang diasumsikan adalah `sekolah,registrasi,npsn`.

### Opsi

| Opsi | Default | Keterangan |
|---|---|---|
| `-in` | — | PDF sumber (wajib) |
| `-sekolah` `-reg` `-npsn` | — | Identitas satu satpen |
| `-csv` | — | Daftar satpen (mode batch) |
| `-out` | `<sumber>_<npsn>.pdf` | Nama file keluaran |
| `-outdir` | `<sumber>_watermark` | Folder keluaran mode batch |
| `-fontsize` | `8` | Ukuran teks (point, absolut) |
| `-pos` | `bl` | Posisi: `tl tc tr l c r bl bc br` |
| `-rot` | `0` | Rotasi teks (derajat); `0` = horizontal |
| `-offset` | `18 18` | Jarak dari tepi halaman (point) |
| `-opacity` | `0.18` | Transparansi watermark |
| `-color` | `0.85 0 0` | Warna teks dan garis kotak |
| `-border` | `1` | Tebal garis kotak (point) |
| `-radius` | `6` | Radius sudut kotak (point); `0` = siku |
| `-cover` | `0` | Jumlah halaman awal yang dilewati |
| `-dryrun` | — | Tampilkan rencana saja |
| `-v` | — | Log pdfcpu |

### Tampilan bawaan

Teks **horizontal** di **kiri bawah** halaman, dibungkus **kotak bersudut
tumpul**, ukuran 8 pt, warna merah (`0.85 0 0`), opasitas 0.18:

```
╭─────────────────────────────────╮
│ SMA Negeri 1 Kediri             │
│ No. Registrasi: BHPNU/2026/001234│
│ NPSN: 20512345                  │
╰─────────────────────────────────╯
```

Ukuran 8 pt adalah ukuran absolut — tidak ikut membesar pada halaman berukuran
besar. Nama sekolah yang terlalu panjang dikecilkan otomatis agar tidak
melewati tepi halaman. Radius sudut otomatis dibatasi bila kotaknya kecil.

Ganti posisi dengan `-pos br` (kanan bawah), `-pos tl` (kiri atas), dan
sebagainya. `-radius 0` mengembalikan kotak bersiku.

Tanpa `-sekolah`/`-csv`, tool memakai 2 satpen contoh agar alur kerja bisa
langsung dilihat.

## pdfsplit — pecah PDF

```
pdfsplit split laporan.pdf              # satu file per halaman
pdfsplit split -n 20 laporan.pdf        # per 20 halaman
pdfsplit bookmark laporan.pdf           # ikut daftar isi
pdfsplit extract -p 1-3,7 laporan.pdf   # ambil halaman tertentu
```

## Catatan penting

**Watermark digambar di atas gambar halaman (mode stamp).** SK seperti ini
umumnya berupa hasil scan — satu gambar penuh per halaman. Watermark yang
digambar di belakang gambar tersebut tidak akan terlihat sama sekali, sehingga
proteksinya hilang tanpa disadari. Ada test khusus yang menjaga properti ini.

**Teks di pojok margin lebih mudah dipotong.** Posisi kiri bawah dipilih supaya
dokumen tetap rapi dan mudah dibaca. Konsekuensinya, identitas berada di area
margin sehingga secara teknis bisa di-crop tanpa merusak isi SK. Versi diagonal
yang menimpa badan teks lebih sulit dihilangkan, tetapi mengganggu keterbacaan.
Bila prioritasnya ketahanan terhadap pemotongan, pakai `-fontsize` lebih besar
dengan `-opacity` rendah agar teks menimpa area konten.

**Sudut tumpul dibuat dengan menambal form watermark.** Pdfcpu hanya
menyediakan *line join* `round`, yang radius pembulatannya terbatas pada
setengah lebar garis — pada `-border 1` hasilnya praktis masih siku (radius
0,5 pt). Karena itu kotaknya dilukis ulang sebagai path dengan kurva Bézier
sesudah watermark dibuat. Alur ini memakai context pdfcpu langsung
(`ReadValidateAndOptimize` → `AddWatermarks` → `WriteContext`), bukan
`api.AddWatermarksFile`, supaya seluruh objek asli dokumen tetap utuh. Hasilnya
sudah diverifikasi lolos `api.ValidateFile`.

**`-cover` membuat celah.** Halaman yang dilewati tidak beridentitas dan bisa
diambil. Pakai hanya bila halaman pertama memang sampul dan tidak perlu ditandai.

**Proteksi ini bersifat menandai, bukan mengunci.** File yang sudah dibagikan
tetap bisa disalin utuh. Yang dicapai di sini adalah: hasil pembagian dapat
dibuktikan bukan milik pemegangnya. Agar punya gigi, perlu disertai ketentuan
bahwa dokumen dengan identitas tidak sesuai dianggap tidak sah.

Lapisan lanjutan yang belum dikerjakan: nomor dokumen unik + QR ke halaman
verifikasi, dan registry supaya pihak ketiga bisa memeriksa keaslian.

## Struktur

```
cmd/pdfsplit/     tool pecah PDF
cmd/watermark/    tool watermark identitas satpen
internal/common/  helper bersama (konfigurasi pdfcpu, validasi input)
```

## Test

```
set PDFSPLIT_FIXTURE=C:\path\ke\SK.pdf
go test ./...
```

Test yang berkaitan dengan dokumen PDF dilewati (skip) bila `PDFSPLIT_FIXTURE`
tidak diset.
