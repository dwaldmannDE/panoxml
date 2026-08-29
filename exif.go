package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	xmpAPP1ID = "http://ns.adobe.com/xap/1.0/\x00"
	exifAPP1  = "Exif\x00\x00"
)

// TIFF/Exif tags read from the files.
const (
	tagModel     = 0x0110
	tagXMP       = 0x02bc
	tagExifIFD   = 0x8769
	tagDateTaken = 0x9003
	tagFocal     = 0x920a
	tagFocal35   = 0xa405
)

// Shot is the metadata of one source image needed to place it in a panorama.
type Shot struct {
	Path    string
	Name    string
	Yaw     float64 // gimbal yaw in degrees, world frame, positive clockwise
	Pitch   float64 // gimbal pitch in degrees, positive up
	Roll    float64
	Focal   float64 // mm
	Focal35 float64 // mm, 35 mm equivalent
	Model   string  // Exif Model, e.g. FC9287
	Product string  // drone-dji:ProductName, e.g. Mavic4 Pro L3B
	Taken   string  // Exif DateTimeOriginal

	hasAngles bool
}

// Camera names the shot by its drone model, falling back to the Exif model.
func (s Shot) Camera() string {
	if s.Product != "" {
		return s.Product
	}
	return s.Model
}

// ReadShot extracts gimbal angles and lens data from a DNG/TIFF or JPEG file.
func ReadShot(path string) (Shot, error) {
	s := Shot{Path: path, Name: filepath.Base(path)}
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return s, err
	}
	var magic [2]byte
	if _, err := f.ReadAt(magic[:], 0); err != nil {
		return s, err
	}
	switch {
	case magic[0] == 0xff && magic[1] == 0xd8:
		err = readJPEG(f, fi.Size(), &s)
	case string(magic[:]) == "II" || string(magic[:]) == "MM":
		err = readTIFF(f, &s)
	default:
		err = fmt.Errorf("not a JPEG or TIFF/DNG file")
	}
	if err != nil {
		return s, err
	}
	if !s.hasAngles {
		return s, fmt.Errorf("no drone-dji gimbal angles in XMP")
	}
	return s, nil
}

type ifdEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	val   [4]byte
}

type tiffReader struct {
	r  io.ReaderAt
	bo binary.ByteOrder
}

func readTIFF(r io.ReaderAt, s *Shot) error {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return err
	}
	t := &tiffReader{r: r}
	switch string(hdr[:2]) {
	case "II":
		t.bo = binary.LittleEndian
	case "MM":
		t.bo = binary.BigEndian
	default:
		return fmt.Errorf("not a TIFF")
	}
	if t.bo.Uint16(hdr[2:4]) != 42 {
		return fmt.Errorf("unsupported TIFF version")
	}
	ifd0, err := t.readIFD(int64(t.bo.Uint32(hdr[4:8])))
	if err != nil {
		return err
	}
	for _, e := range ifd0 {
		switch e.tag {
		case tagModel:
			s.Model = t.ascii(e)
		case tagXMP:
			if b, err := t.value(e); err == nil {
				parseXMP(b, s)
			}
		case tagExifIFD:
			exif, err := t.readIFD(int64(t.uint(e)))
			if err != nil {
				continue
			}
			for _, e := range exif {
				switch e.tag {
				case tagFocal:
					s.Focal = t.rational(e)
				case tagFocal35:
					s.Focal35 = float64(t.uint(e))
				case tagDateTaken:
					s.Taken = t.ascii(e)
				}
			}
		}
	}
	return nil
}

func readJPEG(r io.ReaderAt, size int64, s *Shot) error {
	br := bufio.NewReader(io.NewSectionReader(r, 0, size))
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil {
		return err
	}
	if soi[0] != 0xff || soi[1] != 0xd8 {
		return fmt.Errorf("not a JPEG")
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil
		}
		if b != 0xff {
			continue
		}
		for b == 0xff {
			if b, err = br.ReadByte(); err != nil {
				return nil
			}
		}
		switch {
		case b == 0xd9 || b == 0xda: // end of image, start of scan
			return nil
		case b == 0x00 || b == 0x01 || (b >= 0xd0 && b <= 0xd7): // standalone markers
			continue
		}
		var lb [2]byte
		if _, err := io.ReadFull(br, lb[:]); err != nil {
			return nil
		}
		n := int(binary.BigEndian.Uint16(lb[:])) - 2
		if n < 0 {
			return nil
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			return nil
		}
		if b != 0xe1 { // APP1 carries both Exif and XMP
			continue
		}
		switch {
		case bytes.HasPrefix(payload, []byte(exifAPP1)):
			readTIFF(bytes.NewReader(payload[len(exifAPP1):]), s)
		case bytes.HasPrefix(payload, []byte(xmpAPP1ID)):
			parseXMP(payload[len(xmpAPP1ID):], s)
		}
	}
}

func (t *tiffReader) readIFD(off int64) ([]ifdEntry, error) {
	var cnt [2]byte
	if _, err := t.r.ReadAt(cnt[:], off); err != nil {
		return nil, err
	}
	n := int(t.bo.Uint16(cnt[:]))
	if n > 4096 {
		return nil, fmt.Errorf("implausible IFD entry count %d", n)
	}
	buf := make([]byte, n*12)
	if _, err := t.r.ReadAt(buf, off+2); err != nil {
		return nil, err
	}
	out := make([]ifdEntry, n)
	for i := range out {
		b := buf[i*12:]
		out[i] = ifdEntry{tag: t.bo.Uint16(b), typ: t.bo.Uint16(b[2:]), count: t.bo.Uint32(b[4:])}
		copy(out[i].val[:], b[8:12])
	}
	return out, nil
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

func (t *tiffReader) value(e ifdEntry) ([]byte, error) {
	sz := typeSize[e.typ] * int(e.count)
	if sz <= 0 {
		return nil, fmt.Errorf("tag 0x%04x: unsupported type %d", e.tag, e.typ)
	}
	if sz > 1<<24 {
		return nil, fmt.Errorf("tag 0x%04x: value of %d bytes too large", e.tag, sz)
	}
	if sz <= 4 {
		return e.val[:sz], nil
	}
	b := make([]byte, sz)
	if _, err := t.r.ReadAt(b, int64(t.bo.Uint32(e.val[:]))); err != nil {
		return nil, err
	}
	return b, nil
}

func (t *tiffReader) ascii(e ifdEntry) string {
	b, err := t.value(e)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\x00 ")
}

func (t *tiffReader) uint(e ifdEntry) uint32 {
	switch e.typ {
	case 3:
		return uint32(t.bo.Uint16(e.val[:]))
	case 4:
		return t.bo.Uint32(e.val[:])
	}
	return 0
}

func (t *tiffReader) rational(e ifdEntry) float64 {
	b, err := t.value(e)
	if err != nil || len(b) < 8 {
		return 0
	}
	den := t.bo.Uint32(b[4:8])
	if den == 0 {
		return 0
	}
	return float64(t.bo.Uint32(b[:4])) / float64(den)
}

// parseXMP pulls the drone-dji properties out of an XMP packet. DJI writes them
// as RDF attributes; element form is accepted too.
func parseXMP(b []byte, s *Shot) {
	d := xml.NewDecoder(bytes.NewReader(trimPacket(b)))
	d.Strict = false
	vals := map[string]string{}
	var open string
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			open = ""
			if isDJI(t.Name.Space) {
				open = t.Name.Local
			}
			for _, a := range t.Attr {
				if isDJI(a.Name.Space) {
					vals[a.Name.Local] = a.Value
				}
			}
		case xml.CharData:
			if open != "" {
				vals[open] = strings.TrimSpace(string(t))
			}
		case xml.EndElement:
			open = ""
		}
	}
	yaw, okY := num(vals["GimbalYawDegree"])
	pitch, okP := num(vals["GimbalPitchDegree"])
	if okY && okP {
		s.Yaw, s.Pitch, s.hasAngles = yaw, pitch, true
	}
	if v, ok := num(vals["GimbalRollDegree"]); ok {
		s.Roll = v
	}
	if p := strings.TrimSpace(vals["ProductName"]); p != "" {
		s.Product = p
	}
}

// isDJI matches the drone-dji namespace. DJI has shipped it under more than one
// URI: http://www.uav.com/drone-dji/1.0/ on the Mavic 4 Pro, http://www.dji.com/
// drone-dji/1.0/ on the Air 3.
func isDJI(space string) bool { return strings.Contains(space, "drone-dji") }

// trimPacket cuts xpacket padding and trailing NULs off an XMP block.
func trimPacket(b []byte) []byte {
	i := bytes.IndexByte(b, '<')
	j := bytes.LastIndexByte(b, '>')
	if i < 0 || j < i {
		return nil
	}
	return b[i : j+1]
}

// num parses a DJI XMP number, which may be signed ("+0.00") or rational ("2997/100").
func num(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f, true
	}
	if i := strings.IndexByte(v, '/'); i > 0 {
		n, e1 := strconv.ParseFloat(v[:i], 64)
		den, e2 := strconv.ParseFloat(v[i+1:], 64)
		if e1 == nil && e2 == nil && den != 0 {
			return n / den, true
		}
	}
	return 0, false
}
