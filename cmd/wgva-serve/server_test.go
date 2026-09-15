// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/store"
	"github.com/mdhender/wgva/view"
)

const testSeed = wgva.Seed(0x0123456789abcdef)

func seedPath() string { return "/seed/" + testSeed.String() }

// newDiagnostic starts the viewer in the mode that has no world.
func newDiagnostic(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	s, err := newDiagnosticServer(testSeed, wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("newDiagnosticServer: %v", err)
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	t.Cleanup(s.close)
	return s, ts
}

// newWorld creates a world file and starts the viewer over it.
func newWorld(t *testing.T) (string, *server, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "world.wgva")
	created, err := store.Create(t.Context(), path, testSeed, wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.Close()

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s, err := newWorldServer(opened)
	if err != nil {
		t.Fatalf("newWorldServer: %v", err)
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	t.Cleanup(s.close)
	return path, s, ts
}

func get(t *testing.T, ts *httptest.Server, path string) (*http.Response, []byte) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp, body
}

// TestServerDrawsWhatTheRendererDraws is the viewer's half of the exit
// condition's last clause: the CLI and both web front ends agree byte for byte.
//
// It is one assertion against render rather than a second set of goldens — the
// golden image in render already pins what the renderer draws, and a second copy
// of it in this package would only pin it twice. What is worth asserting is that
// this front end puts nothing of its own between a window and the pixels. The
// CLI's half of the same claim is in cmd/wgva-map.
func TestServerDrawsWhatTheRendererDraws(t *testing.T) {
	_, ts := newDiagnostic(t)

	const query = "q=-200&r=-150&cols=41&rows=31&hex-radius=8&layer=terrain"
	resp, body := get(t, ts, seedPath()+"/map.png?"+query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	v, err := view.Parse(view.ViewerDefaults(testSeed), mustQuery(t, query))
	if err != nil {
		t.Fatal(err)
	}
	vp, err := v.Viewport()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := v.RenderLayer()
	if err != nil {
		t.Fatal(err)
	}
	want, err := render.Render(wgva.NewDefault(testSeed), vp, layer, v.HexRadius)
	if err != nil {
		t.Fatal(err)
	}

	if !samePixels(decode(t, body), want) {
		t.Error("the viewer's image is not the renderer's image for the same window")
	}
}

// TestLinksRoundTrip is what makes the viewer stateless rather than nearly
// stateless: every state it can be in is a URL, so every link the page emits
// must parse back to the state that produced it.
func TestLinksRoundTrip(t *testing.T) {
	s, _ := newDiagnostic(t)

	start, err := view.Parse(view.ViewerDefaults(testSeed), mustQuery(t, "q=17&r=-43&cols=41&rows=31&layer=climate&hex-radius=6&turn=2"))
	if err != nil {
		t.Fatal(err)
	}
	vp, _ := start.Viewport()
	layer, _ := start.RenderLayer()
	p := s.newPage(request{seed: testSeed, view: start}, vp, layer)

	var all []link
	all = append(all, p.Layers...)
	all = append(all, p.Scroll...)
	all = append(all, p.Size...)
	all = append(all, p.Zoom...)
	all = append(all, p.Turns...)
	if len(all) < 20 {
		t.Fatalf("the page emitted %d links; this test is asserting almost nothing", len(all))
	}

	for _, l := range all {
		u, err := url.Parse(l.URL)
		if err != nil {
			t.Errorf("%s: %v", l.Label, err)
			continue
		}
		if u.Path != seedPath() {
			t.Errorf("%s points at %q, not at this world", l.Label, u.Path)
		}
		got, err := view.Parse(view.ViewerDefaults(testSeed), u.Query())
		if err != nil {
			t.Errorf("%s does not parse back: %v", l.Label, err)
			continue
		}
		// The parsed view must render the same URL it came from. That is the
		// round trip: a link that parsed to something whose own link differed
		// would mean the page and the parser disagreed about the state.
		if got.Query() != u.RawQuery {
			t.Errorf("%s round trips to a different window:\n got %s\nwant %s", l.Label, got.Query(), u.RawQuery)
		}
	}
}

// TestScrollStepIsTheGrammarsNotTheServers is the mistake DESIGN.md 29.3 records
// having been made: a front end that computes its own scroll distance has a
// second opinion about the grammar, and the first one that did it got both
// halves wrong.
//
// North and south move a third of the rows, the four diagonals a third of the
// columns, and a step followed by its opposite returns to exactly where it
// started.
func TestScrollStepIsTheGrammarsNotTheServers(t *testing.T) {
	s, _ := newDiagnostic(t)

	start, err := view.Parse(view.ViewerDefaults(testSeed), mustQuery(t, "q=17&r=-43&cols=61&rows=45"))
	if err != nil {
		t.Fatal(err)
	}
	vp, _ := start.Viewport()
	layer, _ := start.RenderLayer()
	p := s.newPage(request{seed: testSeed, view: start}, vp, layer)

	moved := map[string]view.View{}
	for _, l := range p.Scroll {
		u, _ := url.Parse(l.URL)
		v, err := view.Parse(view.ViewerDefaults(testSeed), u.Query())
		if err != nil {
			t.Fatal(err)
		}
		moved[l.Label] = v
	}

	// A third of the rows north, a third of the columns on the diagonals. The
	// numbers are the grammar's and are asserted there; what is asserted here is
	// that this server used them.
	if moved["N"].Q != start.Q || moved["N"].R != start.R-int64(start.ScrollStep("n")) {
		t.Errorf("N moved to (%d, %d) from (%d, %d)", moved["N"].Q, moved["N"].R, start.Q, start.R)
	}
	if start.ScrollStep("n") != start.Rows/3 || start.ScrollStep("ne") != start.Cols/3 {
		t.Errorf("the step is not a third of the window: %d, %d", start.ScrollStep("n"), start.ScrollStep("ne"))
	}

	// There and back again, exactly.
	for a, b := range map[string]string{"N": "S", "NE": "SW", "SE": "NW"} {
		there := start.Scrolled(a, start.ScrollStep(a))
		back := there.Scrolled(b, there.ScrollStep(b))
		if back.Q != start.Q || back.R != start.R {
			t.Errorf("%s then %s landed on (%d, %d), not (%d, %d)", a, b, back.Q, back.R, start.Q, start.R)
		}
	}
}

// TestDiagnosticModeServesEverySeedAndSaysSo is the mode without --db: the
// generator is built in memory from the seed in the route, so every seed is
// servable and the page says the output does not represent a saved world.
func TestDiagnosticModeServesEverySeedAndSaysSo(t *testing.T) {
	_, ts := newDiagnostic(t)

	resp, body := get(t, ts, seedPath())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Diagnostic") {
		t.Error("the diagnostic page does not say it is diagnostic")
	}
	if !strings.Contains(string(body), "wgva-world create") {
		t.Error("the diagnostic page does not name the command that makes a world")
	}
}

// TestWorldModeChecksTheSeedInTheRoute is the other mode. The database supplies
// the seed, so the seed in the route is a check against the stored one rather
// than the source of it, and another seed is a 404 naming the one this server
// holds — a link to a world that is not this one is a broken link rather than a
// different world.
func TestWorldModeChecksTheSeedInTheRoute(t *testing.T) {
	_, _, ts := newWorld(t)

	resp, body := get(t, ts, seedPath())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "Diagnostic") {
		t.Error("a world-backed page calls itself diagnostic")
	}

	resp, body = get(t, ts, "/seed/0x1111111111111111")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another seed gave %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(string(body), testSeed.String()) {
		t.Errorf("the 404 does not name the seed this server holds: %s", body)
	}
}

// TestWorldBackedImageCarriesNoTag is DESIGN.md 29.3, and the reason is worth
// keeping in front of a reader: a world-backed image depends on the overlays as
// well as on the palette, and overlays are mutable player state with no version
// anywhere in the system. A tag that ignored them would go on serving an
// unexplored map after the player explored it.
func TestWorldBackedImageCarriesNoTag(t *testing.T) {
	path, _, ts := newWorld(t)

	resp, first := get(t, ts, seedPath()+"/map.png?cols=21&rows=15&hex-radius=4&layer=terrain")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if tag := resp.Header.Get("ETag"); tag != "" {
		t.Errorf("a world-backed image carries ETag %q", tag)
	}

	// And the overlays are read fresh on every request, so exploring a world and
	// refreshing shows the exploration.
	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDiscovered(t.Context(), wgva.Origin); err != nil {
		t.Fatal(err)
	}
	s.Close()

	_, second := get(t, ts, seedPath()+"/map.png?cols=21&rows=15&hex-radius=4&layer=terrain")
	if samePixels(decode(t, first), decode(t, second)) {
		t.Error("exploring the world and refreshing did not change the picture")
	}
}

// TestDiagnosticImageCarriesAStrongTag is the other half. The PNG is a pure
// function of the seed, the window, the layer, the algorithm version, the render
// version, and the configuration fingerprint, so it carries a strong tag built
// from exactly those.
func TestDiagnosticImageCarriesAStrongTag(t *testing.T) {
	_, ts := newDiagnostic(t)
	const path = "/map.png?cols=21&rows=15&hex-radius=4&layer=terrain"

	resp, _ := get(t, ts, seedPath()+path)
	tag := resp.Header.Get("ETag")
	if tag == "" || strings.HasPrefix(tag, "W/") {
		t.Fatalf("ETag = %q, want a strong tag", tag)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+seedPath()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", tag)
	again, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("a matching If-None-Match gave %d, want 304", again.StatusCode)
	}
}

// TestPageVersionIsInThePageTagAndNotTheImageTag is why PageVersion exists. The
// page is a second representation of the same state, and a release that changed
// only the markup would otherwise emit the same strong tag for different bytes —
// which is the one thing a strong validator exists to prevent. It is deliberately
// absent from the image tag, so changing the markup does not invalidate a cached
// PNG.
func TestPageVersionIsInThePageTagAndNotTheImageTag(t *testing.T) {
	s, _ := newDiagnostic(t)
	v, err := view.Parse(view.ViewerDefaults(testSeed), mustQuery(t, "cols=21&rows=15"))
	if err != nil {
		t.Fatal(err)
	}
	req := request{seed: testSeed, view: v}

	page, img := s.pageTag(req), s.imageTag(req)
	if page == img {
		t.Fatal("the page and the image carry the same tag")
	}
	if !strings.Contains(page, "p1-") {
		t.Errorf("the page tag does not carry PageVersion: %s", page)
	}
	if strings.Contains(img, "p1-") {
		t.Errorf("the image tag carries PageVersion: %s", img)
	}
	// Both carry what they are actually a function of.
	for _, tag := range []string{page, img} {
		if !strings.Contains(tag, s.digest.Tag()) {
			t.Errorf("%s does not carry the configuration fingerprint", tag)
		}
	}
}

// TestPageNamesTheTileAtItsCentre is DESIGN.md 29.3: a link to a window is
// otherwise a link to a picture, and a reader has to count swatches against the
// key to find out what they are looking at.
func TestPageNamesTheTileAtItsCentre(t *testing.T) {
	_, ts := newDiagnostic(t)

	_, body := get(t, ts, seedPath()+"?q=17&r=-43")
	text := string(body)

	tile := wgva.NewDefault(testSeed).Tile(wgva.NewCoord(17, -43))
	for _, want := range []string{
		tile.Terrain.String(),
		tile.Elevation.String(),
		tile.Climate.Heat.String(),
		tile.Climate.Moisture.String(),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the page does not name the centre tile's %q", want)
		}
	}
	// And the scalars the classifications were made from. A band without the
	// number behind it cannot answer "how close to the next band is it?".
	if !strings.Contains(text, "relief") {
		t.Error("the page does not carry the scalars the bands were classified from")
	}
}

// TestServeNeverCreatesAWorld is the promise every tool but `wgva-world create`
// makes: an absent or empty file is a refusal, not an invitation.
func TestServeNeverCreatesAWorld(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.wgva")
	_, err := newServer(absent, "")
	if !errors.Is(err, store.ErrNoWorld) {
		t.Fatalf("newServer: %v, want ErrNoWorld", err)
	}
	if !strings.Contains(err.Error(), "wgva-world create") {
		t.Errorf("the refusal does not name the command that makes one: %v", err)
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused start created %s", absent)
	}
}

// TestAWorldThatFailsAGateIsAServerThatDoesNotStart is why the world is opened
// before the port is bound. The alternative is a server that answers every
// request with a 500.
func TestAWorldThatFailsAGateIsAServerThatDoesNotStart(t *testing.T) {
	path, _, _ := newWorld(t)

	// A second store over the same file, mutated to fail gate 5.
	broken := filepath.Join(t.TempDir(), "broken.wgva")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, data, 0o644); err != nil {
		t.Fatal(err)
	}
	mutateRadius(t, broken)

	if _, err := newServer(broken, ""); !errors.Is(err, store.ErrWrongWorldRadius) {
		t.Fatalf("newServer over a world with the wrong radius: %v", err)
	}
}

// TestMalformedWindowIsA400 is the refusal model: nothing a caller can type is
// the process's fault.
func TestMalformedWindowIsA400(t *testing.T) {
	_, ts := newDiagnostic(t)
	for _, query := range []string{"?q=nonsense", "?layer=marzipan", "?r=99999999"} {
		for _, path := range []string{seedPath(), seedPath() + "/map.png"} {
			resp, _ := get(t, ts, path+query)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("GET %s%s gave %d, want 400", path, query, resp.StatusCode)
			}
		}
	}
	resp, _ := get(t, ts, "/seed/not-a-seed")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a malformed seed gave %d, want 400", resp.StatusCode)
	}
}

// TestRootRedirectsToTheWorld: there is nothing to choose at the root, because
// one process serves one seed.
func TestRootRedirectsToTheWorld(t *testing.T) {
	_, ts := newDiagnostic(t)
	client := *ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != seedPath() {
		t.Errorf("Location = %q, want %q", got, seedPath())
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// decode reads a PNG back. Golden comparison is over decoded RGBA rather than
// file bytes, because image/png's filter and compression choices can change
// between Go releases and what is being asserted is the picture.
func decode(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	out := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func samePixels(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

// mutateRadius rewrites a world's stored radius, to build the file gate 5
// exists to refuse. It uses the sqlite3 shell rather than the store, because the
// store's whole job is to refuse this.
func mutateRadius(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite|sqlite.OpenURI)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer conn.Close()
	if err := sqlitex.ExecuteScript(conn, "UPDATE world SET world_radius = 131071;", nil); err != nil {
		t.Fatalf("mutating %s: %v", path, err)
	}
}
