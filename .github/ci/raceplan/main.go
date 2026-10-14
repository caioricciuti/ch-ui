// Command raceplan decides which Go packages one CI shard runs under -race.
//
// On a pull request (-base set) only packages whose tests are affected by the
// change run: a package is affected when its own directory changed or when it
// (or its tests) imports a changed package. A change to go.mod, go.sum, the CI
// workflow or this planner runs everything. Without -base (main, releases)
// everything runs. The affected units are spread over the shards by their
// recent CI duration, and the handlers package is split by test name because
// it is the longest one.
//
// Output, one go test invocation per line, arguments separated by spaces:
//
//	./internal/a ./internal/b
//	./internal/server/handlers -run ^(TestA|TestB)$
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Seconds per package in a recent CI -race run; anything missing weighs 4.
var weights = map[string]float64{
	"internal/server/handlers": 140,
	"internal/rbac":            81,
	"internal/database":        80,
	"internal/mcpserver":       64,
	"internal/pipelines":       45,
	"internal/reports":         33,
	"internal/clusterhealth":   22,
	"cmd":                      18,
	"internal/models":          16,
	"internal/sqlalerts":       16,
}

const splitPkg = "internal/server/handlers"
const splitParts = 3

var fullTriggers = []string{"go.mod", "go.sum", ".github/workflows/ci.yml", ".github/ci/"}

type unit struct {
	pkg    string // relative dir, "." for the module root
	tests  []string
	weight float64
}

func main() {
	shard := flag.Int("shard", 1, "this shard, 1-based")
	shards := flag.Int("shards", 1, "number of shards")
	base := flag.String("base", "", "git revision to diff against; empty runs everything")
	head := flag.String("head", "HEAD", "git revision with the change")
	flag.Parse()
	if *shard < 1 || *shard > *shards {
		fail("shard %d out of 1..%d", *shard, *shards)
	}

	module := strings.TrimSpace(run("go", "list", "-m"))
	pkgs := testedPackages(module)

	selected := pkgs
	if *base != "" {
		changed := strings.Fields(run("git", "diff", "--name-only", *base, *head))
		if !touchesAny(changed, fullTriggers) {
			own, code := changedPackages(changed, pkgs)
			selected = affected(pkgs, own, code)
		}
	}

	var units []unit
	for _, p := range sortedKeys(selected) {
		w, ok := weights[p]
		if !ok {
			w = 4
		}
		if p == splitPkg {
			names := testNames(p)
			for i := 0; i < splitParts; i++ {
				var part []string
				for j := i; j < len(names); j += splitParts {
					part = append(part, names[j])
				}
				if len(part) > 0 {
					units = append(units, unit{pkg: p, tests: part, weight: w / splitParts})
				}
			}
			continue
		}
		units = append(units, unit{pkg: p, weight: w})
	}

	// Heaviest first onto the lightest shard; ties keep a fixed order so
	// every shard computes the same assignment.
	sort.SliceStable(units, func(i, j int) bool { return units[i].weight > units[j].weight })
	load := make([]float64, *shards)
	var mine []unit
	for _, u := range units {
		best := 0
		for s := range load {
			if load[s] < load[best] {
				best = s
			}
		}
		load[best] += u.weight
		if best == *shard-1 {
			mine = append(mine, u)
		}
	}

	var whole []string
	for _, u := range mine {
		if u.tests == nil {
			whole = append(whole, "./"+u.pkg)
			continue
		}
		fmt.Printf("./%s -run ^(%s)$\n", u.pkg, strings.Join(u.tests, "|"))
	}
	if len(whole) > 0 {
		fmt.Println(strings.Join(whole, " "))
	}
	fmt.Fprintf(os.Stderr, "raceplan: %d of %d tested packages selected, shard %d/%d runs %d units\n",
		len(selected), len(pkgs), *shard, *shards, len(mine))
}

// testedPackages maps each package that has tests (relative dir) to the
// packages its tests depend on (relative dirs, module packages only).
func testedPackages(module string) map[string][]string {
	out := run("go", "list", "-test", "-f", "{{.ForTest}}\t{{join .Deps \" \"}}", "./...")
	pkgs := map[string][]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		forTest, deps, _ := strings.Cut(sc.Text(), "\t")
		if forTest == "" {
			continue
		}
		p := rel(module, forTest)
		for _, d := range strings.Fields(deps) {
			d, _, _ = strings.Cut(d, " ")
			if d == module || strings.HasPrefix(d, module+"/") {
				pkgs[p] = append(pkgs[p], rel(module, d))
			}
		}
		if _, ok := pkgs[p]; !ok {
			pkgs[p] = nil
		}
	}
	return pkgs
}

// changedPackages maps changed files to package dirs. A file below a
// package (testdata, embedded assets) belongs to the nearest package above
// it, but nothing climbs to the module root: a UI or docs change touches no
// Go package.
// The second map holds packages where something other than tests changed;
// only those can affect other packages, since tests never compile into a
// dependency.
func changedPackages(files []string, pkgs map[string][]string) (map[string]bool, map[string]bool) {
	known := map[string]bool{}
	for p, deps := range pkgs {
		known[p] = true
		for _, d := range deps {
			known[d] = true
		}
	}
	changed := map[string]bool{}
	code := map[string]bool{}
	mark := func(d, f string) {
		changed[d] = true
		if !strings.HasSuffix(f, "_test.go") && !strings.Contains(f, "/testdata/") {
			code[d] = true
		}
	}
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(f))
		if dir == "." {
			if strings.HasSuffix(f, ".go") {
				mark(".", f)
			}
			continue
		}
		for d := dir; d != "." && d != "/"; d = filepath.ToSlash(filepath.Dir(d)) {
			if known[d] {
				mark(d, f)
				break
			}
		}
	}
	return changed, code
}

func affected(pkgs map[string][]string, changed, code map[string]bool) map[string][]string {
	sel := map[string][]string{}
	for p, deps := range pkgs {
		if changed[p] {
			sel[p] = deps
			continue
		}
		for _, d := range deps {
			if code[d] {
				sel[p] = deps
				break
			}
		}
	}
	return sel
}

var testFunc = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(\w+ \*testing\.T\)`)

// testNames lists the top-level test functions of a package, sorted.
func testNames(pkg string) []string {
	files, err := filepath.Glob(filepath.Join(pkg, "*_test.go"))
	if err != nil {
		fail("%v", err)
	}
	var names []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fail("%v", err)
		}
		for _, m := range testFunc.FindAllSubmatch(b, -1) {
			names = append(names, string(m[1]))
		}
	}
	sort.Strings(names)
	return names
}

func touchesAny(files, prefixes []string) bool {
	for _, f := range files {
		for _, p := range prefixes {
			if f == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(f, p)) {
				return true
			}
		}
	}
	return false
}

func rel(module, importPath string) string {
	if importPath == module {
		return "."
	}
	return strings.TrimPrefix(importPath, module+"/")
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func run(name string, args ...string) string {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		fail("%s %s: %v\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "raceplan: "+format+"\n", args...)
	os.Exit(1)
}
