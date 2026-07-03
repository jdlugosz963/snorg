package archive

import (
	"strings"
	"testing"
)

func TestRecolorSubstitutesDefaults(t *testing.T) {
	in := []byte(`<svg><path d="M1 2" fill="#000000" /><path d="M3 4" fill="#c9c9c9" /></svg>`)
	out := string(recolor(in, map[string]string{"black": "#1a1aff", "gray": "green"}))
	if !strings.Contains(out, `fill="#1a1aff"`) || !strings.Contains(out, `fill="green"`) {
		t.Errorf("recolor did not remap shades:\n%s", out)
	}
	if strings.Contains(out, `fill="#000000"`) || strings.Contains(out, `fill="#c9c9c9"`) {
		t.Errorf("original fills remain:\n%s", out)
	}
}

func TestRecolorLeavesOverlaysAndUnsetShades(t *testing.T) {
	in := []byte(`<svg><path d="M1 2" fill="#000000" /><path d="M3 4" fill="#9d9d9d" /><rect fill="none" /></svg>`)
	out := string(recolor(in, map[string]string{"black": "red"}))
	if !strings.Contains(out, `fill="none"`) {
		t.Error("overlay fill=none was touched")
	}
	if !strings.Contains(out, `fill="#9d9d9d"`) {
		t.Error("unset darkgray shade was changed")
	}
	if !strings.Contains(out, `fill="red"`) {
		t.Error("black was not recolored")
	}
}

func TestRecolorNoMapNoop(t *testing.T) {
	in := []byte(`<svg><path fill="#000000" /></svg>`)
	if out := recolor(in, nil); string(out) != string(in) {
		t.Errorf("nil map changed svg: %s", out)
	}
	if out := recolor(in, map[string]string{}); string(out) != string(in) {
		t.Errorf("empty map changed svg: %s", out)
	}
	if out := recolor(in, map[string]string{"purple": "x"}); string(out) != string(in) {
		t.Errorf("unknown shade changed svg: %s", out)
	}
}

// A shade recolored to another shade's default must not cascade: substitution is a
// single non-overlapping pass, independent of map order.
func TestRecolorNoCascade(t *testing.T) {
	in := []byte(`<svg><path fill="#000000" /><path fill="#9d9d9d" /></svg>`)
	out := string(recolor(in, map[string]string{"black": "#9d9d9d", "darkgray": "#ffffff"}))
	if strings.Count(out, `fill="#9d9d9d"`) != 1 {
		t.Errorf("black->#9d9d9d cascaded:\n%s", out)
	}
	if strings.Count(out, `fill="#ffffff"`) != 1 {
		t.Errorf("darkgray not mapped exactly once:\n%s", out)
	}
}
