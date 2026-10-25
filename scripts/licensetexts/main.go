// Command licensetexts writes the license texts of the Go modules inside the
// core, and of TigerSB itself, to app/assets/licenses.json. The app shows them
// on its licenses screen next to the licenses Flutter collects. Modules with
// the same text share one entry. Run from the repository root:
//
//	go run ./scripts/licensetexts
//
// CI fails when the file is out of date.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const out = "app/assets/licenses.json"

type entry struct {
	Packages []string `json:"packages"`
	Text     string   `json:"text"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// The same modules as scripts/check-licenses.sh.
	list, err := exec.Command("go", "list", "-deps", "-f",
		"{{with .Module}}{{if not .Main}}{{.Dir}}\t{{.Path}}{{end}}{{end}}",
		"./cmd/tigersb-core", "./mobile").Output()
	if err != nil {
		return fmt.Errorf("go list: %w", err)
	}
	own, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	texts := map[string][]string{normalize(own): {"TigerSB"}}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(list)), "\n") {
		dir, path, ok := strings.Cut(line, "\t")
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		file, err := licenseFile(dir)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		t := normalize(b)
		texts[t] = append(texts[t], path)
	}

	// Go's own library is in every binary.
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env: %w", err)
	}
	goLicense, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), "LICENSE"))
	if err != nil {
		return err
	}
	t := normalize(goLicense)
	texts[t] = append(texts[t], "Go")

	entries := make([]entry, 0, len(texts))
	for t, pkgs := range texts {
		sort.Slice(pkgs, func(i, j int) bool { return rank(pkgs[i]) < rank(pkgs[j]) })
		entries = append(entries, entry{Packages: pkgs, Text: t})
	}
	sort.Slice(entries, func(i, j int) bool { return rank(entries[i].Packages[0]) < rank(entries[j].Packages[0]) })
	b, err := json.MarshalIndent(entries, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(b, '\n'), 0o644)
}

// licenseFile is the first LICENSE* or COPYING* file in the module's root.
func licenseFile(dir string) (string, error) {
	names, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, n := range names {
		up := strings.ToUpper(n.Name())
		if !n.IsDir() && (strings.HasPrefix(up, "LICENSE") || strings.HasPrefix(up, "COPYING")) {
			found = append(found, n.Name())
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no license file in %s", dir)
	}
	sort.Strings(found)
	return filepath.Join(dir, found[0]), nil
}

// rank puts TigerSB and Go before the modules, the rest by path.
func rank(p string) string {
	switch p {
	case "TigerSB":
		return "0"
	case "Go":
		return "1"
	}
	return "2" + p
}

func normalize(b []byte) string {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return strings.TrimSpace(string(b))
}
