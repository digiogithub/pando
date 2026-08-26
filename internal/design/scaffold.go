package design

import (
	"fmt"
	"html"
)

// placeholderEntry returns the minimal renderable entry document written when a
// caller creates an artifact without seed files. It exists so version 1 is
// always renderable; real scaffolds come from design skills (P7).
func placeholderEntry(kind Kind, title string) string {
	safeTitle := html.EscapeString(title)
	if kind == KindDeck {
		return fmt.Sprintf(deckPlaceholder, safeTitle, safeTitle)
	}
	return fmt.Sprintf(webPlaceholder, safeTitle, safeTitle)
}

const webPlaceholder = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; font: 16px/1.5 system-ui, sans-serif; }
  main { max-width: 60rem; margin: 0 auto; padding: 4rem 1.5rem; }
</style>
</head>
<body>
<main>
  <h1>%s</h1>
  <p>Empty design artifact. Ask the agent to build it.</p>
</main>
</body>
</html>
`

// deckPlaceholder ships print styles on purpose: PDF export prints one slide
// per page, which only works when the deck declares @page and page breaks.
const deckPlaceholder = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>%s</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; font: 16px/1.5 system-ui, sans-serif; }
  .slide {
    box-sizing: border-box;
    width: 100vw; height: 100vh;
    display: flex; flex-direction: column;
    justify-content: center; align-items: center;
    padding: 4rem;
  }
  @page { size: 1280px 720px; margin: 0; }
  @media print {
    .slide { width: 1280px; height: 720px; break-after: page; }
    .slide:last-child { break-after: auto; }
  }
</style>
</head>
<body>
<section class="slide" data-slide="0">
  <h1>%s</h1>
  <p>Empty deck. Ask the agent to build it.</p>
</section>
</body>
</html>
`
