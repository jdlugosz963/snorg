// Package querylang parses the query expression language into an AST — syntax
// only, no meaning. It is the one package that knows participle; the field
// vocabulary (which names exist, what their values mean, how a regexp or a date
// spec is compiled) lives in pkg/snorg, which walks this AST into Predicates. So
// an unknown field name, an operator on a field that takes none, or a
// nonsensical date parse fine here and are rejected there, with the Term's
// position to point at.
//
// The grammar, precedence NOT > AND > OR:
//
//	expr   := and ( "OR" and )*
//	and    := unary ( "AND" unary )*
//	unary  := "NOT" unary | "(" expr ")" | term
//	term   := ident ( "[" ident "]" )? ( op value )?
//	op     := ":" | "~" | "="
//	value  := "…" | '…' | bare
//
// AND/OR/NOT are case-insensitive; juxtaposition is not implicit AND, so a
// missing operator is a syntax error rather than a silently different query.
package querylang

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// The lexer is stateful because a value is lexed by different rules than an
// expression: "2026-04-04..2026-09-12" is one token, not five, and a bare value
// may hold anything but the expression's own punctuation. An operator pushes the
// Value state and the value pops it. The Value state deliberately has no
// whitespace rule, so a value must follow its operator immediately — "content: x"
// is an error instead of quietly taking "x" as the value of an empty-looking term.
var queryLexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		{Name: "whitespace", Pattern: `\s+`},
		{Name: "AndOp", Pattern: `(?i)\bAND\b`},
		{Name: "OrOp", Pattern: `(?i)\bOR\b`},
		{Name: "NotOp", Pattern: `(?i)\bNOT\b`},
		{Name: "LParen", Pattern: `\(`},
		{Name: "RParen", Pattern: `\)`},
		{Name: "LBracket", Pattern: `\[`, Action: lexer.Push("Scope")},
		{Name: "Ident", Pattern: `[A-Za-z][A-Za-z0-9_-]*`},
		{Name: "Op", Pattern: `[:~=]`, Action: lexer.Push("Value")},
	},
	// A [scope] holds a template box id, which is whatever the config called it —
	// "1" as readily as "header" — so it is lexed by its delimiters rather than by
	// the identifier rules.
	"Scope": {
		{Name: "ScopeQuoted", Pattern: `"(?:\\.|[^"\\])*"|'[^']*'`},
		{Name: "ScopeID", Pattern: `[^\s\[\]]+`},
		{Name: "RBracket", Pattern: `\]`, Action: lexer.Pop()},
	},
	"Value": {
		{Name: "Quoted", Pattern: `"(?:\\.|[^"\\])*"|'[^']*'`, Action: lexer.Pop()},
		{Name: "Bare", Pattern: `[^\s()\[\]]+`, Action: lexer.Pop()},
	},
})

var parser = participle.MustBuild[Expr](
	participle.Lexer(queryLexer),
	participle.Elide("whitespace"),
)

// Expr is a disjunction: one or more AndExprs joined by OR.
type Expr struct {
	Terms []*AndExpr `parser:"@@ ( OrOp @@ )*"`
}

// AndExpr is a conjunction: one or more Unarys joined by AND.
type AndExpr struct {
	Terms []*Unary `parser:"@@ ( AndOp @@ )*"`
}

// Unary is one operand: a negation, a parenthesized expression, or a term.
// Exactly one field is non-nil.
type Unary struct {
	Not  *Unary `parser:"  NotOp @@"`
	Sub  *Expr  `parser:"| LParen @@ RParen"`
	Term *Term  `parser:"| @@"`
}

// Term is a field reference: a name, an optional [scope] qualifier, and an
// optional operator+value. Whether the name exists, may carry a scope, or
// requires a value is pkg/snorg's call, not the grammar's.
type Term struct {
	Pos   lexer.Position
	Field string `parser:"@Ident"`
	Scope Value  `parser:"( LBracket @(ScopeQuoted | ScopeID) RBracket )?"`
	Op    string `parser:"( @Op"`
	Value Value  `parser:"  @(Quoted | Bare) )?"`
}

// HasValue reports whether the term carried an operator and a value.
func (t *Term) HasValue() bool { return t.Op != "" }

// Value is a term's value with its quoting already removed — a quoted value is
// verbatim (single quotes) or unescaped (double quotes: \" and \\), so what a
// consumer sees is the literal string the user meant, regexp backslashes
// included.
type Value string

// Capture implements participle.Capture, unquoting as the token is captured (the
// lexer cannot transform a token value, and every consumer wants the unquoted
// form).
func (v *Value) Capture(values []string) error {
	s, err := unquote(values[0])
	if err != nil {
		return err
	}
	*v = Value(s)
	return nil
}

func unquote(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	q := s[0]
	if q != '"' && q != '\'' {
		return s, nil
	}
	if len(s) < 2 || s[len(s)-1] != q {
		return "", fmt.Errorf("unterminated quoted value %s", s)
	}
	inner := s[1 : len(s)-1]
	if q == '\'' {
		return inner, nil
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		// Only \" and \\ are escapes; every other backslash stays, so a regexp
		// value like "^\d+$" survives double quoting unchanged.
		if inner[i] == '\\' && i+1 < len(inner) && (inner[i+1] == '"' || inner[i+1] == '\\') {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String(), nil
}

// Parse parses a query expression. An empty expression is an error: every
// command that takes one needs a page set, and "match nothing" is not a useful
// default.
func Parse(s string) (*Expr, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("empty query expression")
	}
	return parser.ParseString("", s)
}

// String renders the AST back to canonical source: fully parenthesized around
// every nested expression, values re-quoted only where the lexer would need it.
// Round-trips through Parse.
func (e *Expr) String() string {
	parts := make([]string, len(e.Terms))
	for i, t := range e.Terms {
		parts[i] = t.String()
	}
	return strings.Join(parts, " OR ")
}

func (a *AndExpr) String() string {
	parts := make([]string, len(a.Terms))
	for i, t := range a.Terms {
		parts[i] = t.String()
	}
	return strings.Join(parts, " AND ")
}

func (u *Unary) String() string {
	switch {
	case u.Not != nil:
		return "NOT " + u.Not.String()
	case u.Sub != nil:
		return "(" + u.Sub.String() + ")"
	default:
		return u.Term.String()
	}
}

func (t *Term) String() string {
	var b strings.Builder
	b.WriteString(t.Field)
	if t.Scope != "" {
		b.WriteString("[" + quote(string(t.Scope)) + "]")
	}
	if t.HasValue() {
		b.WriteString(t.Op)
		b.WriteString(quote(string(t.Value)))
	}
	return b.String()
}

func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n()[]\"'") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
