package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

type Printer struct {
	Format string
	Writer io.Writer
}

func NewPrinter(format string, w io.Writer) *Printer {
	return &Printer{Format: format, Writer: w}
}

func (p *Printer) Print(data any) error {
	switch p.Format {
	case "json":
		return p.printJSON(data)
	case "yaml":
		return p.printYAML(data)
	default:
		return fmt.Errorf("use PrintTable for table output")
	}
}

func (p *Printer) PrintTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(p.Writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

func (p *Printer) printJSON(data any) error {
	enc := json.NewEncoder(p.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func (p *Printer) printYAML(data any) error {
	enc := yaml.NewEncoder(p.Writer)
	enc.SetIndent(2)
	return enc.Encode(data)
}
