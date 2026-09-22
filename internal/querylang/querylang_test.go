package querylang

import "testing"

// Parse is checked through String(), which renders the AST fully parenthesized:
// a wrong precedence or a mis-scoped value shows up as a different string, so one
// table covers both shape and capture.
func TestParse(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"starred", "starred"},
		{"NOT templated", "NOT templated"},
		{"not templated", "NOT templated"},
		// AND binds tighter than OR; NOT tighter than both.
		{"a OR b AND c", "a OR b AND c"},
		{"a AND b OR c", "a AND b OR c"},
		{"NOT a AND b", "NOT a AND b"},
		{"NOT NOT a", "NOT NOT a"},
		{"(a OR b) AND c", "(a OR b) AND c"},
		{"NOT (a OR b)", "NOT (a OR b)"},
		{"a and b or c", "a AND b OR c"},
		// Operators and values.
		{"tag=work", "tag=work"},
		{"tag:work", "tag:work"},
		{"date:2026-04-04..2026-09-12", "date:2026-04-04..2026-09-12"},
		{`content:"some thing"`, `content:"some thing"`},
		{`content:'some thing'`, `content:"some thing"`},
		{`content~"^(a|b)$"`, `content~"^(a|b)$"`},
		{`content~"^\d+$"`, `content~^\d+$`}, // only \" and \\ are escapes
		{`content~"a\"b"`, `content~"a\"b"`}, // …so a quote can still be embedded
		// A bare value may hold brackets, so an ordinary character class needs no
		// quoting. String() re-quotes it anyway — one quote() serves both the value
		// and the [scope], where a bracket really would be ambiguous.
		{"content~[Tt]odo", `content~"[Tt]odo"`},
		{"content~foo[0-9]+", `content~"foo[0-9]+"`},
		{"region[hdr]~[0-9]+", `region[hdr]~"[0-9]+"`},
		{"region[title]:invoice", "region[title]:invoice"},
		// A box id is whatever the config called it, digits included.
		{"region[1]:invoice", "region[1]:invoice"},
		{"region[2.b-c]:invoice", "region[2.b-c]:invoice"},
		{`region["a b"]:invoice`, `region["a b"]:invoice`},
		{"region:invoice", "region:invoice"},
		// A field name may contain a keyword without being one.
		{"android", "android"},
		{"nothing:x", "nothing:x"},
	} {
		e, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got := e.String(); got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Every String() output must itself parse to the same thing, which is what makes
// the table above a sufficient check of the captures.
func TestStringRoundTrips(t *testing.T) {
	for _, in := range []string{
		`starred AND (date:2026-04-04..2026-09-12 OR content:"some thing")`,
		`NOT templated OR region[title]~^Inv`,
		`content~"^(a|b)$"`,
		`content~[Tt]odo`,
		`note="F 1"`,
	} {
		e, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		again, err := Parse(e.String())
		if err != nil {
			t.Fatalf("Parse(%q) [round trip]: %v", e.String(), err)
		}
		if again.String() != e.String() {
			t.Errorf("round trip of %q: %q != %q", in, again.String(), e.String())
		}
	}
}

// The grammar has no implicit AND and no whitespace between an operator and its
// value, so both read as syntax errors instead of a quietly different query.
func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"starred tag:work", // juxtaposition is not AND
		"content: foo",     // the value must follow the operator
		"content:",
		"content~^# Invoice", // an unquoted value stops at the space
		"content~a(b|c)",     // …and at a paren, which closes a group
		`content:"foo`,       // unterminated quote
		"(a",
		"a)",
		"AND",
		"a AND",
		"NOT",
		"[title]:x",
		"region[]:x",
		"region[a b]:x", // an unquoted id stops at the space
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): want error", in)
		}
	}
}

func TestTermHasValue(t *testing.T) {
	e, err := Parse("starred AND tag:work")
	if err != nil {
		t.Fatal(err)
	}
	terms := e.Terms[0].Terms
	if got := terms[0].Term; got.HasValue() {
		t.Errorf("%q: HasValue = true, want false", got)
	}
	got := terms[1].Term
	if !got.HasValue() || got.Op != ":" || got.Value != "work" {
		t.Errorf("tag:work = %+v, want op %q value %q", got, ":", "work")
	}
	if got.Pos.Column != 13 {
		t.Errorf("tag:work column = %d, want 13", got.Pos.Column)
	}
}
