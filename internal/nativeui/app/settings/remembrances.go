package settings

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const rdf = "settings.remembrances.decisionFilter."

func embProviders() []Opt {
	return []Opt{{"", "— select provider —"}, {"openai", "openai"}, {"openai-compatible", "openai-compatible"}, {"anthropic", "anthropic"}, {"ollama", "ollama"}}
}

func remembrancesDoc(s *Settings, rt *remTools) *Doc {
	filterOn := func(d *Doc) bool {
		return d.Bool("context_enrichment_decision_filter_enabled") || d.Bool("memory_context_decision_filter_enabled")
	}
	ctx := on("context_enrichment_enabled")
	loop := func(d *Doc) bool {
		return d.Bool("context_enrichment_enabled") && d.Bool("context_enrichment_agent_loop_enabled")
	}
	vis := func(f Field, v func(*Doc) bool) Field { f.Visible = v; return f }
	baseURL := func(pfx string) func(*Doc) bool {
		return func(d *Doc) bool {
			p := d.Get(pfx + "_embedding_provider")
			return p == "openai-compatible" || p == "ollama"
		}
	}
	notSame := func(d *Doc) bool { return !d.Bool("use_same_model") }
	d := newDoc(s, client.PathConfigServices,
		Group{Fields: []Field{sw("enabled", "Enabled", "Enable the Remembrances memory system")}},
		Group{Title: "KB filesystem sync", Fields: []Field{
			{Path: "kb_path", Label: "KB path", Desc: "Folder of the markdown knowledge base, relative to the working directory. Type it or browse for a folder, inside or outside the project.",
				Kind: KText, Placeholder: "./.kb", Width: 360,
				Control: func(c *ui.Context, d *Doc, v *string, w float32) ui.Element { return s.folderControl(c, v, w, "./.kb") }},
			sw("kb_watch", "Watch KB path", "Monitor markdown changes in real time and re-index automatically"),
			sw("kb_auto_import", "Auto import on startup", "Import markdown files from KB path when Pando starts"),
			sw("kb_convert_documents", "Convert documents", "Convert docx/pdf/xlsx and other rich documents to Markdown on the fly and index them, referencing the original file"),
			sw("kb_wiki_links", "Wiki links", "Index [[wiki links]] written in KB documents as a navigable graph: backlinks, related documents and concepts still undocumented"),
		}},
		Group{Title: "Document embeddings", Fields: []Field{
			rt.providerField("document", "Embedding provider", nil),
			vis(Field{Path: "document_embedding_base_url", Label: "Base URL", Kind: KText, Placeholder: "https://api.example.com/v1", Width: 320}, baseURL("document")),
			{Path: "document_embedding_api_key", Label: "Embedding API key", Kind: KSecret, Placeholder: "sk-…", Width: 320},
			rt.modelField("document", "Embedding model", nil),
			rt.testField("document", nil),
		}},
		Group{Title: "Code embeddings", Fields: []Field{
			sw("use_same_model", "Use same model as document", ""),
			rt.providerField("code", "Code embedding provider", notSame),
			vis(Field{Path: "code_embedding_base_url", Label: "Base URL", Kind: KText, Width: 320}, func(d *Doc) bool { return notSame(d) && baseURL("code")(d) }),
			vis(Field{Path: "code_embedding_api_key", Label: "Code embedding API key", Kind: KSecret, Placeholder: "sk-…", Width: 320}, notSame),
			rt.modelField("code", "Code embedding model", notSame),
			rt.testField("code", notSame),
		}},
		Group{Title: "Chunking", Fields: []Field{
			num("chunk_size", "Chunk size", ""), num("chunk_overlap", "Chunk overlap", ""), num("index_workers", "Index workers", ""),
		}},
		Group{Title: "Context enrichment", Fields: []Field{
			sw("context_enrichment_enabled", "Enable context enrichment", "Before each prompt, search KB and code index and prepend relevant context automatically"),
			vis(txt("context_enrichment_code_project", "Code project", "Select a previously indexed project to include code search results, or index the working directory."), ctx),
			vis(num("context_enrichment_kb_results", "KB results", ""), ctx),
			vis(num("context_enrichment_code_results", "Code results", ""), ctx),
			vis(num("context_enrichment_events_results", "Events results", ""), ctx),
			vis(num("context_enrichment_events_last_days", "Last days", ""), ctx),
			vis(txt("context_enrichment_events_subject", "Subject filter", ""), ctx),
			vis(sw("context_enrichment_agent_loop_enabled", "Agent loop enrichment", "Run enrichment as a separate agent loop on the context-enricher model. It searches memory, KB and the code index iteratively; the main agent only receives the resulting context block."), ctx),
			vis(num("context_enrichment_agent_loop_timeout_seconds", "Loop timeout (s)", ""), loop),
			vis(num("context_enrichment_agent_loop_max_chars", "Loop max chars", ""), loop),
			vis(sw("context_enrichment_agent_loop_every_message", "Run on every message", "Off (default): the loop runs once per session, on the first message. On: it runs on every user turn."), loop),
			vis(inv(sw("context_enrichment_agent_loop_silent", "Announce in chat", "Show start and end notices in the chat while the enrichment agent runs, like context compaction does")), loop),
			vis(inv(sw("context_enrichment_agent_loop_fallback_disabled", "Fallback to search", "Use the classic search pipeline when the loop fails, times out or finds nothing")), loop),
			vis(inv(sw("context_enrichment_agent_loop_hidden_in_chat", "Show loop in chat", "Record the loop as a child session of the chat session so its tool calls can be inspected")), loop),
			sw("context_enrichment_decision_filter_enabled", rdf+"enabled", rdf+"enabledHint"),
			sw("memory_context_decision_filter_enabled", rdf+"memoryEnabled", rdf+"memoryEnabledHint"),
			vis(flt("context_enrichment_decision_filter_threshold", rdf+"threshold", "Between 0 (exclusive) and 1"), filterOn),
			vis(num("context_enrichment_decision_filter_max_candidates", rdf+"maxCandidates", ""), filterOn),
			vis(num("context_enrichment_decision_filter_max_candidate_chars", rdf+"maxCandidateChars", ""), filterOn),
			vis(sw("context_enrichment_decision_filter_allow_hosted", rdf+"allowHosted", rdf+"allowHostedHint"), filterOn),
		}},
		Group{Title: "Memory system", Fields: []Field{
			sw("memory_enabled", "Memory enabled", "Enable the key-value memory subsystem (stored memories survive across sessions)"),
			sw("memory_context_enrichment_enabled", "Auto-inject in context", "Prepend relevant stored memories into the system prompt before each turn"),
			num("memory_context_max_items", "Context max items", ""), num("memory_context_max_chars", "Context max chars", ""),
			num("memory_default_ttl_days", "Default TTL (days)", "0 = no expiry"), txt("memory_gc_interval", "GC interval", "e.g. 1h"),
		}},
	)
	d.Sub, d.PutWhole = "remembrances", true
	d.SavedMsg = "Services settings saved"
	return d
}

// remTools holds the live helpers around the document: model lists, test
// results and the code projects table.
type remTools struct {
	s        *Settings
	models   map[string][]client.EmbeddingModelInfo // "document" / "code"
	modelErr map[string]string
	loadingM map[string]bool
	tests    map[string]*client.EmbeddingTestResult
	testing  map[string]bool
	projects []client.CodeProjectInfo
	indexing bool
	fetched  bool
}

func newRemembrances(s *Settings) section {
	rt := &remTools{s: s, models: map[string][]client.EmbeddingModelInfo{}, modelErr: map[string]string{},
		loadingM: map[string]bool{}, tests: map[string]*client.EmbeddingTestResult{}, testing: map[string]bool{}}
	d := remembrancesDoc(s, rt)
	d.Loaded = func(d *Doc, _ client.Obj) {
		rt.loadProjects(d)
		if d.Get("document_embedding_provider") != "" {
			rt.loadModels(d, "document")
		}
		if d.Get("code_embedding_provider") != "" {
			rt.loadModels(d, "code")
		}
	}
	d.Tail = rt.tail
	return &docPage{Doc: d, title: "settings.categories.remembrances", desc: "settings.remembrances.hero.tagline"}
}

func (rt *remTools) loadProjects(d *Doc) {
	a := rt.s.app
	a.Go(func(ctx context.Context) {
		p, err := a.Client.CodeProjects(ctx)
		a.Update(func() {
			if err != nil {
				p = nil
			}
			rt.projects = p
		})
	})
}

func (rt *remTools) loadModels(d *Doc, kind string) {
	provider := d.Get(kind + "_embedding_provider")
	if provider == "" || rt.loadingM[kind] {
		return
	}
	base, key := d.Get(kind+"_embedding_base_url"), d.Get(kind+"_embedding_api_key")
	rt.loadingM[kind] = true
	a := rt.s.app
	a.Go(func(ctx context.Context) {
		r, err := a.Client.ListEmbeddingModels(ctx, provider, base, key)
		a.Update(func() {
			rt.loadingM[kind] = false
			rt.models[kind], rt.modelErr[kind] = r.Models, r.Error
			if err != nil {
				rt.models[kind], rt.modelErr[kind] = nil, err.Error()
			}
		})
	})
}

func (rt *remTools) tail(c *ui.Context, d *Doc) {
	t := kit.Current()
	kit.SettingsSection(c, "Code indexing", "", func() {
		kit.SettingsRow(c, "Re-index all", "Trigger a full re-index of all registered code projects.", func() {
			if kit.SecondaryButton(c.Key("reindex"), "Re-index now").Clicked() {
				rt.reindex()
			}
		})
		kit.SettingsRow(c, "Index working directory", "Index the current working directory as a new code project.", func() {
			lab := "Index working directory"
			if rt.indexing {
				lab = "Indexing…"
			}
			if kit.Button(c.Key("idx"), kit.ButtonOpts{Label: lab, Variant: kit.Secondary, Disabled: rt.indexing}).Clicked() {
				rt.indexWorkdir(d)
			}
		})
		for _, p := range rt.projects {
			cc := c.Key("proj:" + p.ProjectID)
			kit.SettingsRow(cc, orStr(p.Name, p.ProjectID), p.RootPath, func() {
				kit.Badge(cc, p.Status, kit.ToneNeutral)
				if v, ok := d.vals["context_enrichment_code_project"]; ok {
					if v.s == p.ProjectID {
						ui.Text(cc, "selected").FontSize(t.TextXS).TextColor(t.Accent)
					} else if kit.Button(cc.Key("use"), kit.ButtonOpts{Label: "Use for enrichment", Size: kit.SizeSM}).Clicked() {
						v.s = p.ProjectID
					}
				}
			})
		}
	})
}

// providerField is the embedding provider of kind ("document" / "code");
// choosing one lists its models.
func (rt *remTools) providerField(kind, label string, visible func(*Doc) bool) Field {
	return Field{Path: kind + "_embedding_provider", Label: label, Kind: KSelect, Opts: embProviders(), Visible: visible,
		After: func(d *Doc) {
			rt.models[kind], rt.modelErr[kind] = nil, ""
			rt.loadModels(d, kind)
		}}
}

// modelField is the embedding model of kind: free text with suggestions
// from the models the provider offers, and a button to list them again.
func (rt *remTools) modelField(kind, label string, visible func(*Doc) bool) Field {
	return Field{Path: kind + "_embedding_model", Label: label, Kind: KText, Placeholder: "text-embedding-3-small", Width: 320, Visible: visible,
		Control: func(c *ui.Context, d *Doc, v *string, w float32) ui.Element {
			row := ui.Row(c).Gap(8).AlignItems(ui.Center)
			row.Children(func() {
				items := make([]suggestion, 0, len(rt.models[kind]))
				for _, m := range rt.models[kind] {
					items = append(items, suggestion{ID: m.ID, Title: orStr(m.Name, m.ID), Meta: m.Size})
				}
				status := ""
				if rt.loadingM[kind] {
					status = "Loading models..."
				}
				suggestField(c.Key("model"), v, w-40, "text-embedding-3-small", items, status)
				prov := d.Get(kind + "_embedding_provider")
				if kit.Button(c.Key("refresh"), kit.ButtonOpts{Icon: kit.IconRefresh, Tooltip: "List the provider's models", Variant: kit.Ghost, Size: kit.SizeSM,
					Disabled: prov == "" || rt.loadingM[kind]}).Clicked() {
					rt.loadModels(d, kind)
				}
			})
			return row
		},
		Extra: func(c *ui.Context, d *Doc) {
			t := kit.Current()
			ui.Text(c, rt.hint(kind)).FontSize(t.TextXS).TextColor(t.FgMuted).Padding(0, 16, 10, 16)
		},
	}
}

// testField tests the saved embedding configuration of kind.
func (rt *remTools) testField(kind string, visible func(*Doc) bool) Field {
	return Field{Path: "ui:test_" + kind, Label: "Test connection", Desc: "Tests the saved configuration: save first to test changes.", Kind: KStatic, Visible: visible,
		Control: func(c *ui.Context, d *Doc, _ *string, _ float32) ui.Element {
			row := ui.Row(c).Gap(8).AlignItems(ui.Center)
			row.Children(func() {
				if r := rt.tests[kind]; r != nil {
					tone, txt := kit.ToneDanger, r.Error
					if r.OK {
						tone, txt = kit.ToneSuccess, fmt.Sprintf("OK — %dd, %dms", r.Dimension, r.LatencyMS)
					}
					kit.Badge(c, txt, tone)
				}
				lab := "Test connection"
				if rt.testing[kind] {
					lab = "Testing…"
				}
				if kit.Button(c.Key("t"), kit.ButtonOpts{Label: lab, Size: kit.SizeSM, Variant: kit.Secondary, Disabled: rt.testing[kind]}).Clicked() {
					rt.test(kind)
				}
			})
			return row
		},
	}
}

func (rt *remTools) hint(kind string) string {
	switch {
	case rt.modelErr[kind] != "":
		return "Could not list models: " + rt.modelErr[kind] + ". Type the model name manually."
	case len(rt.models[kind]) > 0:
		return fmt.Sprintf("%d model(s) available. You can also type any model name.", len(rt.models[kind]))
	}
	return "Select an embedding provider to list its models."
}

func (rt *remTools) test(kind string) {
	a := rt.s.app
	rt.testing[kind], rt.tests[kind] = true, nil
	a.Go(func(ctx context.Context) {
		r, err := a.Client.TestEmbedding(ctx, kind)
		a.Update(func() {
			rt.testing[kind] = false
			if err != nil {
				r = &client.EmbeddingTestResult{Error: err.Error()}
			}
			rt.tests[kind] = r
		})
	})
}

func (rt *remTools) reindex() {
	a := rt.s.app
	a.Go(func(ctx context.Context) {
		r, err := a.Client.ReindexAll(ctx)
		a.Update(func() {
			switch {
			case err != nil:
				a.Toasts.Error("Re-index failed", err.Error())
			case len(r.Started) == 0:
				a.Toasts.Info("No code projects registered — nothing to re-index", "")
			default:
				msg := fmt.Sprintf("Re-index started for %d project(s)", len(r.Started))
				if len(r.Failed) > 0 {
					msg += fmt.Sprintf(" (%d failed)", len(r.Failed))
				}
				a.Toasts.Success(msg, "")
			}
		})
	})
}

func (rt *remTools) indexWorkdir(d *Doc) {
	a := rt.s.app
	rt.indexing = true
	a.Go(func(ctx context.Context) {
		pid, _, err := a.Client.IndexWorkdir(ctx)
		var list []client.CodeProjectInfo
		if err == nil {
			list, _ = a.Client.CodeProjects(ctx)
		}
		a.Update(func() {
			rt.indexing = false
			if err != nil {
				a.Toasts.Error("Indexing failed", err.Error())
				return
			}
			a.Toasts.Success("Indexing started — project: "+pid, "")
			rt.projects = list
			if v, ok := d.vals["context_enrichment_code_project"]; ok {
				v.s = pid
			}
		})
	})
}
