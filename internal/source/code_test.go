package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/skill"
)

// units lists what a scanner found as "name start-end", with the lead
// line when comments come first, as "(from N)".
func units(t *testing.T, path, src string) []string {
	t.Helper()
	c, err := languageOf(path).scan(src)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var out []string
	for _, f := range c.fns {
		s := fmt.Sprintf("%s %d-%d", f.name, f.start, f.end)
		if f.lead != f.start {
			s += fmt.Sprintf(" (from %d)", f.lead)
		}
		out = append(out, s)
	}
	return out
}

func TestGoFunctions(t *testing.T) {
	src := `package store

import "sync"

// Set holds values.
type Set[T comparable] struct{ m map[T]bool }

// Add adds v.
func (s *Set[T]) Add(v T) {
	s.m[v] = true
}

func New() *Set[int] { return nil }

var handler = func() {
	println("hi")
}

var x = 1

func asm()
`
	want := []string{"Set.Add 9-11 (from 8)", "New 13-13", "handler 15-17"}
	if got := units(t, "a.go", src); !reflect.DeepEqual(got, want) {
		t.Fatalf("units:\n%q\nwant\n%q", got, want)
	}
}

func TestPythonFunctions(t *testing.T) {
	src := `import os
from typing import Optional


# Loads things.
@cache
@other(1)
def load(
    path: str,
) -> Optional[str]:
    """Load it.

def not_a_function():
    """
    return os.read(path)


class Store(Base):
    """A store."""

    def __init__(self):
        self.x = {
    1: 2}

    async def save(self, a=lambda: 1):
        pass

    class Meta:
        def ordering(self): return []


x = 1
if __name__ == "__main__":
    def main(): pass
`
	want := []string{"load 6-15 (from 5)", "Store.__init__ 21-23", "Store.save 25-26", "Store.Meta.ordering 29-29"}
	if got := units(t, "a.py", src); !reflect.DeepEqual(got, want) {
		t.Fatalf("units:\n%q\nwant\n%q", got, want)
	}
}

func TestJavaScriptFunctions(t *testing.T) {
	src := "import { x } from \"./x\"\n" + // 1
		"const re = /[{}(]+/g\n" + // 2
		"const tpl = `a ${b ? `{` : \"}\"} c`\n" + // 3
		"\n" + // 4
		"/** Overloads: only the body is a unit. */\n" + // 5
		"export function over(a: string): string\n" + // 6
		"export function over(a: any): any {\n" + // 7
		"  return a\n" + // 8
		"}\n" + // 9
		"\n" + // 10
		"@Component({ selector: \"x\" })\n" + // 11
		"export class Widget<T extends { id: string }> extends Base {\n" + // 12
		"  static count = 0\n" + // 13
		"  private handler = (e: Event) => {\n" + // 14
		"    if (e) return\n" + // 15
		"  }\n" + // 16
		"  cb?: () => void\n" + // 17
		"  @Input()\n" + // 18
		"  get name(): string { return \"w\" }\n" + // 19
		"  #hidden() {}\n" + // 20
		"  [Symbol.iterator]() { return this }\n" + // 21
		"  abstract shape(): void;\n" + // 22
		"}\n" + // 23
		"\n" + // 24
		"export const add = (a: number, b: number): number => a + b\n" + // 25
		"const plain = { a: 1 }\n" + // 26
		"type Fn = (a: string) => void\n" + // 27
		"module.exports = function legacy() {}\n" + // 28
		"app.get(\"/users\", async (req, res) => {\n" + // 29
		"  res.json({ ok: true })\n" + // 30
		"})\n" + // 31
		"function result(): { a: string }[] {\n" + // 32
		"  return <p>Don't {\"}\"} stop</p>\n" + // 33
		"}\n" // 34
	want := []string{
		"over 7-9", "Widget.handler 14-16", "Widget.name 18-19", "Widget.#hidden 20-20",
		"Widget.[Symbol.iterator] 21-21", "add 25-25", "module.exports 28-28", `app.get "/users" 29-31`, "result 32-34",
	}
	if got := units(t, "a.tsx", src); !reflect.DeepEqual(got, want) {
		t.Fatalf("units:\n%q\nwant\n%q", got, want)
	}
}

func TestTestsAreUnits(t *testing.T) {
	src := `import { describe, it } from "vitest"

describe("parser", () => {
  const input = "x"
  beforeEach(() => {
    reset()
  })
  it("reads a header", () => {
    expect(parse(input)).toBe(1)
  })
  describe("errors", () => {
    it.each([1, 2])("rejects %s", (n) => {
      expect(() => parse(n)).toThrow()
    })
  })
})

describe("empty", () => {})
`
	want := []string{
		"parser › beforeEach 5-7", `parser › it "reads a header" 8-10`,
		`parser › errors › it.each "rejects %s" 12-14`, `describe "empty" 18-18`,
	}
	if got := units(t, "a.test.ts", src); !reflect.DeepEqual(got, want) {
		t.Fatalf("units:\n%q\nwant\n%q", got, want)
	}
}

func TestJavaFunctions(t *testing.T) {
	src := `package app;

import java.util.List;

@Service
public class Store<T> extends Base implements Api {
    private final Runnable task = () -> run();
    private int count = 0;

    /** Makes a store. */
    public Store() {
        super();
    }

    @Override
    public <R> List<R> map(Function<T, R> f) throws IOException {
        return null;
    }

    abstract void shape();

    static {
        init();
    }

    enum Kind {
        A, B { void x() {} };
        void kind() {}
    }

    record Point(int x, int y) {
        Point {
            check(x);
        }
    }
}

@interface Named {
    String[] value() default {};
}
`
	want := []string{
		"Store.task 7-7", "Store.Store 11-13 (from 10)", "Store.map 15-18",
		"Store.Kind.kind 28-28", "Store.Point.Point 32-34",
	}
	if got := units(t, "A.java", src); !reflect.DeepEqual(got, want) {
		t.Fatalf("units:\n%q\nwant\n%q", got, want)
	}
}

func TestScannersNoticeWhenLost(t *testing.T) {
	for path, src := range map[string]string{
		"a.ts":   "function f() {\n  return 1\n",
		"a.py":   "def f(:\n    pass\n",
		"A.java": "class A { void f() { }",
		"a.go":   "package a\nfunc f( {}\n",
	} {
		if _, err := languageOf(path).scan(src); !errors.Is(err, errLost) {
			t.Errorf("%s: err = %v", path, err)
		}
	}
}

func TestEachFunction(t *testing.T) {
	tree(t, map[string]string{
		"app.py": `import os


class Store:
    # Saves it.
    def save(self):
        return os.sep
`,
		"lib/util.go":   "package lib\n\nfunc Util() {}\n",
		"lib/consts.go": "package lib\n\nconst A = 1\n",
		"lib/broken.ts": "function f() {\n",
		"README.md":     "# Readme\n",
	})
	items, warnings := walk(t, []string{"."}, "", Options{Input: skill.Text, Each: skill.EachFunction})
	if got := labels(items); !reflect.DeepEqual(got, []string{"app.py#L6", "lib/broken.ts", "lib/util.go#L3"}) {
		t.Fatalf("labels = %v", got)
	}
	if items[0].Unit != skill.EachFunction || string(items[0].Value) != `"Store.save"` || items[1].Unit != skill.EachFile {
		t.Fatalf("items = %+v", items)
	}
	var s partState
	json.Unmarshal(items[0].State, &s)
	want := partState{Path: "app.py", Language: "py", Function: "Store.save", Lines: "5-7",
		Context: "import os\n\nclass Store:\n…", Content: "    # Saves it.\n    def save(self):\n        return os.sep"}
	if s != want {
		t.Fatalf("state = %+v\nwant  %+v", s, want)
	}
	wantWarnings := []string{
		"Skipped 1 file not in Go, Python, JavaScript, TypeScript, or Java",
		"Skipped 1 file with no functions",
		"Could not find the functions in lib/broken.ts, so it was judged whole",
	}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("warnings:\n%q\nwant\n%q", warnings, wantWarnings)
	}
	for path, msg := range map[string]string{
		"README.md":     "README.md is not in Go, Python, JavaScript, TypeScript, or Java, so decide cannot find its functions; use --each file",
		"lib/consts.go": "lib/consts.go has no functions; use --each file",
		"-":             "--each function reads source files, not stdin",
	} {
		err := Walk(context.Background(), []string{path}, strings.NewReader("x"), Options{Input: skill.Text, Each: skill.EachFunction}, func(Item) error { return nil })
		if err == nil || err.Error() != msg {
			t.Errorf("%s: err = %v", path, err)
		}
	}
}

func TestLargeFunctionsHaveParts(t *testing.T) {
	defer func(n int) { MaxItemBytes = n }(MaxItemBytes)
	MaxItemBytes = StateRoom + 85
	src := "package a\n\nimport \"os\"\n\nfunc Big() {\n\ta := 1\n\tb := 2\n\n\tc := 3\n\td := 4\n\n\tos.Exit(a + b + c + d)\n}\n"
	tree(t, map[string]string{"a.go": src})
	items, _ := walk(t, []string{"a.go"}, "", Options{Input: skill.Text, Each: skill.EachFunction})
	if len(items) != 1 || items[0].State != nil || len(items[0].Parts) < 2 {
		t.Fatalf("items = %+v", items)
	}
	var got []string
	for _, p := range items[0].Parts {
		var s partState
		json.Unmarshal(p.State, &s)
		if s.Function != "Big" || s.Context != "package a\nimport \"os\"" {
			t.Fatalf("part state = %+v", s)
		}
		got = append(got, s.Lines)
	}
	// Cut after a blank line.
	if want := []string{"5-7", "9-13"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
}

func TestLargeCodeFilesBreakBetweenFunctions(t *testing.T) {
	defer func(n int) { MaxItemBytes = n }(MaxItemBytes)
	MaxItemBytes = StateRoom + len("a.py") + 80
	src := "def one():\n    return 1\n\n\ndef two():\n    x = 2\n    return x\n\n\ndef three():\n    return 3\n"
	tree(t, map[string]string{"a.py": src})
	items, _ := walk(t, []string{"a.py"}, "", Options{Input: skill.Text})
	var got []string
	for _, p := range items[0].Parts {
		var s partState
		json.Unmarshal(p.State, &s)
		got = append(got, s.Lines+" "+s.Section)
	}
	// Whole functions, named in each part.
	if want := []string{"1-7 one, two", "10-11 three"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
}
