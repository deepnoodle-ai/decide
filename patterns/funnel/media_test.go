package funnel_test

import (
	"context"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
	"github.com/deepnoodle-ai/decide/patterns/funnel"
)

func TestSharedCarriesMedia(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.AcceptMedia())
	photo := decide.Media{MIMEType: "image/png", Data: mediatest.PNG(4, 4)}
	ask := funnel.Shared("A photo of a desk.",
		func(it funnel.Item[string]) (map[string]decide.Question, error) {
			return map[string]decide.Question{"shows": decide.Noul("Does the photo show a " + it.Value + "?")}, nil
		}, 2, decide.WithMedia(photo))
	if _, err := funnel.Run(context.Background(), srv.Client(t), []string{"lamp", "mug", "phone"}, 1,
		funnel.Stage[string]{Name: "screen", Ask: ask}); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	for i, r := range reqs {
		if len(r.Request.Media) != 1 || r.Request.Media[0].MIMEType != "image/png" {
			t.Errorf("request %d media = %v", i, r.Request.Media)
		}
	}
}
