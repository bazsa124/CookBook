package api

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"rsc.io/qr"

	"cookbook/internal/netbind"
)

// Pairing without typing a 48-character token on a phone keyboard.
//
// /pair shows a QR code, but only to a browser on the server machine itself
// (loopback): whoever can see it could already read the token file. The QR
// holds http://<tailscale-ip>:port/#pair=<token>. The token rides in the URL
// fragment, which browsers never send to the server, so it does not end up in
// any log. The landing page reads it with JavaScript and offers a
// cookbook://pair link that opens the app with server and token filled in.

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// pairURL is what the phone should open: the tailnet address, not localhost.
func (s *Server) pairURL(r *http.Request) string {
	host := ""
	for _, ip := range netbind.TailscaleIPs() {
		if ip.To4() != nil {
			host = ip.String()
			break
		}
	}
	if host == "" {
		host, _, _ = net.SplitHostPort(r.Host)
	}
	return fmt.Sprintf("http://%s/#pair=%s", net.JoinHostPort(host, strconv.Itoa(s.Port)), s.Token)
}

func (s *Server) apkPath() string { return filepath.Join(s.Store.Root, "cookbook.apk") }

func (s *Server) apk(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(s.apkPath()); err != nil {
		http.Error(w, "No APK published. Copy app-debug.apk to "+s.apkPath(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="cookbook.apk"`)
	http.ServeFile(w, r, s.apkPath())
}

func (s *Server) pairQR(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		http.Error(w, "pairing is only shown on the server machine", http.StatusForbidden)
		return
	}
	code, err := qr.Encode(s.pairURL(r), qr.M)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code.Scale = 8
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(code.PNG())
}

var pairTmpl = template.Must(template.New("pair").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>CookBook – pairing</title>
<style>body{font-family:system-ui,sans-serif;max-width:560px;margin:40px auto;padding:0 16px;color:#2b211d}
h1{color:#a4462a}code{background:#f3e6df;padding:2px 6px;border-radius:4px;word-break:break-all}
img{border:1px solid #ddd;border-radius:8px}</style></head><body>
<h1>Pair a phone</h1>
<ol>
<li>Scan this code with the phone's camera (Tailscale must be on).</li>
<li>If the CookBook app is not installed yet, download it on the page that opens, install it, then scan again.</li>
<li>Tap <b>Open in CookBook app</b>. Server and token are filled in automatically.</li>
</ol>
<img src="/pair.png" alt="pairing QR code" width="330">
<p>Manual setup: server <code>{{.Server}}</code>, token <code>{{.Token}}</code></p>
</body></html>`))

func (s *Server) pairPage(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		http.Error(w, "Open http://localhost:"+strconv.Itoa(s.Port)+"/pair on the server machine itself.", http.StatusForbidden)
		return
	}
	host := ""
	for _, ip := range netbind.TailscaleIPs() {
		if ip.To4() != nil {
			host = ip.String()
			break
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	pairTmpl.Execute(w, map[string]string{"Server": host, "Token": s.Token})
}

var landingTmpl = template.Must(template.New("landing").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>CookBook</title>
<style>body{font-family:system-ui,sans-serif;max-width:520px;margin:32px auto;padding:0 20px;color:#2b211d;line-height:1.5}
h1{color:#a4462a}a.btn{display:block;text-align:center;background:#a4462a;color:#fff;padding:14px;border-radius:28px;
text-decoration:none;font-weight:600;margin:12px 0}a.btn.alt{background:#f3e6df;color:#a4462a}.muted{color:#6b6158;font-size:14px}
[hidden]{display:none}</style></head><body>
<h1>CookBook</h1>
<p>This is the recipe server ({{.Recipes}} recipes). Recipes are viewed and edited in the Android app, not in the browser.</p>
<div id="pair" hidden>
  <a class="btn" id="open" href="#">Open in CookBook app</a>
  <p class="muted">Fills in this server and its token in the app.</p>
</div>
{{if .HasAPK}}<a class="btn alt" href="/app.apk">Download the Android app</a>
<p class="muted">After downloading, open the file and allow installing from this browser if Android asks.</p>{{end}}
<p class="muted" id="nopair">To connect the app, open <b>http://localhost:{{.Port}}/pair</b> on the server machine and scan the QR code with this phone.</p>
<script>
var m = location.hash.match(/pair=([0-9a-f]+)/);
if (m) {
  document.getElementById('pair').hidden = false;
  document.getElementById('nopair').hidden = true;
  document.getElementById('open').href = 'cookbook://pair?server=' + encodeURIComponent(location.host) + '&token=' + m[1];
}
</script>
</body></html>`))

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(s.apkPath())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	landingTmpl.Execute(w, map[string]any{"Recipes": s.Index.Count(), "HasAPK": err == nil, "Port": s.Port})
}
