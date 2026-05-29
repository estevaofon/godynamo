package dynamo

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var profileSectionRe = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)

// ListProfilesFromReader parses INI section headers from r as profile names.
// It returns the names in file order plus the default profile name
// ("default" when a [default] section exists, else "").
//
// Best-effort: a scanner error mid-read yields a possibly-truncated list and no
// error. A local ~/.aws/credentials file is small and rarely unreadable
// mid-parse, so the simpler error-free signature is intentional.
func ListProfilesFromReader(r io.Reader) (names []string, def string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		m := profileSectionRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		names = append(names, name)
		if name == "default" {
			def = "default"
		}
	}
	return names, def
}

// orderProfiles returns the default profile first (when present), then the
// remaining names sorted alphabetically.
func orderProfiles(raw []string, def string) []string {
	ordered := []string{}
	if def != "" {
		ordered = append(ordered, def)
	}
	rest := []string{}
	for _, n := range raw {
		if n != def {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// ListProfiles reads ~/.aws/credentials and returns profile names (default
// first, then sorted) plus the default name ("" if none). A missing file
// yields an empty slice and nil error.
func ListProfiles() (names []string, def string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(filepath.Join(home, ".aws", "credentials"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	defer f.Close()
	raw, def := ListProfilesFromReader(f)
	return orderProfiles(raw, def), def, nil
}
