package gemini

import (
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
)

// One attempt with a 30 MiB file allocates at most twice its size.
func TestEncodeRequestAllocations(t *testing.T) {
	if mediatest.Race {
		t.Skip("the race detector changes allocation counts")
	}
	if testing.Short() {
		t.Skip("allocates hundreds of MiB")
	}
	file := append(mediatest.PDF(), make([]byte, 30<<20)...)
	req := decide.NewRequest(map[string]string{"supplier": "Acme"},
		decide.WithMedia(decide.Media{MIMEType: "application/pdf", Data: file}), decide.WithRequestExtra("store", false))
	decide.Ask(req, "a", decide.Noul("Does the total match?"))
	req.Model = "gemini-decision-1"
	res := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := encodeRequest(req); err != nil {
				b.Fatal(err)
			}
		}
	})
	size := int64(len(file))
	t.Logf("%d bytes allocated for a %d-byte file (%.2fx)", res.AllocedBytesPerOp(), size, float64(res.AllocedBytesPerOp())/float64(size))
	if got := res.AllocedBytesPerOp(); got > 2*size {
		t.Fatalf("one attempt allocates %d bytes for a %d-byte file; want at most %d", got, size, 2*size)
	}
}
