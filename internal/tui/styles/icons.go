package styles

import (
	"path/filepath"
	"strings"
	"sync/atomic"
)

// iconSet bundles every themable glyph the TUI prints. We keep two of them: a
// Nerd Font set (the historical default) and a plain fallback that avoids the
// Private Use Area codepoints Nerd Fonts rely on. A terminal only renders Nerd
// Font glyphs when the user has a patched font configured; otherwise it shows
// tofu (□). The fallback uses widely-supported BMP/ASCII symbols so the UI stays
// legible on any terminal. See SetNerdFonts.
type iconSet struct {
	Pando string

	// Diagnostics
	Check   string
	Error   string
	Warning string
	Info    string
	Hint    string

	// Status
	Spinner string
	Loading string
	Success string
	Failure string

	// Files & documents
	Document   string
	File       string
	Folder     string
	FolderOpen string

	// Git
	GitBranch   string
	GitCommit   string
	GitMerge    string
	GitAdded    string
	GitRemoved  string
	GitModified string

	// Navigation
	ArrowRight   string
	ArrowDown    string
	ChevronRight string
	ChevronDown  string
	ChevronLeft  string
	ChevronUp    string

	// UI
	Search    string
	Settings  string
	Chat      string
	Editor    string
	SplitView string
	Terminal  string
	Close     string
	Plus      string
	Minus     string
	Lock      string
	Unlock    string
	Clipboard string
	Bookmark  string

	// Provider / AI
	Robot string
	AI    string
	Brain string
	Magic string

	// Misc
	Bug       string
	Flame     string
	Lightbulb string
	Clock     string
	Calendar  string
	Tag       string
	Link      string
	Key       string
	Shield    string
	Database  string
	Cloud     string
	Package   string

	// Spinner animation frames.
	SpinnerFrames []string

	// GenericFile is returned by FileIconFor when no specific match is found
	// (and, in fallback mode, for every file since per-type glyphs are Nerd
	// Font only).
	GenericFile string
}

var nerdIconSet = iconSet{
	Pando: "木",

	Check:   "󰗠",
	Error:   "󰅚",
	Warning: "󰀪",
	Info:    "󰋽",
	Hint:    "󰌵",

	Spinner: "󰔟",
	Loading: "󰑓",
	Success: "󰄬",
	Failure: "󰜺",

	Document:   "󰈙",
	File:       "󰈙",
	Folder:     "󰉋",
	FolderOpen: "󰝰",

	GitBranch:   "󰘬",
	GitCommit:   "󰜘",
	GitMerge:    "󰘬",
	GitAdded:    "󰐕",
	GitRemoved:  "󰍴",
	GitModified: "󰏫",

	ArrowRight:   "󰁔",
	ArrowDown:    "󰁅",
	ChevronRight: "󰅂",
	ChevronDown:  "󰅀",
	ChevronLeft:  "󰅁",
	ChevronUp:    "󰅃",

	Search:    "󰍉",
	Settings:  "󰒓",
	Chat:      "󰭹",
	Editor:    "󰈮",
	SplitView: "󰜫",
	Terminal:  "󰆍",
	Close:     "󰅖",
	Plus:      "󰐕",
	Minus:     "󰍴",
	Lock:      "󰌾",
	Unlock:    "󰌿",
	Clipboard: "󰅌",
	Bookmark:  "󰃃",

	Robot: "󰚩",
	AI:    "󱜚",
	Brain: "󰧠",
	Magic: "󰘦",

	Bug:       "󰨰",
	Flame:     "󰈸",
	Lightbulb: "󰌵",
	Clock:     "󰥔",
	Calendar:  "󰃭",
	Tag:       "󰓹",
	Link:      "󰌹",
	Key:       "󰌋",
	Shield:    "󰒃",
	Database:  "󰆼",
	Cloud:     "󰅟",
	Package:   "󰏗",

	SpinnerFrames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	GenericFile:   "󰈙",
}

// asciiIconSet is the fallback used when Nerd Fonts are disabled. Every glyph is
// a BMP symbol or plain ASCII that virtually all terminals render without a
// patched font.
var asciiIconSet = iconSet{
	Pando: "✦",

	Check:   "✓",
	Error:   "✗",
	Warning: "!",
	Info:    "i",
	Hint:    "*",

	Spinner: "*",
	Loading: "~",
	Success: "✓",
	Failure: "✗",

	Document:   "≡",
	File:       "≡",
	Folder:     "▸",
	FolderOpen: "▾",

	GitBranch:   "ᚸ",
	GitCommit:   "●",
	GitMerge:    "ᚷ",
	GitAdded:    "+",
	GitRemoved:  "-",
	GitModified: "~",

	ArrowRight:   "→",
	ArrowDown:    "↓",
	ChevronRight: "›",
	ChevronDown:  "⌄",
	ChevronLeft:  "‹",
	ChevronUp:    "⌃",

	Search:    "⌕",
	Settings:  "⚙",
	Chat:      "▣",
	Editor:    "✎",
	SplitView: "▥",
	Terminal:  "▮",
	Close:     "✕",
	Plus:      "+",
	Minus:     "-",
	Lock:      "▪",
	Unlock:    "▫",
	Clipboard: "▤",
	Bookmark:  "◆",

	Robot: "◉",
	AI:    "✦",
	Brain: "◎",
	Magic: "✶",

	Bug:       "✸",
	Flame:     "△",
	Lightbulb: "*",
	Clock:     "◷",
	Calendar:  "▦",
	Tag:       "◈",
	Link:      "∞",
	Key:       "⚷",
	Shield:    "⛉",
	Database:  "▤",
	Cloud:     "☁",
	Package:   "▢",

	SpinnerFrames: []string{"|", "/", "-", "\\"},
	GenericFile:   "≡",
}

// Exported glyph variables. They are vars (not consts) so the active icon set
// can be swapped at runtime via SetNerdFonts; call sites keep using
// styles.CheckIcon, styles.FolderIcon, etc. unchanged.
var (
	PandoIcon    string
	OpenCodeIcon string

	CheckIcon   string
	ErrorIcon   string
	WarningIcon string
	InfoIcon    string
	HintIcon    string

	SpinnerIcon string
	LoadingIcon string
	SuccessIcon string
	FailureIcon string

	DocumentIcon   string
	FileIcon       string
	FolderIcon     string
	FolderOpenIcon string

	GitBranchIcon   string
	GitCommitIcon   string
	GitMergeIcon    string
	GitAddedIcon    string
	GitRemovedIcon  string
	GitModifiedIcon string

	ArrowRightIcon string
	ArrowDownIcon  string
	ChevronRight   string
	ChevronDown    string
	ChevronLeft    string
	ChevronUp      string

	SearchIcon    string
	SettingsIcon  string
	ChatIcon      string
	EditorIcon    string
	SplitViewIcon string
	TerminalIcon  string
	CloseIcon     string
	PlusIcon      string
	MinusIcon     string
	LockIcon      string
	UnlockIcon    string
	ClipboardIcon string
	BookmarkIcon  string

	RobotIcon string
	AIIcon    string
	BrainIcon string
	MagicIcon string

	BugIcon       string
	FlameIcon     string
	LightbulbIcon string
	ClockIcon     string
	CalendarIcon  string
	TagIcon       string
	LinkIcon      string
	KeyIcon       string
	ShieldIcon    string
	DatabaseIcon  string
	CloudIcon     string
	PackageIcon   string

	// SpinnerFrames is the animation used by spinners across the TUI.
	SpinnerFrames []string
)

// nerdFontsEnabled tracks the active mode (1 = Nerd Fonts, 0 = ASCII fallback)
// so FileIconFor and NerdFontsEnabled can read it without locking.
var nerdFontsEnabled int32 = 1

func init() {
	// Default to the Nerd Font set, preserving historical behavior. Callers
	// (e.g. app startup) may switch to the fallback via SetNerdFonts.
	applyIconSet(nerdIconSet)
}

// SetNerdFonts selects the active icon set. When enabled is false, the TUI
// switches to the plain fallback glyphs that render on terminals without a Nerd
// Font installed. Safe to call once during startup before the TUI renders.
func SetNerdFonts(enabled bool) {
	if enabled {
		atomic.StoreInt32(&nerdFontsEnabled, 1)
		applyIconSet(nerdIconSet)
		return
	}
	atomic.StoreInt32(&nerdFontsEnabled, 0)
	applyIconSet(asciiIconSet)
}

// NerdFontsEnabled reports whether the Nerd Font icon set is currently active.
func NerdFontsEnabled() bool {
	return atomic.LoadInt32(&nerdFontsEnabled) == 1
}

func applyIconSet(s iconSet) {
	PandoIcon = s.Pando
	OpenCodeIcon = s.Pando

	CheckIcon = s.Check
	ErrorIcon = s.Error
	WarningIcon = s.Warning
	InfoIcon = s.Info
	HintIcon = s.Hint

	SpinnerIcon = s.Spinner
	LoadingIcon = s.Loading
	SuccessIcon = s.Success
	FailureIcon = s.Failure

	DocumentIcon = s.Document
	FileIcon = s.File
	FolderIcon = s.Folder
	FolderOpenIcon = s.FolderOpen

	GitBranchIcon = s.GitBranch
	GitCommitIcon = s.GitCommit
	GitMergeIcon = s.GitMerge
	GitAddedIcon = s.GitAdded
	GitRemovedIcon = s.GitRemoved
	GitModifiedIcon = s.GitModified

	ArrowRightIcon = s.ArrowRight
	ArrowDownIcon = s.ArrowDown
	ChevronRight = s.ChevronRight
	ChevronDown = s.ChevronDown
	ChevronLeft = s.ChevronLeft
	ChevronUp = s.ChevronUp

	SearchIcon = s.Search
	SettingsIcon = s.Settings
	ChatIcon = s.Chat
	EditorIcon = s.Editor
	SplitViewIcon = s.SplitView
	TerminalIcon = s.Terminal
	CloseIcon = s.Close
	PlusIcon = s.Plus
	MinusIcon = s.Minus
	LockIcon = s.Lock
	UnlockIcon = s.Unlock
	ClipboardIcon = s.Clipboard
	BookmarkIcon = s.Bookmark

	RobotIcon = s.Robot
	AIIcon = s.AI
	BrainIcon = s.Brain
	MagicIcon = s.Magic

	BugIcon = s.Bug
	FlameIcon = s.Flame
	LightbulbIcon = s.Lightbulb
	ClockIcon = s.Clock
	CalendarIcon = s.Calendar
	TagIcon = s.Tag
	LinkIcon = s.Link
	KeyIcon = s.Key
	ShieldIcon = s.Shield
	DatabaseIcon = s.Database
	CloudIcon = s.Cloud
	PackageIcon = s.Package

	SpinnerFrames = s.SpinnerFrames
}

var fileNameIcons = map[string]string{
	"dockerfile":          "󰡨",
	"docker-compose.yml":  "󰡨",
	"docker-compose.yaml": "󰡨",
	"makefile":            "󱁤",
	"cmakelists.txt":      "󰔷",
	"license":             "󰿃",
	"readme":              "󰍔",
	"readme.md":           "󰍔",
	"readme.txt":          "󰍔",
	".gitignore":          "󰊢",
	".gitattributes":      "󰊢",
	".gitmodules":         "󰊢",
	".editorconfig":       "󰒓",
	".env":                "󰌾",
	".env.local":          "󰌾",
	".env.example":        "󰈞",
	"go.mod":              "󰟓",
	"go.sum":              "󰟓",
	"cargo.toml":          "󰣶",
	"cargo.lock":          "󰣶",
	"package.json":        "󰎙",
	"package-lock.json":   "󰎙",
	"tsconfig.json":       "󰛦",
	"webpack.config.js":   "󰜫",
	"vite.config.ts":      "󱐋",
	"vite.config.js":      "󱐋",
	".eslintrc":           "󰱺",
	".eslintrc.js":        "󰱺",
	".eslintrc.json":      "󰱺",
	".prettierrc":         "󰏫",
	"gemfile":             "󰴭",
	"rakefile":            "󰴭",
	"requirements.txt":    "󰌠",
	"setup.py":            "󰌠",
	"pyproject.toml":      "󰌠",
	"flake.nix":           "󱄅",
	"default.nix":         "󱄅",
	"shell.nix":           "󱄅",
	"mix.exs":             "󰣹",
	"mix.lock":            "󰣹",
}

var fileExtIcons = map[string]string{
	".go":         "󰟓",
	".mod":        "󰟓",
	".sum":        "󰟓",
	".py":         "󰌠",
	".pyi":        "",
	".pyc":        "",
	".rs":         "󱘗",
	".c":          "󰙱",
	".h":          "󰙱",
	".cpp":        "󰙲",
	".cxx":        "",
	".cc":         "",
	".hpp":        "",
	".hxx":        "",
	".java":       "󰬷",
	".jar":        "",
	".class":      "",
	".rb":         "󰴭",
	".erb":        "",
	".php":        "󰌟",
	".js":         "󰌞",
	".mjs":        "",
	".cjs":        "",
	".jsx":        "",
	".ts":         "󰛦",
	".tsx":        "󰜈",
	".html":       "󰌝",
	".htm":        "",
	".css":        "󰌜",
	".scss":       "󰌜",
	".sass":       "󰌜",
	".less":       "󰌜",
	".lua":        "󰢱",
	".vim":        "󰕷",
	".swift":      "󰛥",
	".kt":         "󱈙",
	".kts":        "",
	".dart":       "󰢵",
	".r":          "󰟔",
	".rmd":        "󰟔",
	".zig":        "",
	".nim":        "",
	".ex":         "󰣹",
	".exs":        "󰣹",
	".erl":        "󰟀",
	".hrl":        "",
	".hs":         "",
	".lhs":        "",
	".ml":         "",
	".mli":        "",
	".clj":        "",
	".cljs":       "",
	".cljc":       "",
	".scala":      "",
	".sbt":        "",
	".graphql":    "󰡷",
	".gql":        "󰡷",
	".proto":      "󰯂",
	".sql":        "󰆼",
	".sh":         "󰆍",
	".bash":       "󰆍",
	".zsh":        "󰆍",
	".fish":       "󰆍",
	".ps1":        "󰨊",
	".md":         "󰍔",
	".mdx":        "󰍔",
	".json":       "󰘦",
	".jsonc":      "󰘦",
	".yaml":       "󰈙",
	".yml":        "󰈙",
	".toml":       "󰈙",
	".ini":        "󰈙",
	".cfg":        "󰈙",
	".conf":       "󰈙",
	".xml":        "󰗀",
	".csv":        "󰈛",
	".tsv":        "󰈛",
	".txt":        "󰈙",
	".log":        "󰈙",
	".svg":        "󰜡",
	".png":        "󰋩",
	".jpg":        "󰋩",
	".jpeg":       "󰋩",
	".gif":        "󰋩",
	".bmp":        "󰋩",
	".ico":        "󰋩",
	".webp":       "󰋩",
	".wasm":       "󰘧",
	".lock":       "󰌾",
	".env":        "󰌾",
	".dockerfile": "󰡨",
	".tf":         "󱁢",
	".hcl":        "󱁢",
	".nix":        "󱄅",
	".vue":        "󰡄",
	".svelte":     "󰜈",
	".astro":      "󰑣",
	".sol":        "󰡪",
	".tex":        "󰙩",
	".bib":        "",
	".pdf":        "󰈦",
	".zip":        "󰗄",
	".tar":        "󰗄",
	".gz":         "󰗄",
	".7z":         "󰗄",
	".rar":        "󰗄",
	".rpm":        "󰗄",
	".deb":        "󰗄",
	".dmg":        "󰗄",
	".iso":        "󰗄",
}

// FileIconFor returns the icon for a file name. The per-type icons are Nerd Font
// glyphs, so when the fallback set is active every file resolves to the generic
// document glyph instead of unrenderable tofu.
func FileIconFor(name string) string {
	if !NerdFontsEnabled() {
		return DocumentIcon
	}
	lower := strings.ToLower(filepath.Base(name))
	if icon, ok := fileNameIcons[lower]; ok {
		return icon
	}
	ext := strings.ToLower(filepath.Ext(name))
	if icon, ok := fileExtIcons[ext]; ok {
		return icon
	}
	return DocumentIcon
}
