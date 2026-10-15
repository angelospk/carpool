package api

import (
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The public face of the hostname: a page at / that says what this is, and
// the Expo web build under /app for anybody who cannot or will not install the
// APK — every iPhone, to begin with.

// landingPage is plain for the same reason the route page is: no scripts, no
// fonts, nothing fetched from anywhere, so the CSP can forbid all of it.
var landingPage = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="el"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="Κοινές καθημερινές διαδρομές στη Θράκη. Ο οδηγός μοιράζεται τα έξοδα της βενζίνης, δεν πουλάει διαδρομή.">
<meta property="og:title" content="carpool · κοινές διαδρομές στη Θράκη">
<meta property="og:description" content="Βρες θέση σε αυτοκίνητο που πάει ήδη προς τα εκεί, ή μοιράσου τα έξοδα της δικής σου διαδρομής.">
<title>carpool · κοινές διαδρομές στη Θράκη</title>
<style>
*{box-sizing:border-box}
body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;background:#f1efea;color:#1d2521;margin:0;line-height:1.5}
main{max-width:34rem;margin:auto;padding:24px 16px 40px}
.brand{display:flex;align-items:center;gap:12px;margin-bottom:28px}
.mark{width:44px;height:44px;border-radius:12px;background:#2d4a3e;display:grid;place-items:center}
.brand b{font-size:1.15rem;color:#2d4a3e}
h1{font-size:1.9rem;line-height:1.15;margin:0 0 12px;letter-spacing:-.01em}
.lede{font-size:1.1rem;color:#4b524e;margin:0 0 24px}
.actions{display:grid;gap:10px;margin-bottom:32px}
a.button{display:block;text-align:center;background:#2d4a3e;color:#fff;border-radius:999px;padding:14px 16px;text-decoration:none;font-weight:600}
a.quiet{background:#fff;color:#1d2521;border:1px solid #dcd8cf}
.note{font-size:.9rem;color:#6b716d;margin:-20px 0 32px}
section{background:#fff;border-radius:16px;padding:20px;margin-bottom:12px}
h2{font-size:1.05rem;margin:0 0 10px}
ol{margin:0;padding-left:1.2rem}li{margin:6px 0}
p{margin:8px 0}
.muted{color:#6b716d;font-size:.9rem}
footer{margin-top:24px;font-size:.85rem;color:#6b716d}
footer a{color:#2d4a3e}
</style></head><body><main>
<div class="brand"><div class="mark"><svg width="24" height="24" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 18c4 0 3-7 7-7s3-6 7-6" stroke="#fff" stroke-width="2.4" fill="none" stroke-linecap="round"/><circle cx="5" cy="18" r="2.2" fill="#fff"/><circle cx="19" cy="5" r="2.2" fill="#e8873a"/></svg></div><b>carpool</b></div>

<h1>Κοινές καθημερινές διαδρομές στη Θράκη</h1>
<p class="lede">Αν πηγαίνεις κάθε εβδομάδα Αλεξανδρούπολη, Κομοτηνή, Ξάνθη ή Ορεστιάδα, κάποιος άλλος κάνει ήδη τον ίδιο δρόμο. Εδώ βρίσκεστε, και μοιράζεστε τα έξοδα της βενζίνης.</p>

<div class="actions">
{{if .WebApp}}<a class="button" href="/app/">Δοκίμασέ το στον browser</a>{{end}}
{{if .InstallURL}}<a class="button{{if .WebApp}} quiet{{end}}" href="{{.InstallURL}}">Κατέβασε την εφαρμογή για Android</a>{{end}}
</div>
{{if .WebApp}}<p class="note">Στο iPhone δουλεύει από τον browser. Δεν χρειάζεται εγκατάσταση.</p>{{end}}

<section>
<h2>Πώς δουλεύει</h2>
<ol>
<li><b>Ο οδηγός</b> δημοσιεύει τη διαδρομή που κάνει έτσι κι αλλιώς: από πού, προς τα πού, ποιες μέρες, τι ώρα.</li>
<li><b>Ο επιβάτης</b> ζητά θέση για μια μέρα και μια ώρα. Ρωτάμε έναν έναν τους οδηγούς που περνούν από εκεί.</li>
<li>Όταν κάποιος δεχτεί, συνεννοείστε στη συνομιλία του ταξιδιού. Μετά το ταξίδι, επιβεβαιώνετε και βαθμολογείτε ο ένας τον άλλον.</li>
</ol>
</section>

<section>
<h2>Μοιρασμένα έξοδα, όχι εισιτήριο</h2>
<p>Η εφαρμογή προτείνει ένα ποσό από τη βενζίνη και τα διόδια του κομματιού που ταξιδεύεις. Δεν πληρώνεις μέσα από την εφαρμογή και κανείς δεν βγάζει κέρδος.</p>
</section>

<section>
<h2>Για φοιτητές και όσους πηγαινοέρχονται</h2>
<p>Φτιαγμένο για τις διαδρομές ανάμεσα στις πόλεις της Θράκης, π.χ. ανάμεσα στις σχολές του Δημοκρίτειου. Μια σχολή, ένα γραφείο ή ένα νοσοκομείο μπορεί να φτιάξει δική της ομάδα, ώστε οι διαδρομές να φαίνονται μόνο στα μέλη της.</p>
<p class="muted">Πιλοτική λειτουργία: χρειάζεται κωδικός πρόσκλησης, που τον δίνει όποιος σε κάλεσε. Στην αρχή μπορεί να μη βρεθεί οδηγός αμέσως.</p>
</section>

<footer>Ανοιχτός κώδικας · <a href="https://github.com/angelospk/carpool">github.com/angelospk/carpool</a></footer>
</main></body></html>`))

// handleLanding is the page at the root of the hostname.
func (d Deps) handleLanding(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Cache-Control", "public, max-age=300")
	if err := landingPage.Execute(w, map[string]any{
		"WebApp":     d.WebDir != "",
		"InstallURL": d.safeInstallURL(),
	}); err != nil {
		// The status line is already gone; nothing better to do than stop.
		return
	}
}

// webAppCSP allows exactly what the Expo web build does: its own scripts, the
// inline styles react-native-web writes, images from here or picked from the
// device, and requests and sockets back to this API.
const webAppCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'"

// webApp serves the static export in dir under /app/. Paths the app's router
// owns get index.html; a path that names a file (it has an extension) gets the
// file or a 404, never HTML pretending to be a script.
func webApp(dir string) http.HandlerFunc {
	root := filepath.Clean(dir)
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", webAppCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")

		// path.Clean on a rooted path cannot climb above the root, so the
		// joined name stays inside dir whatever the request said.
		rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/app")), "/")
		if rel != "" {
			name := filepath.Join(root, filepath.FromSlash(rel))
			if info, err := os.Stat(name); err == nil && !info.IsDir() {
				if strings.HasPrefix(rel, "_expo/static/") || strings.HasPrefix(rel, "assets/") {
					// Content-hashed by the export: a new build is a new name.
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				http.ServeFile(w, r, name)
				return
			}
			if path.Ext(rel) != "" {
				writeError(w, http.StatusNotFound, "not_found", "the requested resource does not exist")
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	}
}

// favicon serves the web build's icon at the root of the hostname. A day of
// cache: the name never changes, so a new icon has to be able to arrive.
func favicon(dir string) http.HandlerFunc {
	name := filepath.Join(filepath.Clean(dir), "favicon.ico")
	return func(w http.ResponseWriter, r *http.Request) {
		if info, err := os.Stat(name); err != nil || info.IsDir() {
			writeError(w, http.StatusNotFound, "not_found", "the requested resource does not exist")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, name)
	}
}
