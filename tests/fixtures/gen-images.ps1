Add-Type -AssemblyName System.Drawing
$bmp = New-Object System.Drawing.Bitmap 128, 128
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.Clear([System.Drawing.Color]::FromArgb(40, 120, 220))
$brush = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::White)
$g.FillEllipse($brush, 20, 20, 88, 88)
$dir = Join-Path $PSScriptRoot "."
$pngPath = Join-Path $dir "sample.png"
$jpgPath = Join-Path $dir "sample.jpg"
$bmp.Save($pngPath, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Save($jpgPath, [System.Drawing.Imaging.ImageFormat]::Jpeg)
$g.Dispose()
$bmp.Dispose()
$brush.Dispose()
Write-Output "saved $pngPath"
Write-Output "saved $jpgPath"
Get-Item $pngPath, $jpgPath | ForEach-Object { "$($_.Name) $($_.Length)" }
