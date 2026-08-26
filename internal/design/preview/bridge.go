package preview

import (
	"bytes"
	_ "embed"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed bridge.js
var bridgeScript []byte

// bridgeTag is what gets spliced into a ?bridge=1 document. It is loaded from
// the token-free BridgePath so one cached copy serves every artifact, and it is
// deferred so the document has parsed before the bridge indexes it.
const bridgeTag = "\n<script src=\"" + BridgePath + "\" defer></script>\n"

// serveBridge returns the selection bridge. It carries no artifact content, so
// it needs no token; it is still same-origin, which is what the preview CSP
// requires of a script source.
func (s *Server) serveBridge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	s.writeSecurityHeaders(w)
	http.ServeContent(w, r, "_bridge.js", time.Time{}, bytes.NewReader(bridgeScript))
}

// injectBridge splices the preamble and the bridge tag into a document. It
// inserts before </body> when there is one, and appends otherwise: the artifact
// is the user's file and a preview must never rewrite the rest of it. The
// splice happens in the response only — nothing is written back to disk.
func injectBridge(body, preamble []byte) []byte {
	var addition []byte
	if len(preamble) > 0 {
		addition = append(addition, "\n<script>"...)
		addition = append(addition, preamble...)
		addition = append(addition, "</script>"...)
	}
	addition = append(addition, bridgeTag...)
	return injectHTML(body, addition)
}

func injectLiveReload(body []byte, livePath string) []byte {
	return injectHTML(body, []byte("\n<script>"+liveReloadScript(livePath)+"</script>\n"))
}

func liveReloadScript(livePath string) string {
	return `(function(){var livePath=` + strconv.Quote(livePath) + `;var revision=null;async function poll(){try{var response=await fetch(livePath,{cache:'no-store'});if(!response.ok){return}var next=(await response.text()).trim();if(revision===null){revision=next;return}if(next!==revision){window.location.reload();}}catch(_){}}function start(){poll();window.setInterval(poll,1500);}if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',start,{once:true});}else{start();}})();`
}

func injectHTML(body, addition []byte) []byte {
	lower := strings.ToLower(string(body))
	for _, marker := range []string{"</body>", "</html>"} {
		if idx := strings.LastIndex(lower, marker); idx >= 0 {
			out := make([]byte, 0, len(body)+len(addition))
			out = append(out, body[:idx]...)
			out = append(out, addition...)
			out = append(out, body[idx:]...)
			return out
		}
	}
	return append(append([]byte{}, body...), addition...)
}
