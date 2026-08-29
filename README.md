# panoxml

Reads the gimbal angles from the DJI photos in a folder and writes a Papywizard
XML file. PTGui imports it with **File > Import > Papywizard** to place every
image at the yaw and pitch it was shot at, instead of aligning to a grid.

## Install

```
go install github.com/itdwgmbh/panoxml@latest
```

## Use

```
panoxml ~/Pictures/Import
```

Writes `papywizard.xml` into that folder and prints what it found:

```
225 images, Mavic4 Pro L3B, 40 mm (168 mm equivalent), 75 positions, 3 bracketed exposures each
15 columns x 5 rows, yaw -2.4 to 109.3 step 8.0, pitch -18.0 to 6.0 step 6.0
wrote /Users/xw77d/Pictures/Import/papywizard.xml
```

`-o <file>` writes somewhere else.

## Details

Reads DNG, TIFF and JPEG. Angles come from the `drone-dji` XMP block
(`GimbalYawDegree`, `GimbalPitchDegree`, `GimbalRollDegree`), focal length from
Exif. No external tools.

Images are ordered by file name with digit runs compared numerically, matching
the order PTGui loads them in. A folder holding RAW+JPEG pairs of the same shot
contributes the RAW only.

Bracketing needs no flag. Consecutive frames sharing a gimbal position form one
bracket set and get `bracket="1"`, `"2"`, `"3"` inside a `<pict>` element each.
PTGui counts images when the project is unbracketed and bracket sets when it is
not, so the same file fits both.

Yaw crossing +/-180 degrees is unwrapped into a continuous sweep. Gimbal angles
map onto PTGui unchanged: yaw grows clockwise, pitch is positive up.

Every entry carries its source file name as an XML comment.

## Test

```
go test ./...
```
