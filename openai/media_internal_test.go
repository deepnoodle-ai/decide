package openai

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
	file := append(mediatest.PNG(1, 1), make([]byte, 30<<20)...)
	req := decide.NewRequest(map[string]string{"store": "Acme"},
		decide.WithMedia(decide.Media{MIMEType: "image/png", Data: file}), decide.WithRequestExtra("store", false))
	decide.Ask(req, "ok", decide.Noul("Readable?"))
	req.Model = "gpt-6-luna"
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
