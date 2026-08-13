// contentlint — validates shared/content/*.json seeds against structural
// expectations (brief §4). M0: verifies JSON parses + required top-level keys.
// Run by `make test` (via `go vet`? no — direct). Invoked as a Go program:
//
//	go run ./tools/contentlint
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var requiredKeys = map[string][]string{
	"items":  {"id", "name", "type", "stackable", "base_stats", "vendor_price"},
	"mobs":   {"id", "name", "level", "hp", "zone_id"},
	"skills": {"id", "name", "class", "rank", "kind"},
	"quests": {"id", "name", "min_level", "giver_npc", "turnin_npc", "objectives", "rewards"},
	"npcs":   {"id", "name", "zone_id", "kind"},
	"zones":  {"id", "name", "safe"},
	"drops":  {"id", "mob_def_id", "item_def_id", "chance"},
}

func main() {
	root := flag.String("root", "shared/content", "content seed directory")
	flag.Parse()

	var failures int
	err := filepath.Walk(*root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("FAIL %s: read: %v\n", path, err)
			failures++
			return nil
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			fmt.Printf("FAIL %s: not valid JSON: %v\n", path, err)
			failures++
			return nil
		}
		// Determine expected keys from the containing directory name.
		dir := filepath.Base(filepath.Dir(path))
		keys, ok := requiredKeys[dir]
		if !ok {
			return nil // unknown dirs: structural check only
		}
		obj, ok := v.(map[string]any)
		if !ok {
			fmt.Printf("FAIL %s: expected a JSON object\n", path)
			failures++
			return nil
		}
		for _, k := range keys {
			if _, present := obj[k]; !present {
				fmt.Printf("FAIL %s: missing key %q\n", path, k)
				failures++
			}
		}
		return nil
	})
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	if failures > 0 {
		fmt.Printf("contentlint: %d failure(s)\n", failures)
		os.Exit(1)
	}
	fmt.Println("contentlint: OK")
}
