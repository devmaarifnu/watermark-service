@echo off
REM Build kedua tool untuk Windows.
REM GOOS di go env mesin ini diset "linux", jadi di-override di sini agar
REM menghasilkan binary Windows (PE) yang bisa dijalankan langsung.
setlocal
set GOOS=windows
set GOARCH=amd64

if not exist bin mkdir bin

go build -o bin\pdfsplit.exe  .\cmd\pdfsplit
if errorlevel 1 goto gagal

go build -o bin\watermark.exe .\cmd\watermark
if errorlevel 1 goto gagal

echo Build selesai:
echo   bin\pdfsplit.exe   - pecah PDF (split, bookmark, extract)
echo   bin\watermark.exe  - tempel identitas satpen di setiap halaman
endlocal
exit /b 0

:gagal
echo Build GAGAL.
endlocal
exit /b 1
