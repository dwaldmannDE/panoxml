package main

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNatLess(t *testing.T) {
	got := []string{"p-10.dng", "p-2.dng", "p-1.dng", "p-100.dng", "q-1.dng"}
	slices.SortFunc(got, func(a, b string) int {
		switch {
		case natLess(a, b):
			return -1
		case natLess(b, a):
			return 1
		}
		return 0
	})
	want := []string{"p-1.dng", "p-2.dng", "p-10.dng", "p-100.dng", "q-1.dng"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUnwrapYawAcrossNorth(t *testing.T) {
	shots := []Shot{{Yaw: 170}, {Yaw: 178}, {Yaw: -174}, {Yaw: -166}}
	unwrapYaw(shots)
	want := []float64{170, 178, 186, 194}
	for i, s := range shots {
		if s.Yaw != want[i] {
			t.Errorf("shot %d yaw = %v, want %v", i, s.Yaw, want[i])
		}
	}
}

func TestBracketSets(t *testing.T) {
	shots := []Shot{
		{Yaw: 0, Pitch: 0}, {Yaw: 0, Pitch: 0}, {Yaw: 0.1, Pitch: 0},
		{Yaw: 8, Pitch: 0}, {Yaw: 8, Pitch: 0}, {Yaw: 8, Pitch: 0},
	}
	sets := bracketSets(shots)
	if len(sets) != 2 {
		t.Fatalf("got %d sets, want 2", len(sets))
	}
	if n, uniform := setSize(sets); n != 3 || !uniform {
		t.Errorf("set size = %d, uniform = %v, want 3, true", n, uniform)
	}
}

// A RAW+JPEG pair is one shot, so only the DNG is read.
func TestImageFilesPrefersRaw(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.DNG", "a.JPG", "b.jpg", "c.txt", ".hidden.dng"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := imageFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.DNG"), filepath.Join(dir, "b.jpg")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The generated file has to parse as the XML PTGui and hugin expect: ascending
// ids, a bracket index counting up within each set, and yaw/pitch/roll on every
// position.
func TestWriteDoc(t *testing.T) {
	doc := Doc{
		Title: "Import", Comment: "test", Focal: 40, Crop: 4.2,
		Entries: []Entry{
			{ID: 1, Bracket: 1, Yaw: -2.4, Pitch: 0, Roll: 0, File: "a--1.dng"},
			{ID: 2, Bracket: 2, Yaw: -2.4, Pitch: 0, Roll: 0, File: "a-2.dng"},
			{ID: 3, Bracket: 1, Yaw: 5.3, Pitch: -6, Roll: 0, File: "a-3.dng"},
		},
	}
	var buf bytes.Buffer
	if err := writeDoc(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var got struct {
		XMLName xml.Name `xml:"papywizard"`
		Focal   float64  `xml:"header>lens>focal"`
		Sensor  struct {
			Coef float64 `xml:"coef,attr"`
		} `xml:"header>camera>sensor"`
		Picts []struct {
			ID       int `xml:"id,attr"`
			Bracket  int `xml:"bracket,attr"`
			Position struct {
				Yaw   float64 `xml:"yaw,attr"`
				Pitch float64 `xml:"pitch,attr"`
				Roll  float64 `xml:"roll,attr"`
			} `xml:"position"`
		} `xml:"shoot>pict"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, buf.String())
	}
	if got.Focal != 40 || got.Sensor.Coef != 4.2 {
		t.Errorf("focal = %v, coef = %v, want 40, 4.2", got.Focal, got.Sensor.Coef)
	}
	if len(got.Picts) != 3 {
		t.Fatalf("got %d pict elements, want 3", len(got.Picts))
	}
	for i, p := range got.Picts {
		if p.ID != doc.Entries[i].ID || p.Bracket != doc.Entries[i].Bracket {
			t.Errorf("pict %d = id %d bracket %d, want id %d bracket %d",
				i, p.ID, p.Bracket, doc.Entries[i].ID, doc.Entries[i].Bracket)
		}
		if p.Position.Yaw != doc.Entries[i].Yaw || p.Position.Pitch != doc.Entries[i].Pitch {
			t.Errorf("pict %d position = %v/%v, want %v/%v",
				i, p.Position.Yaw, p.Position.Pitch, doc.Entries[i].Yaw, doc.Entries[i].Pitch)
		}
	}
}

// End to end: a folder of bracketed frames yields one pict per file and one
// bracket set per gimbal position.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	names := []string{"s-1.dng", "s-2.dng", "s-3.dng", "s-10.dng", "s-11.dng", "s-12.dng"}
	for i, n := range names {
		xmp := sampleXMP
		if i >= 3 {
			xmp = replaceYaw(sampleXMP, "+5.30")
		}
		if err := os.WriteFile(filepath.Join(dir, n), buildTIFF(xmp), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "out.xml")
	if err := run(dir, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(b, []byte("<pict ")); n != 6 {
		t.Errorf("got %d pict elements, want 6", n)
	}
	if n := bytes.Count(b, []byte(`bracket="1"`)); n != 2 {
		t.Errorf("got %d bracket sets, want 2", n)
	}
	if !bytes.Contains(b, []byte(`yaw="5.30"`)) {
		t.Errorf("second position missing:\n%s", b)
	}
}

func replaceYaw(xmp, yaw string) string {
	return string(bytes.Replace([]byte(xmp),
		[]byte(`drone-dji:GimbalYawDegree="-2.40"`),
		[]byte(`drone-dji:GimbalYawDegree="`+yaw+`"`), 1))
}
