package settings

import "github.com/digiogithub/pando/internal/nativeui/client"

// Field constructors keep the data-driven sections short.
func sw(path, label, desc string) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KBool}
}
func txt(path, label, desc string) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KText}
}
func num(path, label, desc string) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KInt, Width: 120}
}
func flt(path, label, desc string) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KFloat, Width: 120}
}
func secret(path, label, ph string) Field {
	return Field{Path: path, Label: label, Kind: KSecret, Placeholder: ph}
}
func list(path, label, desc, ph string) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KList, Placeholder: ph}
}
func sel(path, label, desc string, opts ...Opt) Field {
	return Field{Path: path, Label: label, Desc: desc, Kind: KSelect, Opts: opts}
}
func opts(vs ...string) []Opt {
	o := make([]Opt, len(vs))
	for i, v := range vs {
		o[i] = Opt{v, v}
	}
	return o
}
func selv(path, label string, vs ...string) Field {
	return Field{Path: path, Label: label, Kind: KSelect, Opts: opts(vs...)}
}
func when(f Field, enabled func(d *Doc) bool) Field { f.Enabled = enabled; return f }
func on(path string) func(d *Doc) bool              { return func(d *Doc) bool { return d.Bool(path) } }
func inv(f Field) Field                             { f.Inverse = true; return f }

const g = "settings.general."

// generalDoc is the General section: GET/PUT /api/v1/settings. Language is
// a UI preference stored through /api/v1/ui/preferences.
func generalDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathSettings,
		Group{Fields: []Field{
			{Path: "home_directory", Label: g + "homeDirectory", Kind: KStatic, Width: 320},
			{Path: "working_directory", Label: g + "workingDirectory", Kind: KText, Placeholder: g + "workingDirectoryPlaceholder", Width: 320},
			{Path: "default_model", Label: g + "defaultModel", Kind: KText, Width: 320},
		}},
		Group{Fields: []Field{
			sw("llm_cache_enabled", g+"llmCache", g+"llmCacheDescription"),
			sw("models_dev_enabled", g+"modelsDev", g+"modelsDevDescription"),
			sw("image_auto_resize", "Optimize images", "Resize and recompress images to the model's vision resolution before sending. Saves bandwidth and latency."),
			sw("image_use_files_api", "Anthropic Files API (beta)", "Opt in to the Anthropic beta Messages API and upload images to the Files API once, referencing them by file_id across turns."),
			sw("output_filter_enabled", g+"outputFilter", g+"outputFilterDescription"),
			sw("auto_compact", g+"autoCompact", g+"autoCompactDescription"),
			sw("show_hidden_files", g+"showHiddenFiles", g+"showHiddenFilesDescription"),
			sw("nerd_fonts", g+"nerdFonts", g+"nerdFontsDescription"),
			sw("desktop_tray_on_minimize_close", g+"desktopTrayOnMinimizeClose", g+"desktopTrayOnMinimizeCloseDescription"),
		}},
		Group{Title: g + "diagnosticsTitle", Fields: []Field{
			sw("debug", g+"debug", g+"debugDescription"),
			{Path: "telemetry_enabled", Label: g + "telemetryEnabled", Desc: g + "telemetryEnabledDescription", Kind: KBool,
				Enabled: func(d *Doc) bool { return rawBool(d, "telemetry_available") }},
			{Path: "telemetry_min_level", Label: g + "telemetryMinLevel", Kind: KSelect, Visible: on("telemetry_enabled"), Opts: []Opt{
				{"debug", g + "telemetryMinLevelDebug"}, {"info", g + "telemetryMinLevelInfo"},
				{"warn", g + "telemetryMinLevelWarn"}, {"error", g + "telemetryMinLevelError"}}},
		}},
		Group{Title: g + "feedbackOptimizationTitle", Fields: []Field{
			{Path: "caveman_default_mode", Label: g + "caveman", Desc: g + "cavemanDescription", Kind: KSelect, Empty: "off", Opts: []Opt{
				{"off", g + "cavemanOff"}, {"lite", "Lite"}, {"full", "Full"}, {"ultra", "Ultra"}}},
		}},
		Group{Fields: []Field{
			sw("tool_discovery_enabled", g+"toolDiscovery", g+"toolDiscoveryDescription"),
			when(Field{Path: "tool_discovery_mode", Label: g + "toolDiscoveryMode", Kind: KSelect, Opts: []Opt{
				{"auto", g + "toolDiscoveryModeAuto"}, {"always", g + "toolDiscoveryModeAlways"}, {"off", g + "toolDiscoveryModeOff"}}}, on("tool_discovery_enabled")),
			when(num("tool_discovery_max_direct_tools", g+"toolDiscoveryMaxDirectTools", ""), on("tool_discovery_enabled")),
			when(num("tool_discovery_search_limit", g+"toolDiscoverySearchLimit", ""), on("tool_discovery_enabled")),
		}},
		Group{Title: "Project workspaces", Fields: []Field{
			num("projects_max_web_instances", "Max running workspaces", "Maximum number of background project workspaces Pando may run at once. Set 0 for no limit."),
			txt("projects_web_startup_timeout", "Workspace startup timeout", "How long to wait for a project workspace to become healthy before marking startup as failed."),
		}},
		Group{Title: g + "delegation", Fields: delegationFields()},
	)
	d.Skip = []string{"language"}
	return d
}

func rawBool(d *Doc, key string) bool {
	if d.raw == nil {
		return false
	}
	b, _ := d.raw[key].(bool)
	return b
}

func delegationFields() []Field {
	en, br, warm := on("delegation_enabled"), on("delegation_breaker"), func(d *Doc) bool { return d.Bool("delegation_enabled") && d.Bool("delegation_reuse_warm_instances") }
	return []Field{
		sw("delegation_enabled", g+"delegationEnabled", g+"delegationEnabledDescription"),
		sw("delegation_inject_into_live_loop", g+"delegationInjectIntoLiveLoop", g+"delegationInjectIntoLiveLoopDescription"),
		sw("delegation_resurrect_idle_loop", g+"delegationResurrectIdleLoop", g+"delegationResurrectIdleLoopDescription"),
		sw("delegation_synthesize_fallback", g+"delegationSynthesizeFallback", g+"delegationSynthesizeFallbackDescription"),
		when(num("delegation_max_resurrections", g+"delegationMaxResurrections", ""), en),
		sw("delegation_allow_nested_subagents", "settings.mesnada.nesting.allowNested", "settings.mesnada.nesting.allowNestedDescription"),
		when(num("delegation_max_depth", g+"delegationMaxDepth", "settings.mesnada.nesting.maxDepthHint"), on("delegation_allow_nested_subagents")),
		when(num("delegation_max_concurrent", g+"delegationMaxConcurrent", ""), en),
		when(txt("delegation_resurrection_timeout", g+"delegationResurrectionTimeout", ""), en),
		sw("delegation_reuse_warm_instances", g+"delegationReuseWarmInstances", g+"delegationReuseWarmInstancesDescription"),
		sw("delegation_auto_start_warm", g+"delegationAutoStartWarm", g+"delegationAutoStartWarmDescription"),
		when(txt("delegation_warm_idle_timeout", g+"delegationWarmIdleTimeout", ""), warm),
		when(num("delegation_warm_queue_depth", g+"delegationWarmQueueDepth", ""), warm),
		sw("delegation_allow_external_warm_targets", g+"delegationAllowExternalWarmTargets", g+"delegationAllowExternalWarmTargetsDescription"),
		sw("delegation_accept_delegations", g+"delegationAcceptDelegations", g+"delegationAcceptDelegationsDescription"),
		sw("delegation_conclusion_gate", g+"delegationConclusionGate", g+"delegationConclusionGateDescription"),
		sw("delegation_breaker", g+"delegationBreaker", g+"delegationBreakerDescription"),
		when(num("delegation_max_task_retries", g+"delegationMaxTaskRetries", ""), br),
		when(txt("delegation_rate_limit_cooldown", g+"delegationRateLimitCooldown", ""), br),
		when(txt("delegation_recent_success_window", g+"delegationRecentSuccessWindow", ""), br),
		sw("delegation_event_log", g+"delegationEventLog", g+"delegationEventLogDescription"),
		when(num("delegation_event_log_max_entries", g+"delegationEventLogMaxEntries", ""), on("delegation_event_log")),
		num("orchestrator_max_parallel", g+"orchestratorMaxParallel", ""),
		num("orchestrator_max_per_engine", g+"orchestratorMaxPerEngine", ""),
		txt("orchestrator_claim_ttl", g+"orchestratorClaimTtl", ""),
		txt("orchestrator_dispatch_interval", g+"orchestratorDispatchInterval", ""),
	}
}

func toolsDoc(s *Settings) *Doc {
	return newDoc(s, client.PathConfigTools,
		Group{Title: "Fetch", Fields: []Field{sw("fetchEnabled", "Enabled", ""), num("fetchMaxSizeMB", "Max response size (MB)", "")}},
		Group{Title: "Google Search", Fields: []Field{sw("googleSearchEnabled", "Enabled", ""),
			secret("googleApiKey", "API key", "Enter Google API key…"), txt("googleSearchEngineId", "Custom search engine ID (CX)", "")}},
		Group{Title: "Brave Search", Fields: []Field{sw("braveSearchEnabled", "Enabled", ""), secret("braveApiKey", "API key", "Enter Brave Search API key…")}},
		Group{Title: "Perplexity", Fields: []Field{sw("perplexitySearchEnabled", "Enabled", ""), secret("perplexityApiKey", "API key", "Enter Perplexity API key…")}},
		Group{Title: "Exa", Fields: []Field{sw("exaSearchEnabled", "Enabled", ""), secret("exaApiKey", "API key", "Enter Exa API key…")}},
		Group{Title: "Sourcegraph", Fields: []Field{sw("sourcegraphEnabled", "Enabled", ""), secret("sourcegraphToken", "Access token (optional — uses public API if empty)", "")}},
		Group{Title: "Context7 (Library Docs)", Fields: []Field{sw("context7Enabled", "Enabled", "")}},
		Group{Title: "Browser (Chrome DevTools)", Fields: []Field{
			sw("browserEnabled", "Enabled", ""),
			{Path: "browserType", Label: "Browser", Kind: KSelect, Opts: []Opt{{"chrome", "Google Chrome"}, {"msedge", "Microsoft Edge"}, {"chromium", "Chromium"}, {"opera", "Opera"}}},
			{Path: "browserExecutable", Label: "Browser executable", Kind: KText, Placeholder: "Auto-detected from selected browser", Width: 320},
			{Path: "browserUserDataDir", Label: "User data directory", Kind: KText, Placeholder: "/tmp/pando-browser", Width: 320},
			sw("browserHeadless", "Headless", ""), num("browserTimeout", "Timeout (seconds)", ""), num("browserMaxSessions", "Max sessions", ""),
		}},
		Group{Title: "Desktop control", Fields: []Field{
			sw("desktopEnabled", "Enabled", ""),
			{Path: "desktopBackend", Label: "Backend", Kind: KSelect, Opts: []Opt{{"auto", "Auto (platform default)"}, {"atspi", "AT-SPI2 (Linux)"},
				{"uia", "UI Automation (Windows)"}, {"ax", "Accessibility API (macOS)"}, {"cdp", "Chrome DevTools Protocol"}, {"null", "Disabled (Null)"}}},
			sw("desktopAllowPhysicalInput", "Allow physical input", ""),
			num("desktopMaxNodes", "Max nodes", ""), num("desktopDefaultDepth", "Default depth", ""), num("desktopActionTimeout", "Action timeout (s)", ""),
			num("desktopSnapshotTTL", "Snapshot TTL (s)", ""), flt("desktopScreenshotScale", "Screenshot scale", ""),
			list("desktopAllowedApps", "Allowed apps (empty = all)", "", "Add an app…"), list("desktopDeniedApps", "Denied apps", "", "Add an app…"),
		}},
	)
}

func bashDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigBash,
		Group{Title: "Banned commands", Desc: "Commands listed here are always blocked, regardless of user confirmation. Pando's built-in defaults apply when this list is empty.",
			Fields: []Field{list("bannedCommands", "Banned commands", "", "Add a command to ban…")}},
		Group{Title: "Allowed commands", Desc: "Commands listed here skip the confirmation prompt and run immediately. Use this for read-only or trusted commands like ls, cat, git status.",
			Fields: []Field{list("allowedCommands", "Allowed commands", "", "Add a command to allow…")}})
	d.SavedMsg = "Bash settings saved"
	return d
}

const to = "settings.tokenOptimization."

func tokenOptDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigTokenOpt,
		Group{Title: to + "fileReadsSection", Fields: []Field{
			{Path: "readModeDefault", Label: to + "readModeDefault", Desc: to + "readModeDefaultDescription", Kind: KSelect, Opts: []Opt{
				{"full", to + "readModeFull"}, {"auto", to + "readModeAuto"}, {"signatures", to + "readModeSignatures"}, {"map", to + "readModeMap"}}},
			inv(sw("readDedupDisabled", to+"readDedup", to+"readDedupDescription")),
			sw("readModeLearning", to+"readModeLearning", to+"readModeLearningDescription"),
		}},
		Group{Title: to + "shellOutputSection", Fields: []Field{
			sw("outputFilterEnabled", to+"outputFilter", to+"outputFilterDescription"),
			list("outputFilterPaths", to+"outputFilterPaths", to+"outputFilterPathsDescription", ""),
		}},
		Group{Title: to + "codeGraphSection", Fields: []Field{
			sw("buildCodeGraph", to+"buildCodeGraph", to+"buildCodeGraphDescription"),
			sw("relatedFilesHint", to+"relatedFilesHint", to+"relatedFilesHintDescription"),
		}},
		Group{Title: to + "savingsSection", Fields: []Field{
			inv(sw("savingsLedgerDisabled", to+"savingsLedger", to+"savingsLedgerDescription")),
		}})
	d.SavedMsg = "Token optimization settings saved"
	return d
}

const sb = "settings.sandbox."

func sandboxDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigSandbox,
		Group{Fields: []Field{
			inv(sw("disabled", sb+"enabled", sb+"enabledDescription")),
			{Path: "mode", Label: sb + "mode", Desc: sb + "modeHint", Kind: KSelect, Empty: "workspace-write", Opts: []Opt{
				{"workspace-write", sb + "modeWorkspaceWrite"}, {"read-only", sb + "modeReadOnly"}, {"strict", sb + "modeStrict"}, {"off", sb + "modeOff"}}},
			{Path: "network", Label: sb + "network", Desc: sb + "networkHint", Kind: KSelect, Empty: "allowed", Opts: []Opt{
				{"allowed", sb + "networkAllowed"}, {"restricted", sb + "networkRestricted"}}},
			inv(sw("autoAllowBashDisabled", sb+"autoAllowBash", sb+"autoAllowBashDescription")),
			{Path: "useBwrap", Label: sb + "useBwrap", Kind: KSelect, Empty: "auto", Opts: []Opt{
				{"auto", sb + "bwrapAuto"}, {"always", sb + "bwrapAlways"}, {"never", sb + "bwrapNever"}}},
		}},
		Group{Title: sb + "writableRoots", Desc: sb + "writableRootsHint", Fields: []Field{list("writableRoots", sb+"writableRoots", "", "")}},
		Group{Title: sb + "denyPaths", Desc: sb + "denyPathsHint", Fields: []Field{list("denyPaths", sb+"denyPaths", "", "")}},
		Group{Title: sb + "extendTo", Desc: sb + "extendToHint", Fields: []Field{list("extendTo", sb+"extendTo", "mcp, subagents", "mcp / subagents")}},
	)
	d.Sub = "config"
	d.SavedMsg = "Sandbox settings saved"
	return d
}

func containerDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathContainerConfig,
		Group{Title: "Runtime configuration", Fields: []Field{
			selv("runtime", "Runtime", "host", "docker", "podman", "embedded", "auto"),
			txt("image", "Image", ""),
			selv("pull_policy", "Pull policy", "if-not-present", "always", "never"),
			txt("socket", "Socket", ""), txt("work_dir", "Work dir", ""),
			selv("network", "Network", "none", "bridge", "host", "slirp4netns"),
			txt("user", "User", ""), txt("cpu_limit", "CPU limit", ""), txt("mem_limit", "Memory limit", ""), num("pids_limit", "PIDs limit", ""),
		}},
		Group{Title: "Security", Fields: []Field{
			sw("read_only", "Read-only root filesystem", "Recommended secure default for containerized sessions."),
			sw("no_new_privileges", "No new privileges", "Prevent processes from gaining additional Linux privileges."),
		}},
		Group{Title: "Advanced", Fields: []Field{
			list("allow_env", "Allowed environment variables", "", ""), list("allow_mounts", "Allowed mount paths", "", ""),
			list("extra_env", "Extra environment", "", "KEY=value"), list("extra_mounts", "Extra mounts", "", "host:container"),
		}})
	d.SavedMsg = "Container settings saved"
	return d
}

func mcpGatewayDoc(s *Settings) *Doc {
	return newDoc(s, client.PathConfigMCPGateway, Group{Fields: []Field{
		sw("enabled", "Enable MCP Gateway", "Automatically track and surface frequently used MCP tools"),
		num("favorite_threshold", "Favorite threshold", "Minimum uses to become a favorite"),
		num("max_favorites", "Max favorites", "Maximum number of favorites"),
		num("favorite_window_days", "Favorite window (days)", "Rolling window for usage counting"),
		num("decay_days", "Decay days", "Days until unused favorites are demoted"),
	}})
}

func snapshotsDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigServices, Group{Fields: []Field{
		sw("enabled", "Enabled", "Enable session snapshot system"),
		num("maxSnapshots", "Max snapshots", ""), txt("maxFileSize", "Max file size", "e.g. 10MB, 500KB"),
		num("autoCleanupDays", "Auto cleanup (days)", "Automatically delete old snapshots after this many days (0 = never)"),
	}}, Group{Title: "Exclude patterns", Desc: "Files matching these patterns are never included in a snapshot.",
		Fields: []Field{list("excludePatterns", "Exclude patterns", "", "")}})
	d.Sub, d.PutWhole = "snapshots", true
	d.SavedMsg = "Services settings saved"
	return d
}

func luaDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigExtensions, Group{Fields: []Field{
		sw("enabled", "Enabled", "Activate the Lua scripting engine for custom hooks and filters"),
		txt("script_path", "Script path", ""), txt("timeout", "Timeout", "Go duration, e.g. 30s"),
		sw("strict_mode", "Strict mode", "Treat Lua errors as fatal and halt execution"),
		sw("hot_reload", "Hot reload", "Automatically reload Lua scripts when the file changes"),
		sw("log_filtered_data", "Log filtered data", "Log data that was filtered or blocked by Lua scripts"),
	}})
	d.Sub, d.PutWhole = "lua", true
	d.SavedMsg = "Lua settings saved"
	return d
}

func evaluatorDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigEvaluator,
		Group{Fields: []Field{
			sw("enabled", "Enabled", "Activate the self-improvement evaluation loop (LLM-as-Judge)"),
			txt("model", "Judge model", "Select the model that will act as the judge for performance metrics."),
			txt("provider", "Judge provider", ""),
		}},
		Group{Title: "Reward weights", Fields: []Field{
			{Path: "weights.success", Label: "Success (corrections)", Kind: KSlider}, {Path: "weights.tokens", Label: "Token efficiency", Kind: KSlider},
			{Path: "weights.toolErrors", Label: "Tool errors", Kind: KSlider}, {Path: "weights.cancels", Label: "Cancelled runs", Kind: KSlider},
			{Path: "weights.repetition", Label: "Repeated tool calls", Kind: KSlider}, {Path: "weights.turns", Label: "Turns to completion", Kind: KSlider},
			{Path: "weights.endState", Label: "Ended right after an error", Kind: KSlider},
		}},
		Group{Title: "UCB settings", Fields: []Field{
			flt("explorationC", "UCB exploration factor", ""), num("minSessionsForUCB", "Minimum sessions for UCB", ""),
			num("maxTokensBaseline", "Max tokens baseline", ""), num("maxSkills", "Max skills", ""),
			sw("async", "Async evaluation", "Run evaluation in the background after session end (recommended)"),
		}},
		Group{Title: "When sessions are evaluated", Fields: []Field{
			txt("idleTimeout", "Idle timeout", "A session with no new message for this long is scored by the idle sweeper (Go duration, e.g. 30m, 2h)."),
			num("backfillLimit", "Backfill limit", "Historical unscored sessions evaluated at startup. 0 uses the default 50; a negative value disables the backfill."),
			sw("backfillJudge", "Judge during backfill", "Also run the LLM judge on backfilled sessions (costs judge calls; off by default)"),
			sw("includeSubagents", "Include subagent sessions", "Also evaluate delegated (child) sessions. Off by default: only root sessions are scored."),
		}},
		Group{Title: "LLM judge gating and budget", Fields: []Field{
			{Path: "judge.highReward", Label: "High reward band (judge at or above)", Kind: KSlider},
			{Path: "judge.lowReward", Label: "Low reward band (judge at or below)", Kind: KSlider},
			num("judge.minTurns", "Minimum user turns", "The judge only runs for sessions with at least this many user turns."),
			num("judge.maxTranscriptTokens", "Transcript cap (tokens)", "The judge prompt keeps the head and tail of the transcript within this budget."),
			num("judge.dailyCalls", "Daily judge calls", "Maximum judge calls per local day. 0 means unlimited."),
			num("judge.dailyTokens", "Daily judge tokens", "Maximum estimated judge tokens per local day. 0 means unlimited."),
		}},
		Group{Title: "Prompt variants and context trimming", Fields: []Field{
			sw("templates.enabled", "Prompt variant selection", "A/B test the variant files under .pando/prompts/variants/<section>/<name>.md.tpl. Inert until such files exist."),
			sw("contextTrimmer.enabled", "Context trimmer", "Experimental: an extra LLM call on every new session that filters the tools shown to the model. Off by default."),
			flt("contextTrimmer.minConfidence", "Trimmer minimum confidence", ""),
		}},
		Group{Title: "Correction patterns", Fields: []Field{list("correctionsPatterns", "Correction patterns", "Phrases that mark a user correction.", "")}},
		Group{Title: "Judge prompt template", Fields: []Field{{Path: "judgePromptTemplate", Label: "Judge prompt template file", Desc: "Path of a template file; empty uses the built-in default.", Kind: KText, Width: 360}}},
	)
	d.SavedMsg = "Evaluator settings saved"
	return d
}

func apiServerDoc(s *Settings) *Doc {
	d := newDoc(s, client.PathConfigServices, Group{Fields: []Field{
		sw("enabled", "Enabled", "Enable the HTTP API server"),
		txt("host", "Host", ""), num("port", "Port", "Changing host or port requires restarting the API server."),
		sw("requireAuth", "Require authentication", "Protect API endpoints with a bearer token"),
	}})
	d.Sub, d.PutWhole = "server", true
	d.SavedMsg = "Services settings saved"
	return d
}
