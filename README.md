# panoxml

Reads the gimbal angles from the DJI photos in a folder and writes a Papywizard
XML file. PTGui imports it with **File > Import > Papywizard** to place every
image at the yaw and pitch it was shot at, instead of aligning to a grid.

## Install

```
make install
```

## Use

```
panoxml ~/Pictures/Import
```

Writes `papywizard.xml` into that folder and prints a summary of what it found
to stderr. `panoxml -h` lists flags.

## Details

Reads DNG, TIFF and JPEG. Angles come from the `drone-dji` XMP block
(`GimbalYawDegree`, `GimbalPitchDegree`, `GimbalRollDegree`), focal length from
Exif. No external tools.

Images are ordered by file name with digit runs compared numerically, matching
the order PTGui loads them in. A folder holding RAW+JPEG pairs of the same shot
contributes the RAW only.

Files whose pixel size differs from the rest of the folder are skipped and
named on stderr. A stitched panorama written back beside its source frames
carries the first frame's gimbal angles, so leaving it in would shift every
image onto the wrong position.

Bracketing needs no flag. Consecutive frames sharing a gimbal position form one
bracket set and get `bracket="1"`, `"2"`, `"3"` inside a `<pict>` element each.
PTGui counts images when the project is unbracketed and bracket sets when it is
not, so the same file fits both.

Yaw crossing +/-180 degrees is unwrapped into a continuous sweep. Gimbal angles
map onto PTGui unchanged: yaw grows clockwise, pitch is positive up.

Every entry carries its source file name as an XML comment.

## Test

```
make test
```
