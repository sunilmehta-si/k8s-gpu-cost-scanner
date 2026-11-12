// Package price decides what one GPU-hour costs on each node.
//
// gpucost ships no prices of its own: list prices differ by cloud, region, commitment and contract, and a
// made-up number would make every dollar in the report wrong. Prices come from you (a CSV file or flags) or
// from OpenCost; a node with no price is reported in GPU-hours only and named as a blind spot.
package price

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// Price is the cost of one GPU-hour on a node, and where that number came from.
type Price struct {
	PerGPUHour float64 `json:"per_gpu_hour"`
	Source     string  `json:"source"`
}

// Table holds the prices you set, by GPU product, product family or instance type, and a default.
type Table struct {
	products  map[gpu.Product]float64
	instances map[string]float64
	fallback  *float64
	origin    map[string]string
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{products: map[gpu.Product]float64{}, instances: map[string]float64{}, origin: map[string]string{}}
}

// Len is the number of prices set.
func (t *Table) Len() int {
	n := len(t.products) + len(t.instances)
	if t.fallback != nil {
		n++
	}
	return n
}

// Set adds one price. The key is a GPU product in any spelling ("H100 80GB", "h100-80gb", "nvidia-l4"), a product
// family without memory size ("a100"), "instance:" and a cloud instance type ("instance:a2-highgpu-1g"), or "*"
// for every GPU nothing else prices. origin says where the price came from, for the report.
func (t *Table) Set(key string, perGPUHour float64, origin string) error {
	key = strings.TrimSpace(key)
	if perGPUHour < 0 || perGPUHour != perGPUHour {
		return fmt.Errorf("price for %q must be a number zero or above", key)
	}
	switch {
	case key == "":
		return errors.New("price with an empty key")
	case key == "*":
		v := perGPUHour
		t.fallback = &v
		t.origin["*"] = origin
	case strings.HasPrefix(key, "instance:"):
		it := strings.TrimSpace(strings.TrimPrefix(key, "instance:"))
		if it == "" {
			return errors.New(`"instance:" needs an instance type`)
		}
		t.instances[it] = perGPUHour
		t.origin["instance:"+it] = origin
	default:
		p := gpu.Normalize(key)
		if p == gpu.Unknown {
			return fmt.Errorf("%q does not name a GPU", key)
		}
		t.products[p] = perGPUHour
		t.origin[string(p)] = origin
	}
	return nil
}

// ParseFlag reads a --price value: key=price, for example h100-80gb=4.10.
func (t *Table) ParseFlag(v string) error {
	key, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("--price %q: want key=price, for example h100-80gb=4.10", v)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil {
		return fmt.Errorf("--price %q: %w", v, err)
	}
	return t.Set(key, f, "--price "+strings.TrimSpace(key))
}

// LoadCSV reads prices from a CSV file with a header row naming a "gpu" column (the key, as for Set) and a
// "price_per_gpu_hour" column. Other columns, such as a note on where the price came from, are ignored. Lines
// starting with # are comments.
func (t *Table) LoadCSV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.readCSV(f, path)
}

func (t *Table) readCSV(r io.Reader, name string) error {
	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	keyCol, priceCol := -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "gpu":
			keyCol = i
		case "price_per_gpu_hour":
			priceCol = i
		}
	}
	if keyCol < 0 || priceCol < 0 {
		return fmt.Errorf(`%s: the header must name a "gpu" and a "price_per_gpu_hour" column`, name)
	}
	line := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		line++
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if len(rec) <= keyCol || len(rec) <= priceCol {
			return fmt.Errorf("%s line %d: too few columns", name, line)
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rec[priceCol]), 64)
		if err != nil {
			return fmt.Errorf("%s line %d: price %q: %w", name, line, rec[priceCol], err)
		}
		if err := t.Set(rec[keyCol], v, name); err != nil {
			return fmt.Errorf("%s line %d: %w", name, line, err)
		}
	}
}

// Book prices every node of a fleet.
type Book struct {
	nodes map[string]Price
}

// For returns the price of a GPU-hour on the node, or false when nothing priced it.
func (b *Book) For(node string) (Price, bool) {
	p, ok := b.nodes[node]
	return p, ok
}

// Resolve prices each node, most specific first: your price for its instance type, for its exact GPU product,
// for the product's family, then OpenCost's price for the node, then your "*" default.
func Resolve(t *Table, f *fleet.Fleet, s *snapshot.Snapshot) *Book {
	opencost := map[string]float64{}
	for _, smp := range s.Instant["opencost_gpu_hourly"] {
		if n := smp.Labels["node"]; n != "" && smp.Value > 0 && smp.Value == smp.Value {
			opencost[n] = smp.Value
		}
	}
	b := &Book{nodes: map[string]Price{}}
	for name, n := range f.Nodes {
		if v, ok := t.instances[n.InstanceType]; ok && n.InstanceType != "" {
			b.nodes[name] = Price{v, t.origin["instance:"+n.InstanceType] + " (instance " + n.InstanceType + ")"}
			continue
		}
		if v, ok := t.products[n.Product]; ok {
			b.nodes[name] = Price{v, t.origin[string(n.Product)] + " (" + string(n.Product) + ")"}
			continue
		}
		if v, ok := t.products[n.Product.Family()]; ok {
			b.nodes[name] = Price{v, t.origin[string(n.Product.Family())] + " (" + string(n.Product.Family()) + ")"}
			continue
		}
		if v, ok := opencost[name]; ok {
			b.nodes[name] = Price{v, "OpenCost node_gpu_hourly_cost"}
			continue
		}
		if t.fallback != nil {
			b.nodes[name] = Price{*t.fallback, t.origin["*"] + " (default *)"}
		}
	}
	return b
}
