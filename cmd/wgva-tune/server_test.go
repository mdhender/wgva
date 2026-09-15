// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"html"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/view"
)

const testSeed = wgva.Seed(0x0123456789abcdef)

func newTestServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	s := newServer(testSeed, wgva.DefaultConfig())
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts
}

// noRedirect is a client that reports a redirect rather than following it, so a
// test can assert on the Location a POST sends a browser to.
func noRedirect(ts *httptest.Server) *http.Client {
	c := ts.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func get(t *testing.T, ts *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("GET %s: reading the body: %v", path, err)
	}
	return resp, body.String()
}

func seedPath() string { return "/seed/" + view.FormatSeed(testSeed) }

// firstLine keeps a failing test's output readable. A body that is not what the
// test expected is often a PNG, and a megabyte of it in the log buries the one
// line that says what went wrong.
func firstLine(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	if len(line) > 200 {
		line = line[:200] + "..."
	}
	return line
}

// TestRootRedirectsToASeed covers the one route that is not a tab.
func TestRootRedirectsToASeed(t *testing.T) {
	_, ts := newTestServer(t)

	for _, tc := range []struct{ query, want string }{
		{"", seedPath()},
		{"?tab=grid", seedPath() + "/grid"},
		{"?tab=config", seedPath() + "/config"},
		{"?seed=42", "/seed/" + view.FormatSeed(42)},
		{"?seed=42&tab=grid", "/seed/" + view.FormatSeed(42) + "/grid"},
	} {
		resp, err := noRedirect(ts).Get(ts.URL + "/" + tc.query)
		if err != nil {
			t.Fatalf("GET /%s: %v", tc.query, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("GET /%s returned %d, want a redirect", tc.query, resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); !strings.HasPrefix(got, tc.want) {
			t.Errorf("GET /%s redirected to %q, want a path beginning %q", tc.query, got, tc.want)
		}
	}
}

// TestTabsPrintTheIdentity asserts DESIGN.md 29.1's requirement that every tab
// carry the identity pair and label the configuration, conspicuously.
func TestTabsPrintTheIdentity(t *testing.T) {
	_, ts := newTestServer(t)

	d, err := config.Of(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Of: %v", err)
	}

	for _, path := range []string{seedPath(), seedPath() + "/grid", seedPath() + "/config"} {
		resp, body := get(t, ts, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s returned %d", path, resp.StatusCode)
		}
		for _, want := range []string{
			view.FormatSeed(testSeed),
			wgva.Version().String(),
			d.Short(),
			"this binary&#39;s defaults",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not print %q", path, want)
			}
		}
		// The concession the tool makes has to be on the page, not only in the
		// design document.
		if !strings.Contains(body, "not what it drew when the link was copied") {
			t.Errorf("GET %s does not say that a link shows what this process is drawing now", path)
		}
	}
}

// TestMapAndGridDrawPNGs is the exit condition of DESIGN.md section 32, phase 2:
// arbitrary canonical coordinates can be sampled and inspected in a browser, at
// hex scale and at one pixel per hex.
func TestMapAndGridDrawPNGs(t *testing.T) {
	_, ts := newTestServer(t)

	cases := map[string]string{
		"map at the origin":   seedPath() + "/map.png?q=0&r=0&cols=21&rows=15&hex-radius=6",
		"map a long way out":  seedPath() + "/map.png?q=-30000&r=12345&cols=11&rows=11&hex-radius=4&layer=detail",
		"map turned":          seedPath() + "/map.png?q=100&r=-200&turn=4&cols=11&rows=11",
		"grid":                seedPath() + "/grid.png?q=0&r=0&cols=101&rows=71&scale=2",
		"grid coarsely drawn": seedPath() + "/grid.png?q=0&r=0&cols=201&rows=151&scale=1&stride=64&layer=regional",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			resp, body := get(t, ts, path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s returned %d: %s", path, resp.StatusCode, firstLine(body))
			}
			if got := resp.Header.Get("Content-Type"); got != "image/png" {
				t.Fatalf("content type is %q, want image/png", got)
			}
			img, err := png.Decode(strings.NewReader(body))
			if err != nil {
				t.Fatalf("the response is not a PNG: %v", err)
			}
			if b := img.Bounds(); b.Dx() < 1 || b.Dy() < 1 {
				t.Fatalf("the image is %dx%d", b.Dx(), b.Dy())
			}
		})
	}
}

// TestEntityTagCarriesTheFingerprint pins the one departure from a tag that is a
// pure function of the URL. Move a field and every tag moves with it, so a
// browser holding an old image asks again.
func TestEntityTagCarriesTheFingerprint(t *testing.T) {
	s, ts := newTestServer(t)
	path := seedPath() + "/map.png?cols=11&rows=11&hex-radius=4"

	resp, _ := get(t, ts, path)
	first := resp.Header.Get("ETag")
	if first == "" {
		t.Fatal("no entity tag")
	}
	if strings.HasPrefix(first, "W/") {
		t.Fatalf("the tag %s is weak; it must be strong", first)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	req.Header.Set("If-None-Match", first)
	again, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	_ = again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("a conditional GET returned %d, want 304", again.StatusCode)
	}

	cfg := wgva.DefaultConfig()
	cfg.SeaLevel = 0.51
	if err := s.setConfig(cfg); err != nil {
		t.Fatalf("setConfig: %v", err)
	}
	moved, _ := get(t, ts, path)
	if moved.Header.Get("ETag") == first {
		t.Error("moving a configuration field did not move the entity tag")
	}
}

// TestRefusalsAre400 covers the rule that nothing a caller can type is the
// process's fault.
func TestRefusalsAre400(t *testing.T) {
	_, ts := newTestServer(t)

	cases := map[string]string{
		"a coordinate off the map":          seedPath() + "/map.png?q=99999",
		"a coordinate that is not a number": seedPath() + "/map.png?q=origin",
		"a layer that does not exist":       seedPath() + "/map.png?layer=rivers",
		"a seed that is not a number":       "/seed/not-a-seed",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			resp, body := get(t, ts, path)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("GET %s returned %d, want 400", path, resp.StatusCode)
			}
			if strings.TrimSpace(body) == "" {
				t.Error("the refusal has no message")
			}
		})
	}
}

// TestExpensiveWindowsAreDrawnNotRefused is the rule stated where somebody
// would look for the old refusal. The terrain tuning tool exists to be pointed
// at expensive windows; refusing one is refusing the tool's purpose.
func TestExpensiveWindowsAreDrawnNotRefused(t *testing.T) {
	_, ts := newTestServer(t)

	// The largest window the grammar allows, at the most expensive layer, drawn
	// at a stride so the test is not seven million tiles long.
	path := seedPath() + "/grid.png?cols=1001&rows=751&scale=1&stride=256&layer=terrain"
	resp, body := get(t, ts, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", path, resp.StatusCode, firstLine(body))
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type is %q, want image/png", got)
	}
}

// TestPagesPrintWhatAWindowCosts is what stands where the budget did: the number
// is reported rather than enforced.
func TestPagesPrintWhatAWindowCosts(t *testing.T) {
	_, ts := newTestServer(t)

	// 21 x 15 at the terrain layer is 315 tiles and 2205 evaluations.
	_, body := get(t, ts, seedPath()+"?cols=21&rows=15&layer=terrain")
	for _, want := range []string{"315 tiles", "2205 generator evaluations"} {
		if !strings.Contains(body, want) {
			t.Errorf("the map tab does not print %q", want)
		}
	}
	if strings.Contains(body, "budget") {
		t.Error("the map tab still mentions a budget")
	}

	// The same window on a one-evaluation layer costs a seventh of it, which is
	// the whole reason the unit is evaluations and not tiles.
	_, elevation := get(t, ts, seedPath()+"?cols=21&rows=15&layer=elevation")
	if !strings.Contains(elevation, "315 generator evaluations") {
		t.Error("the map tab does not price a one-evaluation layer at one a tile")
	}
}

// TestFormAppliesAndLabels covers the form, the label, and the fingerprint
// moving together.
func TestFormAppliesAndLabels(t *testing.T) {
	s, ts := newTestServer(t)

	form := url.Values{}
	for _, f := range config.Fields() {
		form.Set(f.Key, f.Get(wgva.DefaultConfig()))
	}
	form.Set("sea_level", "0.42")

	resp, err := noRedirect(ts).PostForm(ts.URL+seedPath()+"/config/fields", form)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST returned %d, want a redirect", resp.StatusCode)
	}

	if got := s.config().SeaLevel; got != 0.42 {
		t.Fatalf("sea level is %v, want 0.42", got)
	}
	if config.IsDefault(s.config()) {
		t.Error("the configuration is still labeled as this binary's defaults")
	}

	_, body := get(t, ts, seedPath()+"/config")
	if !strings.Contains(body, "modified") {
		t.Error("the configuration tab does not say the configuration is modified")
	}
}

// TestFormRefusesWhole asserts that a change which would make an unusable world
// changes nothing at all. Applying field by field would leave the tool drawing
// something that could not be written to a file.
func TestFormRefusesWhole(t *testing.T) {
	s, ts := newTestServer(t)
	before := s.config()

	form := url.Values{}
	for _, f := range config.Fields() {
		form.Set(f.Key, f.Get(before))
	}
	form.Set("sea_level", "0.4")
	form.Set("detail_octaves", "9") // below the tile grid's Nyquist wavelength

	resp, err := noRedirect(ts).PostForm(ts.URL+seedPath()+"/config/fields", form)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()

	if s.config() != before {
		t.Fatal("a refused form still changed the configuration")
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "failure=") {
		t.Errorf("the redirect does not carry a failure: %q", got)
	}
}

// TestUploadAndReset covers adopting a file and going back to the defaults.
func TestUploadAndReset(t *testing.T) {
	s, ts := newTestServer(t)

	cfg := wgva.DefaultConfig()
	cfg.SeaLevel = 0.375
	cfg.Local.WavelengthMiles = 96
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "config.toml")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing the part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}

	resp, err := noRedirect(ts).Post(ts.URL+seedPath()+"/config/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if s.config() != cfg {
		t.Fatalf("the uploaded configuration was not adopted:\ngot  %+v\nwant %+v", s.config(), cfg)
	}

	resp, err = noRedirect(ts).PostForm(ts.URL+seedPath()+"/config/reset", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if !config.IsDefault(s.config()) {
		t.Error("reset did not go back to this binary's defaults")
	}
}

// TestUploadRefusesAMissingKey is the tool's half of DESIGN.md 21.1. A pasted
// file with a key removed must be refused, naming it.
func TestUploadRefusesAMissingKey(t *testing.T) {
	s, ts := newTestServer(t)
	before := s.config()

	data, err := config.Marshal(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var kept []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "sea_level =") {
			continue
		}
		kept = append(kept, line)
	}

	resp, err := noRedirect(ts).PostForm(ts.URL+seedPath()+"/config/upload",
		url.Values{"contents": {strings.Join(kept, "\n")}})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()

	if s.config() != before {
		t.Fatal("a file with a missing key was adopted")
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "sea_level") {
		t.Errorf("the refusal does not name the missing key: %q", got)
	}
}

// TestDownloadRoundTrips is what makes the tool's statefulness survivable: a
// picture can always be traced to the configuration that produced it and
// reproduced outside the tool.
func TestDownloadRoundTrips(t *testing.T) {
	s, ts := newTestServer(t)

	cfg := wgva.DefaultConfig()
	cfg.SeaLevel = 0.1 + 0.2
	cfg.Warp.Gain = 1.0 / 3.0
	if err := s.setConfig(cfg); err != nil {
		t.Fatalf("setConfig: %v", err)
	}

	resp, body := get(t, ts, "/config.toml")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /config.toml returned %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "config.toml") {
		t.Errorf("the download is not named: %q", got)
	}

	back, err := config.Unmarshal([]byte(body))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back != cfg {
		t.Errorf("the download did not round-trip:\ngot  %+v\nwant %+v", back, cfg)
	}
	before, _ := config.Of(cfg)
	after, _ := config.Of(back)
	if before != after {
		t.Errorf("the download changed the fingerprint: %s became %s", before, after)
	}
}

// TestGridWarnsAboutShearingTurns pins the warning DESIGN.md 29.1 asks the grid
// tab to carry, and its absence on the map tab.
func TestGridWarnsAboutShearingTurns(t *testing.T) {
	_, ts := newTestServer(t)

	for turn := range 6 {
		path := seedPath() + "/grid?turn=" + string(rune('0'+turn))
		_, body := get(t, ts, path)
		warned := strings.Contains(body, "shears this picture")
		want := turn != 0 && turn != 3
		if warned != want {
			t.Errorf("the grid tab at turn %d %s about shearing, want the other", turn, map[bool]string{true: "warns", false: "says nothing"}[warned])
		}
	}

	for turn := range 6 {
		_, body := get(t, ts, seedPath()+"?turn="+string(rune('0'+turn)))
		if strings.Contains(body, "shears this picture") {
			t.Errorf("the map tab warns about shearing at turn %d; it is undistorted at every turn", turn)
		}
	}
}

// TestPagesCarryTheCreateLine asserts the tool emits the whole command, because
// nobody types the identity pair.
func TestPagesCarryTheCreateLine(t *testing.T) {
	_, ts := newTestServer(t)
	d, err := config.Of(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	for _, path := range []string{seedPath(), seedPath() + "/grid", seedPath() + "/config"} {
		_, body := get(t, ts, path)
		for _, want := range []string{"wgva-world create", "--expect", d.String()} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not carry %q", path, want)
			}
		}
	}
}

// TestMapTabCountsTheWindow is the readout of DESIGN.md 29.1: the map tab says
// what is in the window it is drawing, every declared value is listed, and the
// grid tab shows none of it.
func TestMapTabCountsTheWindow(t *testing.T) {
	_, ts := newTestServer(t)

	_, body := get(t, ts, seedPath()+"?q=0&r=0&cols=21&rows=15")
	if !strings.Contains(body, "what is in this window") {
		t.Fatalf("the map tab carries no readout: %q", firstLine(body))
	}
	if !strings.Contains(body, "315 tiles") {
		t.Errorf("the readout does not name the number of tiles it counted")
	}
	// Every declared terrain, including the two DESIGN.md 17.1 emits nowhere. A
	// row reading zero is usually the row somebody is trying to move off zero.
	for _, want := range []string{"inland-sea", "lake", "plains", "coast"} {
		if !strings.Contains(body, ">"+want+"<") {
			t.Errorf("the readout does not list %q", want)
		}
	}

	_, grid := get(t, ts, seedPath()+"/grid?cols=21&rows=15")
	if strings.Contains(grid, "what is in this window") {
		t.Error("the grid tab carries a readout; a million tiles of readout is a second copy of the image's work")
	}
}

// TestMapPageDrawsALargeWindow covers the page that pays for the window twice —
// once for the image and once for the readout beside it — and is not refused for
// it either.
func TestMapPageDrawsALargeWindow(t *testing.T) {
	_, ts := newTestServer(t)
	resp, body := get(t, ts, seedPath()+"?cols=201&rows=151&layer=terrain")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET a large map page returned %d: %s", resp.StatusCode, firstLine(body))
	}
	if !strings.Contains(body, "what is in this window") {
		t.Error("the page carries no readout")
	}
}

// TestZoomLinksChangeThePicture is the regression test for a pair of controls
// that did nothing.
//
// The zoom links were built once for both tabs and stepped the hex radius, which
// only the map tab draws with — so on the grid tab they rewrote the address bar
// and left the image byte for byte identical. A control that does nothing is
// worse than an absent one: it teaches a person that the thing they wanted is
// not available.
func TestZoomLinksChangeThePicture(t *testing.T) {
	_, ts := newTestServer(t)

	for _, tc := range []struct {
		tab     string
		path    string
		changes string
	}{
		{"map", seedPath(), "hex-radius"},
		{"grid", seedPath() + "/grid", "stride"},
	} {
		t.Run(tc.tab, func(t *testing.T) {
			_, body := get(t, ts, tc.path+"?cols=21&rows=15&scale=2&stride=16&hex-radius=12")

			zoomed := zoomLinks(t, body)
			if len(zoomed) != 2 {
				t.Fatalf("the %s tab has %d zoom links, want 2", tc.tab, len(zoomed))
			}
			for _, href := range zoomed {
				u, err := url.Parse(html.UnescapeString(href))
				if err != nil {
					t.Fatalf("parsing %q: %v", href, err)
				}
				if got := u.Query().Get(tc.changes); got == currentOf(t, tc.changes) {
					t.Errorf("the %s tab's zoom leaves %s at %s", tc.tab, tc.changes, got)
				}
			}
		})
	}
}

// TestGridZoomRedrawsTheImage is the assertion the link-level test cannot make:
// following the grid tab's zoom link produces a different picture.
func TestGridZoomRedrawsTheImage(t *testing.T) {
	_, ts := newTestServer(t)

	const window = "cols=41&rows=31&scale=2&layer=terrain"
	_, before := get(t, ts, seedPath()+"/grid.png?"+window+"&stride=16")
	_, after := get(t, ts, seedPath()+"/grid.png?"+window+"&stride=8")

	if before == after {
		t.Fatal("halving the stride drew the same image; the grid's zoom does nothing")
	}
}

// zoomLinks pulls the hrefs of the two zoom controls out of a page.
func zoomLinks(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, label := range []string{">zoom in<", ">zoom out<"} {
		i := strings.Index(body, label)
		if i < 0 {
			continue
		}
		// Walk back to the href of the anchor this label closes.
		anchor := strings.LastIndex(body[:i], `href="`)
		if anchor < 0 {
			t.Fatalf("no href before %s", label)
		}
		rest := body[anchor+len(`href="`):]
		out = append(out, rest[:strings.Index(rest, `"`)])
	}
	return out
}

// currentOf is the value the test's window was requested with.
func currentOf(t *testing.T, param string) string {
	t.Helper()
	switch param {
	case "hex-radius":
		return "12"
	case "stride":
		return "16"
	default:
		t.Fatalf("no current value for %q", param)
		return ""
	}
}

// TestScrollLinksMoveAThirdOfThePicture is the bug the compass had: every point
// moved the same quarter of the *columns*, so a tall window scrolled north by a
// fraction of its width, and the grid tab — where a cell stands for Stride
// hexes — moved a sixty-fourth of what it was showing.
//
// The distances are the window grammar's and are pinned in view; what this
// asserts is that the page asks it, and asks the right one for the tab.
func TestScrollLinksMoveAThirdOfThePicture(t *testing.T) {
	_, ts := newTestServer(t)

	// The counts are chosen so that no two of the three candidate answers
	// coincide: a third of the rows is 200, a third of the columns is 333, and
	// the quarter of the columns this used to move is 250.
	const window = "cols=1001&rows=601&scale=2&stride=64&hex-radius=12&q=0&r=0"

	for _, tc := range []struct {
		tab   string
		path  string
		north int64 // rows/3, times the stride the tab samples at
		east  int64 // cols/3, likewise
	}{
		{"map", seedPath(), 200, 333},
		{"grid", seedPath() + "/grid", 200 * 64, 333 * 64},
	} {
		t.Run(tc.tab, func(t *testing.T) {
			_, body := get(t, ts, tc.path+"?"+window)

			// North is straight up the r axis, and northeast steps one column.
			// Both are read off the link rather than recomputed, because the
			// link is the thing that was wrong.
			if got := scrollTo(t, body, "N"); got.r != -tc.north || got.q != 0 {
				t.Errorf("N lands on (%d, %d), want (0, %d)", got.q, got.r, -tc.north)
			}
			if got := scrollTo(t, body, "NE"); got.q != tc.east {
				t.Errorf("NE lands on q=%d, want %d", got.q, tc.east)
			}
		})
	}
}

// scrollTo follows one compass link and returns the window centre it names.
func scrollTo(t *testing.T, body, point string) struct{ q, r int64 } {
	t.Helper()

	label := ">" + point + "</a>"
	i := strings.Index(body, label)
	if i < 0 {
		t.Fatalf("no %s scroll link on the page", point)
	}
	anchor := strings.LastIndex(body[:i], `href="`)
	if anchor < 0 {
		t.Fatalf("no href before the %s link", point)
	}
	rest := body[anchor+len(`href="`):]
	u, err := url.Parse(html.UnescapeString(rest[:strings.Index(rest, `"`)]))
	if err != nil {
		t.Fatalf("parsing the %s link: %v", point, err)
	}

	var out struct{ q, r int64 }
	if out.q, err = strconv.ParseInt(u.Query().Get("q"), 10, 64); err != nil {
		t.Fatalf("the %s link has q=%q", point, u.Query().Get("q"))
	}
	if out.r, err = strconv.ParseInt(u.Query().Get("r"), 10, 64); err != nil {
		t.Fatalf("the %s link has r=%q", point, u.Query().Get("r"))
	}
	return out
}
