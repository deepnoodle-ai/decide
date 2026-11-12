package cloudflare

import (
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
)

// One attempt allocates at most twice the files' size. Clef takes 8 MiB of
// images, the largest body it accepts.
func TestEncodeBodyAllocations(t *testing.T) {
	if mediatest.Race {
		t.Skip("the race detector changes allocation counts")
	}
	if testing.Short() {
		t.Skip("allocates tens of MiB")
	}
	file := append(mediatest.PNG(1, 1), make([]byte, 4<<20-200)...)
	req := decide.NewRequest("a receipt", decide.WithMedia(
		decide.Media{MIMEType: "image/png", Data: file}, decide.Media{MIMEType: "image/png", Data: file}))
	decide.Ask(req, "ok", decide.Noul("Readable?"))
	req.Model = "clef"
	res := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := encodeBody(req); err != nil {
				b.Fatal(err)
			}
		}
	})
	size := int64(2 * len(file))
	t.Logf("%d bytes allocated for %d bytes of files (%.2fx)", res.AllocedBytesPerOp(), size, float64(res.AllocedBytesPerOp())/float64(size))
	if got := res.AllocedBytesPerOp(); got > 2*size {
		t.Fatalf("one body allocates %d bytes for %d bytes of files; want at most %d", got, size, 2*size)
	}
}
