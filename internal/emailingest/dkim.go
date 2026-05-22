package emailingest

import "strings"

// DKIMPass reports whether any line of the (possibly multi-line)
// Authentication-Results header value says `dkim=pass` for `domain`.
// Match is exact (case-insensitive) on header.d=, no subdomain allowance.
func DKIMPass(authResults, domain string) bool {
	if authResults == "" || domain == "" {
		return false
	}
	domain = strings.ToLower(domain)
	for _, line := range strings.Split(authResults, "\n") {
		if dkimLineMatches(line, domain) {
			return true
		}
	}
	return false
}

func dkimLineMatches(line, domain string) bool {
	line = strings.ToLower(line)
	if !strings.Contains(line, "dkim=pass") {
		return false
	}
	idx := strings.Index(line, "header.d=")
	if idx < 0 {
		return false
	}
	rest := line[idx+len("header.d="):]
	rest = strings.TrimLeft(rest, `"' `)
	end := strings.IndexAny(rest, " \t,;\"'")
	if end >= 0 {
		rest = rest[:end]
	}
	return rest == domain
}

// FromDomain extracts the domain part of an email address, lowercased.
// Returns "" if the input doesn't look like an address.
func FromDomain(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}
