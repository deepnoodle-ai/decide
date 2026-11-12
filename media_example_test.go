package decide_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// Attach a photo to a question. The same call works on every provider
// that reads images; a provider that cannot read the file returns
// errors.ErrUnsupported before sending anything.
func ExampleWithMedia() {
	var photo bytes.Buffer // in production: os.ReadFile("receipt.png")
	_ = png.Encode(&photo, image.NewGray(image.Rect(0, 0, 64, 64)))
	receipt := decide.Media{MIMEType: "image/png", Data: photo.Bytes()}

	// AcceptMedia stands in for a provider that reads images, such as
	// backend.Gemini.
	srv, err := decidetest.Start(decidetest.AcceptMedia())
	if err != nil {
		panic(err)
	}
	defer srv.Close()
	client, err := srv.NewClient()
	if err != nil {
		panic(err)
	}
	srv.Answer("eval", decidetest.NoulAnswer(.88))

	e, err := decide.Eval(context.Background(), client, "A photo of a store receipt.",
		decide.Noul("Can you read every line item and the total?"),
		decide.WithMedia(receipt))
	if errors.Is(err, errors.ErrUnsupported) {
		fmt.Println("this provider cannot read the photo:", err)
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(e.Answer.Noul, srv.Requests()[0].Request.Media[0].MIMEType)
	// Output: 0.88 image/png
}
