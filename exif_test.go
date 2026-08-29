package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
   xmlns:drone-dji="http://www.uav.com/drone-dji/1.0/"
   drone-dji:GimbalRollDegree="+0.00"
   drone-dji:GimbalYawDegree="-2.40"
   drone-dji:GimbalPitchDegree="-11.90"
   drone-dji:ProductName="Mavic4 Pro L3B"/>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>` + "\x00\x00\x00"

type tiffWriter struct{ b []byte }

func (w *tiffWriter) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *tiffWriter) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *tiffWriter) entry(tag, typ uint16, count, val uint32) {
	w.u16(tag)
	w.u16(typ)
	w.u32(count)
	w.u32(val)
}

// buildTIFF lays out a minimal DNG-shaped file: IFD0 with Model, optional XMP
// and a pointer to an Exif IFD holding the focal lengths.
func buildTIFF(xmp string) []byte {
	const model = "FC9287\x00"
	entries := uint32(2)
	if xmp != "" {
		entries = 3
	}
	modelOff := 8 + 2 + entries*12 + 4
	xmpOff := modelOff + uint32(len(model))
	exifOff := xmpOff + uint32(len(xmp))
	ratOff := exifOff + 2 + 2*12 + 4

	w := &tiffWriter{}
	w.b = append(w.b, 'I', 'I')
	w.u16(42)
	w.u32(8)
	w.u16(uint16(entries))
	w.entry(tagModel, 2, uint32(len(model)), modelOff)
	if xmp != "" {
		w.entry(tagXMP, 1, uint32(len(xmp)), xmpOff)
	}
	w.entry(tagExifIFD, 4, 1, exifOff)
	w.u32(0)
	w.b = append(w.b, model...)
	w.b = append(w.b, xmp...)
	w.u16(2)
	w.entry(tagFocal, 5, 1, ratOff)
	w.entry(tagFocal35, 3, 1, 168)
	w.u32(0)
	w.u32(400)
	w.u32(10)
	return w.b
}

func buildJPEG(tiff []byte, xmp string) []byte {
	b := []byte{0xff, 0xd8}
	app1 := func(payload []byte) {
		b = append(b, 0xff, 0xe1)
		b = binary.BigEndian.AppendUint16(b, uint16(len(payload)+2))
		b = append(b, payload...)
	}
	app1(append([]byte(exifAPP1), tiff...))
	app1(append([]byte(xmpAPP1ID), xmp...))
	return append(b, 0xff, 0xd9)
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkShot(t *testing.T, s Shot) {
	t.Helper()
	if s.Yaw != -2.4 || s.Pitch != -11.9 || s.Roll != 0 {
		t.Errorf("angles = %v/%v/%v, want -2.4/-11.9/0", s.Yaw, s.Pitch, s.Roll)
	}
	if s.Focal != 40 || s.Focal35 != 168 {
		t.Errorf("focal = %v/%v, want 40/168", s.Focal, s.Focal35)
	}
	if s.Model != "FC9287" || s.Product != "Mavic4 Pro L3B" {
		t.Errorf("model = %q/%q", s.Model, s.Product)
	}
	if s.Camera() != "Mavic4 Pro L3B" {
		t.Errorf("Camera() = %q", s.Camera())
	}
}

func TestReadShotDNG(t *testing.T) {
	s, err := ReadShot(write(t, "a.dng", buildTIFF(sampleXMP)))
	if err != nil {
		t.Fatal(err)
	}
	checkShot(t, s)
}

// The JPEG carries its XMP in its own APP1 segment, not in the Exif block.
func TestReadShotJPEG(t *testing.T) {
	s, err := ReadShot(write(t, "a.jpg", buildJPEG(buildTIFF(""), sampleXMP)))
	if err != nil {
		t.Fatal(err)
	}
	checkShot(t, s)
}

func TestReadShotWithoutAngles(t *testing.T) {
	if _, err := ReadShot(write(t, "a.dng", buildTIFF(""))); err == nil {
		t.Fatal("want an error for a file without gimbal angles")
	}
}

func TestNum(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"+0.00", 0, true},
		{"-2.40", -2.4, true},
		{"2997/100", 29.97, true},
		{"", 0, false},
		{"Normal", 0, false},
	} {
		got, ok := num(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("num(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// DJI ships the drone-dji properties under more than one namespace URI.
func TestReadShotDJINamespaceVariants(t *testing.T) {
	for _, ns := range []string{
		"http://www.uav.com/drone-dji/1.0/",
		"http://www.dji.com/drone-dji/1.0/",
	} {
		xmp := strings.Replace(sampleXMP, "http://www.uav.com/drone-dji/1.0/", ns, 1)
		s, err := ReadShot(write(t, "a.dng", buildTIFF(xmp)))
		if err != nil {
			t.Fatalf("%s: %v", ns, err)
		}
		if s.Yaw != -2.4 {
			t.Errorf("%s: yaw = %v, want -2.4", ns, s.Yaw)
		}
	}
}
