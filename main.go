// Command panoxml reads the gimbal angles from the DJI photos in a folder and
// writes a Papywizard XML file for PTGui's File > Import > Papywizard.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func main() {
	out := flag.String("o", "", "output file (default: papywizard.xml in the image folder)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: panoxml [-o file] <folder>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *out); err != nil {
		fmt.Fprintln(os.Stderr, "panoxml:", err)
		os.Exit(1)
	}
}

func run(dir, out string) error {
	paths, err := imageFiles(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no DNG, TIFF or JPEG images in %s", dir)
	}
	shots := make([]Shot, len(paths))
	for i, p := range paths {
		// A missing position breaks the one-to-one mapping onto PTGui's image
		// list, so a single unreadable file has to stop the run.
		if shots[i], err = ReadShot(p); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
	}
	sort.Slice(shots, func(i, j int) bool { return natLess(shots[i].Name, shots[j].Name) })
	shots, foreign := dropForeign(shots)
	for _, s := range foreign {
		fmt.Fprintf(os.Stderr, "skipped %s: %dx%d, not a frame of this panorama\n", s.Name, s.Width, s.Height)
	}
	unwrapYaw(shots)

	sets := bracketSets(shots)
	entries := make([]Entry, 0, len(shots))
	for _, set := range sets {
		for b, i := range set {
			entries = append(entries, Entry{
				ID:      len(entries) + 1,
				Bracket: b + 1,
				Yaw:     shots[i].Yaw,
				Pitch:   shots[i].Pitch,
				Roll:    shots[i].Roll,
				File:    shots[i].Name,
			})
		}
	}

	focal, focal35 := lens(shots)
	crop := 0.0
	if focal > 0 && focal35 > 0 {
		crop = focal35 / focal
	}
	doc := Doc{
		Title:   filepath.Base(strings.TrimSuffix(dir, string(filepath.Separator))),
		Comment: summary(shots, sets, focal, focal35),
		Focal:   focal,
		Crop:    crop,
		Entries: entries,
	}

	if out == "" {
		out = filepath.Join(dir, "papywizard.xml")
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := writeDoc(f, doc); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, doc.Comment)
	fmt.Fprintln(os.Stderr, grid(shots))
	fmt.Fprintf(os.Stderr, "wrote %s\n", out)
	return nil
}

// rawFirst ranks the extensions panoxml reads. When a folder holds RAW+JPEG
// pairs of the same shot only the higher ranked file is used, so each position
// is emitted once.
var rawFirst = map[string]int{".dng": 0, ".tif": 1, ".tiff": 1, ".jpg": 2, ".jpeg": 2}

func imageFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	best := map[string]string{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		rank, ok := rawFirst[ext]
		if !ok {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if cur, ok := best[stem]; ok && rawFirst[strings.ToLower(filepath.Ext(cur))] <= rank {
			continue
		}
		best[stem] = name
	}
	paths := make([]string, 0, len(best))
	for _, name := range best {
		paths = append(paths, filepath.Join(dir, name))
	}
	slices.Sort(paths)
	return paths, nil
}

// dropForeign keeps the frames whose pixel size the majority of the folder
// shares. A stitched panorama or an exported preview saved next to the source
// frames carries gimbal angles copied from the first frame, so without this it
// would be taken for a position and shift every image onto the wrong one.
func dropForeign(shots []Shot) (kept, foreign []Shot) {
	if len(shots) < 2 {
		return shots, nil
	}
	type size struct{ w, h uint32 }
	count := map[size]int{}
	for _, s := range shots {
		count[size{s.Width, s.Height}]++
	}
	var main size
	for sz, n := range count {
		cur := count[main]
		if n > cur || (n == cur && uint64(sz.w)*uint64(sz.h) < uint64(main.w)*uint64(main.h)) {
			main = sz
		}
	}
	for _, s := range shots {
		if (size{s.Width, s.Height}) == main {
			kept = append(kept, s)
		} else {
			foreign = append(foreign, s)
		}
	}
	return kept, foreign
}

// unwrapYaw removes the jump at +/-180 degrees so a panorama that crosses north
// stays a continuous sweep.
func unwrapYaw(shots []Shot) {
	for i := 1; i < len(shots); i++ {
		d := shots[i].Yaw - shots[i-1].Yaw
		shots[i].Yaw -= 360 * math.Round(d/360)
	}
}

// posTol is the gimbal jitter tolerated when deciding that two consecutive
// shots were taken from the same position, i.e. are one bracketed set.
const posTol = 0.25

// bracketSets groups consecutive shots that share a gimbal position. Without
// bracketing every set holds one shot.
func bracketSets(shots []Shot) [][]int {
	var sets [][]int
	for i := range shots {
		if i > 0 && samePosition(shots[i-1], shots[i]) {
			sets[len(sets)-1] = append(sets[len(sets)-1], i)
			continue
		}
		sets = append(sets, []int{i})
	}
	return sets
}

func samePosition(a, b Shot) bool {
	return math.Abs(a.Yaw-b.Yaw) <= posTol &&
		math.Abs(a.Pitch-b.Pitch) <= posTol &&
		math.Abs(a.Roll-b.Roll) <= posTol
}

// lens returns the focal length shared by the shots. Mixed values yield 0.
func lens(shots []Shot) (focal, focal35 float64) {
	focal, focal35 = shots[0].Focal, shots[0].Focal35
	for _, s := range shots[1:] {
		if s.Focal != focal || s.Focal35 != focal35 {
			return 0, 0
		}
	}
	return focal, focal35
}

func summary(shots []Shot, sets [][]int, focal, focal35 float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d images", len(shots))
	if c := shots[0].Camera(); c != "" {
		fmt.Fprintf(&b, ", %s", c)
	}
	if focal > 0 {
		fmt.Fprintf(&b, ", %s mm", number(focal))
		if focal35 > 0 {
			fmt.Fprintf(&b, " (%s mm equivalent)", number(focal35))
		}
	}
	fmt.Fprintf(&b, ", %d positions", len(sets))
	if n, uniform := setSize(sets); uniform && n > 1 {
		fmt.Fprintf(&b, ", %d bracketed exposures each", n)
	}
	return b.String()
}

func setSize(sets [][]int) (n int, uniform bool) {
	n = len(sets[0])
	for _, s := range sets[1:] {
		if len(s) != n {
			return n, false
		}
	}
	return n, true
}

// grid reports the shape of the panorama so the layout can be checked against
// what was actually flown.
func grid(shots []Shot) string {
	yaws := cluster(shots, func(s Shot) float64 { return s.Yaw })
	pitches := cluster(shots, func(s Shot) float64 { return s.Pitch })
	return fmt.Sprintf("%d columns x %d rows, yaw %.1f to %.1f step %.1f, pitch %.1f to %.1f step %.1f",
		len(yaws), len(pitches),
		yaws[0], yaws[len(yaws)-1], step(yaws),
		pitches[0], pitches[len(pitches)-1], step(pitches))
}

// cluster collects the distinct angles, merging values within posTol.
func cluster(shots []Shot, get func(Shot) float64) []float64 {
	vals := make([]float64, len(shots))
	for i, s := range shots {
		vals[i] = get(s)
	}
	slices.Sort(vals)
	out := vals[:1]
	for _, v := range vals[1:] {
		if v-out[len(out)-1] > posTol {
			out = append(out, v)
		}
	}
	return out
}

// step is the median spacing of a sorted angle list.
func step(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	d := make([]float64, len(vals)-1)
	for i := range d {
		d[i] = vals[i+1] - vals[i]
	}
	slices.Sort(d)
	return d[len(d)/2]
}

// natLess orders file names with digit runs compared numerically, so "-2"
// sorts before "-10".
func natLess(a, b string) bool {
	for a != "" && b != "" {
		ca, ra := chunk(a)
		cb, rb := chunk(b)
		switch {
		case isDigit(ca[0]) && isDigit(cb[0]):
			na, nb := strings.TrimLeft(ca, "0"), strings.TrimLeft(cb, "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
		case ca != cb:
			return ca < cb
		}
		a, b = ra, rb
	}
	return len(a) < len(b)
}

func chunk(s string) (head, rest string) {
	digit := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == digit {
		i++
	}
	return s[:i], s[i:]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
