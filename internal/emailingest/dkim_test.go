package emailingest

import "testing"

func TestDKIMPass_Simple(t *testing.T) {
	h := "mail.example; dkim=pass header.d=example.com"
	if !DKIMPass(h, "example.com") {
		t.Error("expected pass for matching domain")
	}
}

func TestDKIMPass_WrongDomain(t *testing.T) {
	h := "mail.example; dkim=pass header.d=other.com"
	if DKIMPass(h, "example.com") {
		t.Error("expected fail when domain doesn't align")
	}
}

func TestDKIMPass_Fail(t *testing.T) {
	h := "mail.example; dkim=fail header.d=example.com"
	if DKIMPass(h, "example.com") {
		t.Error("expected fail")
	}
}

func TestDKIMPass_MultipleResults(t *testing.T) {
	// Two A-R headers concatenated on \n; one passes for the right domain.
	h := "mail.example; dkim=fail header.d=other.com\nmail.example; dkim=pass header.d=example.com"
	if !DKIMPass(h, "example.com") {
		t.Error("expected pass — at least one header authenticated the right domain")
	}
}

func TestDKIMPass_NoHeaderD(t *testing.T) {
	h := "mail.example; dkim=pass spf=pass"
	if DKIMPass(h, "example.com") {
		t.Error("expected fail when no header.d is present")
	}
}

func TestDKIMPass_StrictDomainMatch(t *testing.T) {
	// Many forwarders sign with bounces.gmail.com etc. — exact-match only.
	h := "mail.example; dkim=pass header.d=bounces.example.com"
	if DKIMPass(h, "example.com") {
		t.Error("expected strict match — no subdomain match")
	}
}

func TestDKIMPass_Empty(t *testing.T) {
	if DKIMPass("", "example.com") {
		t.Error("empty A-R must not pass")
	}
	if DKIMPass("mail.x; dkim=pass header.d=example.com", "") {
		t.Error("empty domain must not pass")
	}
}

func TestDKIMPass_QuotedDomain(t *testing.T) {
	h := `mail.example; dkim=pass header.d="example.com"`
	if !DKIMPass(h, "example.com") {
		t.Error("expected pass with quoted header.d value")
	}
}

func TestFromDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"alice@Example.COM", "example.com"},
		{"a@b.co.uk", "b.co.uk"},
		{"not-an-email", ""},
		{"@nohost", ""},
		{"trailing@", ""},
	}
	for _, c := range cases {
		if got := FromDomain(c.in); got != c.want {
			t.Errorf("FromDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
