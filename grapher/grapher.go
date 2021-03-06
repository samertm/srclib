package grapher

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/sqs/fileset"

	"sourcegraph.com/sourcegraph/srclib/config"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/repo"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

type Grapher interface {
	Graph(dir string, unit *unit.SourceUnit, c *config.Repository) (*Output, error)
}

// START Output OMIT
// Output is produced by grapher tools.
type Output struct {
	Defs []*graph.Def `json:",omitempty"`
	Refs []*graph.Ref `json:",omitempty"`
	Docs []*graph.Doc `json:",omitempty"`
}

// END Output OMIT

// TODO(sqs): add grapher validation of output

// Graph uses the registered grapher (if any) to graph the source unit (whose repository is cloned to
// dir).
func Graph(dir string, u *unit.SourceUnit, c *config.Repository) (*Output, error) {
	g, registered := Graphers[ptrTo(u)]
	if !registered {
		return nil, fmt.Errorf("no grapher registered for source unit %T", u)
	}

	o, err := g.Graph(dir, u, c)
	if err != nil {
		return nil, err
	}

	// If the grapher is known to output Unicode character offsets instead of
	// byte offsets, then convert all offsets to byte offsets.
	//
	// TODO(sqs): handle this less hackily
	if u.Type != "GoPackage" {
		ensureOffsetsAreByteOffsets(dir, o)
	}

	return sortedOutput(o), nil
}

func ensureOffsetsAreByteOffsets(dir string, output *Output) {
	fset := fileset.NewFileSet()
	files := make(map[string]*fileset.File)

	addOrGetFile := func(filename string) *fileset.File {
		if f, ok := files[filename]; ok {
			return f
		}
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			panic("ReadFile " + filename + ": " + err.Error())
		}

		f := fset.AddFile(filename, fset.Base(), len(data))
		f.SetByteOffsetsForContent(data)
		files[filename] = f
		return f
	}

	fix := func(filename string, offsets ...*int) {
		defer func() {
			if e := recover(); e != nil {
				log.Printf("failed to convert unicode offset to byte offset in file %s (did grapher output a nonexistent byte offset?) continuing anyway...", filename)
			}
		}()
		if filename == "" {
			return
		}
		filename = filepath.Join(dir, filename)
		if fi, err := os.Stat(filename); err != nil || !fi.Mode().IsRegular() {
			return
		}
		f := addOrGetFile(filename)
		for _, offset := range offsets {
			if *offset == 0 {
				continue
			}
			*offset = f.ByteOffsetOfRune(*offset)
		}
	}

	for _, s := range output.Defs {
		fix(s.File, &s.DefStart, &s.DefEnd)
	}
	for _, r := range output.Refs {
		fix(r.File, &r.Start, &r.End)
	}
	for _, d := range output.Docs {
		fix(d.File, &d.Start, &d.End)
	}
}

func sortedOutput(o *Output) *Output {
	sort.Sort(graph.Defs(o.Defs))
	sort.Sort(graph.Refs(o.Refs))
	sort.Sort(graph.Docs(o.Docs))
	return o
}

// NormalizeData sorts data.
func NormalizeData(o *Output) error {
	for _, ref := range o.Refs {
		if ref.DefRepo != "" {
			ref.DefRepo = repo.MakeURI(string(ref.DefRepo))
		}
	}

	sortedOutput(o)
	return nil
}
