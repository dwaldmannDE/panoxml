package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Entry is one <pict> element: a source image and where the gimbal pointed.
// Bracket counts up from 1 within a set of bracketed exposures; PTGui uses it
// to match the file's sets against the bracket sets in the project.
type Entry struct {
	ID      int
	Bracket int
	Yaw     float64
	Pitch   float64
	Roll    float64
	File    string
}

// Doc is a Papywizard data file.
type Doc struct {
	Title   string
	Comment string
	Focal   float64 // mm
	Crop    float64 // sensor crop factor
	Entries []Entry
}

func writeDoc(w io.Writer, d Doc) error {
	b := bufio.NewWriter(w)
	fmt.Fprintln(b, `<?xml version="1.0" encoding="utf-8"?>`)
	fmt.Fprintln(b, `<papywizard version="c">`)
	fmt.Fprintln(b, ` <header>`)
	fmt.Fprintln(b, `  <general>`)
	fmt.Fprintf(b, "   <title>%s</title>\n", escape(d.Title))
	fmt.Fprintf(b, "   <comment>%s</comment>\n", escape(d.Comment))
	fmt.Fprintln(b, `  </general>`)
	if d.Crop > 0 {
		fmt.Fprintln(b, `  <camera>`)
		fmt.Fprintf(b, "   <sensor coef=\"%s\"/>\n", number(d.Crop))
		fmt.Fprintln(b, `  </camera>`)
	}
	if d.Focal > 0 {
		fmt.Fprintln(b, `  <lens type="rectilinear">`)
		fmt.Fprintf(b, "   <focal>%s</focal>\n", number(d.Focal))
		fmt.Fprintln(b, `  </lens>`)
	}
	fmt.Fprintln(b, ` </header>`)
	fmt.Fprintln(b, ` <shoot>`)
	for _, e := range d.Entries {
		if e.File != "" {
			fmt.Fprintf(b, "  <!-- %s -->\n", comment(e.File))
		}
		fmt.Fprintf(b, "  <pict id=\"%d\" bracket=\"%d\">\n", e.ID, e.Bracket)
		fmt.Fprintf(b, "   <position yaw=\"%.2f\" pitch=\"%.2f\" roll=\"%.2f\"/>\n", e.Yaw, e.Pitch, e.Roll)
		fmt.Fprintln(b, `  </pict>`)
	}
	fmt.Fprintln(b, ` </shoot>`)
	fmt.Fprintln(b, `</papywizard>`)
	return b.Flush()
}

func escape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// comment makes s safe inside an XML comment: "--" is not allowed there and a
// comment must not end in "-".
func comment(s string) string {
	s = strings.ReplaceAll(s, "--", "- -")
	return strings.TrimSuffix(s, "-")
}

// number formats a float without trailing zeros.
func number(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
